package ui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
)

// Polling alone cannot follow a pane that redraws continuously (an editor
// scrolling, lazygit refreshing, `seq` in a terminal): at 100 ms it shows
// about 10 frames a second. Polling faster would fix that but make every idle
// moment cost more, since each capture is a tmux process.
//
// So the pane being shown is also watched through a tmux control-mode client
// (tmux.Watcher), which is told when the pane prints. After such a signal the
// pane is captured and pushed to the UI (as an echoMsg, the same message the
// input echoes use), at most once per pushGap. An idle pane sends no signal,
// so it costs nothing, and the poll in poll.go stays as it was: it is the
// safety net if the watcher fails, and it backs off while the pushes keep
// the preview current.
const pushGap = 30 * time.Millisecond

// pushTarget is the session to push for, or "" for none. Pushing triples the
// capture work while a pane streams (measured: 8% to 25% of a core for a
// continuously printing pane, against 9 and 36 frames a second), so it is
// spent only where frame rate is felt: a companion you are typing into. An
// agent streaming an answer, or any pane you merely look at, stays on the
// back-off poll, as before.
func (m Model) pushTarget(r row) string {
	if m.mode == modeFocus && m.viewOf(r) != viewAgent {
		return m.target(r)
	}
	return ""
}

// pusher follows one session at a time: whichever pane the right side shows.
type pusher struct {
	mu      sync.Mutex
	session string
	watcher *tmux.Watcher
	stop    chan struct{}
	// notify posts a message to the running program; nil in tests that have
	// no program, which also keeps them from starting control clients.
	notify func(tea.Msg)
}

// follow makes session the watched one; an empty session stops watching.
// Asking for the session already watched does nothing, so it is cheap to call
// from every poll.
func (p *pusher) follow(driver tmux.Driver, session string) {
	if p.notify == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if session == p.session && p.watcher != nil {
		select {
		case <-p.watcher.Done(): // the session ended; a new one may reuse its name
		default:
			return
		}
	}
	p.stopLocked()
	if session == "" {
		return
	}
	w, err := driver.Watch(session)
	if err != nil {
		return // polling still works
	}
	p.session, p.watcher, p.stop = session, w, make(chan struct{})
	go p.run(driver, session, w, p.stop)
}

func (p *pusher) stopLocked() {
	if p.watcher != nil {
		close(p.stop)
		p.watcher.Close()
	}
	p.session, p.watcher, p.stop = "", nil, nil
}

// run captures after each signal, no more often than pushGap. The wait comes
// before the capture so that output arriving while waiting lands in it,
// instead of triggering another capture right behind.
func (p *pusher) run(driver tmux.Driver, session string, w *tmux.Watcher, stop <-chan struct{}) {
	var last time.Time
	for {
		select {
		case <-stop:
			return
		case <-w.Done():
			return
		case <-w.Changed():
		}
		if wait := pushGap - time.Since(last); wait > 0 {
			select {
			case <-stop:
				return
			case <-time.After(wait):
			}
		}
		select { // anything that arrived while waiting is covered by this capture
		case <-w.Changed():
		default:
		}
		last = time.Now()
		if c, err := driver.Capture(session); err == nil {
			p.notify(echoMsg{session: session, cap: c, at: last})
		}
	}
}
