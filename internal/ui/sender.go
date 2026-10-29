package ui

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

// echoWaits are the delays, after a key was delivered, at which the pane is
// captured and pushed to the UI. The first catches an agent that echoes at
// once; the second catches one that needs a few more milliseconds. Without
// this the echo would wait for the next poll (up to focusPoll later).
var echoWaits = []time.Duration{8 * time.Millisecond, 20 * time.Millisecond}

// sender delivers input to tmux. Commands run one at a time, in order, since
// concurrent commands would reorder keystrokes; the echo captures run on a
// separate goroutine so that waiting for an agent never delays the next key.
type sender struct {
	ch     chan func()
	echoCh chan echoTarget
	// notify posts a message to the running program. It is set once, before
	// any input is sent, and nil in tests that have no program.
	notify func(tea.Msg)
	// paused stops echo captures while drover is hidden (visibility.go).
	paused atomic.Bool
}

type echoTarget struct {
	driver  tmux.Driver
	session string
}

// echoMsg is a capture taken right after input, ahead of the next poll.
type echoMsg struct {
	session string
	cap     tmux.Capture
	at      time.Time
}

func newSender() *sender {
	s := &sender{ch: make(chan func(), 512), echoCh: make(chan echoTarget, 1)}
	go func() {
		for f := range s.ch {
			f()
		}
	}()
	go s.echoLoop()
	return s
}

func (s *sender) do(f func()) {
	select {
	case s.ch <- f:
	default: // queue full: dropping beats freezing the UI
	}
}

// typed runs send and then asks for the echo to be captured.
func (s *sender) typed(driver tmux.Driver, session string, send func()) {
	s.do(func() {
		send()
		select {
		case s.echoCh <- echoTarget{driver, session}:
		default: // a capture is already pending; it will see this key too
		}
	})
}

// echoLoop captures at each of echoWaits after the latest key. A key that
// arrives mid-sequence restarts it, so a burst of typing costs one sequence
// rather than one per key.
func (s *sender) echoLoop() {
	for target := range s.echoCh {
		for restart := true; restart; {
			restart = false
			for _, wait := range echoWaits {
				select {
				case target = <-s.echoCh:
					restart = true
				case <-time.After(wait):
					s.capture(target)
					continue
				}
				break
			}
		}
	}
}

func (s *sender) capture(t echoTarget) {
	if s.notify == nil || s.paused.Load() {
		return
	}
	at := time.Now()
	if c, err := t.driver.Capture(t.session); err == nil {
		s.notify(echoMsg{session: t.session, cap: c, at: at})
	}
}

// Notify connects the model to its running program, so input echoes can be
// pushed instead of waiting for the poll. Call it once, before Run.
func (m Model) Notify(send func(tea.Msg)) {
	m.send.notify = send
	m.push.notify = send
}
