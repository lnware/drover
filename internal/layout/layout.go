// Package layout keeps the list width the user dragged the divider to. Like
// the folds it is only drover's own state: losing the file just brings back
// the configured or automatic width, so a missing or corrupt file must never
// get in the way.
package layout

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Path is the layout file inside a state root.
func Path(root string) string { return filepath.Join(root, "layout.json") }

type file struct {
	ListWidth int `json:"list_width"`
}

// Load reads the dragged list width. A missing or corrupt file, or a width
// that is not positive, means none was chosen: 0.
func Load(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var f file
	if json.Unmarshal(data, &f) != nil || f.ListWidth < 0 {
		return 0
	}
	return f.ListWidth
}

// Save stores the list width, replacing the file atomically so a second
// drover never reads half a write. A width of 0 forgets the choice by
// removing the file.
func Save(path string, listWidth int) error {
	if listWidth <= 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(file{ListWidth: listWidth}, "", "  ")
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
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
