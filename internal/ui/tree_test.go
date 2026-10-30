package ui

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/fold"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

// foldModel is two orgs, three projects and five groups:
//
//	Acme  infra     base: main            feature: a, b
//	        platform  feature: internals
//	Globex tools       chore: scripts
//
// State is redirected to a temp dir, since every fold key persists.
func foldModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	m := testModel()
	mk := func(org, project, group, branch string, running bool) row {
		path := "/dev/" + org + "/" + project + "/" + group + "/" + branch
		return row{
			org: org, project: project, root: "/dev/" + org + "/" + project,
			wt:      worktree.Worktree{Path: path, Branch: branch, Group: group},
			session: project + "/" + branch, running: running,
		}
	}
	m.rows = []row{
		mk("Acme", "infra", "base", "main", true),
		mk("Acme", "infra", "feature", "a", true),
		mk("Acme", "infra", "feature", "b", false),
		mk("Acme", "platform", "feature", "internals", true),
		mk("Globex", "tools", "chore", "scripts", false),
	}
	m.all = m.rows
	m.cursor, m.selSession = 0, "infra/main"
	m.agents = map[string]status.Entry{
		"infra/main":         {State: status.Working},
		"infra/a":            {State: status.Waiting},
		"platform/internals": {State: status.Done},
	}
	return m
}

func ids(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.id
	}
	return out
}

func TestBuildItemsOrderAndShape(t *testing.T) {
	m := foldModel(t)
	items := m.items()
	// a running worktree is followed by its agent's child row ("a:" ids)
	want := []string{
		"o:Acme", "p:Acme/infra", "g:Acme/infra/base", "infra/main", "a:infra/main",
		"g:Acme/infra/feature", "infra/a", "a:infra/a", "infra/b",
		"p:Acme/platform", "g:Acme/platform/feature", "platform/internals", "a:platform/internals",
		"o:Globex", "p:Globex/tools", "g:Globex/tools/chore", "tools/scripts",
	}
	if got := ids(items); !slices.Equal(got, want) {
		t.Fatalf("items:\n got %v\nwant %v", got, want)
	}
	byID := map[string]item{}
	for _, it := range items {
		byID[it.id] = it
		if it.hidden || it.collapsed {
			t.Errorf("%s: nothing is folded yet", it.id)
		}
	}
	if up := byID["infra/a"].up; !slices.Equal(up, []string{"o:Acme", "p:Acme/infra", "g:Acme/infra/feature"}) {
		t.Errorf("worktree ancestors: %v", up)
	}
	if byID["infra/a"].row != 1 || byID["o:Acme"].row != -1 {
		t.Errorf("row indexes: %d %d", byID["infra/a"].row, byID["o:Acme"].row)
	}
	sum := byID["o:Acme"].sum
	if sum != (tally{total: 4, working: 1, waiting: 1, done: 1}) {
		t.Errorf("org tally: %+v", sum)
	}
	if sum := byID["g:Acme/infra/feature"].sum; sum != (tally{total: 2, waiting: 1}) {
		t.Errorf("group tally: %+v", sum)
	}
	if byID["p:Acme/infra"].root != "/dev/Acme/infra" {
		t.Errorf("project root: %q", byID["p:Acme/infra"].root)
	}
}

func TestBuildItemsHidesWhatFoldsCover(t *testing.T) {
	m := foldModel(t)
	hiddenSet := func(folds map[string]bool, ignore bool) []string {
		var out []string
		for _, it := range buildItems(m.rows, folds, ignore, m.stateOf) {
			if it.hidden {
				out = append(out, it.id)
			}
		}
		return out
	}
	if got := hiddenSet(map[string]bool{"g:Acme/infra/feature": true}, false); !slices.Equal(got, []string{"infra/a", "infra/b"}) {
		t.Errorf("group fold hides %v", got)
	}
	got := hiddenSet(map[string]bool{"p:Acme/infra": true}, false)
	want := []string{"g:Acme/infra/base", "infra/main", "g:Acme/infra/feature", "infra/a", "infra/b"}
	if !slices.Equal(got, want) {
		t.Errorf("project fold hides %v", got)
	}
	// the folded header itself stays on screen
	if got := hiddenSet(map[string]bool{"o:Acme": true}, false); slices.Contains(got, "o:Acme") || len(got) != 9 {
		t.Errorf("org fold hides %v", got)
	}
	// a filter shows every match, and shows the headers as open
	for _, it := range buildItems(m.rows, map[string]bool{"o:Acme": true, "g:Globex/tools/chore": true}, true, m.stateOf) {
		if it.hidden || it.collapsed {
			t.Errorf("%s folded despite ignoreFolds", it.id)
		}
	}
}

func TestBuildItemsWithoutOrgsStartsAtProject(t *testing.T) {
	m := testModel() // org "" for both rows
	items := m.items()
	if got := ids(items); !slices.Equal(got, []string{"p:/infra", "g:/infra/base", "infra/main", "a:infra/main", "g:/infra/feature", "infra/feature/a"}) {
		t.Errorf("items: %v", got)
	}
	if items[0].parent() != "" || items[1].parent() != "p:/infra" {
		t.Errorf("parents: %q %q", items[0].parent(), items[1].parent())
	}
}

func keyLeft() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyLeft} }
func keyRight() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyRight} }

func onHeader(t *testing.T, m Model, id string) {
	t.Helper()
	if m.hdr != id {
		t.Fatalf("cursor on header %q (selSession %q), want header %q", m.hdr, m.selSession, id)
	}
	if _, ok := m.selected(); ok {
		t.Fatal("selected() must be false on a header")
	}
}

func TestCollapseWalksUpAndExpandComesBack(t *testing.T) {
	m := foldModel(t)
	// from infra/main: its agent row, the feature header, then infra/a
	m, _ = press(t, m, keyRunes("j"), keyRunes("j"), keyRunes("j"))
	if m.selSession != "infra/a" || m.hdr != "" {
		t.Fatalf("setup: %q %q", m.selSession, m.hdr)
	}
	// worktree -> its group
	m, _ = press(t, m, keyLeft())
	onHeader(t, m, "g:Acme/infra/feature")
	if !m.folds["g:Acme/infra/feature"] {
		t.Fatal("the group is not collapsed")
	}
	if vis := visibleItems(m.items()); slices.ContainsFunc(vis, func(it item) bool { return it.id == "infra/a" }) {
		t.Error("the folded worktree is still listed")
	}
	// group header -> its project, then the org
	m, _ = press(t, m, keyRunes("h"))
	onHeader(t, m, "p:Acme/infra")
	m, _ = press(t, m, keyRunes("h"))
	onHeader(t, m, "o:Acme")
	// the org has no parent: it folds itself and stays put
	m, _ = press(t, m, keyLeft())
	onHeader(t, m, "o:Acme")
	// and back: right expands the folded header, then steps into it
	m, _ = press(t, m, keyRight())
	if m.folds["o:Acme"] {
		t.Error("right did not expand the org")
	}
	m, _ = press(t, m, keyRight())
	onHeader(t, m, "p:Acme/infra")
	m, _ = press(t, m, keyRight()) // project still folded: opens
	m, _ = press(t, m, keyRight()) // steps to the first group
	onHeader(t, m, "g:Acme/infra/base")
	m, _ = press(t, m, keyRunes("l"), keyRunes("l"))
	if m.hdr != "" || m.selSession != "infra/main" {
		t.Errorf("right on an open header steps to its first child: %q %q", m.hdr, m.selSession)
	}
	// right on a worktree does nothing
	if next, _ := press(t, m, keyRight()); next.selSession != m.selSession || next.hdr != "" {
		t.Error("right moved off a worktree")
	}
}

func TestEnterTogglesHeaderAndFocusesWorktree(t *testing.T) {
	m := foldModel(t)
	m.hdr = "p:Acme/infra"
	m, _ = press(t, m, enter())
	if !m.folds["p:Acme/infra"] || m.mode != modeList {
		t.Fatalf("enter on a header must fold it without focusing: %v mode %d", m.folds, m.mode)
	}
	m, _ = press(t, m, enter())
	if m.folds["p:Acme/infra"] {
		t.Error("second enter must unfold")
	}
}

func TestFoldAllAndUnfoldAll(t *testing.T) {
	m := foldModel(t)
	m, _ = press(t, m, keyRunes("F"))
	vis := visibleItems(m.items())
	if got := ids(vis); !slices.Equal(got, []string{"o:Acme", "o:Globex"}) {
		t.Fatalf("after F only the org bands remain: %v", got)
	}
	onHeader(t, m, "o:Acme") // the worktree under the cursor is folded into its outermost header
	m, _ = press(t, m, keyRunes("F"))
	if len(m.folds) != 0 {
		t.Errorf("a second F unfolds everything: %v", m.folds)
	}
	// one section open: F still folds
	m, _ = press(t, m, keyRunes("h"))
	m, _ = press(t, m, keyRunes("F"))
	if len(visibleItems(m.items())) != 2 {
		t.Errorf("F with something open must fold all")
	}
}

func TestFoldKeepsStaleIds(t *testing.T) {
	m := foldModel(t)
	m.folds = map[string]bool{"p:Gone/project": true}
	m, _ = press(t, m, keyRunes("F"), keyRunes("F"))
	if !m.folds["p:Gone/project"] {
		t.Error("unfolding all must not forget sections that are merely not listed right now")
	}
}

func TestFilterShowsMatchesInsideFoldsAndFoldsReturn(t *testing.T) {
	m := foldModel(t)
	m, _ = press(t, m, keyRunes("F")) // everything folded
	m, _ = press(t, m, keyRunes("/"), keyRunes("internals"))
	got := ids(visibleItems(m.items()))
	want := []string{"o:Acme", "p:Acme/platform", "g:Acme/platform/feature", "platform/internals", "a:platform/internals"}
	if !slices.Equal(got, want) {
		t.Fatalf("a match inside a folded section must show: %v", got)
	}
	plain := ansi.Strip(m.render())
	if !strings.Contains(plain, "internals") {
		t.Errorf("view hides the match:\n%s", plain)
	}
	// the cursor was on the Acme header, which still shows: it stays there,
	// and the match is three steps down
	m, _ = press(t, m, enter()) // keep the filter
	onHeader(t, m, "o:Acme")
	m, _ = press(t, m, keyRunes("j"), keyRunes("j"), keyRunes("j"))
	if m.hdr != "" || m.selSession != "platform/internals" {
		t.Errorf("the match is not reachable: hdr %q sel %q", m.hdr, m.selSession)
	}
	// fold keys pause while filtering instead of silently changing state
	before := len(m.folds)
	m, _ = press(t, m, keyRunes("F"), keyLeft())
	if len(m.folds) != before || m.notice == "" {
		t.Errorf("folds changed under a filter: %v notice %q", m.folds, m.notice)
	}
	// clearing restores the folds exactly as left
	m, _ = press(t, m, esc())
	if m.filtering() {
		t.Fatal("filter not cleared")
	}
	if got := ids(visibleItems(m.items())); !slices.Equal(got, []string{"o:Acme", "o:Globex"}) {
		t.Errorf("folds did not return: %v", got)
	}
	onHeader(t, m, "o:Acme")
}

func TestAttentionFilterIgnoresFoldsToo(t *testing.T) {
	m := foldModel(t)
	m, _ = press(t, m, keyRunes("F"), keyRunes("w"))
	got := ids(visibleItems(m.items()))
	// waiting (infra/a) and done (platform/internals) are shown
	if !slices.Contains(got, "infra/a") || !slices.Contains(got, "platform/internals") || slices.Contains(got, "infra/main") {
		t.Errorf("attention view under folds: %v", got)
	}
}

func TestFoldedHeaderSummaryShowsWaitingAgent(t *testing.T) {
	withColour(t)
	m := foldModel(t)
	m.hdr = "p:Acme/infra"
	m, _ = press(t, m, enter())
	m.hdr = "o:Acme" // unselected, so the colours are not on the cursor background
	var line string
	for _, l := range m.leftLines(60, 20) {
		if strings.Contains(ansi.Strip(l), "▸ infra") {
			line = l
		}
	}
	plain := ansi.Strip(line)
	for _, want := range []string{"3 worktrees", "◆ 1 waiting", "● 1 working"} {
		if !strings.Contains(plain, want) {
			t.Errorf("summary lacks %q: %q", want, plain)
		}
	}
	if !strings.Contains(line, waitingStyle.Render("◆ 1 waiting")) {
		t.Errorf("waiting is not in its own colour: %q", line)
	}
	// narrow: whole parts drop from the end, so the urgent one survives
	narrow := ansi.Strip(m.renderHeader(item{kind: itemProject, label: "infra", collapsed: true, sum: tally{total: 3, working: 1, waiting: 1}}, false, 30))
	if !strings.Contains(narrow, "◆ 1 waiting") || strings.Contains(narrow, "working") || ansi.StringWidth(narrow) != 30 {
		t.Errorf("narrow summary: %q (%d)", narrow, ansi.StringWidth(narrow))
	}
	tiny := ansi.Strip(m.renderHeader(item{kind: itemProject, label: "infra", collapsed: true, sum: tally{total: 3, waiting: 1}}, false, 14))
	if ansi.StringWidth(tiny) != 14 || !strings.HasPrefix(tiny, " ▸ infra") {
		t.Errorf("tiny: %q", tiny)
	}
}

func TestSummaryPartsAndTruncation(t *testing.T) {
	texts := func(t tally) string {
		var s []string
		for _, p := range summaryParts(t) {
			s = append(s, p.text)
		}
		return strings.Join(s, "|")
	}
	if got := texts(tally{total: 1}); got != "1 worktree" {
		t.Errorf("singular: %q", got)
	}
	if got := texts(tally{total: 9, working: 1, waiting: 2, done: 3, failed: 4}); got != "9 worktrees|◆ 2 waiting|● 1 working|✗ 4 error|✓ 3 done" {
		t.Errorf("order: %q", got)
	}
	parts := summaryParts(tally{total: 3, waiting: 1})
	for w := 0; w < 40; w++ {
		if got := ansi.StringWidth(renderSummary(parts, w, nil)); got > w {
			t.Fatalf("summary width %d over limit %d", got, w)
		}
	}
}

// Every line of the full view is exactly the terminal width, whatever is
// folded, wherever the cursor is, at several terminal widths.
func TestViewWidthWithEveryFoldCombination(t *testing.T) {
	withColour(t)
	m := foldModel(t)
	// Child rows under two worktrees (one with both kinds): the cursor walk
	// below visits them, selected and in focus mode, folded or not.
	for _, c := range []struct{ kind, session string }{{"term", "infra/main"}, {"edit", "infra/main"}, {"edit", "infra/a"}} {
		m.companions[tmux.CompanionName(c.kind, c.session)] = true
	}
	headers := []string{
		"o:Acme", "p:Acme/infra", "g:Acme/infra/base", "g:Acme/infra/feature",
		"p:Acme/platform", "g:Acme/platform/feature", "o:Globex", "p:Globex/tools", "g:Globex/tools/chore",
	}
	for _, width := range []int{60, 80, 140} {
		for _, height := range []int{12, 30} {
			m.width, m.height = width, height
			for mask := 0; mask < 1<<len(headers); mask++ {
				folds := map[string]bool{}
				for i, id := range headers {
					if mask>>i&1 == 1 {
						folds[id] = true
					}
				}
				m.folds = folds
				// the cursor on every visible item for a few masks, else the default
				positions := 1
				if mask%61 == 0 {
					positions = len(visibleItems(m.items()))
				}
				for p := 0; p < positions; p++ {
					c := m
					c.reselect()
					if positions > 1 {
						c.step(p)
					}
					for _, mode := range []mode{modeList, modeFocus} {
						c.mode = mode
						lines := strings.Split(c.render(), "\n")
						if len(lines) != c.height {
							t.Fatalf("mask %b %dx%d: %d lines", mask, width, height, len(lines))
						}
						for i, l := range lines {
							if w := ansi.StringWidth(l); w != c.width {
								t.Fatalf("mask %b %dx%d cursor %d line %d width %d:\n%s", mask, width, height, p, i, w, ansi.Strip(c.render()))
							}
						}
					}
				}
			}
		}
	}
}

func TestSelectedHeaderIsMarked(t *testing.T) {
	withColour(t)
	m := foldModel(t)
	m.hdr = "g:Acme/infra/feature"
	sel := m.renderHeader(item{kind: itemGroup, label: "feature"}, true, 30)
	if !strings.Contains(sel, "\x1b[") || ansi.StringWidth(sel) != 30 {
		t.Errorf("selected header: %q", sel)
	}
	if sel == m.renderHeader(item{kind: itemGroup, label: "feature"}, false, 30) {
		t.Error("selection is not visible")
	}
	org := item{kind: itemOrg, label: "Acme", collapsed: true, sum: tally{total: 4, waiting: 1}}
	for _, w := range []int{30, 12, 8, 3} {
		a, b := orgLine(org, false, w), orgLine(org, true, w)
		if ansi.StringWidth(a) != w || ansi.StringWidth(b) != w || a == b {
			t.Errorf("org band at %d: %d %d", w, ansi.StringWidth(a), ansi.StringWidth(b))
		}
	}
	if !strings.Contains(ansi.Strip(orgLine(org, false, 40)), "◆ 1 waiting") {
		t.Error("folded org band lacks its summary")
	}
}

func TestHeaderRightPaneSummarisesSection(t *testing.T) {
	m := foldModel(t)
	m.hdr = "p:Acme/infra"
	_, rightW, bodyH := m.geometry()
	lines := m.rightLines(rightW, bodyH)
	if len(lines) != bodyH {
		t.Fatalf("%d lines, want %d", len(lines), bodyH)
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"infra", "project", "3 worktrees", "feature · a", "base · main", "n new worktree"} {
		if !strings.Contains(plain, want) {
			t.Errorf("header pane lacks %q:\n%s", want, plain)
		}
	}
	m.help = true
	if !strings.Contains(ansi.Strip(strings.Join(m.rightLines(rightW, bodyH), "\n")), "fold or unfold everything") {
		t.Error("help is not shown over a header")
	}
}

func TestWorktreeActionsOnHeaderDoNothing(t *testing.T) {
	m := foldModel(t)
	m.hdr = "p:Acme/infra"
	for _, k := range []string{"x", "d", "v", "A", "r"} {
		next, cmd := press(t, m, keyRunes(k))
		if cmd != nil || next.mode != modeList || next.confirmRemove != "" || next.status != "" {
			t.Errorf("%s on a header: cmd %v mode %d status %q", k, cmd != nil, next.mode, next.status)
		}
	}
	// no capture and no resize without a selected running worktree
	m.width, m.height = 120, 30
	m.sized = size{}
	_ = m.pollPreview()
	if m.sized != (size{}) {
		t.Errorf("a header triggered a resize: %+v", m.sized)
	}
	if m.readSelectedGit() != nil {
		t.Error("a header triggered a git read")
	}
	if next, _ := m.handleFocusKey(keyRunes("a")); next.(Model).mode != modeList {
		t.Error("focus mode must not survive without a worktree")
	}
}

func TestNewOnHeaders(t *testing.T) {
	m := foldModel(t)
	m.hdr = "g:Acme/platform/feature"
	if got := m.selectedRoot(); got != "/dev/Acme/platform" {
		t.Errorf("group header root %q", got)
	}
	m.hdr = "p:Globex/tools"
	if got := m.selectedRoot(); got != "/dev/Globex/tools" {
		t.Errorf("project header root %q", got)
	}
	m.hdr = "o:Acme"
	if got := m.selectedRoot(); got != "" {
		t.Errorf("an org has no single project: %q", got)
	}
	next, _ := m.openNew()
	if got := next.(Model); got.mode != modeList || got.status == "" {
		t.Errorf("n on an org: mode %d status %q", got.mode, got.status)
	}
}

func TestFoldsPersistAndLoad(t *testing.T) {
	m := foldModel(t)
	m.hdr = "p:Acme/infra"
	m, _ = press(t, m, enter())
	path := fold.Path(status.Root())
	if got := fold.Load(path); !got["p:Acme/infra"] || len(got) != 1 {
		t.Fatalf("not written: %v", got)
	}
	// a fresh model gets them from the first render's load
	fresh := foldModel(t) // new state dir, so seed the file by hand
	if err := fold.Save(fold.Path(status.Root()), map[string]bool{"o:Globex": true, "g:Acme/infra/feature": true}); err != nil {
		t.Fatal(err)
	}
	next, cmd := fresh.Update(loadFolds()())
	fresh = next.(Model)
	if cmd == nil || !fresh.folds["o:Globex"] || !fresh.folds["g:Acme/infra/feature"] {
		t.Fatalf("folds not applied: %v", fresh.folds)
	}
	if slices.Contains(ids(visibleItems(fresh.items())), "tools/scripts") {
		t.Error("loaded folds do not hide rows")
	}
}

func TestLoadedFoldHidingTheCursorMovesItToTheHeader(t *testing.T) {
	m := foldModel(t) // cursor on infra/main
	next, _ := m.Update(foldsLoaded{set: map[string]bool{"p:Acme/infra": true}})
	m = next.(Model)
	onHeader(t, m, "p:Acme/infra")
}

func TestCorruptFoldsFileMeansNothingFolded(t *testing.T) {
	m := foldModel(t)
	if err := os.WriteFile(fold.Path(status.Root()), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(loadFolds()())
	if got := next.(Model).folds; len(got) != 0 {
		t.Errorf("corrupt file folded %v", got)
	}
}

func TestSaveFailureIsReportedNotFatal(t *testing.T) {
	m := foldModel(t)
	blocker := t.TempDir() + "/file"
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DROVER_STATE_DIR", blocker+"/sub") // a path under a regular file cannot be created
	m.hdr = "p:Acme/infra"
	m, _ = press(t, m, enter())
	if !strings.Contains(m.status, "folds not saved") || !m.folds["p:Acme/infra"] {
		t.Errorf("status %q folds %v", m.status, m.folds)
	}
}

func TestRefreshKeepsHeaderSelection(t *testing.T) {
	m := foldModel(t)
	m.hdr = "p:Acme/platform"
	m.applyFilter() // what every refresh and status poll does
	onHeader(t, m, "p:Acme/platform")
	// the header's project disappears: the cursor falls back to a worktree
	m.all = slices.DeleteFunc(slices.Clone(m.all), func(r row) bool { return r.project == "platform" })
	m.applyFilter()
	if m.hdr != "" {
		t.Errorf("hdr %q", m.hdr)
	}
	if _, ok := m.selected(); !ok {
		t.Error("no worktree selected after the header vanished")
	}
}

func TestCreatedWorktreeRevealsFoldedProject(t *testing.T) {
	m := foldModel(t)
	m.folds = map[string]bool{"p:Acme/infra": true, "g:Acme/infra/feature": true, "p:Acme/platform": true}
	m.hdr = "p:Acme/infra"
	next, _ := m.handleCreated(worktreeCreated{root: "/dev/Acme/infra", wt: worktree.Worktree{Path: "/dev/Acme/infra/feature/c", Branch: "feature/c", Group: "feature"}})
	m = next.(Model)
	if m.folds["p:Acme/infra"] || m.folds["g:Acme/infra/feature"] || !m.folds["p:Acme/platform"] || m.hdr != "" {
		t.Errorf("reveal: folds %v hdr %q", m.folds, m.hdr)
	}
}

func TestItemAtLineAndScrolling(t *testing.T) {
	withColour(t)
	m := foldModel(t)
	w, h := 36, 8
	lay := m.layout(w)
	if len(lay.lines) != len(lay.owner) {
		t.Fatal("owner must parallel lines")
	}
	// unscrolled: line 0 is the Acme band, line 1 the infra header
	m.cursor, m.selSession = 0, "infra/main"
	m.hdr = "o:Acme"
	if it, ok := m.itemAtLine(w, h, 0); !ok || it.id != "o:Acme" {
		t.Errorf("line 0: %+v %v", it, ok)
	}
	if it, ok := m.itemAtLine(w, h, 3); !ok || it.id != "infra/main" {
		t.Errorf("line 3: %+v %v", it, ok)
	}
	if _, ok := m.itemAtLine(w, h, -1); ok {
		t.Error("negative line")
	}
	// walk the cursor over every visible item with a short window: it must
	// always be on screen, and clicking its line must give the same item
	for _, folds := range []map[string]bool{nil, {"g:Acme/infra/feature": true}, {"o:Globex": true}} {
		m.folds = folds
		m.hdr = ""
		m.reselect()
		for n := 0; n < len(visibleItems(m.items())); n++ {
			cur, _ := m.current(m.items())
			lines := m.leftLines(w, h)
			if len(lines) != h {
				t.Fatalf("window has %d lines", len(lines))
			}
			found := false
			for y := range lines {
				if it, ok := m.itemAtLine(w, h, y); ok && it.id == cur.id {
					found = true
					if !strings.Contains(lines[y], "\x1b[") {
						t.Errorf("cursor line unstyled")
					}
				}
			}
			if !found {
				t.Fatalf("folds %v: cursor %s scrolled out of view:\n%s", folds, cur.id, ansi.Strip(strings.Join(lines, "\n")))
			}
			m.step(1)
		}
	}
	// the blank separators between expanded projects map to no item
	for y, o := range m.layout(w).owner {
		if o < 0 && m.layout(w).lines[y] == "" {
			if _, ok := m.itemAtLine(w, 1000, y); ok {
				t.Errorf("blank line %d maps to an item", y)
			}
		}
	}
}

func TestCollapsedProjectsSitCloseTogether(t *testing.T) {
	m := foldModel(t)
	m.folds = map[string]bool{"p:Acme/infra": true, "p:Acme/platform": true}
	var plain []string
	for _, l := range m.leftLines(40, 12) {
		plain = append(plain, strings.TrimRight(ansi.Strip(l), " "))
	}
	if !strings.HasPrefix(plain[1], " ▸ infra") || !strings.HasPrefix(plain[2], " ▸ platform") {
		t.Errorf("folded projects should be adjacent:\n%s", strings.Join(plain, "\n"))
	}
}
