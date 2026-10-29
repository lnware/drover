package ui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
)

// listLayout is the left list drawn in full, before scrolling. owner maps
// each line to the visible item it shows (an index into items), -1 for
// blanks and messages, so a line number can be turned back into an item:
// this is what a mouse click needs.
type listLayout struct {
	lines      []string
	owner      []int
	items      []item // the visible items, in display order
	cursorLine int
}

func (l *listLayout) add(line string, owner int) {
	l.lines = append(l.lines, line)
	l.owner = append(l.owner, owner)
}

// top is the first line shown in a window of h lines: just enough scrolling
// to keep the cursor visible.
func (l listLayout) top(h int) int {
	return min(max(l.cursorLine-h+1, 0), len(l.lines))
}

func (m Model) layout(w int) listLayout {
	var l listLayout
	if len(m.cfg.Projects) == 0 {
		l.add(" No projects.", -1)
		l.add(dimStyle.Render(" Pass project roots as"), -1)
		l.add(dimStyle.Render(" arguments or set `projects`"), -1)
		l.add(dimStyle.Render(" in "+config.Path()), -1)
	}
	l.items = visibleItems(m.items())
	cur, _ := m.current(l.items)
	for i, it := range l.items {
		var prev item
		if i > 0 {
			prev = l.items[i-1]
		}
		// Blank lines separate orgs and projects, but not a run of folded
		// ones: they exist to take less room.
		switch it.kind {
		case itemOrg:
			if len(l.lines) > 0 && !(i > 0 && prev.kind == itemOrg) {
				l.add("", -1)
			}
		case itemProject:
			if i > 0 && prev.kind != itemOrg && !(prev.kind == itemProject && prev.collapsed && it.collapsed) {
				l.add("", -1)
			}
		}
		selected := it.id == cur.id
		if selected {
			l.cursorLine = len(l.lines)
		}
		switch it.kind {
		case itemWorktree:
			l.add(m.renderRow(it.row, m.rows[it.row], w), i)
			continue
		case itemCompanion:
			l.add(m.renderChild(it, selected, w), i)
			continue
		}
		l.add(m.renderHeader(it, selected && m.hdr != "", w), i)
	}
	if len(m.rows) == 0 && len(m.all) > 0 {
		l.add(dimStyle.Render(" no matches"), -1)
	}
	for _, e := range m.errs {
		l.add("", -1)
		l.add(errStyle.Render(" "+e), -1)
	}
	return l
}

// window cuts the layout to h lines, padding with blanks.
func (l listLayout) window(h int) []string {
	lines := l.lines[l.top(h):]
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// itemAtLine says which item the y-th line of the left pane (counted from the
// pane's top, after scrolling) shows.
func (m Model) itemAtLine(w, h, y int) (item, bool) {
	l := m.layout(w)
	i := l.top(h) + y
	if y < 0 || i >= len(l.owner) || l.owner[i] < 0 {
		return item{}, false
	}
	return l.items[l.owner[i]], true
}

// foldMarker is the disclosure triangle of a header.
func foldMarker(collapsed bool) string {
	if collapsed {
		return "▸"
	}
	return "▾"
}

// summaryPart is one piece of a folded header's summary.
type summaryPart struct {
	text  string
	style lipgloss.Style
}

// summaryParts says what a collapsed section hides: the worktree count, then
// the agent states that want a look, most urgent first so that truncation
// drops the least important last.
func summaryParts(t tally) []summaryPart {
	noun := "worktrees"
	if t.total == 1 {
		noun = "worktree"
	}
	parts := []summaryPart{{fmt.Sprintf("%d %s", t.total, noun), dimStyle}}
	for _, s := range []struct {
		n     int
		glyph string
		word  string
		style lipgloss.Style
	}{
		{t.waiting, "◆", "waiting", waitingStyle},
		{t.working, "●", "working", workingStyle},
		{t.failed, "✗", "error", errStyle},
		{t.done, "✓", "done", doneStyle},
	} {
		if s.n > 0 {
			parts = append(parts, summaryPart{fmt.Sprintf("%s %d %s", s.glyph, s.n, s.word), s.style})
		}
	}
	return parts
}

// renderSummary joins the parts with dim separators in at most w cells; bg,
// when set, is the row's background so the summary does not punch holes in it.
// When it does not fit, the least urgent states go first, then the count, so
// a waiting agent is the last thing a narrow list stops showing.
func renderSummary(parts []summaryPart, w int, bg color.Color) string {
	paint := func(s lipgloss.Style) lipgloss.Style {
		if bg != nil {
			return s.Background(bg)
		}
		return s
	}
	join := func(ps []summaryPart) string {
		var b strings.Builder
		for i, p := range ps {
			if i > 0 {
				b.WriteString(paint(dimStyle).Render(" · "))
			}
			b.WriteString(paint(p.style).Render(p.text))
		}
		return b.String()
	}
	if len(parts) == 0 || w < 2 {
		return ""
	}
	var tries [][]summaryPart
	for n := len(parts); n >= 2; n-- { // the count plus the n-1 most urgent states
		tries = append(tries, parts[:n])
	}
	for n := len(parts); n >= 2; n-- { // states only
		tries = append(tries, parts[1:n])
	}
	tries = append(tries, parts[:1])
	for _, ps := range tries {
		if s := join(ps); ansi.StringWidth(s) <= w {
			return s
		}
	}
	// Nothing fits whole: cut the most urgent piece (or the count) short.
	first := min(1, len(parts)-1)
	return ansi.Truncate(join(parts[first:first+1]), w, "…")
}

// renderHeader draws an org, project or group header on one line of w cells.
// Folded ones carry the summary of what they hide.
func (m Model) renderHeader(it item, selected bool, w int) string {
	if it.kind == itemOrg {
		return orgLine(it, selected, w)
	}
	var bg color.Color
	plain := lipgloss.NewStyle()
	if selected {
		bg, plain = colSelected.color(), selectedStyle
	}
	paint := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(bg)
		}
		return s
	}
	indent, name := " ", projectStyle
	if it.kind == itemGroup {
		indent, name = "   ", groupStyle
	}
	line := lead(plain, selected, len(indent)) + paint(dimStyle).Render(foldMarker(it.collapsed)) + plain.Render(" ") + paint(name).Render(it.label)
	if it.collapsed {
		if room := w - ansi.StringWidth(line) - 3; room > 0 {
			line += plain.Render("  ") + renderSummary(summaryParts(it.sum), room, bg)
		}
	}
	line = ansi.Truncate(line, w, "")
	return line + plain.Render(strings.Repeat(" ", max(w-ansi.StringWidth(line), 0)))
}

// orgLine is the org band. A folded band shows its summary right-aligned on
// a dark chip, because the state colours would not read on the blue fill.
func orgLine(it item, selected bool, w int) string {
	fill := orgStyle
	if selected {
		// the band turns dark with blue text, so the cursor is still visible
		fill = lipgloss.NewStyle().Bold(true).Foreground(colBand.color()).Background(colSelected.color())
	}
	text := " " + foldMarker(it.collapsed) + " " + it.label
	// lead's bar takes the first cell, the one the band leaves blank
	bar, text := lead(fill, selected, 1), strings.TrimPrefix(text, " ")
	chipW := w - 1 - ansi.StringWidth(text) - 3 // a gap, a pad on each side, the band's edge
	if !it.collapsed || chipW < 4 {
		return bar + fill.Render(fit(text, w-1))
	}
	left := bar + fill.Render(text)
	chip := renderSummary(summaryParts(it.sum), chipW, colInk.color())
	pad := lipgloss.NewStyle().Background(colInk.color())
	chip = pad.Render(" ") + chip + pad.Render(" ")
	gap := max(w-ansi.StringWidth(left)-ansi.StringWidth(chip)-1, 1)
	return left + fill.Render(strings.Repeat(" ", gap)) + chip + fill.Render(" ")
}

// headerLines is the right pane while the cursor is on a header: what the
// section holds, so a folded one can be judged without opening it.
func (m Model) headerLines(it item, w, h int) []string {
	kind := map[itemKind]string{itemOrg: "org", itemProject: "project", itemGroup: "group"}[it.kind]
	state := dimStyle.Render("expanded")
	if it.collapsed {
		state = dimStyle.Render("collapsed")
	}
	rule := sepStyle.Render(strings.Repeat("─", w))
	if m.mode == modeFocus {
		rule = focusHint.Render("focused · ctrl+\\ / ctrl+q back")
	}
	lines := []string{
		" " + projectStyle.Render(it.label) + dimStyle.Render("  "+kind+" · ") + state,
		" " + renderSummary(summaryParts(it.sum), w-1, nil),
		"",
		rule,
	}
	switch {
	case m.mode == modeNew:
		lines = append(lines, m.newLines()...)
	case m.help:
		lines = append(lines, m.helpLines()...)
	default:
		lines = append(lines, "")
		for _, c := range m.items() {
			if c.kind != itemWorktree || !slices.Contains(c.up, it.id) {
				continue
			}
			r := m.rows[c.row]
			d := m.display(r)
			name := m.label(r)
			if it.kind == itemOrg {
				name = r.project + " · " + name
			} else if it.kind == itemProject {
				name = r.wt.Group + " · " + name
			}
			lines = append(lines, " "+d.style.Render(d.glyph)+" "+name+"  "+d.style.Render(d.word))
		}
		hint := "enter fold/unfold · ←/→ collapse/expand"
		if it.kind != itemOrg {
			hint += " · n new worktree here"
		}
		lines = append(lines, "", " "+dimStyle.Render(hint))
	}
	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}
