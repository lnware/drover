package ui

import (
	"testing"
	"time"

	"github.com/lnware/drover/internal/tmux"
)

func TestPollIntervalBacksOffAndIsCapped(t *testing.T) {
	for _, focus := range []bool{true, false} {
		prev := time.Duration(0)
		for idle := -1; idle < 20; idle++ {
			d := pollInterval(focus, idle)
			if d < prev {
				t.Errorf("focus=%v idle=%d: interval %v shrank from %v", focus, idle, d, prev)
			}
			prev = d
		}
		if prev > time.Second {
			t.Errorf("focus=%v: the slowest poll is %v, a streaming answer would lag", focus, prev)
		}
	}
	if pollInterval(true, 0) != focusPoll || pollInterval(false, 0) != listPoll {
		t.Error("a changing pane must be polled at the fast rate")
	}
	if pollInterval(true, 0) >= pollInterval(true, 10) {
		t.Error("focus polling does not back off")
	}
}

func TestSameCapture(t *testing.T) {
	a := tmux.Capture{Lines: []string{"x", "y"}, CursorX: 1, CursorY: 2, CursorVisible: true}
	same := tmux.Capture{Lines: []string{"x", "y"}, CursorX: 1, CursorY: 2, CursorVisible: true}
	if !sameCapture(a, same) {
		t.Error("identical captures differ")
	}
	for name, other := range map[string]tmux.Capture{
		"text":    {Lines: []string{"x", "z"}, CursorX: 1, CursorY: 2, CursorVisible: true},
		"cursor":  {Lines: []string{"x", "y"}, CursorX: 2, CursorY: 2, CursorVisible: true},
		"hidden":  {Lines: []string{"x", "y"}, CursorX: 1, CursorY: 2},
		"shorter": {Lines: []string{"x"}, CursorX: 1, CursorY: 2, CursorVisible: true},
	} {
		if sameCapture(a, other) {
			t.Errorf("a changed %s was not noticed", name)
		}
	}
}

func previewFor(m Model, lines ...string) previewMsg {
	return previewMsg{loop: m.loop, session: "infra/main", at: time.Now(), cap: tmux.Capture{Lines: lines}}
}

func TestIdleCountGrowsOnUnchangedPaneAndResetsOnChange(t *testing.T) {
	m := testModel()
	m.mode = modeFocus
	m.preview = preview{} // nothing shown yet
	step := func(lines ...string) Model {
		next, _ := m.handlePreview(previewFor(m, lines...))
		m = next.(Model)
		time.Sleep(time.Millisecond) // captures have increasing timestamps
		return m
	}
	step("a")
	if m.idlePolls != 0 {
		t.Fatalf("first capture is a change: idle = %d", m.idlePolls)
	}
	step("a")
	step("a")
	step("a")
	if m.idlePolls != 3 {
		t.Fatalf("three unchanged captures: idle = %d, want 3", m.idlePolls)
	}
	step("b")
	if m.idlePolls != 0 {
		t.Errorf("a changed pane must reset the back-off: idle = %d", m.idlePolls)
	}
}

func TestKeystrokeLeavesSlowPolling(t *testing.T) {
	m := testModel()
	m.mode = modeFocus
	m.idlePolls = 4 // polling every 400ms
	loop := m.loop
	next, cmd := m.handleFocusKey(keyRunes("a"))
	m = next.(Model)
	if m.idlePolls != 0 || m.loop == loop || cmd == nil {
		t.Errorf("a key must restart fast polling: idle=%d loop %d->%d cmd=%v", m.idlePolls, loop, m.loop, cmd != nil)
	}

	// in fast mode a key must not restart the chain: the echo path covers it
	m.idlePolls, loop = 1, m.loop
	next, cmd = m.handleFocusKey(keyRunes("b"))
	m = next.(Model)
	if m.loop != loop || cmd != nil {
		t.Errorf("a key restarted polling although it was already fast: loop %d->%d", loop, m.loop)
	}
}

func TestSelectionChangeResetsBackoff(t *testing.T) {
	m := testModel()
	m.idlePolls = 4
	m.move(1)
	_ = m.startPreview()
	if m.idlePolls != 0 {
		t.Errorf("a new selection must be polled fast: idle = %d", m.idlePolls)
	}
}

func TestSessionRefreshUpdatesRunningWithoutGit(t *testing.T) {
	m := testModel() // main running, feature/a stopped
	m = m.handleSessions(sessionsMsg{live: []string{"infra/feature/a"}})
	if m.all[0].running || !m.all[1].running {
		t.Errorf("running flags not updated: %v %v", m.all[0].running, m.all[1].running)
	}
	if len(m.rows) != 2 {
		t.Errorf("the session refresh must keep the rows: %d", len(m.rows))
	}
}

func TestOnlyEveryNthTickScansWorktrees(t *testing.T) {
	m := testModel()
	full := 0
	for i := 0; i < worktreeEvery*3; i++ {
		before := m.ticks
		next, _ := m.Update(tick{})
		m = next.(Model)
		if m.ticks != before+1 {
			t.Fatalf("tick %d did not count", i)
		}
		if m.ticks%worktreeEvery == 0 {
			full++
		}
	}
	if full != 3 {
		t.Errorf("%d full scans in %d ticks, want 3", full, worktreeEvery*3)
	}
}
