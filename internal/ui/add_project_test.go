package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/projects"
)

// seedRepo is a local repository with one commit, standing in for a remote.
func seedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func addModel(t *testing.T) Model {
	t.Helper()
	m := contextModel(t) // also points DROVER_STATE_DIR at a temp dir
	m.cfg.Projects = []string{"/dev/acme/infra", "/dev/acme/web-site", "/dev/personal/garden-app"}
	for i := range m.all {
		m.all[i].root = m.cfg.Projects[i]
	}
	m.applyFilter()
	return m
}

// settleClone runs the commands a key produced until the model has nothing more to
// say about the clone: each step's result goes back in, other commands are dropped.
func settleClone(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for range 20 {
		if cmd == nil {
			return m
		}
		msg := cmd()
		done, ok := msg.(addStepDone)
		if !ok {
			return m
		}
		next, c := m.Update(done)
		m, cmd = next.(Model), c
	}
	t.Fatal("the clone never ended")
	return m
}

func TestPOpensTheAddPopupAndTheFolderFollowsTheURL(t *testing.T) {
	m := addModel(t)
	m = typed(t, m, tea.KeyPressMsg{Code: 'P', Text: "P"})
	if m.mode != modeAdd {
		t.Fatalf("P opens the popup: mode %d", m.mode)
	}
	m = typed(t, m, text("git@github.com:acme/payments.git")...)
	if got := m.addFolder(); got != "/dev/acme/payments" {
		t.Errorf("an owner with projects: folder %q, want beside them", got)
	}
	m = typed(t, m, text("")...)
	m.add.url = "https://github.com/Other-Org/tool"
	if got := m.addFolder(); got != "/dev/Other-Org/tool" {
		t.Errorf("a new owner goes under the folder that holds the orgs: %q", got)
	}
	out := ansi.Strip(strings.Join(m.addBox(), "\n"))
	for _, want := range []string{"Add project", "Git URL", "Folder", "bare repo config", "esc"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
	// Typing in the folder starts from what was shown.
	m = typed(t, m, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := m.addFolder(); got != "/dev/Other-Org/toolx" {
		t.Errorf("folder edit: %q", got)
	}
	m = typed(t, m, esc())
	if m.mode != modeList {
		t.Error("esc closes the popup")
	}
}

func TestCommandAddOpensThePopup(t *testing.T) {
	m := addModel(t)
	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("add"), enter())...)...)
	if m.mode != modeAdd {
		t.Errorf(":add: mode %d", m.mode)
	}
}

func TestCloneRegistersTheProjectAndShowsIt(t *testing.T) {
	m := addModel(t)
	seed := seedRepo(t)
	root := filepath.Join(t.TempDir(), "acme", "seed")
	m, _ = func() (Model, tea.Cmd) { n, c := m.openAdd(); return n.(Model), c }()
	m.add.url, m.add.folder, m.add.folderSet = seed, root, true

	next, cmd := m.Update(enter())
	m = next.(Model)
	if !m.add.busy {
		t.Fatalf("enter starts the clone: %+v", m.add)
	}
	m = settleClone(t, m, cmd)
	if m.mode != modeList || m.add.busy {
		t.Fatalf("a finished clone closes the popup: mode %d, %+v", m.mode, m.add)
	}
	if !slices.Contains(m.cfg.Projects, root) || !slices.Contains(projects.Load(), root) {
		t.Errorf("the project is registered: model %v, file %v", m.cfg.Projects, projects.Load())
	}
	if !strings.Contains(m.notice, "added seed") {
		t.Errorf("notice %q", m.notice)
	}
	if _, err := os.Stat(filepath.Join(root, "main", ".git")); err != nil {
		t.Errorf("the first worktree exists: %v", err)
	}
}

func TestFailedCloneLeavesNothingAndKeepsTheFields(t *testing.T) {
	m := addModel(t)
	root := filepath.Join(t.TempDir(), "acme", "nope")
	n, _ := m.openAdd()
	m = n.(Model)
	m.add.url, m.add.folder, m.add.folderSet = filepath.Join(t.TempDir(), "missing"), root, true

	next, cmd := m.Update(enter())
	m = settleClone(t, next.(Model), cmd)
	if m.mode != modeAdd || m.add.busy || !m.add.failed || m.add.err == "" {
		t.Fatalf("a failed clone stays open with the reason: %+v", m.add)
	}
	if m.add.url == "" || m.add.folder != root {
		t.Errorf("the fields survive: %+v", m.add)
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("a failed clone removes the folder it made")
	}
	if len(projects.Load()) != 0 {
		t.Errorf("nothing was registered: %v", projects.Load())
	}
	out := ansi.Strip(strings.Join(m.addBox(), "\n"))
	if !strings.Contains(out, "✗") || !strings.Contains(out, "nothing is left behind") {
		t.Errorf("the popup says where it failed:\n%s", out)
	}
	m = typed(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.add.err != "" || m.add.failed {
		t.Error("typing clears the error")
	}
}

func TestEscWhileCloningStopsAndCleansUp(t *testing.T) {
	m := addModel(t)
	seed := seedRepo(t)
	root := filepath.Join(t.TempDir(), "acme", "seed")
	n, _ := m.openAdd()
	m = n.(Model)
	m.add.url, m.add.folder, m.add.folderSet = seed, root, true

	next, cmd := m.Update(enter())
	m = next.(Model)
	// The first step is running when esc arrives; its result is the one that sees the cancel.
	m = typed(t, m, esc())
	if !m.add.cancelling || m.mode != modeAdd {
		t.Fatalf("esc while busy cancels, it does not close: %+v", m.add)
	}
	m = settleClone(t, m, cmd)
	if m.add.busy || m.add.cancelling || !strings.Contains(m.add.err, "cancelled") {
		t.Errorf("after the cancel: %+v", m.add)
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("a cancelled clone removes what it made")
	}
	if len(projects.Load()) != 0 || slices.Contains(m.cfg.Projects, root) {
		t.Error("a cancelled clone registers nothing")
	}
}

func TestAddRefusesAnExistingProjectAndABlankURL(t *testing.T) {
	m := addModel(t)
	n, _ := m.openAdd()
	m = n.(Model)
	m = typed(t, m, enter())
	if m.add.busy || m.add.err == "" {
		t.Errorf("a blank URL is refused: %+v", m.add)
	}
	m.add.url, m.add.folder, m.add.folderSet = "git@h:acme/infra.git", "/dev/acme/infra", true
	m = typed(t, m, enter())
	if m.add.busy || !strings.Contains(m.add.err, "already a project") {
		t.Errorf("a folder that is a project is refused: %+v", m.add)
	}
}
