package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/gitinfo"
	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
)

// Reading git costs a process per worktree (about 10ms, almost all of it
// process start, so repository size hardly matters), which makes the number of
// reads the thing to ration:
//
//   - the selected worktree is read every gitEvery tick while its agent is
//     working, since files are changing; otherwise every gitIdleEvery ticks;
//   - a session that stops working is read at once, because that is exactly
//     when its files have just changed;
//   - selecting a worktree, or leaving focus mode, reads it too;
//   - every worktree is read once in a while, for external edits only;
//   - never two reads at once: a slow repository cannot pile up processes.
const (
	gitEvery     = 3 * time.Second
	gitIdleEvery = 5   // ticks: 15s
	gitAllEvery  = 100 // ticks: 5 minutes
)

type (
	gitTick struct{}
	gitMsg  struct{ res map[string]gitinfo.Status }
)

func scheduleGitTick() tea.Cmd {
	return tea.Tick(gitEvery, func(time.Time) tea.Msg { return gitTick{} })
}

// readGit reads the given worktrees one after another in a single goroutine,
// so a refresh never runs more than one git at a time.
func readGit(rows []row) tea.Cmd {
	paths := make(map[string]string, len(rows))
	for _, r := range rows {
		paths[r.session] = r.wt.Path
	}
	if len(paths) == 0 {
		return nil
	}
	return func() tea.Msg {
		res := make(map[string]gitinfo.Status, len(paths))
		for session, path := range paths {
			if st, err := gitinfo.Read(path); err == nil {
				res[session] = st
			}
		}
		return gitMsg{res: res}
	}
}

// requestGit starts a read of the given worktrees, or queues them when one
// is already running. It records when each was asked for, which gitDue uses.
func (m *Model) requestGit(rows []row) tea.Cmd {
	if len(rows) == 0 {
		return nil
	}
	if m.gitBusy || m.vis.hidden { // hidden: held until visible again (becomeVisible)
		for _, r := range rows {
			m.gitWant[r.session] = r
		}
		return nil
	}
	cmd := readGit(rows)
	if cmd == nil {
		return nil
	}
	m.gitBusy = true
	for _, r := range rows {
		m.gitAt[r.session] = m.gitTicks
	}
	return cmd
}

// readSelectedGit re-reads the selected worktree, for moments when its state
// has probably just changed: selecting it, or coming back from the agent.
func (m *Model) readSelectedGit() tea.Cmd {
	if r, ok := m.selected(); ok {
		return m.requestGit([]row{r})
	}
	return nil
}

// gitDue says whether the periodic read of a worktree is due now.
func (m Model) gitDue(r row) bool {
	at, seen := m.gitAt[r.session]
	if !seen {
		return true
	}
	every := gitIdleEvery
	if m.agents[r.session].State == status.Working {
		every = 1
	}
	return m.gitTicks-at >= every
}

func (m Model) handleGitTick() (Model, tea.Cmd) {
	m.gitTicks++
	var due []row
	if m.gitTicks%gitAllEvery == 0 {
		due = m.all
	} else if r, ok := m.selected(); ok && m.gitDue(r) {
		due = []row{r}
	}
	return m, tea.Batch(m.requestGit(due), scheduleGitTick())
}

// rowFor finds a worktree by session name.
// rowFor finds the worktree row of an agent session (or of a worktree's own
// session key).
func (m Model) rowFor(agentSession string) (row, bool) {
	base, _, _ := session.Parse(agentSession)
	for _, r := range m.all {
		if r.session == base {
			return r, true
		}
	}
	return row{}, false
}

// handleGit stores a finished read and starts any that were queued behind it.
func (m Model) handleGit(msg gitMsg) (Model, tea.Cmd) {
	next := make(map[string]gitinfo.Status, len(m.git)+len(msg.res))
	for k, v := range m.git {
		next[k] = v
	}
	for k, v := range msg.res {
		next[k] = v
	}
	m.git = next
	m.gitBusy = false
	if len(m.gitWant) == 0 {
		return m, nil
	}
	queued := make([]row, 0, len(m.gitWant))
	for _, r := range m.gitWant {
		queued = append(queued, r)
	}
	clear(m.gitWant)
	return m, m.requestGit(queued)
}
