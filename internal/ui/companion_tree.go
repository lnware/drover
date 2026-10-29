package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/tmux"
)

// A running terminal or editor is a child row under its worktree:
//
//	  ● main
//	     ├ terminal ●
//	     └ editor ●
//
// The worktree row is the agent. Which pane the right side shows is not a
// remembered choice but follows the cursor: m.sub is the child the cursor is
// on (viewAgent when it is on the worktree row itself), and viewOf reads it.
// Children are derived like every other item, from the live companion set, so
// a companion that exits simply vanishes and reselect puts the cursor back on
// its worktree. Not-running companions have no row.

// companionID is the item id of a worktree's child: the companion's tmux
// session name. It can equal neither a worktree session (those contain a "/")
// nor a fold id ("o:", "p:", "g:" prefixes), so ids stay unique.
func companionID(session string, v paneView) string {
	return tmux.CompanionName(v.kind(), session)
}

// companionRunning reports whether the worktree has a companion of that kind
// in the live set.
func (m Model) companionRunning(r row, v paneView) bool {
	return m.companions[companionID(r.session, v)]
}

// withChildren inserts each worktree's running companions right after it. A
// child inherits its worktree's folds (and so its filter membership: rows are
// already filtered), because it takes the worktree's `up` and `hidden`.
func withChildren(items []item, rows []row, running func(row, paneView) bool) []item {
	out := make([]item, 0, len(items)+4)
	for _, it := range items {
		out = append(out, it)
		if it.kind != itemWorktree {
			continue
		}
		var kids []paneView
		for _, v := range companionViews {
			if running(rows[it.row], v) {
				kids = append(kids, v)
			}
		}
		for i, v := range kids {
			out = append(out, item{
				kind: itemCompanion, id: companionID(it.id, v), up: it.up, root: it.root,
				row: it.row, hidden: it.hidden, view: v, last: i == len(kids)-1,
			})
		}
	}
	return out
}

// toParent is left on a child: the cursor goes to its worktree (the agent).
func (m Model) toParent(child item) (tea.Model, tea.Cmd) {
	m.scroll = scrollState{}
	m.moveTo(item{kind: itemWorktree, id: m.rows[child.row].session, row: child.row})
	return m.foldMoved()
}

// renderChild draws a companion child row of w cells: a light connector under
// the worktree's status glyph, the label and a running dot. The selected one
// takes the usual selected-row styling and the FOCUS badge.
func (m Model) renderChild(it item, selected bool, w int) string {
	plain := lipgloss.NewStyle()
	paint := func(s lipgloss.Style) lipgloss.Style { return s }
	if selected {
		plain = selectedStyle
		paint = func(s lipgloss.Style) lipgloss.Style { return s.Background(colSelected.color()) }
	}
	connector := "├"
	if it.last {
		connector = "└"
	}
	label := it.view.label()
	left := lead(plain, selected, 5) + paint(sepStyle).Render(connector) + plain.Render(" "+label+" ") + paint(doneStyle).Render("●")
	right := ""
	if selected && m.mode == modeFocus {
		right = badgeStyle.Render(" FOCUS ")
	}
	pad := max(w-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	line := left + plain.Render(strings.Repeat(" ", pad)) + right + plain.Render(" ")
	if lipgloss.Width(line) > w {
		line = ansi.Truncate(line, w, "")
	}
	return line
}
