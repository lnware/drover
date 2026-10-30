package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/status"
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
	if !ok { // on a header: the switcher opens where the cursor is
		if jump >= 0 {
			return m, nil
		}
		return m.openSwitcherAtHeader()
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
	m.switchScope, m.switchFilter = scopeViews, ""
	m.mode = modeSwitch
	return m, nil
}

// The popup has three scopes, switched with ← and →: the worktree's Views,
// every Worktree of every project (needing attention first), and the Projects.
// Views are picked by number; the other two take typed text as a filter.
const (
	scopeViews = iota
	scopeWorktrees
	scopeProjects
	scopeContexts
	scopeCount
)

var scopeNames = [scopeCount]string{"Views", "Worktrees", "Projects", "Contexts"}

// switchItem is one line of the popup.
type switchItem struct {
	text  string     // the plain label, also what the filter matches
	right string     // the styled state at the end of the line
	view  viewTarget // scopeViews
	row   string     // scopeWorktrees and scopeProjects: the session to go to
	ctx   string     // scopeContexts: the context's name
}

// projectKey names a project the way the list does: "Acme/infra", or just
// "infra" when it has no org.
func projectKey(r row) string { return strings.TrimPrefix(r.org+"/"+r.project, "/") }

// switchItems builds the lines of the current scope, with the filter applied.
func (m Model) switchItems() []switchItem {
	icons := m.icons()
	var items []switchItem
	switch m.switchScope {
	case scopeViews:
		r, ok := m.selected()
		if !ok {
			return nil
		}
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
			items = append(items, switchItem{text: fmt.Sprintf("%d %s %s", i+1, icons.icon(t.view), label), right: state, view: t})
		}
		return items
	case scopeWorktrees:
		rows := slices.Clone(m.contextRows())
		need := func(r row) int { // waiting, error and done come first, in list order within a rank
			if !r.running {
				return 0
			}
			s, ok := m.worktreeState(r)
			if ok && (s == status.Waiting || s == status.Error || s == status.Done) {
				return urgency(s, true)
			}
			return 0
		}
		slices.SortStableFunc(rows, func(a, b row) int { return need(b) - need(a) })
		for _, r := range rows {
			d := m.display(r)
			items = append(items, switchItem{
				text:  r.project + "  " + m.label(r),
				right: d.style.Render(strings.TrimSpace(d.glyph + " " + d.word)),
				row:   r.session,
			})
		}
	case scopeProjects:
		index := map[string]int{}
		count, waiting := map[string]int{}, map[string]int{}
		for _, r := range m.contextRows() {
			k := projectKey(r)
			if _, ok := index[k]; !ok {
				index[k] = len(items)
				items = append(items, switchItem{text: k, row: r.session})
			}
			count[k]++
			if s, ok := m.worktreeState(r); ok && r.running && s == status.Waiting {
				waiting[k]++
			}
		}
		for i := range items {
			k := items[i].text
			right := dimStyle.Render(fmt.Sprintf("%d worktrees", count[k]))
			if waiting[k] > 0 {
				right = waitingStyle.Render(fmt.Sprintf("◆ %d waiting", waiting[k])) + dimStyle.Render(" · ") + right
			}
			items[i].right = right
		}
	case scopeContexts:
		defs := m.contexts()
		cur := m.activeContext(defs)
		for i, d := range defs {
			right := dimStyle.Render(fmt.Sprintf("%d worktrees", len(m.contextRowsOf(defs, i))))
			if n := m.waitingIn(defs, i); n > 0 {
				right = waitingStyle.Render(fmt.Sprintf("◆ %d waiting", n)) + dimStyle.Render(" · ") + right
			}
			if i == cur {
				right = focusHint.Render("current") + dimStyle.Render(" · ") + right
			}
			items = append(items, switchItem{text: d.name, right: right, ctx: d.name})
		}
	}
	query := strings.ToLower(strings.TrimSpace(m.switchFilter))
	if query == "" {
		return items
	}
	return slices.DeleteFunc(items, func(it switchItem) bool {
		hay := strings.ToLower(it.text)
		for _, term := range strings.Fields(query) {
			if !strings.Contains(hay, term) {
				return true
			}
		}
		return false
	})
}

// openSwitcherAtHeader opens the popup on the scope the cursor's header
// stands for: Projects on a project or an org, Worktrees on a group, with the
// choice on that header's own entry.
func (m Model) openSwitcherAtHeader() (tea.Model, tea.Cmd) {
	it, ok := m.selectedHeader()
	if !ok {
		return m, nil
	}
	m.switchBack, m.switchFilter = m.mode, ""
	m.switchScope = scopeProjects
	if it.kind == itemGroup {
		m.switchScope = scopeWorktrees
	}
	m.mode = modeSwitch
	m.switchSel = 0
	for i, entry := range m.switchItems() {
		var here bool
		for _, r := range m.all {
			if r.session != entry.row {
				continue
			}
			switch it.kind {
			case itemGroup:
				here = groupID(r) == m.hdr
			case itemProject:
				here = projectID(r) == m.hdr
			default:
				here = orgID(r) == m.hdr
			}
		}
		if here {
			m.switchSel = i
			break
		}
	}
	return m, nil
}

// handleSwitchKey drives the open popup. ← and → change the scope; the switch
// key and ↑/↓ move the choice; enter picks; esc leaves. Nothing reaches the
// pane. Views are also picked by number, the other scopes filter on typing.
func (m Model) handleSwitchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.switchItems()
	key := msg.String()
	move := func(d int) {
		if n := len(items); n > 0 {
			m.switchSel = (m.switchSel + d + n) % n
		}
	}
	setScope := func(scope int) {
		m.switchScope = (scope + scopeCount) % scopeCount
		m.switchSel, m.switchFilter = 0, ""
		switch m.switchScope {
		case scopeViews:
			m.switchSel = m.viewStart()
		case scopeContexts:
			if defs := m.contexts(); defs != nil {
				m.switchSel = (m.activeContext(defs) + 1) % len(defs) // on the next one, as ":ctx" would go
			}
		}
	}
	switch {
	case key == "esc", key == "ctrl+c", key == `ctrl+\`, key == "ctrl+q":
		m.mode = m.switchBack
		return m, nil
	case key == "left":
		setScope(m.switchScope - 1)
	case key == "right":
		setScope(m.switchScope + 1)
	case key == "enter":
		if m.switchSel < len(items) {
			return m.pickItem(items[m.switchSel])
		}
	case key == m.switchKey(), key == "down", key == "tab", key == "ctrl+tab":
		move(1)
	case key == "shift+"+m.switchKey(), key == "up", key == "shift+tab", key == "ctrl+shift+tab":
		move(-1)
	case m.switchScope == scopeViews:
		i := jumpIndex(key)
		switch {
		case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
			i = int(key[0] - '1')
		case key == "j":
			move(1)
		case key == "k":
			move(-1)
		}
		if i >= 0 && i < len(items) {
			return m.pickItem(items[i])
		}
	default:
		m.switchFilter = editText(m.switchFilter, msg)
		m.switchSel = 0
	}
	return m, nil
}

// viewStart is the choice the Views scope opens on: the stop after the
// current one.
func (m Model) viewStart() int {
	r, ok := m.selected()
	if !ok {
		return 0
	}
	all := m.switchTargets(r, false)
	i := slices.Index(all, m.switchCurrent(r))
	if i < 0 || len(all) == 0 {
		return 0
	}
	return (i + 1) % len(all)
}

// pickItem closes the popup and goes where the item says.
func (m Model) pickItem(it switchItem) (tea.Model, tea.Cmd) {
	m.mode = m.switchBack
	if it.ctx != "" {
		defs := m.contexts()
		if i := slices.IndexFunc(defs, func(d ctxDef) bool { return d.name == it.ctx }); i >= 0 {
			return m.switchContextTo(defs, i)
		}
		return m, nil
	}
	if it.row != "" {
		return m.goToWorktree(it.row)
	}
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m.switchTo(r, it.view)
}

// goToWorktree puts the cursor on a worktree wherever it is: a filter that
// hides it is cleared and the sections that fold it are opened. Focus is kept
// when the agent there can take keys.
func (m Model) goToWorktree(session string) (tea.Model, tea.Cmd) {
	var target row
	found := false
	for _, r := range m.all {
		if r.session == session {
			target, found = r, true
		}
	}
	if !found {
		return m, nil
	}
	if m.filtering() {
		m.search, m.attention = "", false
		m.applyFilter()
	}
	next := m.withFold(orgID(target), false)
	delete(next, projectID(target))
	delete(next, groupID(target))
	if len(next) != len(m.folds) {
		m.setFolds(next)
	}
	i := slices.IndexFunc(m.rows, func(r row) bool { return r.session == session })
	if i < 0 {
		return m, nil
	}
	m.moveTo(item{kind: itemWorktree, id: session, row: i})
	m.scroll = scrollState{}
	if m.mode == modeFocus && !m.targetRunning(m.rows[i]) {
		m.mode = modeList
	}
	return m, tea.Batch(m.startPreview(), m.readSelectedGit())
}

// switcherBox is the switcher as a bordered popup, centred over the screen:
// the scope tabs, a filter line for the long scopes, the items with the choice
// marked, and a line of keys. spliceBox puts it over the frame.
func (m Model) switcherBox() []string {
	items := m.switchItems()
	var tabs []string
	for i, n := range scopeNames {
		if i == m.switchScope {
			tabs = append(tabs, focusHint.Render(n))
		} else {
			tabs = append(tabs, dimStyle.Render(n))
		}
	}
	rows := []string{" " + strings.Join(tabs, dimStyle.Render(" · ")), ""}
	if m.switchScope != scopeViews {
		rows = append(rows, " "+dimStyle.Render("filter ")+m.switchFilter+focusHint.Render("█"), "")
	}
	room := max(m.height-len(rows)-8, 3) // keeps the box on the screen
	start := max(min(m.switchSel-room/2, len(items)-room), 0)
	for i := start; i < min(start+room, len(items)); i++ {
		it := items[i]
		line := "   " + it.text + "  " + it.right
		if i == m.switchSel {
			line = focusHint.Render(" ▸ "+it.text) + "  " + it.right
		}
		rows = append(rows, line)
	}
	if len(items) == 0 {
		msg := "nothing matches"
		if _, ok := m.selected(); !ok && m.switchScope == scopeViews {
			msg = "select a worktree to see its views"
		}
		if m.switchScope == scopeContexts && m.contexts() == nil {
			msg = "only one context: add [[context]] entries to the config"
		}
		rows = append(rows, " "+dimStyle.Render(msg))
	}
	keys := " ←/→ scope · " + m.switchKey() + "/↑↓ choose · enter go · esc"
	if m.switchScope == scopeViews {
		keys = " ←/→ scope · " + m.switchKey() + "/↑↓ · enter or 1-9 go · esc"
	}
	keys = dimStyle.Render(keys)

	return popupBox(" Switch ", rows, keys, 44, m.width-2)
}

// popupBox draws rows in a bordered box with a title in the top edge and a
// line of keys at the bottom. It is as wide as its widest line, between
// minInner and maxInner cells inside the border. spliceBox puts it over a frame.
func popupBox(title string, rows []string, keys string, minInner, maxInner int) []string {
	inner := ansi.StringWidth(keys) + 1
	for _, row := range rows {
		inner = max(inner, ansi.StringWidth(row)+2)
	}
	inner = min(max(inner, minInner), maxInner)
	t := ansi.Truncate(title, max(inner-2, 0), "…")
	top := focusHint.Render("╭─") + projectStyle.Render(t) +
		focusHint.Render(strings.Repeat("─", max(inner-1-ansi.StringWidth(t), 0))+"╮")
	side := focusHint.Render("│")
	box := []string{top}
	lines := slices.Clone(rows)
	if keys != "" {
		lines = append(lines, "", keys)
	}
	for _, row := range lines {
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
	return m.spliceBoxAt(frame, box, max((m.width-w)/2, 0), max((m.height-len(box))/2, 0))
}

// spliceBoxAt draws box with its top left corner at cell (x, y) of a finished
// frame, keeping every line m.width cells wide.
func (m Model) spliceBoxAt(frame string, box []string, x, y int) string {
	w := 0
	for _, l := range box {
		w = max(w, ansi.StringWidth(l))
	}
	lines := strings.Split(frame, "\n")
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
