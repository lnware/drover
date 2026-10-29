package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// git runs one git command against gitDir and returns trimmed stdout.
func git(gitDir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--git-dir=" + gitDir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func refExists(gitDir, ref string) bool {
	_, err := git(gitDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// BaseRef picks the ref a new branch starts from: where the remote's HEAD
// points, then the usual default branch names. It does no network access, so
// the base is as fresh as the last fetch.
func BaseRef(gitDir string) (string, error) {
	if ref, err := git(gitDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && refExists(gitDir, ref) {
		return ref, nil
	}
	for _, ref := range []string{"origin/main", "origin/master", "main", "master"} {
		if refExists(gitDir, ref) {
			return ref, nil
		}
	}
	return "", errors.New("no base branch found (origin/HEAD, main or master)")
}

// ValidateBranch rejects names git would refuse, and names that would put
// the worktree outside the project.
func ValidateBranch(gitDir, branch string) error {
	if branch == "" {
		return errors.New("branch name is empty")
	}
	if _, err := git(gitDir, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("%q is not a valid branch name", branch)
	}
	return nil
}

// Add creates a worktree for a new branch at <root>/<branch>, so a branch
// named feature/a lands in feature/a/ like the rest of the project.
//
// The branch starts from BaseRef without tracking it (a branch tracking
// origin/main would push to main), and is given the same upstream settings
// as `git push -u` would, without pushing anything: pushing is outward-facing
// and stays the user's call.
func Add(root, branch string) (Worktree, error) {
	gitDir, err := GitDir(root)
	if err != nil {
		return Worktree{}, err
	}
	defer invalidate(root)
	if err := ValidateBranch(gitDir, branch); err != nil {
		return Worktree{}, err
	}
	if refExists(gitDir, "refs/heads/"+branch) {
		return Worktree{}, fmt.Errorf("branch %s already exists", branch)
	}
	path := filepath.Join(root, filepath.FromSlash(branch))
	if _, err := os.Stat(path); err == nil {
		return Worktree{}, fmt.Errorf("%s already exists", path)
	}
	base, err := BaseRef(gitDir)
	if err != nil {
		return Worktree{}, err
	}
	if _, err := git(gitDir, "worktree", "add", "--no-track", "-b", branch, path, base); err != nil {
		return Worktree{}, err
	}
	if _, err := git(gitDir, "config", "--get", "remote.origin.url"); err == nil {
		for k, v := range map[string]string{
			"branch." + branch + ".remote": "origin",
			"branch." + branch + ".merge":  "refs/heads/" + branch,
		} {
			if _, err := git(gitDir, "config", k, v); err != nil {
				return Worktree{}, fmt.Errorf("worktree created, but setting upstream failed: %w", err)
			}
		}
	}
	return Worktree{Path: path, Branch: branch, Group: groupOf(root, path)}, nil
}

// Remove deletes a worktree directory with plain `git worktree remove`, which
// refuses when there are uncommitted or untracked files. The branch is kept.
// Group folders left empty (feature/ after feature/a) are cleaned up.
func Remove(root, path string) error {
	gitDir, err := GitDir(root)
	if err != nil {
		return err
	}
	defer invalidate(root)
	if _, err := git(gitDir, "worktree", "remove", path); err != nil {
		return err
	}
	for dir := filepath.Dir(path); dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil { // not empty (or gone): stop
			break
		}
	}
	return nil
}
