package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// HistoryLimit is the scrollback kept per pane (tmux's history-limit). tmux's
// own default is 2000, about one long agent answer; 10000 reaches back a few
// hours of work. Lines are stored at their written width, not padded to the
// pane, so a full history costs a few MB per pane (see the README).
const HistoryLimit = 10000

// CaptureScroll reads the rows of a pane that sit off lines above the live
// screen: off 0 is the live screen, off 100 the screen as it was scrolled 100
// lines back. Only the rows being displayed are read, never the whole
// history, so the cost does not grow with it. hist is how many lines of
// history the pane holds; an off larger than that is clamped by tmux, which
// the caller notices by comparing off with hist.
//
// One invocation returns both, so tracking new output (hist growing while the
// view is scrolled) costs no extra process.
func (d Driver) CaptureScroll(name string, off, rows int) (c Capture, hist int, err error) {
	t := target(name)
	out, err := d.run(
		"display-message", "-p", "-t", t, "#{history_size}", ";",
		// Line 0 is the top of the live screen and history is negative, so
		// the window [-off, rows-1-off] is rows lines ending off above the end.
		"capture-pane", "-p", "-e", "-t", t,
		"-S", strconv.Itoa(-off), "-E", strconv.Itoa(rows-1-off),
	)
	if err != nil {
		return Capture{}, 0, err
	}
	lines := strings.Split(out, "\n")
	if _, err := fmt.Sscanf(lines[0], "%d", &hist); err != nil {
		return Capture{}, 0, fmt.Errorf("tmux history_size %q: %w", lines[0], err)
	}
	// Trailing blank rows were trimmed with the output's final newlines; the
	// renderer pads, so the capture is just short, not wrong.
	return Capture{Lines: lines[1:]}, hist, nil
}
