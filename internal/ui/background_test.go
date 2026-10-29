package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

const testBG = "\x1b[48;2;15;20;25m" // the jetbrains background

func TestPaintBackgroundReappliesAfterEveryReset(t *testing.T) {
	line := "\x1b[1;38;2;1;2;3mab\x1b[m cd \x1b[0mef"
	got := paintBackground(line, testBG)
	want := testBG + "\x1b[1;38;2;1;2;3mab\x1b[m" + testBG + " cd \x1b[0m" + testBG + "ef\x1b[m"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestPaintBackgroundAddsNothingAfterATrailingReset(t *testing.T) {
	if got, want := paintBackground("ab\x1b[m", testBG), testBG+"ab\x1b[m"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := paintBackground("ab\x1b[1mcd", testBG), testBG+"ab\x1b[1mcd\x1b[m"; got != want {
		t.Errorf("an open style must be closed: got %q, want %q", got, want)
	}
}

func TestPaintBackgroundKeepsACellsOwnBackground(t *testing.T) {
	// a selected-row segment sets its own background after the reset before it
	line := "ab\x1b[m\x1b[48;2;38;40;46mcd\x1b[m"
	got := paintBackground(line, testBG)
	i := strings.Index(got, "\x1b[48;2;38;40;46m")
	if i < 0 || !strings.Contains(got[:i], testBG) {
		t.Fatalf("the own background must come after the painted one so it wins: %q", got)
	}
	if strings.LastIndex(got[:i], testBG) < strings.LastIndex(got[:i], "\x1b[m") {
		t.Errorf("bg is not re-applied right after the reset: %q", got)
	}
}

func TestPaintBackgroundChangesNoWidth(t *testing.T) {
	line := "\x1b[1mhello\x1b[m wörld ✓ \x1b[38;2;1;2;3mx\x1b[m"
	if a, b := ansi.StringWidth(line), ansi.StringWidth(paintBackground(line, testBG)); a != b {
		t.Errorf("width %d became %d", a, b)
	}
	if paintBackground(line, "") != line {
		t.Error("an empty background must leave the line untouched")
	}
}

func TestChromeBackgroundFollowsTheSettingAndTheTerminal(t *testing.T) {
	m := testModel()
	t.Cleanup(func() { SetColorProfile(colorprofile.TrueColor) })
	if got := m.chromeBackground(); got != testBG {
		t.Errorf("truecolor: %q, want %q", got, testBG)
	}
	SetColorProfile(colorprofile.ANSI256)
	if got := m.chromeBackground(); got != "\x1b[48;5;233m" {
		t.Errorf("256 colours must use the hand-picked stand-in: %q", got)
	}
	SetColorProfile(colorprofile.ASCII)
	if got := m.chromeBackground(); got != "" {
		t.Errorf("a terminal without colour must not be painted: %q", got)
	}
	SetColorProfile(colorprofile.TrueColor)
	m.cfg.Background = false
	if got := m.chromeBackground(); got != "" {
		t.Errorf("switched off: %q", got)
	}
}

// The list, title bar and footer are painted; the agent's pane is not.
func TestFramePaintsTheChromeButNotThePane(t *testing.T) {
	m := testModel()
	m.cfg.Background = true
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Fatalf("%d lines, want %d", len(lines), m.height)
	}
	if !strings.Contains(lines[0], testBG) || !strings.Contains(lines[len(lines)-1], testBG) {
		t.Error("the title bar and the footer must be painted")
	}
	for i := 1; i < len(lines)-1; i++ {
		sep := strings.Index(lines[i], "│")
		if sep < 0 {
			t.Fatalf("line %d has no separator: %q", i, lines[i])
		}
		// the painted region ends with the separator's own reset; the pane is what follows
		left, right := lines[i][:sep], strings.TrimPrefix(lines[i][sep+len("│"):], "\x1b[m")
		if !strings.Contains(left, testBG) {
			t.Errorf("line %d: the list is not painted: %q", i, left)
		}
		if strings.Contains(right, testBG) {
			t.Errorf("line %d: the pane must keep the terminal's background: %q", i, right)
		}
	}
	m.cfg.Background = false
	if strings.Contains(m.render(), testBG) {
		t.Error("with the setting off nothing may be painted")
	}
}
