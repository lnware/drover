package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

func echoDriver(t *testing.T) tmux.Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := tmux.New(fmt.Sprintf("drover-echo-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "_")))
	t.Cleanup(func() { _ = d.KillServer() })
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	return d
}

type collector struct {
	mu   sync.Mutex
	msgs []echoMsg
}

func (c *collector) send(m tea.Msg) {
	if e, ok := m.(echoMsg); ok {
		c.mu.Lock()
		c.msgs = append(c.msgs, e)
		c.mu.Unlock()
	}
}

func (c *collector) all() []echoMsg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]echoMsg(nil), c.msgs...)
}

func TestTypedPushesTheEchoWithoutWaitingForAPoll(t *testing.T) {
	d := echoDriver(t)
	s := newSender()
	c := &collector{}
	s.notify = c.send

	start := time.Now()
	s.typed(d, "s", func() { _ = d.SendText("s", "hello") })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range c.all() {
			if strings.Contains(strings.Join(e.cap.Lines, "\n"), "hello") {
				// The poll interval is focusPoll; an echo push must beat it by a margin.
				if took := time.Since(start); took > focusPoll+100*time.Millisecond {
					t.Logf("slow machine: echo took %v", took)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no echo capture showed the typed text; got %d messages", len(c.all()))
}

// A burst of keys costs one capture sequence, not one per key: the echo loop
// restarts its timer whenever another key arrives within the echo waits. The
// sends here are instant on purpose. A real tmux send-keys is a process spawn,
// and when that takes longer than the first echo wait the next key cannot
// arrive in time, so how many captures a real burst costs depends on how fast
// the machine starts processes, which is no property of the code.
func TestBurstOfTypingCoalescesIntoFewCaptures(t *testing.T) {
	d := echoDriver(t)
	s := newSender()
	c := &collector{}
	s.notify = c.send

	for i := 0; i < 40; i++ {
		s.typed(d, "s", func() {})
	}
	time.Sleep(400 * time.Millisecond)
	// One sequence is a capture per echo wait.
	if n := len(c.all()); n == 0 || n > len(echoWaits)+1 {
		t.Errorf("40 keystrokes produced %d echo captures, want at most %d (each capture is a tmux process)", n, len(echoWaits)+1)
	}
}

// The text of a burst of real keystrokes reaches the pane and is pushed to the
// UI, whatever the speed of the machine.
func TestBurstOfRealKeystrokesIsPushedToTheUI(t *testing.T) {
	d := echoDriver(t)
	s := newSender()
	c := &collector{}
	s.notify = c.send

	for i := 0; i < 40; i++ {
		s.typed(d, "s", func() { _ = d.SendText("s", "x") })
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range c.all() {
			if strings.Count(strings.Join(e.cap.Lines, ""), "x") >= 40 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no capture ever showed all 40 keystrokes; %d captures were pushed", len(c.all()))
}

func TestEchoWithoutProgramIsHarmless(t *testing.T) {
	d := echoDriver(t)
	s := newSender() // notify stays nil, as in tests that have no program
	s.typed(d, "s", func() { _ = d.SendText("s", "q") })
	time.Sleep(100 * time.Millisecond)
}

func TestOlderCaptureNeverReplacesNewer(t *testing.T) {
	m := testModel()
	now := time.Now()
	m.preview = preview{session: "infra/main", ok: true, at: now, cap: tmux.Capture{Lines: []string{"newer"}}}

	next, _ := m.Update(echoMsg{session: "infra/main", at: now.Add(-time.Second), cap: tmux.Capture{Lines: []string{"stale"}}})
	if got := next.(Model).preview.cap.Lines[0]; got != "newer" {
		t.Errorf("a stale echo replaced a newer frame: %q", got)
	}
	next, _ = m.Update(echoMsg{session: "infra/main", at: now.Add(time.Second), cap: tmux.Capture{Lines: []string{"fresh"}}})
	if got := next.(Model).preview.cap.Lines[0]; got != "fresh" {
		t.Errorf("a newer echo was ignored: %q", got)
	}
	next, _ = m.Update(echoMsg{session: "other/session", at: now.Add(time.Second), cap: tmux.Capture{Lines: []string{"wrong"}}})
	if got := next.(Model).preview.cap.Lines[0]; got != "newer" {
		t.Errorf("an echo for another session replaced this preview: %q", got)
	}
}
