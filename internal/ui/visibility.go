package ui

import (
	"reflect"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/visibility"
)

// While nobody can see drover (the popup was closed, another tmux window is
// shown) nothing should poll: the session keeps running, so every poll would
// be paid for with nobody looking. One gate does this, in Update, before the
// normal handlers:
//
//   - the visibility probe is the only chain that always runs, one tmux call
//     per probe interval; without tmux ($TMUX unset) there is no probe at all
//     and drover is always visible;
//   - while hidden, every poll tick (a message implementing pollTick) is
//     parked instead of handled, which ends its chain without a reschedule;
//   - when visible again the parked ticks are replayed, so each chain resumes
//     exactly once, and everything is refreshed at once.
//
// Adding a poller: give its tick message `func (myTick) pollTick() {}` and
// make its result handler reschedule the tick as usual. Anything that spawns a
// process or reads files for a reason other than a tick (for instance after a
// key or an in-flight result) must check m.vis.hidden. Nothing else is needed;
// a poller that also has to run while hidden must say so here, like statusTick
// does for notifications.
const (
	// probeEvery is how often visibility is probed, in both states. Hidden, it
	// is the whole cost of an idle drover: one short tmux call per interval.
	probeEvery = 2 * time.Second
	// hiddenStatusPoll is the status-file poll while hidden with notify on:
	// the agents' state changes are what notifications are for, but nobody is
	// watching the list, so a slow poll of a handful of tiny files will do.
	hiddenStatusPoll = 1500 * time.Millisecond
)

// pollTick marks the tick message of a polling chain. Ticks are parked while
// drover is hidden.
type pollTick interface{ pollTick() }

func (tick) pollTick()        {}
func (previewTick) pollTick() {}
func (gitTick) pollTick()     {}
func (statusTick) pollTick()  {}

type (
	// visibilityMsg is the answer of the probe; tests send it directly.
	visibilityMsg struct{ visible bool }
	// probeTick asks for a probe. gen drops the ticks of a superseded chain.
	probeTick struct{ gen int }
)

type visibilityState struct {
	probe  visibility.Probe // nil: not in tmux, always visible
	hidden bool
	gen    int
	parked []tea.Msg // one tick per poller, replayed on becoming visible
}

func newVisibility() visibilityState { return visibilityState{probe: visibility.FromEnv()} }

func (m Model) probeNow() tea.Cmd {
	probe := m.vis.probe
	if probe == nil {
		return nil
	}
	return func() tea.Msg {
		visible, err := probe()
		return visibilityMsg{visible: visible || err != nil}
	}
}

// statusEvery is the status-file poll interval for the current visibility.
func (m Model) statusEvery() time.Duration {
	if m.vis.hidden {
		return hiddenStatusPoll
	}
	return statusPoll
}

// gate handles visibility and, while hidden, keeps pollers quiet. It reports
// whether it consumed the message.
func (m Model) gate(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case probeTick:
		if msg.gen != m.vis.gen {
			return m, nil, true
		}
		return m, m.probeNow(), true
	case visibilityMsg:
		next, cmd := m.setVisible(msg.visible)
		return next, cmd, true
	}
	if !m.vis.hidden {
		return m, nil, false
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Attaching a client can resize the window: the cheapest hint that
		// the popup is back. Only remember the size; resuming restarts the
		// preview, and the probe decides.
		m.width, m.height = msg.Width, msg.Height
		return m, m.probeNow(), true
	case previewMsg:
		return m, nil, true // an in-flight capture; its handler would reschedule
	case statusTick:
		if m.cfg.Notify {
			return m, nil, false // notifications need the status files
		}
		return m.park(msg), nil, true
	case pollTick:
		return m.park(msg), nil, true
	}
	return m, nil, false
}

// park keeps the latest tick of each type, ending its chain until resume.
func (m Model) park(msg tea.Msg) Model {
	t := reflect.TypeOf(msg)
	parked := slices.DeleteFunc(slices.Clone(m.vis.parked), func(p tea.Msg) bool { return reflect.TypeOf(p) == t })
	m.vis.parked = append(parked, msg)
	return m
}

// setVisible applies a probe answer and schedules the next probe.
func (m Model) setVisible(visible bool) (Model, tea.Cmd) {
	was := m.vis.hidden
	m.vis.hidden = !visible
	m.send.paused.Store(m.vis.hidden)
	if !was && !visible {
		// The control-mode watcher captures on its own whenever a focused
		// companion prints, so it must stop with everything else. Resuming
		// restarts it: becomeVisible's startPreview calls follow again.
		m.push.follow(m.tmux, "")
	}
	m.vis.gen++
	gen := m.vis.gen
	next := tea.Tick(probeEvery, func(time.Time) tea.Msg { return probeTick{gen: gen} })
	if was && visible {
		m, resume := m.becomeVisible()
		return m, tea.Batch(next, resume)
	}
	return m, next
}

// becomeVisible refreshes everything once and restarts the polling chains that were
// parked, so the user never waits for the next scheduled poll.
func (m Model) becomeVisible() (Model, tea.Cmd) {
	cmds := []tea.Cmd{m.refresh(), m.startPreview()}
	// Git reads asked for while hidden (an agent finished) plus the selection.
	rows := make([]row, 0, len(m.gitWant)+1)
	if r, ok := m.selected(); ok {
		rows = append(rows, r)
	}
	for _, r := range m.gitWant {
		rows = append(rows, r)
	}
	clear(m.gitWant)
	cmds = append(cmds, m.requestGit(rows))
	for _, p := range m.vis.parked {
		cmds = append(cmds, func() tea.Msg { return p })
	}
	m.vis.parked = nil
	return m, tea.Batch(cmds...)
}
