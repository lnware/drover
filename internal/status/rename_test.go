package status

import (
	"testing"
	"time"
)

func TestRenameMovesStatusFile(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "infra/feature/old", Done, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := Rename(dir, "infra/feature/old", "infra/feature/new"); err != nil {
		t.Fatal(err)
	}
	got := ReadAll(dir)
	if _, ok := got["infra/feature/old"]; ok {
		t.Error("old status file remains")
	}
	if got["infra/feature/new"].State != Done {
		t.Errorf("entries = %v", got)
	}
	if err := Rename(dir, "infra/none", "infra/other"); err != nil {
		t.Errorf("a missing file must be fine: %v", err)
	}
}
