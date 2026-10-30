package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

func TestBuildAgentsJoinsRecordsAndLiveSessions(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	recs := []session.Session{
		{ID: "aaaaaa", Worktree: "p/main", Name: "first", Claude: "u1", Created: t0},
		{ID: "bbbbbb", Worktree: "p/main", Name: "second", Created: t0.Add(time.Minute)},
		{ID: "cccccc", Worktree: "p/other", Name: "elsewhere", Created: t0},
	}
	live := []string{"p/main~bbbbbb", "p/main", "p/main~dddddd", "p/other~cccccc", "p/mainx"}
	got := buildAgents("p/main", "main", recs, live)

	type want struct {
		session, name string
		running       bool
	}
	wants := []want{
		{"p/main", "main", true},          // an old-style session, named after the branch
		{"p/main~dddddd", "dddddd", true}, // live, its record gone: the id is all there is
		{"p/main~aaaaaa", "first", false}, // recorded and stopped
		{"p/main~bbbbbb", "second", true},
	}
	if len(got) != len(wants) {
		t.Fatalf("got %d agents %+v, want %d", len(got), got, len(wants))
	}
	for i, w := range wants {
		if got[i].session != w.session || got[i].name != w.name || got[i].running != w.running {
			t.Errorf("agent %d = %+v, want %+v", i, got[i], w)
		}
	}
	if got[2].claude != "u1" {
		t.Errorf("the conversation id of a record must carry over: %+v", got[2])
	}
	if !anyRunning(got) {
		t.Error("anyRunning must see the running ones")
	}
	if none := buildAgents("p/quiet", "quiet", recs, live); len(none) != 0 {
		t.Errorf("a worktree with no sessions has no agents: %+v", none)
	}
}

func TestOldStyleSessionsGetDistinctDefaultNames(t *testing.T) {
	// two sessions with no record (and no id to name them by) must not share a name
	got := buildAgents("p/main", "main", nil, []string{"p/main"})
	if len(got) != 1 || got[0].name != "main" {
		t.Fatalf("%+v", got)
	}
}

func TestAgentChildRowsAndSelection(t *testing.T) {
	m := testModel() // the first worktree runs one old-style agent
	items := ids(visibleItems(m.items()))
	if !slices.Contains(items, agentItemID("infra/main")) {
		t.Fatalf("a running worktree must show its agent as a child row: %v", items)
	}
	// the worktree row and its agent have different ids even though the session
	// of an old-style agent is named like the worktree
	if agentItemID("infra/main") == "infra/main" {
		t.Fatal("agent ids need their own namespace")
	}
	m.step(1) // onto the agent row
	r := m.rows[0]
	a, ok := m.agentUnderCursor(r)
	if !ok || a.session != "infra/main" || m.subAgent != "infra/main" || m.sub != viewAgent {
		t.Fatalf("cursor on the agent row: agent %+v ok %v subAgent %q", a, ok, m.subAgent)
	}
	if m.target(r) != "infra/main" || !m.targetRunning(r) {
		t.Errorf("the pane must show the selected agent: %q running=%v", m.target(r), m.targetRunning(r))
	}
	m.step(-1) // back to the worktree row: its primary agent is shown, but no child is selected
	if _, onChild := m.agentUnderCursor(r); onChild {
		t.Error("the worktree row is not an agent row")
	}
	if m.target(r) != "infra/main" {
		t.Errorf("the worktree row shows its primary agent: %q", m.target(r))
	}
}

func TestSelectionFallsBackToTheWorktreeWhenItsAgentIsGone(t *testing.T) {
	m := testModel()
	m.step(1)
	if m.subAgent == "" {
		t.Fatal("setup: not on the agent row")
	}
	for i := range m.all {
		m.all[i] = m.all[i].withAgents(nil)
	}
	m.applyFilter()
	m.reselect()
	if m.subAgent != "" || m.selSession != "infra/main" || m.hdr != "" {
		t.Errorf("cursor: sub agent %q session %q hdr %q; want the worktree row", m.subAgent, m.selSession, m.hdr)
	}
}

func TestPrimaryAgentPrefersARunningOne(t *testing.T) {
	list := []agent{{session: "a", running: false}, {session: "b", running: true}, {session: "c", running: true}}
	if a, ok := primaryAgent(list); !ok || a.session != "b" {
		t.Errorf("primary = %+v, want the first running one", a)
	}
	if a, ok := primaryAgent([]agent{{session: "x"}}); !ok || a.session != "x" {
		t.Errorf("with none running the first stopped one: %+v", a)
	}
	if _, ok := primaryAgent(nil); ok {
		t.Error("no agents, no primary")
	}
}

func TestWorktreeShowsItsMostUrgentAgent(t *testing.T) {
	m := testModel()
	m.rows[0] = m.rows[0].withAgents([]agent{
		{session: "infra/main~aaaaaa", name: "a", running: true},
		{session: "infra/main~bbbbbb", name: "b", running: true},
		{session: "infra/main~cccccc", name: "c", running: false},
	})
	m.all = m.rows
	m.agents = map[string]status.Entry{
		"infra/main~aaaaaa": {State: status.Working},
		"infra/main~bbbbbb": {State: status.Waiting},
		"infra/main~cccccc": {State: status.Error}, // stopped: its old state does not count
	}
	if d := m.display(m.rows[0]); d.word != "waiting" {
		t.Errorf("the worktree shows %q, want the waiting agent's state", d.word)
	}
	if working, waiting, running := m.counts(); working != 1 || waiting != 1 || running != 2 {
		t.Errorf("counts = %d working, %d waiting, %d running; want 1, 1, 2 (every running agent counts)", working, waiting, running)
	}
}

func TestNotificationNamesTheAgentNotTheTmuxSession(t *testing.T) {
	m := testModel()
	m.rows[0] = m.rows[0].withAgents([]agent{
		{session: "infra/main~aaaaaa", id: "aaaaaa", name: "fix login", running: true},
		{session: "infra/main~bbbbbb", id: "bbbbbb", name: "tests", running: true},
	})
	m.all = m.rows
	if got := m.notifyName("infra/main~aaaaaa"); got != "main · fix login" {
		t.Errorf("notifyName = %q", got)
	}
	m.rows[0] = m.rows[0].withAgents(m.rows[0].sess[:1])
	m.all = m.rows
	if got := m.notifyName("infra/main~aaaaaa"); got != "main" {
		t.Errorf("a worktree with one agent is named by the worktree alone: %q", got)
	}
}

func TestAgentRowsRenderWithIconsConnectorsAndState(t *testing.T) {
	m := testModel()
	m.rows[0] = m.rows[0].withAgents([]agent{
		{session: "infra/main~aaaaaa", id: "aaaaaa", name: "fix login", running: true},
		{session: "infra/main~bbbbbb", id: "bbbbbb", name: "old work", running: false, created: time.Now().Add(-3 * time.Hour)},
	})
	m.all = m.rows
	m.agents = map[string]status.Entry{"infra/main~aaaaaa": {State: status.Working, Since: time.Now().Add(-2 * time.Minute)}}
	m.companions[tmux.CompanionName("term", m.rows[0].session)] = true
	plain := ansi.Strip(m.render())
	for _, want := range []string{"├── ◈ fix login", "├── ◈ old work", "└── $ terminal ●", "working 2m", "stopped 3h"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the list lacks %q:\n%s", want, plain)
		}
	}
	m.cfg.Icons = "nerd"
	if got := ansi.Strip(m.render()); !strings.Contains(got, nerdIcons.agent+" fix login") || !strings.Contains(got, nerdIcons.terminal+" terminal") {
		t.Errorf("the nerd icon set is not used:\n%s", got)
	}
}

func TestAgentBreadcrumbAndStoppedHint(t *testing.T) {
	m := testModel()
	m.rows[0] = m.rows[0].withAgents([]agent{{session: "infra/main~bbbbbb", id: "bbbbbb", name: "old work"}})
	m.all = m.rows
	m.step(1) // onto the stopped agent's row
	plain := ansi.Strip(m.render())
	for _, want := range []string{"main › old work", "stopped", "old work is stopped. Press enter (or v) to resume it, x to forget it."} {
		if !strings.Contains(plain, want) {
			t.Errorf("the pane lacks %q:\n%s", want, plain)
		}
	}
}

func TestSearchFindsAgentsByName(t *testing.T) {
	m := testModel()
	m.rows[0] = m.rows[0].withAgents([]agent{{session: "infra/main~aaaaaa", id: "aaaaaa", name: "migrate-billing", running: true}})
	m.all = m.rows
	m.search = "billing"
	m.applyFilter()
	if len(m.rows) != 1 || m.rows[0].session != "infra/main" {
		t.Errorf("searching an agent's name must find its worktree: %d rows", len(m.rows))
	}
}

// --- the flows, on a real tmux with a fake claude that records its arguments ---

// fakeClaude writes an executable named claude that appends its arguments, one
// per line with a marker between runs, to a file, then behaves like cat.
func fakeClaude(t *testing.T) (path, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.log")
	script := "#!/bin/sh\nprintf '%s\\n' '--run--' \"$@\" >> '" + argsFile + "'\nexec cat\n"
	path = filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, argsFile
}

// runs reads the recorded launches, each as the list of its arguments.
func runs(t *testing.T, argsFile string, want int) [][]string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(argsFile)
		var out [][]string
		for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			switch {
			case line == "--run--":
				out = append(out, nil)
			case line != "" && len(out) > 0:
				out[len(out)-1] = append(out[len(out)-1], line)
			}
		}
		if len(out) >= want || time.Now().After(deadline) {
			if len(out) < want {
				t.Fatalf("expected %d launches of the agent, saw %d: %q", want, len(out), b)
			}
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func updateKey(m Model, k tea.KeyPressMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

// agentModel is a model with one worktree, no sessions, a fake claude and a
// private tmux server and state directory.
func agentModel(t *testing.T) (Model, string) {
	t.Helper()
	skipWithoutTmux(t)
	m := mouseModel(t)
	claudePath, argsFile := fakeClaude(t)
	m.cfg.AgentCmd = claudePath
	m.rows = []row{{
		org: "", project: "infra", root: "/infra",
		wt:      worktree.Worktree{Path: t.TempDir(), Branch: "feature/login", Group: "feature"},
		session: "infra/feature/login",
	}}
	m.all = m.rows
	m.cursor, m.selSession = 0, "infra/feature/login"
	return m, argsFile
}

func TestNewAgentAsksForANameAndPinsTheConversation(t *testing.T) {
	m, argsFile := agentModel(t)
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // no session yet: ask for a name
	if m.mode != modeNameAgent {
		t.Fatalf("enter on a worktree with no session: mode %d, want the name prompt", m.mode)
	}
	if got := ansi.Strip(strings.Join(m.nameAgentLines(), "\n")); !strings.Contains(got, "Empty uses login") {
		t.Errorf("the prompt must offer the branch-based name:\n%s", got)
	}
	m = updateKey(m, keyRunes("fix  the login"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeFocus {
		t.Fatalf("after the name: mode %d, want focus", m.mode)
	}
	a, ok := m.curAgent(m.rows[0])
	if !ok || a.name != "fix the login" || !a.running || a.id == "" || a.claude == "" {
		t.Fatalf("started agent = %+v", a)
	}
	if a.session != session.TmuxName("infra/feature/login", a.id) || !m.tmux.Has(a.session) {
		t.Errorf("the tmux session must be named after the worktree and the id: %q", a.session)
	}
	if m.subAgent != a.session {
		t.Errorf("the cursor must land on the new agent's row: %q", m.subAgent)
	}
	recs := session.Load(session.Path(status.Root()))
	if len(recs) != 1 || recs[0].Name != "fix the login" || recs[0].Claude != a.claude || recs[0].Worktree != "infra/feature/login" {
		t.Fatalf("registry = %+v", recs)
	}
	args := runs(t, argsFile, 1)[0]
	if argAfter(args, "--session-id") != a.claude {
		t.Errorf("claude must be started with --session-id %s: %q", a.claude, args)
	}
	if argAfter(args, "--name") != "fix the login" {
		t.Errorf("claude must get the name as one argument: %q", args)
	}
	if slices.Contains(args, "--resume") || slices.Contains(args, "--continue") {
		t.Errorf("a new session must not resume anything: %q", args)
	}
}

func TestEmptyNameBecomesTheBranchBasedSuggestionAndCountsUp(t *testing.T) {
	m, _ := agentModel(t)
	for i, want := range []string{"login", "login-2", "login-3"} {
		m.mode = modeList
		m = updateKey(m, keyRunes("N"))
		if m.mode != modeNameAgent {
			t.Fatalf("N: mode %d", m.mode)
		}
		m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // empty
		list := m.rows[0].agentList()
		if len(list) != i+1 || list[i].name != want {
			t.Fatalf("after %d sessions the names are %v, want ...%q", i+1, m.agentNames(m.rows[0]), want)
		}
	}
	if _, _, running := m.counts(); running != 3 {
		t.Errorf("three agents run in one worktree: counts say %d", running)
	}
	if n := len(tmuxAgentSessions(t, m)); n != 3 {
		t.Errorf("three tmux sessions expected, found %d", n)
	}
}

func tmuxAgentSessions(t *testing.T, m Model) []string {
	t.Helper()
	live, _, err := m.tmux.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	return live
}

func TestNameAgentPromptRefusesADuplicateAndCancels(t *testing.T) {
	m, _ := agentModel(t)
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updateKey(m, keyRunes("work"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.mode = modeList
	m = updateKey(m, keyRunes("N"))
	m = updateKey(m, keyRunes("work"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeNameAgent || !strings.Contains(m.status, "already used") {
		t.Fatalf("a duplicate name must keep the prompt open with a message: mode %d status %q", m.mode, m.status)
	}
	if len(m.rows[0].agentList()) != 1 {
		t.Error("a refused name must not start a session")
	}
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeList || len(m.rows[0].agentList()) != 1 {
		t.Errorf("esc cancels: mode %d, %d agents", m.mode, len(m.rows[0].agentList()))
	}
	// a pasted name goes through the same filter as typed text
	m = updateKey(m, keyRunes("N"))
	next, _ := m.Update(tea.PasteMsg{Content: "pasted\nname"})
	m = next.(Model)
	if m.input != "pastedname" {
		t.Errorf("a paste into the name prompt: %q", m.input)
	}
}

func TestStopKeepsTheRowAndResumeUsesTheSameConversation(t *testing.T) {
	m, argsFile := agentModel(t)
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updateKey(m, keyRunes("alpha"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	first, _ := m.curAgent(m.rows[0])
	m.mode = modeList
	runs(t, argsFile, 1) // let the agent really start before it is stopped

	m = pressKey(t, m, "x") // stop it
	stopped, ok := m.curAgent(m.rows[0])
	if !ok || stopped.running || m.tmux.Has(first.session) {
		t.Fatalf("x must stop the agent but keep its row: %+v running tmux=%v", stopped, m.tmux.Has(first.session))
	}
	if recs := session.Load(session.Path(status.Root())); len(recs) != 1 {
		t.Fatalf("a stopped session keeps its record: %+v", recs)
	}

	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // enter on a stopped agent revives it
	if again, _ := m.curAgent(m.rows[0]); !again.running || !m.tmux.Has(first.session) || m.mode != modeFocus {
		t.Fatalf("enter on a stopped agent must resume it and focus: %+v mode %d", again, m.mode)
	}
	runsSeen := runs(t, argsFile, 2)
	if got := argAfter(runsSeen[1], "--resume"); got != first.claude {
		t.Errorf("resume must use the session's own conversation %s, got %q (%q)", first.claude, got, runsSeen[1])
	}
	if slices.Contains(runsSeen[1], "--session-id") {
		t.Errorf("a resume must not start a new conversation: %q", runsSeen[1])
	}
}

func TestForgettingAStoppedAgentRemovesItsRecordAndRow(t *testing.T) {
	m, _ := agentModel(t)
	for _, name := range []string{"one", "two"} {
		m.mode = modeList
		m = updateKey(m, keyRunes("N"))
		m = updateKey(m, keyRunes(name))
		m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	m.mode = modeList
	m = pressKey(t, m, "x") // stops "two" (the cursor is on it)
	m = pressKey(t, m, "x") // forgets it
	list := m.rows[0].agentList()
	if len(list) != 1 || list[0].name != "one" {
		t.Fatalf("after forgetting: %+v", list)
	}
	if recs := session.Load(session.Path(status.Root())); len(recs) != 1 || recs[0].Name != "one" {
		t.Errorf("registry after forgetting: %+v", recs)
	}
	if m.subAgent != "" {
		t.Errorf("the cursor was on the forgotten agent and must fall back to the worktree: %q", m.subAgent)
	}
}

func TestStopOnTheWorktreeRowNeedsOneAgentToBeUnambiguous(t *testing.T) {
	m, _ := agentModel(t)
	for _, name := range []string{"one", "two"} {
		m.mode = modeList
		m = updateKey(m, keyRunes("N"))
		m = updateKey(m, keyRunes(name))
		m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	m.mode = modeList
	m.step(-2) // up to the worktree row
	if _, onChild := m.agentUnderCursor(m.rows[0]); onChild {
		t.Fatal("setup: still on an agent row")
	}
	m = pressKey(t, m, "x")
	if !strings.Contains(m.status, "several agents") || runningCount(m.rows[0]) != 2 {
		t.Errorf("x on a worktree with several agents must not guess: status %q, %d running", m.status, runningCount(m.rows[0]))
	}
}

func TestRenameAnAgentUpdatesTheRegistry(t *testing.T) {
	m, _ := agentModel(t)
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updateKey(m, keyRunes("before"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.mode = modeList
	m = pressKey(t, m, "r")
	if m.mode != modeRename || m.renameAgent == "" || m.input != "before" {
		t.Fatalf("r on an agent row: mode %d agent %q input %q", m.mode, m.renameAgent, m.input)
	}
	m = updateKey(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = updateKey(m, keyRunes("after"))
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	a, _ := m.curAgent(m.rows[0])
	if a.name != "after" || m.mode != modeList {
		t.Fatalf("after renaming: %+v mode %d", a, m.mode)
	}
	if recs := session.Load(session.Path(status.Root())); len(recs) != 1 || recs[0].Name != "after" {
		t.Errorf("registry: %+v", recs)
	}
}

func TestResumeWithNoSessionContinuesTheFoldersLastConversation(t *testing.T) {
	m, argsFile := agentModel(t)
	// a conversation exists for this folder: Claude keeps them under ~/.claude/projects
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", strings.NewReplacer("/", "-", ".", "-").Replace(m.rows[0].wt.Path))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressKey(t, m, "v")
	if m.mode != modeNameAgent || !m.nameContinuing {
		t.Fatalf("v with no session: mode %d continuing %v", m.mode, m.nameContinuing)
	}
	m = updateKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	args := runs(t, argsFile, 1)[0]
	if !slices.Contains(args, "--continue") || slices.Contains(args, "--session-id") {
		t.Errorf("a continued session uses --continue and gets no new conversation id: %q", args)
	}
}

func TestBranchRenameMovesTheStoppedSessionsOfTheWorktree(t *testing.T) {
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	path := session.Path(status.Root())
	if err := session.Add(path, session.Session{ID: "aaaaaa", Worktree: "infra/feature/old", Name: "x", Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	m := testModel()
	m.rows[0].session = "infra/feature/old"
	m.rows[0].running = false
	m.rows[0] = m.rows[0].withAgents([]agent{{session: "infra/feature/old~aaaaaa", id: "aaaaaa", name: "x"}})
	m.all, m.selSession = m.rows, "infra/feature/old"
	res := worktree.RenameResult{Worktree: worktree.Worktree{Path: "/p/infra/chore/new", Branch: "chore/new", Group: "chore"}}
	next, _ := m.handleMoved(branchMoved{oldSession: "infra/feature/old", newSession: "infra/chore/new", res: res})
	m = next.(Model)
	if got := m.rows[0].sess[0].session; got != "infra/chore/new~aaaaaa" {
		t.Errorf("the stopped session's tmux name must follow the branch: %q", got)
	}
	if recs := session.Load(path); len(recs) != 1 || recs[0].Worktree != "infra/chore/new" {
		t.Errorf("the registry must follow the branch: %+v", recs)
	}
}

// Exactly one row carries the selection bar, whichever kind of row the cursor is on.
func TestOnlyTheRowUnderTheCursorIsMarkedSelected(t *testing.T) {
	m := testModel()
	m.rows[0] = m.rows[0].withAgents([]agent{
		{session: "infra/main~aaaaaa", id: "aaaaaa", name: "one", running: true},
		{session: "infra/main~bbbbbb", id: "bbbbbb", name: "two", running: true},
	})
	m.all = m.rows
	m.companions[tmux.CompanionName("term", m.rows[0].session)] = true
	bars := func() []string {
		var out []string
		for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
			if i := strings.Index(l, "│"); i >= 0 && strings.Contains(l[:i], "▌") {
				out = append(out, strings.TrimSpace(l[:i]))
			}
		}
		return out
	}
	// worktree row, agent one, agent two, terminal: each alone
	for step, want := range []string{"main", "one", "two", "terminal"} {
		if step > 0 {
			m.step(1)
		}
		got := bars()
		if len(got) != 1 || !strings.Contains(got[0], want) {
			t.Errorf("cursor step %d: selection bars on %q, want exactly the %q row", step, got, want)
		}
	}
}
