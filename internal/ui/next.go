package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/status"
)

// "Next needing me" walks the agents of the active context that want you
// (waiting for an answer, finished or failed, or stopped with files to review),
// in the order of the list, wrapping round. It is how you avoid scanning the list for what to look at.

// needTarget is an agent that wants you.
type needTarget struct {
	base    string // the worktree's session
	session string // the agent's tmux session
}

// needsYou reports whether a running agent wants you: it waits, has finished or
// failed, or has stopped working and left files that are not staged yet, which
// is work waiting for your review.
func (m Model) needsYou(r row, a agent) bool {
	if !a.running {
		return false
	}
	s, ok := m.agentState(a)
	if !ok {
		return false
	}
	if s == status.Waiting || s == status.Done || s == status.Error {
		return true
	}
	return s != status.Working && m.git[r.session].ToReview() > 0
}

// needing lists the agents of the active context that want you.
func (m Model) needing() []needTarget {
	var out []needTarget
	for _, r := range m.contextRows() {
		for _, a := range r.agentList() {
			if m.needsYou(r, a) {
				out = append(out, needTarget{base: r.session, session: a.session})
			}
		}
	}
	return out
}

// currentNeed is the agent the cursor is on, as a position for stepping from.
func (m Model) currentNeed() (needTarget, bool) {
	r, ok := m.selected()
	if !ok || m.sub != viewAgent {
		return needTarget{}, false
	}
	a, ok := m.curAgent(r)
	return needTarget{base: r.session, session: a.session}, ok
}

// nextNeeding moves the cursor to the next (step 1) or previous (step -1)
// agent that wants you. Focus stays on when it was on: such an agent runs.
func (m Model) nextNeeding(step int) (tea.Model, tea.Cmd) {
	list := m.needing()
	if len(list) == 0 {
		m.notice = "nothing needs you here"
		return m, nil
	}
	i := -1
	if cur, ok := m.currentNeed(); ok {
		i = slices.Index(list, cur)
	}
	var at int
	switch {
	case i >= 0:
		at = (i + step + len(list)) % len(list)
	case step > 0:
		at = m.firstAfterCursor(list)
	default:
		at = (m.firstAfterCursor(list) - 1 + len(list)) % len(list)
	}
	t := list[at]
	next, cmd := m.goToWorktree(t.base)
	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	if r, found := nm.rowFor(t.session); found && len(r.agentList()) > 1 {
		nm.selectAgent(t.base, t.session)
	}
	nm.notice = fmt.Sprintf("%s %s", nm.notifyName(t.session), nm.agentDisplay(agent{session: t.session, running: true}).word)
	return nm, cmd
}

// firstAfterCursor is the position in list of the first agent below the
// cursor, so that stepping from a place that is not itself in the list lands
// on its neighbour.
func (m Model) firstAfterCursor(list []needTarget) int {
	pos := -1
	if r, ok := m.selected(); ok {
		pos = slices.IndexFunc(m.contextRows(), func(o row) bool { return o.session == r.session })
	}
	rows := m.contextRows()
	for i, t := range list {
		if idx := slices.IndexFunc(rows, func(o row) bool { return o.session == t.base }); idx > pos {
			return i
		}
	}
	return 0
}

// nextKey is the configured extra key, empty when none: tab and shift+tab
// work in the list without it, and it is how the walk works in focus mode,
// where every other key goes to the pane.
func (m Model) nextKey() string { return m.cfg.NextKey }

func (m Model) nextStep(key string) (int, bool) {
	if k := m.nextKey(); k != "" {
		switch key {
		case k:
			return 1, true
		case "shift+" + k:
			return -1, true
		}
	}
	return 0, false
}
