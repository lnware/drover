package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// RepoName is the project name a clone URL implies: its last path element
// without ".git" ("git@github.com:acme/payments.git" gives "payments").
func RepoName(url string) string {
	_, name := splitRepo(url)
	return name
}

// RepoOwner is the element before the name, usually the organisation or user
// ("acme"); empty when the URL has none.
func RepoOwner(url string) string {
	owner, _ := splitRepo(url)
	return owner
}

func splitRepo(url string) (owner, name string) {
	s := strings.TrimSpace(url)
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")
	// scp-like "git@host:owner/repo": the part after the colon is the path.
	if !strings.Contains(s, "://") {
		if i := strings.LastIndex(s, ":"); i >= 0 {
			s = s[i+1:]
		}
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' })
	switch n := len(parts); {
	case n == 0:
		return "", ""
	case n == 1 || strings.Contains(parts[n-1], ":"):
		return "", parts[n-1]
	default:
		return parts[n-2], parts[n-1]
	}
}

// CloneStepNames are the steps of a CloneJob, in order, as shown to the user.
var CloneStepNames = []string{
	"git clone --bare",
	"bare repo config",
	"git fetch origin",
	"default branch",
	"first worktree",
}

// CloneJob makes a bare-layout project from a Git URL:
//
//	root/.bare      the bare clone, with the fetch refspec a plain clone lacks
//	root/.git       a file pointing at .bare
//	root/<branch>   a worktree of the default branch
//
// It runs one step at a time so the caller can show progress, and every git
// child is cancelled by Cancel. Nothing outside root is touched, and Cleanup
// removes what the job made.
type CloneJob struct {
	URL, Root string

	ctx     context.Context
	cancel  context.CancelFunc
	created bool   // Root did not exist before the job
	branch  string // the default branch, known after step 3
}

func NewCloneJob(url, root string) *CloneJob {
	ctx, cancel := context.WithCancel(context.Background())
	return &CloneJob{URL: strings.TrimSpace(url), Root: root, ctx: ctx, cancel: cancel}
}

// Cancel stops the running git child, if any. Cleanup still has to be called.
func (j *CloneJob) Cancel() { j.cancel() }

// Cleanup removes what the job made: the whole folder when the job created
// it, otherwise just .bare and the first worktree.
func (j *CloneJob) Cleanup() {
	j.cancel()
	if j.created {
		_ = os.RemoveAll(j.Root)
		return
	}
	_ = os.RemoveAll(filepath.Join(j.Root, ".bare"))
	if j.branch != "" {
		_ = os.RemoveAll(filepath.Join(j.Root, filepath.FromSlash(j.branch)))
	}
}

// Worktree is the first worktree, once the last step has run.
func (j *CloneJob) Worktree() Worktree {
	return Worktree{Path: filepath.Join(j.Root, filepath.FromSlash(j.branch)), Branch: j.branch, Group: BaseGroup}
}

func (j *CloneJob) bare() string { return filepath.Join(j.Root, ".bare") }

// Run runs step i of CloneStepNames. A cancelled job reports context.Canceled.
func (j *CloneJob) Run(i int) error {
	if err := j.ctx.Err(); err != nil {
		return err
	}
	switch i {
	case 0:
		return j.clone()
	case 1:
		// The pointer lets git, lazygit and editors work from the project
		// root; the refspec is what makes origin/* refs exist at all.
		if err := os.WriteFile(filepath.Join(j.Root, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
			return err
		}
		return j.git(j.bare(), "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	case 2:
		return j.git(j.bare(), "fetch", "--prune", "origin")
	case 3:
		return j.defaultBranch()
	case 4:
		return j.firstWorktree()
	}
	return fmt.Errorf("no step %d", i)
}

func (j *CloneJob) clone() error {
	if j.URL == "" {
		return errors.New("type the Git URL")
	}
	if strings.HasPrefix(j.URL, "-") {
		return errors.New("that is not a Git URL")
	}
	if j.Root == "" {
		return errors.New("type the folder")
	}
	switch entries, err := os.ReadDir(j.Root); {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(j.Root, 0o755); err != nil {
			return err
		}
		j.created = true
	case err != nil:
		return err
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty", j.Root)
	}
	return j.git("", "clone", "--bare", "--", j.URL, j.bare())
}

// defaultBranch finds what the remote calls its default: the bare clone's own
// HEAD points at it, and origin/HEAD is made to agree so that BaseRef and
// the PR view find it.
func (j *CloneJob) defaultBranch() error {
	out, err := j.output(j.bare(), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	b := strings.TrimSpace(out)
	if b == "" {
		return errors.New("the repository has no default branch")
	}
	if err := j.git(j.bare(), "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+b); err != nil {
		return err
	}
	j.branch = b
	return nil
}

// firstWorktree checks out the default branch with the upstream settings
// Add gives new branches.
func (j *CloneJob) firstWorktree() error {
	path := filepath.Join(j.Root, filepath.FromSlash(j.branch))
	if err := j.git(j.bare(), "worktree", "add", path, j.branch); err != nil {
		return err
	}
	for k, v := range map[string]string{
		"branch." + j.branch + ".remote": "origin",
		"branch." + j.branch + ".merge":  "refs/heads/" + j.branch,
	} {
		if err := j.git(j.bare(), "config", k, v); err != nil {
			return err
		}
	}
	return nil
}

func (j *CloneJob) git(dir string, args ...string) error {
	_, err := j.output(dir, args...)
	return err
}

// output runs git without a terminal. A new session keeps ssh and git from
// opening /dev/tty to ask for a passphrase or a password, which would draw on
// top of the UI; they fail instead, and the error says why.
func (j *CloneJob) output(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(j.ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if cerr := j.ctx.Err(); cerr != nil {
			return "", cerr
		}
		return "", errors.New(lastLine(stderr.String(), err))
	}
	return stdout.String(), nil
}

// lastLine is the most useful line of git's complaint. The lines that only
// say the remote could not be read are skipped, since the one before them
// (ssh's "Permission denied (publickey)") is the cause.
func lastLine(stderr string, fallback error) string {
	boring := []string{
		"fatal: could not read from remote repository",
		"please make sure you have the correct access rights",
		"and the repository exists",
	}
	var keep string
	for l := range strings.SplitSeq(stderr, "\n") {
		l = strings.TrimSpace(l)
		low := strings.ToLower(strings.TrimSuffix(l, "."))
		if l == "" || slices.Contains(boring, low) {
			continue
		}
		keep = l
	}
	keep = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(keep, "fatal:"), "error:"))
	if keep == "" {
		return fallback.Error()
	}
	return keep
}
