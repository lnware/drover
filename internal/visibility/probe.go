// Package visibility answers one question for drover: is anybody looking at
// it? The only way to ask is the tmux that drover itself runs in.
//
// Why the outer tmux and not terminal focus events: drover is normally run in
// a popup, i.e. in a session `<parent>_popup` that a popup client attaches to.
// Closing the popup detaches that client and leaves the session, and drover,
// running. Tested with tmux 3.8, `focus-events on` and an app that had asked
// for focus reporting (CSI ? 1004 h): the pane received no focus-in or
// focus-out when the popup opened or closed, because tmux reports focus of the
// outer terminal, not attach and detach. Reporting would also depend on a
// user setting. `#{session_attached}` goes 0 -> 1 -> 0 exactly with the popup.
//
// Hooks (client-attached, client-detached) were rejected too: they would mean
// changing the user's tmux configuration from a TUI.
package visibility

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Probe reports whether drover can be seen. An error means the answer is
// unknown; callers must then treat drover as visible, since a wrongly paused
// UI is worse than a wasted poll.
type Probe func() (bool, error)

// format is read in one tmux call: the session has a client attached, the
// window is the one shown, and the pane is not hidden behind a zoomed sibling.
const format = "#{session_attached} #{window_active} #{window_zoomed_flag} #{pane_active}"

// FromEnv builds a probe from $TMUX and $TMUX_PANE, or returns nil when drover
// does not run inside tmux, in which case it is always visible and needs no
// probing at all.
func FromEnv() Probe {
	socket, ok := socketPath(os.Getenv("TMUX"))
	pane := os.Getenv("TMUX_PANE")
	if !ok || pane == "" {
		return nil
	}
	return New(socket, pane)
}

// New probes the pane on the tmux server listening on socket (a path, as in
// tmux -S).
func New(socket, pane string) Probe {
	return func() (bool, error) {
		// -f /dev/null: should the server have gone, never load the user's
		// configuration. -S, not $TMUX: drover's own private server is
		// addressed with -L elsewhere and must not be confused with this one.
		out, err := exec.Command("tmux", "-f", "/dev/null", "-S", socket,
			"display-message", "-p", "-t", pane, format).Output()
		if err != nil {
			return true, fmt.Errorf("tmux display-message: %w", err)
		}
		return parse(string(out))
	}
}

// socketPath extracts the socket from $TMUX, "socket,server-pid,session-id".
// It is split from the right because the socket path may itself hold commas.
func socketPath(env string) (string, bool) {
	for range 2 {
		i := strings.LastIndexByte(env, ',')
		if i < 0 {
			return "", false
		}
		env = env[:i]
	}
	return env, env != ""
}

func parse(out string) (bool, error) {
	f := strings.Fields(out)
	if len(f) != 4 {
		return true, fmt.Errorf("unexpected tmux output %q", strings.TrimSpace(out))
	}
	attached, windowActive, zoomed, paneActive := f[0], f[1], f[2], f[3]
	if attached == "0" || windowActive != "1" {
		return false, nil
	}
	return !(zoomed == "1" && paneActive != "1"), nil
}
