package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoNameAndOwner(t *testing.T) {
	for url, want := range map[string][2]string{
		"git@github.com:acme/payments.git":          {"acme", "payments"},
		"https://github.com/acme/payments":          {"acme", "payments"},
		"https://github.com/acme/payments.git/":     {"acme", "payments"},
		"ssh://git@host:2222/acme/payments.git":     {"acme", "payments"},
		"https://gitlab.com/group/sub/payments.git": {"sub", "payments"},
		"  git@github.com:acme/payments.git  ":      {"acme", "payments"},
		"/srv/git/payments.git":                     {"git", "payments"},
		"payments":                                  {"", "payments"},
		"":                                          {"", ""},
	} {
		if o, n := RepoOwner(url), RepoName(url); o != want[0] || n != want[1] {
			t.Errorf("%q: owner %q name %q, want %q %q", url, o, n, want[0], want[1])
		}
	}
}

func runAll(t *testing.T, j *CloneJob) error {
	t.Helper()
	for i := range CloneStepNames {
		if err := j.Run(i); err != nil {
			return err
		}
	}
	return nil
}

func TestCloneJobMakesTheBareLayout(t *testing.T) {
	seed := t.TempDir()
	run(t, seed, "init", "-q")
	run(t, seed, "commit", "-q", "--allow-empty", "-m", "initial")
	root := filepath.Join(t.TempDir(), "acme", "payments")

	j := NewCloneJob(seed, root)
	if err := runAll(t, j); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".git")); string(b) != "gitdir: ./.bare\n" {
		t.Errorf(".git pointer = %q", b)
	}
	bare := filepath.Join(root, ".bare")
	if got := run(t, root, "--git-dir="+bare, "config", "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Errorf("fetch refspec = %q", got)
	}
	if got := run(t, bare, "rev-parse", "--verify", "refs/remotes/origin/main"); got == "" {
		t.Error("origin/main does not exist: the fetch refspec did nothing")
	}
	if got := run(t, bare, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/main" {
		t.Errorf("origin/HEAD = %q", got)
	}
	if wt := j.Worktree(); wt.Branch != "main" || wt.Path != filepath.Join(root, "main") {
		t.Errorf("worktree = %+v", wt)
	}
	wts, err := List(root)
	if err != nil || len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("List = %+v, %v", wts, err)
	}
	if got := run(t, bare, "config", "branch.main.merge"); got != "refs/heads/main" {
		t.Errorf("upstream merge = %q", got)
	}
	// A new branch starts from a ref that updates, which is the point.
	if _, err := Add(root, "feature/x"); err != nil {
		t.Errorf("Add after Clone: %v", err)
	}
}

func TestCloneJobRefusesAndCleansUp(t *testing.T) {
	seed := t.TempDir()
	run(t, seed, "init", "-q")
	run(t, seed, "commit", "-q", "--allow-empty", "-m", "initial")

	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewCloneJob(seed, full).Run(0); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("clone into a folder with files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(full, ".bare")); err == nil {
		t.Error("a refused clone left .bare behind")
	}

	// A failing URL leaves nothing the job made, and an existing empty folder stays.
	empty := t.TempDir()
	j := NewCloneJob(filepath.Join(t.TempDir(), "missing"), empty)
	if err := j.Run(0); err == nil {
		t.Fatal("cloning a missing repository must fail")
	}
	j.Cleanup()
	if entries, err := os.ReadDir(empty); err != nil || len(entries) != 0 {
		t.Errorf("empty folder after cleanup: %v, %v", entries, err)
	}

	made := filepath.Join(t.TempDir(), "a", "b")
	j = NewCloneJob(filepath.Join(t.TempDir(), "missing"), made)
	_ = j.Run(0)
	j.Cleanup()
	if _, err := os.Stat(made); err == nil {
		t.Error("a folder the job created must be removed")
	}

	for _, bad := range []string{"", "-oProxyCommand=x"} {
		if err := NewCloneJob(bad, filepath.Join(t.TempDir(), "p")).Run(0); err == nil {
			t.Errorf("URL %q accepted", bad)
		}
	}
}

func TestCancelledJobDoesNotRun(t *testing.T) {
	j := NewCloneJob("x", filepath.Join(t.TempDir(), "p"))
	j.Cancel()
	if err := j.Run(0); err == nil {
		t.Error("a cancelled job ran a step")
	}
}

func TestLastLinePrefersTheCauseOverGitsSummary(t *testing.T) {
	got := lastLine("git@h: Permission denied (publickey).\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n", nil)
	if got != "git@h: Permission denied (publickey)." {
		t.Errorf("got %q", got)
	}
	if got := lastLine("fatal: repository 'x' does not exist\n", nil); got != "repository 'x' does not exist" {
		t.Errorf("got %q", got)
	}
}
