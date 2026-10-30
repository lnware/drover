// Package gitinfo reads the git state of one worktree for the status bar:
// branch, ahead/behind, and counts of staged, modified, untracked and
// conflicted files, the same facts gitmux shows in a tmux status line.
package gitinfo

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Status struct {
	Branch      string // empty when detached
	Head        string // abbreviated commit
	Detached    bool
	Upstream    string // e.g. origin/main; empty when none is set
	Ahead       int
	Behind      int
	Staged      int
	Modified    int
	Partial     int // files staged and then changed again: the staged part was reviewed, the rest was not
	Untracked   int
	Conflicts   int
	HasUpstream bool
}

// Dirty reports whether the working tree differs from HEAD in any way.
func (s Status) Dirty() bool {
	return s.Staged+s.Modified+s.Untracked+s.Conflicts > 0
}

// ToReview is the number of files with changes that are not staged: edited
// since the index (a file you staged and the agent changed again counts), plus
// files git does not know. Staging is how the work gets reviewed, so these are
// the ones still waiting for a look.
func (s Status) ToReview() int { return s.Modified + s.Untracked }

// timeout bounds a read; a huge repository must not stall the UI's refresh.
const timeout = 5 * time.Second

// Read returns the status of the worktree at dir. --no-optional-locks keeps
// it from taking the index lock, which would otherwise make it race with git
// commands the agent is running in the same worktree.
func Read(dir string) (Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir,
		"status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	out, err := cmd.Output()
	if err != nil {
		return Status{}, err
	}
	return parse(string(out)), nil
}

// parse reads `git status --porcelain=v2 --branch` output.
func parse(out string) Status {
	var s Status
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			s.Head = shorten(strings.TrimPrefix(line, "# branch.oid "))
		case strings.HasPrefix(line, "# branch.head "):
			if name := strings.TrimPrefix(line, "# branch.head "); name == "(detached)" {
				s.Detached = true
			} else {
				s.Branch = name
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			s.Upstream, s.HasUpstream = strings.TrimPrefix(line, "# branch.upstream "), true
		case strings.HasPrefix(line, "# branch.ab "):
			// "# branch.ab +3 -1"
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				switch f[0] {
				case '+':
					s.Ahead = n
				case '-':
					s.Behind = n
				}
			}
		case strings.HasPrefix(line, "? "):
			s.Untracked++
		case strings.HasPrefix(line, "u "):
			s.Conflicts++
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			// "1 XY ..." ordinary change, "2 XY ..." rename or copy. X is the
			// index side, Y the working tree side; '.' means unchanged.
			if len(line) >= 4 {
				if line[2] != '.' {
					s.Staged++
				}
				if line[3] != '.' {
					s.Modified++
				}
				if line[2] != '.' && line[3] != '.' {
					s.Partial++
				}
			}
		}
	}
	return s
}

func shorten(oid string) string {
	if oid == "(initial)" {
		return "initial"
	}
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}
