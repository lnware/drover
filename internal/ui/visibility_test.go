package ui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/gitinfo"
)

// hiddenModel is a test model that has been told nobody is looking.
func hiddenModel(t *testing.T) Model {
	t.Helper()
	m := testModel()
	m.vis.probe = nil
	next, _ := m.setVisible(false)
	if !next.vis.hidden {
		t.Fatal("setVisible(false) did not hide")
	}
	return next
}

func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestHiddenParksEveryPollTick(t *testing.T) {
	for _, msg := range []tea.Msg{tick{}, previewTick{loop: 1}, gitTick{}, statusTick{}} {
		m := hiddenModel(t)
		next, cmd := update(m, msg)
		if cmd != nil {
			t.Errorf("%T while hidden returned a command (it would poll or reschedule)", msg)
		}
		if len(next.vis.parked) != 1 {
			t.Errorf("%T was not parked: %v", msg, next.vis.parked)
		}
		if next.ticks != 0 || next.gitTicks != 0 {
			t.Errorf("%T advanced a counter while hidden", msg)
		}
	}
}

func TestVisibleHandlesTicksNormally(t *testing.T) {
	m := testModel()
	next, cmd := update(m, tick{})
	if cmd == nil || next.ticks != 1 || len(next.vis.parked) != 0 {
		t.Errorf("a visible tick must run: cmd=%v ticks=%d parked=%d", cmd != nil, next.ticks, len(next.vis.parked))
	}
}

func TestParkKeepsOnePerPoller(t *testing.T) {
	m := hiddenModel(t)
	m, _ = update(m, previewTick{loop: 1})
	m, _ = update(m, previewTick{loop: 2})
	m, _ = update(m, gitTick{})
	if len(m.vis.parked) != 2 {
		t.Fatalf("parked %d ticks, want one per type: %v", len(m.vis.parked), m.vis.parked)
	}
}

func TestHiddenDropsInFlightPreviewAndResizeDoesNotCapture(t *testing.T) {
	m := hiddenModel(t)
	before := m.preview
	next, cmd := update(m, previewMsg{loop: m.loop, session: "infra/main", at: time.Now(), err: errGone})
	if cmd != nil || next.preview.ok != before.ok {
		t.Error("an in-flight capture must be dropped: its handler would refresh and reschedule")
	}
	next, cmd = update(m, tea.WindowSizeMsg{Width: 90, Height: 20})
	if cmd != nil {
		t.Error("a resize while hidden must not capture or resize the pane (no probe here, so no command at all)")
	}
	if next.width != 90 || next.height != 20 {
		t.Error("the new size must still be remembered")
	}
}

func TestHiddenHoldsGitReadsUntilVisible(t *testing.T) {
	m := hiddenModel(t)
	if cmd := m.requestGit(m.all); cmd != nil || m.gitBusy {
		t.Fatal("git must not start while hidden")
	}
	if len(m.gitWant) != len(m.all) {
		t.Fatalf("reads asked for while hidden must be remembered, have %d", len(m.gitWant))
	}
	// An agent finishing while hidden is the way a read gets requested.
	m.git = map[string]gitinfo.Status{}
	next, cmd := m.setVisible(true)
	if cmd == nil || len(next.gitWant) != 0 || !next.gitBusy {
		t.Errorf("becoming visible must start the held reads: cmd=%v want=%d busy=%v", cmd != nil, len(next.gitWant), next.gitBusy)
	}
}

func TestBecomingVisibleResumesAndUnparks(t *testing.T) {
	m := hiddenModel(t)
	m, _ = update(m, tick{})
	m, _ = update(m, gitTick{})
	if !m.send.paused.Load() {
		t.Error("the echo captures were not paused")
	}
	loop := m.loop
	next, cmd := update(m, visibilityMsg{visible: true})
	if next.vis.hidden || cmd == nil {
		t.Fatalf("hidden=%v cmd=%v", next.vis.hidden, cmd != nil)
	}
	if len(next.vis.parked) != 0 {
		t.Error("parked ticks must be replayed exactly once, then forgotten")
	}
	if next.loop == loop {
		t.Error("the preview chain was not restarted")
	}
	if next.send.paused.Load() {
		t.Error("echo captures stay paused")
	}
}

func TestStaleProbeTicksAreDropped(t *testing.T) {
	m := hiddenModel(t)
	next, cmd := update(m, probeTick{gen: m.vis.gen - 1})
	if cmd != nil || next.vis.gen != m.vis.gen {
		t.Error("a superseded probe chain must end")
	}
	// A visibility answer always starts exactly one new chain.
	next, cmd = update(m, visibilityMsg{visible: false})
	if cmd == nil || next.vis.gen != m.vis.gen+1 || !next.vis.hidden {
		t.Error("an answer must schedule the next probe and keep the state")
	}
}

func TestProbeErrorMeansVisible(t *testing.T) {
	m := testModel()
	m.vis.probe = func() (bool, error) { return false, errors.New("boom") }
	if msg := m.probeNow()().(visibilityMsg); !msg.visible {
		t.Error("an unknown answer must not pause drover")
	}
	m.vis.probe = func() (bool, error) { return false, nil }
	if msg := m.probeNow()().(visibilityMsg); msg.visible {
		t.Error("a clean 'not visible' must pause")
	}
	m.vis.probe = nil
	if m.probeNow() != nil {
		t.Error("outside tmux there is nothing to probe")
	}
}

func TestStatusPollIsRelaxedWhenHiddenAndParkedWithoutNotify(t *testing.T) {
	m := hiddenModel(t)
	if m.statusEvery() <= statusPoll {
		t.Error("the hidden status poll must be slower")
	}
	m.cfg.Notify = true
	next, cmd := update(m, statusTick{})
	if cmd == nil || len(next.vis.parked) != 0 {
		t.Error("with notify on, the status files are still read while hidden")
	}
}

func TestNoTmuxMeansNoProbe(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	if newVisibility().probe != nil {
		t.Error("without $TMUX drover is always visible")
	}
}
