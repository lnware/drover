// Package hooks builds the Claude Code hooks that feed drover's status files
// and injects them into each session with `claude --settings <file>`. The
// user's own settings.json is never read or written: Claude Code merges the
// extra file with the settings it already loads.
package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Placeholder in agent_cmd that is replaced by the quoted settings path, for
// wrappers where --settings cannot simply be appended.
const Placeholder = "{settings}"

type handler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type group struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []handler `json:"hooks"`
}

// events maps each Claude Code hook event to the state it records. Order
// matters only for readability of the generated JSON.
var events = []struct {
	Event, Matcher, State string
}{
	{"SessionStart", "", "idle"},
	{"UserPromptSubmit", "", "working"},
	{"PreToolUse", "", "working"},
	// After a permission prompt is approved nothing else fires until the
	// next tool starts, so finishing the tool must flip waiting back.
	{"PostToolUse", "", "working"},
	{"PostToolUseFailure", "", "working"},
	{"PermissionDenied", "", "working"},
	{"PermissionRequest", "", "waiting"},
	{"Notification", "permission_prompt|elicitation_dialog", "waiting"},
	{"Notification", "idle_prompt", "done"},
	{"Stop", "", "done"},
	{"StopFailure", "", "error"},
	{"SessionEnd", "", "ended"},
}

// Settings returns the settings JSON whose hook commands run bin.
func Settings(bin string) ([]byte, error) {
	hooks := map[string][]group{}
	for _, e := range events {
		hooks[e.Event] = append(hooks[e.Event], group{
			Matcher: e.Matcher,
			Hooks:   []handler{{Type: "command", Command: shellQuote(bin) + " hook " + e.State}},
		})
	}
	return json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
}

// WriteSettings writes the settings file for bin to path, replacing it
// atomically. It runs on every spawn so a moved or rebuilt binary is never
// left behind in a stale file.
func WriteSettings(path, bin string) error {
	data, err := Settings(bin)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Command turns the configured agent command into the one to run: Claude
// gets --settings appended, a {settings} placeholder is filled in, anything
// else runs unchanged because only Claude Code understands these hooks.
func Command(agentCmd, settingsPath string) string {
	quoted := shellQuote(settingsPath)
	if strings.Contains(agentCmd, Placeholder) {
		return strings.ReplaceAll(agentCmd, Placeholder, quoted)
	}
	if IsClaude(agentCmd) {
		return agentCmd + " --settings " + quoted
	}
	return agentCmd
}

// IsClaude reports whether agentCmd starts with the claude executable.
func IsClaude(agentCmd string) bool {
	fields := strings.Fields(agentCmd)
	return len(fields) > 0 && filepath.Base(fields[0]) == "claude"
}

// Prepare returns the command to run for agentCmd, first (re)writing the
// settings file when the command takes it. On error the returned command is
// still runnable, just without status hooks, so a read-only state directory
// degrades the status display instead of blocking the session.
func Prepare(agentCmd, settingsPath string) (string, error) {
	cmd := Command(agentCmd, settingsPath)
	if cmd == agentCmd {
		return cmd, nil
	}
	bin, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(bin); rerr == nil {
			bin = resolved
		}
		err = WriteSettings(settingsPath, bin)
	}
	if err != nil {
		return strings.ReplaceAll(agentCmd, Placeholder, ""), err
	}
	return cmd, nil
}

// Quote single-quotes s for sh, so a name with spaces or quotes is one word in
// a command line.
func Quote(s string) string { return shellQuote(s) }

// shellQuote single-quotes s for sh, so install paths with spaces work.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
