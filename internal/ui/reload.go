package ui

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/config"
)

// WithConfigFile tells the model where its config came from, so c can edit it
// and R re-read it. projectsFixed says the project roots were given on the
// command line, which a reload must not replace with the file's list.
func (m Model) WithConfigFile(path string, projectsFixed bool) Model {
	m.cfgPath, m.projectsFixed = path, projectsFixed
	return m
}

// configEdited arrives when the editor opened by c exits.
type configEdited struct{ err error }

// configEditor is the user's editor for a file drover does not own: $VISUAL,
// then $EDITOR, then Helix, then vi.
func configEditor() string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := strings.TrimSpace(os.Getenv(v)); e != "" {
			return e
		}
	}
	if onPath("hx") {
		return "hx"
	}
	return "vi"
}

// configEditCmd runs the editor on path. The editor goes through sh because
// $EDITOR may carry arguments ("code -w"); the path travels as $1, so it is
// never parsed as shell.
func configEditCmd(editor, path string) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$1"`, "drover", path)
}

// openConfig suspends drover while the config is edited, as A does for a
// tmux attach; the file is re-read once the editor exits.
func (m Model) openConfig() (tea.Model, tea.Cmd) {
	path := m.cfgPath
	if path == "" {
		path = config.Path()
	}
	if err := config.EnsureFile(path); err != nil {
		m.status = err.Error()
		return m, nil
	}
	return m, tea.ExecProcess(configEditCmd(configEditor(), path), func(err error) tea.Msg {
		return configEdited{err: err}
	})
}

func (m Model) handleConfigEdited(msg configEdited) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "editor: " + msg.err.Error()
		return m, nil
	}
	return m.reloadConfig()
}

// reloadConfig re-reads the config file and applies what can change while
// running: the theme, the project list, icons, notifications, the zoom key
// and the commands that start things. A file that does not parse changes
// nothing. The tmux socket and mouse capture are fixed at start-up, so a
// change to them is reported rather than half-applied.
func (m Model) reloadConfig() (tea.Model, tea.Cmd) {
	path := m.cfgPath
	if path == "" {
		path = config.Path()
	}
	next, err := config.Load(path)
	if err != nil {
		m.status = "config not reloaded: " + err.Error()
		return m, nil
	}
	old := m.cfg
	if m.projectsFixed {
		next.Projects = old.Projects
	}
	var restart []string
	if next.Socket != old.Socket {
		restart = append(restart, "socket")
		next.Socket = old.Socket
	}
	if next.Mouse != old.Mouse {
		restart = append(restart, "mouse")
		next.Mouse = old.Mouse
	}
	if _, ok := ResolveTheme(next.Theme); !ok {
		m.status = fmt.Sprintf("unknown theme %q, keeping the current one (themes: %s)", next.Theme, strings.Join(ThemeNames(), ", "))
		next.Theme = old.Theme
	}
	m.cfg = next
	SetTheme(next.Theme)
	m.notice = "config reloaded"
	m.warnContextPaths()
	if len(restart) > 0 {
		m.notice += " · " + strings.Join(restart, " and ") + " change needs a restart"
	}
	if !slices.Equal(old.Projects, next.Projects) {
		m.notice += " · projects changed"
	}
	return m, tea.Batch(m.refresh(), m.startPreview())
}
