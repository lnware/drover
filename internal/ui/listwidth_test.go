package ui

import "testing"

func TestListWidth(t *testing.T) {
	for _, c := range []struct{ total, want int }{
		{60, 26},  // the smallest window: the floor
		{87, 26},  // 30% is 26
		{100, 30}, // scales with the window
		{120, 36}, // unchanged from before
		{154, 46}, // reaches the cap
		{200, 46}, // unchanged from before
		{256, 46}, // a laptop screen is unchanged
		{300, 57}, // grows past it: 46 + 44/4
		{420, 87}, // a wide screen
		{600, 90}, // and stops
	} {
		if got := listWidth(c.total); got != c.want {
			t.Errorf("listWidth(%d) = %d, want %d", c.total, got, c.want)
		}
	}
}

// The list never shrinks as the window grows, and the pane keeps real room.
func TestListWidthIsMonotonicAndLeavesRoomForThePane(t *testing.T) {
	prev := 0
	for total := minWidth; total <= 700; total++ {
		w := listWidth(total)
		if w < prev {
			t.Fatalf("the list shrank from %d to %d going to %d columns", prev, w, total)
		}
		prev = w
		m := testModel()
		m.width = total
		l, r, _ := m.geometry()
		if l+1+r != total {
			t.Fatalf("width %d: list %d + separator + pane %d do not add up", total, l, r)
		}
		if r < 30 || r < l {
			t.Fatalf("width %d: the pane is only %d wide next to a %d list", total, r, l)
		}
	}
}
