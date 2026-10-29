package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/lnware/drover/internal/gitinfo"
)

// Symbols follow gitmux's defaults, so the bar reads the way it does in a
// tmux status line.
const (
	symBranch    = "⎇"
	symAhead     = "↑"
	symBehind    = "↓"
	symStaged    = "●"
	symModified  = "✚"
	symUntracked = "…"
	symConflict  = "✖"
	symClean     = "✔"
	symNoUp      = "◌"
)

// gitSegments lists the facts about a worktree's git state, coloured.
// spaced puts a space between symbol and count ("✚ 3"); showClean adds the
// tick for a clean, synced tree. bg, when set, is applied to every segment so
// they sit properly on the selected row.
func gitSegments(st gitinfo.Status, spaced, showClean bool, bg color.Color) []string {
	sep := ""
	if spaced {
		sep = " "
	}
	paint := func(s lipgloss.Style, text string) string {
		if bg != nil {
			s = s.Background(bg)
		}
		return s.Render(text)
	}
	count := func(sym string, n int) string { return fmt.Sprintf("%s%s%d", sym, sep, n) }

	var out []string
	if st.Ahead > 0 {
		out = append(out, paint(gitAheadStyle, count(symAhead, st.Ahead)))
	}
	if st.Behind > 0 {
		out = append(out, paint(gitBehindStyle, count(symBehind, st.Behind)))
	}
	if st.Conflicts > 0 {
		out = append(out, paint(gitConflictStyle, count(symConflict, st.Conflicts)))
	}
	if st.Staged > 0 {
		out = append(out, paint(gitStagedStyle, count(symStaged, st.Staged)))
	}
	if st.Modified > 0 {
		out = append(out, paint(gitModifiedStyle, count(symModified, st.Modified)))
	}
	if st.Untracked > 0 {
		out = append(out, paint(gitUntrackedStyle, count(symUntracked, st.Untracked)))
	}
	if !st.HasUpstream && !st.Detached && showClean {
		// A local-only branch matters in this workflow: it has no upstream
		// until its first push.
		out = append(out, paint(gitNoUpStyle, symNoUp+sep+"no upstream"))
	}
	if showClean && !st.Dirty() && st.Ahead == 0 && st.Behind == 0 {
		out = append(out, paint(gitCleanStyle, symClean))
	}
	return out
}

// symBranchNerd is the Powerline / Nerd Font git branch glyph (U+E0A0). It is
// in the Private Use Area, so only fonts that carry it show it; the badge keeps
// a space after it either way.
const symBranchNerd = ""

// gitIcons is the glyph set for the git bar. Only the branch badge differs
// between sets; the other symbols are plain Unicode in both.
type gitIcons struct{ branch string }

// gitIconsFor maps the config's `icons` value to a set. Anything but "nerd"
// gets the Unicode set, so a bad value never breaks the bar.
func gitIconsFor(mode string) gitIcons {
	if mode == "nerd" {
		return gitIcons{branch: symBranchNerd}
	}
	return gitIcons{branch: symBranch}
}

// gitBar is the gitmux-style line for the pane header: a badge, the branch,
// then ahead/behind and the change counts.
func gitBar(st gitinfo.Status, icons gitIcons) string {
	name, style := st.Branch, gitBranchStyle
	if st.Detached {
		name, style = "detached "+st.Head, gitModifiedStyle.Bold(true)
	}
	parts := []string{gitBadgeStyle.Render(" "+icons.branch+" ") + " " + style.Render(name)}
	parts = append(parts, gitSegments(st, true, true, nil)...)
	return strings.Join(parts, "  ")
}

// gitBrief is the compact form for list rows: only what deviates, no clean
// tick, no branch (the row already shows it).
func gitBrief(st gitinfo.Status, bg color.Color) string {
	return strings.Join(gitSegments(st, false, false, bg), " ")
}
