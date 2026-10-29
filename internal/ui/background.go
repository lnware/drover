package ui

import (
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// Left alone, drover leaves every cell without a background, so a terminal
// with a background image or opacity shows through everywhere. The chrome
// (title bar, list, footer) is painted in the theme's background instead; the
// pane that shows an agent is left alone, since it is the agent's picture and
// not drover's.

// chromeBackground is the SGR that sets the chrome's background, or "" when
// it should not be painted: switched off, or a terminal without colour.
func (m Model) chromeBackground() string {
	if !m.cfg.Background || colorProfile < colorprofile.ANSI {
		return ""
	}
	return ansi.Style{}.BackgroundColor(colBackground.color()).String()
}

// paintBackground gives every cell of line that has no background of its own
// the colour bg. A reset puts the terminal's default background back, so each
// one is followed by bg again; a cell that sets a background itself (the
// selected row, a chip) sets it after the reset and keeps it. Widths do not
// change: only escape sequences are added.
func paintBackground(line, bg string) string {
	if bg == "" {
		return line
	}
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+bg)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg)
	line = strings.TrimSuffix(bg+line, bg) // a reset at the very end needs no colour after it
	if strings.HasSuffix(line, "\x1b[m") || strings.HasSuffix(line, "\x1b[0m") {
		return line
	}
	return line + "\x1b[m"
}
