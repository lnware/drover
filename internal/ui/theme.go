package ui

import (
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// shade is one palette colour: the exact 24-bit value, plus hand-picked
// stand-ins for terminals without truecolor. lipgloss would otherwise round
// to the nearest 256 colours itself, which turns the grays lilac and the
// selected row darker than the background.
type shade struct{ hex, x256, ansi string }

// color is the shade for the terminal's colour depth. Lip Gloss v2 renders
// full-fidelity colour and leaves downsampling to the output, which would
// round a 24-bit grey to the nearest of 256 by its own arithmetic; picking
// the hand-made stand-in here means the output has nothing left to round.
func (s shade) color() color.Color {
	return lipgloss.Complete(colorProfile)(lipgloss.Color(s.ansi), lipgloss.Color(s.x256), lipgloss.Color(s.hex))
}

// colorProfile is the colour depth the styles are built for. Tests and
// terminals with truecolor use the 24-bit values; main sets it from the
// environment.
var colorProfile = colorprofile.TrueColor

// SetColorProfile rebuilds the styles for a terminal with the given colour
// depth. Call it before the program starts, with the theme already set.
func SetColorProfile(p colorprofile.Profile) {
	colorProfile = p
	applyTheme(current)
}

// palette is one theme: a colour for every role the interface draws.
type palette struct {
	background shade // the terminal background
	ink        shade // text on a filled bar or badge; the background, or darker
	band       shade // org bars and the title
	text       shade // project names
	muted      shade // group labels, secondary text
	faint      shade // the "no session" circle
	working    shade
	waiting    shade // "you are needed"
	done       shade
	err        shade
	focus      shade // focus badge and hints
	selected   shade // selected row
	sep        shade // pane separator
	// relax lowers the readability floors for a palette whose colours cannot
	// reach them (see theme_test.go); 0 for all but one.
	relax float64
}

// DefaultTheme is used when the config names none, or one drover lacks.
const DefaultTheme = "jetbrains"

// themes are the selectable palettes. theme_test.go measures every colour of
// every one, and its 256-colour stand-in, against the theme background and
// the selected-row background, and fails if one becomes hard to read or
// drifts far from the theme.
//
// jetbrains is a Helix theme, jetbrains_dark with the background
// #0f1419 and orange #ff8f40 it overrides; Helix names are noted so a colour
// can be traced back to its source. The catppuccin ones are the dark
// flavours, Catppuccin's own names noted. Latte is left out: it is a light
// theme, and the contrast floors here assume text lighter than its ground.
var themes = map[string]palette{
	"jetbrains": {
		background: shade{"#0f1419", "233", "0"},
		ink:        shade{"#0f1419", "233", "0"},
		band:       shade{"#56a8f5", "75", "12"},  // blue245 (function)
		text:       shade{"#bcbec4", "250", "7"},  // blue196 (foreground)
		muted:      shade{"#a1a3ab", "248", "7"},  // blue171
		faint:      shade{"#868a91", "245", "8"},  // blue145 (inlay hint)
		working:    shade{"#d5b778", "179", "3"},  // red213 (tag): gold
		waiting:    shade{"#ff8f40", "209", "11"}, // the theme's own orange
		done:       shade{"#6aab73", "71", "2"},   // green171 (string)
		err:        shade{"#f07178", "210", "9"},  // lighter than red214 (#d64d5b, only 4.5:1 here)
		focus:      shade{"#2aacb8", "37", "6"},   // blue184 (numeric)
		selected:   shade{"#26282e", "235", "8"},  // blue46 (cursorline); barely lighter than the background, hence the bar
		sep:        shade{"#4b5059", "240", "8"},  // blue89
	},
	// Catppuccin roles: base, crust (the ink on bars), blue, text, subtext0,
	// overlay2, yellow, peach, green, red, teal, surface0, surface1. Where a
	// stock colour is too dim for the contrast floors, the flavour's own
	// alternate is used: maroon for red, sapphire for blue, mantle for
	// surface0. Frappé's base is lighter than the others', so its warm colours
	// only reach about 5.6:1 and it carries a relax of 0.5 (still above AA).
	"catppuccin-mocha": {
		background: shade{"#1e1e2e", "235", "0"},
		ink:        shade{"#11111b", "233", "0"},
		band:       shade{"#89b4fa", "111", "12"},
		text:       shade{"#cdd6f4", "189", "7"},
		muted:      shade{"#a6adc8", "146", "7"},
		faint:      shade{"#9399b2", "103", "8"},
		working:    shade{"#f9e2af", "223", "3"},
		waiting:    shade{"#fab387", "216", "11"},
		done:       shade{"#a6e3a1", "151", "2"},
		err:        shade{"#f38ba8", "211", "9"},
		focus:      shade{"#94e2d5", "116", "6"},
		selected:   shade{"#313244", "237", "8"},
		sep:        shade{"#45475a", "239", "8"},
	},
	"catppuccin-macchiato": {
		background: shade{"#24273a", "236", "0"},
		ink:        shade{"#181926", "234", "0"},
		band:       shade{"#7dc4e4", "116", "12"},
		text:       shade{"#cad3f5", "189", "7"},
		muted:      shade{"#a5adcb", "146", "7"},
		faint:      shade{"#939ab7", "109", "8"},
		working:    shade{"#eed49f", "223", "3"},
		waiting:    shade{"#f5a97f", "216", "11"},
		done:       shade{"#a6da95", "150", "2"},
		err:        shade{"#ee99a0", "217", "9"},
		focus:      shade{"#8bd5ca", "115", "6"},
		selected:   shade{"#363a4f", "238", "8"},
		sep:        shade{"#494d64", "240", "8"},
	},
	"catppuccin-frappe": {
		background: shade{"#303446", "237", "0"},
		ink:        shade{"#232634", "235", "0"},
		band:       shade{"#85c1dc", "116", "12"},
		text:       shade{"#c6d0f5", "189", "7"},
		muted:      shade{"#a5adce", "146", "7"},
		faint:      shade{"#949cbb", "109", "8"},
		working:    shade{"#e5c890", "186", "3"},
		waiting:    shade{"#ef9f76", "216", "11"},
		done:       shade{"#a6d189", "150", "2"},
		err:        shade{"#ea999c", "211", "9"},
		focus:      shade{"#81c8be", "115", "6"},
		selected:   shade{"#292c3c", "236", "8"},
		sep:        shade{"#51576d", "59", "8"},
		relax:      0.5,
	},
}

// ThemeNames lists the selectable themes, for messages.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolveTheme maps a config value to a theme name: "catppuccin" is mocha,
// and anything unknown or empty is the default. ok is false for the unknown
// non-empty ones, so the caller can say so.
func ResolveTheme(name string) (resolved string, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "":
		return DefaultTheme, true
	case "catppuccin":
		name = "catppuccin-mocha"
	}
	if _, found := themes[name]; found {
		return name, true
	}
	return DefaultTheme, false
}

// The palette in use, one colour per role. They are assigned by applyTheme,
// which runs at start-up and again from SetTheme; nothing else writes them.
var (
	colBackground, colInk, colBand, colText, colMuted, colFaint shade
	colWorking, colWaiting, colDone, colError, colFocus         shade
	colSelected, colSep                                         shade
)

var (
	orgStyle, titleStyle, projectStyle, groupStyle, dimStyle lipgloss.Style
	runningStyle, idleStyle, selectedStyle, badgeStyle       lipgloss.Style
	focusHint, errStyle, sepStyle                            lipgloss.Style
	// selBarStyle is the bar at the left edge of the selected row. The row's
	// background has to stay close to the terminal's for the state colours to
	// stay readable on it, so the bar is what makes the cursor easy to find.
	selBarStyle                                   lipgloss.Style
	workingStyle, waitingStyle, doneStyle         lipgloss.Style
	gitBadgeStyle, gitBranchStyle                 lipgloss.Style
	gitAheadStyle, gitBehindStyle, gitStagedStyle lipgloss.Style
	gitModifiedStyle, gitUntrackedStyle           lipgloss.Style
	gitConflictStyle, gitCleanStyle, gitNoUpStyle lipgloss.Style
)

func init() { applyTheme(themes[DefaultTheme]) }

// SetTheme switches the whole interface to the named theme (see
// ResolveTheme). Call it before the program starts: the styles are shared
// package state, not part of the model.
func SetTheme(name string) {
	resolved, _ := ResolveTheme(name)
	applyTheme(themes[resolved])
}

// current is the palette in use, kept so a change of colour profile can
// rebuild the styles.
var current palette

func applyTheme(p palette) {
	current = p
	colBackground, colInk, colBand, colText, colMuted, colFaint = p.background, p.ink, p.band, p.text, p.muted, p.faint
	colWorking, colWaiting, colDone, colError, colFocus = p.working, p.waiting, p.done, p.err, p.focus
	colSelected, colSep = p.selected, p.sep

	fg := func(s shade) lipgloss.Style { return lipgloss.NewStyle().Foreground(s.color()) }
	orgStyle = lipgloss.NewStyle().Bold(true).Foreground(colInk.color()).Background(colBand.color())
	titleStyle = fg(colBand).Bold(true)
	projectStyle = fg(colText).Bold(true)
	groupStyle = fg(colMuted)
	dimStyle = fg(colMuted)
	runningStyle = fg(colDone)
	idleStyle = fg(colFaint)
	selectedStyle = lipgloss.NewStyle().Bold(true).Background(colSelected.color())
	selBarStyle = fg(colFocus).Bold(true).Background(colSelected.color())
	badgeStyle = lipgloss.NewStyle().Bold(true).Foreground(colInk.color()).Background(colFocus.color())
	focusHint = fg(colFocus)
	errStyle = fg(colError).Bold(true)
	sepStyle = fg(colSep)

	workingStyle = fg(colWorking)
	waitingStyle = fg(colWaiting).Bold(true)
	doneStyle = fg(colDone)

	gitBadgeStyle = fg(colText).Bold(true).Background(colSep.color())
	gitBranchStyle = fg(colText).Bold(true)
	// Each kind of change has its own colour from the palette, so a glance
	// tells staged (green) from modified (gold) from untracked (blue).
	gitAheadStyle = fg(colFocus)
	gitBehindStyle = fg(colWaiting)
	gitStagedStyle = fg(colDone)
	gitModifiedStyle = fg(colWorking)
	gitUntrackedStyle = fg(colBand)
	gitConflictStyle = fg(colError).Bold(true)
	gitCleanStyle = fg(colDone)
	gitNoUpStyle = fg(colFaint)
}

// lead renders the n-cell indent of a row; on the selected row its first cell
// is the selection bar.
func lead(plain lipgloss.Style, selected bool, n int) string {
	if !selected || n < 1 {
		return plain.Render(strings.Repeat(" ", n))
	}
	return selBarStyle.Render("▌") + plain.Render(strings.Repeat(" ", n-1))
}
