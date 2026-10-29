package ui

import (
	"testing"

	"github.com/lnware/drover/internal/gitinfo"
	"github.com/lnware/drover/internal/status"
)

func TestGitDueIdleWorkingAndUnseen(t *testing.T) {
	m := testModel()
	r := m.rows[0]
	if !m.gitDue(r) {
		t.Error("a worktree never read must be due")
	}
	m.gitAt[r.session] = 10
	m.gitTicks = 10
	if m.gitDue(r) {
		t.Error("just read: not due")
	}
	m.gitTicks = 10 + gitIdleEvery - 1
	if m.gitDue(r) {
		t.Errorf("an idle agent must not be re-read before %d ticks", gitIdleEvery)
	}
	m.gitTicks = 10 + gitIdleEvery
	if !m.gitDue(r) {
		t.Error("an idle worktree is due after gitIdleEvery ticks")
	}
	m.agents = map[string]status.Entry{r.session: {State: status.Working}}
	m.gitTicks = 11
	if !m.gitDue(r) {
		t.Error("a working agent changes files: re-read every tick")
	}
}

func TestIdleTicksDoNotSpawnGit(t *testing.T) {
	m := testModel()
	m.gitAt[m.rows[0].session] = 0
	reads := 0
	for i := 0; i < gitIdleEvery*3; i++ {
		before := m.gitAt[m.rows[0].session]
		next, _ := m.handleGitTick()
		m = next
		m.gitBusy = false // pretend each read finished at once
		if m.gitAt[m.rows[0].session] != before {
			reads++
		}
	}
	if reads != 3 {
		t.Errorf("%d reads of an idle worktree in %d ticks, want 3", reads, gitIdleEvery*3)
	}
}

func TestReadsNeverOverlapAndQueuedOnesRunAfter(t *testing.T) {
	m := testModel()
	if cmd := m.requestGit([]row{m.rows[0]}); cmd == nil || !m.gitBusy {
		t.Fatal("first request must start a read")
	}
	if cmd := m.requestGit([]row{m.rows[1]}); cmd != nil {
		t.Error("a second read started while one was running")
	}
	if len(m.gitWant) != 1 {
		t.Fatalf("the second request was not queued: %v", m.gitWant)
	}
	next, cmd := m.handleGit(gitMsg{res: map[string]gitinfo.Status{}})
	m = next
	if cmd == nil || !m.gitBusy || len(m.gitWant) != 0 {
		t.Errorf("finishing a read must start the queued one: cmd=%v busy=%v queued=%d", cmd != nil, m.gitBusy, len(m.gitWant))
	}
	next, cmd = m.handleGit(gitMsg{})
	if cmd != nil || next.gitBusy {
		t.Error("with nothing queued, finishing a read must leave things idle")
	}
}

func TestAgentThatStopsWorkingIsReadAtOnce(t *testing.T) {
	m := testModel()
	m.cfg.Notify = false
	m.statusLoaded = true
	m.prevStates = map[string]status.State{"infra/main": status.Working}
	next, _ := m.handleStatus(statusMsg{entries: map[string]status.Entry{"infra/main": {State: status.Done}}})
	if !next.(Model).gitBusy {
		t.Error("working -> done must trigger a git read: the agent has just changed files")
	}

	// no read for a state change that is not the end of work
	m = testModel()
	m.cfg.Notify = false
	m.statusLoaded = true
	m.prevStates = map[string]status.State{"infra/main": status.Idle}
	next, _ = m.handleStatus(statusMsg{entries: map[string]status.Entry{"infra/main": {State: status.Working}}})
	if next.(Model).gitBusy {
		t.Error("idle -> working must not trigger a read")
	}

	// the very first status load is a baseline, not a transition
	m = testModel()
	m.cfg.Notify = false
	next, _ = m.handleStatus(statusMsg{entries: map[string]status.Entry{"infra/main": {State: status.Done}}})
	if next.(Model).gitBusy {
		t.Error("the first load must not trigger reads")
	}
}

func TestFullScanIsRare(t *testing.T) {
	if gitAllEvery*int(gitEvery.Seconds()) < 5*60 {
		t.Errorf("a full read of every worktree every %ds is too frequent for something only external edits need", gitAllEvery*int(gitEvery.Seconds()))
	}
}
