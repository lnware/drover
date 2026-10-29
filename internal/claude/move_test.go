package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func history(t *testing.T, cfg, dir string) string {
	t.Helper()
	d := filepath.Join(cfg, "projects", ProjectDirName(dir))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "s.jsonl"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMoveConversationsRenamesHistoryFolder(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	old, neu := "/p/infra/feature/old", "/p/infra/feature/new"
	src := history(t, cfg, old)

	res := MoveConversations(Spelling{old, old}, Spelling{neu, neu}, nil)
	if res.Moved != 1 || len(res.Notes) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("old history folder still there")
	}
	if !HasConversation(neu) {
		t.Error("the new path does not see the conversation")
	}
}

func TestMoveConversationsNeverOverwrites(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	old, neu := "/p/infra/feature/old", "/p/infra/feature/new"
	src := history(t, cfg, old)
	dst := history(t, cfg, neu)
	if err := os.WriteFile(filepath.Join(dst, "s.jsonl"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := MoveConversations(Spelling{old, old}, Spelling{neu, neu}, nil)
	if res.Moved != 0 || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "already has history") {
		t.Fatalf("result = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "s.jsonl")); string(b) != "theirs\n" {
		t.Errorf("existing history was changed: %q", b)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("old history was removed although it could not be moved")
	}
}

func TestMoveConversationsLeavesSharedFolders(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	// Both encode to -p-infra-feature-old-x, so Claude itself shares one folder.
	old, other := "/p/infra/feature/old-x", "/p/infra/feature-old/x"
	src := history(t, cfg, old)

	res := MoveConversations(Spelling{old, old}, Spelling{"/p/infra/feature/new", "/p/infra/feature/new"}, []string{other})
	if res.Moved != 0 || len(res.Notes) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("history another worktree uses was moved")
	}
}

func TestMoveConversationsWithoutHistoryIsANoOp(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	res := MoveConversations(Spelling{"/a/b", "/a/b"}, Spelling{"/a/c", "/a/c"}, nil)
	if res.Moved != 0 || len(res.Notes) != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestMoveConversationsTriesResolvedSpelling(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	// History recorded under the symlink-resolved path, not the given one.
	givenOld, givenNew := "/link/feature/old", "/link/feature/new"
	realOld, realNew := "/real/feature/old", "/real/feature/new"
	history(t, cfg, realOld)

	res := MoveConversations(Spelling{givenOld, realOld}, Spelling{givenNew, realNew}, nil)
	if res.Moved != 1 || !HasConversation(realNew) {
		t.Fatalf("result = %+v", res)
	}
}
