package ui

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/tmux"
)

// companionModel is testModel on a private tmux server, with the selected
// worktree's directory real so companions can start there.
func companionModel(t *testing.T) Model {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	m := testModel()
	m.cfg.Socket = fmt.Sprintf("agt-companion-ui-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "_"))
	m.tmux = tmux.New(m.cfg.Socket)
	t.Cleanup(func() { _ = m.tmux.KillServer() })
	m.rows[0].wt.Path = t.TempDir()
	m.all = m.rows
	m.cfg.TerminalCmd, m.cfg.EditorCmd, m.cfg.LazygitCmd = "cat", "cat", "cat"
	return m
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, _ := m.handleKey(keyRunes(key))
	return next.(Model)
}

func TestCompanionKeysOpenFocusAndShowTheCompanion(t *testing.T) {
	m := companionModel(t)
	r := m.rows[0]
	for _, c := range []struct {
		key  string
		view paneView
		kind string
	}{{"T", viewTerminal, "term"}, {"o", viewEditor, "edit"}} {
		m = pressKey(t, m, c.key)
		if m.viewOf(r) != c.view || m.mode != modeFocus {
			t.Fatalf("%s: view %d mode %d, want view %d focus", c.key, m.viewOf(r), m.mode, c.view)
		}
		want := tmux.CompanionName(c.kind, r.session)
		if m.target(r) != want || !m.tmux.Has(want) || !m.targetRunning(r) {
			t.Errorf("%s: target %q running=%v", c.key, m.target(r), m.tmux.Has(want))
		}
		m.mode = modeList
	}
	// The agent was never started by any of this, and the companions do not
	// show up as worktrees.
	if m.tmux.Has(r.session) {
		t.Error("opening a companion must not start the agent")
	}
	if live, _ := m.tmux.Sessions(); len(live) != 0 {
		t.Errorf("companions leaked into Sessions(): %v", live)
	}

	// Back to the agent keeps the companions alive, as child rows.
	m = pressKey(t, m, "a")
	if m.viewOf(r) != viewAgent || m.target(r) != r.session {
		t.Errorf("a: view %d target %q", m.viewOf(r), m.target(r))
	}
	if !m.anyCompanion(r) {
		t.Error("companions must outlive switching back to the agent")
	}
	// Opening one that is running just shows it: same session, no new one.
	m = pressKey(t, m, "T")
	if !m.tmux.Has(tmux.CompanionName("term", r.session)) || m.mode != modeFocus {
		t.Error("reopening a running companion must show and focus it")
	}
}

// The shown pane is derived from the cursor, not remembered: on the worktree
// row it is the agent, on a child row that companion.
func TestViewFollowsTheCursor(t *testing.T) {
	m := companionModel(t)
	m = pressKey(t, m, "T")
	m.mode = modeList
	if m.viewOf(m.rows[0]) != viewTerminal {
		t.Fatal("the terminal row must show the terminal")
	}
	m.move(2) // off the child, past the "feature" group header: the next worktree
	if m.selSession != m.rows[1].session || m.viewOf(m.rows[1]) != viewAgent {
		t.Errorf("sel %q view %d: another worktree must show its agent", m.selSession, m.viewOf(m.rows[1]))
	}
	if m.viewOf(m.rows[0]) != viewAgent {
		t.Error("a worktree the cursor left shows nothing but its agent")
	}
	m.move(-2) // back onto the child row
	if m.viewOf(m.rows[0]) != viewTerminal {
		t.Error("the child row must show the terminal again")
	}
	m.move(-1) // the worktree row itself
	if m.viewOf(m.rows[0]) != viewAgent || m.target(m.rows[0]) != m.rows[0].session {
		t.Error("the worktree row is the agent")
	}
}

func TestChildRowKeys(t *testing.T) {
	m := companionModel(t)
	m = pressKey(t, m, "T")
	m = pressKey(t, m, "o") // editor row appears under the terminal row
	m.mode = modeList
	names := ids(visibleItems(m.items()))
	term, edit := tmux.CompanionName("term", m.rows[0].session), tmux.CompanionName("edit", m.rows[0].session)
	// the agent comes first among the children, then the terminal, then the editor
	if i := slices.Index(names, term); i < 0 || names[i+1] != edit || names[i-1] != agentItemID(m.rows[0].session) || names[i-2] != m.rows[0].session {
		t.Fatalf("children must follow their worktree in order (agent, terminal, editor): %v", names)
	}
	// h goes to the parent and never folds.
	foldsBefore := len(m.folds)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = next.(Model)
	if m.sub != viewAgent || m.selSession != m.rows[0].session || len(m.folds) != foldsBefore {
		t.Errorf("left on a child: sub %d sel %q folds %v", m.sub, m.selSession, m.folds)
	}
	// a from a child jumps to the parent too; enter on a child focuses it.
	m.move(3) // the agent row, the terminal row, then the editor row
	if m.sub != viewEditor {
		t.Fatalf("sub = %d, want the editor row", m.sub)
	}
	m = pressKey(t, m, "a")
	if m.sub != viewAgent {
		t.Error("a must jump to the parent row")
	}
	m.move(3)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.mode != modeFocus || m.target(m.rows[0]) != edit {
		t.Errorf("enter on the editor row: mode %d target %q", m.mode, m.target(m.rows[0]))
	}
	m.mode = modeList
	// x kills that companion only; the row goes and the cursor returns to the parent.
	m = pressKey(t, m, "x")
	if m.tmux.Has(edit) || !m.tmux.Has(term) || m.sub != viewAgent {
		t.Errorf("x on the editor row: edit alive %v term alive %v sub %d", m.tmux.Has(edit), m.tmux.Has(term), m.sub)
	}
	if slices.Contains(ids(visibleItems(m.items())), edit) {
		t.Error("the killed companion still has a row")
	}
}

// A companion that ends while the cursor is on it leaves focus mode at once,
// instead of letting keys flow to the agent behind it.
func TestVanishedCompanionRowReturnsCursorAndLeavesFocus(t *testing.T) {
	m := companionModel(t)
	m = pressKey(t, m, "T")
	name := tmux.CompanionName("term", m.rows[0].session)
	_ = m.tmux.Kill(name)
	next, _ := m.Update(sessionsMsg{live: nil, companions: nil})
	m = next.(Model)
	if m.sub != viewAgent || m.mode != modeList || !strings.Contains(m.notice, "terminal exited") {
		t.Errorf("sub %d mode %d notice %q", m.sub, m.mode, m.notice)
	}
}

func TestChildrenFollowFoldsAndFilters(t *testing.T) {
	m := foldModel(t)
	a := m.rows[1]
	m.companions[tmux.CompanionName("term", a.session)] = true
	child := companionID(a.session, viewTerminal)
	if !slices.Contains(ids(visibleItems(m.items())), child) {
		t.Fatal("a running terminal needs a child row")
	}
	m.folds = map[string]bool{"g:Acme/infra/feature": true}
	if slices.Contains(ids(visibleItems(m.items())), child) {
		t.Error("a fold must hide the children with their worktree")
	}
	m.folds = nil
	m.search = "zzz-nothing"
	m.applyFilter()
	if slices.Contains(ids(m.items()), child) {
		t.Error("a filtered-out worktree must take its children along")
	}
	m.search = "infra/a"
	m.applyFilter()
	if !slices.Contains(ids(m.items()), child) {
		t.Error("a matching worktree keeps its children")
	}
	// Header tallies count worktrees only.
	for _, it := range m.items() {
		if it.id == "p:Acme/infra" && it.sum.total != 1 { // only infra/a matches "infra/a"
			t.Errorf("tally %+v counts children", it.sum)
		}
	}
}

func TestMissingLazygitSaysSoInsteadOfOpeningADeadPane(t *testing.T) {
	m := companionModel(t)
	m.cfg.LazygitCmd = "lazygit-that-is-not-installed-xyz"
	r := m.rows[0]
	m = pressKey(t, m, "G")
	if !strings.Contains(m.status, "lazygit") || !strings.Contains(m.status, "lazygit_cmd") {
		t.Errorf("status = %q", m.status)
	}
	if m.viewOf(r) != viewAgent || m.mode != modeList || m.tmux.Has(tmux.CompanionName("git", r.session)) {
		t.Error("nothing may open when lazygit is missing")
	}
}

func TestCompanionCommandDefaults(t *testing.T) {
	m := testModel()
	t.Setenv("SHELL", "/bin/zsh")
	if cmd, _ := m.companionCmd(viewTerminal); cmd != "/bin/zsh" {
		t.Errorf("terminal default = %q, want $SHELL", cmd)
	}
	t.Setenv("SHELL", "")
	if cmd, _ := m.companionCmd(viewTerminal); cmd != "/bin/sh" {
		t.Errorf("terminal fallback = %q", cmd)
	}
	m.cfg.TerminalCmd = "fish -l"
	if cmd, _ := m.companionCmd(viewTerminal); cmd != "fish -l" {
		t.Errorf("configured terminal = %q", cmd)
	}

	// With hx off the PATH the editor falls back to $EDITOR, then vi.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("EDITOR", "nano")
	m = testModel()
	if cmd, _ := m.companionCmd(viewEditor); cmd != "nano ." {
		t.Errorf("editor via $EDITOR = %q", cmd)
	}
	t.Setenv("EDITOR", "")
	if cmd, _ := m.companionCmd(viewEditor); cmd != "vi ." {
		t.Errorf("editor fallback = %q", cmd)
	}
	m.cfg.EditorCmd = "hx --config /x"
	if cmd, _ := m.companionCmd(viewEditor); cmd != "hx --config /x" {
		t.Errorf("a configured editor_cmd is used as written, got %q", cmd)
	}
	if _, err := m.companionCmd(viewLazygit); err == nil {
		t.Error("lazygit is not on this PATH and must be reported")
	}
}

func TestLifecycleKillAndRemove(t *testing.T) {
	m := companionModel(t)
	r := m.rows[0]
	m.rows[0].running = false // only the companion runs
	m.all = m.rows
	m = pressKey(t, m, "T")
	m.mode = modeList
	m = pressKey(t, m, "o")
	m.mode = modeList

	// d is blocked while any companion runs, even with no agent.
	next, cmd := m.remove()
	m = next.(Model)
	if cmd != nil || !strings.Contains(m.status, "terminal, editor or lazygit") {
		t.Fatalf("d with companions running: cmd=%v status=%q", cmd != nil, m.status)
	}

	// x kills the shown companion only, and returns to the agent view.
	next, _ = m.kill()
	m = next.(Model)
	if m.tmux.Has(tmux.CompanionName("edit", r.session)) || !m.tmux.Has(tmux.CompanionName("term", r.session)) {
		t.Error("x must kill only the shown companion")
	}
	if m.viewOf(r) != viewAgent {
		t.Error("after x the pane shows the agent again")
	}

	// X kills everything and forgets the views.
	m = pressKey(t, m, "T")
	m.confirmAll = true
	next, _ = m.killAll()
	m = next.(Model)
	if m.tmux.Has(tmux.CompanionName("term", r.session)) || m.viewOf(r) != viewAgent || m.anyCompanion(r) {
		t.Error("X must kill the companions and reset the panes")
	}
}

func TestCompanionExitReturnsToAgentWithANote(t *testing.T) {
	m := companionModel(t)
	r := m.rows[0]
	m.cfg.TerminalCmd = "sleep 0.5" // exits soon, after the session is fully set up
	m = pressKey(t, m, "T")
	name := m.target(r)
	deadline := time.Now().Add(5 * time.Second)
	for m.tmux.Has(name) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// What the preview poll reports once the session is gone.
	next, _ := m.Update(previewMsg{loop: m.loop, session: name, err: errGone})
	m = next.(Model)
	if m.viewOf(r) != viewAgent || m.mode != modeList {
		t.Errorf("view %d mode %d: want the agent in list mode", m.viewOf(r), m.mode)
	}
	if !strings.Contains(m.notice, "terminal exited") {
		t.Errorf("notice = %q", m.notice)
	}
	// Enter now focuses the agent, not the dead terminal.
	if m.target(r) != r.session {
		t.Errorf("target = %q", m.target(r))
	}
}

func TestChildRowsAndBreadcrumbReplaceTheTabStrip(t *testing.T) {
	m := testModel()
	r := m.rows[0]
	m.companions[tmux.CompanionName("edit", r.session)] = true
	m.companions[tmux.CompanionName("term", r.session)] = true
	m.moveTo(visibleItems(m.items())[2]) // worktree row: after the project and group headers
	m.step(3)                            // the agent, then terminal, then editor
	if m.sub != viewEditor {
		t.Fatalf("sub = %d, want the editor row", m.sub)
	}
	for _, mode := range []mode{modeList, modeFocus} {
		m.mode = mode
		plain := ansi.Strip(m.render())
		for _, want := range []string{"┣━━ $ terminal ●", "╰━━ ✎ editor ●", "main › editor"} {
			if !strings.Contains(plain, want) {
				t.Errorf("mode %d: view lacks %q", mode, want)
			}
		}
		if strings.Contains(plain, "agent  terminal") {
			t.Errorf("mode %d: a tab strip is still drawn", mode)
		}
	}
	// The full view keeps its exact size with the companion shown.
	m.preview = preview{session: m.target(r), ok: true}
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d width %d, want %d", i, w, m.width)
		}
	}
}

func TestFocusKeysGoToTheCompanion(t *testing.T) {
	m := companionModel(t)
	r := m.rows[0]
	m.cfg.TerminalCmd = "cat"
	m = pressKey(t, m, "T")
	name := m.target(r)
	// A real program, so the echo comes back through the sender.
	got := make(chan echoMsg, 8)
	m.send.notify = func(msg tea.Msg) {
		if e, ok := msg.(echoMsg); ok {
			select {
			case got <- e:
			default:
			}
		}
	}
	next, _ := m.handleFocusKey(keyRunes("z"))
	m = next.(Model)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-got:
			if e.session != name {
				t.Fatalf("echo from %q, want the companion %q", e.session, name)
			}
			if strings.Contains(strings.Join(e.cap.Lines, "\n"), "z") {
				return
			}
		case <-deadline:
			t.Fatal("typed key never reached the companion")
		}
	}
}
