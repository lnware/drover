package tmux

import (
	"bufio"
	"bytes"
	"io"
	"os/exec"
)

// Watcher tells you when a session's pane produced output, without polling.
// It runs a tmux control-mode client (`tmux -C attach`), which tmux feeds
// with a %output line whenever a pane of the session prints something. A
// capture is then only needed after such a line, so an idle pane costs
// nothing and a busy one is followed at whatever rate the caller chooses.
//
// The client is created with ignore-size, so it never takes part in window
// sizing (Resize keeps the window at the right pane's size). It is not
// read-only: tmux then refuses send-keys from every other client too ("client
// is read-only"), which is how the first version of this broke typing. It
// never sends a command itself, since nothing is written to its stdin.
type Watcher struct {
	driver  Driver
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	changed chan struct{}
	done    chan struct{}
}

// Watch starts a control-mode client on the session. It fails if tmux
// cannot start or the session does not exist (the client exits at once, which
// shows as Done).
func (d Driver) Watch(name string) (*Watcher, error) {
	cmd := d.command("-C", "attach-session", "-t", "="+name, "-f", "ignore-size")
	// A control client reads commands from stdin and exits at EOF, so the
	// pipe stays open for as long as the watcher lives. It also closes when
	// drover itself exits, which takes the client down with it.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	w := &Watcher{driver: d, cmd: cmd, stdin: stdin, changed: make(chan struct{}, 1), done: make(chan struct{})}
	go w.read(stdout)
	return w, nil
}

// read turns %output lines into at most one pending signal: the consumer
// only needs to know that something changed, and a flood of output must not
// queue up work.
func (w *Watcher) read(r io.Reader) {
	defer close(w.done)
	defer func() { _ = w.cmd.Wait() }()
	br := bufio.NewReaderSize(r, 64<<10)
	continued := false // inside a line longer than the buffer
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 && !continued && isOutput(line) {
			if pane, ok := unansweredDA1(line); ok {
				go w.answerDA1(pane)
			}
			select {
			case w.changed <- struct{}{}:
			default:
			}
		}
		continued = err == bufio.ErrBufferFull
		if err != nil && !continued {
			return
		}
	}
}

// da1Reply is what drover answers a passthrough DA1 with: a VT220-class
// terminal that claims no extra features. Claiming none matters, since a
// program that hears "sixel" may start sending images that nothing here can
// show.
var da1Reply = []byte("\x1b[?62c")

// passthroughDA1 is a device-attributes query wrapped in tmux's passthrough,
// as control mode prints it (ESC is \033). Programs that find they run inside
// tmux send their queries this way to reach the terminal outside it (yazi
// does, in a second round of probing). tmux answers a plain DA1 itself, but
// drops a wrapped one unless allow-passthrough is on and a client is attached,
// so on the private server nobody answers and the program waits out a
// timeout of its own: yazi, five seconds when it quits.
var passthroughDA1 = [][]byte{
	[]byte(`Ptmux;\033\033[0c`),
	[]byte(`Ptmux;\033\033[c`),
}

// unansweredDA1 reports whether a control-mode %output line carries a wrapped
// DA1 query, and for which pane.
func unansweredDA1(line []byte) (pane string, ok bool) {
	rest, found := bytes.CutPrefix(line, []byte("%output "))
	if !found {
		return "", false
	}
	id, data, found := bytes.Cut(rest, []byte(" "))
	if !found || len(id) < 2 || id[0] != '%' {
		return "", false
	}
	for _, q := range passthroughDA1 {
		if bytes.Contains(data, q) {
			return string(id), true
		}
	}
	return "", false
}

// answerDA1 plays the outside terminal for one wrapped DA1 query.
func (w *Watcher) answerDA1(pane string) {
	_ = w.driver.SendBytes(pane, da1Reply)
}

func isOutput(line []byte) bool {
	return bytes.HasPrefix(line, []byte("%output ")) || bytes.HasPrefix(line, []byte("%extended-output "))
}

// Changed receives a value after the pane printed something. Values are
// coalesced, so one receive can stand for many outputs.
func (w *Watcher) Changed() <-chan struct{} { return w.changed }

// Done is closed once the control client has exited: the session ended, or
// the watcher was closed.
func (w *Watcher) Done() <-chan struct{} { return w.done }

// Close ends the control client.
func (w *Watcher) Close() {
	_ = w.stdin.Close()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
}
