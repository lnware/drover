package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/status"
)

// nextModel is contextModel with three agents running: infra waits, web-site
// is done, and garden-app (another context) waits too.
func nextModel(t *testing.T) Model {
	m := commandModel(t)
	for i := range m.all {
		m.all[i].running = true
	}
	m.agents = map[string]status.Entry{
		"infra/main":      {State: status.Waiting},
		"web-site/main":   {State: status.Done},
		"garden-app/main": {State: status.Waiting},
	}
	m.applyFilter()
	return m
}

func tab() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyTab} }
func shiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func TestTabWalksTheAgentsThatNeedYouInThisContextAndWraps(t *testing.T) {
	m := nextModel(t)
	if m.selSession != "infra/main" {
		t.Fatalf("starts on the first worktree: %q", m.selSession)
	}
	m = typed(t, m, tab())
	if m.selSession != "web-site/main" || !strings.Contains(m.notice, "finished") && !strings.Contains(m.notice, "done") {
		t.Fatalf("tab goes to the next agent that needs you: %q (%q)", m.selSession, m.notice)
	}
	m = typed(t, m, tab())
	if m.selSession != "infra/main" {
		t.Errorf("it wraps, and never leaves the context for garden-app: %q", m.selSession)
	}
	m = typed(t, m, shiftTab())
	if m.selSession != "web-site/main" {
		t.Errorf("shift+tab goes back: %q", m.selSession)
	}
}

func TestNextFromAWorktreeThatDoesNotNeedYouLandsOnItsNeighbour(t *testing.T) {
	m := nextModel(t)
	m.agents["infra/main"] = status.Entry{State: status.Working} // nothing waits at the top any more
	m.agents["web-site/main"] = status.Entry{State: status.Waiting}
	m = typed(t, m, tab())
	if m.selSession != "web-site/main" {
		t.Errorf("from a quiet worktree tab finds the one that waits: %q", m.selSession)
	}
	q := nextModel(t)
	q.agents["infra/main"] = status.Entry{State: status.Working}
	q.moveTo(item{kind: itemWorktree, id: "web-site/main", row: 1})
	q.agents["web-site/main"] = status.Entry{State: status.Working}
	q.agents["infra/main"] = status.Entry{State: status.Waiting}
	if got := typed(t, q, tab()); got.selSession != "infra/main" {
		t.Errorf("past the last one it wraps to the first: %q", got.selSession)
	}
}

func TestNextSaysSoWhenNothingNeedsYou(t *testing.T) {
	m := nextModel(t)
	m.agents = map[string]status.Entry{"infra/main": {State: status.Working}, "garden-app/main": {State: status.Waiting}}
	m = typed(t, m, tab())
	if m.selSession != "infra/main" || m.notice != "nothing needs you here" {
		t.Errorf("another context's agent does not count: %q %q", m.selSession, m.notice)
	}
}

func TestNextPicksTheAgentThatWaitsInAWorktreeWithSeveral(t *testing.T) {
	m := nextModel(t)
	m.agents = map[string]status.Entry{
		"web-site/main~aaaaaa": {State: status.Working},
		"web-site/main~bbbbbb": {State: status.Waiting},
	}
	m.all[1] = m.all[1].withAgents([]agent{
		{session: "web-site/main~aaaaaa", id: "aaaaaa", name: "one", running: true},
		{session: "web-site/main~bbbbbb", id: "bbbbbb", name: "two", running: true},
	})
	m.applyFilter()
	m = typed(t, m, tab())
	if m.selSession != "web-site/main" || m.subAgent != "web-site/main~bbbbbb" {
		t.Errorf("the cursor lands on the agent that waits: %q / %q", m.selSession, m.subAgent)
	}
	if !strings.Contains(m.notice, "two") {
		t.Errorf("the notice names the agent: %q", m.notice)
	}
}

func TestNextCommandAndTheOptionalKeyInFocusMode(t *testing.T) {
	m := nextModel(t)
	m = typed(t, m, append([]tea.KeyPressMsg{{Code: ':', Text: ":"}}, append(text("next"), enter())...)...)
	if m.selSession != "web-site/main" || m.mode != modeList {
		t.Errorf(":next walks too: %q mode %d", m.selSession, m.mode)
	}
	m.cfg.NextKey = "f10"
	m.mode = modeFocus
	m = typed(t, m, tea.KeyPressMsg{Code: tea.KeyF10})
	if m.selSession != "infra/main" || m.mode != modeFocus {
		t.Errorf("the configured key works in focus mode and keeps focus: %q mode %d", m.selSession, m.mode)
	}
	// tab itself belongs to the pane in focus mode
	m.cfg.NextKey = ""
	if got := typed(t, m, tab()); got.selSession != "infra/main" {
		t.Errorf("tab in focus mode is not ours: %q", got.selSession)
	}
}
