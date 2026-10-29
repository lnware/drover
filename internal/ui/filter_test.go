package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/alias"
	"github.com/lnware/drover/internal/status"
)

func press(t *testing.T, m Model, keys ...tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(k)
		m = next.(Model)
	}
	return m, cmd
}

func esc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func TestEditTextDropsControlCharacters(t *testing.T) {
	got := editText("a", keyRunes("b\x1b[31m\nc"))
	if got != "ab[31mc" {
		t.Errorf("editText = %q, want control characters dropped", got)
	}
	if got := editText("ab", tea.KeyPressMsg{Code: []rune("x")[0], Mod: tea.ModAlt}); got != "ab" {
		t.Errorf("alt+key must not type: %q", got)
	}
}

func TestSearchFiltersLiveAndEscClears(t *testing.T) {
	m := testModel()
	m, _ = press(t, m, keyRunes("/"))
	if m.mode != modeSearch {
		t.Fatalf("/ did not start search: mode %d", m.mode)
	}
	m, _ = press(t, m, keyRunes("fea"))
	if len(m.rows) != 1 || m.rows[0].session != "infra/feature/a" {
		t.Fatalf("live filter: rows = %+v", m.rows)
	}
	if len(m.all) != 2 {
		t.Fatal("filtering must not drop rows from the full list")
	}
	m, _ = press(t, m, esc())
	if m.mode != modeList || m.search != "" || len(m.rows) != 2 {
		t.Errorf("esc must clear: mode=%d search=%q rows=%d", m.mode, m.search, len(m.rows))
	}
}

func TestSearchEnterKeepsFilterAndEscThenClearsIt(t *testing.T) {
	m := testModel()
	m, _ = press(t, m, keyRunes("/"), keyRunes("main"), enter())
	if m.mode != modeList || !m.filtering() || len(m.rows) != 1 {
		t.Fatalf("enter must keep the filter: mode=%d rows=%d", m.mode, len(m.rows))
	}
	m, _ = press(t, m, esc())
	if m.filtering() || len(m.rows) != 2 {
		t.Errorf("esc in the list must clear the filter: rows=%d", len(m.rows))
	}
}

func TestSearchMatchesStateWordAliasAndAllTerms(t *testing.T) {
	m := testModel()
	m.agents = map[string]status.Entry{"infra/main": {State: status.Waiting}}
	m.aliases = map[string]string{"infra/feature/a": "flux setup"}
	for query, want := range map[string]int{
		"waiting":       1, // state word
		"flux":          1, // alias
		"INFRA":         2, // case-insensitive, project name
		"infra feature": 1, // every word must match
		"nomatch":       0,
	} {
		m.search = query
		m.applyFilter()
		if len(m.rows) != want {
			t.Errorf("search %q: %d rows, want %d", query, len(m.rows), want)
		}
	}
}

func TestSelectionMovesWithFilterAndRestartsPreview(t *testing.T) {
	m := testModel()
	m.move(2) // past the feature header to feature/a (headers are selectable now)
	if m.selSession != "infra/feature/a" {
		t.Fatal("setup: selection not on feature/a")
	}
	m, cmd := press(t, m, keyRunes("/"), keyRunes("main"))
	if m.selSession != "infra/main" || cmd == nil {
		t.Errorf("selection = %q, cmd = %v: must move to the surviving row and restart the preview", m.selSession, cmd != nil)
	}
}

func TestAttentionFilter(t *testing.T) {
	m := testModel()
	m.agents = map[string]status.Entry{
		"infra/main":      {State: status.Working},
		"infra/feature/a": {State: status.Done}, // done, but not running
	}
	m, _ = press(t, m, keyRunes("w"))
	if len(m.rows) != 0 {
		t.Errorf("working and stopped agents need no attention: %+v", m.rows)
	}
	m.agents["infra/main"] = status.Entry{State: status.Waiting}
	m.applyFilter()
	if len(m.rows) != 1 || m.rows[0].session != "infra/main" {
		t.Errorf("waiting agent must show: %+v", m.rows)
	}
	m, _ = press(t, m, keyRunes("w"))
	if m.attention || len(m.rows) != 2 {
		t.Errorf("w again must show everything: attention=%v rows=%d", m.attention, len(m.rows))
	}
}

func TestStatusUpdatesRefilterAttention(t *testing.T) {
	m := testModel()
	m.attention = true
	m.applyFilter()
	next, _ := m.handleStatus(statusMsg{entries: map[string]status.Entry{"infra/main": {State: status.Waiting}}})
	m = next.(Model)
	if len(m.rows) != 1 {
		t.Errorf("a new waiting state must appear in the attention view: %d rows", len(m.rows))
	}
}

func TestTitleBarAndListShowFilter(t *testing.T) {
	m := testModel()
	m, _ = press(t, m, keyRunes("/"), keyRunes("fea"), enter())
	got := ansi.Strip(m.render())
	for _, want := range []string{"1 of 2 worktrees", "/fea", "filtered · esc clears"} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	m.search = "zzz"
	m.applyFilter()
	if !strings.Contains(ansi.Strip(m.render()), "no matches") {
		t.Error("an empty filter result must say so")
	}
}

func TestRenameSavesAliasAndShowsIt(t *testing.T) {
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	m := testModel()
	m, _ = press(t, m, keyRunes("r"))
	if m.mode != modeRename || m.renameSession != "infra/main" {
		t.Fatalf("r did not open rename for the selection: mode=%d session=%q", m.mode, m.renameSession)
	}
	m, _ = press(t, m, keyRunes("trunk line"), enter())
	if m.mode != modeList || m.aliases["infra/main"] != "trunk line" {
		t.Fatalf("alias not applied: mode=%d aliases=%v", m.mode, m.aliases)
	}
	if got := alias.Load(alias.Path(status.Root()))["infra/main"]; got != "trunk line" {
		t.Errorf("alias not persisted: %q", got)
	}
	view := ansi.Strip(m.render())
	if !strings.Contains(view, "trunk line") {
		t.Error("alias missing from the view")
	}
	// the branch stays visible in the header, and search finds either name
	if !strings.Contains(view, "main") {
		t.Error("branch must still show beside the alias")
	}
	m.search = "trunk"
	m.applyFilter()
	if len(m.rows) != 1 {
		t.Error("search must find a row by its alias")
	}

	// reopening prefills the alias; an empty name clears it
	m.search = ""
	m.applyFilter()
	m, _ = press(t, m, keyRunes("r"))
	if m.input != "trunk line" {
		t.Errorf("rename must prefill the current alias: %q", m.input)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, enter())
	if _, still := m.aliases["infra/main"]; still {
		t.Error("empty name must clear the alias")
	}
	if got := alias.Load(alias.Path(status.Root())); len(got) != 0 {
		t.Errorf("alias file not cleared: %v", got)
	}
}

func TestRenameEscCancelsWithoutWriting(t *testing.T) {
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	m := testModel()
	m, _ = press(t, m, keyRunes("r"), keyRunes("nope"), esc())
	if m.mode != modeList || len(m.aliases) != 0 {
		t.Errorf("esc must cancel: mode=%d aliases=%v", m.mode, m.aliases)
	}
	if got := alias.Load(alias.Path(status.Root())); len(got) != 0 {
		t.Errorf("cancelled rename wrote %v", got)
	}
}

func TestSetRunningUpdatesBothLists(t *testing.T) {
	m := testModel()
	m.search = "feature"
	m.applyFilter()
	m.setRunning("infra/feature/a", true)
	if !m.all[1].running || !m.rows[0].running {
		t.Errorf("full and filtered lists disagree: all=%v rows=%v", m.all[1].running, m.rows[0].running)
	}
}

func TestViewIsExactlyTerminalSizedInNewModes(t *testing.T) {
	m := testModel()
	for _, mode := range []mode{modeSearch, modeRename} {
		m.mode, m.search, m.input, m.renameSession = mode, "fe", "label", "infra/main"
		lines := strings.Split(m.render(), "\n")
		if len(lines) != m.height {
			t.Errorf("mode %d: %d lines, want %d", mode, len(lines), m.height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != m.width {
				t.Errorf("mode %d line %d width %d, want %d", mode, i, w, m.width)
			}
		}
	}
}
