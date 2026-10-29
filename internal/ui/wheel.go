package ui

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

// Scrollback reads tmux's history, and a program on the alternate screen has
// none: Claude Code in fullscreen mode (tui: fullscreen), Helix, lazygit and
// vim draw on a screen of their own and scroll it themselves. For those the
// wheel is not ours to use, so while the shown pane is on the alternate screen
// and has asked for mouse reports, wheel events are passed to it as the
// terminal would have sent them, and the program scrolls its own view.
//
// Capture says both things (Alt, Mouse), so the decision costs no extra tmux
// call. The wheel then moves what the program calls scrolling, which is also
// what its users expect: Claude's conversation, Helix's buffer.

// maxWheelBurst bounds the events sent in one tmux call, so a trackpad that
// was spun hard cannot queue an endless scroll.
const maxWheelBurst = 40

// altNoMouseNotice is shown when the wheel meets a program that is on the
// alternate screen but takes no mouse, so there is nothing to scroll.
const altNoMouseNotice = "this program uses the alternate screen and takes no mouse: scroll inside it (PgUp/PgDn work in focus mode)"

// shownCapture is the latest capture of the pane the right side shows, when
// it is for that pane (not a stale one from before a selection change).
func (m Model) shownCapture() (tmux.Capture, string, bool) {
	r, ok := m.selected()
	if !ok {
		return tmux.Capture{}, "", false
	}
	t := m.target(r)
	if !m.preview.ok || m.preview.session != t {
		return tmux.Capture{}, "", false
	}
	return m.preview.cap, t, true
}

// panePoint maps a screen cell to a 1-based position inside the pane's
// terminal area, clamped to it (a wheel over the pane's header counts as the
// first row). It uses the same geometry the renderer and the mouse mapping do.
func (m Model) panePoint(x, row int) (col, r int) {
	cols, rows := m.paneSize()
	left := 0
	if !m.zoomed() {
		leftW, _, _ := m.geometry()
		left = leftW + 1 // the list and its separator
	}
	col = min(max(x-left+1, 1), max(cols, 1))
	r = min(max(row+1, 1), max(rows, 1))
	return col, r
}

// forwardWheel passes a wheel notch to the shown program when that is what
// scrolling means for it. handled reports whether the wheel was consumed.
func (m Model) forwardWheel(h hit, up bool) (Model, bool) {
	c, target, ok := m.shownCapture()
	if !ok || !c.Alt {
		return m, false // a normal screen: tmux history is the scrollback
	}
	if !c.Mouse {
		m.notice = altNoMouseNotice
		return m, true
	}
	// h.line is a body line; the pane's own header is above its terminal area.
	row := m.paneRowAt(h.line)
	col, r := m.panePoint(h.x, row)
	m.wheel.add(m.send, m.tmux, wheelRun{target: target, up: up, col: col, row: r, sgr: c.MouseSGR, n: 1})
	m.idlePolls = 0
	return m, true
}

// forwardScrollKey turns the list-mode scroll keys into PageUp / PageDown for
// a program on the alternate screen, whose own scrolling they drive (in focus
// mode PgUp and PgDn already reach it as ordinary keys).
func (m Model) forwardScrollKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	c, target, ok := m.shownCapture()
	if !ok || !c.Alt {
		return nil, false
	}
	key := ""
	switch msg.String() {
	case "pgup", "ctrl+u":
		key = "PageUp"
	case "pgdown", "ctrl+d":
		key = "PageDown"
	default:
		return nil, false
	}
	driver := m.tmux
	m.send.typed(driver, target, func() { _ = driver.SendKeys(target, key) })
	return nil, true
}

// wheelRun is a number of identical notches waiting to be sent.
type wheelRun struct {
	target   string
	up, sgr  bool
	col, row int
	n        int
}

func (a wheelRun) sameKind(b wheelRun) bool {
	return a.target == b.target && a.up == b.up && a.sgr == b.sgr
}

// wheelBatch coalesces wheel notches. A trackpad can produce dozens of events
// a second, and each tmux call is a process; notches that arrive while an
// earlier send is still running go out together in the next one.
type wheelBatch struct {
	mu        sync.Mutex
	runs      []wheelRun
	scheduled bool
}

func (w *wheelBatch) add(s *sender, d tmux.Driver, run wheelRun) {
	w.mu.Lock()
	if n := len(w.runs); n > 0 && w.runs[n-1].sameKind(run) {
		w.runs[n-1].n += run.n
		w.runs[n-1].col, w.runs[n-1].row = run.col, run.row
	} else {
		w.runs = append(w.runs, run)
	}
	schedule := !w.scheduled
	w.scheduled = true
	w.mu.Unlock()
	if schedule {
		// Through the sender, so wheel events stay ordered with typed keys,
		// and the pane is captured shortly after to show the result.
		s.typed(d, run.target, func() { w.flush(d) })
	}
}

func (w *wheelBatch) flush(d tmux.Driver) {
	w.mu.Lock()
	runs := w.runs
	w.runs, w.scheduled = nil, false
	w.mu.Unlock()
	for _, r := range runs {
		_ = d.SendWheel(r.target, r.up, min(r.n, maxWheelBurst), r.col, r.row, r.sgr)
	}
}
