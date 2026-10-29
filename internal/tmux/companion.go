package tmux

import "strings"

// Companion panes (terminal, editor, lazygit) are tmux sessions of their own
// on the same private server, one per worktree and kind. Their names must
// never equal a worktree session name, since the UI maps session names to
// worktree rows.
//
// Worktree sessions are always "<project>/<path>", so they always contain a
// "/". A companion name is "<kind>@<worktree session>" with every "/" (and
// the "%" that escapes it) percent-encoded, so it never contains one. The
// two name spaces are therefore disjoint by construction, not by luck, and
// the encoding is reversible so a name can be traced back to its worktree.
const companionSep = "@"

var companionKinds = []string{"term", "edit", "git"}

// CompanionName is the tmux session name of a worktree's companion of the
// given kind ("term", "edit" or "git").
func CompanionName(kind, session string) string {
	esc := strings.NewReplacer("%", "%25", "/", "%2F").Replace(session)
	return kind + companionSep + esc
}

// ParseCompanion splits a companion session name back into its kind and the
// worktree session it belongs to.
func ParseCompanion(name string) (kind, session string, ok bool) {
	if strings.Contains(name, "/") {
		return "", "", false // a worktree session
	}
	kind, esc, found := strings.Cut(name, companionSep)
	if !found || !isKind(kind) {
		return "", "", false
	}
	return kind, strings.NewReplacer("%2F", "/", "%25", "%").Replace(esc), true
}

// IsCompanion reports whether name is a companion session rather than a
// worktree's agent session.
func IsCompanion(name string) bool {
	_, _, ok := ParseCompanion(name)
	return ok
}

func isKind(k string) bool {
	for _, c := range companionKinds {
		if k == c {
			return true
		}
	}
	return false
}

// ListSessions splits the live sessions into worktree agent sessions and
// companions in one tmux call. A missing server means none of either.
func (d Driver) ListSessions() (agents, companions []string, err error) {
	all, err := d.allSessions()
	if err != nil {
		return nil, nil, err
	}
	for _, s := range all {
		if IsCompanion(s) {
			companions = append(companions, s)
		} else {
			agents = append(agents, s)
		}
	}
	return agents, companions, nil
}

// SpawnCompanion starts a detached companion session running command in dir.
// Unlike Spawn it sets no DROVER_SESSION: a shell or editor must not report
// agent state under the worktree's name, and title is what the status line
// shows when the companion is attached full screen.
func (d Driver) SpawnCompanion(name, dir, command, title string) error {
	// Options first, in the same invocation (see Spawn): a pane reads
	// history-limit when it is created.
	args := append(d.configureArgs(), ";", "new-session", "-d", "-s", name, "-c", dir, command)
	if _, err := d.run(args...); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", target(name), "status-left", " drover | "+strings.ReplaceAll(title, "#", "##")+" ")
	return err
}
