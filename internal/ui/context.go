package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/status"
)

// Contexts keep one thing in front of you at a time: the list shows the
// active context's worktrees and nothing else, and the other contexts are tabs
// in the title bar that say when something there wants you. Without any
// configured, each org is a context; with fewer than two there is nothing to
// switch between and the feature stays out of sight.

// ctxDef is a context as the list sees it.
type ctxDef struct {
	name  string
	match []string
	other bool // the implicit one: whatever no other context takes
}

// has reports whether a row is one the context names.
func (c ctxDef) has(r row) bool {
	for _, entry := range c.match {
		kind, val := "", entry
		if k, v, ok := strings.Cut(entry, ":"); ok && (k == "org" || k == "project") {
			kind, val = k, v
		}
		val = strings.TrimSpace(val)
		if (kind != "project" && strings.EqualFold(val, r.org)) || (kind != "org" && strings.EqualFold(val, r.project)) {
			return true
		}
	}
	return false
}

// contexts lists the contexts in order, or nil when there is nothing to
// switch between.
func (m Model) contexts() []ctxDef {
	var defs []ctxDef
	if len(m.cfg.Contexts) > 0 {
		for _, c := range m.cfg.Contexts {
			defs = append(defs, ctxDef{name: c.Name, match: c.Match})
		}
		if slices.ContainsFunc(m.all, func(r row) bool { return contextOf(defs, r) < 0 }) {
			defs = append(defs, ctxDef{name: "Other", other: true})
		}
	} else {
		seen := map[string]bool{}
		for _, r := range m.all {
			if seen[r.org] {
				continue
			}
			seen[r.org] = true
			name := r.org
			if name == "" {
				name = "Other"
			}
			defs = append(defs, ctxDef{name: name, match: []string{"org:" + r.org}, other: r.org == ""})
		}
	}
	if len(defs) < 2 {
		return nil
	}
	return defs
}

// contextOf is the index of the context a row belongs to: the first that
// names it, else the implicit "Other" one, else -1.
func contextOf(defs []ctxDef, r row) int {
	for i, d := range defs {
		if !d.other && d.has(r) {
			return i
		}
	}
	for i, d := range defs {
		if d.other {
			return i
		}
	}
	return -1
}

// activeContext is the index of the active context in defs; the first when
// the remembered name is gone (the config changed, a project was removed).
func (m Model) activeContext(defs []ctxDef) int {
	if i := slices.IndexFunc(defs, func(d ctxDef) bool { return strings.EqualFold(d.name, m.ctxActive) }); i >= 0 {
		return i
	}
	return 0
}

// inContext reports whether a row is in the active context; with no contexts
// everything is.
func (m Model) inContext(r row) bool {
	defs := m.contexts()
	return defs == nil || contextOf(defs, r) == m.activeContext(defs)
}

// contextRows are the worktrees of the active context.
func (m Model) contextRows() []row {
	defs := m.contexts()
	if defs == nil {
		return m.all
	}
	act := m.activeContext(defs)
	var out []row
	for _, r := range m.all {
		if contextOf(defs, r) == act {
			out = append(out, r)
		}
	}
	return out
}

// contextKey is the configured switch key, or the default for a config built
// by hand.
func (m Model) contextKey() string {
	if k := m.cfg.ContextKey; k != "" {
		return k
	}
	return config.DefaultContextKey
}

// contextStep says whether a key switches context, and which way.
func (m Model) contextStep(key string) (int, bool) {
	switch key {
	case m.contextKey():
		return 1, true
	case "shift+" + m.contextKey():
		return -1, true
	}
	return 0, false
}

// awayEvent is something an agent outside the active context needed: it
// started waiting, finished or failed while you were elsewhere.
type awayEvent struct {
	session string
	state   status.State
	at      time.Time
}

// noteAway remembers that an agent outside the active context needs you, to
// be shown when you go there. One entry per agent: the latest state wins.
func (m *Model) noteAway(session string, s status.State) {
	if s != status.Waiting && s != status.Done && s != status.Error {
		return
	}
	r, ok := m.rowFor(session)
	if !ok || m.inContext(r) {
		return
	}
	next := slices.DeleteFunc(slices.Clone(m.away), func(e awayEvent) bool { return e.session == session })
	m.away = append(next, awayEvent{session: session, state: s, at: time.Now()})
}

// takeAway returns, and forgets, the events for a context that still hold:
// an agent that has moved on since is no longer news.
func (m *Model) takeAway(defs []ctxDef, idx int) []awayEvent {
	var out, keep []awayEvent
	for _, e := range m.away {
		r, ok := m.rowFor(e.session)
		switch {
		case !ok:
		case contextOf(defs, r) != idx:
			keep = append(keep, e)
		case m.agents[e.session].State == e.state:
			out = append(out, e)
		}
	}
	m.away = keep
	return out
}

// waitingIn counts the agents of a context that are waiting on you.
func (m Model) waitingIn(defs []ctxDef, idx int) int {
	n := 0
	for _, r := range m.all {
		if contextOf(defs, r) != idx {
			continue
		}
		for _, a := range r.agentList() {
			if s, ok := m.agentState(a); ok && a.running && s == status.Waiting {
				n++
			}
		}
	}
	return n
}

// contextTabs draws the contexts for the title bar: the active one marked,
// the others dim with a count of the agents there that wait on you.
func (m Model) contextTabs() string {
	defs := m.contexts()
	if defs == nil {
		return ""
	}
	act := m.activeContext(defs)
	var parts []string
	for i, d := range defs {
		if i == act {
			parts = append(parts, badgeStyle.Render(" "+d.name+" "))
			continue
		}
		tab := dimStyle.Render(" " + d.name)
		if n := m.waitingIn(defs, i); n > 0 {
			tab += waitingStyle.Render(fmt.Sprintf(" ◆%d", n))
		}
		parts = append(parts, tab+" ")
	}
	return strings.Join(parts, "")
}

// switchContext moves to the next or previous context. The selection in the
// context you leave is remembered and restored; a search or attention filter
// does not carry over, since it was about the other context.
func (m Model) switchContext(step int) (tea.Model, tea.Cmd) {
	defs := m.contexts()
	if defs == nil {
		m.notice = "only one context: add [[context]] entries to the config, or work in more than one org"
		return m, nil
	}
	cur := m.activeContext(defs)
	if m.hdr == "" && m.selSession != "" {
		m.ctxLast = cloneMap(m.ctxLast)
		m.ctxLast[defs[cur].name] = m.selSession
	}
	next := (cur + step + len(defs)) % len(defs)
	m.ctxActive = defs[next].name
	m.search, m.attention = "", false
	m.applyFilter()
	m.goToRemembered(defs[next].name)
	m.scroll = scrollState{}
	if m.mode == modeFocus {
		if r, ok := m.selected(); !ok || !m.targetRunning(r) {
			m.mode = modeList
		}
	}
	saveContext(m.ctxActive, m.ctxLast)
	m.notice = "context: " + defs[next].name
	if events := m.takeAway(defs, next); len(events) > 0 {
		m.digest, m.digestName, m.digestBack = events, defs[next].name, m.mode
		m.mode = modeDigest
	}
	return m, tea.Batch(m.startPreview(), m.readSelectedGit())
}

// goToRemembered puts the cursor on the worktree last used in a context, or
// on its first one.
func (m *Model) goToRemembered(name string) {
	if len(m.rows) == 0 {
		return
	}
	i := slices.IndexFunc(m.rows, func(r row) bool { return r.session == m.ctxLast[name] })
	if i < 0 {
		i = 0
	}
	m.moveTo(item{kind: itemWorktree, id: m.rows[i].session, row: i})
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// handleDigestKey closes the digest on any key.
func (m Model) handleDigestKey(tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode, m.digest = m.digestBack, nil
	return m, nil
}

// digestBox is the popup that lists what happened in a context while you were
// elsewhere.
func (m Model) digestBox() []string {
	rows := []string{" " + dimStyle.Render("Held while you were away"), ""}
	for _, e := range m.digest {
		name := e.session
		if r, ok := m.rowFor(e.session); ok {
			name = r.project + "  " + m.notifyName(e.session)
		}
		d := stateDisplay(e.state)
		rows = append(rows, " "+d.style.Render(d.glyph)+" "+name+"  "+d.style.Render(d.word)+" "+dimStyle.Render(e.at.Format("15:04")))
	}
	return popupBox(" "+m.digestName+" ", rows, dimStyle.Render(" any key closes"), 44, m.width-2)
}

// The persisted part of the contexts: which one is active and where you were
// in each. Like the folds it is only drover's own state, and a missing or
// corrupt file means the first context.
type contextFile struct {
	Active string            `json:"active"`
	Last   map[string]string `json:"last,omitempty"`
}

func contextPath() string { return filepath.Join(status.Root(), "context.json") }

type ctxLoaded struct {
	active string
	last   map[string]string
}

func loadContext() tea.Cmd {
	return func() tea.Msg {
		var f contextFile
		if data, err := os.ReadFile(contextPath()); err == nil {
			_ = json.Unmarshal(data, &f)
		}
		return ctxLoaded{active: f.Active, last: f.Last}
	}
}

func saveContext(active string, last map[string]string) {
	data, err := json.MarshalIndent(contextFile{Active: active, Last: last}, "", "  ")
	if err != nil {
		return
	}
	path := contextPath()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
