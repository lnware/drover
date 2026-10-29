// Package status is the hand-off between Claude Code hooks and the TUI: each
// hook invocation writes one small JSON file per session, and the TUI reads
// the directory. Files are replaced atomically so a reader never sees a
// half-written one.
package status

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type State string

const (
	Idle    State = "idle"
	Working State = "working"
	Waiting State = "waiting"
	Done    State = "done"
	Error   State = "error"
)

// Valid reports whether s is a state a hook may record.
func Valid(s string) bool {
	switch State(s) {
	case Idle, Working, Waiting, Done, Error:
		return true
	}
	return false
}

type Entry struct {
	State State `json:"state"`
	// Since is when the state last changed; Updated is the last hook call.
	Since   time.Time `json:"since"`
	Updated time.Time `json:"updated"`
}

// debounce skips rewriting an unchanged state: PreToolUse and PostToolUse
// fire on every tool call and would otherwise rewrite the file each time.
const debounce = time.Second

// Root is drover's state directory: $DROVER_STATE_DIR, else the XDG state dir.
func Root() string {
	if d := os.Getenv("DROVER_STATE_DIR"); d != "" {
		return d
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "drover-state")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "drover")
}

// Dir is where the per-session status files live.
func Dir() string { return filepath.Join(Root(), "status") }

// Session names contain '/', so they are escaped into one file name.
func path(dir, session string) string {
	return filepath.Join(dir, url.PathEscape(session)+".json")
}

// Write records a session's state at now, keeping Since when the state
// did not change.
func Write(dir, session string, state State, now time.Time) error {
	prev, havePrev := read(path(dir, session))
	entry := Entry{State: state, Since: now, Updated: now}
	if havePrev && prev.State == state {
		if now.Sub(prev.Updated) < debounce {
			return nil
		}
		entry.Since = prev.Since
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
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
	return os.Rename(tmp.Name(), path(dir, session))
}

// Remove forgets a session's state; a missing file is fine.
func Remove(dir, session string) error {
	if err := os.Remove(path(dir, session)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ReadAll returns every readable entry by session name. Unreadable or
// corrupt files are skipped: the TUI would rather show less than fail.
func ReadAll(dir string) map[string]Entry {
	entries, err := os.ReadDir(dir)
	out := make(map[string]Entry, len(entries))
	if err != nil {
		return out
	}
	for _, de := range entries {
		name, ok := strings.CutSuffix(de.Name(), ".json")
		if !ok || de.IsDir() {
			continue
		}
		session, err := url.PathUnescape(name)
		if err != nil {
			continue
		}
		if e, ok := read(filepath.Join(dir, de.Name())); ok {
			out[session] = e
		}
	}
	return out
}

func read(file string) (Entry, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Entry{}, false
	}
	var e Entry
	if json.Unmarshal(data, &e) != nil || !Valid(string(e.State)) {
		return Entry{}, false
	}
	return e, true
}
