package ui

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

// paneView is what the right pane shows: the worktree's agent, or one of the
// companion programs that run in the same directory. It is derived from the
// tree cursor (companion_tree.go), never stored per worktree. viewLazygit is
// never shown in the pane: lazygit is an ephemeral popup (popup.go) that only
// borrows the kind, label and command of this type.
type paneView int

const (
	viewAgent paneView = iota
	viewTerminal
	viewEditor
	viewLazygit
)

// companionViews are the companions that get a child row.
var companionViews = []paneView{viewTerminal, viewEditor}

func (v paneView) kind() string { // the tmux name prefix
	return [...]string{"", "term", "edit", "git"}[v]
}

func (v paneView) label() string {
	return [...]string{"agent", "terminal", "editor", "lazygit"}[v]
}

// viewOf is the view shown for a worktree: the child the cursor is on when it
// is on this worktree, else the agent.
func (m Model) viewOf(r row) paneView {
	if sel, ok := m.selected(); ok && sel.session == r.session {
		return m.sub
	}
	return viewAgent
}

// target is the tmux session the right pane captures and forwards keys to:
// the agent's session, or the companion the cursor's child row stands for.
func (m Model) target(r row) string {
	if v := m.viewOf(r); v != viewAgent {
		return tmux.CompanionName(v.kind(), r.session)
	}
	if a, ok := m.curAgent(r); ok {
		return a.session
	}
	return r.session
}

// targetRunning reports whether the shown session exists.
func (m Model) targetRunning(r row) bool {
	if m.viewOf(r) == viewAgent {
		a, ok := m.curAgent(r)
		return ok && a.running
	}
	return m.companions[m.target(r)]
}

// anyCompanion reports whether the worktree has any companion running,
// lazygit's popup included (it too runs inside the folder).
func (m Model) anyCompanion(r row) bool {
	for _, v := range append(slices.Clone(companionViews), viewLazygit) {
		if m.companions[tmux.CompanionName(v.kind(), r.session)] {
			return true
		}
	}
	return false
}

// setView moves the cursor between a worktree row (viewAgent) and one of its
// child rows, which must be running.
func (m *Model) setView(v paneView) {
	m.scroll = scrollState{} // scrollback belongs to the pane being shown
	m.sub = v
}

// forgetCompanions drops every companion, closes the popup and puts the
// cursor back on the worktree.
func (m *Model) forgetCompanions() {
	clear(m.companions)
	m.sub = viewAgent
	m.popup = popupState{}
	if m.mode == modePopup {
		m.mode = modeList
	}
}

// setCompanions replaces the live set. A lazygit session that is not the open
// popup is a leftover (an earlier drover that died with a popup open, or one
// that kept lazygit as a pane): the popup is ephemeral, so it is killed
// rather than listed.
func (m *Model) setCompanions(live []string) {
	next := make(map[string]bool, len(live))
	for _, s := range live {
		if kind, _, ok := tmux.ParseCompanion(s); ok && kind == viewLazygit.kind() && s != m.popup.session {
			_ = m.tmux.Kill(s)
			continue
		}
		next[s] = true
	}
	m.companions = next
	// A companion started just after the listing was taken is missing from
	// it; losing the row under the cursor would throw the cursor off it.
	if m.sub != viewAgent {
		if r, ok := m.selected(); ok && !m.companions[m.target(r)] && m.tmux.Has(m.target(r)) {
			m.companions[m.target(r)] = true
		}
	}
}

// errNotInstalled marks a companion whose program is missing, so the user
// gets a message instead of a pane that dies at once.
var errNotInstalled = errors.New("not installed")

// companionCmd resolves the command for a view: the config key, else a
// default found on this machine.
func (m Model) companionCmd(v paneView) (string, error) {
	var cmd string
	switch v {
	case viewTerminal:
		cmd = m.cfg.TerminalCmd
		if cmd == "" {
			cmd = os.Getenv("SHELL")
		}
		if cmd == "" {
			cmd = "/bin/sh"
		}
	case viewEditor:
		cmd = m.cfg.EditorCmd
		if cmd == "" {
			// "." makes the editor open the worktree (Helix: file picker).
			switch {
			case onPath("hx"):
				cmd = "hx ."
			case os.Getenv("EDITOR") != "":
				cmd = os.Getenv("EDITOR") + " ."
			default:
				cmd = "vi ."
			}
		}
	case viewLazygit:
		cmd = m.cfg.LazygitCmd
		if cmd == "" {
			cmd = "lazygit"
		}
		if !commandInstalled(cmd) {
			return "", errors.Join(errNotInstalled, errors.New("lazygit is not on PATH: install it or set lazygit_cmd in the config"))
		}
	}
	return cmd, nil
}

func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// commandInstalled checks the program a command line starts with. Command
// lines that begin with a variable assignment are shell, not a program name,
// so they are not second-guessed.
func commandInstalled(cmd string) bool {
	f := strings.Fields(cmd)
	if len(f) == 0 || strings.Contains(f[0], "=") {
		return true
	}
	return onPath(f[0])
}

// ensureCompanion starts the companion when it is not running.
func (m *Model) ensureCompanion(r row, v paneView) bool {
	name := tmux.CompanionName(v.kind(), r.session)
	if m.tmux.Has(name) { // not the cached set: the program may have just exited
		m.companions[name] = true
		return true
	}
	cmd, err := m.companionCmd(v)
	if err != nil {
		m.status = err.Error()
		return false
	}
	title := v.label() + " | " + r.session
	if err := m.tmux.SpawnCompanion(name, r.wt.Path, cmd, title); err != nil {
		m.status = err.Error()
		return false
	}
	m.companions[name] = true
	return true
}

// openView puts the cursor on the agent (the worktree row) or on a
// companion's child row. A companion is started if needed, which makes its
// row appear, and focused at once, since opening an editor to look at it is
// rare; opening one that is already running just selects and focuses it. The
// agent is shown without changing focus.
func (m Model) openView(v paneView) (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	if v == viewAgent {
		m.setView(v)
		return m, m.startPreview()
	}
	if !m.ensureCompanion(r, v) {
		return m, nil
	}
	m.setView(v)
	m.enterFocus()
	return m, m.startPreview()
}

// closeCompanion kills the shown companion; its row goes and the cursor
// returns to the worktree.
func (m Model) closeCompanion(r row) (tea.Model, tea.Cmd) {
	v := m.viewOf(r)
	name := tmux.CompanionName(v.kind(), r.session)
	if err := m.tmux.Kill(name); err != nil {
		m.status = err.Error()
	}
	delete(m.companions, name)
	m.setView(viewAgent)
	m.preview = preview{}
	cmd := m.startPreview() // before returning m, so the copy carries the new loop
	return m, tea.Batch(m.refresh(), cmd)
}

// companionGone handles a companion whose session ended (the program quit):
// its row disappears and the cursor falls back to the worktree with a note
// instead of a dead view. It reports false for anything that is not a
// companion.
func (m Model) companionGone(session string) (tea.Model, tea.Cmd, bool) {
	kind, _, ok := tmux.ParseCompanion(session)
	if !ok {
		return m, nil, false
	}
	delete(m.companions, session)
	if kind == viewLazygit.kind() {
		if session == m.popup.session { // lazygit quit by itself: the popup closes
			next, cmd := m.closePopup()
			return next, cmd, true
		}
		cmd := m.startPreview() // a stray one; nothing on screen depended on it
		return m, cmd, true
	}
	for _, v := range companionViews {
		if v.kind() == kind {
			m.notice = v.label() + " exited · back to the agent"
		}
	}
	m.reselect() // the child row is gone: the cursor falls back to its worktree
	m.scroll = scrollState{}
	// Focus would otherwise land on an agent the user was not typing to.
	m.mode = modeList
	m.preview = preview{}
	cmd := m.startPreview() // before returning m, so the copy carries the new loop
	return m, cmd, true
}

// companionsChanged reports the fall-back of a refresh: the cursor was on a
// companion row (was) and the listing no longer has it, so it is on the
// worktree now. Without leaving focus mode here, keys would silently start
// going to the agent.
func (m Model) companionsChanged(was paneView, wasSession string) (Model, tea.Cmd) {
	if was == viewAgent || m.sub != viewAgent || m.hdr != "" || m.selSession != wasSession {
		return m, nil
	}
	m.notice = was.label() + " exited · back to the agent"
	m.mode = modeList
	m.preview = preview{}
	cmd := m.startPreview()
	return m, cmd
}

// paneRule is the right pane's rule line: the focus hint while focused, else
// a plain rule.
func (m Model) paneRule(w int) string {
	if m.mode == modeFocus {
		return focusHint.Render("focused · ctrl+\\ / ctrl+q back · " + m.zoomKey() + " zoom")
	}
	return sepStyle.Render(strings.Repeat("─", w))
}

// notRunningHint is what an empty right pane says.
func (m Model) notRunningHint(r row) string {
	if v := m.viewOf(r); v != viewAgent {
		return "The " + v.label() + " is not running. Press enter to start it."
	}
	if a, ok := m.curAgent(r); ok && !a.running {
		return m.agentLabel(a) + " is stopped. Press enter (or v) to resume it, x to forget it."
	}
	return "No session here. Press enter to name and start `" + m.cfg.AgentCmd + "`."
}
