package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// altModel is a mouseModel whose first row runs a program that switches to
// the alternate screen (and optionally asks for SGR mouse reports), echoing
// every byte it receives in caret notation so the test can see what arrived.
func altModel(t *testing.T, mouse bool) Model {
	t.Helper()
	skipWithoutTmux(t)
	m := mouseModel(t)
	manyRows(&m, 2)
	m.rows[0].running = true
	m.all = m.rows
	s := m.rows[0].session
	modes := `\033[?1049h`
	if mouse {
		modes += `\033[?1000h\033[?1006h`
	}
	if err := m.tmux.Spawn(s, t.TempDir(), `printf '`+modes+`'; exec cat -v`); err != nil {
		t.Fatal(err)
	}
	cols, rows := m.paneSize()
	if err := m.tmux.Resize(s, cols, rows); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ { // wait for the modes to take effect, then adopt the capture
		c, err := m.tmux.Capture(s)
		if err == nil && c.Alt && c.Mouse == mouse {
			m.preview = preview{session: s, ok: true, cap: c}
			return m
		}
		if i > 100 {
			t.Fatalf("pane never reached alt=true mouse=%v: %+v", mouse, c)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func paneHas(t *testing.T, m Model, want string) string {
	t.Helper()
	s := m.rows[0].session
	var all string
	for i := 0; i < 100; i++ {
		if c, err := m.tmux.Capture(s); err == nil {
			all = strings.Join(c.Lines, "")
			if strings.Contains(all, want) {
				return all
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the pane never received %q; it shows %q", want, all)
	return ""
}

func TestWheelOverAnAlternateScreenProgramIsForwardedNotScrolledBack(t *testing.T) {
	m := altModel(t, true)
	leftW, _, _ := m.geometry()
	x, y := leftW+4, 12 // pane column 4; body line 11 is pane row 7, so 1-based (4, 8)

	next, _ := m.Update(click(x, y, tea.MouseWheelUp))
	m = next.(Model)
	paneHas(t, m, "^[[<64;4;8M")
	if m.scrolled() {
		t.Error("the wheel entered drover's scrollback, which is empty on the alternate screen")
	}

	next, _ = m.Update(click(x, y, tea.MouseWheelDown))
	paneHas(t, next.(Model), "^[[<65;4;8M")
}

func TestWheelBurstsAreCoalescedWithoutLosingNotches(t *testing.T) {
	m := altModel(t, true)
	leftW, _, _ := m.geometry()
	for i := 0; i < 30; i++ {
		next, _ := m.Update(click(leftW+4, 12, tea.MouseWheelDown))
		m = next.(Model)
	}
	deadline := time.Now().Add(3 * time.Second)
	got := 0
	for time.Now().Before(deadline) {
		c, _ := m.tmux.Capture(m.rows[0].session)
		got = strings.Count(strings.Join(c.Lines, ""), "^[[<65;4;8M")
		if got == 30 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("%d of 30 wheel notches arrived", got)
}

func TestWheelOverAnAlternateScreenWithoutMouseExplainsItself(t *testing.T) {
	m := altModel(t, false)
	leftW, _, _ := m.geometry()
	next, cmd := m.Update(click(leftW+4, 12, tea.MouseWheelUp))
	m = next.(Model)
	if cmd != nil {
		t.Error("nothing should be scheduled when there is nothing to scroll")
	}
	if !strings.Contains(m.notice, "alternate screen") {
		t.Errorf("the user is told nothing: notice %q", m.notice)
	}
	time.Sleep(150 * time.Millisecond)
	c, _ := m.tmux.Capture(m.rows[0].session)
	if all := strings.Join(c.Lines, ""); strings.Contains(all, "^[") {
		t.Errorf("escape sequences were typed into a program that did not ask for them: %q", all)
	}
}

func TestListModePageKeysDriveAnAlternateScreenProgram(t *testing.T) {
	m := altModel(t, true)
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyPgUp}, {Code: tea.KeyPgDown}, {Code: 'u', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}} {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	all := paneHas(t, m, "^[[6~") // PageDown arrives last
	if strings.Count(all, "^[[5~") != 2 || strings.Count(all, "^[[6~") != 2 {
		t.Errorf("expected two PageUp and two PageDown, got %q", all)
	}
}

func TestZoomedWheelUsesThePointerPositionInsideTheZoomedPane(t *testing.T) {
	m := focused(altModel(t, true))
	m.zoom = true
	cols, rows := m.paneSize()
	if err := m.tmux.Resize(m.rows[0].session, cols, rows); err != nil {
		t.Fatal(err)
	}
	// zoomed: no list, one header line, so screen (10, 5) is pane column 11, row 5
	next, _ := m.Update(click(10, 5, tea.MouseWheelUp))
	paneHas(t, next.(Model), "^[[<64;11;5M")
}

func TestWheelOverANormalScreenStillUsesDromersScrollback(t *testing.T) {
	m := scrollModel(t, 300) // a plain pane with history, as before
	m = wheel(t, m, true)
	if !m.scrolled() {
		t.Error("a normal-screen pane lost its tmux scrollback")
	}
}
