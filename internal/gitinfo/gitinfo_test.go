package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseCountsEverything(t *testing.T) {
	out := `# branch.oid 1234567890abcdef1234567890abcdef12345678
# branch.head feature/x
# branch.upstream origin/feature/x
# branch.ab +3 -1
1 M. N... 100644 100644 100644 aaa bbb staged.go
1 .M N... 100644 100644 100644 aaa bbb modified.go
1 MM N... 100644 100644 100644 aaa bbb both.go
2 R. N... 100644 100644 100644 aaa bbb R100 new.go	old.go
u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.go
? new1.txt
? new2.txt
? dir/new3.txt
`
	got := parse(out)
	want := Status{
		Branch: "feature/x", Head: "1234567", Upstream: "origin/feature/x", HasUpstream: true,
		Ahead: 3, Behind: 1,
		Staged: 3, Modified: 2, Partial: 1, // staged.go, both.go, new.go | modified.go, both.go | both.go
		Untracked: 3, Conflicts: 1,
	}
	if got != want {
		t.Errorf("parse =\n %+v\nwant\n %+v", got, want)
	}
	if !got.Dirty() {
		t.Error("Dirty() = false")
	}
	if n := got.ToReview(); n != 5 { // modified.go, both.go and the three untracked files
		t.Errorf("ToReview() = %d, want 5", n)
	}
}

func TestParseCleanDetachedAndNoUpstream(t *testing.T) {
	clean := parse("# branch.oid abcdef1234567\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +0 -0\n")
	if clean.Dirty() || clean.Ahead != 0 || !clean.HasUpstream {
		t.Errorf("clean = %+v", clean)
	}
	detached := parse("# branch.oid abcdef1234567\n# branch.head (detached)\n")
	if !detached.Detached || detached.Branch != "" || detached.Head != "abcdef1" {
		t.Errorf("detached = %+v", detached)
	}
	if detached.HasUpstream {
		t.Error("a detached HEAD has no upstream")
	}
	fresh := parse("# branch.oid (initial)\n# branch.head main\n")
	if fresh.Head != "initial" || fresh.Branch != "main" {
		t.Errorf("unborn branch = %+v", fresh)
	}
}

func TestParseIgnoresGarbage(t *testing.T) {
	if got := parse("\n\nnot git output\n1 \n# branch.ab +x -y\n"); got.Dirty() {
		t.Errorf("garbage produced %+v", got)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "init.defaultBranch=main", "-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadRealRepository covers what parsing alone cannot: that the flags
// we pass produce the output we parse, ahead/behind against a real remote.
func TestReadRealRepository(t *testing.T) {
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	work := t.TempDir()
	git(t, work, "init", "-q")
	write(t, work, "a.txt", "1\n")
	write(t, work, "b.txt", "1\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "one")
	git(t, work, "remote", "add", "origin", remote)
	git(t, work, "push", "-q", "-u", "origin", "main")

	s, err := Read(work)
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch != "main" || s.Dirty() || !s.HasUpstream || s.Upstream != "origin/main" || s.Ahead != 0 {
		t.Fatalf("clean pushed repo = %+v", s)
	}

	git(t, work, "commit", "-q", "--allow-empty", "-m", "local only") // ahead 1
	write(t, work, "a.txt", "2\n")                                    // modified
	write(t, work, "b.txt", "2\n")                                    // modified, then staged
	git(t, work, "add", "b.txt")                                      // staged
	write(t, work, "new.txt", "x\n")                                  // untracked
	s, err = Read(work)
	if err != nil {
		t.Fatal(err)
	}
	if s.Ahead != 1 || s.Behind != 0 || s.Staged != 1 || s.Modified != 1 || s.Untracked != 1 {
		t.Errorf("dirty repo = %+v, want ahead 1, staged 1, modified 1, untracked 1", s)
	}
}

func TestReadFailsOutsideARepo(t *testing.T) {
	if _, err := Read(t.TempDir()); err == nil {
		t.Error("Read of a non-repository succeeded")
	}
}

// TestReadTakesNoIndexLock proves the point of --no-optional-locks: a held
// index lock must not make status fail, as it would for a plain git status
// racing an agent's own git command.
func TestReadTakesNoIndexLock(t *testing.T) {
	work := t.TempDir()
	git(t, work, "init", "-q")
	write(t, work, "a.txt", "1\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "one")
	write(t, work, "a.txt", "2\n") // stat-dirty, so a plain status would want to refresh the index
	if err := os.WriteFile(filepath.Join(work, ".git", "index.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Read(work)
	if err != nil {
		t.Fatalf("Read failed while the index was locked: %v", err)
	}
	if s.Modified != 1 {
		t.Errorf("modified = %d, want 1", s.Modified)
	}
}
