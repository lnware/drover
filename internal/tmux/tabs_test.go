package tmux

import (
	"strings"
	"testing"
)

func TestExpandTabsReachesTheNextStop(t *testing.T) {
	for in, want := range map[string]string{
		"no tabs":            "no tabs",
		"a\tb":               "a       b",
		"12345678\tb":        "12345678        b",
		"\tx":                "        x",
		"a.\tIN\tTXT\t\"x\"": "a.      IN      TXT     \"x\"",
		// Escape sequences take no column, and wide runes take two.
		"\x1b[31mab\x1b[0m\tc": "\x1b[31mab\x1b[0m      c",
		"界\tx":                 "界      x",
	} {
		if got := expandTabs(in); got != want {
			t.Errorf("expandTabs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCaptureShowsTabbedOutputAsColumns(t *testing.T) {
	d := testDriver(t)
	if err := d.Spawn("s", t.TempDir(), `printf 'a.\tIN\tTXT\n'; sleep 60`); err != nil {
		t.Fatal(err)
	}
	c := waitFor(t, d, "s", "TXT")
	if got := c.Lines[0]; strings.Contains(got, "\t") || !strings.HasPrefix(got, "a.      IN      TXT") {
		t.Errorf("first line %q", got)
	}
	scrolled, _, err := d.CaptureScroll("s", 0, 3)
	if err != nil || strings.Contains(scrolled.Lines[0], "\t") {
		t.Errorf("scrollback capture keeps tabs: %q, %v", scrolled.Lines, err)
	}
}
