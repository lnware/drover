package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The command line, opened with ":" in the list (in focus mode every key goes
// to the pane, ":" included: Helix uses it). It is for the verbs that do not
// deserve a key of their own: switching context, jumping to a worktree by name,
// opening the terminal or editor. Keys stay for what you do every minute.
// Commands are few on purpose, and a new feature gets one here rather than a
// key.

// command is one verb. complete lists the choices for its argument given what
// is typed so far; a command without arguments has none. listAll says the
// bare verb shows every choice (a short fixed set), rather than one hint.
type command struct {
	name     string
	help     string
	hint     string // shown for the bare verb when listAll is false
	listAll  bool
	complete func(m Model, arg string) []cmdChoice
	run      func(m Model, arg string) (tea.Model, tea.Cmd)
}

// cmdChoice is one suggestion: the whole line it stands for and what it does.
type cmdChoice struct {
	text string
	help string
}

func (m Model) commands() []command {
	return []command{
		{name: "ctx", help: "switch context", listAll: true, complete: completeContexts, run: runContext},
		{name: "wt", help: "jump to a worktree", hint: "<name>", complete: completeWorktrees, run: runWorktree},
		{name: "agent", help: "show the agent of this worktree", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m.openView(viewAgent) }},
		{name: "term", help: "open the terminal", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m.openView(viewTerminal) }},
		{name: "edit", help: "open the editor", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m.openView(viewEditor) }},
		{name: "new", help: "new worktree", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m.openNew() }},
		{name: "config", help: "edit the config file", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m.openConfig() }},
		{name: "reload", help: "reload the config", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m.reloadConfig() }},
		{name: "help", help: "the keys", run: func(m Model, _ string) (tea.Model, tea.Cmd) { m.help = true; return m, nil }},
		{name: "q", help: "quit", run: func(m Model, _ string) (tea.Model, tea.Cmd) { return m, tea.Quit }},
	}
}

// completeContexts lists the other contexts in the order ":ctx" would visit
// them, then the current one, and "-" for the previous.
func completeContexts(m Model, arg string) []cmdChoice {
	defs := m.contexts()
	if defs == nil {
		return nil
	}
	cur := m.activeContext(defs)
	var out []cmdChoice
	add := func(text, help string) {
		if strings.HasPrefix(strings.ToLower(text), strings.ToLower(arg)) {
			out = append(out, cmdChoice{"ctx " + text, help})
		}
	}
	for i := 1; i <= len(defs); i++ {
		idx := (cur + i) % len(defs)
		help := fmt.Sprintf("%d worktrees", len(m.contextRowsOf(defs, idx)))
		if n := m.waitingIn(defs, idx); n > 0 {
			help += fmt.Sprintf(" · ◆ %d waiting", n)
		}
		if idx == cur {
			help = "current"
		}
		add(defs[idx].name, help)
	}
	if m.ctxPrev != "" {
		add("-", "back to "+m.ctxPrev)
	}
	return out
}

func runContext(m Model, arg string) (tea.Model, tea.Cmd) {
	defs := m.contexts()
	if defs == nil {
		return m.switchContext(1) // says there is only one
	}
	switch arg = strings.TrimSpace(arg); arg {
	case "":
		return m.switchContext(1)
	case "-":
		arg = m.ctxPrev
	}
	var hits []int
	for i, d := range defs {
		if strings.EqualFold(d.name, arg) {
			return m.switchContextTo(defs, i)
		}
		if strings.HasPrefix(strings.ToLower(d.name), strings.ToLower(arg)) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 1 {
		return m.switchContextTo(defs, hits[0])
	}
	m.status = fmt.Sprintf("no context %q (contexts: %s)", arg, strings.Join(ctxNames(defs), ", "))
	return m, nil
}

func ctxNames(defs []ctxDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.name
	}
	return out
}

// completeWorktrees lists the worktrees of the active context whose project
// and branch contain every word typed.
func completeWorktrees(m Model, arg string) []cmdChoice {
	var out []cmdChoice
	for _, r := range m.contextRows() {
		hay := strings.ToLower(r.project + " " + r.wt.Label() + " " + m.label(r))
		ok := true
		for _, w := range strings.Fields(strings.ToLower(arg)) {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			d := m.display(r)
			out = append(out, cmdChoice{"wt " + r.session, d.style.Render(strings.TrimSpace(d.glyph + " " + d.word))})
		}
	}
	return out
}

func runWorktree(m Model, arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	if !slices.ContainsFunc(m.contextRows(), func(r row) bool { return r.session == arg }) {
		if hits := completeWorktrees(m, arg); len(hits) > 0 && arg != "" {
			arg = strings.TrimPrefix(hits[0].text, "wt ")
		} else {
			m.status = fmt.Sprintf("no worktree matches %q in this context", arg)
			return m, nil
		}
	}
	return m.goToWorktree(arg)
}

// cmdSplit separates the verb from what follows it.
func cmdSplit(input string) (verb, arg string, hasArg bool) {
	input = strings.TrimLeft(input, " ")
	verb, arg, hasArg = strings.Cut(input, " ")
	return verb, arg, hasArg
}

// cmdSuggestions are the lines offered for what is typed.
func (m Model) cmdSuggestions() []cmdChoice {
	verb, arg, hasArg := cmdSplit(m.cmdInput)
	var out []cmdChoice
	for _, c := range m.commands() {
		switch {
		case hasArg && c.name == verb && c.complete != nil:
			out = append(out, c.complete(m, arg)...)
		case hasArg:
		case !strings.HasPrefix(c.name, strings.ToLower(verb)):
		case c.complete != nil && c.listAll:
			out = append(out, c.complete(m, "")...)
		case c.complete != nil:
			out = append(out, cmdChoice{c.name + " " + c.hint, c.help})
		default:
			out = append(out, cmdChoice{c.name, c.help})
		}
	}
	return out
}

func (m Model) openCommand() (tea.Model, tea.Cmd) {
	m.mode, m.cmdInput, m.cmdSel, m.cmdPos = modeCommand, "", 0, 0
	return m, nil
}

// handleCommandKey edits the line. Tab completes to the chosen suggestion,
// up and down choose, and up from the top (or down from a recalled line)
// walks the history. Enter runs the chosen suggestion, or the line itself
// when it matches nothing but names a command.
func (m Model) handleCommandKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.status, m.notice = "", ""
	sugg := m.cmdSuggestions()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
		return m, nil
	case "enter":
		return m.runCommand(sugg)
	case "tab":
		if m.cmdSel < len(sugg) {
			m.cmdInput = sugg[m.cmdSel].text
			if verb, _, hasArg := cmdSplit(m.cmdInput); !hasArg && m.hasArgs(verb) {
				m.cmdInput += " "
			}
			m.cmdSel = 0
		}
	case "down", "ctrl+n":
		switch {
		case m.cmdPos > 0:
			m.cmdPos--
			m.cmdInput = ""
			if m.cmdPos > 0 {
				m.cmdInput = m.cmdHist[len(m.cmdHist)-m.cmdPos]
			}
		case len(sugg) > 0:
			m.cmdSel = (m.cmdSel + 1) % len(sugg)
		}
	case "up", "ctrl+p":
		switch {
		case m.cmdSel > 0 && m.cmdPos == 0:
			m.cmdSel--
		case m.cmdPos < len(m.cmdHist):
			m.cmdPos++
			m.cmdInput, m.cmdSel = m.cmdHist[len(m.cmdHist)-m.cmdPos], 0
		}
	default:
		m.cmdInput = editText(m.cmdInput, msg)
		m.cmdSel, m.cmdPos = 0, 0
	}
	return m, nil
}

func (m Model) hasArgs(verb string) bool {
	for _, c := range m.commands() {
		if c.name == verb {
			return c.complete != nil
		}
	}
	return false
}

// runCommand runs the chosen suggestion; with none, the typed line if it is
// a command, else says it is not one.
func (m Model) runCommand(sugg []cmdChoice) (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.cmdInput)
	if m.cmdSel < len(sugg) {
		line = sugg[m.cmdSel].text
	}
	verb, arg, _ := cmdSplit(line)
	for _, c := range m.commands() {
		if c.name != verb {
			continue
		}
		m.mode = modeList
		if line != "" && (len(m.cmdHist) == 0 || m.cmdHist[len(m.cmdHist)-1] != line) {
			m.cmdHist = append(slices.Clone(m.cmdHist), line)
		}
		next, cmd := c.run(m, arg)
		// A command that complains (an unknown context, a worktree that is
		// not there) leaves the line open, so it can be corrected.
		if rm, ok := next.(Model); ok && rm.status != "" && rm.mode == modeList {
			rm.mode = modeCommand
			return rm, cmd
		}
		return next, cmd
	}
	m.status = fmt.Sprintf("no command %q: type : and a letter to see them", verb)
	return m, nil
}

// commandLine is the footer while the command line is open.
func (m Model) commandLine() string {
	left := focusHint.Render(" :") + m.cmdInput + focusHint.Render("█")
	right := dimStyle.Render("tab complete · ↑↓ choose · enter run · esc cancel ")
	if m.status != "" {
		right = errStyle.Render(m.status + " ")
	}
	gap := max(m.width-ansi.StringWidth(left)-ansi.StringWidth(right), 1)
	return fit(left+strings.Repeat(" ", gap)+right, m.width)
}

// commandBox is the suggestion list above the command line.
func (m Model) commandBox() []string {
	sugg := m.cmdSuggestions()
	room := max(min(m.height-6, 10), 3)
	start := max(min(m.cmdSel-room/2, len(sugg)-room), 0)
	var rows []string
	for i := start; i < min(start+room, len(sugg)); i++ {
		verb, rest, _ := cmdSplit(sugg[i].text)
		var line string
		if i == m.cmdSel {
			line = focusHint.Render(" ▸ ") + projectStyle.Render(verb) + focusHint.Render(" "+rest)
		} else {
			line = "   " + focusHint.Render(verb) + " " + rest
		}
		rows = append(rows, line+"  "+dimStyleOr(sugg[i].help))
	}
	if len(rows) == 0 {
		rows = []string{" " + dimStyle.Render("no such command")}
	}
	return popupBox(" Commands ", rows, "", 56, m.width-2)
}

// dimStyleOr dims a help text that is not already styled.
func dimStyleOr(s string) string {
	if strings.Contains(s, "\x1b[") {
		return s
	}
	return dimStyle.Render(s)
}
