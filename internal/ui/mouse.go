package ui

import (
	tea "charm.land/bubbletea/v2"
)

// Mouse support. Everything here maps screen cells through the renderer's own
// geometry() and itemAtLine(), so a layout change cannot leave clicks
// pointing at the wrong thing (mouse_test.go checks it against View()).
//
// In focus mode mouse events are never forwarded to the agent: only the wheel
// over the right pane is used, to scroll back. Forwarding would mean tracking
// which mouse mode the agent asked for and re-encoding events for it, and
// agents such as Claude Code do not use the mouse anyway.

type zone int

const (
	zoneNone zone = iota // title bar, footer, separator, outside the screen
	zoneList             // the left list
	zonePane             // the right pane, header included
)

// hit says which part of the screen a cell is in. line is the body line (0 is
// the first line under the title bar), so callers can map it further.
type hit struct {
	zone zone
	line int
	x    int // the screen column, for forwarding a pointer position to a pane
}

func (m Model) hit(x, y int) hit {
	if m.width < minWidth || m.height < minHeight || x < 0 || x >= m.width {
		return hit{}
	}
	if m.zoomed() { // no list and no title bar: all of it is the pane
		if y < 0 || y >= m.height {
			return hit{}
		}
		return hit{zonePane, y, x}
	}
	leftW, _, bodyH := m.geometry()
	line := y - 1 // the title bar is line -1
	switch {
	case line < 0 || line >= bodyH:
		return hit{}
	case x < leftW:
		return hit{zoneList, line, x}
	case x == leftW: // separator
		return hit{}
	}
	return hit{zonePane, line, x}
}

// listRowAt is the index in m.rows of the worktree on a body line of the
// left list, or -1 when the line is a band, header or blank.
func (m Model) listRowAt(line int) int {
	leftW, _, bodyH := m.geometry()
	if it, ok := m.itemAtLine(leftW, bodyH, line); ok && it.kind == itemWorktree {
		return it.row
	}
	return -1
}

// paneRowAt is the row inside the right pane's terminal area on a body line,
// or -1 for the pane's own header lines. Today only tests need it; a pane
// that wants to forward clicks would start from here.
func (m Model) paneRowAt(line int) int {
	_, rows := m.paneSize()
	chrome := paneChrome
	if m.zoomed() {
		chrome = zoomChrome
	}
	if r := line - chrome; r >= 0 && r < rows {
		return r
	}
	return -1
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Prompts and help own the screen; a stray click must not act under them.
	// The lazygit popup takes every event, which means it ignores the mouse.
	if m.help || m.mode == modeNew || m.mode == modeSearch || m.mode == modeRename || m.mode == modeNameAgent || m.mode == modeSwitch || m.mode == modeDigest || m.mode == modeCommand || m.mode == modePopup {
		return m, nil
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelDown {
			return m.handleWheel(m.hit(mouse.X, mouse.Y), mouse.Button == tea.MouseWheelUp)
		}
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			return m.handleClick(m.hit(mouse.X, mouse.Y))
		}
	}
	return m, nil // releases and drags carry nothing we use
}

// handleWheel scrolls the scrollback over the right pane. Over the list it
// moves the selection (the list follows the cursor, so there is no separate
// list scroll position to move), a row per notch like the arrow keys.
func (m Model) handleWheel(h hit, up bool) (tea.Model, tea.Cmd) {
	switch h.zone {
	case zonePane:
		if next, handled := m.forwardWheel(h, up); handled {
			return next, nil // the program scrolls itself; see wheel.go
		}
		delta := wheelStep
		if !up {
			delta = -wheelStep
		}
		cmd := m.scrollBy(delta)
		return m, cmd
	case zoneList:
		if m.mode == modeFocus {
			return m, nil // keep the agent you are typing to
		}
		before := m.cursor
		if up {
			m.move(-1)
		} else {
			m.move(1)
		}
		if m.cursor == before {
			return m, nil
		}
		cmd := tea.Batch(m.startPreview(), m.readSelectedGit())
		return m, cmd
	}
	return m, nil
}

func (m Model) handleClick(h hit) (tea.Model, tea.Cmd) {
	if m.mode == modeFocus {
		return m, nil // already talking to the agent; clicks are not forwarded
	}
	// A click answers a pending "press again" like any other key would.
	m.status, m.notice, m.confirmAll, m.confirmRemove = "", "", false, ""
	switch h.zone {
	case zoneList:
		leftW, _, bodyH := m.geometry()
		it, ok := m.itemAtLine(leftW, bodyH, h.line)
		if !ok || it.isHeader() {
			return m.clickNonRow(h.line)
		}
		if cur, ok := m.current(m.items()); ok && cur.id == it.id {
			return m, nil // already on it (a worktree row or one of its children)
		}
		// moveTo also leaves a header the cursor may have been on.
		m.moveTo(it)
		m.scroll = scrollState{}
		cmd := tea.Batch(m.startPreview(), m.readSelectedGit())
		return m, cmd
	case zonePane:
		return m.focus()
	}
	return m, nil
}

// clickNonRow handles a click on an org band or a project or group header:
// it selects the header and toggles its fold, like enter does on one.
func (m Model) clickNonRow(line int) (tea.Model, tea.Cmd) {
	leftW, _, bodyH := m.geometry()
	it, ok := m.itemAtLine(leftW, bodyH, line)
	if !ok || !it.isHeader() {
		return m, nil
	}
	m.moveTo(it)
	m.scroll = scrollState{}
	return m.toggleHeader()
}
