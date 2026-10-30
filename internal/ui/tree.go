package ui

import (
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/fold"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/worktree"
)

// The list is a three-level tree (org > project > group) over the worktree
// rows. Headers are selectable so that a folded section stays reachable, so
// the cursor is no longer just an index into m.rows:
//
//   - on a worktree, m.hdr is empty and m.cursor indexes m.rows, exactly as
//     before (selected() and every worktree action are untouched);
//   - on a header, m.hdr holds that header's id and selected() reports false;
//   - on a companion child row, m.hdr is empty, m.cursor is the parent's row and
//     m.sub says which child (companion_tree.go), so the pane shown is derived
//     from the cursor alone.
//
// The display items are derived on demand from m.rows and the fold set by a
// pure function (buildItems), never stored, so they cannot go stale.

type itemKind int

const (
	itemOrg itemKind = iota
	itemProject
	itemGroup
	itemWorktree
	// itemAgent is one of the worktree's agent sessions, running or not,
	// shown as a child row under it (companion_tree.go).
	itemAgent
	// itemCompanion is a running terminal or editor, shown as a child row
	// under its worktree (companion_tree.go).
	itemCompanion
)

// tally counts the worktrees under a header by agent state.
type tally struct{ total, working, waiting, done, failed int }

func (t *tally) add(s status.State, running bool) {
	t.total++
	if !running {
		return
	}
	switch s {
	case status.Working:
		t.working++
	case status.Waiting:
		t.waiting++
	case status.Done:
		t.done++
	case status.Error:
		t.failed++
	}
}

// item is one selectable entry of the list.
type item struct {
	kind itemKind
	// id is the fold id for a header (see orgID, projectID, groupID) and the
	// session name for a worktree. Fold ids are what folds.json stores.
	id    string
	up    []string // ids of the headers above, outermost first
	label string   // header text; empty for worktrees
	root  string   // project root, for project and group headers
	row   int      // index into m.rows for a worktree or its child, else -1
	// view and last describe a child: which companion program (agent children
	// have none), and whether it is the last child of its worktree (drawn with
	// └ instead of ├). agent is the session of an agent child.
	view  paneView
	last  bool
	agent string
	// collapsed is the header's own fold; hidden says a header above it is
	// collapsed, so it is not on screen.
	collapsed, hidden bool
	sum               tally // everything under a header
}

func (it item) isHeader() bool { return it.kind < itemWorktree }

// isChild reports whether the item is a child row of a worktree.
func (it item) isChild() bool { return it.kind == itemAgent || it.kind == itemCompanion }

// parent is the id of the header directly above the item, or "".
func (it item) parent() string {
	if len(it.up) == 0 {
		return ""
	}
	return it.up[len(it.up)-1]
}

// Fold ids are stable across restarts: the org name, the project's display
// name within its org (unique by construction, see layoutProjects), and the
// group folder within the project.
func orgID(r row) string     { return "o:" + r.org }
func projectID(r row) string { return "p:" + r.org + "/" + r.project }
func groupID(r row) string   { return "g:" + r.org + "/" + r.project + "/" + r.wt.Group }

// buildItems lays rows out as headers and worktrees, in display order, with
// every header's tally and which items a fold hides. Rows must already be
// grouped by org, project and group, as refresh produces them. With
// ignoreFolds (an active filter) nothing is collapsed or hidden: a fold must
// never hide a match.
func buildItems(rows []row, folds map[string]bool, ignoreFolds bool, state func(row) (status.State, bool)) []item {
	sums := map[string]*tally{}
	for _, r := range rows {
		s, running := state(r)
		for _, id := range []string{orgID(r), projectID(r), groupID(r)} {
			if sums[id] == nil {
				sums[id] = &tally{}
			}
			sums[id].add(s, running)
		}
	}
	collapsed := func(id string) bool { return !ignoreFolds && folds[id] }

	var items []item
	lastOrg, lastProject, lastGroup := "", "", ""
	for i, r := range rows {
		var up []string
		hidden := false
		header := func(kind itemKind, id, label string) {
			items = append(items, item{
				kind: kind, id: id, up: slices.Clone(up), label: label, root: r.root, row: -1,
				collapsed: collapsed(id), hidden: hidden, sum: *sums[id],
			})
			up = append(up, id)
			hidden = hidden || collapsed(id)
		}
		// No org header when orgs are not shown (r.org == ""); the project is
		// then the top level.
		if r.org != "" {
			if id := orgID(r); id != lastOrg {
				header(itemOrg, id, r.org)
				lastOrg = id
			} else {
				up, hidden = append(up, id), hidden || collapsed(id)
			}
		}
		if id := projectID(r); id != lastProject {
			header(itemProject, id, r.project)
			lastProject = id
		} else {
			up, hidden = append(up, id), hidden || collapsed(id)
		}
		if id := groupID(r); id != lastGroup {
			header(itemGroup, id, r.wt.Group)
			lastGroup = id
		} else {
			up, hidden = append(up, id), hidden || collapsed(id)
		}
		items = append(items, item{kind: itemWorktree, id: r.session, up: up, root: r.root, row: i, hidden: hidden})
	}
	return items
}

// visibleItems drops the items a fold hides.
func visibleItems(items []item) []item {
	out := make([]item, 0, len(items))
	for _, it := range items {
		if !it.hidden {
			out = append(out, it)
		}
	}
	return out
}

// stateOf is the agent state the tallies count; "" when nothing is known.
func (m Model) stateOf(r row) (status.State, bool) {
	s, _ := m.worktreeState(r)
	return s, r.running
}

// items is the full display list. A filter shows every match, folded or not.
func (m Model) items() []item {
	return withChildren(buildItems(m.rows, m.folds, m.filtering(), m.stateOf), m.rows, m.companionRunning)
}

// current finds the item the cursor is on.
func (m Model) current(items []item) (item, bool) {
	id := m.hdr
	if id == "" {
		if m.cursor < 0 || m.cursor >= len(m.rows) {
			return item{}, false
		}
		id = m.rows[m.cursor].session
		switch {
		case m.sub != viewAgent:
			id = companionID(id, m.sub)
		case m.subAgent != "":
			id = agentItemID(m.subAgent)
		}
	}
	i := slices.IndexFunc(items, func(it item) bool { return it.id == id })
	if i < 0 {
		return item{}, false
	}
	return items[i], true
}

// selectedHeader is the header under the cursor, when it is on one.
func (m Model) selectedHeader() (item, bool) {
	if m.hdr == "" {
		return item{}, false
	}
	return m.current(m.items())
}

// selectedRoot is the project root the cursor is in, for n: the worktree's
// own, or the header's project. An org header has none.
func (m Model) selectedRoot() string {
	if r, ok := m.selected(); ok {
		return r.root
	}
	if it, ok := m.selectedHeader(); ok && it.kind != itemOrg {
		return it.root
	}
	return ""
}

// moveTo puts the cursor on an item.
func (m *Model) moveTo(it item) {
	m.sub, m.subAgent = viewAgent, ""
	switch it.kind {
	case itemWorktree:
		m.hdr, m.cursor, m.selSession = "", it.row, it.id
	case itemAgent:
		m.hdr, m.cursor, m.selSession, m.subAgent = "", it.row, m.rows[it.row].session, it.agent
	case itemCompanion:
		m.hdr, m.cursor, m.selSession, m.sub = "", it.row, m.rows[it.row].session, it.view
	default:
		m.hdr = it.id
	}
}

// step moves the cursor by delta visible items, across headers.
func (m *Model) step(delta int) {
	vis := visibleItems(m.items())
	if len(vis) == 0 {
		return
	}
	i := 0
	if cur, ok := m.current(vis); ok {
		i = slices.IndexFunc(vis, func(it item) bool { return it.id == cur.id })
	}
	m.moveTo(vis[min(max(i+delta, 0), len(vis)-1)])
}

// reselect keeps the cursor on the same header or session after rows or
// folds changed. A selection a fold has hidden moves to the header that hides
// it, so a collapsed section stays reachable.
func (m *Model) reselect() {
	items := m.items()
	byID := make(map[string]item, len(items))
	for _, it := range items {
		byID[it.id] = it
	}
	id := m.hdr
	if _, ok := byID[id]; !ok {
		id = ""
	}
	keepSub := false // the cursor stays on the worktree it was on, so its child may too
	if id == "" {
		if i := slices.IndexFunc(m.rows, func(r row) bool { return r.session == m.selSession }); i >= 0 {
			id, keepSub = m.selSession, true
		} else {
			m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
			if m.cursor < len(m.rows) {
				id = m.rows[m.cursor].session
				m.selSession = id
			}
		}
	}
	if id == "" {
		m.hdr = ""
		return
	}
	it := byID[id]
	if keepSub {
		// A child that is gone from the items (a companion that ended, an
		// agent that was forgotten) falls back to its worktree, which is what
		// `it` already is.
		switch {
		case m.sub != viewAgent:
			if c, ok := byID[companionID(id, m.sub)]; ok {
				it = c
			}
		case m.subAgent != "":
			if c, ok := byID[agentItemID(m.subAgent)]; ok {
				it = c
			}
		}
	}
	if it.hidden {
		// The outermost collapsed header above it is the one on screen.
		for _, a := range it.up {
			if byID[a].collapsed {
				it = byID[a]
				break
			}
		}
	}
	m.moveTo(it)
}

// --- fold actions ---

// foldsPaused explains why a fold key did nothing while a filter is active.
func (m Model) foldsPaused() (tea.Model, tea.Cmd) {
	m.notice = "folds apply when the filter is cleared (esc)"
	return m, nil
}

// setFolds stores and persists a new fold set. The set is copied so older
// copies of the model never see it change.
func (m *Model) setFolds(next map[string]bool) {
	m.folds = next
	if err := fold.Save(fold.Path(status.Root()), next); err != nil {
		m.status = "folds not saved: " + err.Error()
	}
	m.reselect()
}

func (m Model) withFold(id string, on bool) map[string]bool {
	next := maps.Clone(m.folds)
	if next == nil {
		next = map[string]bool{}
	}
	if on {
		next[id] = true
	} else {
		delete(next, id)
	}
	return next
}

// foldMoved is what every fold key returns: the cursor may have changed
// rows, so the preview restarts (it idles when no worktree is selected).
func (m Model) foldMoved() (tea.Model, tea.Cmd) {
	return m, tea.Batch(m.startPreview(), m.readSelectedGit())
}

// collapse folds the section the cursor is in: a worktree's group, a group's
// project, a project's org. A top-level header folds itself.
func (m Model) collapse() (tea.Model, tea.Cmd) {
	if cur, ok := m.current(m.items()); ok && cur.isChild() {
		return m.toParent(cur) // tree semantics: left on a child goes up, it never folds
	}
	if m.filtering() {
		return m.foldsPaused()
	}
	cur, ok := m.current(m.items())
	if !ok {
		return m, nil
	}
	target := cur.parent()
	if target == "" {
		target = cur.id
	}
	m.setFolds(m.withFold(target, true))
	return m.foldMoved()
}

// expand opens a collapsed header; on an open one it steps to the first
// child, so right retraces what left folded.
func (m Model) expand() (tea.Model, tea.Cmd) {
	if m.filtering() {
		return m.foldsPaused()
	}
	cur, ok := m.current(m.items())
	if !ok || !cur.isHeader() {
		return m, nil
	}
	if cur.collapsed {
		m.setFolds(m.withFold(cur.id, false))
	} else {
		m.step(1)
	}
	return m.foldMoved()
}

// toggleHeader is enter on a header.
func (m Model) toggleHeader() (tea.Model, tea.Cmd) {
	if m.filtering() {
		return m.foldsPaused()
	}
	cur, ok := m.current(m.items())
	if !ok || !cur.isHeader() {
		return m, nil
	}
	m.setFolds(m.withFold(cur.id, !cur.collapsed))
	return m.foldMoved()
}

// foldAll collapses every header, or opens them all when everything is
// already folded.
func (m Model) foldAll() (tea.Model, tea.Cmd) {
	if m.filtering() {
		return m.foldsPaused()
	}
	items := m.items()
	anyOpen := false
	for _, it := range items {
		anyOpen = anyOpen || it.isHeader() && !it.collapsed
	}
	next := maps.Clone(m.folds)
	if next == nil {
		next = map[string]bool{}
	}
	for _, it := range items {
		if !it.isHeader() {
			continue
		}
		if anyOpen {
			next[it.id] = true
		} else {
			delete(next, it.id)
		}
	}
	m.setFolds(next)
	return m.foldMoved()
}

// revealNew opens the sections a just-created worktree will appear in, so
// creating one in a folded project is not invisible.
func (m *Model) revealNew(root string, wt worktree.Worktree) {
	i := slices.IndexFunc(m.all, func(r row) bool { return r.root == root })
	if i < 0 {
		return
	}
	r := m.all[i]
	r.wt.Group = wt.Group
	next := maps.Clone(m.folds)
	for _, id := range []string{orgID(r), projectID(r), groupID(r)} {
		delete(next, id)
	}
	if len(next) != len(m.folds) {
		m.setFolds(next)
	}
}

// foldsLoaded carries the persisted fold set, read off the first render.
type foldsLoaded struct{ set map[string]bool }

func loadFolds() tea.Cmd {
	return func() tea.Msg { return foldsLoaded{set: fold.Load(fold.Path(status.Root()))} }
}
