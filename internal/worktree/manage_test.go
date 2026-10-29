package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newProject builds the bare layout as documented in the README: <root>/.bare, a
// .git pointer, and a main worktree. fetchRefs mirrors the gist's refspec
// step, which creates the origin/* refs.
func newProject(t *testing.T, fetchRefs bool) (root, seed string) {
	t.Helper()
	seed = t.TempDir()
	run(t, seed, "init", "-q")
	run(t, seed, "commit", "-q", "--allow-empty", "-m", "initial")
	root = t.TempDir()
	run(t, root, "clone", "-q", "--bare", seed, filepath.Join(root, ".bare"))
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fetchRefs {
		run(t, root, "--git-dir=.bare", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
		run(t, root, "--git-dir=.bare", "fetch", "-q", "origin")
	}
	run(t, root, "--git-dir=.bare", "worktree", "add", "-q", "main", "main")
	return root, seed
}

func TestAddCreatesNestedWorktreeWithoutPushing(t *testing.T) {
	root, seed := newProject(t, true)
	// Move the seed forward; the new branch must start from the fetched origin/main.
	wt, err := Add(root, "feature/feature-a")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "feature", "feature-a"); wt.Path != want {
		t.Errorf("path = %s, want %s", wt.Path, want)
	}
	if wt.Group != "feature" {
		t.Errorf("group = %q", wt.Group)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, ".git")); err != nil {
		t.Errorf("worktree not checked out: %v", err)
	}

	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range list {
		found = found || w.Branch == "feature/feature-a"
	}
	if !found {
		t.Errorf("new worktree missing from List: %+v", list)
	}

	gd := filepath.Join(root, ".bare")
	if got := run(t, root, "--git-dir="+gd, "config", "branch.feature/feature-a.merge"); got != "refs/heads/feature/feature-a" {
		t.Errorf("upstream merge = %q: must track its own branch, not origin/main", got)
	}
	if got := run(t, root, "--git-dir="+gd, "config", "branch.feature/feature-a.remote"); got != "origin" {
		t.Errorf("upstream remote = %q", got)
	}
	if out := run(t, seed, "branch", "--list", "feature/*"); out != "" {
		t.Errorf("Add must not push; the remote now has %q", out)
	}
}

func TestAddFallsBackToLocalMainWithoutOriginRefs(t *testing.T) {
	root, _ := newProject(t, false) // bare clone with no origin/* refs
	if _, err := Add(root, "bug/bug1"); err != nil {
		t.Fatal(err)
	}
	base, _ := BaseRef(filepath.Join(root, ".bare"))
	if base != "main" {
		t.Errorf("base = %q, want local main", base)
	}
}

func TestAddRejectsBadInput(t *testing.T) {
	root, _ := newProject(t, true)
	if _, err := Add(root, "feature/x"); err != nil {
		t.Fatal(err)
	}
	for name, branch := range map[string]string{
		"empty":            "",
		"space":            "feature/a b",
		"dotdot":           "../escape",
		"trailing slash":   "feature/",
		"leading dash":     "-x",
		"already a branch": "feature/x",
		"main exists":      "main",
	} {
		if _, err := Add(root, branch); err == nil {
			t.Errorf("%s: Add(%q) succeeded, want an error", name, branch)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "..", "escape")); err == nil {
		t.Error("a worktree was created outside the project")
	}
}

func TestRemoveKeepsBranchAndCleansEmptyGroupDir(t *testing.T) {
	root, _ := newProject(t, true)
	wt, err := Add(root, "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, wt.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Error("worktree directory still exists")
	}
	if _, err := os.Stat(filepath.Join(root, "feature")); err == nil {
		t.Error("empty group directory feature/ was left behind")
	}
	if !refExists(filepath.Join(root, ".bare"), "refs/heads/feature/a") {
		t.Error("Remove must keep the branch")
	}
}

func TestRemoveRefusesDirtyWorktree(t *testing.T) {
	root, _ := newProject(t, true)
	wt, err := Add(root, "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "notes.txt"), []byte("unsaved"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, wt.Path); err == nil {
		t.Fatal("Remove deleted a worktree with untracked files")
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "notes.txt")); err != nil {
		t.Error("user's file was lost")
	}
}

// TestListGroupsByFolderForUnprefixedBranches reproduces a real-world
// project: chore/api-scripts holds branch api-scripts. t.TempDir also sits
// behind a symlink on macOS (/var -> /private/var), which git resolves.
func TestListGroupsByFolderForUnprefixedBranches(t *testing.T) {
	root, _ := newProject(t, true)
	gd := filepath.Join(root, ".bare")
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "-b", "api-scripts", "chore/api-scripts", "main")
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "-b", "feature/kevy", "feature/kevy", "main")

	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, w := range list {
		got[w.Branch] = w.Group
		if !strings.HasPrefix(w.Path, root) {
			t.Errorf("path %s not rewritten under the root %s", w.Path, root)
		}
	}
	for branch, want := range map[string]string{"main": BaseGroup, "api-scripts": "chore", "feature/kevy": "feature"} {
		if got[branch] != want {
			t.Errorf("branch %s: group %q, want %q (all: %v)", branch, got[branch], want, got)
		}
	}
	if list[0].Group != BaseGroup {
		t.Errorf("base group must sort first: %+v", list)
	}
}

func branches(t *testing.T, root string) string {
	t.Helper()
	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range list {
		names = append(names, w.Branch)
	}
	return strings.Join(names, ",")
}

func TestListIsCachedUntilSomethingChanges(t *testing.T) {
	root, _ := newProject(t, true)
	gd := filepath.Join(root, ".bare")

	start := gitListRuns.Load()
	for i := 0; i < 5; i++ {
		if got := branches(t, root); got != "main" {
			t.Fatalf("list = %q", got)
		}
	}
	if runs := gitListRuns.Load() - start; runs != 1 {
		t.Fatalf("five identical lists ran git %d times, want 1", runs)
	}

	// drover's own add and remove
	wt, err := Add(root, "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	if got := branches(t, root); got != "main,feature/a" {
		t.Errorf("after Add: %q", got)
	}

	// a worktree added behind drover's back, by plain git
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "-b", "chore/x", "chore/x", "main")
	if got := branches(t, root); !strings.Contains(got, "chore/x") {
		t.Errorf("an external worktree add was hidden by the cache: %q", got)
	}

	// a branch switched inside an existing worktree
	run(t, wt.Path, "switch", "-q", "-c", "feature/renamed")
	if got := branches(t, root); !strings.Contains(got, "feature/renamed") || strings.Contains(got, "feature/a") {
		t.Errorf("a branch switch was hidden by the cache: %q", got)
	}

	if err := Remove(root, wt.Path); err != nil {
		t.Fatal(err)
	}
	if got := branches(t, root); strings.Contains(got, "feature/renamed") {
		t.Errorf("after Remove: %q", got)
	}

	// unchanged again: back to cache hits
	before := gitListRuns.Load()
	branches(t, root)
	branches(t, root)
	if runs := gitListRuns.Load() - before; runs > 1 {
		t.Errorf("a settled project still spawns git: %d runs", runs)
	}
}

func TestCachedListIsACopy(t *testing.T) {
	root, _ := newProject(t, true)
	first, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Branch = "tampered"
	second, _ := List(root)
	if second[0].Branch != "main" {
		t.Errorf("a caller's edit leaked into the cache: %q", second[0].Branch)
	}
}

func TestFingerprintNeedsAGitDirectory(t *testing.T) {
	if _, ok := fingerprintOf(t.TempDir()); ok {
		t.Error("a directory without HEAD must not be cacheable")
	}
}
