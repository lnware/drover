package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/focuskeys"
	"github.com/lnware/drover/internal/tmux"
)

// Lazygit is a popup, not a pane or a row: it is reached ad hoc, so it is an
// overlay over the middle of drover's screen, like a tmux display-popup.
//
//   - It is ephemeral. Closing it (ctrl+\ or ctrl+q) kills the tmux session
//     behind it, and when lazygit exits by itself the popup closes. Nothing
//     persists: no row, no dot, no leftover session.
//   - While it is open every key goes to lazygit except the two closing keys,
//     and the mouse is ignored.
//   - The session is a normal companion session (tmux.CompanionName("git", ...)),
//     sized to the popup's inner area, captured with the same poll and, like a
//     focused companion, pushed through the control-mode watcher for low lag.
//   - It is drawn by splicing the popup's lines into the finished frame, so
//     every line of View() keeps exactly the terminal width.

// modePopup is set apart from the iota list in model.go, like modeMoveBranch.
const modePopup mode = 101

type popupState struct {
	open    bool
	session string  // tmux session lazygit runs in
	title   string  // shown in the border
	pv      preview // latest capture, kept apart from the right pane's
	sized   size    // the size the session was last resized to
	memo    *overlayMemo
}

// popupBox is the popup's outer rectangle: about 90% by 85% of the screen,
// centred.
func (m Model) popupBox() (x, y, w, h int) {
	w = min(max(m.width*90/100, 20), m.width)
	h = min(max(m.height*85/100, 6), m.height)
	return (m.width - w) / 2, (m.height - h) / 2, w, h
}

// popupSize is the terminal area inside the border: what lazygit is resized to.
func (m Model) popupSize() (cols, rows int) {
	_, _, w, h := m.popupBox()
	return w - 2, h - 2
}

// openPopup starts lazygit in the selected worktree's directory.
func (m Model) openPopup() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	cmd, err := m.companionCmd(viewLazygit)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	name := tmux.CompanionName(viewLazygit.kind(), r.session)
	if m.tmux.Has(name) {
		_ = m.tmux.Kill(name) // ephemeral: a session that is still around is never reused
	}
	if err := m.tmux.SpawnCompanion(name, r.wt.Path, cmd, "lazygit | "+r.session); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.companions[name] = true
	m.popup = popupState{open: true, session: name, title: "lazygit · " + r.wt.Label(), memo: &overlayMemo{}}
	m.mode = modePopup
	m.scroll = scrollState{}
	return m, m.startPreview()
}

// closePopup kills lazygit's session (a no-op when it already ended) and
// returns to the list. Lazygit may have changed files, so git is re-read.
func (m Model) closePopup() (tea.Model, tea.Cmd) {
	if m.popup.open {
		m.push.follow(m.tmux, "") // stop the watcher before its session goes
		_ = m.tmux.Kill(m.popup.session)
		delete(m.companions, m.popup.session)
	}
	m.popup = popupState{}
	m.mode = modeList
	m.preview = preview{}
	poll := m.startPreview() // before returning m, so the copy carries the new loop
	return m, tea.Batch(m.refresh(), poll, m.readSelectedGit())
}

// handlePopupKey sends everything to lazygit except the way out.
func (m Model) handlePopupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s := msg.String(); s == `ctrl+\` || s == "ctrl+q" {
		return m.closePopup()
	}
	m.forward(msg, m.popup.session)
	if m.idlePolls > 1 { // it is about to react: leave slow polling at once
		return m, m.startPreview()
	}
	return m, nil
}

// forwardPaste delivers a paste to a tmux session as one bracketed paste.
func (m Model) forwardPaste(content, name string) {
	driver := m.tmux
	in := focuskeys.EncodePaste(content)
	m.send.typed(driver, name, func() { _ = driver.Paste(name, in.Text) })
}

// forward delivers a key press to a tmux session; focus mode and the popup
// share it.
func (m Model) forward(msg tea.KeyPressMsg, name string) {
	driver := m.tmux
	switch in := focuskeys.Encode(msg); in.Kind {
	case focuskeys.KindText:
		m.send.typed(driver, name, func() { _ = driver.SendText(name, in.Text) })
	case focuskeys.KindKeys:
		m.send.typed(driver, name, func() { _ = driver.SendKeys(name, in.Keys...) })
	case focuskeys.KindPaste:
		m.send.typed(driver, name, func() { _ = driver.Paste(name, in.Text) })
	}
}

// pollPopup is the preview poll while the popup is open: it resizes lazygit's
// session to the popup and captures it. The right pane is not polled; it
// stays as it was behind the popup.
func (m *Model) pollPopup() tea.Cmd {
	name := m.popup.session
	if m.width == 0 {
		return m.nextPoll()
	}
	m.push.follow(m.tmux, name)
	var cmds []tea.Cmd
	driver := m.tmux
	if cols, rows := m.popupSize(); cols > 0 && rows > 0 {
		if want := (size{name, cols, rows}); m.popup.sized != want {
			m.popup.sized = want
			cmds = append(cmds, func() tea.Msg {
				_ = driver.Resize(name, cols, rows)
				return nil
			})
		}
	}
	loop := m.loop
	cmds = append(cmds, func() tea.Msg {
		at := time.Now()
		c, err := driver.Capture(name)
		if err != nil && !driver.Has(name) {
			err = errGone
		}
		return previewMsg{loop: loop, session: name, cap: c, err: err, at: at}
	})
	return tea.Batch(cmds...)
}

// takeCapture stores a capture of the popup's session; false when the
// capture is not for it (or is older than the one held).
func (m *Model) takeCapture(session string, c tmux.Capture, at time.Time) bool {
	if !m.popup.open || session != m.popup.session || at.Before(m.popup.pv.at) {
		return false
	}
	if m.popup.pv.ok && sameCapture(m.popup.pv.cap, c) {
		m.idlePolls++
	} else {
		m.idlePolls = 0
	}
	m.popup.pv = preview{session: session, cap: c, ok: true, at: at}
	return true
}

// popupLines draws the popup box: a border with the title, then the capture.
func (m Model) popupLines(w, h int) []string {
	inner := w - 2
	title := ansi.Truncate(" "+m.popup.title+" ", max(inner-2, 0), "…")
	top := focusHint.Render("╭─") + projectStyle.Render(title) +
		focusHint.Render(strings.Repeat("─", max(inner-2-ansi.StringWidth(title), 0))+"╮")
	body := make([]string, 0, h-2)
	if m.popup.pv.ok {
		body = previewLines(m.popup.pv, inner, h-2, true)
	} else {
		for i := 0; i < h-2; i++ {
			body = append(body, strings.Repeat(" ", inner))
		}
		body[0] = fit(" "+dimStyle.Render("starting lazygit…"), inner)
	}
	side := focusHint.Render("│")
	out := []string{fit(top, w)}
	for _, l := range body {
		// The reset keeps the capture's colours out of the border.
		out = append(out, side+l+"\x1b[0m"+side) // l is exactly inner cells wide
	}
	return append(out, focusHint.Render("╰"+strings.Repeat("─", inner)+"╯"))
}

// overlayPopup splices the popup into a finished frame. The frame's lines are
// exactly m.width cells and so are the popup's, and each cut piece is padded
// to its slot, so the result keeps that width.
func (m Model) overlayPopup(frame string) string {
	// Bubble Tea calls View after every message, and most leave the screen as
	// it was (poll ticks of an idle pane). Splicing costs about as much as
	// the rest of the frame, so an unchanged frame and capture reuse the last
	// result: measured, it keeps an idle popup at the CPU of an idle pane.
	key := overlayKey{frame: frame, at: m.popup.pv.at, title: m.popup.title, w: m.width, h: m.height}
	if memo := m.popup.memo; memo != nil && memo.valid && memo.key == key {
		return memo.out
	}
	out := m.spliceOverlay(frame)
	if memo := m.popup.memo; memo != nil {
		memo.key, memo.out, memo.valid = key, out, true
	}
	return out
}

// overlayMemo remembers the last spliced frame. It is a pointer so that the
// copies of the model Bubble Tea passes around share it.
type overlayMemo struct {
	key   overlayKey
	out   string
	valid bool
}

type overlayKey struct {
	frame, title string
	at           time.Time
	w, h         int
}

func (m Model) spliceOverlay(frame string) string {
	lines := strings.Split(frame, "\n")
	x, y, w, h := m.popupBox()
	for i, pl := range m.popupLines(w, h) {
		if y+i >= len(lines) {
			break
		}
		l := lines[y+i]
		lines[y+i] = fit(ansi.Cut(l, 0, x), x) + "\x1b[0m" + pl + "\x1b[0m" +
			fit(ansi.Cut(l, x+w, m.width), m.width-x-w)
	}
	return strings.Join(lines, "\n")
}
