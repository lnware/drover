package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/gitinfo"
	"github.com/lnware/drover/internal/pr"
)

// Review happens in lazygit, and git's index is the record of it: a file you
// staged is reviewed, a file with changes that are not staged is waiting. So
// drover does not keep marks of its own. It shows the number of files waiting
// on the row of each worktree (✎3), those staged and then changed again
// (↻1: the part you reviewed is no longer all of it), and the number of
// worktrees with something waiting in the title bar. V and :review open
// lazygit; :pr shows what a pull request from the branch would hold.

// reviewChip is "✎3" for three files waiting for a look, and "↻1" for one of
// them that you had staged and was changed again. Both are empty when nothing
// waits.
func reviewChip(st gitinfo.Status) (chip, partial string) {
	if n := st.ToReview(); n > 0 {
		chip = fmt.Sprintf("✎%d", n)
		if st.Partial > 0 {
			partial = fmt.Sprintf("↻%d", st.Partial)
		}
	}
	return chip, partial
}

// reviewCount is the number of worktrees of the active context with files
// waiting for review.
func (m Model) reviewCount() int {
	n := 0
	for _, r := range m.contextRows() {
		if m.git[r.session].ToReview() > 0 {
			n++
		}
	}
	return n
}

type prMsg struct {
	session string
	sum     pr.Summary
	err     error
}

// openPR shows the popup and reads the branch in the background: a few git
// calls, which are not worth a place in the periodic reads.
func (m Model) openPR() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.notice = "select a worktree to see its pull request"
		return m, nil
	}
	m.mode, m.prSum, m.prErr, m.prLoading = modePR, pr.Summary{}, "", true
	path, session := r.wt.Path, r.session
	return m, func() tea.Msg {
		sum, err := pr.Read(path)
		return prMsg{session: session, sum: sum, err: err}
	}
}

func (m Model) handlePRRead(msg prMsg) (tea.Model, tea.Cmd) {
	if r, ok := m.selected(); !ok || r.session != msg.session || m.mode != modePR {
		return m, nil // closed, or moved on, before it came back
	}
	m.prLoading, m.prSum = false, msg.sum
	if msg.err != nil {
		m.prErr = msg.err.Error()
	}
	return m, nil
}

// handlePRKey closes the popup on any key.
func (m Model) handlePRKey(tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeList
	return m, nil
}

// prBox is the pull request popup.
func (m Model) prBox() []string {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	title := " Pull request · " + r.project + "  " + m.label(r) + " "
	var rows []string
	add := func(s string) { rows = append(rows, " "+s) }
	sum := m.prSum
	switch {
	case m.prLoading:
		add(dimStyle.Render("reading…"))
	case m.prErr != "":
		add(dimStyle.Render(m.prErr))
	case sum.Base == "":
		add(dimStyle.Render("nothing to compare: this is the default branch, or there is none"))
	case !sum.Any():
		add(dimStyle.Render("no commits of its own yet, compared with " + sum.Base))
	default:
		add(dimStyle.Render("into " + sum.Base + " · from " + sum.Fork))
		add(fmt.Sprintf("%d commits · %d files · %s %s",
			sum.Total, sum.Files, doneStyle.Render(fmt.Sprintf("+%d", sum.Added)), errStyle.Render(fmt.Sprintf("−%d", sum.Removed))))
		rows = append(rows, "")
		shown := min(len(sum.Commits), max(m.height-14, 3))
		for _, c := range sum.Commits[:shown] {
			add(waitingStyle.Render(c.Hash) + "  " + c.Subject)
		}
		if rest := sum.Total - shown; rest > 0 {
			add(dimStyle.Render(fmt.Sprintf("… and %d more", rest)))
		}
	}
	if st, ok := m.git[r.session]; ok && !m.prLoading {
		if n := st.ToReview(); n > 0 {
			rows = append(rows, "")
			add(dimStyle.Render(fmt.Sprintf("not included: %d files with uncommitted changes", n)))
		}
		if st.HasUpstream && st.Ahead > 0 {
			add(dimStyle.Render(fmt.Sprintf("%d commits are not pushed yet", st.Ahead)))
		}
	}
	return popupBox(title, rows, dimStyle.Render(" any key closes"), 52, m.width-2)
}
