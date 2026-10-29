package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	zoomKeyMsg = tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl} // "ctrl+z", the default
	backKeyMsg = tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl}
)

func focused(m Model) Model {
	m.enterFocus()
	return m
}

func press1(t *testing.T, m Model, k tea.KeyPressMsg) Model {
	t.Helper()
	next, _ := m.handleFocusKey(k)
	return next.(Model)
}

func TestZoomFillsTheTerminalAndRestores(t *testing.T) {
	m := focused(scrollModel(t, 5))
	splitCols, splitRows := m.paneSize()

	z := press1(t, m, zoomKeyMsg)
	if !z.zoomed() {
		t.Fatal("the zoom key did not zoom")
	}
	if cols, rows := z.paneSize(); cols != z.width || rows != z.height-zoomChrome {
		t.Errorf("zoomed pane is %dx%d, want the whole terminal %dx%d", cols, rows, z.width, z.height-zoomChrome)
	}
	if cols, rows := z.paneSize(); cols <= splitCols || rows <= splitRows {
		t.Errorf("zoom did not enlarge the pane: %dx%d vs %dx%d in the split", cols, rows, splitCols, splitRows)
	}
	lines := strings.Split(z.render(), "\n")
	if len(lines) != z.height {
		t.Fatalf("zoomed view has %d lines, want %d", len(lines), z.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != z.width {
			t.Errorf("zoomed line %d is %d wide, want %d", i, w, z.width)
		}
	}
	if head := ansi.Strip(lines[0]); !strings.Contains(head, "ZOOM") || !strings.Contains(head, "ctrl+z restore") {
		t.Errorf("zoomed header does not say how to get out: %q", head)
	}
	if strings.Contains(ansi.Strip(z.render()), "D R O V E R") {
		t.Error("the list and title bar must be gone while zoomed")
	}
	if z.sized.cols != z.width || z.sized.rows != z.height-zoomChrome {
		t.Errorf("the pane was not queued for resize to the zoomed size: %+v", z.sized)
	}

	back := press1(t, z, zoomKeyMsg)
	if back.zoomed() {
		t.Error("the zoom key did not restore the split")
	}
	if cols, rows := back.paneSize(); cols != splitCols || rows != splitRows {
		t.Errorf("restored pane is %dx%d, want %dx%d", cols, rows, splitCols, splitRows)
	}
}

// The key must be consumed: an editor or shell that received ctrl+z would act (it suspends)
// on it (a tty echoes it as ^Z and stops the program, which is how this test sees it).
func TestZoomKeyIsNotForwardedToThePane(t *testing.T) {
	m := focused(scrollModel(t, 3))
	m = press1(t, m, zoomKeyMsg)
	m = press1(t, m, keyRunes("x"))
	s := m.rows[0].session
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := m.tmux.Capture(s)
		if err == nil && strings.Contains(strings.Join(c.Lines, "\n"), "x") {
			if all := strings.Join(c.Lines, "\n"); strings.Contains(all, "^Z") {
				t.Fatalf("the zoom key reached the pane:\n%s", all)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the key typed after zooming never arrived")
}

func TestZoomNeverOutlivesFocus(t *testing.T) {
	m := focused(testModel())
	m = press1(t, m, zoomKeyMsg)
	if !m.zoomed() {
		t.Fatal("setup: not zoomed")
	}
	m = press1(t, m, backKeyMsg) // ctrl+\ leaves focus, and with it the zoom
	if m.zoomed() || m.mode != modeList {
		t.Fatalf("after leaving focus: zoomed=%v mode=%d", m.zoomed(), m.mode)
	}
	m.enterFocus()
	if m.zoomed() {
		t.Error("a new focus must start in the split, not zoomed from last time")
	}
	// even a stale flag does nothing outside focus mode
	m.mode, m.zoom = modeList, true
	if m.zoomed() {
		t.Error("zoomed() must require focus mode")
	}
}

func TestZoomedViewIsExactlyTheTerminalSize(t *testing.T) {
	for _, sz := range [][2]int{{60, 12}, {80, 24}, {130, 40}, {200, 50}} {
		m := focused(testModel())
		m.width, m.height = sz[0], sz[1]
		m.zoom = true
		lines := strings.Split(m.render(), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%dx%d line %d is %d wide", sz[0], sz[1], i, w)
			}
		}
	}
}

func TestZoomedMouseMapsTheWholeScreenToThePane(t *testing.T) {
	m := focused(testModel())
	m.zoom = true
	_, rows := m.paneSize()
	for y := 0; y < m.height; y++ {
		h := m.hit(0, y) // x = 0 is the list column in the split; there is no list now
		if h.zone != zonePane {
			t.Fatalf("y=%d: zone %v, want the pane", y, h.zone)
		}
		want := y - zoomChrome
		if y < zoomChrome {
			want = -1
		}
		if got := m.paneRowAt(h.line); got != want {
			t.Errorf("y=%d: pane row %d, want %d", y, got, want)
		}
	}
	if last := m.paneRowAt(m.hit(0, m.height-1).line); last != rows-1 {
		t.Errorf("the last screen line is pane row %d, want %d", last, rows-1)
	}
	if h := m.hit(0, m.height); h.zone == zonePane {
		t.Error("a click below the screen hit the pane")
	}
}

func TestZoomKeyIsConfigurable(t *testing.T) {
	m := focused(testModel())
	m.cfg.ZoomKey = "f10"
	if z := press1(t, m, tea.KeyPressMsg{Code: tea.KeyF10}); !z.zoomed() {
		t.Error("the configured key did not zoom")
	}
	if z := press1(t, m, zoomKeyMsg); z.zoomed() {
		t.Error("the default key still zooms although another is configured")
	}
	if got := ansi.Strip(m.footer()); !strings.Contains(got, "f10 zoom") {
		t.Errorf("the footer does not name the configured key: %q", got)
	}
}

func TestZoomingLeavesScrollback(t *testing.T) {
	m := focused(scrollModel(t, 300))
	m = wheel(t, m, true)
	if !m.scrolled() {
		t.Fatal("setup: not scrolled back")
	}
	z := press1(t, m, zoomKeyMsg)
	if z.scrolled() {
		t.Error("scrollback (sized to the old pane) must return to live when the pane changes size")
	}
}
