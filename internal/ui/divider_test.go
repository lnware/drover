package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/layout"
	"github.com/lnware/drover/internal/status"
)

func motion(x, y int) tea.MouseMsg {
	return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func release(x, y int) tea.MouseMsg {
	return tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func TestChosenListWidth(t *testing.T) {
	for _, c := range []struct {
		name                string
		width, cfg, dragged int
		want                int
	}{
		{"automatic", 120, 0, 0, listWidth(120)},
		{"config", 120, 50, 0, 50},
		{"drag beats config", 120, 50, 40, 40},
		{"negative config is automatic", 120, -3, 0, listWidth(120)},
		{"too narrow", 120, 5, 0, minListWidth},
		{"leaves the pane its minimum", 120, 200, 0, 120 - 1 - minPaneWidth},
		{"smallest window", minWidth, 0, 50, minWidth - 1 - minPaneWidth},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testModel()
			m.width, m.cfg.ListWidth, m.drag.width = c.width, c.cfg, c.dragged
			leftW, rightW, _ := m.geometry()
			if leftW != c.want || leftW+1+rightW != c.width {
				t.Errorf("geometry = %d | %d, want list %d of %d", leftW, rightW, c.want, c.width)
			}
		})
	}
}

// TestDragDivider drags the divider and checks the split follows, the width
// is saved on release, and a fresh model loads it back.
func TestDragDivider(t *testing.T) {
	m := mouseModel(t)
	manyRows(&m, 3)
	leftW, _, _ := m.geometry()

	// Motion without a press on the divider is not a drag.
	if got := step(m, motion(leftW+10, 5)); got.drag.width != 0 {
		t.Fatalf("motion alone moved the divider to %d", got.drag.width)
	}
	m = step(m, click(leftW, 5, tea.MouseLeft))
	if !m.drag.active || m.mode != modeList {
		t.Fatalf("press on the divider: active %v mode %d", m.drag.active, m.mode)
	}
	m = step(m, motion(leftW+10, 7))
	if got, _, _ := m.geometry(); got != leftW+10 {
		t.Errorf("dragged to %d, list is %d", leftW+10, got)
	}
	m = step(m, motion(m.width, 7)) // past the edge: the pane keeps its minimum
	if got, rightW, _ := m.geometry(); rightW != minPaneWidth {
		t.Errorf("drag to the edge left the pane %d wide (list %d)", rightW, got)
	}
	m = step(m, motion(leftW+10, 7))
	m = step(m, release(leftW+10, 7))
	if m.drag.active {
		t.Error("release did not end the drag")
	}
	if got := layout.Load(layout.Path(status.Root())); got != leftW+10 {
		t.Errorf("saved width %d, want %d", got, leftW+10)
	}
	// Motion after the release is not a drag.
	if got := step(m, motion(leftW+20, 7)); got.drag.width != leftW+10 {
		t.Errorf("motion after release moved the divider to %d", got.drag.width)
	}

	saved := status.Root()
	fresh := mouseModel(t)
	t.Setenv("DROVER_STATE_DIR", saved) // mouseModel gave it a new dir; read the saved one
	fresh = step(fresh, loadLayout()())
	if got, _, _ := fresh.geometry(); got != leftW+10 {
		t.Errorf("restart loaded list width %d, want %d", got, leftW+10)
	}
}

func TestDoubleClickDividerForgetsWidth(t *testing.T) {
	m := mouseModel(t)
	m.cfg.ListWidth = 50
	m.drag.width = 40
	if err := layout.Save(layout.Path(status.Root()), 40); err != nil {
		t.Fatal(err)
	}
	m.drag.lastDown = time.Now()
	m = step(m, click(40, 5, tea.MouseLeft))
	if m.drag != (dividerDrag{touched: true}) {
		t.Errorf("double-click left drag state %+v", m.drag)
	}
	if got, _, _ := m.geometry(); got != 50 {
		t.Errorf("after reset the list is %d, want list_width 50", got)
	}
	if got := layout.Load(layout.Path(status.Root())); got != 0 {
		t.Errorf("double-click left the saved width %d", got)
	}
	// The release that follows saves nothing.
	if m = step(m, release(50, 5)); layout.Load(layout.Path(status.Root())) != 0 {
		t.Error("release after the reset saved a width")
	}
	// A press long after the last one is a new drag, not a reset.
	m.drag.lastDown = time.Now().Add(-time.Second)
	if got := step(m, click(50, 5, tea.MouseLeft)); !got.drag.active {
		t.Error("a slow second press did not start a drag")
	}
}

func TestNoDividerWhenZoomed(t *testing.T) {
	m := mouseModel(t)
	leftW, _, _ := m.geometry()
	m.mode, m.zoom = modeFocus, true
	if m.onDivider(leftW, 5) {
		t.Error("the zoomed pane has no divider")
	}
}

// TestDividerDragNeverSticks covers the ways a drag could outlive the button:
// a release swallowed by a prompt a key opened mid-drag, a release that never
// arrived, and motion with no button held.
func TestDividerDragNeverSticks(t *testing.T) {
	press := func(t *testing.T) Model {
		m := mouseModel(t)
		manyRows(&m, 3)
		leftW, _, _ := m.geometry()
		return step(m, click(leftW, 5, tea.MouseLeft))
	}
	t.Run("release under a prompt", func(t *testing.T) {
		m := press(t)
		m.mode = modeCommand
		if m = step(m, release(3, 5)); m.drag.active {
			t.Error("a release under the command line left the drag active")
		}
	})
	t.Run("lost release", func(t *testing.T) {
		m := press(t)
		if m = step(m, click(3, 5, tea.MouseLeft)); m.drag.active {
			t.Error("a press off the divider left the drag active")
		}
		if got := step(m, motion(60, 5)); got.drag.width != 0 {
			t.Errorf("motion after a lost release moved the divider to %d", got.drag.width)
		}
	})
	t.Run("motion without the button", func(t *testing.T) {
		m := press(t)
		hover := tea.MouseMotionMsg(tea.Mouse{X: 60, Y: 5, Button: tea.MouseNone})
		if got := step(m, hover); got.drag.width != 0 {
			t.Errorf("hover moved the divider to %d", got.drag.width)
		}
	})
}

// A quick press right after a drag is a second grab to fine-tune, not a reset.
func TestRegrabAfterDragDoesNotReset(t *testing.T) {
	m := mouseModel(t)
	leftW, _, _ := m.geometry()
	m = step(m, click(leftW, 5, tea.MouseLeft))
	m = step(m, motion(leftW+8, 5))
	m = step(m, release(leftW+8, 5))
	m = step(m, click(leftW+8, 5, tea.MouseLeft))
	if m.drag.width != leftW+8 || !m.drag.active {
		t.Errorf("re-grab after a drag: width %d active %v, want %d and a new drag", m.drag.width, m.drag.active, leftW+8)
	}
}

// A saved width that loads after the user reset the divider must not return.
func TestLateLoadDoesNotUndoReset(t *testing.T) {
	m := mouseModel(t)
	leftW, _, _ := m.geometry()
	m = step(m, click(leftW, 5, tea.MouseLeft))
	m = step(m, click(leftW, 5, tea.MouseLeft)) // double-click: reset
	if m = step(m, layoutLoaded{width: 40}); m.drag.width != 0 {
		t.Errorf("a late load brought back width %d after a reset", m.drag.width)
	}
}
