package ui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// iconSet is the glyph drawn at the start of a child row, so that an agent, a
// terminal and an editor can be told apart without reading the label. The
// unicode ones are plain one-cell symbols; the nerd ones need a Nerd Font
// (icons = "nerd").
type iconSet struct{ agent, terminal, editor string }

var (
	unicodeIcons = iconSet{agent: "◈", terminal: "$", editor: "✎"}
	nerdIcons    = iconSet{agent: "\U000F06A9", terminal: "", editor: ""}
)

func (m Model) icons() iconSet {
	if m.cfg.Icons == "nerd" {
		return nerdIcons
	}
	return unicodeIcons
}

// icon is the glyph of a companion view.
func (s iconSet) icon(v paneView) string {
	switch v {
	case viewTerminal:
		return s.terminal
	case viewEditor:
		return s.editor
	}
	return s.agent
}

// connector is the guide from a worktree to its child: thick, two cells wide, as in
// agent-manager's tree, so the children read as hanging off the worktree.
func connector(last bool) string {
	if last {
		return "╰━━"
	}
	return "┣━━"
}

// childStyles returns the plain style for a child row and a function that
// puts the selected row's background behind a styled segment.
func childStyles(selected bool) (lipgloss.Style, func(lipgloss.Style) lipgloss.Style) {
	if selected {
		return selectedStyle, func(s lipgloss.Style) lipgloss.Style { return s.Background(colSelected.color()) }
	}
	return lipgloss.NewStyle(), func(s lipgloss.Style) lipgloss.Style { return s }
}

// agentByID finds an agent of a row by its session.
func agentByID(r row, id string) (agent, bool) {
	for _, a := range r.agentList() {
		if a.session == id {
			return a, true
		}
	}
	return agent{}, false
}

// agentAge is how long ago the agent last changed state, or for one that has
// stopped, how long ago it was started; "" when neither is known.
func (m Model) agentAge(a agent, now time.Time) string {
	if a.running {
		if e, ok := m.agents[a.session]; ok && !e.Since.IsZero() {
			return age(e.Since, now)
		}
		return ""
	}
	if !a.created.IsZero() {
		return age(a.created, now)
	}
	return ""
}

// renderAgent draws an agent child row of w cells:
//
//	┣━━ ◈ feature-a                    working 2m
//
// The icon takes the state's colour, the name is bright while the agent runs
// and dim once it has stopped, and the FOCUS badge replaces the state on the
// selected row while it is focused.
func (m Model) renderAgent(it item, selected bool, w int) string {
	a, ok := agentByID(m.rows[it.row], it.agent)
	if !ok {
		return fit("", w)
	}
	plain, paint := childStyles(selected)
	d := m.agentDisplay(a)
	nameStyle := lipgloss.NewStyle().Foreground(colText.color())
	if !a.running {
		nameStyle = idleStyle
	}
	left := lead(plain, selected, 5) + paint(connectorStyle()).Render(connector(it.last)) + plain.Render(" ") +
		paint(d.style).Render(m.icons().agent) + plain.Render(" ") + paint(nameStyle).Render(a.name)

	var right string
	switch {
	case selected && m.mode == modeFocus && a.running:
		right = badgeStyle.Render(" FOCUS ")
	default:
		word := d.word
		if !a.running {
			word = "stopped"
		}
		right = paint(d.style).Render(word)
		if ag := m.agentAge(a, time.Now()); ag != "" {
			right += plain.Render(" ") + paint(dimStyle).Render(ag)
		}
	}
	return joinRow(left, right, plain, w)
}

// joinRow puts right at the end of a row of w cells, dropping it when the
// left side leaves no room for it, and pads or truncates to exactly w.
func joinRow(left, right string, plain lipgloss.Style, w int) string {
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		right = ""
	}
	pad := max(w-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	line := left + plain.Render(strings.Repeat(" ", pad)) + right + plain.Render(" ")
	if lipgloss.Width(line) > w {
		line = ansi.Truncate(line, w, "")
	}
	return line
}

// connectorStyle is bright and bold, so the thick guide stays visible on both
// the plain and the selected background; the dim separator colour vanished.
func connectorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colMuted.color()).Bold(true)
}
