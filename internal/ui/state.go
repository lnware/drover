package ui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/lnware/drover/internal/status"
)

// display is how a row's agent state looks in the list and the pane header.
type display struct {
	glyph string
	word  string
	style lipgloss.Style
}

// display resolves a row to what the user should see. A running session
// with no hook data (started elsewhere, or hooks not installed) is shown as
// plain "running" rather than guessed at.
func (m Model) display(r row) display {
	if !r.running {
		return display{glyph: "○", style: idleStyle}
	}
	e, ok := m.agents[r.session]
	if !ok {
		return display{glyph: "●", word: "running", style: dimStyle}
	}
	switch e.State {
	case status.Working:
		return display{"●", "working", workingStyle}
	case status.Waiting:
		return display{"◆", "waiting", waitingStyle}
	case status.Done:
		return display{"✓", "done", doneStyle}
	case status.Error:
		return display{"✗", "error", errStyle}
	}
	return display{"●", "idle", runningStyle}
}

// age renders how long ago t was, coarsely: 8s, 3m, 2h, 4d.
func age(since time.Time, now time.Time) string {
	d := max(now.Sub(since), 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// counts tallies sessions by display word for the title bar.
func (m Model) counts() (working, waiting, running int) {
	for _, r := range m.all {
		if !r.running {
			continue
		}
		running++
		switch m.agents[r.session].State {
		case status.Working:
			working++
		case status.Waiting:
			waiting++
		}
	}
	return working, waiting, running
}
