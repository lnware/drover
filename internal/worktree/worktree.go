// Package worktree discovers git worktrees of a bare-repo project container:
//
//	project/
//	  .bare/
//	  main/
//	  feature/feature-a/
//
// The container itself is not a working tree, so everything goes through
// --git-dir instead of relying on a root .git file.
package worktree

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// BaseGroup is the group for worktrees that sit directly in the project
// folder (main, develop) instead of inside a type folder like feature/.
const BaseGroup = "base"

type Worktree struct {
	Path     string
	Branch   string // short name; empty when detached
	Head     string // abbreviated commit
	Detached bool
	Group    string // the type folder it sits in ("feature"), or BaseGroup
}

// Label is what the UI shows for the worktree.
func (w Worktree) Label() string {
	if w.Branch != "" {
		return w.Branch
	}
	return "(detached " + w.Head + ")"
}

// GitDir finds the git directory of a project root: .bare first, then
// whatever git itself reports for a regular repo or worktree.
func GitDir(root string) (string, error) {
	bare := filepath.Join(root, ".bare")
	if st, err := os.Stat(bare); err == nil && st.IsDir() {
		return bare, nil
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not a bare-layout project or git repo", root)
	}
	return strings.TrimSpace(string(out)), nil
}

// List returns the project's worktrees, base branches first, then by group
// and path. The bare repository entry itself is skipped.
func List(root string) ([]Worktree, error) {
	gitDir, err := GitDir(root)
	if err != nil {
		return nil, err
	}
	fp, cacheable := fingerprintOf(gitDir)
	if cacheable {
		if list, hit := lookup(root, fp); hit {
			return list, nil
		}
	}
	gitListRuns.Add(1)
	cmd := exec.Command("git", "--git-dir="+gitDir, "worktree", "list", "--porcelain")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	wts := parsePorcelain(string(out))
	normalizePaths(root, wts)
	for i := range wts {
		wts[i].Group = groupOf(root, wts[i].Path)
	}
	sort.SliceStable(wts, func(i, j int) bool {
		a, b := wts[i], wts[j]
		if (a.Group == BaseGroup) != (b.Group == BaseGroup) {
			return a.Group == BaseGroup
		}
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Path < b.Path
	})
	if cacheable {
		store(root, fp, wts)
	}
	return wts, nil
}

func parsePorcelain(out string) []Worktree {
	var wts []Worktree
	var cur Worktree
	var bare, open bool
	flush := func() {
		if open && !bare {
			wts = append(wts, cur)
		}
		cur, bare, open = Worktree{}, false, false
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = filepath.FromSlash(strings.TrimPrefix(line, "worktree "))
			open = true
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = shorten(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			bare = true
		case line == "detached":
			cur.Detached = true
		}
	}
	flush()
	return wts
}

// groupOf names the group a worktree belongs to: the folder it sits in,
// like feature/ in project/feature/feature-a. The branch name plays no part,
// because folders are the project's own convention and branches in them are
// often unprefixed (feature/http-client holding branch http-client).
func groupOf(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return BaseGroup
	}
	if dir, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && dir != "" {
		return dir
	}
	return BaseGroup
}

// normalizePaths rewrites worktree paths that git reports through a
// different spelling of the project root (macOS /var vs /private/var), so
// paths relative to root stay meaningful.
func normalizePaths(root string, wts []Worktree) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	for i := range wts {
		real, err := filepath.EvalSymlinks(wts[i].Path)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(realRoot, real); err == nil && !strings.HasPrefix(rel, "..") {
			wts[i].Path = filepath.Join(root, rel)
		}
	}
}

func shorten(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
