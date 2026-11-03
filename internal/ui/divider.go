package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/layout"
	"github.com/lnware/drover/internal/status"
)

// Dragging the divider (the │ between the list and the pane) resizes the
// list. A press on it starts a drag, motion with the button held moves it,
// and the release saves the width (layout.json), so it survives a restart.
// Two presses in quick succession, with no drag between them, forget it: the
// list goes back to list_width from the config, or to the automatic width.
//
// While dragging only the split moves; the session is resized by the preview
// poll at its own pace and once more on release, so the agent is not sent a
// SIGWINCH for every column crossed.

// doubleClick is how close two presses on the divider must be to reset it.
const doubleClick = 400 * time.Millisecond

type dividerDrag struct {
	width    int       // the dragged list width; 0 when none is chosen
	active   bool      // the left button went down on the divider and is held
	lastDown time.Time // the previous press that did not drag, for the double-click
	// touched is set by any press on the divider, so a saved width that
	// loads late cannot undo what the user just did.
	touched bool
}

type layoutLoaded struct{ width int }

func loadLayout() tea.Cmd {
	return func() tea.Msg { return layoutLoaded{width: layout.Load(layout.Path(status.Root()))} }
}

// onDivider says whether a screen cell is the divider. Zoomed there is none.
func (m Model) onDivider(x, y int) bool {
	if m.zoomed() || m.width < minWidth || m.height < minHeight {
		return false
	}
	leftW, _, bodyH := m.geometry()
	return x == leftW && y >= 1 && y <= bodyH
}

func (m Model) pressDivider(now time.Time) (tea.Model, tea.Cmd) {
	m.clearPrompts()
	if now.Sub(m.drag.lastDown) < doubleClick {
		m.drag = dividerDrag{touched: true}
		m.saveListWidth(0)
		return m.relayout()
	}
	m.drag.active, m.drag.lastDown, m.drag.touched = true, now, true
	return m, nil
}

// dragDivider puts the divider in screen column x, which is the list width.
func (m Model) dragDivider(x int) (tea.Model, tea.Cmd) {
	w := clampListWidth(x, m.width)
	if leftW, _, _ := m.geometry(); w == leftW {
		return m, nil
	}
	m.drag.width = w
	m.drag.lastDown = time.Time{} // a press that dragged is not half a double-click
	return m, nil
}

func (m Model) releaseDivider() (tea.Model, tea.Cmd) {
	m.drag.active = false
	if m.drag.width == 0 {
		return m, nil
	}
	m.saveListWidth(m.drag.width)
	return m.relayout()
}

func (m *Model) saveListWidth(w int) {
	if err := layout.Save(layout.Path(status.Root()), w); err != nil {
		m.status = "list width not saved: " + err.Error()
	}
}

// relayout makes the pane follow the new split. Like toggleZoom: the next
// poll resizes the session, and scrollback (whose window is sized to the old
// pane) returns to live.
func (m Model) relayout() (tea.Model, tea.Cmd) {
	m.scroll = scrollState{}
	return m, m.startPreview()
}
