package tmux

import (
	"strings"
	"testing"
	"time"
)

// Enabling DEC private modes the way a full-screen program does: 1049 is the
// alternate screen, 1000 mouse reports, 1006 their SGR encoding.
const (
	altOn   = `\033[?1049h`
	mouseOn = `\033[?1000h`
	sgrOn   = `\033[?1006h`
)

func spawnModes(t *testing.T, d Driver, name, modes, then string) {
	t.Helper()
	if err := d.Spawn(name, t.TempDir(), `printf '`+modes+`'; exec `+then); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureReportsAlternateScreenAndMouse(t *testing.T) {
	d := testDriver(t)
	spawnModes(t, d, "full", altOn+mouseOn+sgrOn, "cat")
	spawnModes(t, d, "plain", "", "cat")

	var c Capture
	for i := 0; i < 50; i++ { // the program enables its modes asynchronously
		var err error
		if c, err = d.Capture("full"); err != nil {
			t.Fatal(err)
		}
		if c.Alt && c.Mouse && c.MouseSGR {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !c.Alt || !c.Mouse || !c.MouseSGR {
		t.Errorf("full-screen mouse program reported as %+v", c)
	}
	p, err := d.Capture("plain")
	if err != nil {
		t.Fatal(err)
	}
	if p.Alt || p.Mouse || p.MouseSGR {
		t.Errorf("a plain pane reported alt=%v mouse=%v sgr=%v", p.Alt, p.Mouse, p.MouseSGR)
	}
	// the cursor fields must survive the longer format
	if p.CursorX < 0 || p.CursorY < 0 {
		t.Errorf("cursor not parsed: %+v", p)
	}
}

func TestSendWheelReachesAProgramThatAskedForTheMouse(t *testing.T) {
	d := testDriver(t)
	spawnModes(t, d, "m", mouseOn+sgrOn, "cat -v")
	time.Sleep(200 * time.Millisecond)

	if err := d.SendWheel("m", true, 3, 10, 5, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SendWheel("m", false, 1, 12, 6, true); err != nil {
		t.Fatal(err)
	}
	c := waitFor(t, d, "m", "^[[<65;12;6M")
	all := strings.Join(c.Lines, "")
	if got := strings.Count(all, "^[[<64;10;5M"); got != 3 {
		t.Errorf("wheel-up burst delivered %d of 3 events: %q", got, all)
	}
}

func TestSendWheelLegacyEncoding(t *testing.T) {
	d := testDriver(t)
	spawnModes(t, d, "m", mouseOn, "cat -v") // no 1006: the classic X10-style encoding
	time.Sleep(200 * time.Millisecond)

	// wheel up is button 64, sent as byte 32+64 = 96 ('`'), then 32+col and 32+row
	if err := d.SendWheel("m", true, 1, 10, 5, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, d, "m", "^[[M`*%")

	// beyond what a UTF-8 pane can carry in this encoding: dropped, not garbled
	if err := d.SendWheel("m", true, 1, 200, 5, false); err != nil {
		t.Fatal(err)
	}
}
