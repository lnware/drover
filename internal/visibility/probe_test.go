package visibility

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		out     string
		visible bool
		err     bool
	}{
		{"1 1 0 1\n", true, false},
		{"2 1 0 1\n", true, false},
		{"0 1 0 1\n", false, false}, // popup closed: no client attached
		{"1 0 0 1\n", false, false}, // another window is shown
		{"1 1 1 0\n", false, false}, // a zoomed sibling covers the pane
		{"1 1 1 1\n", true, false},  // zoomed, but it is us
		{"1 1 0 0\n", true, false},  // a split sibling is active; still on screen
		{"", true, true},
		{"1 1\n", true, true},
	} {
		got, err := parse(c.out)
		if got != c.visible || (err != nil) != c.err {
			t.Errorf("parse(%q) = %v, %v; want %v, err=%v", c.out, got, err, c.visible, c.err)
		}
	}
}

func TestSocketPath(t *testing.T) {
	for env, want := range map[string]string{
		"/private/tmp/tmux-501/default,1234,0": "/private/tmp/tmux-501/default",
		"/tmp/a,b/sock,99,12":                  "/tmp/a,b/sock",
		"":                                     "",
		"nocommas":                             "",
		"one,1":                                "",
	} {
		got, ok := socketPath(env)
		if got != want || ok != (want != "") {
			t.Errorf("socketPath(%q) = %q, %v; want %q", env, got, ok, want)
		}
	}
}

func TestFromEnvIsNilOutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	if FromEnv() != nil {
		t.Error("no $TMUX must mean no probe (always visible)")
	}
	t.Setenv("TMUX", "/tmp/x,1,0")
	if FromEnv() != nil {
		t.Error("no $TMUX_PANE must mean no probe")
	}
	t.Setenv("TMUX_PANE", "%1")
	if FromEnv() == nil {
		t.Error("$TMUX and $TMUX_PANE set must give a probe")
	}
}

// outerTmux starts a throwaway tmux server, standing in for the user's own,
// on a unique socket, and removes it when the test ends.
func outerTmux(t *testing.T) (run func(args ...string) string, socket string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	name := fmt.Sprintf("agt-hidden-t%d", time.Now().UnixNano())
	run = func(args ...string) string {
		out, err := exec.Command("tmux", append([]string{"-L", name, "-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("new-session", "-d", "-s", "popup", "-x", "100", "-y", "30", "sleep 600")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", name, "kill-server").Run() })
	return run, run("display-message", "-p", "#{socket_path}")
}

// TestProbeFollowsAttachAndWindows drives the real thing: a control-mode
// client stands in for the popup's attached client (it counts in
// session_attached like a tty client does; the real display-popup flow was
// checked by hand with a pty client).
func TestProbeFollowsAttachAndWindows(t *testing.T) {
	run, socket := outerTmux(t)
	pane := run("list-panes", "-t", "popup", "-F", "#{pane_id}")
	probe := New(socket, pane)
	want := func(step string, visible bool) {
		t.Helper()
		got, err := probe()
		if err != nil || got != visible {
			t.Fatalf("%s: visible=%v err=%v, want %v", step, got, err, visible)
		}
	}

	want("popup closed (nobody attached)", false)

	client := exec.Command("tmux", "-S", socket, "-f", "/dev/null", "-C", "attach", "-t", "popup")
	stdin, err := client.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = client.Process.Kill(); _ = client.Wait() })
	waitFor(t, "client to attach", func() bool { v, _ := probe(); return v })
	want("popup open", true)

	run("new-window", "-d", "-t", "popup", "sleep 600")
	run("select-window", "-t", "popup:1")
	want("another window selected", false)
	run("select-window", "-t", "popup:0")
	want("our window selected again", true)

	run("split-window", "-d", "-t", pane, "sleep 600")
	run("select-pane", "-t", "popup:0.1")
	run("resize-pane", "-Z", "-t", "popup:0.1")
	want("a sibling pane zoomed over us", false)
	run("resize-pane", "-Z", "-t", "popup:0.1")
	want("unzoomed", true)

	_ = stdin.Close()
	_ = client.Wait()
	waitFor(t, "client to detach", func() bool { v, _ := probe(); return !v })
	want("popup closed again", false)

	// A killed server is an error, which callers read as visible.
	run("kill-server")
	if v, err := probe(); !v || err == nil {
		t.Errorf("dead server: visible=%v err=%v, want visible with an error", v, err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}
