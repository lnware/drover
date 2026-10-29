package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapshot captures everything a refused or rolled-back rename must leave
// exactly as it was: refs, worktree registrations, branch config, folders.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	gd := "--git-dir=" + filepath.Join(root, ".bare")
	var b strings.Builder
	b.WriteString(run(t, root, gd, "for-each-ref", "refs/heads"))
	b.WriteString(run(t, root, gd, "worktree", "list", "--porcelain"))
	if out, err := git(filepath.Join(root, ".bare"), "config", "--get-regexp", `^branch\.`); err == nil {
		b.WriteString(out)
	}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == ".bare" {
			return filepath.SkipDir
		}
		if err == nil && d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if err == nil {
			b.WriteString("\n" + p)
		}
		return nil
	})
	return b.String()
}

func cfg(t *testing.T, root, key string) string {
	t.Helper()
	out, err := git(filepath.Join(root, ".bare"), "config", "--get", key)
	if err != nil {
		return ""
	}
	return out
}

func TestRenameMovesBranchFolderAndUpstreamConfig(t *testing.T) {
	root, seed := newProject(t, true)
	wt, err := Add(root, "feature/old")
	if err != nil {
		t.Fatal(err)
	}
	// Uncommitted work travels with the folder; a commit stays on the branch.
	if err := os.WriteFile(filepath.Join(wt.Path, "wip.txt"), []byte("unsaved"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, wt.Path, "commit", "-q", "--allow-empty", "-m", "work")
	sha := run(t, wt.Path, "rev-parse", "HEAD")
	List(root) // warm the cache: Rename must not leave it stale

	res, err := Rename(root, wt.Path, "chore/new")
	if err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(root, "chore", "new")
	if res.Worktree.Path != newPath || res.Worktree.Branch != "chore/new" || res.Worktree.Group != "chore" {
		t.Errorf("result = %+v", res)
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Error("old folder is still there")
	}
	if _, err := os.Stat(filepath.Join(root, "feature")); err == nil {
		t.Error("the emptied old group folder feature/ was left behind")
	}
	if b, err := os.ReadFile(filepath.Join(newPath, "wip.txt")); err != nil || string(b) != "unsaved" {
		t.Errorf("uncommitted file did not move along: %v %q", err, b)
	}
	if got := run(t, newPath, "rev-parse", "--abbrev-ref", "HEAD"); got != "chore/new" {
		t.Errorf("worktree HEAD = %q", got)
	}
	if got := run(t, newPath, "rev-parse", "HEAD"); got != sha {
		t.Errorf("the commit changed: %s != %s", got, sha)
	}
	gd := filepath.Join(root, ".bare")
	if refExists(gd, "refs/heads/feature/old") {
		t.Error("old branch still exists")
	}

	// git branch -m carries the upstream settings; they keep naming the old
	// remote branch, because nothing was pushed or renamed there.
	if cfg(t, root, "branch.chore/new.remote") != "origin" || cfg(t, root, "branch.chore/new.merge") != "refs/heads/feature/old" {
		t.Errorf("config not carried: remote=%q merge=%q", cfg(t, root, "branch.chore/new.remote"), cfg(t, root, "branch.chore/new.merge"))
	}
	if cfg(t, root, "branch.feature/old.remote") != "" || cfg(t, root, "branch.feature/old.merge") != "" {
		t.Error("config under the old branch name was left behind")
	}
	if res.Upstream != "origin/feature/old" {
		t.Errorf("Upstream = %q", res.Upstream)
	}
	if out := run(t, seed, "branch", "--list"); strings.Contains(out, "chore") {
		t.Errorf("Rename must not push; the remote has %q", out)
	}

	// cache: the list shows the new state at once
	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range list {
		if w.Branch == "feature/old" {
			t.Errorf("stale row %+v", w)
		}
		found = found || (w.Branch == "chore/new" && w.Path == newPath && w.Group == "chore")
	}
	if !found {
		t.Errorf("renamed worktree missing from List: %+v", list)
	}
}

func TestRenameWithinTheSameGroupAndIntoANewOne(t *testing.T) {
	root, _ := newProject(t, true)
	a, _ := Add(root, "feature/a")
	b, _ := Add(root, "feature/b") // keeps feature/ alive
	if _, err := Rename(root, a.Path, "feature/a2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "feature", "a2")); err != nil {
		t.Errorf("same-group move: %v", err)
	}
	if _, err := Rename(root, filepath.Join(root, "feature", "a2"), "deep/er/a2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "deep", "er", "a2")); err != nil {
		t.Errorf("nested new group: %v", err)
	}
	if _, err := os.Stat(b.Path); err != nil {
		t.Error("a sibling worktree was disturbed")
	}
}

// A real-world layout: chore/api-scripts holds the unprefixed branch
// api-scripts. The folder keeps its group unless the new name has a prefix.
func TestRenameUnprefixedBranchKeepsItsGroupFolder(t *testing.T) {
	root, _ := newProject(t, true)
	gd := filepath.Join(root, ".bare")
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "-b", "api-scripts", "chore/api-scripts", "main")
	path := filepath.Join(root, "chore", "api-scripts")

	if got, err := NewPathFor(root, Worktree{Path: path, Branch: "api-scripts"}, "api-tools"); err != nil || got != filepath.Join(root, "chore", "api-tools") {
		t.Errorf("plain new name: %s %v", got, err)
	}
	if got, err := NewPathFor(root, Worktree{Path: path, Branch: "api-scripts"}, "feature/api-tools"); err != nil || got != filepath.Join(root, "feature", "api-tools") {
		t.Errorf("prefixed new name picks its own group: %s %v", got, err)
	}
	res, err := Rename(root, path, "api-tools")
	if err != nil {
		t.Fatal(err)
	}
	if res.Worktree.Path != filepath.Join(root, "chore", "api-tools") || res.Worktree.Group != "chore" {
		t.Errorf("result = %+v", res.Worktree)
	}
	if res.Upstream != "" {
		t.Errorf("a branch without upstream reported %q", res.Upstream)
	}
}

func TestRenameRefusesAndChangesNothing(t *testing.T) {
	root, _ := newProject(t, true)
	gd := filepath.Join(root, ".bare")
	old, _ := Add(root, "feature/old")
	Add(root, "feature/taken")
	Add(root, "feature/twin")
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "-b", "stray", "feature/stray-name", "main")
	// a branch that exists without a worktree, and a folder in the way
	run(t, root, "--git-dir="+gd, "branch", "chore/branch-only")
	if err := os.MkdirAll(filepath.Join(root, "chore", "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a locked worktree
	locked, _ := Add(root, "feature/locked")
	run(t, root, "--git-dir="+gd, "worktree", "lock", "--reason", "on usb", locked.Path)
	// a detached one
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "--detach", "feature/det", "main")
	// the same branch checked out twice (git needs --force for that)
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "--force", "chore/twin", "feature/twin")

	before := snapshot(t, root)
	for _, c := range []struct {
		name, path, branch, want string
	}{
		{"base worktree", filepath.Join(root, "main"), "release", "base worktree"},
		{"empty", old.Path, "  ", "type the new branch name"},
		{"invalid", old.Path, "feature/a b", "not a valid branch name"},
		{"dotdot", old.Path, "../escape", "not a valid branch name"},
		{"same name", old.Path, "feature/old", "same as the current"},
		{"branch exists", old.Path, "feature/taken", "already exists"},
		{"branch exists without worktree", old.Path, "chore/branch-only", "already exists"},
		{"checked out twice", filepath.Join(root, "feature", "twin"), "feature/new", "also checked out"},
		{"path exists", filepath.Join(root, "feature", "taken"), "chore/in-the-way", "already exists"},
		{"folder does not follow the branch", filepath.Join(root, "feature", "stray-name"), "new", "does not follow the branch"},
		{"locked", locked.Path, "feature/unlocked", "locked (on usb)"},
		{"detached", filepath.Join(root, "feature", "det"), "feature/x", "detached"},
		{"not a worktree", filepath.Join(root, "nowhere"), "feature/x", "not a worktree"},
	} {
		_, err := Rename(root, c.path, c.branch)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", c.name, err, c.want)
		}
	}
	if after := snapshot(t, root); after != before {
		t.Errorf("a refused rename changed state:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

func TestRenameRefusesTheProjectsBaseBranchInAGroupFolder(t *testing.T) {
	root, _ := newProject(t, true)
	gd := filepath.Join(root, ".bare")
	// a second checkout of the default branch under a type folder
	run(t, root, "--git-dir="+gd, "worktree", "add", "-q", "--force", "feature/main-copy", "main")
	if _, err := Rename(root, filepath.Join(root, "feature", "main-copy"), "feature/x"); err == nil {
		t.Error("renaming the base branch was allowed")
	}
}

func TestRenameRefusesSubmodulesBeforeTouchingAnything(t *testing.T) {
	root, _ := newProject(t, true)
	wt, _ := Add(root, "feature/sub")
	other := t.TempDir()
	run(t, other, "init", "-q")
	run(t, other, "commit", "-q", "--allow-empty", "-m", "sub")
	run(t, wt.Path, "-c", "protocol.file.allow=always", "submodule", "add", "-q", other, "vendor")
	run(t, wt.Path, "commit", "-q", "-m", "add submodule")

	before := snapshot(t, root)
	_, err := Rename(root, wt.Path, "feature/sub2")
	if err == nil || !strings.Contains(err.Error(), "submodules") {
		t.Fatalf("err = %v", err)
	}
	if snapshot(t, root) != before {
		t.Error("state changed")
	}
}

func TestRenameRollsBackTheBranchWhenTheMoveFails(t *testing.T) {
	root, _ := newProject(t, true)
	wt, _ := Add(root, "feature/old")
	before := snapshot(t, root)

	orig := moveStep
	defer func() { moveStep = orig }()
	moveStep = func(gitDir, from, to string) error { return errors.New("simulated move failure") }

	// "fresh/group" does not exist yet: the folders made for the move must go too.
	_, err := Rename(root, wt.Path, "fresh/group/new")
	if err == nil || !strings.Contains(err.Error(), "simulated move failure") || !strings.Contains(err.Error(), "undone") {
		t.Fatalf("err = %v", err)
	}
	if after := snapshot(t, root); after != before {
		t.Errorf("rollback left a difference:\n--- before\n%s\n--- after\n%s", before, after)
	}
	if cfg(t, root, "branch.feature/old.merge") != "refs/heads/feature/old" || cfg(t, root, "branch.fresh/group/new.merge") != "" {
		t.Error("branch config was not restored")
	}
	if got := run(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature/old" {
		t.Errorf("worktree HEAD = %q after rollback", got)
	}
}

func TestRenameRollsBackWhenGitItselfCannotMove(t *testing.T) {
	root, _ := newProject(t, true)
	wt, _ := Add(root, "feature/old")
	// an existing but read-only group folder: creating it is not needed, the
	// real `git worktree move` is what fails.
	group := filepath.Join(root, "chore")
	if err := os.Mkdir(group, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(group, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(group, 0o755)
	if f, err := os.Create(filepath.Join(group, "probe")); err == nil { // running as root: cannot test
		f.Close()
		t.Skip("directory permissions are not enforced here")
	}
	before := snapshot(t, root)
	if _, err := Rename(root, wt.Path, "chore/new"); err == nil {
		t.Fatal("expected a failure")
	} else if !strings.Contains(err.Error(), "undone") {
		t.Errorf("err = %v", err)
	}
	if after := snapshot(t, root); after != before {
		t.Errorf("state changed:\n%s\n---\n%s", before, after)
	}
}

func TestRenameFailureOfFolderCreationRollsBack(t *testing.T) {
	root, _ := newProject(t, true)
	wt, _ := Add(root, "feature/old")
	// a file where the group folder must go
	if err := os.WriteFile(filepath.Join(root, "chore"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)
	// the plan itself passes: chore/new does not exist (chore is a file)
	_, err := Rename(root, wt.Path, "chore/new")
	if err == nil || !strings.Contains(err.Error(), "is a file") || !strings.Contains(err.Error(), "undone") {
		t.Fatalf("err = %v", err)
	}
	if after := snapshot(t, root); after != before {
		t.Errorf("state changed after %v:\n%s\n---\n%s", err, before, after)
	}
}

func TestNewPathForShapes(t *testing.T) {
	root := "/p"
	for _, c := range []struct {
		path, branch, next, want string
		err                      bool
	}{
		{"/p/feature/old", "feature/old", "feature/new", "/p/feature/new", false},
		{"/p/feature/old", "feature/old", "chore/new", "/p/chore/new", false},
		{"/p/feature/old", "feature/old", "plain", "/p/plain", false},
		{"/p/feature/http", "http", "http2", "/p/feature/http2", false},
		{"/p/feature/http", "http", "chore/http", "/p/chore/http", false},
		{"/p/feature/odd", "other", "x", "", true},
		{"/elsewhere/x", "x", "y", "", true},
		{"/p", "main", "y", "", true},
	} {
		got, err := NewPathFor(root, Worktree{Path: c.path, Branch: c.branch}, c.next)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%s %s -> %s: got %q err=%v, want %q err=%v", c.path, c.branch, c.next, got, err, c.want, c.err)
		}
	}
	if _, err := NewPathFor(root, Worktree{Path: "/p/x/y"}, "z"); err == nil {
		t.Error("a detached worktree has no branch")
	}
}
