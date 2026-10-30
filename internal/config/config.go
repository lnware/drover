// Package config loads ~/.config/drover/config.toml.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Context is a named set of worktrees shown together. Projects lists paths: a
// path takes a project when it is the project's root or a folder above it, so
// "~/dev/acme" takes everything under it. A project that no context takes falls
// into an implicit "Other" context.
type Context struct {
	Name     string   `toml:"name"`
	Projects []string `toml:"projects"`
}

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
	// SwitchKey moves to the next view of the worktree (its agents, then the
	// terminal and the editor), in the list and in focus mode alike. F1 to F9
	// and ctrl+1 to ctrl+9 jump straight to the nth view and are not
	// configurable. A function key is the default because it reaches drover
	// through kitty and tmux as they come; ctrl+tab and ctrl+space need
	// settings that not every setup has (and ctrl+space is often tmux's own).
	SwitchKey string `toml:"switch_key"`
	// ContextKey is an optional key that cycles through the contexts (shift+
	// the key goes back), in the list and in focus mode alike. Empty by
	// default: ":ctx" in the list switches context. A function key is the
	// safe choice, for the reasons SwitchKey gives.
	ContextKey string `toml:"context_key"`
	// NextKey is an optional key that jumps to the next agent that needs you
	// (shift+ the key to the previous), in the list and in focus mode. Empty
	// by default: tab and shift+tab do it in the list. Like ContextKey, a
	// function key is the safe choice.
	NextKey string `toml:"next_key"`
	// Contexts group the list into the things you work on one at a time: only
	// the active one is shown. Without any, every org is a context of its own.
	Contexts []Context `toml:"context"`
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
	return Config{AgentCmd: "claude", Socket: "drover", Icons: "unicode", Background: true, Mouse: true, ZoomKey: DefaultZoomKey, SwitchKey: DefaultSwitchKey, ContextKey: DefaultContextKey}
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
	cfg.SwitchKey = strings.ToLower(strings.TrimSpace(cfg.SwitchKey))
	switch cfg.SwitchKey {
	case "", `ctrl+\`, "ctrl+q", cfg.ZoomKey: // unset, or a key that is already taken
		cfg.SwitchKey = DefaultSwitchKey
	}
	// No context key by default: ":ctx" is the way, and a key is opt-in.
	cfg.ContextKey = strings.ToLower(strings.TrimSpace(cfg.ContextKey))
	if k := cfg.ContextKey; k == cfg.ZoomKey || k == cfg.SwitchKey || k == `ctrl+\` || k == "ctrl+q" || isJumpKey(k) {
		cfg.ContextKey = "" // taken by something else: no key rather than a clash
	}
	cfg.NextKey = strings.ToLower(strings.TrimSpace(cfg.NextKey))
	if k := cfg.NextKey; k == cfg.ZoomKey || k == cfg.SwitchKey || k == cfg.ContextKey || k == `ctrl+\` || k == "ctrl+q" || isJumpKey(k) {
		cfg.NextKey = "" // taken by something else: no key rather than a clash
	}
	var contexts []Context
	for _, c := range cfg.Contexts {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" { // a context without a name cannot be shown or switched to
			continue
		}
		paths := make([]string, 0, len(c.Projects))
		for _, p := range c.Projects {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, filepath.Clean(ExpandHome(p)))
			}
		}
		c.Projects = paths
		contexts = append(contexts, c)
	}
	cfg.Contexts = contexts
	return cfg, nil
}

// DefaultContextKey is the context-switch key when the config sets none: none.
const DefaultContextKey = ""

// DefaultSwitchKey is the next-view key when the config sets none.
const DefaultSwitchKey = "f12"

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

// UnmatchedContextPaths lists the context paths that take no configured
// project, as "Context: path", so a typo shows up instead of leaving a
// context empty.
func (c Config) UnmatchedContextPaths() []string {
	var out []string
	for _, ctx := range c.Contexts {
		for _, p := range ctx.Projects {
			if !slices.ContainsFunc(c.Projects, func(root string) bool { return PathTakes(p, filepath.Clean(root)) }) {
				out = append(out, ctx.Name+": "+p)
			}
		}
	}
	return out
}

// PathTakes reports whether a context path takes a project root: it is the
// root itself or a folder above it.
func PathTakes(path, root string) bool {
	return root == path || strings.HasPrefix(root, strings.TrimSuffix(path, string(filepath.Separator))+string(filepath.Separator))
}

// isJumpKey reports the keys that go straight to a view of the worktree: F1 to
// F9 and ctrl+1 to ctrl+9. They are not available for anything else.
func isJumpKey(k string) bool {
	for _, prefix := range []string{"f", "ctrl+"} {
		if rest, ok := strings.CutPrefix(k, prefix); ok && len(rest) == 1 && rest[0] >= '1' && rest[0] <= '9' {
			return true
		}
	}
	return false
}
