package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/config"
)

// Zoom gives the focused pane the whole terminal: the list, the title bar and
// the footer disappear, one header line stays, and the pane is resized to
// fill the rest. It is the in-drover counterpart of `A` (a real tmux attach):
// it keeps scrollback, the tree and everything else that lives in drover, but
// the pane is still a captured view, so it has the same small lag as the split.
//
// It exists only while focused. enterFocus clears it, so every focus starts
// in the split, and zoomed() requires focus mode, so leaving focus by any
// route (ctrl+\, a pane that exits) ends it too.

// zoomChrome is the header the zoomed pane keeps above its terminal area.
const zoomChrome = 1

// zoomed holds while the view switcher is open over a zoomed pane, so opening
// it does not resize the pane underneath.
func (m Model) zoomed() bool {
	return m.zoom && (m.mode == modeFocus || (m.mode == modeSwitch && m.switchBack == modeFocus))
}

// zoomKey is the configured toggle, or the default for a config built by hand.
func (m Model) zoomKey() string {
	if k := m.cfg.ZoomKey; k != "" {
		return k
	}
	return config.DefaultZoomKey
}

// enterFocus starts focus mode in the split.
func (m *Model) enterFocus() {
	m.mode = modeFocus
	m.zoom = false
}

// toggleZoom switches between the split and the zoomed pane. The pane changes
// size, so it is resized at once, and scrollback (whose window is sized to the
// old pane) returns to live.
func (m Model) toggleZoom() (tea.Model, tea.Cmd) {
	m.zoom = !m.zoom
	m.scroll = scrollState{}
	m.sized = size{}
	return m, m.startPreview()
}

// zoomView is the whole screen while zoomed: the header, then the pane.
func (m Model) zoomView() string {
	cols, rows := m.paneSize()
	var lines []string
	r, ok := m.selected()
	if ok && m.preview.ok && m.preview.session == m.target(r) {
		lines = m.paneLines(cols, rows)
	} else { // right after toggling: the old-size capture is not for this size
		lines = make([]string, rows)
		if rows > 0 {
			lines[0] = dimStyle.Render(" connecting…")
		}
	}
	var b strings.Builder
	b.WriteString(fit(m.zoomHeader(r, ok), m.width) + "\x1b[0m")
	for _, l := range lines {
		b.WriteString("\n" + fit(l, cols) + "\x1b[0m")
	}
	return b.String()
}

// zoomHeader names the pane and says how to get out. While scrolled back the
// scrollback indicator (which also says how to return to live) takes the
// place of the hint.
func (m Model) zoomHeader(r row, ok bool) string {
	hint := focusHint.Render("ZOOM") + dimStyle.Render(" · "+m.zoomKey()+" restore · ctrl+\\ back")
	if s := m.scrollRule(); s != "" {
		hint = s
	}
	if !ok {
		return " " + hint
	}
	return " " + m.headerTitle(r) + "  " + m.paneState(r) + "  " + hint
}
