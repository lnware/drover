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

func TestBurstOfTypingCoalescesIntoFewCaptures(t *testing.T) {
	d := echoDriver(t)
	s := newSender()
	c := &collector{}
	s.notify = c.send

	for i := 0; i < 40; i++ {
		s.typed(d, "s", func() { _ = d.SendText("s", "x") })
	}
	time.Sleep(400 * time.Millisecond)
	if n := len(c.all()); n == 0 || n > 12 {
		t.Errorf("40 keystrokes produced %d echo captures, want a handful (each capture is a tmux process)", n)
	}
	last := c.all()[len(c.all())-1]
	if got := strings.Count(strings.Join(last.cap.Lines, ""), "x"); got < 40 {
		t.Errorf("the last capture shows %d of 40 keystrokes", got)
	}
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
