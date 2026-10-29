package claude

import (
	"fmt"
	"os"
	"path/filepath"
)

// Spelling is a directory as Claude Code may have recorded it: as given and
// with symlinks resolved (macOS home and temp paths often differ). Resolve
// the old directory before it is moved, since it is gone afterwards.
type Spelling struct{ Given, Resolved string }

// Spell captures both spellings of dir. Resolved falls back to Given when
// the directory cannot be resolved.
func Spell(dir string) Spelling {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return Spelling{dir, r}
	}
	return Spelling{dir, dir}
}

// maxProjectName is where Claude Code starts shortening long project folder
// names with a hash. drover does not know that scheme, so it does not
// pretend to: such directories are left alone.
const maxProjectName = 200

// MoveResult says what MoveConversations did, in words fit for the user.
type MoveResult struct {
	Moved int      // history folders renamed
	Notes []string // why something was left where it was
}

// MoveConversations renames Claude Code's stored conversations for a
// directory that moved from -> to, so `claude --continue` in the new place
// finds them. It runs after the directory move succeeded and only ever:
//
//   - renames a folder under <config>/projects, atomically, within that folder;
//   - when the destination does not exist, so nothing is overwritten;
//   - never deletes or copies anything, and never touches ~/.claude.json.
//
// Claude's name for a directory replaces every non-alphanumeric character, so
// different directories can share one history folder. keep lists the other
// directories that still use the old location: if any maps to the same
// folder the history is theirs too and stays put.
//
// Failures are reported in Notes, not as errors: the git rename is done and
// must not be reported as failed because of history.
func MoveConversations(from, to Spelling, keep []string) MoveResult {
	var res MoveResult
	base := configDir()
	if base == "" {
		return res
	}
	pairs := [][2]string{{from.Given, to.Given}}
	if from.Resolved != from.Given || to.Resolved != to.Given {
		pairs = append(pairs, [2]string{from.Resolved, to.Resolved})
	}
	done := map[string]bool{}
	for _, p := range pairs {
		src, dst := ProjectDirName(p[0]), ProjectDirName(p[1])
		if src == dst || done[src] {
			continue
		}
		done[src] = true
		srcDir := filepath.Join(base, "projects", src)
		st, err := os.Lstat(srcDir)
		if err != nil {
			continue // no history under this spelling
		}
		if !st.IsDir() {
			res.Notes = append(res.Notes, srcDir+" is not a folder, left alone")
			continue
		}
		if len(dst) > maxProjectName {
			res.Notes = append(res.Notes, "the new path is too long for drover to name Claude's history folder; history stays under the old path")
			continue
		}
		if shared(src, keep) {
			res.Notes = append(res.Notes, "another worktree shares the old history folder, so it stays under the old path")
			continue
		}
		dstDir := filepath.Join(base, "projects", dst)
		if _, err := os.Lstat(dstDir); err == nil {
			res.Notes = append(res.Notes, "Claude already has history for the new path, so the old history was not merged and stays under the old path")
			continue
		}
		if err := os.Rename(srcDir, dstDir); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("could not move Claude history: %v", err))
			continue
		}
		res.Moved++
	}
	return res
}

func shared(encoded string, dirs []string) bool {
	for _, d := range dirs {
		if ProjectDirName(d) == encoded {
			return true
		}
		if r, err := filepath.EvalSymlinks(d); err == nil && ProjectDirName(r) == encoded {
			return true
		}
	}
	return false
}
