package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDirName(t *testing.T) {
	// Names taken from a real ~/.claude/projects listing.
	for in, want := range map[string]string{
		"/Users/dev/dev/acme/infra/.bare":        "-Users-dev-dev-acme-infra--bare",
		"/Users/dev/dev/acme/infra/main":         "-Users-dev-dev-acme-infra-main",
		"/Users/dev/dev/acme/platform/feature/x": "-Users-dev-dev-acme-platform-feature-x",
	} {
		if got := ProjectDirName(in); got != want {
			t.Errorf("ProjectDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasConversation(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	work := t.TempDir()
	if HasConversation(work) {
		t.Fatal("no conversation yet, reported one")
	}
	dir := filepath.Join(cfg, "projects", ProjectDirName(work))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if HasConversation(work) {
		t.Fatal("an empty project dir is not a conversation")
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasConversation(work) {
		t.Fatal("conversation file not found")
	}
}
