// Package pr works out what a branch would put in a pull request: the commits
// since it left the default branch, and the size of the change.
//
// It reads only commits. Uncommitted work is not part of a pull request, and
// reviewing it is what the index is for: files you have staged are reviewed,
// files with changes since are not.
package pr

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Commit is one commit of the range.
type Commit struct {
	Hash    string // abbreviated
	Subject string
}

// Summary is what a pull request from the branch would contain.
type Summary struct {
	Branch  string
	Base    string   // the default branch it is measured against, e.g. origin/main
	Fork    string   // abbreviated commit where the branch left it
	Commits []Commit // newest first, at most maxCommits
	Total   int      // the number of commits, which may exceed len(Commits)
	Files   int
	Added   int
	Removed int
}

// Any reports whether the pull request would contain anything.
func (s Summary) Any() bool { return s.Total > 0 }

// maxCommits bounds the list a summary carries; Total still counts them all.
const maxCommits = 30

// timeout bounds one git call; a huge repository must not stall the UI.
const timeout = 5 * time.Second

// ErrNoBranch means HEAD is detached: a pull request comes from a branch.
var ErrNoBranch = errors.New("detached HEAD: not on a branch")

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func verify(dir, rev string) (string, bool) {
	out, err := git(dir, "rev-parse", "--verify", "-q", rev+"^{commit}")
	return out, err == nil && out != ""
}

func short(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}

// defaultBranch finds what a pull request would be merged into: origin's HEAD,
// else origin/main or origin/master, else the local main or master.
func defaultBranch(dir string) string {
	var candidates []string
	if ref, err := git(dir, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		candidates = append(candidates, ref)
	}
	candidates = append(candidates, "origin/main", "origin/master", "main", "master")
	for _, c := range candidates {
		if _, ok := verify(dir, c); ok {
			return c
		}
	}
	return ""
}

// Read works out what a pull request from the worktree at dir would contain.
// The default branch itself, a branch with no commits of its own, and a
// repository without a default branch give an empty summary.
func Read(dir string) (Summary, error) {
	branch, err := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return Summary{}, err
	}
	if branch == "HEAD" {
		return Summary{}, ErrNoBranch
	}
	s := Summary{Branch: branch}
	head, ok := verify(dir, "HEAD")
	if !ok {
		return s, nil // a branch with no commit yet
	}
	def := defaultBranch(dir)
	if def == "" || strings.TrimPrefix(def, "origin/") == branch {
		return s, nil
	}
	s.Base = def
	fork, err := git(dir, "merge-base", def, head)
	if err != nil || fork == "" {
		return s, nil // unrelated histories: nothing sensible to compare
	}
	s.Fork = short(fork)
	if n, err := git(dir, "rev-list", "--count", fork+".."+head); err == nil {
		s.Total, _ = strconv.Atoi(n)
	}
	if s.Total == 0 {
		return s, nil
	}
	if out, err := git(dir, "log", "--format=%h%x09%s", "-n", strconv.Itoa(maxCommits), fork+".."+head); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if hash, subject, ok := strings.Cut(line, "\t"); ok {
				s.Commits = append(s.Commits, Commit{Hash: hash, Subject: subject})
			}
		}
	}
	if out, err := git(dir, "diff", "--numstat", fork, head); err == nil {
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			s.Files++
			if a, err := strconv.Atoi(f[0]); err == nil { // "-" for a binary file
				s.Added += a
			}
			if r, err := strconv.Atoi(f[1]); err == nil {
				s.Removed += r
			}
		}
	}
	return s, nil
}
