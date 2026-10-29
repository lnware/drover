package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// numbers maps each row of a capture of `seq` output to its number (-1 for
// blank rows), which makes "which lines are on screen" easy to assert.
func numbers(lines []string) []int {
	out := make([]int, len(lines))
	for i, l := range lines {
		n, err := strconv.Atoi(strings.TrimSpace(l))
		if err != nil {
			n = -1
		}
		out[i] = n
	}
	return out
}

func TestCaptureScrollWindowAndHistoryLimit(t *testing.T) {
	d := testDriver(t)
	// More lines than HistoryLimit, so the limit itself is asserted, and on
	// the first pane of a fresh server, which is the one a late set-option
	// would have missed.
	const total = HistoryLimit + 2000
	if err := d.Spawn("s", t.TempDir(), "seq 1 "+strconv.Itoa(total)+"; cat"); err != nil {
		t.Fatal(err)
	}
	if err := d.Resize("s", 40, 10); err != nil {
		t.Fatal(err)
	}
	live := waitFor(t, d, "s", strconv.Itoa(total))
	want := numbers(live.Lines)

	got, hist, err := d.CaptureScroll("s", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	// tmux drops 10% of the history in one go when it passes the limit, so
	// the size sits between 90% and 100% of it.
	if hist > HistoryLimit || hist < HistoryLimit*9/10 {
		t.Errorf("history_size = %d, want within 10%% of the limit %d", hist, HistoryLimit)
	}
	for i, n := range numbers(got.Lines) {
		if n != want[i] {
			t.Fatalf("off 0 row %d = %d, live capture has %d", i, n, want[i])
		}
	}

	for _, off := range []int{1, 7, 100, 5000} {
		c, _, err := d.CaptureScroll("s", off, 10)
		if err != nil {
			t.Fatal(err)
		}
		g := numbers(c.Lines)
		if len(g) != 10 {
			t.Fatalf("off %d: %d rows, want 10", off, len(g))
		}
		// Row i of the live screen holds line want[0]+i (the last row is
		// blank under cat's cursor), so scrolling back by off shifts that.
		for i, n := range g {
			if wantN := want[0] + i - off; n != wantN {
				t.Fatalf("off %d row %d = %d, want %d (window %v)", off, i, n, wantN, g)
			}
		}
	}

	// Past the oldest line tmux clamps; the caller sees off > hist and fixes it.
	c, hist, err := d.CaptureScroll("s", HistoryLimit+500, 10)
	if err != nil {
		t.Fatal(err)
	}
	if hist > HistoryLimit || len(c.Lines) > 10 {
		t.Errorf("clamped capture: hist %d, %d rows", hist, len(c.Lines))
	}
}

// BenchmarkCaptureScroll shows the cost of one scroll step against a full
// history: it is the process start, not the history size, that is paid.
func BenchmarkCaptureScroll(b *testing.B) {
	if _, err := exec.LookPath("tmux"); err != nil {
		b.Skip("tmux not installed")
	}
	d := New(fmt.Sprintf("agt-mouse-bench-%d", os.Getpid()))
	b.Cleanup(func() { _ = d.KillServer() })
	if err := d.Spawn("s", b.TempDir(), "seq 1 12000; cat"); err != nil {
		b.Fatal(err)
	}
	if err := d.Resize("s", 100, 40); err != nil {
		b.Fatal(err)
	}
	for i := 0; ; i++ {
		if c, _ := d.Capture("s"); strings.Contains(strings.Join(c.Lines, ""), "12000") {
			break
		}
		if i > 200 {
			b.Fatal("agent never finished printing")
		}
		time.Sleep(20 * time.Millisecond)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := d.CaptureScroll("s", 5000, 40); err != nil {
			b.Fatal(err)
		}
	}
}
