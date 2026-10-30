package ui

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/alias"
	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
)

// editText applies one key press to a single-line text field. It is shared
// by the new-worktree, rename and search prompts. Control characters never
// enter the text, so pasted newlines and escape sequences are dropped.
func editText(s string, msg tea.KeyPressMsg) string {
	switch msg.String() {
	case "backspace":
		if r := []rune(s); len(r) > 0 {
			return string(r[:len(r)-1])
		}
		return s
	case "ctrl+u":
		return ""
	}
	// Text is set only for a plain printable key (or a paste handed in as
	// one); Alt and Ctrl combinations have none and so never enter the field.
	if k := tea.Key(msg); k.Text != "" && k.Mod&^tea.ModShift == 0 {
		var b strings.Builder
		for _, r := range k.Text {
			if !unicode.IsControl(r) {
				b.WriteRune(r)
			}
		}
		return s + b.String()
	}
	return s
}

// pasteAsKey turns a paste into the key press a prompt's key handler already
// understands, so a pasted line is filtered exactly like typed text.
func pasteAsKey(content string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: content} }

// label is what a row is called in the list: its alias, else its branch.
func (m Model) label(r row) string {
	if a := m.aliases[r.session]; a != "" {
		return a
	}
	return r.wt.Label()
}

// filtering reports whether the visible list is narrower than the full one.
func (m Model) filtering() bool {
	return strings.TrimSpace(m.search) != "" || m.attention
}

// matches decides whether a row passes the search text and attention filter.
// The query is matched against everything the user can see about a row,
// including its state word, so "waiting" finds the agents waiting on you.
func (m Model) matches(r row, query string) bool {
	if m.attention {
		switch s, _ := m.worktreeState(r); s {
		case status.Waiting, status.Done, status.Error:
		default:
			return false
		}
		if !r.running {
			return false
		}
	}
	if query == "" {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{
		m.label(r), r.wt.Label(), r.org, r.project, r.session, m.display(r).word,
		strings.Join(m.agentNames(r), " "),
	}, " "))
	for _, term := range strings.Fields(query) { // every word must match
		if !strings.Contains(hay, term) {
			return false
		}
	}
	return true
}

// applyFilter rebuilds the visible rows from the full list and keeps the
// selection on the same session where it survives.
func (m *Model) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.search))
	rows := make([]row, 0, len(m.all))
	for _, r := range m.all {
		if m.matches(r, query) {
			rows = append(rows, r)
		}
	}
	m.rows = rows
	m.reselect()
}

// setRunning updates a session's running flag in both lists, so the list
// reacts at once instead of waiting for the next refresh.
// setRunning records that an agent session started or stopped, ahead of the
// next refresh, so the list does not lag behind what the user just did.
func (m *Model) setRunning(agentSession string, running bool) {
	base, _, _ := session.Parse(agentSession)
	for i := range m.all {
		if m.all[i].session != base {
			continue
		}
		list := slices.Clone(m.all[i].sess)
		found := false
		for j := range list {
			if list[j].session == agentSession {
				list[j].running, found = running, true
			}
		}
		switch {
		case found:
			m.all[i] = m.all[i].withAgents(list)
		case len(list) == 0 && agentSession == base:
			m.all[i].running = running // a row that has only the one old-style session
		case running:
			m.all[i] = m.all[i].withAgents(append(list, agent{session: agentSession, name: agentSession, running: true}))
		}
	}
	m.applyFilter()
}

func (m Model) openSearch() (tea.Model, tea.Cmd) {
	m.mode = modeSearch
	return m, nil
}

// handleSearchKey filters live as you type. Enter keeps the filter and
// returns to the list; esc clears it.
func (m Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	before := m.selSession
	switch msg.String() {
	case "esc", "ctrl+c":
		m.search, m.mode = "", modeList
	case "enter":
		m.mode = modeList
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	default:
		m.search = editText(m.search, msg)
	}
	m.applyFilter()
	if m.selSession != before {
		return m, m.startPreview()
	}
	return m, nil
}

// toggleAttention shows only agents that need a look: waiting, done or error.
func (m Model) toggleAttention() (tea.Model, tea.Cmd) {
	before := m.selSession
	m.attention = !m.attention
	m.applyFilter()
	if m.selSession != before {
		return m, m.startPreview()
	}
	return m, nil
}

// clearFilters drops the search text and the attention filter.
func (m Model) clearFilters() (tea.Model, tea.Cmd) {
	before := m.selSession
	m.search, m.attention = "", false
	m.applyFilter()
	if m.selSession != before {
		return m, m.startPreview()
	}
	return m, nil
}

func (m Model) openRename() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	if a, onChild := m.agentUnderCursor(r); onChild {
		m.mode, m.renameSession, m.renameAgent, m.input = modeRename, r.session, a.session, m.agentLabel(a)
		return m, nil
	}
	m.mode, m.renameSession, m.renameAgent, m.input = modeRename, r.session, "", m.aliases[r.session]
	return m, nil
}

// renameAgentTo gives the agent a new name: a recorded session in the
// registry, an old-style one (which has no record) as a label.
func (m Model) renameAgentTo(name string) (tea.Model, tea.Cmd) {
	r, ok := m.rowFor(m.renameAgent)
	a, found := agentByID(r, m.renameAgent)
	if !ok || !found {
		m.mode = modeList
		return m, nil
	}
	name = session.CleanName(name)
	switch {
	case name == "":
		m.status = "an agent needs a name"
		return m, nil
	case name != m.agentLabel(a) && slices.Contains(m.agentNames(r), name):
		m.status = fmt.Sprintf("%q is already used in this worktree", name)
		return m, nil
	}
	if a.id != "" {
		if _, err := session.Rename(session.Path(status.Root()), a.id, name); err != nil {
			m.status = err.Error()
			return m, nil
		}
		for i := range m.all {
			if m.all[i].session == r.session {
				list := slices.Clone(m.all[i].agentList())
				for j := range list {
					if list[j].session == a.session {
						list[j].name = name
					}
				}
				m.all[i] = m.all[i].withAgents(list)
			}
		}
	} else {
		if err := alias.Set(alias.Path(status.Root()), a.session, name); err != nil {
			m.status = err.Error()
			return m, nil
		}
		next := maps.Clone(m.aliases)
		if next == nil {
			next = map[string]string{}
		}
		next[a.session] = name
		m.aliases = next
	}
	m.mode, m.renameAgent = modeList, ""
	m.notice = "renamed to " + name
	m.applyFilter()
	return m, nil
}

// handleRenameKey edits the display name. An empty name clears the alias.
func (m Model) handleRenameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode, m.renameAgent = modeList, ""
	case "enter":
		if m.renameAgent != "" {
			return m.renameAgentTo(m.input)
		}
		name := strings.TrimSpace(m.input)
		if err := alias.Set(alias.Path(status.Root()), m.renameSession, name); err != nil {
			m.status = err.Error()
			return m, nil
		}
		next := make(map[string]string, len(m.aliases)+1)
		for k, v := range m.aliases {
			next[k] = v
		}
		if name == "" {
			delete(next, m.renameSession)
			m.notice = "alias cleared"
		} else {
			next[m.renameSession] = name
			m.notice = "renamed to " + name
		}
		m.aliases, m.mode = next, modeList
		m.applyFilter()
	default:
		m.input = editText(m.input, msg)
	}
	return m, nil
}

// renameLines is the rename form shown in the right pane.
func (m Model) renameLines() []string {
	branch := ""
	for _, r := range m.all {
		if r.session == m.renameSession {
			branch = r.wt.Label() + "  " + filepath.ToSlash(r.wt.Path)
		}
	}
	if m.renameAgent != "" {
		return []string{
			"",
			" " + titleStyle.Render("Rename agent"),
			"",
			" name    " + m.input + focusHint.Render("█"),
			" where   " + dimStyle.Render(branch),
			"",
			" " + dimStyle.Render("The name is only a label here; it is also shown in Claude's"),
			" " + dimStyle.Render("session list the next time the session starts."),
		}
	}
	return []string{
		"",
		" " + titleStyle.Render("Rename"),
		"",
		" name    " + m.input + focusHint.Render("█"),
		" branch  " + dimStyle.Render(branch),
		"",
		" " + dimStyle.Render("This is a display label only. The branch, the folder and the"),
		" " + dimStyle.Render("tmux session keep their names. An empty name clears it."),
	}
}
