package ui

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func skipWithoutTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// scrollModel is a mouseModel whose first row runs a fake agent that prints
// the numbers 1..n and then waits, with the right pane at its real size.
func scrollModel(t *testing.T, n int) Model {
	t.Helper()
	skipWithoutTmux(t)
	m := mouseModel(t)
	manyRows(&m, 2)
	m.rows[0].running = true
	m.all = m.rows
	s := m.rows[0].session
	if err := m.tmux.Spawn(s, t.TempDir(), "seq 1 "+strconv.Itoa(n)+"; cat"); err != nil {
		t.Fatal(err)
	}
	cols, rows := m.paneSize()
	if err := m.tmux.Resize(s, cols, rows); err != nil {
		t.Fatal(err)
	}
	// The agent prints asynchronously; wait until the last number is there.
	for i := 0; ; i++ {
		c, err := m.tmux.Capture(s)
		if err == nil && strings.Contains(strings.Join(c.Lines, "\n"), strconv.Itoa(n)) {
			m.preview = preview{session: s, ok: true, cap: c}
			return m
		}
		if i > 100 {
			t.Fatal("fake agent never printed its output")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// settle runs the capture the model asked for and feeds the answer back,
// until no read is in flight. Only scroll reads are run, never poll ticks,
// so the test is deterministic and spawns no timers.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; m.scroll.busy; i++ {
		if cmd == nil || i > 10 {
			t.Fatalf("scroll read in flight but no command to complete it (round %d)", i)
		}
		msg, ok := cmd().(scrollMsg)
		if !ok {
			t.Fatalf("a busy scroll must be waiting on a scrollMsg, got %T", cmd())
		}
		next, c := m.handleScroll(msg)
		m, cmd = next.(Model), c
	}
	return m
}

func wheel(t *testing.T, m Model, up bool) Model {
	t.Helper()
	leftW, _, _ := m.geometry()
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	next, cmd := m.Update(click(leftW+4, 12, b))
	return settle(t, next.(Model), cmd)
}

// paneText is the right pane's terminal area as plain text, one string per row.
func paneText(m Model) []string {
	leftW, _, _ := m.geometry()
	screen := strings.Split(m.render(), "\n")
	_, rows := m.paneSize()
	var out []string
	for i := 0; i < rows; i++ {
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(screen[1+paneChrome+i], leftW+1, m.width)), " "))
	}
	return out
}

func TestWheelScrollsBackAndForth(t *testing.T) {
	m := scrollModel(t, 300)
	live := paneText(m)
	_, rows := m.paneSize()

	m = wheel(t, m, true)
	if m.scroll.off != wheelStep || !m.scrolled() {
		t.Fatalf("one notch up: off = %d", m.scroll.off)
	}
	got := paneText(m)
	// Same numbers, shifted down by the step: row i now holds live row i-step.
	for i := wheelStep; i < rows; i++ {
		if got[i] != live[i-wheelStep] {
			t.Fatalf("row %d = %q, want live row %d %q", i, got[i], i-wheelStep, live[i-wheelStep])
		}
	}
	if !strings.Contains(ansi.Strip(m.render()), "scrollback -3") || !strings.Contains(ansi.Strip(m.render()), "End returns") {
		t.Errorf("no scrollback indicator in the header:\n%s", ansi.Strip(m.render()))
	}

	m = wheel(t, m, true)
	if m.scroll.off != 2*wheelStep {
		t.Errorf("two notches: off = %d", m.scroll.off)
	}
	m = wheel(t, m, false)
	m = wheel(t, m, false)
	if m.scroll.off != 0 || m.scrolled() {
		t.Errorf("wheel back down to the bottom must return to live: off = %d", m.scroll.off)
	}
	if strings.Contains(ansi.Strip(m.render()), "scrollback") {
		t.Error("indicator still shown when live")
	}
	// Past the bottom there is nothing to do.
	if m = wheel(t, m, false); m.scroll.off != 0 {
		t.Errorf("wheel down while live: off = %d", m.scroll.off)
	}
}

func TestScrollClampsAtOldestLine(t *testing.T) {
	m := scrollModel(t, 300)
	for i := 0; i < 15; i++ {
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = settle(t, next.(Model), cmd)
	}
	hist := m.scroll.hist
	if hist < 250 || m.scroll.off != hist {
		t.Fatalf("after paging past the top: off %d, history %d", m.scroll.off, hist)
	}
	if top := paneText(m)[0]; top != "1" {
		t.Errorf("oldest line shown at the top = %q, want 1", top)
	}
	// And it can come back down from the clamp.
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = settle(t, next.(Model), cmd)
	if m.scroll.off >= hist {
		t.Errorf("PageDown did not move: off %d", m.scroll.off)
	}
}

// TestScrolledViewStaysPutWhileOutputArrives is the offset-tracking promise:
// new output must not move the lines being read.
func TestScrolledViewStaysPutWhileOutputArrives(t *testing.T) {
	m := scrollModel(t, 300)
	m = wheel(t, m, true)
	m = wheel(t, m, true)
	before, off := paneText(m), m.scroll.off

	s := m.rows[0].session
	const extra = 5
	for i := 0; i < extra; i++ {
		if err := m.tmux.SendText(s, "x"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		if err := m.tmux.SendKeys(s, "Enter"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; ; i++ { // wait until the history has actually grown
		_, hist, _ := m.tmux.CaptureScroll(s, 1, 5)
		if hist >= m.scroll.hist+extra {
			break
		}
		if i > 100 {
			t.Fatal("history never grew")
		}
		time.Sleep(20 * time.Millisecond)
	}

	m.loop = 7
	cmd, ok := m.pollScroll()
	if !ok || !m.scroll.busy {
		t.Fatal("polling while scrolled must read the scrolled window")
	}
	m = settle(t, m, cmd)
	if m.scroll.off <= off {
		t.Errorf("offset did not follow the new output: %d -> %d", off, m.scroll.off)
	}
	if after := paneText(m); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("lines moved under the reader\nbefore:\n%s\nafter:\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

func TestScrollKeysInListMode(t *testing.T) {
	m := scrollModel(t, 300)
	_, rows := m.paneSize()

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = settle(t, next.(Model), cmd)
	if m.scroll.off != rows-2 {
		t.Errorf("PageUp: off %d, want a page minus overlap (%d)", m.scroll.off, rows-2)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = settle(t, next.(Model), cmd)
	if want := rows - 2 - rows/2; m.scroll.off != want {
		t.Errorf("ctrl+d: off %d, want %d", m.scroll.off, want)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = settle(t, next.(Model), cmd)

	// q returns to live while scrolled; it must not quit.
	next, cmd = m.Update(keyRunes("q"))
	m = next.(Model)
	if m.scrolled() {
		t.Error("q did not leave scrollback")
	}
	if cmd != nil {
		if _, quit := runQuiet(cmd).(tea.QuitMsg); quit {
			t.Error("q quit instead of returning to live")
		}
	}
	// ...and quits as before once live.
	_, cmd = m.Update(keyRunes("q"))
	if cmd == nil || runQuiet(cmd) != (tea.QuitMsg{}) {
		t.Error("q no longer quits when live")
	}

	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEnd}, {Code: tea.KeyEscape}} {
		next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = settle(t, next.(Model), cmd)
		next, _ = m.Update(k)
		if next.(Model).scrolled() {
			t.Errorf("%v did not leave scrollback", k)
		}
	}
}

// runQuiet runs a command that is known to return at once (quit, or a batch
// of them), not one that sleeps on a timer.
func runQuiet(c tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func TestFocusModeScrollbackThenTyping(t *testing.T) {
	m := scrollModel(t, 300)
	m.mode = modeFocus
	m = wheel(t, m, true) // wheel scrolls in focus mode
	if !m.scrolled() {
		t.Fatal("wheel over the pane did not scroll in focus mode")
	}
	if strings.Contains(ansi.Strip(m.render()), "█") || !strings.Contains(ansi.Strip(m.render()), "typing returns") {
		t.Errorf("focus-mode scrollback must hide the cursor and say typing returns:\n%s", ansi.Strip(m.render()))
	}

	// PageUp keeps scrolling instead of reaching the agent.
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = settle(t, next.(Model), cmd)
	if m.scroll.off <= wheelStep {
		t.Errorf("PageUp in scrolled focus mode: off %d", m.scroll.off)
	}

	// A typed key returns to live and is forwarded, which the agent shows.
	next, _ = m.Update(keyRunes("z"))
	m = next.(Model)
	if m.scrolled() || m.mode != modeFocus {
		t.Errorf("typing must leave scrollback and stay focused: off %d mode %d", m.scroll.off, m.mode)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := m.tmux.Capture(m.rows[0].session)
		if err == nil && strings.Contains(strings.Join(c.Lines, "\n"), "z") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the key that left scrollback never reached the agent")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// With nothing scrolled, PageUp is the agent's, not ours.
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if next.(Model).scrolled() {
		t.Error("PageUp scrolled while live in focus mode; it belongs to the agent")
	}
	_ = cmd
}

func TestScrollOffsetBelongsToItsPane(t *testing.T) {
	m := scrollModel(t, 300)
	m = wheel(t, m, true)
	if !m.scrolled() {
		t.Fatal("not scrolled")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.scrolled() || m.scroll.off != 0 {
		t.Errorf("moving to another worktree kept the scroll: %+v", m.scroll)
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.scrolled() {
		t.Error("coming back must show the live screen")
	}
}
