package layout

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndForget(t *testing.T) {
	dir := t.TempDir()
	path := Path(filepath.Join(dir, "deep", "state"))
	if err := Save(path, 52); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != 52 {
		t.Errorf("got %d, want 52", got)
	}
	if err := Save(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("forgetting must remove the file: %v", err)
	}
	if err := Save(path, 0); err != nil {
		t.Errorf("forgetting twice: %v", err)
	}
}

func TestLoadToleratesMissingAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	if got := Load(filepath.Join(dir, "nope.json")); got != 0 {
		t.Errorf("missing file: %d", got)
	}
	for _, body := range []string{"", "not json", `{"list_width": "wide"}`, `{"list_width": -4}`, `[3]`} {
		p := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Load(p); got != 0 {
			t.Errorf("corrupt %q: %d", body, got)
		}
	}
}
