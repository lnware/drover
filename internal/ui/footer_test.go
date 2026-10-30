package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func footerText(m Model) string { return ansi.Strip(m.footer()) }

func TestFooterFitsAndKeepsHelpAndQuitAtEveryWidth(t *testing.T) {
	for _, hdr := range []string{"", "org:x"} {
		for _, running := range []bool{true, false} {
			for w := 20; w <= 260; w += 7 {
				m := testModel()
				m.width, m.hdr = w, hdr
				m.rows[0].running = running
				m.all = m.rows
				got := footerText(m)
				if n := ansi.StringWidth(got); n != w {
					t.Fatalf("hdr=%q running=%v width %d: footer is %d wide", hdr, running, w, n)
				}
				if !strings.Contains(got, "? keys") || !strings.Contains(got, "q quit") {
					t.Errorf("hdr=%q running=%v width %d: lost help or quit: %q", hdr, running, w, got)
				}
			}
		}
	}
}

func TestFooterDropsLeastImportantHintsFirst(t *testing.T) {
	m := testModel()
	m.width = 60
	got := footerText(m)
	if !strings.Contains(got, "enter focus") {
		t.Errorf("the primary action must survive a narrow footer: %q", got)
	}
	if strings.Contains(got, "lazygit") {
		t.Errorf("a rarely used hint should be gone at 60 columns: %q", got)
	}
	m.width = 260
	if got := footerText(m); !strings.Contains(got, "lazygit") || !strings.Contains(got, "terminal/editor") {
		t.Errorf("a wide terminal should show everything: %q", got)
	}
}

func TestFooterFollowsTheSelectedRow(t *testing.T) {
	m := testModel()
	m.width = 260
	m.rows[0].running, m.all = true, m.rows
	running := footerText(m)
	if !strings.Contains(running, "x stop") || !strings.Contains(running, "N new agent") || strings.Contains(running, "v resume") || strings.Contains(running, "d remove") {
		t.Errorf("running agent footer: %q", running)
	}
	m.rows[0].running, m.all = false, m.rows
	stopped := footerText(m)
	if !strings.Contains(stopped, "v resume") || !strings.Contains(stopped, "d remove") || strings.Contains(stopped, "x stop") {
		t.Errorf("stopped agent footer: %q", stopped)
	}
}

func TestFooterIsShortEnoughForAnAverageTerminal(t *testing.T) {
	m := testModel()
	for _, running := range []bool{true, false} {
		m.rows[0].running, m.all = running, m.rows
		if w := hintWidth("", m.listHints()); w > 130 {
			t.Errorf("running=%v: the full footer is %d columns; it was 182 before", running, w)
		}
	}
}

func TestModeFootersFitNarrowTerminals(t *testing.T) {
	for _, mode := range []mode{modeSearch, modeRename, modeMoveBranch, modeNew, modeFocus, modePopup} {
		m := testModel()
		m.width, m.mode = 50, mode
		if n := ansi.StringWidth(m.footer()); n != 50 {
			t.Errorf("mode %d: footer is %d wide at 50 columns", mode, n)
		}
	}
}

func TestFitHintsNeverDropsPriorityZero(t *testing.T) {
	hints := []hint{{"a", "one", 1}, {"b", "two", 0}, {"c", "three", 2}}
	got := fitHints("", hints, 1)
	if len(got) != 1 || got[0].key != "b" {
		t.Errorf("only the never-dropped hint should remain: %+v", got)
	}
	got = fitHints("", hints, hintWidth("", hints)-1)
	if len(got) != 2 || got[0].key != "a" || got[1].key != "b" {
		t.Errorf("the highest priority number goes first: %+v", got)
	}
}
