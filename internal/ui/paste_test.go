package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestPastedTextReachesTheAgentInFocusMode(t *testing.T) {
	m := scrollModel(t, 3) // a cat agent is running in the first row
	m.mode = modeFocus
	next, _ := m.Update(tea.PasteMsg{Content: "hello\nworld"})
	m = next.(Model)
	s := m.rows[0].session
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := m.tmux.Capture(s); err == nil {
			if joined := strings.Join(c.Lines, "\n"); strings.Contains(joined, "hello") && strings.Contains(joined, "world") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, _ := m.tmux.Capture(s)
	t.Fatalf("the paste never reached the agent; its pane shows:\n%s", strings.Join(c.Lines, "\n"))
}

func TestPasteIntoAPromptDropsControlCharacters(t *testing.T) {
	for _, c := range []struct {
		name string
		mode mode
		text func(Model) string
	}{
		{"search", modeSearch, func(m Model) string { return m.search }},
		{"new worktree", modeNew, func(m Model) string { return m.input }},
		{"rename", modeRename, func(m Model) string { return m.input }},
		{"rename branch", modeMoveBranch, func(m Model) string { return m.input }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testModel()
			m.mode = c.mode
			next, _ := m.Update(tea.PasteMsg{Content: "ab\n\x1b[31mc"})
			if got := c.text(next.(Model)); got != "ab[31mc" {
				t.Errorf("field = %q, want the paste without its newline and escape byte", got)
			}
		})
	}
}

func TestPasteInTheListDoesNothing(t *testing.T) {
	m := testModel()
	next, cmd := m.Update(tea.PasteMsg{Content: "x\ny"})
	got := next.(Model)
	if cmd != nil || got.search != "" || got.input != "" || got.mode != modeList {
		t.Errorf("a paste in list mode must be ignored: mode %d search %q input %q", got.mode, got.search, got.input)
	}
}

func TestViewDeclaresTheTerminalModes(t *testing.T) {
	m := testModel()
	m.cfg.Mouse = true
	if v := m.View(); !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("mouse on: alt screen %v, mouse mode %v", v.AltScreen, v.MouseMode)
	}
	m.cfg.Mouse = false
	if v := m.View(); !v.AltScreen || v.MouseMode != tea.MouseModeNone {
		t.Errorf("mouse off: alt screen %v, mouse mode %v (the terminal must keep its own selection)", v.AltScreen, v.MouseMode)
	}
	if v := m.View(); v.Content != m.render() {
		t.Error("View must carry exactly the rendered screen")
	}
}

func TestMouseMotionAndReleaseAreIgnored(t *testing.T) {
	m := testModel()
	before := m.cursor
	for _, msg := range []tea.Msg{
		tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 4, Button: tea.MouseLeft}),
		tea.MouseReleaseMsg(tea.Mouse{X: 3, Y: 4, Button: tea.MouseLeft}),
		tea.MouseClickMsg(tea.Mouse{X: 3, Y: 4, Button: tea.MouseRight}),
	} {
		if got := step(m, msg); got.cursor != before {
			t.Errorf("%T moved the selection", msg)
		}
	}
}

func TestPasteInFocusModeLeavesScrollback(t *testing.T) {
	m := scrollModel(t, 3)
	m.mode = modeFocus
	key, _ := m.scrollKey()
	m.scroll = scrollState{key: key, off: 5}
	if !m.scrolled() {
		t.Fatal("setup: the model should be scrolled back")
	}
	next, _ := m.Update(tea.PasteMsg{Content: "x"})
	if next.(Model).scrolled() {
		t.Error("a paste must leave scrollback first, like any other key, so it is never typed into a view that is not live")
	}
}
