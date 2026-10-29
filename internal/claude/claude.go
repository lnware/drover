// Package claude knows where Claude Code keeps its per-directory state, so
// drover can tell whether `claude --continue` has anything to continue.
package claude

import (
	"os"
	"path/filepath"
	"regexp"
)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ProjectDirName is how Claude Code names a working directory under
// ~/.claude/projects: every character that is not a letter or digit becomes
// '-', so /a/b/.bare is -a-b--bare.
func ProjectDirName(dir string) string {
	return nonAlnum.ReplaceAllString(dir, "-")
}

func configDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// HasConversation reports whether Claude Code stored a conversation for dir.
// The symlink-resolved path is tried too, since macOS temp and home paths
// often differ from what Claude recorded.
func HasConversation(dir string) bool {
	base := configDir()
	if base == "" {
		return false
	}
	candidates := []string{dir}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != dir {
		candidates = append(candidates, resolved)
	}
	for _, d := range candidates {
		matches, _ := filepath.Glob(filepath.Join(base, "projects", ProjectDirName(d), "*.jsonl"))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}
