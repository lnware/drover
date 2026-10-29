package ui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/status"
)

// spawnLog puts logging tmux and git on PATH and returns how many times each
// was started so far. Counting process starts is the honest measure of idle
// cost here, since each is a fork+exec whatever it then does.
func spawnLog(t *testing.T) func() int {
	t.Helper()
	dir, log := t.TempDir(), filepath.Join(t.TempDir(), "spawns.log")
	script := "#!/bin/sh\necho \"$0 $*\" >> " + log + "\necho 'no server running' >&2\nexit 1\n"
	for _, name := range []string{"tmux", "git"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "\n")
	}
}

// runHeadless runs the real Bubble Tea loop with no terminal.
func runHeadless(t *testing.T, m Model) *tea.Program {
	t.Helper()
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignals())
	m.Notify(p.Send)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = p.Run() }()
	t.Cleanup(func() { p.Quit(); wg.Wait() })
	return p
}

func waitUntil(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestHiddenSpawnsNothingAndResumesAtOnce runs the whole loop: visible it
// spawns, hidden (notify off) it spawns and reads nothing across more than a
// full refresh and git interval, and becoming visible refreshes at once.
func TestHiddenSpawnsNothingAndResumesAtOnce(t *testing.T) {
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	t.Setenv("TMUX", "") // no probe: this test is the only source of visibility
	spawns := spawnLog(t)
	cfg := config.Default()
	cfg.Notify = false
	p := runHeadless(t, New(cfg))

	waitUntil(t, "a visible drover to spawn", 3*time.Second, func() bool { return spawns() > 0 })

	p.Send(visibilityMsg{visible: false})
	time.Sleep(500 * time.Millisecond) // let in-flight work finish
	base := spawns()
	time.Sleep(refreshEvery + gitEvery + 300*time.Millisecond)
	if got := spawns(); got != base {
		t.Fatalf("%d process(es) started while hidden", got-base)
	}

	p.Send(visibilityMsg{visible: true})
	waitUntil(t, "an immediate refresh on becoming visible", 500*time.Millisecond, func() bool { return spawns() > base })
}

// TestNotificationsStillFireWhileHidden is the point of the relaxed status
// poll: the user is away exactly when an agent needs them. It also checks that
// the notification path starts no tmux or git.
func TestNotificationsStillFireWhileHidden(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DROVER_STATE_DIR", stateDir)
	t.Setenv("TMUX", "")
	spawns := spawnLog(t)

	var mu sync.Mutex
	var got []string
	old := sendNotification
	sendNotification = func(_, body string) error { mu.Lock(); got = append(got, body); mu.Unlock(); return nil }
	t.Cleanup(func() { sendNotification = old })

	cfg := config.Default()
	cfg.Notify = true
	p := runHeadless(t, New(cfg))
	time.Sleep(600 * time.Millisecond) // first status read seeds the baseline
	p.Send(visibilityMsg{visible: false})
	time.Sleep(500 * time.Millisecond)
	base := spawns()

	// The fake agent finishes while nobody looks.
	if err := status.Write(status.Dir(), "fake/agent", status.Waiting, time.Now()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a notification while hidden", hiddenStatusPoll+time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) > 0
	})
	mu.Lock()
	if len(got) != 1 || got[0] != "fake/agent needs you" {
		t.Errorf("notifications = %q", got)
	}
	mu.Unlock()
	time.Sleep(refreshEvery + 300*time.Millisecond)
	if n := spawns(); n != base {
		t.Errorf("%d process(es) started while hidden with notify on", n-base)
	}
}
