package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/gitinfo"
)

// withColour builds the styles for a truecolor terminal and restores the
// profile after, so a test can look for exact 24-bit codes.
func withColour(t *testing.T) {
	t.Helper()
	prev := colorProfile
	SetColorProfile(colorprofile.TrueColor)
	t.Cleanup(func() { SetColorProfile(prev) })
}

var busy = gitinfo.Status{
	Branch: "perf/typing-latency", Upstream: "origin/perf/typing-latency", HasUpstream: true,
	Ahead: 2, Behind: 1, Staged: 2, Modified: 3, Untracked: 1,
}

func TestGitBarShowsEverythingInGitmuxOrder(t *testing.T) {
	got := ansi.Strip(gitBar(busy, gitIconsFor("unicode")))
	want := " ⎇  perf/typing-latency  ↑ 2  ↓ 1  ● 2  ✚ 3  … 1" // the badge pads one space each side
	if got != want {
		t.Errorf("gitBar = %q\nwant      %q", got, want)
	}
}

func TestGitBarForCleanSyncedBranch(t *testing.T) {
	st := gitinfo.Status{Branch: "main", HasUpstream: true, Upstream: "origin/main"}
	got := ansi.Strip(gitBar(st, gitIconsFor("unicode")))
	if !strings.Contains(got, "main") || !strings.HasSuffix(got, symClean) {
		t.Errorf("clean bar = %q, want branch and a tick", got)
	}
	if strings.ContainsAny(got, "↑↓●✚…✖") {
		t.Errorf("a clean branch shows change symbols: %q", got)
	}
}

func TestGitBarFlagsLocalOnlyDetachedAndConflicts(t *testing.T) {
	local := ansi.Strip(gitBar(gitinfo.Status{Branch: "feature/new"}, gitIconsFor("unicode")))
	if !strings.Contains(local, "no upstream") {
		t.Errorf("a branch without upstream must say so: %q", local)
	}
	detached := ansi.Strip(gitBar(gitinfo.Status{Detached: true, Head: "abc1234"}, gitIconsFor("unicode")))
	if !strings.Contains(detached, "detached abc1234") || strings.Contains(detached, "no upstream") {
		t.Errorf("detached = %q", detached)
	}
	conflict := ansi.Strip(gitBar(gitinfo.Status{Branch: "x", HasUpstream: true, Conflicts: 2}, gitIconsFor("unicode")))
	if !strings.Contains(conflict, symConflict+" 2") || strings.Contains(conflict, symClean) {
		t.Errorf("conflicts = %q", conflict)
	}
}

func TestGitBriefIsCompactAndEmptyWhenClean(t *testing.T) {
	if got := ansi.Strip(gitBrief(busy, nil)); got != "↑2 ↓1 ●2 ✚3 …1" {
		t.Errorf("brief = %q", got)
	}
	if got := gitBrief(gitinfo.Status{Branch: "main", HasUpstream: true}, nil); got != "" {
		t.Errorf("a clean worktree must add nothing to its row: %q", got)
	}
	if got := gitBrief(gitinfo.Status{Branch: "local"}, nil); got != "" {
		t.Errorf("no-upstream belongs in the header, not on every row: %q", got)
	}
}

func TestGitColoursAreDistinct(t *testing.T) {
	withColour(t)
	// staged, modified and untracked must not look alike
	seen := map[string]string{}
	for name, seg := range map[string]string{
		"staged":    gitSegments(gitinfo.Status{Staged: 1, HasUpstream: true}, false, false, nil)[0],
		"modified":  gitSegments(gitinfo.Status{Modified: 1, HasUpstream: true}, false, false, nil)[0],
		"untracked": gitSegments(gitinfo.Status{Untracked: 1, HasUpstream: true}, false, false, nil)[0],
		"ahead":     gitSegments(gitinfo.Status{Ahead: 1, HasUpstream: true}, false, false, nil)[0],
		"behind":    gitSegments(gitinfo.Status{Behind: 1, HasUpstream: true}, false, false, nil)[0],
		"conflict":  gitSegments(gitinfo.Status{Conflicts: 1, HasUpstream: true}, false, false, nil)[0],
	} {
		i := strings.Index(seg, "38;2;")
		if i < 0 {
			t.Fatalf("%s has no 24-bit foreground colour: %q", name, seg)
		}
		colour := seg[i : i+strings.Index(seg[i:], "m")]
		if other, dup := seen[colour]; dup {
			t.Errorf("%s and %s share the colour %q", name, other, colour)
		}
		seen[colour] = name
	}
}

func TestRowsAndHeaderShowGitState(t *testing.T) {
	m := orgModel()
	m.git = map[string]gitinfo.Status{m.rows[0].session: busy}
	view := ansi.Strip(m.render())
	for _, want := range []string{"⎇  perf/typing-latency", "↑ 2", "✚ 3", "↑2", "✚3"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d width %d, want %d", i, w, m.width)
		}
	}
}

func TestRowMarkersDropWhenTheListIsNarrow(t *testing.T) {
	m := orgModel()
	m.git = map[string]gitinfo.Status{m.rows[0].session: busy}
	for _, w := range []int{20, 24, 30, 46} {
		line := m.renderRow(0, m.rows[0], w)
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("row of width %d overflows: %d", w, got)
		}
	}
	wide := ansi.Strip(m.renderRow(0, m.rows[0], 46))
	if !strings.Contains(wide, "✚3") {
		t.Errorf("a wide row must show the markers: %q", wide)
	}
	narrow := ansi.Strip(m.renderRow(0, m.rows[0], 22))
	if !strings.Contains(narrow, "main") {
		t.Errorf("the label must survive a narrow row: %q", narrow)
	}
}

func TestSelectedRowKeepsItsBackgroundAroundMarkers(t *testing.T) {
	withColour(t)
	m := orgModel()
	m.cursor = 0
	m.git = map[string]gitinfo.Status{m.rows[0].session: busy}
	line := m.renderRow(0, m.rows[0], 46)
	if strings.Count(line, "48;") < 4 { // each marker segment carries the row background
		t.Errorf("markers lose the selected row background: %q", line)
	}
}

func TestGitMessagesMergeAndTicksRation(t *testing.T) {
	m := testModel()
	m, _ = m.handleGit(gitMsg{res: map[string]gitinfo.Status{"a": {Branch: "one"}}})
	m, _ = m.handleGit(gitMsg{res: map[string]gitinfo.Status{"b": {Branch: "two"}}})
	if len(m.git) != 2 || m.git["a"].Branch != "one" {
		t.Errorf("a later read dropped an earlier one: %+v", m.git)
	}
	for i := 1; i <= gitAllEvery; i++ {
		next, cmd := m.handleGitTick()
		m = next
		if m.gitTicks != i || cmd == nil {
			t.Fatalf("tick %d: count %d cmd %v", i, m.gitTicks, cmd != nil)
		}
	}
}

func TestReadGitReadsRealWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		full := append([]string{"-c", "user.name=t", "-c", "user.email=t@x.invalid", "-c", "init.defaultBranch=main", "-C", dir}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "x")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := readGit([]row{{session: "p/main", wt: orgModel().rows[0].wt}})
	if cmd == nil {
		t.Fatal("no command for one row")
	}
	rows := []row{{session: "p/main"}}
	rows[0].wt.Path = dir
	msg := readGit(rows)().(gitMsg)
	if st, ok := msg.res["p/main"]; !ok || st.Untracked != 1 || st.Branch != "main" {
		t.Errorf("readGit = %+v", msg.res)
	}
	if readGit(nil) != nil {
		t.Error("nothing to read must not schedule work")
	}
}

func TestGitBarBranchGlyphFollowsIconSet(t *testing.T) {
	for _, c := range []struct{ mode, glyph string }{
		{"unicode", "⎇"},
		{"nerd", ""},
		{"garbage", "⎇"},
	} {
		got := ansi.Strip(gitBar(busy, gitIconsFor(c.mode)))
		if want := " " + c.glyph + " " + " perf/typing-latency"; !strings.HasPrefix(got, want) {
			t.Errorf("%s: gitBar = %q, want prefix %q", c.mode, got, want)
		}
	}
}

func TestFullViewKeepsWidthInBothIconSets(t *testing.T) {
	for _, mode := range []string{"unicode", "nerd"} {
		m := orgModel()
		m.cfg.Icons = mode
		m.git = map[string]gitinfo.Status{m.rows[0].session: busy}
		for i, l := range strings.Split(m.render(), "\n") {
			if w := ansi.StringWidth(l); w != m.width {
				t.Errorf("%s: line %d width %d, want %d", mode, i, w, m.width)
			}
		}
		if mode == "nerd" && !strings.Contains(ansi.Strip(m.render()), "") {
			t.Errorf("nerd mode must show the branch glyph in the header")
		}
	}
}
