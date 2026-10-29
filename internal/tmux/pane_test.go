package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func testDriver(t *testing.T) Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := New(fmt.Sprintf("drover-pane-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "_")))
	t.Cleanup(func() { _ = d.KillServer() })
	return d
}

// waitFor polls a capture until it contains want, or fails.
func waitFor(t *testing.T, d Driver, name, want string) Capture {
	t.Helper()
	var c Capture
	for range 40 {
		var err error
		if c, err = d.Capture(name); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(c.Lines, "\n"), want) {
			return c
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane never showed %q:\n%s", want, strings.Join(c.Lines, "\n"))
	return c
}

func TestResizeAndCapture(t *testing.T) {
	d := testDriver(t)
	if err := d.Spawn("s", t.TempDir(), `printf '\033[31mhello\033[0m'; sleep 60`); err != nil {
		t.Fatal(err)
	}
	if err := d.Resize("s", 60, 12); err != nil {
		t.Fatal(err)
	}
	c := waitFor(t, d, "s", "hello")
	if len(c.Lines) != 12 {
		t.Errorf("capture has %d lines, want pane height 12", len(c.Lines))
	}
	if !strings.Contains(c.Lines[0], "\x1b[31m") {
		t.Errorf("capture lost SGR colors: %q", c.Lines[0])
	}
	if got, _ := d.run("display-message", "-p", "-t", target("s"), "#{pane_width}"); got != "60" {
		t.Errorf("pane width = %q, want 60", got)
	}
	if c.CursorX != 5 || c.CursorY != 0 {
		t.Errorf("cursor = %d,%d want 5,0", c.CursorX, c.CursorY)
	}
}

func TestSendTextKeysAndPaste(t *testing.T) {
	d := testDriver(t)
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	if err := d.SendText("s", "-ä x"); err != nil { // leading dash and non-ASCII
		t.Fatal(err)
	}
	if err := d.SendKeys("s", "Enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, d, "s", "-ä x")
	if err := d.Paste("s", "one\ntwo"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, d, "s", "one")
}

// tmux's command parser reads an argument that ends in ';' as the end of a
// command, so literal text and key names ending in a semicolon used to
// vanish: typing ";" into an agent or editor sent nothing. This pins every
// shape of the problem against a real tmux.
func TestSemicolonsReachThePaneIntact(t *testing.T) {
	d := testDriver(t)
	if err := d.Spawn("s", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	// Letters between the cases show where each one landed.
	var want strings.Builder
	for i, text := range []string{";", "x;", "a;b", `\;`, `y\;`, ";;", "end;"} {
		if err := d.SendText("s", text); err != nil {
			t.Fatal(err)
		}
		marker := string(rune('A' + i))
		if err := d.SendText("s", marker); err != nil {
			t.Fatal(err)
		}
		want.WriteString(text + marker)
	}
	c := waitFor(t, d, "s", "end;G")
	if got := strings.TrimSpace(c.Lines[0]); got != want.String() {
		t.Errorf("pane received %q\nwant            %q", got, want.String())
	}
}

func TestSemicolonKeyNamesAndPastesSurvive(t *testing.T) {
	d := testDriver(t)
	if err := d.Spawn("s", t.TempDir(), "cat -v"); err != nil {
		t.Fatal(err)
	}
	// Alt+; arrives as the key name "M-;", which ends in a semicolon.
	if err := d.SendKeys("s", "M-;"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, d, "s", "^[;")

	d2 := testDriver(t)
	if err := d2.Spawn("p", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	if err := d2.Paste("p", "first;second;"); err != nil { // a paste ending in ';'
		t.Fatal(err)
	}
	waitFor(t, d2, "p", "first;second;")
}

// Claude Code reads this option and prints a warning when it is off; the
// private server never loads a tmux.conf, so drover has to set it.
func TestServerEnablesFocusEvents(t *testing.T) {
	d := testDriver(t)
	if err := d.Spawn("f", t.TempDir(), "cat"); err != nil {
		t.Fatal(err)
	}
	got, err := d.run("show-options", "-gv", "focus-events")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "on" {
		t.Errorf("focus-events = %q, want on", strings.TrimSpace(got))
	}
}
