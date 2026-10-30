// Package tmux drives a private tmux server that owns the agent sessions, so
// they survive the TUI and never touch the user's own tmux.
package tmux

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultPrefix differs from the usual outer prefixes (C-b, C-Space) so keys
// pass through when attached from inside the user's own tmux.
const DefaultPrefix = "C-a"

type Driver struct {
	Socket string // tmux -L name
	Prefix string
}

func New(socket string) Driver {
	return Driver{Socket: socket, Prefix: DefaultPrefix}
}

func (d Driver) command(args ...string) *exec.Cmd {
	// -f /dev/null: the private server ignores the user's tmux.conf, which
	// is where plugins and the outer prefix live.
	// -u: force UTF-8 so captured panes keep their multibyte glyphs even
	// when the environment has no UTF-8 locale.
	full := append([]string{"-u", "-L", d.Socket, "-f", "/dev/null"}, args...)
	return exec.Command("tmux", full...)
}

func (d Driver) run(args ...string) (string, error) {
	cmd := d.command(args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// SessionName builds a tmux-safe session name; tmux rejects '.' and ':'.
func SessionName(project, branch string) string {
	return sanitize(project + "/" + branch)
}

func sanitize(s string) string {
	return strings.NewReplacer(".", "_", ":", "_", " ", "_").Replace(s)
}

// Sessions lists the live worktree (agent) session names. Companion panes
// live on the same server but are not worktrees, so they are left out; see
// ListSessions for both. A missing server means no sessions.
func (d Driver) Sessions() ([]string, error) {
	agents, _, err := d.ListSessions()
	return agents, err
}

func (d Driver) allSessions() ([]string, error) {
	out, err := d.run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func noServer(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "error connecting to") ||
		strings.Contains(msg, "No such file or directory")
}

// Has reports whether a session with exactly this name exists.
func (d Driver) Has(name string) bool {
	return d.command("has-session", "-t", "="+name).Run() == nil
}

// Spawn starts a detached session running command in dir.
func (d Driver) Spawn(name, dir, command string) error {
	// The options go in the same invocation, ahead of new-session: a pane
	// reads history-limit when it is created, so setting it afterwards would
	// leave the first session of a fresh server with tmux's default of 2000.
	args := append(d.configureArgs(), ";", "new-session", "-d", "-s", name, "-c", dir,
		"-e", "DROVER_SESSION="+name, command)
	_, err := d.run(args...)
	return err
}

// configureArgs are the server-wide options as one tmux command line. They
// are idempotent, so they run on every spawn rather than tracking whether the
// server is fresh.
func (d Driver) configureArgs() []string {
	return []string{
		"set-option", "-g", "prefix", d.Prefix, ";",
		"set-option", "-g", "escape-time", "0", ";",
		// Programs inside check this option and nag about it when it is off
		// (Claude Code does), and the user's tmux.conf cannot reach this
		// server. Focus events themselves only flow to a pane through an
		// attached client, so this mainly serves A.
		"set-option", "-g", "focus-events", "on", ";",
		"set-option", "-g", "history-limit", strconv.Itoa(HistoryLimit), ";",
		"set-option", "-g", "status-style", "bg=colour60,fg=colour255", ";",
		"set-option", "-g", "status-left", " drover | #S ", ";",
		"set-option", "-g", "status-left-length", "60", ";",
		"set-option", "-g", "status-right", " " + d.Prefix + " d: back to drover ",
	}
}

// AttachCmd returns the command that attaches to a session. TMUX is removed
// from the environment, otherwise tmux refuses to nest.
func (d Driver) AttachCmd(name string) *exec.Cmd {
	cmd := d.command("attach-session", "-t", "="+name)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	return cmd
}

func (d Driver) Kill(name string) error {
	_, err := d.run("kill-session", "-t", "="+name)
	return err
}

// KillServer stops the private server and every session on it, and removes
// its socket file: tmux leaves the file behind, so a server per test or per
// run would otherwise pile up thousands of dead sockets in the tmux directory.
func (d Driver) KillServer() error {
	_, err := d.run("kill-server")
	if err != nil && noServer(err) {
		err = nil
	}
	if err == nil {
		_ = os.Remove(d.socketPath())
	}
	return err
}

// socketPath is where tmux puts the socket of `-L name`: $TMUX_TMPDIR (else
// /tmp), then tmux-<uid>. It is worked out here because a server that has
// already exited can no longer be asked.
func (d Driver) socketPath() string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), d.Socket)
}
