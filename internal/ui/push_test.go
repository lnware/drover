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

type pushes struct {
	mu   sync.Mutex
	msgs []echoMsg
}

func (p *pushes) send(m tea.Msg) {
	if e, ok := m.(echoMsg); ok {
		p.mu.Lock()
		p.msgs = append(p.msgs, e)
		p.mu.Unlock()
	}
}

func (p *pushes) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.msgs)
}

func (p *pushes) last() echoMsg {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.msgs[len(p.msgs)-1]
}

func pushDriver(t *testing.T, cmd string) tmux.Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := tmux.New(fmt.Sprintf("agt-companion-push-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "_")))
	t.Cleanup(func() { _ = d.KillServer() })
	if err := d.Spawn("s", t.TempDir(), cmd); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPusherCapturesWhenThePaneChangesAndNotBefore(t *testing.T) {
	d := pushDriver(t, "cat")
	got := &pushes{}
	p := &pusher{notify: got.send}
	t.Cleanup(func() { p.follow(d, "") })
	p.follow(d, "s")

	time.Sleep(300 * time.Millisecond)
	base := got.count()
	time.Sleep(300 * time.Millisecond)
	if got.count() != base {
		t.Fatalf("an idle pane pushed %d captures", got.count()-base)
	}

	_ = d.SendText("s", "pushed")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(strings.Join(lastLines(got), "\n"), "pushed") {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(strings.Join(lastLines(got), "\n"), "pushed") {
		t.Fatal("the change was not pushed")
	}
	if got.last().session != "s" {
		t.Errorf("session = %q", got.last().session)
	}
}

func lastLines(p *pushes) []string {
	if p.count() == 0 {
		return nil
	}
	return p.last().cap.Lines
}

func TestPusherCapsTheCaptureRate(t *testing.T) {
	d := pushDriver(t, `sh -c 'while :; do echo x; done'`)
	got := &pushes{}
	p := &pusher{notify: got.send}
	t.Cleanup(func() { p.follow(d, "") })
	p.follow(d, "s")
	time.Sleep(time.Second)
	n := got.count()
	// pushGap bounds it from above; a floor keeps the test honest about
	// pushes actually happening under a flood.
	if max := int(time.Second/pushGap) + 2; n > max {
		t.Errorf("%d captures in 1s, more than 1 per %v", n, pushGap)
	}
	if n < 5 {
		t.Errorf("only %d captures in 1s of continuous output", n)
	}
}

func TestPusherSwitchesSessionsAndSurvivesARestart(t *testing.T) {
	d := pushDriver(t, "cat")
	got := &pushes{}
	p := &pusher{notify: got.send}
	t.Cleanup(func() { p.follow(d, "") })
	p.follow(d, "s")
	p.follow(d, "s") // same session: no second watcher
	first := p.watcher

	_ = d.Kill("s")
	select {
	case <-first.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("watcher outlived its session")
	}
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	p.follow(d, "s") // the same name again, now a different session
	if p.watcher == first {
		t.Fatal("a dead watcher was kept for a recreated session")
	}
	before := got.count()
	_ = d.SendText("s", "again")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && got.count() == before {
		time.Sleep(10 * time.Millisecond)
	}
	if got.count() == before {
		t.Error("the new watcher pushed nothing")
	}
	p.follow(d, "")
	if p.watcher != nil {
		t.Error("follow(\"\") must stop watching")
	}
}

func TestPusherDoesNothingWithoutAProgram(t *testing.T) {
	d := tmux.New(fmt.Sprintf("agt-companion-none-%d", os.Getpid()))
	p := &pusher{}
	p.follow(d, "anything")
	if p.watcher != nil {
		t.Error("with no program to notify, no control client may start")
	}
}

func TestPushesOnlyForAFocusedCompanion(t *testing.T) {
	m := testModel()
	r := m.rows[0]
	for _, c := range []struct {
		mode mode
		view paneView
		want bool
	}{
		{modeFocus, viewAgent, false}, // an agent stays on the poll
		{modeList, viewTerminal, false},
		{modeFocus, viewTerminal, true},
		{modeFocus, viewEditor, true},
	} {
		m.mode = c.mode
		m.sub = c.view
		if got := m.pushTarget(r) != ""; got != c.want {
			t.Errorf("mode %d view %d: push = %v, want %v", c.mode, c.view, got, c.want)
		}
	}
	if want := tmux.CompanionName("edit", r.session); m.pushTarget(r) != want {
		t.Errorf("target = %q, want %q", m.pushTarget(r), want)
	}
}
