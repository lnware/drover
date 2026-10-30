package session

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewIDAndUUIDHaveTheirShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID()
		if !idPattern.MatchString(id) {
			t.Fatalf("id %q is not %d hex digits", id, idLen)
		}
		seen[id] = true
	}
	if len(seen) < 190 {
		t.Errorf("200 ids gave only %d distinct ones", len(seen))
	}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 50; i++ {
		if u := NewUUID(); !uuid.MatchString(u) {
			t.Fatalf("%q is not a version 4 uuid", u)
		}
	}
}

func TestTmuxNameRoundTrips(t *testing.T) {
	for _, wt := range []string{"infra/main", "infra/feature/feature-a", "p/bug/bug1", "infra/a.b"} {
		name := TmuxName(wt, "0a1b2c")
		got, id, ok := Parse(name)
		if !ok || got != wt || id != "0a1b2c" {
			t.Errorf("Parse(%q) = %q, %q, %v; want %q, 0a1b2c, true", name, got, id, ok, wt)
		}
	}
}

func TestParseTreatsAnOldSessionNameAsLegacy(t *testing.T) {
	for _, name := range []string{"infra/main", "infra/feature/a", "infra/x~notahexid", "infra/x~0a1b2", "infra/x~0A1B2C", ""} {
		got, id, ok := Parse(name)
		if ok || id != "" || got != name {
			t.Errorf("Parse(%q) = %q, %q, %v; want the whole name, no id, false", name, got, id, ok)
		}
	}
}

func TestDefaultName(t *testing.T) {
	for _, c := range []struct {
		branch string
		taken  []string
		want   string
	}{
		{"feature/feature-a", nil, "feature-a"},
		{"main", nil, "main"},
		{"bug/bug1", []string{"bug1"}, "bug1-2"},
		{"main", []string{"main", "main-2"}, "main-3"},
		{"main", []string{"main-2"}, "main"}, // a gap is filled from the bottom
		{"(detached HEAD)", nil, "agent"},
		{"", nil, "agent"},
		{"feature/" + strings.Repeat("x", 80), nil, strings.Repeat("x", maxName)},
	} {
		if got := DefaultName(c.branch, c.taken); got != c.want {
			t.Errorf("DefaultName(%q, %v) = %q, want %q", c.branch, c.taken, got, c.want)
		}
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"  fix  the\tbug \n": "fix the bug",
		"a\x1b[31mb":         "a[31mb",
		"":                   "",
		"   ":                "",
		"ärsyttävä bugi":     "ärsyttävä bugi",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryAddRenameRemoveAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "sessions.json")
	t0 := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	for i, s := range []Session{
		{ID: "bbbbbb", Worktree: "p/main", Name: "second", Created: t0.Add(time.Minute)},
		{ID: "aaaaaa", Worktree: "p/main", Name: "first", Claude: "u1", Created: t0},
		{ID: "cccccc", Worktree: "p/other", Name: "elsewhere", Created: t0},
	} {
		if err := Add(path, s); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	got := ForWorktree(Load(path), "p/main")
	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Fatalf("ForWorktree = %+v, want first then second (oldest first)", got)
	}
	if ok, err := Rename(path, "bbbbbb", "renamed"); err != nil || !ok {
		t.Fatalf("rename: %v %v", ok, err)
	}
	if ok, _ := Rename(path, "nope00", "x"); ok {
		t.Error("renaming a session that does not exist must report false")
	}
	if err := Add(path, Session{ID: "bbbbbb", Worktree: "p/main", Name: "replaced", Created: t0}); err != nil {
		t.Fatal(err)
	}
	if n := len(Load(path)); n != 3 {
		t.Errorf("adding an existing id must replace it, not duplicate: %d records", n)
	}
	if err := Remove(path, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorktree(path, "p/other"); err != nil {
		t.Fatal(err)
	}
	if left := Load(path); len(left) != 1 || left[0].ID != "bbbbbb" || left[0].Name != "replaced" {
		t.Errorf("left after removals: %+v", left)
	}
}

func TestMoveWorktreeFollowsABranchRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	for _, s := range []Session{
		{ID: "aaaaaa", Worktree: "p/feature/old", Name: "x"},
		{ID: "bbbbbb", Worktree: "p/feature/other", Name: "y"},
	} {
		if err := Add(path, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := MoveWorktree(path, "p/feature/old", "p/chore/new"); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got[0].Worktree != "p/chore/new" || got[1].Worktree != "p/feature/other" {
		t.Errorf("after the move: %+v", got)
	}
}

func TestLoadSurvivesAMissingOrBrokenFile(t *testing.T) {
	dir := t.TempDir()
	if got := Load(filepath.Join(dir, "none.json")); len(got) != 0 {
		t.Errorf("missing file: %+v", got)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(bad); len(got) != 0 {
		t.Errorf("corrupt file: %+v", got)
	}
	half := filepath.Join(dir, "half.json")
	if err := os.WriteFile(half, []byte(`[{"id":"","worktree":"p/x"},{"id":"aaaaaa","worktree":"p/x","name":"ok"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(half); len(got) != 1 || got[0].Name != "ok" {
		t.Errorf("a record without an id must be dropped: %+v", got)
	}
}
