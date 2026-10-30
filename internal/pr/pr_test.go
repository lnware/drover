package pr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file, body, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", file)
	run(t, dir, "commit", "-q", "-m", msg)
}

// repo has main with one commit and feature/x with two more.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "a.txt", "a\n", "first")
	run(t, dir, "switch", "-q", "-c", "feature/x")
	commit(t, dir, "b.txt", "1\n2\n3\n", "add b")
	commit(t, dir, "a.txt", "a\nmore\n", "touch a")
	return dir
}

func TestThePullRequestHoldsTheCommitsSinceTheBranchLeftMain(t *testing.T) {
	dir := repo(t)
	s, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 2 || len(s.Commits) != 2 || s.Commits[0].Subject != "touch a" || s.Commits[1].Subject != "add b" {
		t.Fatalf("two commits since main, newest first: %+v", s)
	}
	if s.Files != 2 || s.Added != 4 || s.Removed != 0 || !s.Any() || s.Branch != "feature/x" || s.Base != "main" || s.Fork == "" {
		t.Errorf("two files, four lines added: %+v", s)
	}
}

func TestUncommittedWorkIsNotPartOfIt(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "loose.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := Read(dir); s.Total != 2 || s.Files != 2 {
		t.Errorf("only commits count: %+v", s)
	}
}

func TestTheDefaultBranchADetachedHeadAndAnEmptyBranchHaveNothing(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-q", "main")
	if s, err := Read(dir); err != nil || s.Any() || s.Base != "" {
		t.Errorf("main is not a pull request: %+v %v", s, err)
	}
	run(t, dir, "switch", "-q", "-c", "feature/empty")
	if s, _ := Read(dir); s.Any() || s.Base != "main" {
		t.Errorf("a branch with no commits of its own: %+v", s)
	}
	run(t, dir, "switch", "-q", "--detach", "feature/x")
	if _, err := Read(dir); err != ErrNoBranch {
		t.Errorf("a detached HEAD has no branch: %v", err)
	}
}

func TestTheDefaultBranchIsFoundThroughOrigin(t *testing.T) {
	dir := repo(t)
	run(t, dir, "branch", "trunk", "main")
	run(t, dir, "update-ref", "refs/remotes/origin/trunk", "main")
	run(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	if got := defaultBranch(dir); got != "origin/trunk" {
		t.Errorf("origin's HEAD wins: %q", got)
	}
	if s, _ := Read(dir); s.Total != 2 || s.Base != "origin/trunk" {
		t.Errorf("the range is measured from it: %+v", s)
	}
}
