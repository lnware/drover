package projects

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAddIsOnceAndSurvivesAReload(t *testing.T) {
	t.Setenv("DROVER_STATE_DIR", t.TempDir())
	if got := Load(); len(got) != 0 {
		t.Fatalf("empty state: %v", got)
	}
	for _, r := range []string{"/a/one/", "/a/two", "/a/one"} {
		if err := Add(r); err != nil {
			t.Fatal(err)
		}
	}
	if got := Load(); !slices.Equal(got, []string{"/a/one", "/a/two"}) {
		t.Errorf("Load = %v", got)
	}
}

func TestLoadIgnoresAGarbledFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DROVER_STATE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "projects.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got != nil {
		t.Errorf("Load = %v", got)
	}
}

func TestMergeKeepsConfigFirstAndDropsVanishedFolders(t *testing.T) {
	here := t.TempDir()
	gone := filepath.Join(here, "gone")
	got := Merge([]string{"/cfg/a", here}, []string{here, "/cfg/a", gone, "/cfg/a"})
	// "/cfg/a" does not exist but is configured, so it stays; a registered
	// duplicate of it is not added; the vanished registered folder is dropped.
	if want := []string{"/cfg/a", here}; !slices.Equal(got, want) {
		t.Errorf("Merge = %v, want %v", got, want)
	}
}
