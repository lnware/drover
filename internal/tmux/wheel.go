package tmux

import (
	"fmt"
	"strings"
)

// SendWheel delivers mouse wheel events to a program that asked for mouse
// reports, as the terminal would: tmux's own mouse handling is bypassed
// because these events come from drover, not from a terminal. col and row are
// 1-based positions inside the pane. n events go in one tmux call.
//
// The program must have requested mouse reporting (Capture.Mouse): sent to
// one that did not, the escape sequences would be typed into it as text.
// sgr says which encoding it asked for. The legacy encoding can only name
// positions up to 95 in a UTF-8 pane, so events beyond that are dropped.
func (d Driver) SendWheel(name string, up bool, n, col, row int, sgr bool) error {
	button := 65 // wheel down
	if up {
		button = 64
	}
	var one string
	switch {
	case sgr:
		one = fmt.Sprintf("\x1b[<%d;%d;%dM", button, col, row)
	case col+32 < 128 && row+32 < 128:
		one = fmt.Sprintf("\x1b[M%c%c%c", rune(32+button), rune(32+col), rune(32+row))
	default:
		return nil
	}
	_, err := d.run("send-keys", "-l", "-t", target(name), "--", strings.Repeat(one, max(n, 1)))
	return err
}
