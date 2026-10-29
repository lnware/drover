// Package notify raises a desktop notification. It is best effort: a missing
// notifier must never disturb the TUI.
package notify

import (
	"os/exec"
	"runtime"
)

// Send shows a notification and returns without waiting for the notifier.
func Send(title, body string) error {
	cmd := command(title, body)
	if cmd == nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func command(title, body string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		// Text goes in as handler arguments, never spliced into the script.
		return exec.Command("osascript",
			"-e", "on run argv",
			"-e", "display notification (item 1 of argv) with title (item 2 of argv)",
			"-e", "end run",
			body, title)
	case "linux":
		return exec.Command("notify-send", title, body)
	}
	return nil
}
