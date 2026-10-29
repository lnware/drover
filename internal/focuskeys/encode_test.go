package focuskeys

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/lnware/drover/internal/tmux"
)

// cases are written as the bytes a terminal sends, decoded by the same parser
// Bubble Tea uses, so they cover how keys really arrive (Alt+Shift+b is
// ShiftedCode, Ctrl+@ is "ctrl+space") and not how a test author builds them.
var cases = []struct {
	name string
	raw  string
	want Input
}{
	{"letter", "a", Input{Kind: KindText, Text: "a"}},
	{"capital", "A", Input{Kind: KindText, Text: "A"}},
	{"finnish", "ä", Input{Kind: KindText, Text: "ä"}},
	{"at sign", "@", Input{Kind: KindText, Text: "@"}},
	{"dash", "-", Input{Kind: KindText, Text: "-"}},
	{"space", " ", Input{Kind: KindText, Text: " "}},
	{"alt letter", "\x1bb", keys("M-b")},
	{"alt capital", "\x1bB", keys("M-B")},
	{"alt finnish", "\x1bä", keys("M-ä")},
	{"alt at sign", "\x1b@", keys("M-@")},
	{"alt space", "\x1b ", keys("M-Space")},
	{"alt enter", "\x1b\r", keys("M-Enter")},
	{"enter", "\r", keys("Enter")},
	{"tab", "\t", keys("Tab")},
	{"shift tab", "\x1b[Z", keys("BTab")},
	{"backspace", "\x7f", keys("BSpace")},
	{"escape", "\x1b", keys("Escape")},
	{"up", "\x1b[A", keys("Up")},
	{"alt up", "\x1b[1;3A", keys("M-Up")},
	{"ctrl up", "\x1b[1;5A", keys("C-Up")},
	{"shift left", "\x1b[1;2D", keys("S-Left")},
	{"ctrl shift up", "\x1b[1;6A", keys("C-S-Up")},
	{"ctrl a", "\x01", keys("C-a")},
	{"ctrl c", "\x03", keys("C-c")},
	{"ctrl z", "\x1a", keys("C-z")},
	{"ctrl backslash", "\x1c", keys("C-\\")},
	{"ctrl right bracket", "\x1d", keys("C-]")},
	{"ctrl at (ctrl+space)", "\x00", keys("C-Space")},
	{"ctrl alt a", "\x1b\x01", keys("C-M-a")},
	{"pgup", "\x1b[5~", keys("PageUp")},
	{"pgdown", "\x1b[6~", keys("PageDown")},
	{"delete", "\x1b[3~", keys("DC")},
	{"home", "\x1b[H", keys("Home")},
	{"end", "\x1b[F", keys("End")},
	// The same keys as a kitty-protocol terminal sends them, which Bubble Tea
	// negotiates with only the "disambiguate" flag.
	{"kitty ctrl a", "\x1b[97;5u", keys("C-a")},
	{"kitty escape", "\x1b[27u", keys("Escape")},
	{"kitty alt letter", "\x1b[98;3u", keys("M-b")},
	{"kitty alt capital (no shifted code)", "\x1b[98;4u", keys("M-B")},
	{"kitty alt capital (shifted code)", "\x1b[98:66;4u", keys("M-B")},
	{"kitty shift enter", "\x1b[13;2u", keys("S-Enter")},
	{"kitty ctrl enter", "\x1b[13;5u", keys("C-Enter")},
	{"kitty ctrl backslash", "\x1b[92;5u", keys("C-\\")},
	{"f5", "\x1b[15~", keys("F5")},
	{"f12", "\x1b[24~", keys("F12")},
}

// press decodes terminal bytes into the message Bubble Tea would deliver.
func press(t testing.TB, raw string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	_, ev := d.Decode([]byte(raw))
	k, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("%q decoded to %T, not a key press", raw, ev)
	}
	return tea.KeyPressMsg(uv.Key(k))
}

func keys(k ...string) Input { return Input{Kind: KindKeys, Keys: k} }

func TestEncode(t *testing.T) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := press(t, c.raw)
			got := Encode(msg)
			if got.Kind != c.want.Kind || got.Text != c.want.Text || !slices.Equal(got.Keys, c.want.Keys) {
				t.Errorf("Encode(%q = %q) = %+v, want %+v", c.raw, msg.String(), got, c.want)
			}
		})
	}
}

func TestEncodePaste(t *testing.T) {
	if got := EncodePaste("a\nb"); got.Kind != KindPaste || got.Text != "a\nb" {
		t.Errorf("EncodePaste = %+v", got)
	}
}

// TestKeyNamesAreValidTmux sends every encoded key name to a real tmux, which
// rejects names it cannot parse.
func TestKeyNamesAreValidTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	d := tmux.New(fmt.Sprintf("drover-keys-%d", os.Getpid()))
	t.Cleanup(func() { _ = d.KillServer() })
	// SIGINT is ignored so the C-c case does not end the session.
	if err := d.Spawn("k", t.TempDir(), "trap '' INT QUIT; exec cat -v"); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		in := Encode(press(t, c.raw))
		if in.Kind != KindKeys {
			continue
		}
		if err := d.SendKeys("k", in.Keys...); err != nil {
			t.Errorf("%s: tmux rejected %v: %v", c.name, in.Keys, err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	c, err := d.Capture("k")
	if err != nil {
		t.Fatal(err)
	}
	// cat -v echoes escape sequences caret-style; Up arrives as ^[OA or ^[[A.
	if joined := strings.Join(c.Lines, "\n"); !strings.Contains(joined, "^[") {
		t.Errorf("no escape sequences reached the pane:\n%s", joined)
	}
}
