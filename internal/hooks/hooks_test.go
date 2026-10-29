package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lnware/drover/internal/status"
)

func TestSettingsAreValidAndUseKnownStates(t *testing.T) {
	out, err := Settings("/opt/my tools/drover")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("settings are not valid JSON: %v", err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "PermissionRequest", "Notification", "Stop", "SessionEnd"} {
		if len(parsed.Hooks[event]) == 0 {
			t.Errorf("missing %s", event)
		}
	}
	for event, groups := range parsed.Hooks {
		for _, g := range groups {
			cmd := g.Hooks[0].Command
			if !strings.HasPrefix(cmd, `'/opt/my tools/drover' hook `) {
				t.Errorf("%s: command %q does not quote the binary path", event, cmd)
			}
			state := cmd[strings.LastIndex(cmd, " ")+1:]
			if state != "ended" && !status.Valid(state) {
				t.Errorf("%s records unknown state %q", event, state)
			}
		}
	}
	if n := len(parsed.Hooks["Notification"]); n != 2 {
		t.Errorf("Notification has %d matcher groups, want 2", n)
	}
}

func TestWriteSettingsReplacesStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "claude-settings.json")
	if err := WriteSettings(path, "/old/drover"); err != nil {
		t.Fatal(err)
	}
	if err := WriteSettings(path, "/new/drover"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/old/drover") || !strings.Contains(string(data), "/new/drover") {
		t.Errorf("file still points at the old binary:\n%s", data)
	}
}

func TestCommand(t *testing.T) {
	const p = "/s/my settings.json"
	for _, c := range []struct{ in, want string }{
		{"claude", "claude --settings '/s/my settings.json'"},
		{"/opt/homebrew/bin/claude --model opus", "/opt/homebrew/bin/claude --model opus --settings '/s/my settings.json'"},
		{"mywrap {settings} --flag", "mywrap '/s/my settings.json' --flag"},
		{"codex", "codex"},
		{"echo hi; claude", "echo hi; claude"}, // a shell chain is not recognised; use {settings}
		{"", ""},
	} {
		if got := Command(c.in, p); got != c.want {
			t.Errorf("Command(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote(`a'b c`), `'a'\''b c'`; got != want {
		t.Errorf("shellQuote = %s, want %s", got, want)
	}
}
