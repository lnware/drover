// Package session keeps drover's record of the agent sessions in a worktree.
//
// A worktree can run several agents. Each is a Session: a short id that is
// part of its tmux session name, the name the user (or drover) gave it, and
// the Claude Code conversation it holds, so that a stopped session can be
// resumed exactly instead of "whatever ran last in this directory". The
// records live in one small JSON file next to the other state; a session that
// is running but has no record (one started by an earlier drover) still works
// and is described by its tmux name alone.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Session is one agent session of a worktree.
type Session struct {
	ID string `json:"id"`
	// Worktree is the worktree's session key (project/branch): the part of the
	// tmux name before the id, and what a record is looked up by.
	Worktree string `json:"worktree"`
	Name     string `json:"name"`
	// Claude is the conversation id passed to the agent as --session-id, and
	// to resume it as --resume. Empty for a session whose agent is not Claude.
	Claude  string    `json:"claude,omitempty"`
	Created time.Time `json:"created"`
}

// Path is the registry file inside a state root.
func Path(root string) string { return filepath.Join(root, "sessions.json") }

// Load reads every record. A missing or corrupt file means no records: the
// list should still show, just without the names.
func Load(path string) []Session {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var all []Session
	if json.Unmarshal(data, &all) != nil {
		return nil
	}
	return slices.DeleteFunc(all, func(s Session) bool { return s.ID == "" || s.Worktree == "" })
}

// Save replaces the file atomically, so a concurrent reader never sees half
// a write.
func Save(path string, all []Session) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Add stores a record, replacing one with the same id.
func Add(path string, s Session) error {
	all := slices.DeleteFunc(Load(path), func(o Session) bool { return o.ID == s.ID })
	return Save(path, append(all, s))
}

// Remove forgets a session.
func Remove(path, id string) error {
	return Save(path, slices.DeleteFunc(Load(path), func(o Session) bool { return o.ID == id }))
}

// RemoveWorktree forgets every session of a worktree, once it is removed.
func RemoveWorktree(path, worktree string) error {
	return Save(path, slices.DeleteFunc(Load(path), func(o Session) bool { return o.Worktree == worktree }))
}

// Rename changes a session's name; it reports whether the session exists.
func Rename(path, id, name string) (bool, error) {
	all := Load(path)
	i := slices.IndexFunc(all, func(o Session) bool { return o.ID == id })
	if i < 0 {
		return false, nil
	}
	all[i].Name = name
	return true, Save(path, all)
}

// MoveWorktree points every session of one worktree at another, when the
// branch (and so the worktree's session key) is renamed.
func MoveWorktree(path, from, to string) error {
	all := Load(path)
	for i := range all {
		if all[i].Worktree == from {
			all[i].Worktree = to
		}
	}
	return Save(path, all)
}

// ForWorktree lists a worktree's records, oldest first.
func ForWorktree(all []Session, worktree string) []Session {
	var out []Session
	for _, s := range all {
		if s.Worktree == worktree {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b Session) int { return a.Created.Compare(b.Created) })
	return out
}

// idLen is the length of a session id: enough that two sessions of one
// worktree never collide, short enough to read in a tmux session list.
const idLen = 6

// NewID returns a fresh random id.
func NewID() string {
	b := make([]byte, idLen/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewUUID returns a random (version 4) UUID, the form Claude Code wants for
// --session-id.
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// sep joins a worktree key and a session id in a tmux session name. Git
// forbids '~' in branch names, so a worktree key never contains one and the
// split is unambiguous.
const sep = "~"

var idPattern = regexp.MustCompile(`^[0-9a-f]{` + fmt.Sprint(idLen) + `}$`)

// TmuxName is the tmux session name of a session.
func TmuxName(worktree, id string) string { return worktree + sep + id }

// Parse splits a tmux session name into its worktree key and session id. A
// name without an id is a session an earlier drover started: the whole name is
// the worktree and ok is false.
func Parse(name string) (worktree, id string, ok bool) {
	i := strings.LastIndex(name, sep)
	if i < 0 || !idPattern.MatchString(name[i+1:]) {
		return name, "", false
	}
	return name[:i], name[i+1:], true
}

// maxName is the longest name kept: long enough to say what a session is
// for, short enough for a list row.
const maxName = 40

// CleanName tidies a typed name: control characters dropped, runs of
// whitespace collapsed, ends trimmed, cut to maxName runes.
func CleanName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = b.Len() > 0
		case unicode.IsControl(r):
		default:
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
		}
	}
	if r := []rune(b.String()); len(r) > maxName {
		return strings.TrimSpace(string(r[:maxName]))
	}
	return b.String()
}

// DefaultName is the name given to a session when none was typed: the last
// part of the branch (feature/feature-a gives feature-a), then -2, -3 for the
// next ones in the same worktree. taken are the names already used there.
func DefaultName(branch string, taken []string) string {
	base := branch
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if base = CleanName(base); base == "" || strings.HasPrefix(base, "(") {
		base = "agent" // a detached worktree has no branch to name it after
	}
	if !slices.Contains(taken, base) {
		return base
	}
	for n := 2; ; n++ {
		if name := fmt.Sprintf("%s-%d", base, n); !slices.Contains(taken, name) {
			return name
		}
	}
}
