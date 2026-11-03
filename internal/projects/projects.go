// Package projects keeps the project roots drover itself added (by cloning),
// in a file of its own beside the sessions registry. config.toml stays yours:
// drover never writes to it, and a hand-written list keeps its comments.
package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/lnware/drover/internal/status"
)

type file struct {
	Projects []string `json:"projects"`
}

// Path is the registry file.
func Path() string { return filepath.Join(status.Root(), "projects.json") }

// Load reads the registered roots; a missing or unreadable file is an empty
// list, since drover must start with nothing but its config.
func Load() []string {
	b, err := os.ReadFile(Path())
	if err != nil {
		return nil
	}
	var f file
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Projects
}

// Add registers root once.
func Add(root string) error {
	root = filepath.Clean(root)
	list := Load()
	if slices.Contains(list, root) {
		return nil
	}
	b, err := json.MarshalIndent(file{Projects: append(list, root)}, "", "  ")
	if err != nil {
		return err
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".projects-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Merge is the configured roots followed by the registered ones that are not
// already there and still exist on disk, so a folder deleted by hand does not
// leave a broken project behind.
func Merge(configured, registered []string) []string {
	out := slices.Clone(configured)
	for _, r := range registered {
		if slices.Contains(out, r) {
			continue
		}
		if _, err := os.Stat(r); errors.Is(err, os.ErrNotExist) {
			continue
		}
		out = append(out, r)
	}
	return out
}
