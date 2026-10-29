package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

// These tests cover where companion panes meet the pause-while-hidden,
// scrollback and branch-rename features. Each of those was built against a
// model that showed only the agent, so each needed to learn what the right
// pane is showing.

// showCompanion starts a terminal companion for the first row, running
// command, sized like the right pane, and makes it the shown pane.
func showCompanion(t *testing.T, m *Model, command string) string {
	t.Helper()
	r := m.rows[0]
	name := tmux.CompanionName(viewTerminal.kind(), r.session)
	if err := m.tmux.SpawnCompanion(name, t.TempDir(), command, "test"); err != nil {
		t.Fatal(err)
	}
	cols, rows := m.paneSize()
	if err := m.tmux.Resize(name, cols, rows); err != nil {
		t.Fatal(err)
	}
	m.companions[name] = true
	m.sub = viewTerminal // the cursor is on the first worktree's terminal row
	return name
}

func TestHidingStopsTheControlWatcherAndShowingRestartsIt(t *testing.T) {
	m := scrollModel(t, 5)
	name := showCompanion(t, &m, "cat")
	m.mode = modeFocus
	m.push.notify = func(tea.Msg) {} // without a program follow does nothing, as in production
	t.Cleanup(func() { m.push.follow(m.tmux, "") })

	_ = m.startPreview()
	if m.push.session != name {
		t.Fatalf("a focused companion must be watched: following %q, want %q", m.push.session, name)
	}

	m, _ = m.setVisible(false)
	if m.push.session != "" || m.push.watcher != nil {
		t.Fatalf("hidden drover is still watching %q: the control client would keep capturing", m.push.session)
	}

	m, _ = m.setVisible(true)
	if m.push.session != name {
		t.Errorf("becoming visible must resume watching the companion: following %q", m.push.session)
	}
}

func TestHidingWhileTheAgentIsShownLeavesNothingToStop(t *testing.T) {
	m := scrollModel(t, 5)
	m.push.notify = func(tea.Msg) {}
	m, _ = m.setVisible(false) // must not panic or start anything
	if m.push.session != "" {
		t.Errorf("following %q although the agent is shown", m.push.session)
	}
}

func lastNumber(lines []string) int {
	best := 0
	for _, l := range lines {
		if n, err := strconv.Atoi(strings.TrimSpace(l)); err == nil && n > best {
			best = n
		}
	}
	return best
}

func TestScrollbackReadsTheShownCompanionNotTheAgent(t *testing.T) {
	m := scrollModel(t, 5) // the agent printed 1..5 only
	name := showCompanion(t, &m, "seq 1 400; cat")
	for i := 0; ; i++ { // wait for the companion's output
		c, err := m.tmux.Capture(name)
		if err == nil && strings.Contains(strings.Join(c.Lines, "\n"), "400") {
			m.preview = preview{session: name, ok: true, cap: c}
			break
		}
		if i > 100 {
			t.Fatal("companion never printed")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if key, ok := m.scrollKey(); !ok || key != name {
		t.Fatalf("scrollback applies to %q (ok=%v), want the shown companion %q", key, ok, name)
	}
	m = wheel(t, m, true)
	if !m.scrolled() {
		t.Fatal("the wheel over a companion did not scroll back")
	}
	if top := lastNumber(paneText(m)); top <= 5 {
		t.Errorf("scrolled view ends at %d: that is the agent's output (1..5), not the companion's (1..400)", top)
	}

	// Switching the shown pane drops the scroll position: coming back to the
	// companion must show its live screen, not an old offset.
	m.setView(viewAgent)
	m.setView(viewTerminal)
	if m.scrolled() {
		t.Error("returning to the companion landed on a stale scrollback position")
	}
}

func TestScrollbackIgnoresACompanionThatIsNotRunning(t *testing.T) {
	m := scrollModel(t, 5) // the agent is running
	m.sub = viewEditor     // shown, but there is no such session
	if key, ok := m.scrollKey(); ok {
		t.Errorf("scrollback would read %q, a pane that does not exist", key)
	}
}

func TestRenameIsRefusedWhileACompanionRuns(t *testing.T) {
	m := scrollModel(t, 5)
	r := m.rows[0]
	_ = m.tmux.Kill(r.session) // only the companion lives
	m.rows[0].running, m.all = false, m.rows
	name := showCompanion(t, &m, "cat")
	m.sub = viewAgent // refused although not currently shown

	next, _ := m.openMove()
	got := next.(Model)
	if got.mode == modeMoveBranch || !strings.Contains(got.status, "stop the terminal, editor or lazygit") {
		t.Errorf("the prompt opened over a running companion: mode %d, status %q", got.mode, got.status)
	}

	// The list can be two seconds stale; the check at confirm time uses tmux itself.
	m.companions = map[string]bool{}
	out := renameBranch(renameReq{driver: m.tmux, oldSession: r.session, root: "/k/infra", oldPath: r.wt.Path, newBranch: "feature/renamed"})
	if out.err == nil || !strings.Contains(out.err.Error(), "terminal is running") {
		t.Errorf("confirm-time check missed the companion %s: %v", name, out.err)
	}
}

// The cursor, and with it the shown pane, stays on the renamed worktree. (The
// rename refuses while a companion runs, so the child row is only there when
// a stale list still has it; the selection must not jump to the agent row
// because of the rename itself.)
func TestRenameKeepsTheCursorOnTheWorktree(t *testing.T) {
	m := testModel()
	m.moveTo(visibleItems(m.items())[2])
	next, _ := m.handleMoved(branchMoved{
		oldSession: "infra/main", newSession: "infra/chore/new",
		res: worktree.RenameResult{Worktree: worktree.Worktree{Path: "/p/infra/chore/new", Branch: "chore/new", Group: "chore"}},
	})
	got := next.(Model)
	if got.selSession != "infra/chore/new" || got.hdr != "" || got.sub != viewAgent {
		t.Errorf("sel %q hdr %q sub %d", got.selSession, got.hdr, got.sub)
	}
}
