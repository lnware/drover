package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/worktree"
)

// mouseModel is a model whose tmux driver can never reach the user's own
// server: a unique socket with a harmless agent, and a throwaway state dir.
func mouseModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	cfg := config.Default()
	cfg.Socket = fmt.Sprintf("agt-mouse-%d-%s", os.Getpid(), strings.NewReplacer("/", "_").Replace(t.Name()))
	cfg.AgentCmd = "cat"
	m := New(cfg)
	t.Cleanup(func() { _ = m.tmux.KillServer() })
	m.cfg.Projects = []string{"/k/infra", "/o/tools"}
	m.width, m.height = 120, 30
	return m
}

// manyRows fills the list with several orgs, projects and groups, enough to
// scroll, so that bands, headers and blank lines all occur between rows.
func manyRows(m *Model, perProject int) {
	m.rows = nil
	for _, p := range []struct{ org, name string }{{"Acme", "infra"}, {"Acme", "platform"}, {"Globex", "tools"}} {
		for i := 0; i < perProject; i++ {
			group, branch := "feature", fmt.Sprintf("feature/%s-%02d", p.name, i)
			if i == 0 {
				group, branch = worktree.BaseGroup, "main"
			}
			m.rows = append(m.rows, row{
				org: p.org, project: p.name, root: "/" + p.name,
				wt:      worktree.Worktree{Path: "/" + p.name + "/" + branch, Branch: branch, Group: group},
				session: p.name + "/" + branch,
			})
		}
	}
	m.all = m.rows
	m.cursor = 0
	m.selSession = m.rows[0].session
}

func click(x, y int, b tea.MouseButton) tea.MouseMsg {
	m := tea.Mouse{X: x, Y: y, Button: b}
	if b == tea.MouseWheelUp || b == tea.MouseWheelDown {
		return tea.MouseWheelMsg(m)
	}
	return tea.MouseClickMsg(m)
}

func step(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// TestListHitsAgreeWithView walks every cell of the screen: whatever the hit
// test says is a row must be a line of the rendered view that shows that
// row's label, and every rendered row must be reachable. It runs with the
// cursor at the top, in the middle and at the end, where the list scrolls.
func TestListHitsAgreeWithView(t *testing.T) {
	m := testModel()
	manyRows(&m, 8)
	leftW, _, bodyH := m.geometry()
	for _, cur := range []int{0, 5, 12, len(m.rows) - 1} {
		m.cursor, m.selSession = cur, m.rows[cur].session
		screen := strings.Split(ansi.Strip(m.render()), "\n")
		seen := map[int]bool{}
		for y := 0; y < m.height; y++ {
			for x := 0; x < m.width; x++ {
				h := m.hit(x, y)
				switch {
				case y == 0 || y > bodyH:
					if h.zone != zoneNone {
						t.Fatalf("cursor %d: title/footer cell %d,%d is zone %d", cur, x, y, h.zone)
					}
				case x < leftW:
					if h.zone != zoneList || h.line != y-1 {
						t.Fatalf("cursor %d: list cell %d,%d hit %+v", cur, x, y, h)
					}
				case x == leftW:
					if h.zone != zoneNone {
						t.Fatalf("cursor %d: separator cell %d,%d is zone %d", cur, x, y, h.zone)
					}
				default:
					if h.zone != zonePane {
						t.Fatalf("cursor %d: pane cell %d,%d hit %+v", cur, x, y, h)
					}
				}
			}
			if y == 0 || y > bodyH {
				continue
			}
			i := m.listRowAt(y - 1)
			text := ansi.Cut(screen[y], 0, leftW)
			if i < 0 {
				// A line that is not a row must not show any row's label.
				for _, r := range m.rows {
					if strings.Contains(text, m.label(r)) && !strings.Contains(text, "feature") {
						t.Errorf("cursor %d: y=%d shows %q but hit-tests as no row: %q", cur, y, m.label(r), text)
					}
				}
				continue
			}
			seen[i] = true
			if !strings.Contains(text, m.label(m.rows[i])) {
				t.Errorf("cursor %d: y=%d hit-tests as row %d (%q) but the line is %q", cur, y, i, m.label(m.rows[i]), text)
			}
		}
		if !seen[cur] {
			t.Errorf("cursor %d: the selected row is not reachable by any click", cur)
		}
	}
}

// TestPaneRowsAgreeWithView pins the right pane's terminal area to what View
// draws, through paneChrome: the capture's first line must be on the screen
// line the mouse code calls pane row 0, and the line above it must not be.
func TestPaneRowsAgreeWithView(t *testing.T) {
	m := testModel()
	leftW, _, _ := m.geometry()
	screen := strings.Split(ansi.Strip(m.render()), "\n")
	for y := 0; y < m.height; y++ {
		if m.hit(leftW+1, y).zone != zonePane {
			continue
		}
		pr := m.paneRowAt(m.hit(leftW+1, y).line)
		text := ansi.Cut(screen[y], leftW+1, m.width)
		if got := strings.Contains(text, "hello agent"); got != (pr == 0) {
			t.Errorf("y=%d: pane row %d, shows capture line 0 = %v", y, pr, got)
		}
	}
	_, rows := m.paneSize()
	if last := m.paneRowAt(m.hit(leftW+1, m.height-2).line); last != rows-1 {
		t.Errorf("last body line is pane row %d, want %d", last, rows-1)
	}
}

func TestClickSelectsRowAndHeadersToggleFolds(t *testing.T) {
	m := mouseModel(t)
	manyRows(&m, 4)
	t.Setenv("DROVER_STATE_DIR", t.TempDir()) // folds are saved; never touch the real ones
	// Find a screen line for each kind: a row, an org band, a project header, a blank.
	screen := strings.Split(ansi.Strip(m.render()), "\n")
	leftW, _, bodyH := m.geometry()
	var rowY, bandY, blankY, projY = -1, -1, -1, -1
	for y := 1; y <= bodyH; y++ {
		text := strings.TrimSpace(ansi.Cut(screen[y], 0, leftW))
		switch {
		case m.listRowAt(y-1) >= 0 && rowY < 0 && m.listRowAt(y-1) == 2:
			rowY = y
		case strings.HasSuffix(text, "Acme") && bandY < 0: // headers carry a fold marker
			bandY = y
		case strings.HasSuffix(text, "platform") && projY < 0:
			projY = y
		case text == "" && blankY < 0 && y > 2:
			blankY = y
		}
	}
	if rowY < 0 || bandY < 0 || blankY < 0 || projY < 0 {
		t.Fatalf("test layout lacks a line kind: row %d band %d blank %d project %d\n%s", rowY, bandY, blankY, projY, strings.Join(screen, "\n"))
	}

	// A click on a header selects it and toggles its fold, as enter does.
	for name, y := range map[string]int{"org band": bandY, "project header": projY} {
		next := step(m, click(3, y, tea.MouseLeft))
		if next.hdr == "" || len(next.folds) != 1 || next.mode != modeList {
			t.Errorf("click on the %s: hdr %q, folds %v, mode %d; want the header selected and folded", name, next.hdr, next.folds, next.mode)
		}
		again := step(next, click(3, y, tea.MouseLeft))
		if len(again.folds) != 0 {
			t.Errorf("a second click on the %s must unfold it: %v", name, again.folds)
		}
	}
	// A blank line does nothing.
	if next := step(m, click(3, blankY, tea.MouseLeft)); next.cursor != 0 || next.hdr != "" || len(next.folds) != 0 {
		t.Errorf("click on a blank line changed state: cursor %d hdr %q folds %v", next.cursor, next.hdr, next.folds)
	}

	next := step(m, click(3, rowY, tea.MouseLeft))
	if next.cursor != 2 || next.selSession != m.rows[2].session {
		t.Errorf("click on row 2 selected %d (%q)", next.cursor, next.selSession)
	}
	if next.mode != modeList {
		t.Error("clicking a row must select, not focus")
	}
	// Clicking a row while a header is selected must leave the header.
	onHeader := m
	onHeader.hdr = orgID(m.rows[0]) // header selected, nothing folded, so lines stay where they are
	if back := step(onHeader, click(3, rowY, tea.MouseLeft)); back.hdr != "" || back.cursor != 2 {
		t.Errorf("a row click left the header selected: hdr %q cursor %d", back.hdr, back.cursor)
	}
	// Releases and motion are not clicks.
	rel := tea.MouseReleaseMsg(tea.Mouse{X: 3, Y: rowY, Button: tea.MouseLeft})
	if got := step(m, rel); got.cursor != 0 {
		t.Error("a release moved the selection")
	}
	// The separator and the footer are not the list.
	leftW, _, _ = m.geometry()
	for _, p := range [][2]int{{leftW, rowY}, {3, m.height - 1}, {3, 0}} {
		if got := step(m, click(p[0], p[1], tea.MouseLeft)); got.cursor != 0 || got.hdr != "" || len(got.folds) != 0 {
			t.Errorf("click at %v changed state", p)
		}
	}
}

// The wheel over the list must behave exactly like the arrow keys, which step
// through headers as well as worktrees.
func TestWheelOverListMovesLikeTheArrowKeys(t *testing.T) {
	m := mouseModel(t)
	manyRows(&m, 3)
	key := func(m Model, k string) Model {
		next, _ := m.Update(keyRunes(k))
		return next.(Model)
	}
	wheelModel, keyModel := m, m
	for i := 0; i < 40; i++ { // far past the end, to cover stopping at the bottom
		wheelModel = step(wheelModel, click(3, 5, tea.MouseWheelDown))
		keyModel = key(keyModel, "j")
		if wheelModel.cursor != keyModel.cursor || wheelModel.hdr != keyModel.hdr || wheelModel.selSession != keyModel.selSession {
			t.Fatalf("notch %d down: wheel at (%d,%q), j at (%d,%q)", i+1, wheelModel.cursor, wheelModel.hdr, keyModel.cursor, keyModel.hdr)
		}
	}
	for i := 0; i < 40; i++ { // and back past the top
		wheelModel = step(wheelModel, click(3, 5, tea.MouseWheelUp))
		keyModel = key(keyModel, "k")
		if wheelModel.cursor != keyModel.cursor || wheelModel.hdr != keyModel.hdr || wheelModel.selSession != keyModel.selSession {
			t.Fatalf("notch %d up: wheel at (%d,%q), k at (%d,%q)", i+1, wheelModel.cursor, wheelModel.hdr, keyModel.cursor, keyModel.hdr)
		}
	}
	if wheelModel.cursor != 0 && wheelModel.hdr == "" {
		t.Errorf("wheeling to the top left the cursor on row %d", wheelModel.cursor)
	}
}

func TestMouseIgnoredInPromptsHelpAndFocus(t *testing.T) {
	leftW := 0
	for _, setup := range []struct {
		name string
		f    func(*Model)
	}{
		{"new", func(m *Model) { m.mode = modeNew }},
		{"search", func(m *Model) { m.mode = modeSearch }},
		{"rename", func(m *Model) { m.mode = modeRename }},
		{"help", func(m *Model) { m.help = true }},
		{"focus", func(m *Model) { m.mode = modeFocus }},
	} {
		m := mouseModel(t)
		manyRows(&m, 3)
		setup.f(&m)
		before := m.mode
		leftW, _, _ = m.geometry()
		for _, msg := range []tea.MouseMsg{
			click(3, 6, tea.MouseLeft), click(3, 6, tea.MouseWheelDown),
			click(leftW+2, 6, tea.MouseLeft),
		} {
			got := step(m, msg)
			if got.cursor != 0 || got.mode != before || got.help != m.help {
				t.Errorf("%s: %v changed state (cursor %d, mode %d)", setup.name, msg, got.cursor, got.mode)
			}
		}
	}
}

// TestClickInPaneFocusesAndFocusSwallowsMouse uses a real tmux on a private
// socket: a click on a worktree with no session asks for a name, and once it
// is given the agent starts and is focused.
func TestClickInPaneFocusesAndFocusSwallowsMouse(t *testing.T) {
	skipWithoutTmux(t)
	m := mouseModel(t)
	manyRows(&m, 2)
	leftW, _, _ := m.geometry()
	m = step(m, click(leftW+5, 10, tea.MouseLeft))
	if m.mode != modeNameAgent {
		t.Fatalf("click in the pane left mode %d, want the name prompt", m.mode)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // an empty name takes the suggestion
	m = next.(Model)
	if m.mode != modeFocus {
		t.Fatalf("starting the agent left mode %d, want focus", m.mode)
	}
	if a, ok := m.curAgent(m.rows[0]); !ok || !m.tmux.Has(a.session) {
		t.Error("focusing by click did not start the agent")
	}
	// In focus mode a click on another row must not select it, and nothing
	// is forwarded: the pane only ever receives keys.
	if got := step(m, click(3, 4, tea.MouseLeft)); got.cursor != 0 || got.mode != modeFocus {
		t.Errorf("click in focus mode changed state: cursor %d mode %d", got.cursor, got.mode)
	}
	if got := step(m, click(3, 4, tea.MouseWheelDown)); got.cursor != 0 {
		t.Error("wheel over the list in focus mode moved the selection")
	}
}
