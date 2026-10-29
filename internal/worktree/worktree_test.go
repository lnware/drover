package worktree

import "testing"

const sample = `worktree /p/infra/.bare
bare

worktree /p/infra/main
HEAD f66bd82aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
branch refs/heads/main

worktree /p/infra/feature/feature-a
HEAD 1234567890abcdef1234567890abcdef12345678
branch refs/heads/feature/feature-a

worktree /p/infra/bug/bug1
HEAD abcdef1234567890abcdef1234567890abcdef12
detached
`

func TestParsePorcelain(t *testing.T) {
	wts := parsePorcelain(sample)
	if len(wts) != 3 {
		t.Fatalf("want 3 worktrees (bare skipped), got %d: %+v", len(wts), wts)
	}
	want := []struct{ path, branch string }{
		{"/p/infra/main", "main"},
		{"/p/infra/feature/feature-a", "feature/feature-a"},
		{"/p/infra/bug/bug1", ""},
	}
	for i, w := range want {
		got := wts[i]
		if got.Path != w.path || got.Branch != w.branch {
			t.Errorf("worktree %d = %+v, want %+v", i, got, w)
		}
	}
	if !wts[2].Detached || wts[2].Head != "abcdef1" {
		t.Errorf("detached worktree = %+v", wts[2])
	}
}

// TestGroupOfUsesFolderNotBranch pins the layout the user actually has:
// worktrees in type folders whose branches are not necessarily prefixed.
func TestGroupOfUsesFolderNotBranch(t *testing.T) {
	const root = "/p/tools"
	for path, want := range map[string]string{
		"/p/tools/main":                BaseGroup,
		"/p/tools/chore/api-scripts":   "chore",   // branch api-scripts
		"/p/tools/feature/http-client": "feature", // branch http-client
		"/p/tools/feature/team/deeper": "feature", // deeper nesting keeps the top folder
		"/p/tools":                     BaseGroup,
		"/elsewhere/feature/x":         BaseGroup, // not under the project at all
		"/p/tools-other/feature/x":     BaseGroup,
	} {
		if got := groupOf(root, path); got != want {
			t.Errorf("groupOf(%s) = %q, want %q", path, got, want)
		}
	}
}
