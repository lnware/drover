package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIconsDefaultsAndParsing(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) Config {
		t.Helper()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%q): %v", body, err)
		}
		return cfg
	}

	if got := Default().Icons; got != "unicode" {
		t.Errorf("default icons = %q, want unicode", got)
	}
	if got := load(`agent_cmd = "claude"`).Icons; got != "unicode" {
		t.Errorf("missing key: icons = %q, want unicode", got)
	}
	if got := load(`icons = "nerd"`).Icons; got != "nerd" {
		t.Errorf(`icons = "nerd": got %q`, got)
	}
	for _, bad := range []string{`icons = "garbage"`, `icons = ""`, `icons = "Nerdy"`} {
		if got := load(bad).Icons; got != "unicode" {
			t.Errorf("%s: icons = %q, want unicode fallback", bad, got)
		}
	}
}

func TestMouseDefaultsOnAndCanBeTurnedOff(t *testing.T) {
	dir := t.TempDir()
	missing, err := Load(filepath.Join(dir, "none.toml"))
	if err != nil || !missing.Mouse {
		t.Fatalf("no config file: mouse = %v, err = %v; want on", missing.Mouse, err)
	}
	p := filepath.Join(dir, "c.toml")
	if err := os.WriteFile(p, []byte(`socket = "x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(p); !c.Mouse {
		t.Error("a config that does not mention mouse must keep it on")
	}
	if err := os.WriteFile(p, []byte("mouse = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(p); c.Mouse {
		t.Error("mouse = false was ignored")
	}
}

func TestZoomKeyDefaultsAndNormalises(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) string {
		t.Helper()
		p := filepath.Join(dir, "z.toml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		return c.ZoomKey
	}
	if got := Default().ZoomKey; got != DefaultZoomKey {
		t.Errorf("default = %q", got)
	}
	for body, want := range map[string]string{
		`socket = "x"`:         DefaultZoomKey, // not mentioned
		`zoom_key = ""`:        DefaultZoomKey,
		`zoom_key = "F10"`:     "f10", // case-insensitive
		`zoom_key = " alt+z "`: "alt+z",
		`zoom_key = "ctrl+\\"`: DefaultZoomKey, // already leaves focus
		`zoom_key = "ctrl+q"`:  DefaultZoomKey,
	} {
		if got := load(body); got != want {
			t.Errorf("%s: zoom key %q, want %q", body, got, want)
		}
	}
}

func TestThemeIsReadFromTheConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("theme = \"catppuccin-mocha\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "catppuccin-mocha" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
	if def := Default().Theme; def != "" {
		t.Errorf("default Theme = %q, want empty (the ui package owns the default)", def)
	}
}

func TestEnsureFileCreatesOnceAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("the template must be a valid config: %v", err)
	}
	if err := os.WriteFile(path, []byte("icons = \"nerd\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "icons = \"nerd\"\n" {
		t.Errorf("an existing file was overwritten: %q", b)
	}
}
