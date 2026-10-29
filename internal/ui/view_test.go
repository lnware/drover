package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

func TestFitIsExactWidth(t *testing.T) {
	for _, s := range []string{"", "abc", "\x1b[31mred\x1b[0m", strings.Repeat("ä", 50), "\x1b[1;38;5;200m" + strings.Repeat("x", 80)} {
		if got := ansi.StringWidth(fit(s, 20)); got != 20 {
			t.Errorf("fit(%q, 20) has width %d", s, got)
		}
	}
}

func TestWithCursorKeepsWidthAndMarksCell(t *testing.T) {
	line := fit("\x1b[31mhello\x1b[0m", 10)
	got := withCursor(line, 1, 10)
	if w := ansi.StringWidth(got); w != 10 {
		t.Errorf("width after cursor = %d, want 10", w)
	}
	if !strings.Contains(got, "\x1b[7me\x1b[27m") {
		t.Errorf("cursor not drawn over 'e': %q", got)
	}
	if ansi.Strip(got) != ansi.Strip(line) {
		t.Errorf("cursor changed visible text: %q vs %q", ansi.Strip(got), ansi.Strip(line))
	}
}

func TestPreviewLinesPadsAndKeepsBottom(t *testing.T) {
	p := preview{cap: tmux.Capture{Lines: []string{"a", "b", "c", "d"}, CursorY: 3}}
	got := previewLines(p, 5, 3, false)
	if len(got) != 3 || strings.TrimSpace(got[0]) != "b" || strings.TrimSpace(got[2]) != "d" {
		t.Errorf("tall capture must keep the bottom rows: %q", got)
	}
	got = previewLines(preview{cap: tmux.Capture{Lines: []string{"a"}}}, 5, 3, false)
	if len(got) != 3 || ansi.StringWidth(got[2]) != 5 {
		t.Errorf("short capture must be padded: %q", got)
	}
}

func testModel() Model {
	m := New(config.Default())
	m.cfg.Projects = []string{"/p/infra"}
	m.width, m.height = 120, 30
	m.rows = []row{
		{project: "infra", wt: worktree.Worktree{Path: "/p/infra/main", Branch: "main", Group: worktree.BaseGroup}, session: "infra/main", running: true},
		{project: "infra", wt: worktree.Worktree{Path: "/p/infra/feature/a", Branch: "feature/a", Group: "feature"}, session: "infra/feature/a"},
	}
	m.all = m.rows
	m.selSession = "infra/main"
	m.preview = preview{session: "infra/main", ok: true, cap: tmux.Capture{Lines: []string{"\x1b[32mhello agent\x1b[0m"}}}
	return m
}

func TestViewLinesAreExactlyTerminalSized(t *testing.T) {
	m := testModel()
	for _, mode := range []mode{modeList, modeFocus} {
		m.mode = mode
		lines := strings.Split(m.render(), "\n")
		if len(lines) != m.height {
			t.Errorf("mode %d: %d lines, want %d", mode, len(lines), m.height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != m.width {
				t.Errorf("mode %d line %d width %d, want %d", mode, i, w, m.width)
			}
		}
		if !strings.Contains(ansi.Strip(m.render()), "hello agent") {
			t.Errorf("mode %d: preview text missing", mode)
		}
	}
}

func TestDisplayStates(t *testing.T) {
	m := testModel()
	r := m.rows[0] // running
	if d := m.display(r); d.word != "running" {
		t.Errorf("running with no hook data = %q, want plain running", d.word)
	}
	if d := m.display(m.rows[1]); d.glyph != "○" || d.word != "" {
		t.Errorf("not running = %+v", d)
	}
	for state, want := range map[status.State]string{
		status.Working: "working", status.Waiting: "waiting", status.Done: "done",
		status.Error: "error", status.Idle: "idle",
	} {
		m.agents = map[string]status.Entry{r.session: {State: state}}
		if d := m.display(r); d.word != want {
			t.Errorf("state %s displays as %q, want %q", state, d.word, want)
		}
	}
}

func TestStateShownInListHeaderAndTitle(t *testing.T) {
	m := testModel()
	m.agents = map[string]status.Entry{"infra/main": {State: status.Waiting, Since: time.Now().Add(-90 * time.Second)}}
	got := ansi.Strip(m.render())
	for _, want := range []string{"waiting", "◆ 1 waiting", "· 1m"} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q:\n%s", want, got)
		}
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d width %d, want %d", i, w, m.width)
		}
	}
}

func TestHandleStatusSeedsBaselineThenTracks(t *testing.T) {
	m := testModel()
	m.cfg.Notify = false // never fire real notifications from a test
	next, _ := m.handleStatus(statusMsg{entries: map[string]status.Entry{"infra/main": {State: status.Working}}})
	m = next.(Model)
	if !m.statusLoaded || m.prevStates["infra/main"] != status.Working {
		t.Fatalf("baseline not recorded: %+v", m.prevStates)
	}
	if got := m.agents["infra/main"].State; got != status.Working {
		t.Errorf("agents not stored: %v", got)
	}
}

func TestNotificationText(t *testing.T) {
	cases := map[status.State]string{
		status.Waiting: "s needs you", status.Done: "s finished", status.Error: "s hit an error",
		status.Working: "", status.Idle: "",
	}
	for state, want := range cases {
		if got := notification("s", state); got != want {
			t.Errorf("notification(%s) = %q, want %q", state, got, want)
		}
	}
}

func TestAge(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{
		5 * time.Second: "5s", 3 * time.Minute: "3m", 5 * time.Hour: "5h", 72 * time.Hour: "3d", -time.Second: "0s",
	} {
		if got := age(now.Add(-d), now); got != want {
			t.Errorf("age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestPaneSizeMatchesRenderedArea(t *testing.T) {
	m := testModel()
	cols, rows := m.paneSize()
	if cols <= 0 || rows <= 0 {
		t.Fatalf("pane size %dx%d", cols, rows)
	}
	_, rightW, bodyH := m.geometry()
	if cols != rightW || rows != bodyH-paneChrome {
		t.Errorf("pane size %dx%d disagrees with geometry %d x %d", cols, rows, rightW, bodyH)
	}
}
