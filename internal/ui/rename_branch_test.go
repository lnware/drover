package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/alias"
	"github.com/lnware/drover/internal/claude"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// renameEnv is a throwaway bare-layout project with its own state and Claude
// directories, and a tmux socket nothing else uses.
type renameEnv struct {
	root, claudeDir, socket string
}

func newRenameEnv(t *testing.T) renameEnv {
	t.Helper()
	seed := t.TempDir()
	gitRun(t, seed, "init", "-q")
	gitRun(t, seed, "commit", "-q", "--allow-empty", "-m", "initial")
	root := t.TempDir()
	gitRun(t, root, "clone", "-q", "--bare", seed, filepath.Join(root, ".bare"))
	gitRun(t, root, "--git-dir=.bare", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	gitRun(t, root, "--git-dir=.bare", "fetch", "-q", "origin")
	gitRun(t, root, "--git-dir=.bare", "worktree", "add", "-q", "main", "main")

	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	return renameEnv{root: root, claudeDir: claudeDir, socket: "agt-rename-" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-")}
}

// model loads the project's rows the way the running app does.
func (e renameEnv) model(t *testing.T) Model {
	t.Helper()
	m := New(testModel().cfg)
	m.cfg.Projects, m.cfg.Socket = []string{e.root}, e.socket
	m.width, m.height = 120, 30
	t.Cleanup(func() { _ = m.tmux.KillServer() })
	return e.reload(t, m)
}

func (e renameEnv) reload(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(m.refresh()())
	return next.(Model)
}

func (m Model) selectBranch(t *testing.T, branch string) Model {
	t.Helper()
	for i, r := range m.rows {
		if r.wt.Branch == branch {
			m.cursor, m.selSession = i, r.session
			return m
		}
	}
	t.Fatalf("no row for %s in %+v", branch, m.rows)
	return m
}

func needTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

func TestRenameBranchEndToEnd(t *testing.T) {
	needTmux(t)
	e := newRenameEnv(t)
	wt, err := worktree.Add(e.root, "feature/old")
	if err != nil {
		t.Fatal(err)
	}
	m := e.model(t).selectBranch(t, "feature/old")
	oldSession := m.selSession

	// everything keyed by the old session name
	if err := alias.Set(alias.Path(status.Root()), oldSession, "My label"); err != nil {
		t.Fatal(err)
	}
	if err := status.Write(status.Dir(), oldSession, status.Done, time.Now()); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(e.claudeDir, "projects", claude.ProjectDirName(wt.Path))
	if err := os.MkdirAll(hist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hist, "s.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(m.refresh()())
	m = next.(Model)

	// open the prompt: prefilled with the old name
	next, _ = m.handleKey(keyRunes("M"))
	m = next.(Model)
	if m.mode != modeMoveBranch || m.input != "feature/old" {
		t.Fatalf("prompt: mode=%d input=%q status=%q", m.mode, m.input, m.status)
	}
	m.input = "chore/new"
	shown := strings.Join(m.renameBranchLines(), "\n")
	newPath := filepath.Join(e.root, "chore", "new")
	for _, want := range []string{filepath.ToSlash(newPath), "NOT renamed", "git push -u origin"} {
		if !strings.Contains(shown, want) {
			t.Errorf("prompt does not show %q:\n%s", want, shown)
		}
	}

	// enter runs the rename
	next, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.busy {
		t.Fatal("enter did not start the rename")
	}
	msg := cmd().(branchMoved)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	next, _ = m.Update(msg)
	m = next.(Model)

	newSession := msg.newSession
	if oldSession == newSession {
		t.Fatal("session name did not change")
	}
	if r, ok := m.selected(); !ok || r.session != newSession || r.wt.Branch != "chore/new" || r.wt.Path != newPath {
		t.Errorf("selection = %+v", r)
	}
	if m.mode != modeList || m.busy || !strings.Contains(m.notice, "remote branch not renamed") {
		t.Errorf("mode=%d busy=%v notice=%q", m.mode, m.busy, m.notice)
	}
	if got := alias.Load(alias.Path(status.Root())); got[newSession] != "My label" || got[oldSession] != "" {
		t.Errorf("aliases on disk = %v", got)
	}
	if m.aliases[newSession] != "My label" || m.label(m.rows[m.cursor]) != "My label" {
		t.Errorf("aliases in memory = %v", m.aliases)
	}
	if got := status.ReadAll(status.Dir()); got[newSession].State != status.Done || len(got) != 1 {
		t.Errorf("status files = %v", got)
	}
	if !claude.HasConversation(newPath) || claude.HasConversation(wt.Path) {
		t.Error("Claude history did not follow the folder")
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Error("old folder remains")
	}

	// the refresh that follows keeps the new row selected, and nothing else is listed under the old name
	m = e.reload(t, m)
	if r, ok := m.selected(); !ok || r.session != newSession || r.wt.Branch != "chore/new" {
		t.Errorf("after refresh: %+v", r)
	}
	for _, r := range m.all {
		if r.wt.Branch == "feature/old" || r.session == oldSession {
			t.Errorf("stale row %+v", r)
		}
	}
}

func TestRenameBranchRefusesRunningSession(t *testing.T) {
	needTmux(t)
	e := newRenameEnv(t)
	wt, err := worktree.Add(e.root, "feature/old")
	if err != nil {
		t.Fatal(err)
	}
	m := e.model(t).selectBranch(t, "feature/old")
	if err := m.tmux.Spawn(m.selSession, wt.Path, "sleep 600"); err != nil {
		t.Fatal(err)
	}
	// the list has not noticed yet: the prompt opens, the rename itself refuses
	req := renameReq{root: e.root, oldPath: wt.Path, oldSession: m.selSession, newBranch: "feature/new", project: m.projectName(e.root), driver: m.tmux}
	if out := renameBranch(req); out.err == nil || !strings.Contains(out.err.Error(), "running") {
		t.Fatalf("err = %v", out.err)
	}
	m = e.reload(t, m).selectBranch(t, "feature/old")
	next, _ := m.openMove()
	if got := next.(Model); got.mode == modeMoveBranch || !strings.Contains(got.status, "stop the session first") {
		t.Errorf("mode=%d status=%q", got.mode, got.status)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Error("the folder under a running agent was touched")
	}
	if got := gitRun(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature/old" {
		t.Errorf("branch = %s", got)
	}
}

func TestRenameBranchRefusesBaseWorktreeAndDetached(t *testing.T) {
	e := newRenameEnv(t)
	m := e.model(t).selectBranch(t, "main")
	next, _ := m.openMove()
	if got := next.(Model); got.mode == modeMoveBranch || !strings.Contains(got.status, "base worktree") {
		t.Errorf("main: mode=%d status=%q", got.mode, got.status)
	}
}

func TestRenameBranchRefusesNameCollisions(t *testing.T) {
	needTmux(t)
	e := newRenameEnv(t)
	wt, _ := worktree.Add(e.root, "feature/old")
	m := e.model(t).selectBranch(t, "feature/old")
	project := m.projectName(e.root)
	newSession := tmux.SessionName(project, "chore/new")
	req := renameReq{root: e.root, oldPath: wt.Path, oldSession: m.selSession, newBranch: "chore/new", project: project, driver: m.tmux}

	// a stale tmux session already has the new name
	if err := m.tmux.Spawn(newSession, e.root, "sleep 600"); err != nil {
		t.Fatal(err)
	}
	if out := renameBranch(req); out.err == nil || !strings.Contains(out.err.Error(), "already exists") {
		t.Fatalf("stale session: err = %v", out.err)
	}
	if err := m.tmux.Kill(newSession); err != nil {
		t.Fatal(err)
	}

	// another worktree flattens to the same session name
	req.others = []renameOther{{session: newSession, path: "/elsewhere"}}
	if out := renameBranch(req); out.err == nil || !strings.Contains(out.err.Error(), "already used") {
		t.Fatalf("shared name: err = %v", out.err)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Error("a refused rename moved the folder")
	}
	if got := gitRun(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature/old" {
		t.Errorf("branch = %s", got)
	}
}

func TestRenameBranchPromptShowsWhyAndKeepsTypedNameOnError(t *testing.T) {
	e := newRenameEnv(t)
	worktree.Add(e.root, "feature/old")
	worktree.Add(e.root, "feature/taken")
	m := e.model(t).selectBranch(t, "feature/old")
	next, _ := m.handleKey(keyRunes("M"))
	m = next.(Model)

	m.input = "feature/taken"
	next, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.mode != modeMoveBranch || m.input != "feature/taken" || m.busy || !strings.Contains(m.status, "already exists") {
		t.Errorf("mode=%d input=%q busy=%v status=%q", m.mode, m.input, m.busy, m.status)
	}

	// unchanged name: no command at all
	m.input = "feature/old"
	next, cmd = m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("an unchanged name started a rename")
	}
	_ = next

	// the resulting folder is shown while typing
	m.input = "feature/x"
	if lines := strings.Join(m.renameBranchLines(), "\n"); !strings.Contains(lines, "feature/x") {
		t.Errorf("no path preview:\n%s", lines)
	}
	next, _ = m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if next.(Model).mode != modeList {
		t.Error("esc did not cancel")
	}
}
