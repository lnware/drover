package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reloadModel(t *testing.T, content string, fixed bool) (Model, string) {
	t.Helper()
	t.Cleanup(func() { SetTheme(DefaultTheme) })
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return testModel().WithConfigFile(path, fixed), path
}

func TestRReloadsThemeAndSettings(t *testing.T) {
	m, path := reloadModel(t, "", false)
	if err := os.WriteFile(path, []byte("theme = \"catppuccin-mocha\"\nicons = \"nerd\"\nprojects = [\"/other\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressKey(t, m, "R")
	if m.cfg.Icons != "nerd" || len(m.cfg.Projects) != 1 || m.cfg.Projects[0] != "/other" {
		t.Errorf("settings not applied: %+v", m.cfg)
	}
	if colBand != themes["catppuccin-mocha"].band {
		t.Error("the theme was not switched")
	}
	if !strings.Contains(m.notice, "config reloaded") || !strings.Contains(m.notice, "projects changed") {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestReloadKeepsCommandLineProjects(t *testing.T) {
	m, path := reloadModel(t, "", true)
	before := m.cfg.Projects
	if err := os.WriteFile(path, []byte("projects = [\"/from/file\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressKey(t, m, "R")
	if len(m.cfg.Projects) != 1 || m.cfg.Projects[0] != before[0] {
		t.Errorf("projects given on the command line were replaced: %v", m.cfg.Projects)
	}
}

func TestReloadWithBrokenFileChangesNothing(t *testing.T) {
	m, path := reloadModel(t, "", false)
	was := m.cfg
	if err := os.WriteFile(path, []byte("theme = [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressKey(t, m, "R")
	if m.cfg.Theme != was.Theme || m.cfg.Icons != was.Icons {
		t.Errorf("a file that does not parse changed the config: %+v", m.cfg)
	}
	if !strings.Contains(m.status, "not reloaded") {
		t.Errorf("status = %q, want the parse error", m.status)
	}
}

func TestReloadReportsWhatNeedsARestart(t *testing.T) {
	m, path := reloadModel(t, "", false)
	was := m.cfg
	if err := os.WriteFile(path, []byte("socket = \"another\"\nmouse = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressKey(t, m, "R")
	if m.cfg.Socket != was.Socket || m.cfg.Mouse != was.Mouse {
		t.Errorf("socket and mouse cannot change while running: %+v", m.cfg)
	}
	if !strings.Contains(m.notice, "socket and mouse change needs a restart") {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestReloadKeepsThemeWhenTheNewNameIsUnknown(t *testing.T) {
	m, path := reloadModel(t, "", false)
	m.cfg.Theme = "catppuccin-mocha"
	SetTheme(m.cfg.Theme)
	if err := os.WriteFile(path, []byte("theme = \"solarized\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressKey(t, m, "R")
	if m.cfg.Theme != "catppuccin-mocha" || colBand != themes["catppuccin-mocha"].band {
		t.Errorf("an unknown theme replaced the current one: %q", m.cfg.Theme)
	}
	if !strings.Contains(m.status, "unknown theme") {
		t.Errorf("status = %q", m.status)
	}
}

func TestConfigEditCmdPassesThePathAsAnArgument(t *testing.T) {
	cmd := configEditCmd("code -w", "/tmp/my config; rm -rf x.toml")
	want := []string{"sh", "-c", `code -w "$1"`, "drover", "/tmp/my config; rm -rf x.toml"}
	if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
}

func TestConfigEditorPrefersVisualThenEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano")
	if got := configEditor(); got != "nano" {
		t.Errorf("EDITOR only: %q", got)
	}
	t.Setenv("VISUAL", "code -w")
	if got := configEditor(); got != "code -w" {
		t.Errorf("VISUAL wins: %q", got)
	}
}

func TestEditedConfigIsReloadedButAnEditorErrorIsNot(t *testing.T) {
	m, path := reloadModel(t, "", false)
	if err := os.WriteFile(path, []byte("icons = \"nerd\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, _ := m.handleConfigEdited(configEdited{})
	if next.(Model).cfg.Icons != "nerd" {
		t.Error("the config was not re-read after the editor exited")
	}
	next, _ = m.handleConfigEdited(configEdited{err: os.ErrNotExist})
	if next.(Model).cfg.Icons == "nerd" || !strings.Contains(next.(Model).status, "editor") {
		t.Errorf("a failed editor must not reload: %+v", next.(Model).status)
	}
}
