package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/alias"
	"github.com/lnware/drover/internal/claude"
	"github.com/lnware/drover/internal/gitinfo"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

// modeMoveBranch is the prompt for a real branch rename plus folder move (M).
// Its value is set apart from the iota list in model.go on purpose, so other
// branches adding modes there never collide with it.
const modeMoveBranch mode = 100

// openMove starts the branch-rename prompt for the selected worktree. What can
// be refused without git or tmux is refused here, before any typing.
func (m Model) openMove() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	if r.running {
		m.status = "stop the session first (x): its agent runs inside this folder, and moving it would break the agent"
		return m, nil
	}
	if m.anyCompanion(r) {
		m.status = "stop the terminal, editor or lazygit first (switch to it and press x): it runs inside this folder, and moving it would break it"
		return m, nil
	}
	if err := worktree.CheckRenamable(r.root, r.wt); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.mode, m.moveSession, m.input, m.busy = modeMoveBranch, r.session, r.wt.Branch, false
	return m, nil
}

// branchMoved reports a finished rename (or why it was refused).
type branchMoved struct {
	oldSession, newSession string
	res                    worktree.RenameResult
	notes                  []string // things left where they were, for the user to know
	err                    error
}

func (m Model) handleMoveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.status = ""
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "enter":
		r, ok := m.rowFor(m.moveSession)
		if !ok {
			m.mode = modeList
			return m, nil
		}
		next := strings.TrimSpace(m.input)
		if next == r.wt.Branch {
			m.status = "change the name first; it is still " + next
			return m, nil
		}
		req := renameReq{
			root: r.root, oldPath: r.wt.Path, oldSession: r.session,
			newBranch: next, project: m.projectName(r.root), driver: m.tmux,
		}
		for _, o := range m.all {
			if o.session != r.session {
				req.others = append(req.others, renameOther{session: o.session, path: o.wt.Path})
			}
		}
		m.busy = true
		return m, func() tea.Msg { return renameBranch(req) }
	default:
		m.input = editText(m.input, msg)
	}
	return m, nil
}

// handleMoved applies a finished rename to the model at once, so the new row
// is selected immediately instead of after the next refresh.
func (m Model) handleMoved(msg branchMoved) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.status = msg.err.Error() // the prompt stays open with the typed name
		return m, nil
	}
	m.mode = modeList
	m.notice = "renamed to " + msg.res.Worktree.Branch + " · folder moved to " + msg.res.Worktree.Path
	if len(msg.notes) > 0 {
		m.notice += " · " + strings.Join(msg.notes, " · ")
	}

	// Everything in memory keyed by the old session name follows it.
	old, neu := msg.oldSession, msg.newSession
	patch := func(rows []row) []row {
		out := make([]row, len(rows))
		copy(out, rows)
		for i := range out {
			if out[i].session == old {
				out[i].session = neu
				out[i].wt.Path, out[i].wt.Branch, out[i].wt.Group = msg.res.Worktree.Path, msg.res.Worktree.Branch, msg.res.Worktree.Group
			}
		}
		return out
	}
	m.all, m.rows = patch(m.all), patch(m.rows)
	if old != neu {
		m.git = rekey(m.git, old, neu, func(s gitinfo.Status) gitinfo.Status {
			s.Branch = msg.res.Worktree.Branch
			return s
		})
		m.agents = rekey(m.agents, old, neu, nil)
		m.prevStates = rekey(m.prevStates, old, neu, nil)
		m.gitAt = rekey(m.gitAt, old, neu, nil)
		if a, ok := m.aliases[old]; ok {
			next := make(map[string]string, len(m.aliases))
			for k, v := range m.aliases {
				next[k] = v
			}
			delete(next, old)
			next[neu] = a
			m.aliases = next
		}
	}
	m.selSession = neu
	m.applyFilter()
	m.sized, m.preview = size{}, preview{}
	// The refresh restores the sort order; the git read refreshes the bar.
	return m, tea.Batch(m.refresh(), m.readSelectedGit())
}

// rekey returns a copy of a session-keyed map with one key renamed. The
// original is left alone because Model values share their maps.
func rekey[V any](in map[string]V, old, neu string, adjust func(V) V) map[string]V {
	v, ok := in[old]
	if !ok {
		return in
	}
	out := make(map[string]V, len(in))
	for k, x := range in {
		if k != old {
			out[k] = x
		}
	}
	if adjust != nil {
		v = adjust(v)
	}
	out[neu] = v
	return out
}

// renameBranchLines is the prompt shown in the right pane. The resulting folder is
// worked out live, so the user sees where the worktree lands before pressing
// enter, or why it cannot go anywhere.
func (m Model) renameBranchLines() []string {
	r, ok := m.rowFor(m.moveSession)
	if !ok {
		return nil
	}
	next := strings.TrimSpace(m.input)
	folder := dimStyle.Render("type the new name")
	if next != "" && next != r.wt.Branch {
		if p, err := worktree.NewPathFor(r.root, r.wt, next); err != nil {
			folder = errStyle.Render(err.Error())
		} else {
			folder = dimStyle.Render(filepath.ToSlash(p))
		}
	}
	lines := []string{
		"",
		" " + titleStyle.Render("Rename branch and move folder"),
		"",
		" branch  " + m.input + focusHint.Render("█"),
		" from    " + dimStyle.Render(r.wt.Branch),
		" folder  " + folder,
		"",
		" " + dimStyle.Render("This runs git branch -m and git worktree move on the local clone."),
		" " + dimStyle.Render("The remote branch is NOT renamed and nothing is pushed: the upstream"),
		" " + dimStyle.Render("setting keeps naming the old remote branch until you run"),
		" " + dimStyle.Render("`git push -u origin <new name>` yourself."),
		" " + dimStyle.Render("Claude's history for this folder moves along, so v still resumes."),
	}
	if m.busy {
		lines = append(lines, "", " "+dimStyle.Render("renaming…"))
	}
	return lines
}

type renameOther struct{ session, path string }

// renameReq is everything the rename needs, captured so it can run off the UI
// goroutine and be tested without a Model.
type renameReq struct {
	root, oldPath, oldSession, newBranch, project string
	driver                                        tmux.Driver
	others                                        []renameOther // every other worktree
}

// renameBranch does the whole rename and carries the state keyed by the
// session name. Order: refuse what git cannot see (a live session, a name
// collision), do the git steps (which roll themselves back on failure), and
// only then touch the session-keyed files, since those must never move for a
// rename that did not happen.
func renameBranch(req renameReq) branchMoved {
	out := branchMoved{oldSession: req.oldSession}

	// The authoritative check: the list's running flag can be a refresh old.
	// The agent's working directory is the old folder, so moving it would pull
	// the floor from under a live process.
	if req.driver.Has(req.oldSession) {
		out.err = fmt.Errorf("the session is running: stop it first (x), then rename")
		return out
	}
	// Companions run in the same folder, and are named after the session, so
	// they would break or be orphaned just like the agent.
	for _, v := range companionViews {
		if req.driver.Has(tmux.CompanionName(v.kind(), req.oldSession)) {
			out.err = fmt.Errorf("the %s is running: stop it first (switch to it and press x), then rename", v.label())
			return out
		}
	}
	plan, err := worktree.PlanRename(req.root, req.oldPath, req.newBranch)
	if err != nil {
		out.err = err
		return out
	}
	newSession := tmux.SessionName(req.project, sessionKey(req.root, worktree.Worktree{Path: plan.NewPath}))
	if newSession != req.oldSession {
		// tmux flattens '.', ':' and ' ', so two folders can share a session
		// name; the new row would then attach to the other's agent.
		for _, o := range req.others {
			if o.session == newSession {
				out.err = fmt.Errorf("the session name %s is already used by the worktree at %s", newSession, o.path)
				return out
			}
		}
		if req.driver.Has(newSession) {
			out.err = fmt.Errorf("a tmux session named %s already exists, left over from something else; stop or rename it first", newSession)
			return out
		}
	}
	out.newSession = newSession

	oldSpelling := claude.Spell(req.oldPath) // the folder is gone after the move
	res, err := worktree.Rename(req.root, req.oldPath, req.newBranch)
	if err != nil {
		out.err = err
		return out
	}
	out.res = res

	// From here the rename happened; problems become notes, never errors.
	if newSession != req.oldSession {
		if _, err := alias.Rename(alias.Path(status.Root()), req.oldSession, newSession); err != nil {
			out.notes = append(out.notes, "could not carry the display label over: "+err.Error())
		}
		if err := status.Rename(status.Dir(), req.oldSession, newSession); err != nil {
			out.notes = append(out.notes, "could not move the status file: "+err.Error())
		}
	}
	var keep []string
	for _, o := range req.others {
		keep = append(keep, o.path)
	}
	hist := claude.MoveConversations(oldSpelling, claude.Spell(res.Worktree.Path), keep)
	out.notes = append(out.notes, hist.Notes...)
	if res.Upstream != "" {
		out.notes = append(out.notes, "remote branch not renamed: upstream still "+res.Upstream+", `git push -u origin "+res.Worktree.Branch+"` when ready")
	} else {
		out.notes = append(out.notes, "remote branch not renamed")
	}
	return out
}
