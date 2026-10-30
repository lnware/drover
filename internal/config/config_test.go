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

func TestBackgroundDefaultsOnAndCanBeSwitchedOff(t *testing.T) {
	if !Default().Background {
		t.Error("the chrome background defaults to on")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("background = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Background {
		t.Error("background = false was ignored")
	}
}

func TestSwitchKeyDefaultsAndRefusesTakenKeys(t *testing.T) {
	load := func(body string) string {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return c.SwitchKey
	}
	if got := Default().SwitchKey; got != DefaultSwitchKey {
		t.Errorf("default = %q", got)
	}
	for body, want := range map[string]string{
		`socket = "x"`:          DefaultSwitchKey,
		`switch_key = "F11"`:    "f11",
		`switch_key = "ctrl+q"`: DefaultSwitchKey, // leaves focus
		`zoom_key = "f12"` + "\n" + `switch_key = "f12"`: DefaultSwitchKey, // the zoom key
	} {
		if got := load(body); got != want {
			t.Errorf("%s: switch key %q, want %q", body, got, want)
		}
	}
}

func TestContextsAndContextKeyFromTheConfig(t *testing.T) {
	load := func(body string) Config {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := load("[[context]]\nname = \"Work\"\nmatch = [\"Acme\", \"project:tools\"]\n\n[[context]]\nname = \" \"\nmatch = [\"x\"]\n\n[[context]]\nname = \"Free time\"\nmatch = [\"Personal\"]\n")
	if len(c.Contexts) != 2 || c.Contexts[0].Name != "Work" || c.Contexts[1].Name != "Free time" || c.Contexts[0].Match[1] != "project:tools" {
		t.Errorf("contexts keep their order and a nameless one is dropped: %+v", c.Contexts)
	}
	if got := Default().ContextKey; got != DefaultContextKey {
		t.Errorf("default = %q", got)
	}
	for body, want := range map[string]string{
		`socket = "x"`:                                 DefaultContextKey,
		`context_key = "F8"`:                           "f8",
		`context_key = "ctrl+q"`:                       DefaultContextKey, // leaves focus
		`switch_key = "f11"`:                           "f10",             // the default is taken, so the next free one
		`context_key = "f12"`:                          DefaultContextKey, // the switch key
		`zoom_key = "f11"`:                             "f10",
		`context_key = "f9"` + "\nswitch_key = \"f9\"": DefaultContextKey,
	} {
		if got := load(body).ContextKey; got != want {
			t.Errorf("%s: context key %q, want %q", body, got, want)
		}
	}
}
