// Package alias keeps display names for worktrees. An alias is only a label
// in drover's own state: the branch, the directory and the tmux session keep
// their names, so renaming can never break git or a running agent.
package alias

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Path is the aliases file inside a state root.
func Path(root string) string { return filepath.Join(root, "aliases.json") }

// Load reads session name -> alias. A missing or corrupt file means no
// aliases: the list should still show, just with branch names.
func Load(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var parsed map[string]string
	if json.Unmarshal(data, &parsed) != nil {
		return out
	}
	for k, v := range parsed {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// Set stores an alias for session, or clears it when name is empty. The file
// is replaced atomically, so a concurrent reader never sees half a write.
func Set(path, session, name string) error {
	aliases := Load(path)
	if name == "" {
		delete(aliases, session)
	} else {
		aliases[session] = name
	}
	return save(path, aliases)
}

func save(path string, aliases map[string]string) error {
	data, err := json.MarshalIndent(aliases, "", "  ")
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
