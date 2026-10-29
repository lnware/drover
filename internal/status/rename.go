package status

import (
	"errors"
	"os"
)

// Rename moves a session's status file to a new session name. It exists for
// a worktree renamed while stopped, so any file is a leftover of its last run;
// a leftover already under the new name is replaced, because hook state is
// rewritten on every spawn and holds nothing worth keeping. A missing file is
// fine.
func Rename(dir, from, to string) error {
	if from == to {
		return nil
	}
	if err := os.Rename(path(dir, from), path(dir, to)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
