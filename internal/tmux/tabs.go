package tmux

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// tabWidth is the distance between tab stops, as in every terminal's default.
const tabWidth = 8

// expandTabs turns each tab of a captured line into the spaces that reach the
// next tab stop. tmux keeps a program's tab as a tab cell and capture-pane
// prints it as a real tab character; the UI measures and cuts lines by cell
// width and a tab has none, so "dig" columns ("IN\tTXT") would run together.
// Escape sequences take no width, so the column counts only what is shown.
func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var b strings.Builder
	col := 0
	state := byte(0)
	for len(line) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(line, state, nil)
		state = newState
		if seq == "\t" {
			pad := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		} else {
			b.WriteString(seq)
			col += width
		}
		line = line[n:]
	}
	return b.String()
}

func expandAll(lines []string) []string {
	for i, l := range lines {
		lines[i] = expandTabs(l)
	}
	return lines
}
