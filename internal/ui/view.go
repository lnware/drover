package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minWidth, minHeight = 60, 12
	// paneChrome is the right pane's own header: title, git bar, dir, hint/rule.
	paneChrome = 4
)

const (
	// The list is 30% of the window, but never narrower than minListWidth nor
	// wider than listWidthCap; the cap is what a 256-column laptop screen
	// reaches. Past wideFrom columns it keeps growing, by a quarter of every
	// extra column, so a wide screen does not strand it at the left edge with
	// all the new room going to the agent. maxListWidth stops that.
	minListWidth = 26
	listWidthCap = 46
	wideFrom     = 256
	maxListWidth = 90
)

// listWidth is the width of the list for a window total columns wide.
func listWidth(total int) int {
	w := min(max(total*3/10, minListWidth), listWidthCap)
	if total > wideFrom {
		w += (total - wideFrom) / 4
	}
	return min(w, maxListWidth)
}

// geometry splits the screen: left list, one separator column, right pane.
func (m Model) geometry() (leftW, rightW, bodyH int) {
	leftW = listWidth(m.width)
	return leftW, m.width - leftW - 1, m.height - 2 // title bar and footer
}

// paneSize is the terminal area inside the right pane, which is also the
// size the agent's tmux window is resized to.
func (m Model) paneSize() (cols, rows int) {
	if m.zoomed() {
		return m.width, m.height - zoomChrome
	}
	_, rightW, bodyH := m.geometry()
	return rightW, bodyH - paneChrome
}

// View is what Bubble Tea draws: the rendered screen plus the terminal modes
// drover wants. v2 takes those as fields here instead of program options.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	if m.cfg.Mouse {
		// Cell motion reports presses, releases and drags, which is all
		// clicks and the wheel need; all-motion would stream every move.
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// render draws the whole screen as a string of exactly width x height cells.
func (m Model) render() string {
	frame := m.renderBase()
	if m.width >= minWidth && m.height >= minHeight {
		switch m.mode {
		case modeSwitch:
			return m.spliceBox(frame, m.switcherBox())
		case modeDigest:
			return m.spliceBox(frame, m.digestBox())
		case modePR:
			return m.spliceBox(frame, m.prBox())
		case modeCommand:
			box := m.commandBox()
			return m.spliceBoxAt(frame, box, 0, max(m.height-1-len(box), 1))
		}
	}
	return frame
}

func (m Model) renderBase() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("drover needs at least %dx%d, have %dx%d", minWidth, minHeight, m.width, m.height)
	}
	if m.zoomed() {
		return m.zoomView()
	}
	leftW, rightW, bodyH := m.geometry()
	left := m.leftLines(leftW, bodyH)
	right := m.rightLines(rightW, bodyH)
	sep := sepStyle.Render("│")

	bg := m.chromeBackground()
	var b strings.Builder
	b.WriteString(paintBackground(m.titleBar(), bg) + "\n")
	for i := 0; i < bodyH; i++ {
		// The reset after each right line stops pane colors bleeding into
		// the next row; it adds no width. The pane itself stays unpainted.
		b.WriteString(paintBackground(fit(left[i], leftW)+sep, bg) + fit(right[i], rightW) + "\x1b[0m\n")
	}
	b.WriteString(paintBackground(m.footer(), bg))
	if m.popup.open {
		return m.overlayPopup(b.String())
	}
	return b.String()
}

// fit truncates or pads an ANSI string to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func (m Model) titleBar() string {
	working, waiting, running := m.counts()
	left := titleStyle.Render(" D R O V E R")
	if tabs := m.contextTabs(); tabs != "" {
		left += "  " + tabs
	}
	right := dimStyle.Render(fmt.Sprintf("%d running · ", running))
	if waiting > 0 {
		right += waitingStyle.Render(fmt.Sprintf("◆ %d waiting", waiting)) + dimStyle.Render(" · ")
	}
	if working > 0 {
		right += workingStyle.Render(fmt.Sprintf("● %d working", working)) + dimStyle.Render(" · ")
	}
	if n := m.reviewCount(); n > 0 {
		right += doneStyle.Render(fmt.Sprintf("✎ %d to review", n)) + dimStyle.Render(" · ")
	}
	if m.filtering() {
		right += dimStyle.Render(fmt.Sprintf("%d of %d worktrees ", len(m.rows), len(m.contextRows())))
	} else {
		right += dimStyle.Render(fmt.Sprintf("%d worktrees ", len(m.contextRows())))
	}
	if f := m.filterSummary(); f != "" {
		right = focusHint.Render(f) + dimStyle.Render(" · ") + right
	}
	gap := max(m.width-ansi.StringWidth(left)-ansi.StringWidth(right), 1)
	return fit(left+strings.Repeat(" ", gap)+right, m.width)
}

func (m Model) footer() string {
	w := m.width
	var s string
	switch {
	case m.mode == modePopup:
		s = hintLine("LAZYGIT", []hint{{"ctrl+\\ / ctrl+q", "close", 1}, {"", "every other key goes to lazygit", 2}}, w)
	case m.status != "":
		s = errStyle.Render(" " + m.status)
	case m.notice != "":
		s = doneStyle.Render(" " + m.notice)
	case m.mode == modeSearch:
		s = hintLine("SEARCH /"+m.search+"█", []hint{{"enter", "keep filter", 1}, {"esc", "clear", 1}, {"↑/↓", "move", 2}}, w)
	case m.mode == modeNameAgent:
		s = hintLine("NEW AGENT", []hint{{"enter", "start (empty: suggested name)", 1}, {"esc", "cancel", 1}, {"ctrl+u", "clear", 2}}, w)
	case m.mode == modePR:
		s = hintLine("PULL REQUEST", []hint{{"any key", "close", 1}}, w)
	case m.mode == modeCommand:
		s = m.commandLine()
	case m.mode == modeDigest:
		s = hintLine("CONTEXT", []hint{{"any key", "close", 1}}, w)
	case m.mode == modeSwitch:
		s = hintLine("SWITCH", []hint{{"←/→", "views · worktrees · projects", 1}, {m.switchKey(), "next", 1}, {"enter", "go", 1}, {"esc", "cancel", 1}}, w)
	case m.mode == modeRename:
		s = hintLine("RENAME", []hint{{"enter", "save (empty clears)", 1}, {"esc", "cancel", 1}, {"ctrl+u", "clear", 2}}, w)
	case m.mode == modeMoveBranch:
		s = hintLine("RENAME BRANCH", []hint{{"enter", "rename and move", 1}, {"esc", "cancel", 1}, {"ctrl+u", "clear", 2}}, w)
	case m.filtering():
		s = hintLine("", []hint{{"", "filtered", 3}, {"esc", "clears", 1}, {"/", "search", 3}, {"w", "attention", 4}, {"enter", "focus", 2}, hintHelp, hintQuit}, w)
	case m.mode == modeNew:
		s = hintLine("NEW WORKTREE", []hint{{"enter", "create", 1}, {"esc", "cancel", 1}, {"ctrl+u", "clear", 2}}, w)
	case m.mode == modeFocus:
		s = hintLine("FOCUS", []hint{{"ctrl+\\ / ctrl+q", "back", 1}, {m.switchKey(), "switch view", 1}, {"f1-f9", "go to view", 2}, {m.zoomKey(), "zoom", 3}, {"", "every other key goes to the pane", 4}}, w)
	default:
		hints := append(m.listHints(), hint{"tab", "next needing you", 3}, hint{":", "command", 4})
		s = hintLine("", hints, w)
	}
	return fit(s, w)
}

func (m Model) leftLines(w, h int) []string { return m.layout(w).window(h) }

func (m Model) renderRow(i int, r row, w int) string {
	d := m.display(r)
	label := m.label(r)
	// a selected child (a companion, or an agent row) takes the highlight
	selected := i == m.cursor && m.hdr == "" && m.sub == viewAgent && m.subAgent == ""
	bg := colSelected.color()

	// The state word goes right-aligned when it fits; the focus badge takes
	// its place on the selected row.
	glyph, word := d.style, d.style
	if selected {
		glyph, word = glyph.Background(bg), word.Background(bg)
	}
	plain := lipgloss.NewStyle()
	if selected {
		plain = selectedStyle
	}
	left := lead(plain, selected, 5) + glyph.Render(d.glyph) + plain.Render(" "+label)
	used := 5 + 1 + 1 + lipgloss.Width(label)
	var right string
	switch {
	case selected && m.mode == modeFocus:
		right = badgeStyle.Render(" FOCUS ")
	case d.word != "" && used+2+len(d.word) < w:
		right = word.Render(d.word)
	}
	// Git markers sit left of the state word, only where they still fit.
	{
		var rowBg color.Color
		if selected {
			rowBg = bg
		}
		var brief string
		if st, ok := m.git[r.session]; ok {
			brief = gitBrief(st, rowBg)
		}
		if chip, partial := reviewChip(m.git[r.session]); chip != "" {
			chipStyle, partialStyle := doneStyle, waitingStyle
			if selected {
				chipStyle, partialStyle = chipStyle.Background(bg), partialStyle.Background(bg)
			}
			styled := chipStyle.Render(chip)
			if partial != "" {
				styled += plain.Render(" ") + partialStyle.Render(partial)
			}
			// both when they fit; else the git bar as before (it carries the
			// same counts); else the chip alone
			candidates := []string{styled}
			if brief != "" {
				candidates = []string{styled + plain.Render(" ") + brief, brief, styled}
			}
			for _, c := range candidates {
				sep := ""
				if right != "" {
					sep = plain.Render(" ")
				}
				if used+2+lipgloss.Width(c)+lipgloss.Width(sep)+lipgloss.Width(right) < w {
					right = c + sep + right
					break
				}
			}
		} else if brief != "" {
			sep := ""
			if right != "" {
				sep = plain.Render(" ")
			}
			if used+2+lipgloss.Width(brief)+lipgloss.Width(sep)+lipgloss.Width(right) < w {
				right = brief + sep + right
			}
		}
	}
	pad := max(w-used-lipgloss.Width(right)-1, 1)
	line := left + plain.Render(strings.Repeat(" ", pad)) + right + plain.Render(" ")
	if lipgloss.Width(line) > w {
		line = ansi.Truncate(line, w, "")
	}
	return line
}

func (m Model) rightLines(w, h int) []string {
	blank := strings.Repeat(" ", w)
	r, ok := m.selected()
	if it, onHeader := m.selectedHeader(); onHeader {
		return m.headerLines(it, w, h)
	}
	if !ok {
		out := make([]string, h)
		for i := range out {
			out[i] = blank
		}
		out[0] = dimStyle.Render(" nothing to show")
		return out
	}

	state := m.paneState(r)
	rule := m.paneRule(w)
	if s := m.scrollRule(); s != "" { // the scrollback indicator replaces the rule while scrolled
		rule = s
	}
	gitLine := ""
	if st, ok := m.git[r.session]; ok {
		gitLine = " " + gitBar(st, gitIconsFor(m.cfg.Icons))
	}
	lines := []string{
		" " + m.headerTitle(r) + "  " + state,
		gitLine,
		" " + dimStyle.Render("dir  "+r.wt.Path),
		rule,
	}

	body := h - paneChrome
	switch {
	case m.mode == modeNew:
		lines = append(lines, m.newLines()...)
	case m.mode == modeRename:
		lines = append(lines, m.renameLines()...)
	case m.mode == modeNameAgent:
		lines = append(lines, m.nameAgentLines()...)
	case m.mode == modeMoveBranch:
		lines = append(lines, m.renameBranchLines()...)
	case m.help:
		lines = append(lines, m.helpLines()...)
	case !m.targetRunning(r):
		lines = append(lines, "", " "+dimStyle.Render(m.notRunningHint(r)))
	case !m.preview.ok || m.preview.session != m.target(r):
		lines = append(lines, "", " "+dimStyle.Render("connecting…"))
	default:
		lines = append(lines, m.paneLines(w, body)...)
	}
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

func (m Model) helpLines() []string {
	rows := [][2]string{
		{"↑/k  ↓/j", "move"},
		{"enter", "focus the agent or the terminal/editor row (starts the agent if needed); on a header, fold it"},
		{"←/h  →/l", "collapse / expand: group, project, org; on a terminal/editor row, back to its worktree"},
		{"F", "fold or unfold everything"},
		{"ctrl+\\ / ctrl+q", "leave focus mode"},
		{m.zoomKey(), "zoom the focused pane to the whole terminal, and back"},
		{"V", "review the changes in lazygit (also :review); :pr shows what a pull request would hold"},
		{"tab / shift+tab", "go to the next / previous agent that waits, finished or failed in this context (the list)"},
		{":", "command line: ctx (switch context), wt (jump to a worktree), term, edit, new, config, reload, help, q"},
		{m.switchKey(), "open the switcher: ←/→ views of this worktree, every worktree, every project (list and focus mode)"},
		{"f1-f9 / ctrl+1-9", "go straight to view 1-9, starting the terminal or editor if needed"},
		{"PgUp PgDn", "scroll output back (list: also ctrl+u/d)"},
		{"End q esc", "back to live output"},
		{"mouse", "click selects/focuses, wheel scrolls"},
		{"n", "new worktree in this project"},
		{"N", "new agent session in this worktree: asks for a name (empty: named after the branch)"},
		{"/", "search; filters as you type (enter keeps, esc clears)"},
		{"w", "show only agents waiting, done or in error"},
		{"r", "rename: an agent's name, or a worktree's display label"},
		{"M", "rename the branch and move the folder (stopped sessions only)"},
		{"esc", "clear the search and attention filter"},
		{"v", "resume the stopped agent under the cursor with its own conversation"},
		{"d", "remove the worktree (press twice, keeps the branch)"},
		{"T  o", "start a terminal / editor: a child row under the worktree, focused"},
		{"G", "lazygit popup (ctrl+\\ / ctrl+q closes and ends it)"},
		{"a", "back to the agent (the worktree row)"},
		{"A", "attach the shown pane full screen (C-a d returns)"},
		{"x", "stop what the cursor is on (agent, terminal or editor); on a stopped agent, forget it"},
		{"X", "kill all sessions (press twice)"},
		{"c", "edit the config in $VISUAL / $EDITOR; reloads when it exits"},
		{"R", "reload the config (theme, projects, icons, commands; socket and mouse need a restart)"},
		{"?", "this help"},
		{"q", "quit; sessions keep running"},
	}
	out := []string{"", " " + titleStyle.Render("Keys")}
	for _, r := range rows {
		out = append(out, fmt.Sprintf("  %-18s %s", r[0], dimStyle.Render(r[1])))
	}
	return out
}

// previewLines renders a capture into exactly rows lines of cols cells.
func previewLines(p preview, cols, rows int, showCursor bool) []string {
	lines := p.cap.Lines
	cursorY := p.cap.CursorY
	if extra := len(lines) - rows; extra > 0 { // window not yet resized: keep the bottom
		lines = lines[extra:]
		cursorY -= extra
	}
	out := make([]string, 0, rows)
	for i, l := range lines {
		l = fit(l, cols)
		if showCursor && p.cap.CursorVisible && i == cursorY {
			l = withCursor(l, p.cap.CursorX, cols)
		}
		out = append(out, l)
	}
	for len(out) < rows {
		out = append(out, strings.Repeat(" ", cols))
	}
	return out
}

// withCursor draws a reverse-video block over one cell of a padded line.
func withCursor(line string, x, w int) string {
	if x < 0 || x >= w {
		return line
	}
	cell := ansi.Strip(ansi.Cut(line, x, x+1))
	if cell == "" {
		cell = " "
	}
	return ansi.Cut(line, 0, x) + "\x1b[7m" + cell + "\x1b[27m" + ansi.Cut(line, x+1, w)
}

// newLines is the new-worktree prompt shown in the right pane.
func (m Model) newLines() []string {
	project := filepath.Base(m.newRoot)
	path := filepath.Join(m.newRoot, filepath.FromSlash(strings.TrimSpace(m.input)))
	lines := []string{
		"",
		" " + titleStyle.Render("New worktree in "+project),
		"",
		" branch  " + m.input + focusHint.Render("█"),
		" base    " + dimStyle.Render(m.newBase),
		" path    " + dimStyle.Render(path),
	}
	if hint := m.groupPrefixes(m.newRoot); len(hint) > 0 {
		lines = append(lines, "", " "+dimStyle.Render("prefixes in use: "+strings.Join(hint, "  ")))
	}
	lines = append(lines, "", " "+dimStyle.Render("Nothing is pushed; upstream is set so a later `git push` works."))
	if m.busy {
		lines = append(lines, "", " "+dimStyle.Render("creating…"))
	}
	return lines
}

// headerTitle is the right pane's title: the alias with the branch beside it
// when they differ.
func (m Model) headerTitle(r row) string {
	name := m.label(r)
	title := projectStyle.Render(name)
	if strip := m.switcherStrip(r); strip != "" { // the switcher's numbered views replace the breadcrumb
		return title + "  " + strip
	}
	if v := m.viewOf(r); v != viewAgent { // breadcrumb: worktree › terminal
		title += dimStyle.Render(" › ") + focusHint.Render(v.label())
	} else if a, ok := m.agentUnderCursor(r); ok { // worktree › feature-a
		title += dimStyle.Render(" › ") + focusHint.Render(m.agentLabel(a))
	}
	if branch := r.wt.Label(); branch != name {
		title += dimStyle.Render("  " + branch)
	}
	return title
}

// filterSummary names the active filters for the title bar.
func (m Model) filterSummary() string {
	var parts []string
	if q := strings.TrimSpace(m.search); q != "" {
		parts = append(parts, "/"+q)
	}
	if m.attention {
		parts = append(parts, "◆ attention")
	}
	return strings.Join(parts, " ")
}

// orgBand is the full-width bar that heads an org's projects. It is the one
// filled element in the list, so the top level reads at a glance.
func orgBand(org string, w int) string {
	return orgStyle.Render(fit(" "+org, w))
}

// paneState is the pane header's status text: the agent's state with its age,
// or whether a companion (a child row's own program) is running.
func (m Model) paneState(r row) string {
	state := idleStyle.Render("○ no session")
	if a, ok := m.curAgent(r); ok {
		d := m.agentDisplay(a)
		if !a.running {
			state = d.style.Render("○ stopped")
		} else {
			state = d.style.Render(d.glyph + " " + d.word)
			if e, ok := m.agents[a.session]; ok {
				state += dimStyle.Render(" · " + age(e.Since, time.Now()))
			}
		}
	}
	if v := m.viewOf(r); v != viewAgent { // a child row shows its own program, not the agent
		if m.targetRunning(r) {
			state = doneStyle.Render("● running")
		} else {
			state = dimStyle.Render("○ not running")
		}
	}
	return state
}
