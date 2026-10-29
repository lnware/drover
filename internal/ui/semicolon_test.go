package ui

import (
	"strings"
	"testing"
	"time"
)

// Typing in focus mode goes key press -> focuskeys -> sender -> tmux. A
// semicolon used to vanish at the last step (see the tmux package test); this
// proves the whole path from a key press.
func TestSemicolonTypedInFocusModeReachesTheAgent(t *testing.T) {
	m := scrollModel(t, 3) // a cat agent is running in the first row
	m.mode = modeFocus
	for _, r := range "a;b; c;" {
		next, _ := m.handleFocusKey(keyRunes(string(r)))
		m = next.(Model)
	}
	s := m.rows[0].session
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := m.tmux.Capture(s); err == nil && strings.Contains(strings.Join(c.Lines, "\n"), "a;b; c;") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, _ := m.tmux.Capture(s)
	t.Fatalf("the agent never received \"a;b; c;\"; its pane shows:\n%s", strings.Join(c.Lines, "\n"))
}
