package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// typed presses a series of keys, and keeps the model.
func typed(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	return m
}

func text(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

func commandModel(t *testing.T) Model {
	m := contextModel(t)
	m.cfg.ContextKey = "" // the command line is the way
	return m
}

func TestCommandLineSwitchesContextAndGoesBack(t *testing.T) {
	m := commandModel(t)
	m = typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"})
	if m.mode != modeCommand {
		t.Fatalf(": opens the command line: mode %d", m.mode)
	}
	m = typed(t, m, text("ctx")...)
	sugg := m.cmdSuggestions()
	if len(sugg) != 2 || sugg[0].text != "ctx Personal" || sugg[1].text != "ctx Acme" || sugg[1].help != "current" {
		t.Fatalf("ctx lists the other contexts first, then the current one: %+v", sugg)
	}
	m = typed(t, m, enter()) // the first suggestion
	if m.mode != modeList || m.ctxActive != "Personal" || m.rows[0].project != "garden-app" {
		t.Fatalf("enter runs the chosen suggestion: mode %d ctx %q", m.mode, m.ctxActive)
	}

	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("ctx -"), enter())...)...)
	if m.ctxActive != "Acme" {
		t.Errorf("ctx - goes back: %q", m.ctxActive)
	}
	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("ctx per"), enter())...)...)
	if m.ctxActive != "Personal" {
		t.Errorf("a prefix of the name is enough: %q", m.ctxActive)
	}
	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("ctx nope"), enter())...)...)
	if m.ctxActive != "Personal" || m.mode != modeCommand || !strings.Contains(ansi.Strip(m.commandLine()), "") {
		t.Errorf("an unknown context changes nothing and stays on the line: %q mode %d", m.ctxActive, m.mode)
	}
}

func TestCommandTabCompletesAndHistoryRecalls(t *testing.T) {
	m := commandModel(t)
	m = typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"})
	m = typed(t, m, text("c")...)
	m = typed(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.cmdInput != "ctx Personal" {
		t.Fatalf("tab completes to the chosen suggestion: %q", m.cmdInput)
	}
	m = typed(t, m, enter())
	m = typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"}, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cmdInput != "ctx Personal" {
		t.Errorf("up on an empty line recalls the last command: %q", m.cmdInput)
	}
	m = typed(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cmdInput != "" {
		t.Errorf("down returns to an empty line: %q", m.cmdInput)
	}
	m = typed(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeList {
		t.Errorf("esc closes the line: mode %d", m.mode)
	}
}

func TestCommandWorktreeJumpAndQuit(t *testing.T) {
	m := commandModel(t)
	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("wt web"), enter())...)...)
	if m.selSession != "web-site/main" || m.mode != modeList {
		t.Fatalf("wt jumps to a worktree of this context: sel %q mode %d", m.selSession, m.mode)
	}
	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("wt garden"), enter())...)...)
	if m.selSession != "web-site/main" || m.mode != modeCommand {
		t.Errorf("a worktree of another context is not offered: sel %q mode %d", m.selSession, m.mode)
	}
	m = typed(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"}, tea.KeyPressMsg{Code: 'q', Text: "q"})
	_, cmd := m.Update(enter())
	if cmd == nil {
		t.Fatal("q must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q quits: %T", cmd())
	}
	m = typed(t, m, text("zz")...) // "qzz" is not a command
	n, _ := m.Update(enter())
	if g := n.(Model); g.status == "" || g.mode != modeCommand {
		t.Errorf("an unknown command says so and keeps the line: status %q mode %d", g.status, g.mode)
	}
}

func TestCommandLineIsDrawnAboveTheFooterAndKeepsTheFrameWidth(t *testing.T) {
	m := commandModel(t)
	m = typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"})
	m = typed(t, m, text("ctx")...)
	frame := m.render()
	plain := ansi.Strip(frame)
	lines := strings.Split(plain, "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, " :ctx") || !strings.Contains(last, "tab complete") {
		t.Errorf("the footer is the command line: %q", last)
	}
	for _, want := range []string{"Commands", "▸ ctx Personal", "ctx Acme", "current"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the suggestions lack %q:\n%s", want, plain)
		}
	}
	for i, l := range strings.Split(frame, "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.width)
		}
	}
}

func TestColonIsNotACommandInFocusModeOrOtherPrompts(t *testing.T) {
	m := commandModel(t)
	m.mode = modeFocus
	if got := typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"}); got.mode == modeCommand {
		t.Error(": in focus mode belongs to the pane (Helix uses it)")
	}
	m.mode = modeSearch
	if got := typed(t, m, tea.KeyPressMsg{Code: ':', Text: ":"}); got.mode != modeSearch || got.search != ":" {
		t.Errorf(": typed into the search prompt: mode %d search %q", got.mode, got.search)
	}
}
