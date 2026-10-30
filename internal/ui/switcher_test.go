package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/tmux"
)

func TestSwitcherKeys(t *testing.T) {
	m := testModel()
	for _, c := range []struct {
		key        string
		step, jump int
		ok         bool
	}{
		{"f12", 1, -1, true},
		{"ctrl+tab", 1, -1, true},
		{"ctrl+shift+tab", -1, -1, true},
		{"shift+f12", -1, -1, true},
		{"f1", 0, 0, true},
		{"f9", 0, 8, true},
		{"ctrl+3", 0, 2, true},
		{"f10", 0, -1, false}, // outside 1..9: left for the zoom key and the pane
		{"ctrl+space", 0, -1, false},
		{"ctrl+0", 0, -1, false},
		{"tab", 0, -1, false},
	} {
		step, jump, ok := m.switchKeyPressed(c.key)
		if ok != c.ok || (ok && (step != c.step || jump != c.jump)) {
			t.Errorf("%s: got step %d jump %d ok %v, want %d %d %v", c.key, step, jump, ok, c.step, c.jump, c.ok)
		}
	}
}

// a worktree with two agents and then the terminal and the editor: F12 opens
// the overlay on the next stop, F12 again moves on, enter goes; a number or an
// F-key goes straight to the nth stop and starts what is missing.
func TestSwitcherOverlayAndJumps(t *testing.T) {
	m, _ := agentModel(t)
	m.cfg.TerminalCmd, m.cfg.EditorCmd = "cat", "cat"
	for _, name := range []string{"one", "two"} {
		m.mode = modeList
		m = updateKey(m, keyRunes("N"))
		m = updateKey(m, keyRunes(name))
		m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if m.mode != modeFocus {
		t.Fatalf("a new agent is focused: mode %d", m.mode)
	}
	press := func(key tea.KeyPressMsg) {
		next, _ := m.Update(key)
		m = next.(Model)
	}
	where := func() string {
		r := m.rows[0]
		if v := m.viewOf(r); v != viewAgent {
			return v.label()
		}
		a, _ := m.curAgent(r)
		return m.agentLabel(a)
	}
	f12 := tea.KeyPressMsg{Code: tea.KeyF12}

	if where() != "two" {
		t.Fatalf("on the newest agent: %q", where())
	}
	press(f12) // opens on the stop after "two": the terminal
	if m.mode != modeSwitch || m.switchSel != 2 {
		t.Fatalf("f12 opens the switcher on the next stop: mode %d sel %d", m.mode, m.switchSel)
	}
	overlay := ansi.Strip(m.render())
	for _, want := range []string{"Switch view", "▸ 3 $ terminal", "1 ◈ one", "2 ◈ two", "4 ✎ editor", "not started"} {
		if !strings.Contains(overlay, want) {
			t.Errorf("the overlay lacks %q:\n%s", want, overlay)
		}
	}
	press(f12) // the choice moves to the editor
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if where() != "editor" || m.mode != modeFocus || !m.tmux.Has(tmuxCompanion(m, viewEditor)) {
		t.Fatalf("enter starts and focuses the editor: %q mode %d", where(), m.mode)
	}

	press(f12) // from the editor the next stop wraps to the first agent
	press(tea.KeyPressMsg{Code: '1', Text: "1"})
	if where() != "one" || m.mode != modeFocus {
		t.Fatalf("a number in the overlay picks that view and keeps focus: %q mode %d", where(), m.mode)
	}

	press(f12)
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if where() != "one" || m.mode != modeFocus {
		t.Fatalf("esc closes the overlay and changes nothing: %q mode %d", where(), m.mode)
	}
	for _, k := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: tea.KeyEnter}} {
		press(f12)
		press(k)
		if k.Code == 'x' && m.mode != modeSwitch {
			t.Fatalf("a key the overlay does not know must not leave it or reach the pane")
		}
		m.mode = modeFocus
	}

	press(tea.KeyPressMsg{Code: tea.KeyF3}) // direct, no overlay
	if where() != "terminal" || m.mode != modeFocus {
		t.Fatalf("f3 jumps to the terminal at once: %q mode %d", where(), m.mode)
	}
	press(tea.KeyPressMsg{Code: '1', Mod: tea.ModCtrl})
	if where() != "one" {
		t.Fatalf("ctrl+1 jumps to the first agent: %q", where())
	}
	press(tea.KeyPressMsg{Code: tea.KeyF9})
	if m.notice == "" || where() != "one" {
		t.Errorf("a number past the last view changes nothing and says so: %q", m.notice)
	}

	// the numbered strip tells which key goes where
	strip := ansi.Strip(m.switcherStrip(m.rows[0]))
	for _, want := range []string{"1 ◈ one", "2 ◈ two", "3 $ terminal", "4 ✎ editor"} {
		if !strings.Contains(strip, want) {
			t.Errorf("strip %q lacks %q", strip, want)
		}
	}
}

func tmuxCompanion(m Model, v paneView) string {
	return tmux.CompanionName(v.kind(), m.rows[0].session)
}

func TestSwitcherJumpInTheListMovesOnlyTheCursor(t *testing.T) {
	m, _ := agentModel(t)
	m.cfg.TerminalCmd = "cat"
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updateKey(m, keyRunes("solo"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.mode = modeList
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	m = next.(Model)
	if m.viewOf(m.rows[0]) != viewTerminal || m.mode != modeList {
		t.Errorf("f2 in the list shows the terminal without focusing it: view %d mode %d", m.viewOf(m.rows[0]), m.mode)
	}
}
