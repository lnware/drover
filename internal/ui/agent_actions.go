package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/claude"
	"github.com/lnware/drover/internal/hooks"
	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
)

// How a session is launched: a new conversation, the exact one it already has,
// or the last one in the directory (a session started without a record).
type launch int

const (
	launchNew launch = iota
	launchResume
	launchContinue
)

// agentCommand is the command line that runs the agent for a. For Claude it
// names the session (--name) and pins its conversation (--session-id when it
// starts, --resume <id> when it comes back), so a session resumes exactly the
// conversation it held. Other agents get only the status hooks and, on a
// resume, the configured resume_args.
func (m Model) agentCommand(a agent, how launch) (string, error) {
	cmd, err := hooks.Prepare(m.cfg.AgentCmd, filepath.Join(status.Root(), "claude-settings.json"))
	if !hooks.IsClaude(m.cfg.AgentCmd) {
		if how != launchNew {
			cmd = m.withResume(cmd)
		}
		return cmd, err
	}
	switch {
	case how == launchNew && a.claude != "":
		cmd += " --session-id " + a.claude
	case how == launchResume && a.claude != "":
		cmd += " --resume " + a.claude
	case how != launchNew:
		cmd = m.withResume(cmd)
	}
	if a.name != "" {
		cmd += " --name " + hooks.Quote(a.name)
	}
	return cmd, err
}

// agentNames are the names in use in a worktree, for the default and for
// refusing a duplicate.
func (m Model) agentNames(r row) []string {
	var names []string
	for _, a := range r.agentList() {
		names = append(names, m.agentLabel(a))
	}
	return names
}

// agentLabel is the name shown for an agent: its name, or the label given to
// an old-style session that has no record to hold one.
func (m Model) agentLabel(a agent) string {
	if a.id == "" {
		if n := m.aliases[a.session]; n != "" {
			return n
		}
	}
	return a.name
}

// suggestedName is what an empty name becomes for a new agent in r.
func (m Model) suggestedName(r row) string {
	return session.DefaultName(r.wt.Branch, m.agentNames(r))
}

// openNameAgent asks for the name of a new agent session in r. continuing
// makes the new session continue the directory's last conversation instead of
// starting a fresh one.
func (m Model) openNameAgent(r row, continuing bool) (tea.Model, tea.Cmd) {
	m.mode, m.nameFor, m.nameContinuing, m.input = modeNameAgent, r.session, continuing, ""
	return m, nil
}

// newAgent is N: another agent in the selected worktree.
func (m Model) newAgent() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.status = "select a worktree first"
		return m, nil
	}
	return m.openNameAgent(r, false)
}

// handleNameAgentKey edits the name; enter starts the agent, and an empty name
// becomes the suggestion.
func (m Model) handleNameAgentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "enter":
		r, ok := m.rowFor(m.nameFor)
		if !ok {
			m.mode = modeList
			return m, nil
		}
		name := session.CleanName(m.input)
		if name == "" {
			name = m.suggestedName(r)
		} else if slices.Contains(m.agentNames(r), name) {
			m.status = fmt.Sprintf("%q is already used in this worktree", name)
			return m, nil
		}
		m.mode = modeList
		return m.startNewAgent(r, name, m.nameContinuing)
	default:
		m.input = editText(m.input, msg)
	}
	return m, nil
}

// nameAgentLines is the form shown in the right pane.
func (m Model) nameAgentLines() []string {
	where, suggestion := "", ""
	if r, ok := m.rowFor(m.nameFor); ok {
		where = r.wt.Label() + "  " + filepath.ToSlash(r.wt.Path)
		suggestion = m.suggestedName(r)
	}
	what := "starts a new conversation"
	if m.nameContinuing {
		what = "continues the last conversation in this folder"
	}
	return []string{
		"",
		" " + titleStyle.Render("New agent session"),
		"",
		" name   " + m.input + focusHint.Render("█"),
		" where  " + dimStyle.Render(where),
		"",
		" " + dimStyle.Render("Empty uses "+suggestion+". The session "+what+"."),
	}
}

// spawnAgent starts a's tmux session and records it as running.
func (m *Model) spawnAgent(r row, a agent, how launch) bool {
	// A file left by an earlier run must not show as this session's state.
	_ = status.Remove(status.Dir(), a.session)
	// Claude only resumes a conversation it stored: one closed before its first
	// message starts again under the same id instead.
	if how == launchResume && a.claude != "" && !claude.HasSession(r.wt.Path, a.claude) {
		how = launchNew
	}
	cmd, err := m.agentCommand(a, how)
	if err != nil {
		// The agent still starts; only the live status is lost.
		m.status = "status hooks unavailable: " + err.Error()
	}
	if err := m.tmux.Spawn(a.session, r.wt.Path, cmd); err != nil {
		m.status = err.Error()
		return false
	}
	m.sized = size{}
	return true
}

// startNewAgent records a new session of r, starts it and focuses it.
func (m Model) startNewAgent(r row, name string, continuing bool) (tea.Model, tea.Cmd) {
	a := agent{id: session.NewID(), name: name, created: time.Now()}
	a.session = session.TmuxName(r.session, a.id)
	if hooks.IsClaude(m.cfg.AgentCmd) && !continuing {
		a.claude = session.NewUUID()
	}
	rec := session.Session{ID: a.id, Worktree: r.session, Name: name, Claude: a.claude, Created: a.created}
	path := session.Path(status.Root())
	if err := session.Add(path, rec); err != nil {
		m.status = "could not record the session: " + err.Error()
		return m, nil
	}
	how := launchNew
	if continuing {
		how = launchContinue
	}
	if !m.spawnAgent(r, a, how) {
		_ = session.Remove(path, a.id)
		return m, nil
	}
	a.running = true
	m.addAgent(r.session, a)
	m.selectAgent(r.session, a.session)
	m.enterFocus()
	return m, m.startPreview()
}

// revive brings a stopped session back, resuming its conversation, and
// focuses it.
func (m Model) revive(r row, a agent) (tea.Model, tea.Cmd) {
	if !m.reviveSession(r, a) {
		return m, nil
	}
	m.selectAgent(r.session, a.session)
	m.enterFocus()
	return m, m.startPreview()
}

// reviveSession starts a stopped session again under its own name; it does
// nothing for one that is running.
func (m *Model) reviveSession(r row, a agent) bool {
	if a.running || m.tmux.Has(a.session) {
		return true
	}
	if !m.spawnAgent(r, a, launchResume) {
		return false
	}
	m.setRunning(a.session, true)
	return true
}

// addAgent puts a new agent on its worktree's row.
func (m *Model) addAgent(base string, a agent) {
	for i := range m.all {
		if m.all[i].session == base {
			m.all[i] = m.all[i].withAgents(append(slices.Clone(m.all[i].agentList()), a))
		}
	}
	m.applyFilter()
}

// selectAgent puts the cursor on an agent's child row.
func (m *Model) selectAgent(base, agentSession string) {
	if i := slices.IndexFunc(m.rows, func(r row) bool { return r.session == base }); i >= 0 {
		m.moveTo(item{kind: itemAgent, id: agentItemID(agentSession), agent: agentSession, row: i})
	}
}

// forgetAgent drops a stopped session's record and its row.
func (m Model) forgetAgent(r row, a agent) (tea.Model, tea.Cmd) {
	if a.id != "" {
		if err := session.Remove(session.Path(status.Root()), a.id); err != nil {
			m.status = err.Error()
			return m, nil
		}
	}
	_ = status.Remove(status.Dir(), a.session)
	for i := range m.all {
		if m.all[i].session == r.session {
			list := slices.DeleteFunc(slices.Clone(m.all[i].agentList()), func(o agent) bool { return o.session == a.session })
			m.all[i] = m.all[i].withAgents(list)
		}
	}
	m.applyFilter()
	m.reselect() // the row is gone: the cursor falls back to its worktree
	m.notice = "forgot " + m.agentLabel(a) + " (its conversation stays in Claude's history)"
	return m, tea.Batch(m.refresh(), m.startPreview())
}

// agentUnderCursor is the agent the cursor is on when it is on an agent
// child row (not the worktree row, which stands for the primary agent).
func (m Model) agentUnderCursor(r row) (agent, bool) {
	if m.subAgent == "" || m.sub != viewAgent {
		return agent{}, false
	}
	return m.curAgent(r)
}

// notifyName is how a status notification names an agent: the worktree and
// the agent, not the tmux session.
func (m Model) notifyName(agentSession string) string {
	r, ok := m.rowFor(agentSession)
	if !ok {
		return agentSession
	}
	label := m.label(r)
	if a, ok := agentByID(r, agentSession); ok && len(r.agentList()) > 1 {
		label += " · " + m.agentLabel(a)
	}
	return label
}

// hasConversation reports whether Claude has a conversation stored for a
// worktree's folder, which `v` can continue.
func hasConversation(r row) bool { return claude.HasConversation(r.wt.Path) }

// looking reports whether the user is focused on that agent's pane right now,
// so a notification about it would only tell them what they can see.
func (m Model) looking(agentSession string) bool {
	if m.mode != modeFocus || m.sub != viewAgent {
		return false
	}
	r, ok := m.selected()
	if !ok {
		return false
	}
	a, ok := m.curAgent(r)
	return ok && a.session == agentSession
}
