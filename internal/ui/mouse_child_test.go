package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/tmux"
)

// Every line of the left list that the hit test calls a child row must be
// drawn as that child, and clicking it selects it; clicking its worktree
// selects the agent again.
func TestClickSelectsChildRowsAndWorktrees(t *testing.T) {
	m := testModel()
	manyRows(&m, 4)
	for _, r := range m.rows[:3] {
		m.companions[tmux.CompanionName("term", r.session)] = true
	}
	m.companions[tmux.CompanionName("edit", m.rows[1].session)] = true
	leftW, _, bodyH := m.geometry()
	screen := strings.Split(ansi.Strip(m.render()), "\n")

	children := 0
	for y := 0; y < bodyH; y++ {
		it, ok := m.itemAtLine(leftW, bodyH, y)
		text := ansi.Cut(screen[y+1], 0, leftW)
		if !ok || it.kind != itemCompanion {
			if strings.Contains(text, "├ ") || strings.Contains(text, "└ ") {
				t.Errorf("line %d draws a child but does not hit-test as one: %q", y, text)
			}
			continue
		}
		children++
		if !strings.Contains(text, it.view.label()) {
			t.Errorf("line %d hit-tests as the %s row but shows %q", y, it.view.label(), text)
		}
		c := step(m, click(5, y+1, tea.MouseLeft))
		if c.sub != it.view || c.selSession != m.rows[it.row].session || c.hdr != "" {
			t.Errorf("clicking the %s row of %s: sub %d sel %q hdr %q", it.view.label(), m.rows[it.row].session, c.sub, c.selSession, c.hdr)
		}
		if c.viewOf(m.rows[it.row]) != it.view || c.target(m.rows[it.row]) != tmux.CompanionName(it.view.kind(), m.rows[it.row].session) {
			t.Errorf("the pane does not follow the clicked %s row", it.view.label())
		}
		// Clicking the worktree row above brings the agent back.
		for up := y - 1; up >= 0; up-- {
			w, ok := m.itemAtLine(leftW, bodyH, up)
			if ok && w.kind == itemWorktree && w.row == it.row {
				back := step(c, click(5, up+1, tea.MouseLeft))
				if back.sub != viewAgent || back.selSession != w.id {
					t.Errorf("clicking the worktree row left sub %d sel %q", back.sub, back.selSession)
				}
				break
			}
		}
	}
	if children != 4 {
		t.Errorf("%d child rows on screen, want 4", children)
	}
}
