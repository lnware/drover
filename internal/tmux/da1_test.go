package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnansweredDA1(t *testing.T) {
	for _, c := range []struct {
		name string
		line string
		pane string
		ok   bool
	}{
		{"wrapped DA1", `%output %7 \033Ptmux;\033\033[0c\033\134`, "%7", true},
		{"wrapped DA1 without the zero", `%output %12 x\033Ptmux;\033\033[c\033\134y`, "%12", true},
		{"inside a longer burst", `%output %3 \033]11;?\007\033Ptmux;\033\033[>q\033\134\033Ptmux;\033\033[0c\033\134\033[1K`, "%3", true},
		{"plain DA1 is answered by tmux itself", `%output %7 \033[0c`, "", false},
		{"other wrapped queries", `%output %7 \033Ptmux;\033\033[>q\033\134`, "", false},
		{"not output", `%window-add @3`, "", false},
		{"extended output is left alone", `%extended-output %7 1 : \033Ptmux;\033\033[0c`, "", false},
		{"no pane id", `%output \033Ptmux;\033\033[0c`, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			pane, ok := unansweredDA1([]byte(c.line))
			if ok != c.ok || pane != c.pane {
				t.Errorf("unansweredDA1(%q) = %q, %v; want %q, %v", c.line, pane, ok, c.pane, c.ok)
			}
		})
	}
}

func TestSendBytesDeliversControlCharacters(t *testing.T) {
	d := testDriver(t)
	out := filepath.Join(t.TempDir(), "got")
	if err := d.Spawn("b", t.TempDir(), "stty raw -echo; head -c 6 > "+out); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := d.SendBytes("=b:", []byte("\x1b[?62c")); err != nil {
		t.Fatal(err)
	}
	if got := waitForFile(t, out, 6); got != "\x1b[?62c" {
		t.Errorf("pane received %q", got)
	}
}

// waitForFile polls until path holds at least n bytes, or fails.
func waitForFile(t *testing.T, path string, n int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) >= n {
			return string(b)
		}
		time.Sleep(30 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("%s holds %q after 5s, want %d bytes", filepath.Base(path), b, n)
	return ""
}

// The point of it all: a program that asks the outside terminal for its
// device attributes through passthrough gets an answer. Before this, nothing
// did, and yazi waited five seconds on exit.
func TestWatcherAnswersAPassthroughDA1(t *testing.T) {
	d := testDriver(t)
	out := filepath.Join(t.TempDir(), "got")
	script := `sleep 1; stty raw -echo; printf '\033Ptmux;\033\033[0c\033\\'; head -c 6 > ` + out
	if err := d.Spawn("w", t.TempDir(), script); err != nil {
		t.Fatal(err)
	}
	w, err := d.Watch("w")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	if got := waitForFile(t, out, 6); got != "\x1b[?62c" {
		t.Errorf("the program received %q, want drover's DA1 reply", got)
	}
}

// tmux answers a plain DA1 itself; a second answer from drover would arrive as
// stray keystrokes.
func TestWatcherLeavesAPlainDA1Alone(t *testing.T) {
	d := testDriver(t)
	out := filepath.Join(t.TempDir(), "got")
	script := `sleep 1; stty raw -echo; printf '\033[0c'; head -c 8 > ` + out + `; sleep 1`
	if err := d.Spawn("p", t.TempDir(), script); err != nil {
		t.Fatal(err)
	}
	w, err := d.Watch("p")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	got := waitForFile(t, out, 8) // tmux's own reply is ESC[?1;2;4c, 8 bytes
	if strings.Contains(got, "62c") {
		t.Errorf("drover answered a DA1 that tmux had already answered: %q", got)
	}
}
