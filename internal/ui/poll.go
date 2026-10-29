package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

// Polling costs processes: every capture is a `tmux` invocation and every
// worktree refresh is a `git` per project. So the intervals back off while
// nothing changes and snap back the moment something does.
//
// Typing does not depend on them: input echoes are captured and pushed by the
// sender (sender.go), so the poll only has to catch what the agent does on
// its own, like a streaming answer or a spinner.
var (
	focusBackoff = []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 250 * time.Millisecond, 400 * time.Millisecond}
	listBackoff  = []time.Duration{250 * time.Millisecond, 250 * time.Millisecond, 400 * time.Millisecond, 600 * time.Millisecond, time.Second}
)

const (
	// focusPoll and listPoll are the fastest intervals, used while the pane changes.
	focusPoll = 100 * time.Millisecond
	listPoll  = 250 * time.Millisecond
	// worktreeEvery counts refresh ticks between full worktree scans: the
	// session list is cheap (one tmux call), a scan is one git per project.
	worktreeEvery = 5
)

// pollInterval is the wait before the next preview capture, after idle
// consecutive captures that showed no change.
func pollInterval(focus bool, idle int) time.Duration {
	steps := listBackoff
	if focus {
		steps = focusBackoff
	}
	return steps[min(max(idle, 0), len(steps)-1)]
}

// sameCapture reports whether two snapshots would draw identically.
func sameCapture(a, b tmux.Capture) bool {
	return a.CursorX == b.CursorX && a.CursorY == b.CursorY &&
		a.CursorVisible == b.CursorVisible && slices.Equal(a.Lines, b.Lines)
}

// sessionsMsg carries the live session names, read without touching git.
type sessionsMsg struct{ live, companions []string }

// refreshSessions updates only which worktrees have a running session.
func (m Model) refreshSessions() tea.Cmd {
	driver := m.tmux
	return func() tea.Msg {
		live, companions, err := driver.ListSessions()
		if err != nil {
			return nil // keep what is shown; the next full refresh reports it
		}
		return sessionsMsg{live: live, companions: companions}
	}
}

func (m Model) handleSessions(msg sessionsMsg) Model {
	all := make([]row, len(m.all))
	copy(all, m.all)
	for i := range all {
		all[i].running = slices.Contains(msg.live, all[i].session)
	}
	m.all = all
	m.setCompanions(msg.companions)
	m.applyFilter()
	return m
}
