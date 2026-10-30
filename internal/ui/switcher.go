package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/tmux"
)

// The view switcher moves between what a worktree has to show: its agents,
// then the terminal and the editor. One key does it, in the list and in focus
// mode, so the keys never come in pairs: the configured switch key goes to the
// next view, and F1..F9 or ctrl+1..ctrl+9 go straight to the nth.

// viewTarget is one stop of the switcher: an agent (by its tmux session) or a
// companion view.
type viewTarget struct {
	view  paneView
	agent string // the agent's session, for viewAgent
}

// switchKey is the configured next-view key, or the default for a config
// built by hand.
func (m Model) switchKey() string {
	if k := m.cfg.SwitchKey; k != "" {
		return k
	}
	return config.DefaultSwitchKey
}

// switchTargets lists the stops of a worktree in order. With running set only
// those that exist count (what cycling visits); without it the terminal and
// the editor are listed even when not started, since a direct jump starts them.
func (m Model) switchTargets(r row, running bool) []viewTarget {
	var out []viewTarget
	for _, a := range r.agentList() {
		if !running || a.running {
			out = append(out, viewTarget{view: viewAgent, agent: a.session})
		}
	}
	for _, v := range companionViews {
		if !running || m.companions[tmux.CompanionName(v.kind(), r.session)] {
			out = append(out, viewTarget{view: v})
		}
	}
	return out
}

// switchCurrent is the stop the cursor is on.
func (m Model) switchCurrent(r row) viewTarget {
	if v := m.viewOf(r); v != viewAgent {
		return viewTarget{view: v}
	}
	a, _ := m.curAgent(r)
	return viewTarget{view: viewAgent, agent: a.session}
}

// jumpIndex is the position a direct-jump key stands for, or -1.
func jumpIndex(key string) int {
	for _, prefix := range []string{"f", "ctrl+"} {
		if rest, ok := strings.CutPrefix(key, prefix); ok && len(rest) == 1 && rest[0] >= '1' && rest[0] <= '9' {
			return int(rest[0] - '1')
		}
	}
	return -1
}

// switchKeyPressed reports what a key does for the switcher: step is +1 or -1
// for cycling, jump is the index for a direct jump, else ok is false.
func (m Model) switchKeyPressed(key string) (step, jump int, ok bool) {
	switch {
	case key == m.switchKey() || key == "ctrl+tab":
		return 1, -1, true
	case key == "ctrl+shift+tab" || key == "shift+"+m.switchKey():
		return -1, -1, true
	}
	if i := jumpIndex(key); i >= 0 {
		return 0, i, true
	}
	return 0, -1, false
}

// switchView handles a switcher key outside the overlay: a number jumps at
// once, the switch key opens the overlay on the next (or previous) stop.
func (m Model) switchView(step, jump int) (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || m.hdr != "" {
		return m, nil
	}
	all := m.switchTargets(r, false)
	if jump >= 0 {
		if jump >= len(all) {
			m.notice = "this worktree has no view " + string(rune('1'+jump))
			return m, nil
		}
		return m.switchTo(r, all[jump])
	}
	cur := m.switchCurrent(r)
	i := slices.Index(all, cur)
	if i < 0 {
		i = 0
	}
	m.switchBack, m.switchSel = m.mode, (i+step+len(all))%len(all)
	m.mode = modeSwitch
	return m, nil
}

// handleSwitchKey drives the open overlay: the switch key and the arrows move
// the choice, enter or a number picks, esc leaves it. Nothing reaches the pane.
func (m Model) handleSwitchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	all := m.switchTargets(r, false)
	if !ok || len(all) == 0 {
		m.mode = m.switchBack
		return m, nil
	}
	key := msg.String()
	move := func(d int) { m.switchSel = (m.switchSel + d + len(all)) % len(all) }
	switch {
	case key == "esc", key == "ctrl+c", key == `ctrl+\`, key == "ctrl+q":
		m.mode = m.switchBack
		return m, nil
	case key == "enter":
		return m.pickView(r, all[m.switchSel])
	case key == m.switchKey(), key == "down", key == "tab", key == "ctrl+tab", key == "j":
		move(1)
	case key == "shift+"+m.switchKey(), key == "up", key == "shift+tab", key == "ctrl+shift+tab", key == "k":
		move(-1)
	default:
		i := jumpIndex(key)
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			i = int(key[0] - '1')
		}
		if i >= 0 && i < len(all) {
			return m.pickView(r, all[i])
		}
	}
	return m, nil
}

// pickView closes the overlay and goes to the chosen stop.
func (m Model) pickView(r row, t viewTarget) (tea.Model, tea.Cmd) {
	m.mode = m.switchBack
	return m.switchTo(r, t)
}

// switcherBox is the switcher as a small bordered popup, centred over the
// screen: the views of the worktree, the choice marked, a line of keys below.
// It returns the box's lines and leaves the splice to spliceBox.
func (m Model) switcherBox() []string {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	icons := m.icons()
	var rows []string
	for i, t := range m.switchTargets(r, false) {
		label, state := t.view.label(), dimStyle.Render("not started")
		if t.view == viewAgent {
			a, _ := agentByID(r, t.agent)
			label = m.agentLabel(a)
			d := m.agentDisplay(a)
			state = d.style.Render(d.word)
			if !a.running {
				state = idleStyle.Render("stopped")
			}
		} else if m.companions[tmux.CompanionName(t.view.kind(), r.session)] {
			state = doneStyle.Render("running")
		}
		name := fmt.Sprintf("%d %s %s", i+1, icons.icon(t.view), label)
		if i == m.switchSel {
			rows = append(rows, focusHint.Render(" ▸ "+name)+"  "+state)
		} else {
			rows = append(rows, "   "+name+"  "+state)
		}
	}
	keys := dimStyle.Render(" " + m.switchKey() + "/↑↓ choose · enter go · esc cancel")
	inner := ansi.StringWidth(keys) + 1
	for _, row := range rows {
		inner = max(inner, ansi.StringWidth(row)+2)
	}
	inner = min(max(inner, 30), m.width-2)

	title := ansi.Truncate(" Switch view ", max(inner-2, 0), "…")
	top := focusHint.Render("╭─") + projectStyle.Render(title) +
		focusHint.Render(strings.Repeat("─", max(inner-1-ansi.StringWidth(title), 0))+"╮")
	side := focusHint.Render("│")
	box := []string{top}
	for _, row := range append(append(rows, ""), keys) {
		box = append(box, side+fit(row, inner)+"\x1b[0m"+side)
	}
	return append(box, focusHint.Render("╰"+strings.Repeat("─", inner)+"╯"))
}

// spliceBox draws box, centred, over a finished frame whose lines are exactly
// m.width cells wide, keeping that width.
func (m Model) spliceBox(frame string, box []string) string {
	if len(box) == 0 {
		return frame
	}
	w := 0
	for _, l := range box {
		w = max(w, ansi.StringWidth(l))
	}
	lines := strings.Split(frame, "\n")
	x, y := max((m.width-w)/2, 0), max((m.height-len(box))/2, 0)
	for i, bl := range box {
		if y+i >= len(lines) {
			break
		}
		l := lines[y+i]
		lines[y+i] = fit(ansi.Cut(l, 0, x), x) + "\x1b[0m" + fit(bl, w) + "\x1b[0m" +
			fit(ansi.Cut(l, x+w, m.width), max(m.width-x-w, 0))
	}
	return strings.Join(lines, "\n")
}

// switchTo puts the cursor on a stop, starting a companion that is not
// running yet. A stopped agent is selected but cannot take keys, so focus
// ends there, with the usual hint to resume it.
func (m Model) switchTo(r row, t viewTarget) (tea.Model, tea.Cmd) {
	if t.view == viewAgent {
		m.selectAgent(r.session, t.agent)
	} else {
		if !m.ensureCompanion(r, t.view) {
			return m, nil
		}
		m.setView(t.view)
	}
	m.scroll = scrollState{}
	if m.mode == modeFocus && !m.targetRunning(r) {
		m.mode = modeList
	}
	return m, tea.Batch(m.startPreview(), m.readSelectedGit())
}

// switcherStrip is the pane header's view list: "1 ◈ feature-a  2 $ terminal",
// the current stop highlighted. It is what the number keys refer to.
func (m Model) switcherStrip(r row) string {
	all := m.switchTargets(r, false)
	if len(all) < 2 {
		return ""
	}
	cur := m.switchCurrent(r)
	icons := m.icons()
	var parts []string
	for i, t := range all {
		label := t.view.label()
		running := m.companions[tmux.CompanionName(t.view.kind(), r.session)]
		if t.view == viewAgent {
			a, _ := agentByID(r, t.agent)
			label, running = m.agentLabel(a), a.running
		}
		part := string(rune('1'+i)) + " " + icons.icon(t.view) + " " + label
		switch {
		case t == cur:
			part = focusHint.Render(part)
		case running:
			part = dimStyle.Render(part)
		default:
			part = idleStyle.Render(part)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ")
}
