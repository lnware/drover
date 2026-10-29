package ui

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// hexRGB parses "#rrggbb". The palette is 24-bit hex, so a stray terminal
// index or a typo fails loudly here instead of rendering as a wrong colour.
func hexRGB(t *testing.T, name, hex string) [3]float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("%s = %q: want #rrggbb", name, hex)
	}
	var c [3]float64
	for i := range c {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("%s = %q: %v", name, hex, err)
		}
		c[i] = float64(v)
	}
	return c
}

func luminance(c [3]float64) float64 {
	lin := func(x float64) float64 {
		x /= 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

// contrast is the WCAG contrast ratio between two colours.
func contrast(a, b [3]float64) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// xterm256 returns the RGB of an xterm-256 colour index 16..255. The 16
// system colours are terminal-defined, so they are not measured.
func xterm256(t *testing.T, name, index string) [3]float64 {
	t.Helper()
	i, err := strconv.Atoi(index)
	if err != nil || i < 16 || i > 255 {
		t.Fatalf("%s: 256-colour stand-in %q must be an index from 16 to 255", name, index)
	}
	if i >= 232 {
		v := float64(8 + 10*(i-232))
		return [3]float64{v, v, v}
	}
	i -= 16
	level := []float64{0, 95, 135, 175, 215, 255}
	return [3]float64{level[i/36], level[(i/6)%6], level[i%6]}
}

// Floors, as WCAG ratios. 4.5 is AA for body text and 7 is AAA; the theme's
// own colours are 6.5 to 10 on its background, so the floors sit just under
// that rather than brightening the user's favourite theme away from itself.
const (
	floorOnBackground = 6.0 // text on the terminal background
	floorOnSelected   = 5.0 // text on the selected row
	floorQuiet        = 4.5 // decoration meant to recede, still above AA
	floorBarText      = 6.5 // dark text on a filled bar
	// The 256-colour stand-ins are coarser, so they get 0.5 less headroom,
	// and must stay within this per-channel distance of the real colour.
	floorSlack    = 0.5
	maxStandInGap = 48.0
)

func textShades(p palette) map[string]shade {
	return map[string]shade{
		"band": p.band, "text": p.text, "muted": p.muted,
		"working": p.working, "waiting": p.waiting, "done": p.done,
		"error": p.err, "focus": p.focus,
	}
}

// eachTheme runs f once per theme, as a subtest named after it.
func eachTheme(t *testing.T, f func(t *testing.T, p palette)) {
	t.Helper()
	for name, p := range themes {
		t.Run(name, func(t *testing.T) { f(t, p) })
	}
}

func TestTextColoursAreReadable(t *testing.T) {
	eachTheme(t, func(t *testing.T, p palette) {
		for _, mode := range []struct {
			name  string
			color func(t *testing.T, name string, s shade) [3]float64
			slack float64
		}{
			{"truecolor", func(t *testing.T, n string, s shade) [3]float64 { return hexRGB(t, n, s.hex) }, 0},
			{"256-colour", func(t *testing.T, n string, s shade) [3]float64 { return xterm256(t, n, s.x256) }, floorSlack},
		} {
			bg := mode.color(t, "background", p.background)
			selected := mode.color(t, "selected", p.selected)
			for name, s := range textShades(p) {
				c := mode.color(t, name, s)
				if got := contrast(c, bg); got < floorOnBackground-p.relax-mode.slack {
					t.Errorf("%s %s on the background: contrast %.1f, want at least %.1f", mode.name, name, got, floorOnBackground-p.relax-mode.slack)
				}
				if got := contrast(c, selected); got < floorOnSelected-p.relax-mode.slack {
					t.Errorf("%s %s on the selected row: contrast %.1f, want at least %.1f", mode.name, name, got, floorOnSelected-p.relax-mode.slack)
				}
			}
			if got := contrast(mode.color(t, "faint", p.faint), bg); got < floorQuiet-mode.slack {
				t.Errorf("%s colFaint: contrast %.1f, want at least %.1f", mode.name, got, floorQuiet-mode.slack)
			}
		}
	})
}

func TestBarTextIsReadable(t *testing.T) {
	eachTheme(t, func(t *testing.T, p palette) {
		for _, mode := range []string{"truecolor", "256-colour"} {
			pick := func(name string, s shade) [3]float64 {
				if mode == "truecolor" {
					return hexRGB(t, name, s.hex)
				}
				return xterm256(t, name, s.x256)
			}
			slack := 0.0
			if mode != "truecolor" {
				slack = floorSlack
			}
			text := pick("ink", p.ink)
			for name, s := range map[string]shade{"org band": p.band, "focus badge": p.focus} {
				if got := contrast(text, pick(name, s)); got < floorBarText-slack {
					t.Errorf("%s: text on the %s: contrast %.1f, want at least %.1f", mode, name, got, floorBarText-slack)
				}
			}
		}
	})
}

// TestStandInsStayCloseToTheTheme keeps the hand-picked 256-colour values
// honest: lipgloss's own rounding turned grays lilac, so a stand-in far from
// its real colour would quietly bring that back.
func TestStandInsStayCloseToTheTheme(t *testing.T) {
	eachTheme(t, func(t *testing.T, p palette) {
		all := map[string]shade{"background": p.background, "ink": p.ink, "faint": p.faint, "selected": p.selected, "sep": p.sep}
		for k, v := range textShades(p) {
			all[k] = v
		}
		for name, s := range all {
			real, stand := hexRGB(t, name, s.hex), xterm256(t, name, s.x256)
			for i := range real {
				if d := math.Abs(real[i] - stand[i]); d > maxStandInGap {
					t.Errorf("%s: 256-colour %s is %.0f away from %s on channel %d (max %.0f)", name, s.x256, d, s.hex, i, maxStandInGap)
				}
			}
		}
	})
}

// TestStateColoursAreDistinct guards against two roles sharing a colour
// again, which was the problem with purple: band, project names and waiting.
func TestStateColoursAreDistinct(t *testing.T) {
	eachTheme(t, func(t *testing.T, p palette) {
		seenHex, seen256 := map[string]string{}, map[string]string{}
		for name, s := range textShades(p) {
			if other, dup := seenHex[s.hex]; dup {
				t.Errorf("%s and %s share colour %s", name, other, s.hex)
			}
			if other, dup := seen256[s.x256]; dup {
				t.Errorf("%s and %s share the 256-colour stand-in %s", name, other, s.x256)
			}
			seenHex[s.hex], seen256[s.x256] = name, name
		}
	})
}

func TestSelectedRowStartsWithTheBar(t *testing.T) {
	plain := lipgloss.NewStyle()
	if got := lipgloss.Width(lead(plain, true, 5)); got != 5 {
		t.Errorf("selected indent is %d cells, want 5", got)
	}
	if !strings.Contains(lead(plain, true, 3), "▌") {
		t.Error("selected indent has no bar")
	}
	if got := lead(plain, false, 3); got != "   " {
		t.Errorf("unselected indent = %q, want spaces", got)
	}
}

func TestOrgLineKeepsItsWidthAndMarksSelection(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		for _, selected := range []bool{false, true} {
			it := item{kind: itemOrg, label: "Acme", collapsed: collapsed, sum: tally{total: 6}}
			line := orgLine(it, selected, 40)
			if got := lipgloss.Width(line); got != 40 {
				t.Errorf("collapsed=%v selected=%v: width %d, want 40", collapsed, selected, got)
			}
			if got := strings.Contains(line, "▌"); got != selected {
				t.Errorf("collapsed=%v selected=%v: bar present = %v", collapsed, selected, got)
			}
		}
	}
}

func TestResolveTheme(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"", DefaultTheme, true},
		{"  Catppuccin ", "catppuccin-mocha", true},
		{"catppuccin-FRAPPE", "catppuccin-frappe", true},
		{"jetbrains", "jetbrains", true},
		{"catppuccin-latte", DefaultTheme, false}, // light theme, not offered
		{"solarized", DefaultTheme, false},
	} {
		got, ok := ResolveTheme(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ResolveTheme(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSetThemeSwitchesEveryColour(t *testing.T) {
	t.Cleanup(func() { SetTheme(DefaultTheme) })
	SetTheme("catppuccin-mocha")
	mocha := themes["catppuccin-mocha"]
	if colBand != mocha.band || colSelected != mocha.selected || colInk != mocha.ink {
		t.Errorf("palette not applied: band %v selected %v ink %v", colBand, colSelected, colInk)
	}
	// styles are rebuilt too, not just the colours
	if got := orgStyle.GetBackground(); got != colBand.color() {
		t.Errorf("orgStyle background = %v, want the mocha band %v", got, colBand.color())
	}
	if got := gitConflictStyle.GetForeground(); got != colError.color() {
		t.Errorf("gitConflictStyle foreground = %v, want %v", got, colError.color())
	}
	SetTheme("nonsense")
	if colBand != themes[DefaultTheme].band {
		t.Error("an unknown theme must fall back to the default palette")
	}
}
