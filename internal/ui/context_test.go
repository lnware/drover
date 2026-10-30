package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/worktree"
)

// oneContext makes the model's orgs one context, so that tests about the
// whole list do not see it narrowed to an org (each org is a context by
// default).
func oneContext(m Model) Model {
	seen := map[string]bool{}
	var match []string
	for _, r := range m.all {
		if !seen[r.org] {
			seen[r.org] = true
			match = append(match, r.org)
		}
	}
	m.cfg.Contexts = []config.Context{{Name: "all", Match: match}}
	return m
}

// contextModel has Acme (infra, web-site) and Personal (garden-app); no
// context is configured, so each org is one.
func contextModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	m := testModel()
	mk := func(org, project, branch string) row {
		return row{
			org: org, project: project, root: "/" + project,
			wt:      worktree.Worktree{Path: "/" + project + "/" + branch, Branch: branch, Group: worktree.BaseGroup},
			session: project + "/" + branch,
		}
	}
	m.all = []row{mk("Acme", "infra", "main"), mk("Acme", "web-site", "main"), mk("Personal", "garden-app", "main")}
	m.agents = map[string]status.Entry{}
	m.applyFilter()
	return m
}

func TestEachOrgIsAContextByDefaultAndOnlyTheActiveOneIsListed(t *testing.T) {
	m := contextModel(t)
	defs := m.contexts()
	if len(defs) != 2 || defs[0].name != "Acme" || defs[1].name != "Personal" {
		t.Fatalf("each org is a context, in list order: %+v", defs)
	}
	if len(m.rows) != 2 || m.rows[0].org != "Acme" {
		t.Fatalf("the first context is shown: %+v", m.rows)
	}
	if got := len(m.contextRows()); got != 2 {
		t.Errorf("contextRows: %d", got)
	}
	if title := ansi.Strip(m.titleBar()); !strings.Contains(title, "Acme") || !strings.Contains(title, "Personal") || !strings.Contains(title, "2 worktrees") {
		t.Errorf("the title bar has the context tabs and the context's own count: %q", title)
	}
	one := contextModel(t)
	one.all = one.all[:2]
	if one.contexts() != nil {
		t.Error("one org is nothing to switch between: no contexts")
	}
}

func TestConfiguredContextsMatchOrgsAndProjectsAndLeaveAnOtherContext(t *testing.T) {
	m := contextModel(t)
	m.cfg.Contexts = []config.Context{
		{Name: "Work", Match: []string{"infra"}},             // a project name
		{Name: "Free time", Match: []string{"org:Personal"}}, // an org, spelled out
	}
	defs := m.contexts()
	if len(defs) != 3 || defs[2].name != "Other" {
		t.Fatalf("web-site is in neither, so an Other context appears: %+v", defs)
	}
	for i, want := range []int{0, 2, 1} {
		if got := contextOf(defs, m.all[i]); got != want {
			t.Errorf("%s is in context %d, want %d", m.all[i].session, got, want)
		}
	}
	// a name that is only an org does not match a project of that name
	if (ctxDef{match: []string{"project:Acme"}}).has(m.all[0]) {
		t.Error("project: must not match an org")
	}
}

func TestSwitchingContextShowsItsRowsRemembersTheCursorAndPersists(t *testing.T) {
	m := contextModel(t)
	m.moveTo(item{kind: itemWorktree, id: "web-site/main", row: 1}) // the second Acme worktree
	m.selSession = "web-site/main"
	m.search = "x" // a filter is about the context you leave
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF11})
	m = next.(Model)
	if m.ctxActive != "Personal" || len(m.rows) != 1 || m.rows[0].project != "garden-app" || m.search != "" {
		t.Fatalf("f11 goes to the next context: active %q rows %+v search %q", m.ctxActive, m.rows, m.search)
	}
	if m.selSession != "garden-app/main" {
		t.Errorf("the cursor lands on the context's worktree: %q", m.selSession)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyF11, Mod: tea.ModShift})
	m = next.(Model)
	if m.ctxActive != "Acme" || m.selSession != "web-site/main" {
		t.Errorf("shift+f11 goes back to where you were: %q on %q", m.ctxActive, m.selSession)
	}

	// persisted: a fresh model reads it back through the same message Init sends
	msg := loadContext()().(ctxLoaded)
	if msg.active != "Acme" || msg.last["Personal"] != "garden-app/main" {
		t.Errorf("the active context and the last worktrees are saved: %+v", msg)
	}
	fresh := contextModel(t)
	got, _ := fresh.Update(ctxLoaded{active: "Personal"})
	if g := got.(Model); g.ctxActive != "Personal" || g.rows[0].project != "garden-app" {
		t.Errorf("a saved context is restored: %q", g.ctxActive)
	}
	// a context that no longer exists falls back to the first
	got, _ = fresh.Update(ctxLoaded{active: "Gone"})
	if g := got.(Model); g.rows[0].org != "Acme" {
		t.Errorf("an unknown saved context falls back: %+v", g.rows)
	}
}

func TestContextKeyWorksInFocusModeAndIsRefusedWithoutContexts(t *testing.T) {
	m := contextModel(t)
	m.agents = map[string]status.Entry{"garden-app/main": {State: status.Working}}
	m.all[2].running = true
	m.mode = modeFocus
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF11})
	got := next.(Model)
	if got.ctxActive != "Personal" || got.mode != modeFocus {
		t.Errorf("in focus the key switches and stays focused when the agent runs: %q mode %d", got.ctxActive, got.mode)
	}
	one := contextModel(t)
	one.all = one.all[:2]
	one.applyFilter()
	n, _ := one.Update(tea.KeyPressMsg{Code: tea.KeyF11})
	if g := n.(Model); g.notice == "" || len(g.rows) != 2 {
		t.Errorf("with one context the key says so: %q", g.notice)
	}
}

func TestAgentsOutsideTheContextCountOnTheTabAndFillTheDigest(t *testing.T) {
	m := contextModel(t)
	m.all[2].running = true
	m.statusLoaded = true
	m.prevStates = map[string]status.State{"garden-app/main": status.Working}
	next, _ := m.handleStatus(statusMsg{entries: map[string]status.Entry{"garden-app/main": {State: status.Waiting}}})
	m = next.(Model)
	if len(m.away) != 1 {
		t.Fatalf("a waiting agent outside the context is remembered: %+v", m.away)
	}
	if title := ansi.Strip(m.titleBar()); !strings.Contains(title, "Personal ◆1") {
		t.Errorf("the other context's tab says something waits there: %q", title)
	}
	if strings.Contains(ansi.Strip(m.titleBar()), "1 waiting") {
		t.Error("the title counts only the active context")
	}

	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF11})
	m = got.(Model)
	if m.mode != modeDigest || len(m.digest) != 1 || len(m.away) != 0 {
		t.Fatalf("entering the context shows what was held: mode %d digest %d away %d", m.mode, len(m.digest), len(m.away))
	}
	if shown := ansi.Strip(m.render()); !strings.Contains(shown, "Held while you were away") || !strings.Contains(shown, "garden-app") || !strings.Contains(shown, "waiting") {
		t.Errorf("the digest popup:\n%s", shown)
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.width)
		}
	}
	closed, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if c := closed.(Model); c.mode != modeList || c.digest != nil {
		t.Errorf("any key closes it: mode %d", c.mode)
	}

	// news that has gone stale is not shown: the agent moved on while away
	m2 := contextModel(t)
	m2.all[2].running = true
	m2.agents = map[string]status.Entry{"garden-app/main": {State: status.Working}}
	m2.away = []awayEvent{{session: "garden-app/main", state: status.Waiting}}
	s, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyF11})
	if g := s.(Model); g.mode == modeDigest {
		t.Error("an agent that is working again is not news")
	}
}
