package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/gitinfo"
	"github.com/lnware/drover/internal/pr"
	"github.com/lnware/drover/internal/status"
)

func reviewModel(t *testing.T) Model {
	m := commandModel(t)
	m.git = map[string]gitinfo.Status{
		"infra/main":    {Branch: "main", Modified: 2, Untracked: 1, Staged: 1, Partial: 1},
		"web-site/main": {Branch: "main", Staged: 2}, // all staged: reviewed
	}
	return m
}

func TestChipCountsFilesNotStagedAndFlagsThoseChangedAfterStaging(t *testing.T) {
	cases := []struct {
		st            gitinfo.Status
		chip, partial string
	}{
		{gitinfo.Status{}, "", ""},
		{gitinfo.Status{Staged: 3}, "", ""},                                 // staged is reviewed
		{gitinfo.Status{Modified: 2, Untracked: 1}, "✎3", ""},               // unstaged and new files wait
		{gitinfo.Status{Modified: 2, Untracked: 1, Partial: 1}, "✎3", "↻1"}, // one of them was staged before
	}
	for _, c := range cases {
		if chip, partial := reviewChip(c.st); chip != c.chip || partial != c.partial {
			t.Errorf("%+v: %q %q, want %q %q", c.st, chip, partial, c.chip, c.partial)
		}
	}
}

func TestRowShowsTheChipAndTheTitleCountsWorktreesWithFilesWaiting(t *testing.T) {
	m := reviewModel(t)
	frame := ansi.Strip(m.render())
	if !strings.Contains(frame, "✎3") || !strings.Contains(frame, "↻1") {
		t.Errorf("the row carries the chip:\n%s", frame)
	}
	if title := ansi.Strip(m.titleBar()); !strings.Contains(title, "✎ 1 to review") {
		t.Errorf("the title counts worktrees, and the staged-only one is not among them: %q", title)
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.width)
		}
	}
}

func TestVAndReviewOpenLazygit(t *testing.T) {
	m := reviewModel(t)
	m.cfg.LazygitCmd = "definitely-not-installed-lazygit"
	for name, keys := range map[string][]tea.KeyPressMsg{
		"V":       {{Code: 'V', Text: "V"}},
		":review": append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("review"), enter())...),
	} {
		got := typed(t, m, keys...)
		if !strings.Contains(got.status, "lazygit") {
			t.Errorf("%s goes where G goes (here: lazygit is missing, so it says so): status %q mode %d", name, got.status, got.mode)
		}
	}
}

func TestNextAlsoFindsAnIdleAgentThatLeftFilesToReview(t *testing.T) {
	m := nextModel(t)
	m.agents = map[string]status.Entry{"infra/main": {State: status.Idle}, "web-site/main": {State: status.Working}}
	m.git = map[string]gitinfo.Status{"infra/main": {Modified: 1}, "web-site/main": {Modified: 4}}
	if got := m.needing(); len(got) != 1 || got[0].base != "infra/main" {
		t.Fatalf("an idle agent with files to review counts; a working one does not yet: %+v", got)
	}
	m.git["infra/main"] = gitinfo.Status{Staged: 1} // you staged everything
	if got := m.needing(); len(got) != 0 {
		t.Errorf("nothing waits once it is all staged: %+v", got)
	}
}

func TestPRPopupReadsTheBranchInTheBackground(t *testing.T) {
	m := reviewModel(t)
	next, cmd := m.openPR()
	m = next.(Model)
	if m.mode != modePR || !m.prLoading || cmd == nil {
		t.Fatalf(":pr opens the popup and starts a read: mode %d loading %v", m.mode, m.prLoading)
	}
	if !strings.Contains(ansi.Strip(m.render()), "reading…") {
		t.Errorf("while it reads:\n%s", ansi.Strip(m.render()))
	}
	sum := pr.Summary{Branch: "feature/login", Base: "origin/main", Fork: "a1b2c3d", Total: 3, Files: 7, Added: 182, Removed: 41,
		Commits: []pr.Commit{{Hash: "e4f5a6b", Subject: "Add login form"}, {Hash: "9c8d7e6", Subject: "Validate the email field"}, {Hash: "2b3c4d5", Subject: "Fix the redirect"}}}
	n2, _ := m.Update(prMsg{session: "infra/main", sum: sum})
	m = n2.(Model)
	shown := ansi.Strip(m.render())
	for _, want := range []string{"Pull request · infra  main", "into origin/main · from a1b2c3d", "3 commits · 7 files · +182 −41", "e4f5a6b  Add login form", "not included: 3 files with uncommitted changes"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the popup lacks %q:\n%s", want, shown)
		}
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.width)
		}
	}
	if closed := typed(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"}); closed.mode != modeList {
		t.Errorf("any key closes it: mode %d", closed.mode)
	}
	// an answer for a worktree the cursor has left is ignored
	m.mode = modePR
	m.prLoading = true
	if stale, _ := m.Update(prMsg{session: "web-site/main", sum: sum}); !stale.(Model).prLoading {
		t.Error("a read for another worktree must not fill this popup")
	}
}

func TestPRPopupAgainstARealBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	for i, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", name)
		gitIn(t, dir, "commit", "-q", "-m", "c"+name)
		if i == 0 {
			gitIn(t, dir, "switch", "-q", "-c", "feature/y")
		}
	}
	m := reviewModel(t)
	m.rows[0].wt.Path, m.all[0].wt.Path = dir, dir
	next, cmd := m.openPR()
	msg := cmd().(prMsg)
	m = next.(Model)
	n2, _ := m.Update(msg)
	if shown := ansi.Strip(n2.(Model).render()); !strings.Contains(shown, "1 commits") || !strings.Contains(shown, "cb") {
		t.Errorf("one commit since main:\n%s", shown)
	}
}
