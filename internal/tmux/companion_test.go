package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCompanionNamesNeverCollideWithWorktreeSessions(t *testing.T) {
	for _, session := range []string{
		"infra/main",
		"acme/infra/feature/a_b",
		"infra/100%/x",
		"infra/a%2Fb", // looks like an escape already
	} {
		for _, kind := range companionKinds {
			name := CompanionName(kind, session)
			if strings.Contains(name, "/") {
				t.Errorf("%q contains a slash, so it could equal a worktree session", name)
			}
			if name != sanitize(name) {
				t.Errorf("%q has characters tmux would rewrite", name)
			}
			k, s, ok := ParseCompanion(name)
			if !ok || k != kind || s != session {
				t.Errorf("ParseCompanion(%q) = %q, %q, %v; want %q, %q", name, k, s, ok, kind, session)
			}
		}
		// A worktree session is never read as a companion, whatever its text.
		if IsCompanion(session) {
			t.Errorf("worktree session %q read as a companion", session)
		}
	}
	if IsCompanion("term@x") != true || IsCompanion("other@x") || IsCompanion("plain") {
		t.Error("only <kind>@<name> without a slash is a companion")
	}
	// A project that happens to be called "term@x" still yields a slash.
	if IsCompanion(SessionName("term@x", "main")) {
		t.Error("a project named like a companion must not turn its worktrees into companions")
	}
}

func companionDriver(t *testing.T) Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := New(fmt.Sprintf("agt-companion-test-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "_")))
	t.Cleanup(func() { _ = d.KillServer() })
	return d
}

func TestListSessionsSeparatesCompanionsAndSessionsHidesThem(t *testing.T) {
	d := companionDriver(t)
	dir := t.TempDir()
	if err := d.Spawn("infra/main", dir, "cat"); err != nil {
		t.Fatal(err)
	}
	comp := CompanionName("term", "infra/main")
	if err := d.SpawnCompanion(comp, dir, "cat", "terminal | infra/main"); err != nil {
		t.Fatal(err)
	}
	agents, companions, err := d.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(agents, []string{"infra/main"}) || !slices.Equal(companions, []string{comp}) {
		t.Errorf("agents = %v, companions = %v", agents, companions)
	}
	if got, _ := d.Sessions(); !slices.Equal(got, []string{"infra/main"}) {
		t.Errorf("Sessions must not return companions: %v", got)
	}
	if !d.Has(comp) {
		t.Error("companion not found by exact name")
	}
	// A companion is its own session: it runs in the directory and has no
	// DROVER_SESSION, so hooks cannot report agent state under it.
	sh := CompanionName("edit", "infra/main")
	if err := d.SpawnCompanion(sh, dir, "sh", "editor | infra/main"); err != nil {
		t.Fatal(err)
	}
	if err := d.SendText(sh, "echo S=${DROVER_SESSION:-none}; pwd"); err != nil {
		t.Fatal(err)
	}
	_ = d.SendKeys(sh, "Enter")
	var screen string
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && !strings.Contains(screen, "S=none\n"); time.Sleep(20 * time.Millisecond) {
		c, _ := d.Capture(sh)
		screen = strings.Join(c.Lines, "\n")
	}
	if !strings.Contains(screen, "S=none\n") {
		t.Errorf("a companion must not carry DROVER_SESSION: %q", screen)
	}
	if want := filepathBase(dir); !strings.Contains(screen, want) {
		t.Errorf("companion did not start in %s: %q", dir, screen)
	}
}

func filepathBase(p string) string { return p[strings.LastIndex(p, "/")+1:] }

func TestSpawnCompanionStatusLineNamesTheWorktree(t *testing.T) {
	d := companionDriver(t)
	comp := CompanionName("git", "infra/feature/a")
	if err := d.SpawnCompanion(comp, t.TempDir(), "cat", "lazygit | infra/feature/a"); err != nil {
		t.Fatal(err)
	}
	out, err := d.run("show-option", "-v", "-t", target(comp), "status-left")
	if err != nil || !strings.Contains(out, "lazygit | infra/feature/a") {
		t.Errorf("status-left = %q, err %v", out, err)
	}
}

func TestWatcherSignalsOutputAndStaysQuietWhenIdle(t *testing.T) {
	d := companionDriver(t)
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	w, err := d.Watch("s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)

	// Drain what attaching itself produced, then expect silence.
	time.Sleep(300 * time.Millisecond)
	select {
	case <-w.Changed():
	default:
	}
	select {
	case <-w.Changed():
		t.Fatal("an idle pane must not signal")
	case <-time.After(300 * time.Millisecond):
	}

	// Typing must still work with a watcher attached (a read-only control
	// client makes tmux reject send-keys from everyone).
	if err := d.SendText("s", "hello"); err != nil {
		t.Fatalf("send-keys with a watcher attached: %v", err)
	}
	select {
	case <-w.Changed():
	case <-time.After(3 * time.Second):
		t.Fatal("no signal after the pane printed")
	}
	c, err := d.Capture("s")
	if err != nil || !strings.Contains(strings.Join(c.Lines, "\n"), "hello") {
		t.Errorf("capture = %v, %v", c.Lines, err)
	}
}

func TestWatcherDoneWhenSessionEndsAndOnClose(t *testing.T) {
	d := companionDriver(t)
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	w, err := d.Watch("s")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Kill("s"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("watcher still running after its session ended")
	}

	if err := d.Spawn("t", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	w2, err := d.Watch("t")
	if err != nil {
		t.Fatal(err)
	}
	w2.Close()
	select {
	case <-w2.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not stop the control client")
	}
}
