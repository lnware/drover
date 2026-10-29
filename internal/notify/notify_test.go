package notify

import (
	"runtime"
	"slices"
	"testing"
)

// TestTextIsNeverPartOfTheScript guards against injection: session names come
// from branch names, so they must reach the notifier as plain arguments.
func TestTextIsNeverPartOfTheScript(t *testing.T) {
	cmd := command("drover", `x" & do shell script "evil" & "`)
	if cmd == nil {
		t.Skipf("no notifier for %s", runtime.GOOS)
	}
	if runtime.GOOS == "darwin" {
		for i, a := range cmd.Args {
			if i > 0 && cmd.Args[i-1] == "-e" && a != "" && slices.Contains([]string{`x" & do shell script "evil" & "`, "drover"}, a) {
				t.Errorf("notification text spliced into AppleScript: %q", a)
			}
		}
	}
	if !slices.Contains(cmd.Args, `x" & do shell script "evil" & "`) {
		t.Errorf("body must be passed as its own argument: %v", cmd.Args)
	}
}
