// Command drover manages coding-agent sessions per git worktree.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/hooks"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/ui"
)

const usage = `usage: drover [project-root...]
       drover hooks       show the status hooks drover injects into Claude Code
       drover stop        stop the private tmux server and every session on it

Project roots default to "projects" in ~/.config/drover/config.toml.
"drover hook <state>" is run by those hooks and is not meant for people.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "drover:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Hooks run on every tool call of every Claude session, so this path
	// must stay cheap and must never fail: it skips config loading and
	// always exits 0.
	if len(args) > 0 && args[0] == "hook" {
		runHook(args[1:])
		return nil
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return fmt.Errorf("%s: %w", config.Path(), err)
	}
	if len(args) == 1 {
		switch args[0] {
		case "-h", "--help", "help":
			fmt.Println(usage)
			return nil
		case "stop":
			return tmux.New(cfg.Socket).KillServer()
		case "hooks":
			return printHooks()
		}
	}
	if len(args) > 0 {
		cfg.Projects = cfg.Projects[:0]
		for _, a := range args {
			abs, err := filepath.Abs(config.ExpandHome(a))
			if err != nil {
				return err
			}
			cfg.Projects = append(cfg.Projects, abs)
		}
	}
	if _, ok := ui.ResolveTheme(cfg.Theme); !ok {
		fmt.Fprintf(os.Stderr, "drover: unknown theme %q, using %s (themes: %s)\n",
			cfg.Theme, ui.DefaultTheme, strings.Join(ui.ThemeNames(), ", "))
	}
	ui.SetTheme(cfg.Theme)
	ui.SetColorProfile(colorprofile.Detect(os.Stdout, os.Environ()))
	model := ui.New(cfg).WithConfigFile(config.Path(), len(args) > 0)
	p := tea.NewProgram(model, tea.WithFPS(120))
	model.Notify(p.Send)
	_, err = p.Run()
	return err
}

// runHook records the agent state for the drover session this process runs
// under. Outside a drover session there is nothing to do.
func runHook(args []string) {
	session := os.Getenv("DROVER_SESSION")
	if session == "" || len(args) != 1 {
		return
	}
	// Claude pipes event JSON on stdin; drain it so the writer never blocks,
	// but not from a terminal, where a manual run would hang.
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice == 0 {
		_, _ = io.Copy(io.Discard, io.LimitReader(os.Stdin, 1<<20))
	}
	dir := status.Dir()
	var err error
	if args[0] == "ended" {
		err = status.Remove(dir, session)
	} else if status.Valid(args[0]) {
		err = status.Write(dir, session, status.State(args[0]), time.Now())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "drover hook:", err)
	}
}

func printHooks() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	out, err := hooks.Settings(bin)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "# For inspection only: drover passes these to `claude --settings` for the")
	fmt.Fprintln(os.Stderr, "# sessions it starts, so there is nothing to merge into ~/.claude/settings.json.")
	fmt.Fprintln(os.Stderr, "# File:", filepath.Join(status.Root(), "claude-settings.json"))
	fmt.Println(string(out))
	return nil
}
