package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSessionName(t *testing.T) {
	got := SessionName("infra", "feature/a.b:c")
	if want := "infra/feature/a_b_c"; got != want {
		t.Errorf("SessionName = %q, want %q", got, want)
	}
}

func TestAttachCmdDropsTMUX(t *testing.T) {
	t.Setenv("TMUX", "/tmp/outer,1,0")
	cmd := New("x").AttachCmd("s")
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "TMUX=") {
			t.Fatalf("TMUX leaked into attach env: %s", kv)
		}
	}
	if len(cmd.Env) == 0 {
		t.Fatal("env must be set explicitly, an empty Env inherits TMUX")
	}
}

func TestSpawnListKill(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := New(fmt.Sprintf("drover-test-%d", os.Getpid()))
	t.Cleanup(func() { _ = d.KillServer() })

	if got, err := d.Sessions(); err != nil || len(got) != 0 {
		t.Fatalf("fresh server: sessions = %v, err = %v", got, err)
	}
	name := SessionName("proj", "feature/x")
	if err := d.Spawn(name, t.TempDir(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	if !d.Has(name) {
		t.Fatal("Has = false after Spawn")
	}
	if d.Has("proj") {
		t.Fatal("Has must match exact names, not prefixes")
	}
	if got, _ := d.Sessions(); !slices.Equal(got, []string{name}) {
		t.Fatalf("Sessions = %v", got)
	}
	if err := d.Kill(name); err != nil {
		t.Fatal(err)
	}
	if d.Has(name) {
		t.Fatal("Has = true after Kill")
	}
}

// tmux leaves its socket file behind when a server is killed; KillServer must
// remove it, or every test run adds dead sockets to the tmux directory.
func TestKillServerRemovesTheSocketFile(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := New(fmt.Sprintf("agt-killsock-%d", os.Getpid()))
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	path, err := d.run("display-message", "-p", "#{socket_path}")
	if err == nil && filepath.Clean(path) != filepath.Clean(d.socketPath()) && mustResolve(t, path) != mustResolve(t, d.socketPath()) {
		t.Fatalf("socketPath %q is not where tmux put it: %q", d.socketPath(), path)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a running server has its socket: %v", err)
	}
	if err := d.KillServer(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the socket file must be gone after KillServer: %v", err)
	}
	if err := d.KillServer(); err != nil {
		t.Errorf("killing a server that is not running is not an error: %v", err)
	}
}

func mustResolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

// A server whose last session ended has exited already, but its socket file is
// still there: KillServer must clear that one too.
func TestKillServerRemovesTheSocketOfAnExitedServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := New(fmt.Sprintf("agt-exited-%d", os.Getpid()))
	if err := d.Spawn("s", t.TempDir(), "true"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ { // the server exits once its only session has
		if _, err := d.run("list-sessions"); err != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := d.KillServer(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.socketPath()); !os.IsNotExist(err) {
		t.Errorf("the socket of an exited server must be removed too: %v", err)
	}
}
