package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Renaming a branch and moving its folder is two git operations that cannot
// be one transaction, and the folder move is the destructive-ish one (an
// agent, an editor or a shell may be sitting in it). So everything that can
// be checked is checked first, in PlanRename, and refused with a message
// before anything changes; the two steps then run in an order whose failure
// can always be undone.

// RenamePlan is a validated rename: nothing has been changed yet.
type RenamePlan struct {
	Root, GitDir         string
	OldBranch, NewBranch string
	OldPath, NewPath     string
}

// RenameResult describes a finished rename.
type RenameResult struct {
	Worktree Worktree // at its new path and branch
	OldPath  string
	OldRef   string // the old branch name
	// Upstream is "<remote>/<branch>" as the config still names it, or ""
	// when the branch has none. The remote branch is not renamed.
	Upstream string
}

// moveStep performs the folder move; a variable so tests can make it fail
// after the group folders were created.
var moveStep = func(gitDir, from, to string) error {
	_, err := git(gitDir, "worktree", "move", from, to)
	return err
}

// NewPathFor is where a worktree's folder goes when its branch is renamed,
// by the project's own convention: the folder follows the branch name, so
// feature/old in <root>/feature/old becomes feature/new in <root>/feature/new,
// and chore/new lands in <root>/chore/new (another group folder).
//
// Branches inside a group folder are often unprefixed (branch http-client in
// feature/http-client). Then the folder keeps its group and only the last
// part changes, unless the new name carries its own prefix, which the user
// then chose on purpose. A folder that matches neither shape has no
// convention drover can follow, and guessing a destination is worse than
// asking the user to do it by hand.
func NewPathFor(root string, wt Worktree, newBranch string) (string, error) {
	if wt.Branch == "" {
		return "", errors.New("a detached worktree has no branch to rename")
	}
	rel, err := filepath.Rel(root, wt.Path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is not inside the project folder", wt.Path)
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == wt.Branch: // folder follows the branch
		return filepath.Join(root, filepath.FromSlash(newBranch)), nil
	case strings.HasSuffix(rel, "/"+wt.Branch): // unprefixed branch in a group folder
		if strings.Contains(newBranch, "/") {
			return filepath.Join(root, filepath.FromSlash(newBranch)), nil
		}
		return filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(rel, wt.Branch)+newBranch)), nil
	}
	return "", fmt.Errorf("the folder %s does not follow the branch name %s, so there is no way to tell where it should move; rename the branch with git instead", rel, wt.Branch)
}

// entry is one `git worktree list --porcelain` block with the fields a
// rename must check and List does not keep.
type entry struct {
	path, branch, lockReason string
	locked, bare, detached   bool
}

func listEntries(gitDir string) ([]entry, error) {
	out, err := git(gitDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []entry
	var cur *entry
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			all = append(all, entry{path: filepath.FromSlash(strings.TrimPrefix(line, "worktree "))})
			cur = &all[len(all)-1]
		case cur == nil:
		case strings.HasPrefix(line, "branch "):
			cur.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.bare = true
		case line == "detached":
			cur.detached = true
		case line == "locked":
			cur.locked = true
		case strings.HasPrefix(line, "locked "):
			cur.locked, cur.lockReason = true, strings.TrimPrefix(line, "locked ")
		}
	}
	return all, nil
}

// samePath compares two spellings of a path (macOS /var vs /private/var).
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// CheckRenamable is the part of the checks that needs no git, so the UI can
// refuse before it opens a prompt.
func CheckRenamable(root string, wt Worktree) error {
	if wt.Branch == "" {
		return errors.New("a detached worktree has no branch to rename")
	}
	if groupOf(root, wt.Path) == BaseGroup {
		return fmt.Errorf("%s is the project's base worktree: renaming its branch changes what every other branch is based on, so do it deliberately with git branch -m", wt.Branch)
	}
	return nil
}

// PlanRename checks every precondition and returns what would happen, or an
// error saying why not. It changes nothing.
//
// The base worktree (the one directly in the project folder, or on the
// default branch) is refused on purpose: its branch is what origin/HEAD, new
// branches and every other worktree are measured against, renaming it is a
// repository-wide decision that belongs to a deliberate `git branch -m`, and
// its folder is not part of the type-folder convention this moves between.
func PlanRename(root, oldPath, newBranch string) (RenamePlan, error) {
	gitDir, err := GitDir(root)
	if err != nil {
		return RenamePlan{}, err
	}
	newBranch = strings.TrimSpace(newBranch)
	entries, err := listEntries(gitDir)
	if err != nil {
		return RenamePlan{}, err
	}
	var self *entry
	for i := range entries {
		if !entries[i].bare && samePath(entries[i].path, oldPath) {
			self = &entries[i]
		}
	}
	if self == nil {
		return RenamePlan{}, fmt.Errorf("%s is not a worktree of this project", oldPath)
	}
	old := self.branch
	if self.detached {
		old = ""
	}
	if err := CheckRenamable(root, Worktree{Path: oldPath, Branch: old}); err != nil {
		return RenamePlan{}, err
	}
	if base, err := BaseRef(gitDir); err == nil && (base == old || strings.TrimPrefix(base, "origin/") == old) {
		return RenamePlan{}, fmt.Errorf("%s is the project's base branch and is not renamed from here", old)
	}
	if self.locked {
		reason := ""
		if self.lockReason != "" {
			reason = " (" + self.lockReason + ")"
		}
		return RenamePlan{}, fmt.Errorf("the worktree is locked%s; unlock it with git worktree unlock first", reason)
	}
	for _, e := range entries {
		if e.branch == old && !samePath(e.path, oldPath) {
			return RenamePlan{}, fmt.Errorf("%s is also checked out in %s", old, e.path)
		}
	}
	if newBranch == "" {
		return RenamePlan{}, errors.New("type the new branch name")
	}
	if err := ValidateBranch(gitDir, newBranch); err != nil {
		return RenamePlan{}, err
	}
	if newBranch == old {
		return RenamePlan{}, errors.New("the new name is the same as the current one")
	}
	if refExists(gitDir, "refs/heads/"+newBranch) {
		return RenamePlan{}, fmt.Errorf("branch %s already exists", newBranch)
	}
	newPath, err := NewPathFor(root, Worktree{Path: oldPath, Branch: old}, newBranch)
	if err != nil {
		return RenamePlan{}, err
	}
	if newPath == filepath.Clean(oldPath) {
		return RenamePlan{}, errors.New("the folder would not change")
	}
	if strings.HasPrefix(newPath, filepath.Clean(oldPath)+string(filepath.Separator)) {
		return RenamePlan{}, errors.New("the new folder would be inside the current one")
	}
	if st, err := os.Lstat(newPath); err == nil {
		if o, oerr := os.Lstat(oldPath); oerr == nil && os.SameFile(st, o) {
			return RenamePlan{}, errors.New("a rename that only changes upper/lower case is not supported on this file system; rename it in two steps")
		}
		return RenamePlan{}, fmt.Errorf("%s already exists", newPath)
	}
	// git refuses to move a worktree with submodules, but only after the
	// branch was renamed; asking first avoids a pointless rollback.
	if idx, err := exec.Command("git", "-C", oldPath, "ls-files", "--stage").Output(); err == nil {
		for _, l := range strings.Split(string(idx), "\n") {
			if strings.HasPrefix(l, "160000 ") {
				return RenamePlan{}, errors.New("this worktree has submodules, which git cannot move")
			}
		}
	}
	return RenamePlan{Root: root, GitDir: gitDir, OldBranch: old, NewBranch: newBranch, OldPath: oldPath, NewPath: newPath}, nil
}

// Rename renames the branch of the worktree at oldPath and moves its folder
// to follow, or changes nothing and says why.
//
// Order: the branch first, then the folder. The branch rename is one atomic
// ref update that `git branch -m new old` exactly undoes, while the move is
// the step with the most ways to fail (permissions, a busy directory,
// submodules), so it runs second and a failure there is rolled back with the
// cheap, reliable undo. Neither step uses --force. The upstream settings are
// left as git moved them: no push happens, so they keep naming the old remote
// branch until the user pushes the new name.
func Rename(root, oldPath, newBranch string) (RenameResult, error) {
	defer invalidate(root)
	p, err := PlanRename(root, oldPath, newBranch)
	if err != nil {
		return RenameResult{}, err
	}

	// `git branch -m` moves branch.<old>.* (including the upstream) to
	// branch.<new>.* and repoints this worktree's HEAD.
	if _, err := git(p.GitDir, "branch", "-m", p.OldBranch, p.NewBranch); err != nil {
		return RenameResult{}, fmt.Errorf("git branch -m failed, nothing changed: %w", err)
	}
	undo := func(created []string) string {
		for i := len(created) - 1; i >= 0; i-- {
			os.Remove(created[i]) // only ever empty folders drover made
		}
		if _, err := git(p.GitDir, "branch", "-m", p.NewBranch, p.OldBranch); err != nil {
			return fmt.Sprintf("; the branch is still named %s, undo it with: git --git-dir=%s branch -m %s %s (%v)", p.NewBranch, p.GitDir, p.NewBranch, p.OldBranch, err)
		}
		return "; the branch rename was undone"
	}

	created, err := mkdirAll(filepath.Dir(p.NewPath))
	if err != nil {
		return RenameResult{}, fmt.Errorf("cannot create the folder for %s: %w%s", p.NewPath, err, undo(created))
	}
	if err := moveStep(p.GitDir, p.OldPath, p.NewPath); err != nil {
		return RenameResult{}, fmt.Errorf("git worktree move failed: %w%s", err, undo(created))
	}

	// The old group folder (feature/ after the last worktree left it) is
	// removed only when empty; os.Remove refuses otherwise.
	for dir := filepath.Dir(p.OldPath); dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}

	res := RenameResult{
		Worktree: Worktree{Path: p.NewPath, Branch: p.NewBranch, Group: groupOf(root, p.NewPath)},
		OldPath:  p.OldPath, OldRef: p.OldBranch,
	}
	if remote, err := git(p.GitDir, "config", "branch."+p.NewBranch+".remote"); err == nil && remote != "" {
		if merge, err := git(p.GitDir, "config", "branch."+p.NewBranch+".merge"); err == nil {
			res.Upstream = remote + "/" + strings.TrimPrefix(merge, "refs/heads/")
		}
	}
	return res, nil
}

// mkdirAll creates dir and its missing parents, returning the ones it made
// (outermost first) so a failure can remove exactly those again.
func mkdirAll(dir string) (created []string, err error) {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if st, err := os.Stat(d); err == nil {
			if !st.IsDir() {
				return nil, fmt.Errorf("%s is a file, not a folder", d)
			}
			break
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil {
			return created, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}
