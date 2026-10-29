package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyRunes(s string) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Text: s}
	if r := []rune(s); len(r) == 1 {
		k.Code = r[0]
	}
	return k
}

func typeInto(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.handleNewKey(k)
		m = next.(Model)
	}
	return m
}

func TestNewPromptEditing(t *testing.T) {
	m := testModel()
	m.mode, m.newRoot = modeNew, "/p/infra"
	m = typeInto(t, m, keyRunes("feature/ab"), tea.KeyPressMsg{Code: tea.KeyBackspace}, keyRunes("c"))
	if m.input != "feature/ac" {
		t.Fatalf("input = %q, want feature/ac", m.input)
	}
	m = typeInto(t, m, keyRunes("ä"), tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input != "feature/a" {
		t.Errorf("backspace must remove whole runes: %q", m.input)
	}
	m = typeInto(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.input != "" {
		t.Errorf("ctrl+u left %q", m.input)
	}
	m = typeInto(t, m, pasteAsKey("a\nb"))
	if m.input != "ab" {
		t.Errorf("a pasted newline must not enter the name: %q", m.input)
	}
	m = typeInto(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeList {
		t.Errorf("esc did not cancel: mode %d", m.mode)
	}
}

func TestNewPromptRejectsEmptyAndStaysOpen(t *testing.T) {
	m := testModel()
	m.mode, m.newRoot = modeNew, "/p/infra"
	next, cmd := m.handleNewKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.mode != modeNew || m.status == "" {
		t.Errorf("empty name: cmd=%v mode=%d status=%q", cmd != nil, m.mode, m.status)
	}
}

func TestCreateErrorKeepsPromptOpenWithName(t *testing.T) {
	m := testModel()
	m.mode, m.input, m.busy = modeNew, "feature/x", true
	next, _ := m.handleCreated(worktreeCreated{err: errGone})
	m = next.(Model)
	if m.mode != modeNew || m.input != "feature/x" || m.busy || m.status == "" {
		t.Errorf("after failed create: mode=%d input=%q busy=%v status=%q", m.mode, m.input, m.busy, m.status)
	}
}

func TestRemoveNeedsSecondPressAndStoppedSession(t *testing.T) {
	m := testModel()
	m.cursor = 0 // running worktree
	next, cmd := m.remove()
	m = next.(Model)
	if cmd != nil || !strings.Contains(m.status, "stop the session first") {
		t.Fatalf("running session must block removal: status=%q", m.status)
	}

	m.cursor = 1 // stopped worktree
	next, cmd = m.remove()
	m = next.(Model)
	if cmd != nil || m.confirmRemove != m.rows[1].session {
		t.Fatalf("first press must only ask for confirmation")
	}
	next, cmd = m.remove()
	if cmd == nil {
		t.Fatal("second press must start the removal")
	}
	_ = next
}

func TestRemoveConfirmationResetsOnOtherKeys(t *testing.T) {
	m := testModel()
	m.cursor = 1
	next, _ := m.remove()
	m = next.(Model)
	next, _ = m.handleKey(keyRunes("j"))
	m = next.(Model)
	if m.confirmRemove != "" {
		t.Error("moving away must cancel the pending removal")
	}
}

func TestResumeGuards(t *testing.T) {
	m := testModel()
	m.cursor = 0
	next, _ := m.resume()
	if got := next.(Model).status; !strings.Contains(got, "already running") {
		t.Errorf("running row: %q", got)
	}
	m.cursor = 1
	m.cfg.AgentCmd = "codex"
	next, _ = m.resume()
	if got := next.(Model).status; !strings.Contains(got, "resume_args") {
		t.Errorf("non-claude agent without resume_args: %q", got)
	}
	m.cfg.AgentCmd = "claude" // claude, but the fake path has no stored conversation
	next, _ = m.resume()
	if got := next.(Model).status; !strings.Contains(got, "no previous conversation") {
		t.Errorf("no conversation: %q", got)
	}
}

func TestResumeArgs(t *testing.T) {
	m := testModel()
	for _, c := range []struct{ agent, args, want string }{
		{"claude", "", "--continue"},
		{"/opt/homebrew/bin/claude --model opus", "", "--continue"},
		{"codex", "", ""},
		{"codex", "resume --last", "resume --last"},
		{"claude", "--resume", "--resume"},
	} {
		m.cfg.AgentCmd, m.cfg.ResumeArgs = c.agent, c.args
		if got := m.resumeArgs(); got != c.want {
			t.Errorf("agent %q args %q: resumeArgs = %q, want %q", c.agent, c.args, got, c.want)
		}
	}
}

func TestGroupPrefixes(t *testing.T) {
	m := testModel()
	m.rows[0].root, m.rows[1].root = "/p/infra", "/p/infra"
	if got := strings.Join(m.groupPrefixes("/p/infra"), ","); got != "feature/" {
		t.Errorf("prefixes = %q (base group must not be listed)", got)
	}
}
