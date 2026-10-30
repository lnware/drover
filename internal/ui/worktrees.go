package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/claude"
	"github.com/lnware/drover/internal/hooks"
	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

// openNew starts the new-worktree prompt for the selected row's project.
func (m Model) openNew() (tea.Model, tea.Cmd) {
	root := m.selectedRoot()
	if root == "" && len(m.cfg.Projects) == 1 {
		root = m.cfg.Projects[0]
	}
	if root == "" {
		m.status = "no project to add a worktree to"
		return m, nil
	}
	gitDir, err := worktree.GitDir(root)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	base, err := worktree.BaseRef(gitDir)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.mode, m.input, m.newRoot, m.newBase, m.busy = modeNew, "", root, base, false
	return m, nil
}

// handleNewKey edits the branch name; the prompt stays open on errors so a
// typo does not cost the whole name.
func (m Model) handleNewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.status = ""
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "enter":
		branch := strings.TrimSpace(m.input)
		if branch == "" {
			m.status = "type a branch name, like feature/my-change"
			return m, nil
		}
		m.busy = true
		root := m.newRoot
		return m, func() tea.Msg {
			wt, err := worktree.Add(root, branch)
			return worktreeCreated{root: root, wt: wt, err: err}
		}
	default:
		m.input = editText(m.input, msg)
	}
	return m, nil
}

func (m Model) handleCreated(msg worktreeCreated) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}
	m.mode = modeList
	m.notice = "created " + msg.wt.Branch + " · enter starts an agent there"
	// Select the new row as soon as the refresh brings it in.
	m.selSession = tmux.SessionName(m.projectName(msg.root), sessionKey(msg.root, msg.wt))
	m.hdr = ""
	m.revealNew(msg.root, msg.wt)
	return m, m.refresh()
}

// remove deletes the selected worktree after a second press. A running
// session blocks it, and git itself refuses dirty trees, so the worst a
// stray double-press does is remove a clean checkout whose branch survives.
func (m Model) remove() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	if r.running {
		m.status = "stop the session first (x), then remove the worktree"
		return m, nil
	}
	if m.anyCompanion(r) {
		m.status = "a terminal, editor or lazygit is still running here: show it and press x, then remove the worktree"
		return m, nil
	}
	if m.confirmRemove != r.session {
		m.confirmRemove = r.session
		m.status = "press d again to remove " + r.wt.Path + " (the branch is kept)"
		return m, nil
	}
	m.confirmRemove, m.busy = "", true
	root, path, key, agents := r.root, r.wt.Path, r.session, r.agentList()
	return m, func() tea.Msg {
		err := worktree.Remove(root, path)
		if err == nil {
			// The worktree's stopped sessions go with it: they have nowhere to resume.
			_ = session.RemoveWorktree(session.Path(status.Root()), key)
			for _, a := range agents {
				_ = status.Remove(status.Dir(), a.session)
			}
		}
		return worktreeRemoved{err: err}
	}
}

// resume is v: a stopped agent comes back with its own conversation; in a
// worktree with no session it continues the folder's last conversation as a
// new session.
func (m Model) resume() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	a, has := m.curAgent(r)
	if has && a.running {
		m.status = "already running; press enter to focus it"
		return m, nil
	}
	if has {
		return m.revive(r, a)
	}
	if m.resumeArgs() == "" {
		m.status = "set resume_args in the config to resume this agent"
		return m, nil
	}
	if !claude.HasConversation(r.wt.Path) {
		m.status = "no previous conversation in " + r.wt.Path + " (enter starts a new one)"
		return m, nil
	}
	return m.openNameAgent(r, true)
}

func (m Model) resumeArgs() string {
	if m.cfg.ResumeArgs != "" {
		return m.cfg.ResumeArgs
	}
	if hooks.IsClaude(m.cfg.AgentCmd) {
		return "--continue"
	}
	return ""
}

func (m Model) withResume(agent string) string {
	if args := m.resumeArgs(); args != "" {
		return agent + " " + args
	}
	return agent
}

// groupPrefixes lists the branch prefixes in use in a project, as hints for
// the new-worktree prompt.
func (m Model) groupPrefixes(root string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range m.all {
		if r.root != root || r.wt.Group == worktree.BaseGroup || seen[r.wt.Group] {
			continue
		}
		seen[r.wt.Group] = true
		out = append(out, r.wt.Group+"/")
	}
	return out
}
