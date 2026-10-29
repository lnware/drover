// Package config loads ~/.config/drover/config.toml.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	// Projects are bare-layout project roots (the directory holding .bare).
	Projects []string `toml:"projects"`
	// AgentCmd is run in each new session's worktree.
	AgentCmd string `toml:"agent_cmd"`
	// Socket names the private tmux server (tmux -L).
	Socket string `toml:"socket"`
	// Notify raises a desktop notification when an agent that is not in
	// focus starts waiting for you or finishes.
	Notify bool `toml:"notify"`
	// ResumeArgs are appended to AgentCmd to continue the previous
	// conversation; it defaults to --continue when AgentCmd is claude.
	ResumeArgs string `toml:"resume_args"`
	// Icons picks the glyph set for the git bar: "unicode" (the default) or
	// "nerd" for the Nerd Font branch glyph. Any other value means "unicode".
	Icons string `toml:"icons"`
	// Theme picks the colour palette: "jetbrains" (the default),
	// "catppuccin-mocha", "catppuccin-macchiato" or "catppuccin-frappe";
	// "catppuccin" is mocha. The ui package owns the names and falls back to
	// the default for one it does not know.
	Theme string `toml:"theme"`
	// ZoomKey toggles the shown pane between its place in the split and the
	// whole terminal while a pane is focused. The key is consumed, so the pane
	// never sees it, and no Ctrl+letter is free in every program drover hosts
	// (Claude Code, Helix, shells). ctrl+z costs least: it only suspends a
	// program, which is never wanted in a captured pane, and it is the same
	// key on every keyboard layout. Bubble Tea key names ("ctrl+z", "f10",
	// "alt+z"); ctrl+\ and ctrl+q are taken by "leave focus".
	ZoomKey string `toml:"zoom_key"`
	// Background paints the title bar, the list and the footer in the theme's
	// background, so a terminal background image or opacity does not show
	// through them. The pane that shows an agent is never painted.
	Background bool `toml:"background"`
	// Mouse turns on click and wheel support. Capturing the mouse stops the
	// terminal's own text selection (kitty: hold shift to select anyway).
	Mouse bool `toml:"mouse"`
	// TerminalCmd, EditorCmd and LazygitCmd run in the companion panes.
	// Empty means the default, resolved by the ui package (so tests and
	// the PATH lookup stay out of config loading).
	TerminalCmd string `toml:"terminal_cmd"`
	EditorCmd   string `toml:"editor_cmd"`
	LazygitCmd  string `toml:"lazygit_cmd"`
}

func Default() Config {
	return Config{AgentCmd: "claude", Socket: "drover", Icons: "unicode", Background: true, Mouse: true, ZoomKey: DefaultZoomKey}
}

func Path() string {
	// XDG layout rather than os.UserConfigDir, which is ~/Library/Application
	// Support on macOS and not where a terminal setup keeps its dotfiles.
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.toml"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "drover", "config.toml")
}

// Load reads the config file; a missing file yields the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	for i, p := range cfg.Projects {
		cfg.Projects[i] = ExpandHome(p)
	}
	if strings.ToLower(strings.TrimSpace(cfg.Icons)) == "nerd" {
		cfg.Icons = "nerd"
	} else {
		cfg.Icons = "unicode"
	}
	cfg.ZoomKey = strings.ToLower(strings.TrimSpace(cfg.ZoomKey))
	switch cfg.ZoomKey {
	case "", `ctrl+\`, "ctrl+q": // unset, or a key that already leaves focus
		cfg.ZoomKey = DefaultZoomKey
	}
	return cfg, nil
}

// DefaultZoomKey is the zoom toggle when the config sets none (or a bad one).
const DefaultZoomKey = "ctrl+z"

func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// template is what a new config file starts with, so the editor opens on
// something to change rather than on nothing.
const template = `# drover configuration. See the README for every key.
# projects = ["~/dev/work/project"]
# theme = "jetbrains"   # or "catppuccin", "catppuccin-macchiato", "catppuccin-frappe"
# icons = "unicode"     # or "nerd"
# background = true     # paint the list, title bar and footer in the theme colour
# agent_cmd = "claude"
`

// EnsureFile creates the config file, and its directory, when it is missing.
// An existing file is never touched.
func EnsureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(template), 0o644)
}
