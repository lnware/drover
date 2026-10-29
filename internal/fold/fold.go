// Package fold keeps which list sections the user has collapsed. Like the
// aliases it is only drover's own state: losing the file just unfolds
// everything, so a missing or corrupt file must never get in the way.
package fold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Path is the folds file inside a state root.
func Path(root string) string { return filepath.Join(root, "folds.json") }

type file struct {
	Collapsed []string `json:"collapsed"`
}

// Load reads the set of collapsed section ids. A missing or corrupt file
// means nothing is collapsed.
func Load(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f file
	if json.Unmarshal(data, &f) != nil {
		return out
	}
	for _, id := range f.Collapsed {
		if id != "" {
			out[id] = true
		}
	}
	return out
}

// Save writes the collapsed ids sorted (so the file diffs cleanly) and
// replaces the file atomically, so a second drover never reads half a write.
func Save(path string, collapsed map[string]bool) error {
	f := file{Collapsed: []string{}}
	for id, on := range collapsed {
		if on {
			f.Collapsed = append(f.Collapsed, id)
		}
	}
	sort.Strings(f.Collapsed)
	data, err := json.MarshalIndent(f, "", "  ")
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
