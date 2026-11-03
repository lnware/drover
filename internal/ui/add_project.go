package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/projects"
	"github.com/lnware/drover/internal/worktree"
)

// Adding a project: P (or :add) asks for a Git URL, makes a bare-layout clone
// (worktree.CloneJob) and registers the folder in projects.json. config.toml is
// never written. The clone runs one step per command so the popup can show
// where it is; esc stops the running git and removes what the job made.

type addState struct {
	url, folder string
	folderSet   bool // the folder was typed; until then it follows the URL
	field       int  // 0 the URL, 1 the folder
	job         *worktree.CloneJob
	step        int // the step running, or the one that failed
	busy        bool
	cancelling  bool
	failed      bool // a step failed (err says why), as opposed to a bad field
	err         string
}

// addStepDone reports one finished step of a clone job.
type addStepDone struct {
	job  *worktree.CloneJob
	step int
	err  error
}

func (m Model) openAdd() (tea.Model, tea.Cmd) {
	m.mode, m.add = modeAdd, addState{}
	return m, nil
}

// tilde shortens the home directory to ~ for display.
func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return p
}

// defaultFolder is where a URL's project goes by default: beside the projects
// of the same owner when there are some (~/dev/acme/payments), else under the
// folder that holds the selected project's org.
func (m Model) defaultFolder(url string) string {
	name := worktree.RepoName(url)
	if name == "" {
		return ""
	}
	owner := worktree.RepoOwner(url)
	if owner != "" {
		for _, p := range m.cfg.Projects {
			if parent := filepath.Dir(filepath.Clean(p)); strings.EqualFold(filepath.Base(parent), owner) {
				return tilde(filepath.Join(parent, name))
			}
		}
	}
	var base string
	switch root := m.selectedRoot(); {
	case root != "":
		base = filepath.Dir(filepath.Dir(filepath.Clean(root)))
	case len(m.cfg.Projects) > 0:
		base = filepath.Dir(filepath.Dir(filepath.Clean(m.cfg.Projects[0])))
	default:
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "dev")
	}
	if owner == "" {
		return tilde(filepath.Join(base, name))
	}
	return tilde(filepath.Join(base, owner, name))
}

// addFolder is the folder as shown: what was typed, else the one the URL implies.
func (m Model) addFolder() string {
	if m.add.folderSet {
		return m.add.folder
	}
	return m.defaultFolder(m.add.url)
}

// contextOfRoot names the context a project root would show up in, or "" when
// contexts do not apply (a single org and no configured contexts).
func (m Model) contextOfRoot(root string) string {
	root = filepath.Clean(root)
	if len(m.cfg.Contexts) > 0 {
		defs := m.contexts()
		if i := contextOf(defs, row{root: root, org: orgOf(root)}); i >= 0 {
			return defs[i].name
		}
		return "Other"
	}
	return orgOf(root)
}

func (m Model) handleAddKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.add.busy {
		if (key == "esc" || key == "ctrl+c") && !m.add.cancelling {
			m.add.cancelling = true
			m.add.job.Cancel()
		}
		return m, nil
	}
	m.status = ""
	switch key {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "tab", "down", "shift+tab", "up":
		m.add.field = (m.add.field + 1) % 2
	case "enter":
		return m.startClone()
	default:
		m.add.err, m.add.failed = "", false
		if m.add.field == 0 {
			m.add.url = editText(m.add.url, msg)
			break
		}
		if !m.add.folderSet {
			m.add.folder = m.defaultFolder(m.add.url)
		}
		m.add.folder = editText(m.add.folder, msg)
		m.add.folderSet = m.add.folder != ""
	}
	return m, nil
}

func (m Model) startClone() (tea.Model, tea.Cmd) {
	url, folder := strings.TrimSpace(m.add.url), strings.TrimSpace(m.addFolder())
	switch {
	case url == "":
		m.add.err, m.add.field = "type the Git URL (ssh or https)", 0
		return m, nil
	case folder == "":
		m.add.err, m.add.field = "type the folder", 1
		return m, nil
	}
	root, err := filepath.Abs(config.ExpandHome(folder))
	if err != nil {
		m.add.err = err.Error()
		return m, nil
	}
	if slices.Contains(m.cfg.Projects, root) {
		m.add.err = tilde(root) + " is already a project"
		return m, nil
	}
	job := worktree.NewCloneJob(url, root)
	m.add.job, m.add.step, m.add.busy, m.add.cancelling, m.add.failed, m.add.err = job, 0, true, false, false, ""
	return m, runCloneStep(job, 0)
}

func runCloneStep(job *worktree.CloneJob, step int) tea.Cmd {
	return func() tea.Msg { return addStepDone{job: job, step: step, err: job.Run(step)} }
}

func (m Model) handleAddStep(msg addStepDone) (tea.Model, tea.Cmd) {
	if m.mode != modeAdd || msg.job != m.add.job {
		return m, nil
	}
	if m.add.cancelling || msg.err != nil {
		msg.job.Cleanup()
		m.add.busy, m.add.job = false, nil
		if m.add.cancelling {
			m.add.cancelling, m.add.step, m.add.err = false, 0, "cancelled, nothing was kept"
		} else {
			m.add.failed, m.add.err = true, msg.err.Error()
		}
		return m, nil
	}
	if next := msg.step + 1; next < len(worktree.CloneStepNames) {
		m.add.step = next
		return m, runCloneStep(msg.job, next)
	}
	root, wt := msg.job.Root, msg.job.Worktree()
	if err := projects.Add(root); err != nil {
		m.add.busy, m.add.job = false, nil
		m.add.failed, m.add.err = true, "cloned, but could not register the project: "+err.Error()
		return m, nil
	}
	if !slices.Contains(m.cfg.Projects, root) {
		m.cfg.Projects = append(slices.Clone(m.cfg.Projects), root)
	}
	m.mode, m.add = modeList, addState{}
	m.notice = "added " + filepath.Base(root)
	if ctx := m.contextOfRoot(root); ctx != "" {
		if defs := m.contexts(); defs == nil || defs[m.activeContext(defs)].name != ctx {
			m.notice += " in " + ctx
		}
	}
	m.selSession = ""
	m.hdr = ""
	m.revealNew(root, wt)
	return m, m.refresh()
}

// addBox is the popup: the two fields, then the steps.
func (m Model) addBox() []string {
	a := m.add
	cursor := func(on bool) string {
		if on && !a.busy {
			return focusHint.Render("█")
		}
		return ""
	}
	label := func(s string, on bool) string {
		if on {
			return projectStyle.Render(fmt.Sprintf(" %-8s", s))
		}
		return dimStyle.Render(fmt.Sprintf(" %-8s", s))
	}
	folder := m.addFolder()
	folderShown := folder
	if !a.folderSet && folder != "" {
		folderShown = dimStyle.Render(folder)
	}
	rows := []string{
		"",
		label("Git URL", a.field == 0) + a.url + cursor(a.field == 0),
		label("Folder", a.field == 1) + folderShown + cursor(a.field == 1),
	}
	if ctx := m.contextOfRoot(config.ExpandHome(folder)); ctx != "" && folder != "" {
		rows = append(rows, dimStyle.Render(fmt.Sprintf(" %-8s", "Context"))+ctx+dimStyle.Render(" · by folder, nothing to configure"))
	}
	rows = append(rows, "", " "+dimStyle.Render("What happens"))
	for i, name := range worktree.CloneStepNames {
		glyph, text := dimStyle.Render("○"), dimStyle.Render(name)
		started := a.busy || a.failed
		switch {
		case started && i < a.step:
			glyph, text = doneStyle.Render("✓"), name
		case started && i == a.step && a.busy:
			glyph, text = waitingStyle.Render("◐"), name
		case started && i == a.step:
			glyph, text = errStyle.Render("✗"), name
		}
		rows = append(rows, "  "+glyph+" "+text)
	}
	if a.err != "" {
		rows = append(rows, "", " "+errStyle.Render("✗ "+a.err))
		if a.failed {
			rows = append(rows, " "+dimStyle.Render("  nothing is left behind; fix the field and press enter"))
		}
	}
	keys := " " + focusHint.Render("enter") + dimStyle.Render(" clone · ") + focusHint.Render("tab") + dimStyle.Render(" next field · ") + focusHint.Render("esc") + dimStyle.Render(" cancel")
	if a.busy {
		keys = " " + focusHint.Render("esc") + dimStyle.Render(" stops and cleans up")
	}
	return popupBox(" Add project ", rows, keys, 64, m.width-2)
}
