package fold

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := Path(t.TempDir())
	want := map[string]bool{"o:Acme": true, "p:Acme/infra": true, "g:Acme/infra/base": true}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSaveSkipsExpandedAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	if err := Save(path, map[string]bool{"a": true, "b": false}); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); len(got) != 1 || !got["a"] {
		t.Errorf("only collapsed ids belong in the file: %v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestLoadToleratesMissingAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	if got := Load(filepath.Join(dir, "nope.json")); got == nil || len(got) != 0 {
		t.Errorf("missing file: %v", got)
	}
	for _, body := range []string{"", "not json", `{"collapsed": 3}`, `["a"]`} {
		p := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Load(p); got == nil || len(got) != 0 {
			t.Errorf("corrupt %q: %v", body, got)
		}
	}
}

func TestSaveCreatesStateDir(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "deep", "state"))
	if err := Save(path, map[string]bool{"x": true}); err != nil {
		t.Fatal(err)
	}
	if !Load(path)["x"] {
		t.Error("not saved")
	}
}
