package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
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
