package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
)

func TestSwitcherKeys(t *testing.T) {
	m := testModel()
	for _, c := range []struct {
		key        string
		step, jump int
		ok         bool
	}{
		{"f12", 1, -1, true},
		{"ctrl+tab", 1, -1, true},
		{"ctrl+shift+tab", -1, -1, true},
		{"shift+f12", -1, -1, true},
		{"f1", 0, 0, true},
		{"f9", 0, 8, true},
		{"ctrl+3", 0, 2, true},
		{"f10", 0, -1, false}, // outside 1..9: left for the zoom key and the pane
		{"ctrl+space", 0, -1, false},
		{"ctrl+0", 0, -1, false},
		{"tab", 0, -1, false},
	} {
		step, jump, ok := m.switchKeyPressed(c.key)
		if ok != c.ok || (ok && (step != c.step || jump != c.jump)) {
			t.Errorf("%s: got step %d jump %d ok %v, want %d %d %v", c.key, step, jump, ok, c.step, c.jump, c.ok)
		}
	}
}

// a worktree with two agents and then the terminal and the editor: F12 opens
// the overlay on the next stop, F12 again moves on, enter goes; a number or an
// F-key goes straight to the nth stop and starts what is missing.
func TestSwitcherOverlayAndJumps(t *testing.T) {
	m, _ := agentModel(t)
	m.cfg.TerminalCmd, m.cfg.EditorCmd = "cat", "cat"
	for _, name := range []string{"one", "two"} {
		m.mode = modeList
		m = updateKey(m, keyRunes("N"))
		m = updateKey(m, keyRunes(name))
		m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if m.mode != modeFocus {
		t.Fatalf("a new agent is focused: mode %d", m.mode)
	}
	press := func(key tea.KeyPressMsg) {
		next, _ := m.Update(key)
		m = next.(Model)
	}
	where := func() string {
		r := m.rows[0]
		if v := m.viewOf(r); v != viewAgent {
			return v.label()
		}
		a, _ := m.curAgent(r)
		return m.agentLabel(a)
	}
	f12 := tea.KeyPressMsg{Code: tea.KeyF12}

	if where() != "two" {
		t.Fatalf("on the newest agent: %q", where())
	}
	press(f12) // opens on the stop after "two": the terminal
	if m.mode != modeSwitch || m.switchSel != 2 {
		t.Fatalf("f12 opens the switcher on the next stop: mode %d sel %d", m.mode, m.switchSel)
	}
	overlay := ansi.Strip(m.render())
	for _, want := range []string{"Switch", "▸ 3 $ terminal", "1 ◈ one", "2 ◈ two", "4 ✎ editor", "not started"} {
		if !strings.Contains(overlay, want) {
			t.Errorf("the overlay lacks %q:\n%s", want, overlay)
		}
	}
	for i, l := range strings.Split(m.render(), "\n") { // the popup must not change the frame's width
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.width)
		}
	}
	box := m.switcherBox()
	for i, l := range box { // the border closes on every line
		if ansi.StringWidth(l) != ansi.StringWidth(box[0]) {
			t.Errorf("box line %d is %d cells wide, the top is %d", i, ansi.StringWidth(l), ansi.StringWidth(box[0]))
		}
	}
	if !strings.Contains(overlay, "╭─ Switch ") || !strings.Contains(overlay, "│ ") {
		t.Errorf("the switcher is a bordered popup:\n%s", overlay)
	}
	press(f12) // the choice moves to the editor
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if where() != "editor" || m.mode != modeFocus || !m.tmux.Has(tmuxCompanion(m, viewEditor)) {
		t.Fatalf("enter starts and focuses the editor: %q mode %d", where(), m.mode)
	}

	press(f12) // from the editor the next stop wraps to the first agent
	press(tea.KeyPressMsg{Code: '1', Text: "1"})
	if where() != "one" || m.mode != modeFocus {
		t.Fatalf("a number in the overlay picks that view and keeps focus: %q mode %d", where(), m.mode)
	}

	press(f12)
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if where() != "one" || m.mode != modeFocus {
		t.Fatalf("esc closes the overlay and changes nothing: %q mode %d", where(), m.mode)
	}
	for _, k := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: tea.KeyEnter}} {
		press(f12)
		press(k)
		if k.Code == 'x' && m.mode != modeSwitch {
			t.Fatalf("a key the overlay does not know must not leave it or reach the pane")
		}
		m.mode = modeFocus
	}

	press(tea.KeyPressMsg{Code: tea.KeyF3}) // direct, no overlay
	if where() != "terminal" || m.mode != modeFocus {
		t.Fatalf("f3 jumps to the terminal at once: %q mode %d", where(), m.mode)
	}
	press(tea.KeyPressMsg{Code: '1', Mod: tea.ModCtrl})
	if where() != "one" {
		t.Fatalf("ctrl+1 jumps to the first agent: %q", where())
	}
	press(tea.KeyPressMsg{Code: tea.KeyF9})
	if m.notice == "" || where() != "one" {
		t.Errorf("a number past the last view changes nothing and says so: %q", m.notice)
	}

	// the numbered strip tells which key goes where
	strip := ansi.Strip(m.switcherStrip(m.rows[0]))
	for _, want := range []string{"1 ◈ one", "2 ◈ two", "3 $ terminal", "4 ✎ editor"} {
		if !strings.Contains(strip, want) {
			t.Errorf("strip %q lacks %q", strip, want)
		}
	}
}

func tmuxCompanion(m Model, v paneView) string {
	return tmux.CompanionName(v.kind(), m.rows[0].session)
}

func TestSwitcherJumpInTheListMovesOnlyTheCursor(t *testing.T) {
	m, _ := agentModel(t)
	m.cfg.TerminalCmd = "cat"
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updateKey(m, keyRunes("solo"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.mode = modeList
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	m = next.(Model)
	if m.viewOf(m.rows[0]) != viewTerminal || m.mode != modeList {
		t.Errorf("f2 in the list shows the terminal without focusing it: view %d mode %d", m.viewOf(m.rows[0]), m.mode)
	}
}

// switcherScopeModel has three projects of three worktrees; one agent in the
// last project is waiting.
func switcherScopeModel(t *testing.T) Model {
	t.Helper()
	m := mouseModel(t)
	manyRows(&m, 3)
	m.all[7].running = true // tools/feature/tools-01
	m.agents = map[string]status.Entry{m.all[7].session: {State: status.Waiting}}
	m.rows = m.all
	return oneContext(m)
}

func switcherPress(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	return m
}

func TestSwitcherWorktreesScopeListsAttentionFirstAndFilters(t *testing.T) {
	m := switcherScopeModel(t)
	right := tea.KeyPressMsg{Code: tea.KeyRight}
	m = switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyF12})
	if m.mode != modeSwitch || m.switchScope != scopeViews {
		t.Fatalf("f12 opens on the views: mode %d scope %d", m.mode, m.switchScope)
	}
	m = switcherPress(t, m, right)
	items := m.switchItems()
	if len(items) != 9 || items[0].row != "tools/feature/tools-01" {
		t.Fatalf("worktrees scope lists all 9 with the waiting one first: %d, first %+v", len(items), items[0])
	}
	if shown := ansi.Strip(m.render()); !strings.Contains(shown, "Views · Worktrees · Projects") || !strings.Contains(shown, "waiting") {
		t.Errorf("the popup shows the scope tabs and the state:\n%s", shown)
	}

	for _, r := range "plat" {
		m = switcherPress(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.switchItems(); len(got) != 3 || !strings.HasPrefix(got[0].text, "platform") {
		t.Fatalf("typing filters to the platform worktrees: %+v", got)
	}

	// the target sits in a folded section: going there opens it
	m.folds = map[string]bool{"p:Acme/platform": true}
	m = switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeList || m.selSession != "platform/feature/platform-01" || m.hdr != "" {
		t.Fatalf("enter goes to the worktree and closes the popup: mode %d sel %q hdr %q", m.mode, m.selSession, m.hdr)
	}
	if m.folds["p:Acme/platform"] {
		t.Error("the section that hid the worktree must be opened")
	}
}

func TestSwitcherProjectsScopeAndWrapping(t *testing.T) {
	m := switcherScopeModel(t)
	m = switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyF12}, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.switchScope != scopeContexts {
		t.Fatalf("left from the views wraps to the contexts: scope %d", m.switchScope)
	}
	m = switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.switchScope != scopeProjects {
		t.Fatalf("left again is the projects: scope %d", m.switchScope)
	}
	items := m.switchItems()
	if len(items) != 3 || items[2].text != "Globex/tools" || !strings.Contains(ansi.Strip(items[2].right), "1 waiting") {
		t.Fatalf("projects with their waiting count: %+v", items)
	}
	m = switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.selSession != "tools/main" {
		t.Errorf("a project goes to its first worktree: %q", m.selSession)
	}

	// esc changes nothing
	before := m.selSession
	m = switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyF12}, tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeList || m.selSession != before {
		t.Errorf("esc closes the popup where it was: mode %d sel %q", m.mode, m.selSession)
	}
}

// The popup opens on the scope the cursor stands for: Projects on a project
// header (or an org), Worktrees on a group, Views on a worktree.
func TestSwitcherOpensOnTheScopeOfTheCursor(t *testing.T) {
	m := switcherScopeModel(t)
	itemOf := func(kind itemKind, id string) item {
		for _, it := range m.items() {
			if it.kind == kind && (id == "" || it.id == id) {
				return it
			}
		}
		t.Fatalf("no item of kind %d %q", kind, id)
		return item{}
	}
	f12 := tea.KeyPressMsg{Code: tea.KeyF12}

	// a project header: Projects, with that project chosen
	m.moveTo(itemOf(itemProject, "p:Acme/platform"))
	opened := switcherPress(t, m, f12)
	if opened.mode != modeSwitch || opened.switchScope != scopeProjects {
		t.Fatalf("on a project the switcher opens on Projects: mode %d scope %d", opened.mode, opened.switchScope)
	}
	if got := opened.switchItems()[opened.switchSel].text; got != "Acme/platform" {
		t.Errorf("the project under the cursor is chosen, got %q", got)
	}
	if shown := ansi.Strip(opened.render()); !strings.Contains(shown, "▸ Acme/platform") {
		t.Errorf("the popup marks it:\n%s", shown)
	}
	// choosing another project goes there, though the cursor was on a header
	picked := switcherPress(t, opened, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if picked.mode != modeList || picked.hdr != "" || picked.selSession != "tools/main" {
		t.Errorf("enter goes to the project's worktree: mode %d hdr %q sel %q", picked.mode, picked.hdr, picked.selSession)
	}

	// an org band behaves like its project
	m.moveTo(itemOf(itemOrg, "o:Globex"))
	if o := switcherPress(t, m, f12); o.switchScope != scopeProjects || o.switchItems()[o.switchSel].text != "Globex/tools" {
		t.Errorf("an org opens Projects on its first project: scope %d", o.switchScope)
	}

	// a group header: Worktrees, on that group's first worktree
	m.moveTo(itemOf(itemGroup, "g:Acme/platform/feature"))
	g := switcherPress(t, m, f12)
	if g.switchScope != scopeWorktrees || g.switchItems()[g.switchSel].row != "platform/feature/platform-01" {
		t.Errorf("a group opens Worktrees on its first worktree: scope %d sel %+v", g.switchScope, g.switchItems()[g.switchSel])
	}

	// the Views scope has nothing to show on a header, and says so
	v := switcherPress(t, g, tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Code: tea.KeyRight})
	if v.switchScope != scopeViews || len(v.switchItems()) != 0 || !strings.Contains(ansi.Strip(v.render()), "select a worktree") {
		t.Errorf("views on a header: scope %d, %d items", v.switchScope, len(v.switchItems()))
	}

	// on a worktree it is still the Views (the behaviour that was there before)
	m.moveTo(item{kind: itemWorktree, id: m.rows[1].session, row: 1})
	if w := switcherPress(t, m, f12); w.switchScope != scopeViews {
		t.Errorf("on a worktree the switcher opens on Views: scope %d", w.switchScope)
	}
	// a number key does nothing on a header
	m.moveTo(itemOf(itemProject, "p:Acme/platform"))
	if n := switcherPress(t, m, tea.KeyPressMsg{Code: tea.KeyF1}); n.mode != modeList || n.hdr == "" {
		t.Errorf("f1 on a header must do nothing: mode %d hdr %q", n.mode, n.hdr)
	}
}
