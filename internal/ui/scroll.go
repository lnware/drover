package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

// Scrollback for the right pane.
//
// The view is a window of the pane's history: off lines above the live
// bottom (0 = live). Only that window is captured, never the whole history,
// so scrolling costs one tmux call of pane height however long the history is.
//
// The state belongs to "the pane being shown" (scrollKey), not to the agent
// session as such, so panes other than the agent's can reuse it by returning
// their own key and capture target.

const (
	// wheelStep is lines per wheel notch, what terminals and pagers use.
	wheelStep = 3
)

type scrollState struct {
	key  string // pane the offset belongs to; any other pane is live
	off  int    // lines above the live bottom; 0 = live, state is cleared
	hist int    // history size at the last capture, to follow new output

	// cap is the latest window read, taken at capOff.
	cap   tmux.Capture
	capOK bool
	// busy means a capture is in flight. Wheel events arrive faster than
	// tmux answers, so they only move off; the completion re-reads if off
	// moved meanwhile. One process at a time, and always the newest offset.
	busy bool
}

// scrollMsg is a window read at off. poll says it belongs to the preview
// polling chain, which must be continued when it is handled.
type scrollMsg struct {
	key  string
	off  int
	loop int
	poll bool
	cap  tmux.Capture
	hist int
	err  error
}

// scrollKey identifies the pane that scrollback would apply to right now.
func (m Model) scrollKey() (string, bool) {
	r, ok := m.selected()
	if !ok || !m.targetRunning(r) {
		return "", false
	}
	return m.target(r), true
}

// scrolled reports whether the pane being shown is scrolled back.
func (m Model) scrolled() bool {
	key, ok := m.scrollKey()
	return ok && m.scroll.off > 0 && m.scroll.key == key
}

// scrollBy moves the window delta lines back (positive) or forward.
func (m *Model) scrollBy(delta int) tea.Cmd {
	key, ok := m.scrollKey()
	if !ok {
		return nil
	}
	if m.scroll.key != key {
		m.scroll = scrollState{key: key}
	}
	off := max(m.scroll.off+delta, 0)
	if h := m.scroll.hist; h > 0 {
		off = min(off, h)
	}
	switch {
	case off == m.scroll.off:
		return nil
	case off == 0:
		return m.exitScroll()
	}
	m.scroll.off = off
	return m.scrollFetch(false)
}

// exitScroll returns to the live view and refreshes it at once.
func (m *Model) exitScroll() tea.Cmd {
	m.scroll = scrollState{}
	return m.startPreview()
}

// scrollFetch reads the window at the current offset, unless a read is in
// flight (its completion will notice the offset moved and read again).
func (m *Model) scrollFetch(poll bool) tea.Cmd {
	_, rows := m.paneSize()
	if m.scroll.busy || rows <= 0 {
		return nil
	}
	m.scroll.busy = true
	driver, key, off, loop := m.tmux, m.scroll.key, m.scroll.off, m.loop
	return func() tea.Msg {
		c, hist, err := driver.CaptureScroll(key, off, rows)
		return scrollMsg{key: key, off: off, loop: loop, poll: poll, cap: c, hist: hist, err: err}
	}
}

// pollScroll is the preview poll while scrolled back. It keeps polling,
// rather than freezing, so the offset can follow new output: when the history
// grows by n lines the view moves back by n, so the lines you are reading stay
// where they are instead of sliding up under you. (Freezing would show
// different lines each time the agent printed something.)
func (m *Model) pollScroll() (tea.Cmd, bool) {
	if !m.scrolled() {
		return nil, false
	}
	if c := m.scrollFetch(true); c != nil {
		return c, true
	}
	return m.nextPoll(), true // a read is in flight or the pane has no size yet
}

func (m Model) handleScroll(msg scrollMsg) (tea.Model, tea.Cmd) {
	m.scroll.busy = false
	// Only the chain that is current continues; older ones were replaced by
	// startPreview and must end here.
	poll := msg.poll && msg.loop == m.loop
	next := func() tea.Cmd {
		if poll {
			return m.nextPoll()
		}
		return nil
	}
	if !m.scrolled() || m.scroll.key != msg.key {
		return m, next() // left scrollback while the read was in flight
	}
	if msg.err != nil {
		// Let the live poll find out whether the session is gone.
		cmd := m.exitScroll()
		return m, cmd
	}
	if m.scroll.hist > 0 && msg.hist > m.scroll.hist {
		m.scroll.off += msg.hist - m.scroll.hist // output arrived: stay on the same lines
	}
	m.scroll.hist = msg.hist
	m.scroll.off = min(m.scroll.off, msg.hist) // tmux clamped a request past the oldest line
	if m.scroll.off == 0 {
		cmd := m.exitScroll()
		return m, cmd
	}
	if msg.off != m.scroll.off {
		cmd := m.scrollFetch(poll)
		return m, cmd
	}
	if m.scroll.capOK && sameCapture(m.scroll.cap, msg.cap) {
		m.idlePolls++
	} else {
		m.idlePolls = 0
	}
	m.scroll.cap, m.scroll.capOK = msg.cap, true
	return m, next()
}

// scrollRule is the pane's header rule while scrolled back, empty when live.
func (m Model) scrollRule() string {
	if !m.scrolled() {
		return ""
	}
	s := focusHint.Render(fmt.Sprintf("scrollback -%d", m.scroll.off)) + dimStyle.Render(" · End returns")
	if m.mode == modeFocus {
		s += dimStyle.Render(" · typing returns")
	}
	return s
}

// paneLines draws the pane body: the scrolled window when scrolled back
// (without the agent's cursor, which belongs to the live screen), else the
// live capture.
func (m Model) paneLines(w, rows int) []string {
	if m.scrolled() && m.scroll.capOK {
		return previewLines(preview{cap: m.scroll.cap}, w, rows, false)
	}
	return previewLines(m.preview, w, rows, m.mode == modeFocus && !m.scrolled())
}

// scrollListKey handles scrollback keys in list mode and reports whether it
// consumed the key. ctrl+u / ctrl+d are free here; in focus mode they belong
// to the agent.
func (m *Model) scrollListKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if _, ok := m.scrollKey(); !ok {
		return nil, false
	}
	if c, ok := m.forwardScrollKey(msg); ok {
		return c, true
	}
	_, rows := m.paneSize()
	page, half := max(rows-2, 1), max(rows/2, 1)
	switch msg.String() {
	case "pgup":
		return m.scrollBy(page), true
	case "pgdown":
		return m.scrollBy(-page), true
	case "ctrl+u":
		return m.scrollBy(half), true
	case "ctrl+d":
		return m.scrollBy(-half), true
	}
	if m.scrolled() {
		switch msg.String() {
		case "end", "esc", "q": // q would otherwise quit; leaving scrollback comes first
			return m.exitScroll(), true
		}
	}
	return nil, false
}

// scrollFocusKey handles keys in focus mode while scrolled back. Page keys
// keep scrolling, End and esc return to live, and anything else exits
// scrollback and then reaches the agent, so you never type into a view that
// is not the live screen. When live, every key goes to the agent as before.
func (m *Model) scrollFocusKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.scrolled() {
		return nil, false
	}
	_, rows := m.paneSize()
	switch msg.String() {
	case "pgup":
		return m.scrollBy(max(rows-2, 1)), true
	case "pgdown":
		return m.scrollBy(-max(rows-2, 1)), true
	case "end", "esc":
		return m.exitScroll(), true
	}
	m.scroll = scrollState{} // the echo of the key refreshes the live view
	return nil, false
}
