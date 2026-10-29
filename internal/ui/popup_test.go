package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/tmux"
)

func ctrlBackslash() tea.KeyPressMsg { return tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl} }

func assertExactWidth(t *testing.T, m Model, what string) {
	t.Helper()
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Fatalf("%s: %d lines, want %d", what, len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("%s: line %d is %d cells, want %d:\n%s", what, i, w, m.width, ansi.Strip(m.render()))
		}
	}
}

func allSessions(t *testing.T, m Model) []string {
	t.Helper()
	agents, companions, err := m.tmux.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	return append(agents, companions...)
}

func TestPopupOpensOverTheScreenAndCloseLeavesNoSession(t *testing.T) {
	m := companionModel(t)
	r := m.rows[0]
	next, open := m.handleKey(keyRunes("G"))
	m = next.(Model)
	name := tmux.CompanionName("git", r.session)
	if !m.popup.open || m.mode != modePopup || !m.tmux.Has(name) {
		t.Fatalf("popup %+v mode %d session alive %v", m.popup, m.mode, m.tmux.Has(name))
	}
	// Lazygit is not a pane, a row or a tab: nothing in the tree changed.
	if m.sub != viewAgent {
		t.Error("the popup must not move the cursor onto a child")
	}
	for _, it := range m.items() {
		if it.kind == itemCompanion {
			t.Errorf("lazygit got a row: %+v", it)
		}
	}
	plain := ansi.Strip(m.render())
	if !strings.Contains(plain, "lazygit · main") {
		t.Errorf("the popup title is missing:\n%s", plain)
	}
	if !m.anyCompanion(r) {
		t.Error("d and the rename must keep refusing while the popup runs")
	}
	// The session is the popup's size, once polled.
	cols, rows := m.popupSize()
	runCmd(open) // the poll that opening starts resizes the session, then captures
	if c, err := m.tmux.Capture(name); err != nil || len(c.Lines) != rows {
		t.Fatalf("session not resized to the popup's %dx%d: %d lines, err %v", cols, rows, len(c.Lines), err)
	}

	// Every key goes to it, even the ones that quit or act in the list.
	got := make(chan echoMsg, 16)
	m.send.notify = func(msg tea.Msg) {
		if e, ok := msg.(echoMsg); ok {
			select {
			case got <- e:
			default:
			}
		}
	}
	next, quit := m.Update(keyRunes("q"))
	m = next.(Model)
	if quit != nil || !m.popup.open || m.mode != modePopup {
		t.Fatal("q must go to lazygit, not quit drover or close the popup")
	}
	deadline := time.After(3 * time.Second)
wait:
	for {
		select {
		case e := <-got:
			if e.session != name {
				t.Fatalf("echo from %q, want the popup's session %q", e.session, name)
			}
			if strings.Contains(strings.Join(e.cap.Lines, "\n"), "q") {
				break wait
			}
		case <-deadline:
			t.Fatal("the key never reached lazygit")
		}
	}

	// ctrl+\ closes and kills.
	next, _ = m.Update(ctrlBackslash())
	m = next.(Model)
	if m.popup.open || m.mode != modeList {
		t.Errorf("popup %+v mode %d after ctrl+\\", m.popup, m.mode)
	}
	if s := allSessions(t, m); len(s) != 0 {
		t.Errorf("sessions left behind: %v", s)
	}
	if m.anyCompanion(r) {
		t.Error("the closed popup is still counted as a companion")
	}
}

func TestPopupClosesWithCtrlQToo(t *testing.T) {
	m := companionModel(t)
	m = pressKey(t, m, "G")
	next, _ := m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	m = next.(Model)
	if m.popup.open || len(allSessions(t, m)) != 0 {
		t.Errorf("ctrl+q left popup %+v sessions %v", m.popup, allSessions(t, m))
	}
}

func TestPopupClosesByItselfWhenLazygitExits(t *testing.T) {
	m := companionModel(t)
	m.cfg.LazygitCmd = "sleep 0.4"
	m = pressKey(t, m, "G")
	name := m.popup.session
	deadline := time.Now().Add(5 * time.Second)
	for m.tmux.Has(name) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// What the poll reports once the session is gone.
	next, _ := m.Update(previewMsg{loop: m.loop, session: name, err: errGone})
	m = next.(Model)
	if m.popup.open || m.mode != modeList {
		t.Errorf("popup %+v mode %d: lazygit quit but the popup stayed", m.popup, m.mode)
	}
	if m.notice != "" {
		t.Errorf("a normal quit needs no note, got %q", m.notice)
	}
}

func TestPopupIgnoresTheMouse(t *testing.T) {
	m := companionModel(t)
	m = pressKey(t, m, "G")
	before := m.cursor
	for _, msg := range []tea.MouseMsg{
		click(5, 3, tea.MouseLeft), click(100, 10, tea.MouseLeft),
		click(5, 3, tea.MouseWheelDown), click(100, 10, tea.MouseWheelUp),
	} {
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd != nil || m.cursor != before || m.mode != modePopup || m.scroll.off != 0 {
			t.Fatalf("mouse %+v changed state: cursor %d mode %d", msg, m.cursor, m.mode)
		}
	}
}

func TestMissingLazygitOpensNoPopup(t *testing.T) {
	m := companionModel(t)
	m.cfg.LazygitCmd = "lazygit-that-is-not-installed-xyz"
	m = pressKey(t, m, "G")
	if m.popup.open || m.mode != modeList || !strings.Contains(m.status, "lazygit_cmd") {
		t.Errorf("popup %+v mode %d status %q", m.popup, m.mode, m.status)
	}
}

func TestStrayLazygitSessionsAreReaped(t *testing.T) {
	m := companionModel(t)
	stray := tmux.CompanionName("git", m.rows[0].session)
	if err := m.tmux.SpawnCompanion(stray, t.TempDir(), "cat", "stray"); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(sessionsMsg{live: nil, companions: []string{stray}})
	m = next.(Model)
	if m.tmux.Has(stray) || m.companions[stray] {
		t.Error("lazygit is ephemeral: a session that is not the open popup must not survive")
	}
}

func TestViewIsExactWidthWithThePopupOpen(t *testing.T) {
	withColour(t)
	m := foldModel(t)
	m.companions[tmux.CompanionName("term", "infra/main")] = true
	m.companions[tmux.CompanionName("edit", "infra/main")] = true
	wide := "\x1b[1;31mred 日本語 wide\x1b[0m " + strings.Repeat("x", 300)
	for _, size := range [][2]int{{60, 12}, {61, 13}, {80, 24}, {140, 40}, {200, 50}} {
		m.width, m.height = size[0], size[1]
		cols, rows := m.popupSize()
		lines := make([]string, rows)
		for i := range lines {
			lines[i] = wide
		}
		for _, withCapture := range []bool{false, true} {
			for _, steps := range []int{0, 1, 2, 5} {
				c := m
				c.reselect()
				c.step(steps)
				c.mode = modePopup
				c.popup = popupState{open: true, session: "git@x", title: "lazygit · " + strings.Repeat("long-branch/", 8)}
				if withCapture {
					c.popup.pv = preview{session: "git@x", ok: true, cap: tmux.Capture{Lines: lines, CursorX: cols - 1, CursorY: rows - 1, CursorVisible: true}}
				}
				assertExactWidth(t, c, "popup")
				if plain := ansi.Strip(c.render()); !strings.Contains(plain, "╭") || !strings.Contains(plain, "╯") {
					t.Fatalf("%dx%d: popup border missing", size[0], size[1])
				}
			}
		}
	}
}

// The memoised overlay must never show a stale capture or frame.
func TestOverlayMemoFollowsCaptureAndFrame(t *testing.T) {
	m := testModel()
	m.popup = popupState{open: true, session: "git@x", title: "t", memo: &overlayMemo{}}
	m.mode = modePopup
	first := m.render()
	if m.render() != first {
		t.Fatal("an unchanged model must render the same")
	}
	m.popup.pv = preview{session: "git@x", ok: true, at: time.Now(), cap: tmux.Capture{Lines: []string{"fresh lazygit output"}}}
	if v := m.render(); !strings.Contains(ansi.Strip(v), "fresh lazygit output") {
		t.Error("a new capture did not reach the screen")
	}
	m.mode, m.notice = modeList, "footer changed" // the footer lies outside the popup
	if v := m.render(); !strings.Contains(ansi.Strip(v), "footer changed") {
		t.Error("a changed frame was served from the memo")
	}
	m.mode = modePopup
	assertExactWidth(t, m, "memoised popup")
}

func TestPopupIsCentredAndRoughlyNinetyByEightyFive(t *testing.T) {
	m := testModel()
	m.width, m.height = 200, 50
	x, y, w, h := m.popupBox()
	if w != 180 || h != 42 || x != 10 || y != 4 {
		t.Errorf("box %d,%d %dx%d", x, y, w, h)
	}
}

func TestHidingStopsTheWatcherForThePopupAndShowingRestartsIt(t *testing.T) {
	m := companionModel(t)
	m.push.notify = func(tea.Msg) {}
	t.Cleanup(func() { m.push.follow(m.tmux, "") })
	m = pressKey(t, m, "G")
	if m.push.session != m.popup.session {
		t.Fatalf("the open popup must be watched: following %q, want %q", m.push.session, m.popup.session)
	}
	m, _ = m.setVisible(false)
	if m.push.session != "" || m.push.watcher != nil {
		t.Fatalf("hidden drover still watches %q", m.push.session)
	}
	m, _ = m.setVisible(true)
	if m.push.session != m.popup.session {
		t.Errorf("becoming visible must resume watching the popup: following %q", m.push.session)
	}
	// Closing stops it for good.
	next, _ := m.Update(ctrlBackslash())
	m = next.(Model)
	if m.push.session != "" {
		t.Errorf("closing the popup left %q watched", m.push.session)
	}
}

// runCmd runs a command and discards its message, to let a poll's resize happen.
func runCmd(c tea.Cmd) {
	if c == nil {
		return
	}
	if batch, ok := c().(tea.BatchMsg); ok {
		for _, sub := range batch {
			runCmd(sub)
		}
	}
}
