package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A hint is one entry of the footer: a key and what it does. The footer is
// one line, so when the hints do not all fit the least important ones are
// dropped, never the line cut off at the edge.
type hint struct {
	key, desc string // key may be empty: a plain remark such as "filtered"
	// prio says how long the hint survives a narrow terminal: 0 is never
	// dropped, and the larger the number the sooner it goes.
	prio int
}

const hintSep = " · "

// hintWidth is the width of the rendered line: a leading space, the label if
// any, and the hints with a separator between them.
func hintWidth(label string, hints []hint) int {
	w := 1 + ansi.StringWidth(label)
	for i, h := range hints {
		if i > 0 || label != "" {
			w += len(hintSep)
		}
		w += ansi.StringWidth(h.key + h.desc)
		if h.key != "" && h.desc != "" {
			w++
		}
	}
	return w
}

// fitHints drops the least important hints until the line fits width w, the
// later one first among equals, and keeps the rest in their order.
func fitHints(label string, hints []hint, w int) []hint {
	out := append([]hint(nil), hints...)
	for hintWidth(label, out) > w {
		drop := -1
		for i, h := range out {
			if h.prio > 0 && (drop < 0 || h.prio >= out[drop].prio) {
				drop = i
			}
		}
		if drop < 0 {
			break // only the never-dropped ones are left; fit() trims the line
		}
		out = append(out[:drop], out[drop+1:]...)
	}
	return out
}

// hintLine renders a footer of width w: an optional mode label, then the
// hints that fit, each key in the accent colour and its description dimmed.
func hintLine(label string, hints []hint, w int) string {
	hints = fitHints(label, hints, w)
	var b strings.Builder
	b.WriteString(" ")
	if label != "" {
		b.WriteString(focusHint.Render(label))
	}
	for i, h := range hints {
		if i > 0 || label != "" {
			b.WriteString(dimStyle.Render(hintSep))
		}
		if h.key != "" {
			b.WriteString(focusHint.Render(h.key))
			if h.desc != "" {
				b.WriteString(" ")
			}
		}
		b.WriteString(dimStyle.Render(h.desc))
	}
	return b.String()
}

var (
	hintHelp = hint{"?", "keys", 0}
	hintQuit = hint{"q", "quit", 0}
)

// listHints are the footer hints for the row the cursor is on. They follow
// what the row can actually do: a running agent can be killed but not
// resumed or removed, a stopped one the other way round.
func (m Model) listHints() []hint {
	switch {
	case m.hdr != "":
		return []hint{
			{"↑/↓", "move", 2}, {"enter", "fold", 1}, {"←/→", "collapse/expand", 3},
			{"F", "fold all", 5}, {"n", "new", 4}, {"/", "search", 4}, {"w", "attention", 6},
			hintHelp, hintQuit,
		}
	case m.sub != viewAgent:
		return []hint{
			{"↑/↓", "move", 2}, {"enter", "focus", 1}, {"←/h", "worktree", 3}, {"a", "agent", 4},
			{"x", "kill", 2}, {"T/o", "terminal/editor", 5}, {"G", "lazygit", 5}, {"A", "attach", 6},
			hintHelp, hintQuit,
		}
	}
	// The footer is for what you reach for every minute; the rarer keys
	// (fold arrows, w, r, M, A, X) live on the ? screen.
	r, ok := m.selected()
	if ok {
		if a, onChild := m.agentUnderCursor(r); onChild {
			if a.running {
				return []hint{
					{"↑/↓", "move", 2}, {"enter", "focus", 1}, {"N", "new agent", 3}, {"x", "stop", 2},
					{"r", "rename", 4}, {"T/o", "terminal/editor", 5}, {"G", "lazygit", 6},
					hintHelp, hintQuit,
				}
			}
			return []hint{
				{"↑/↓", "move", 2}, {"enter", "resume", 1}, {"N", "new agent", 3}, {"x", "forget", 2},
				{"r", "rename", 4}, {"T/o", "terminal/editor", 5},
				hintHelp, hintQuit,
			}
		}
	}
	if ok && r.running {
		return []hint{
			{"↑/↓", "move", 2}, {"enter", "focus", 1}, {"N", "new agent", 3}, {"n", "new", 4}, {"/", "search", 4},
			{"T/o", "terminal/editor", 3}, {"G", "lazygit", 5}, {"x", "stop", 2},
			hintHelp, hintQuit,
		}
	}
	return []hint{
		{"↑/↓", "move", 2}, {"enter", "start", 1}, {"v", "resume", 2}, {"N", "new agent", 3}, {"n", "new", 4},
		{"/", "search", 4}, {"T/o", "terminal/editor", 5}, {"d", "remove", 3},
		hintHelp, hintQuit,
	}
}
