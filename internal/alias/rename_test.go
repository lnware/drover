package alias

import (
	"path/filepath"
	"testing"
)

func TestRenameMovesAliasAndLeavesOthers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "aliases.json")
	for k, v := range map[string]string{"infra/feature/old": "Old label", "infra/main": "Main"} {
		if err := Set(p, k, v); err != nil {
			t.Fatal(err)
		}
	}
	moved, err := Rename(p, "infra/feature/old", "infra/feature/new")
	if err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	got := Load(p)
	if got["infra/feature/new"] != "Old label" || got["infra/main"] != "Main" {
		t.Errorf("aliases = %v", got)
	}
	if _, ok := got["infra/feature/old"]; ok {
		t.Error("the old key is still there")
	}
}

func TestRenameWithoutAliasKeepsUnrelatedEntryUnderNewName(t *testing.T) {
	p := filepath.Join(t.TempDir(), "aliases.json")
	if err := Set(p, "infra/feature/new", "Keep me"); err != nil {
		t.Fatal(err)
	}
	moved, err := Rename(p, "infra/feature/old", "infra/feature/new")
	if err != nil || moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if Load(p)["infra/feature/new"] != "Keep me" {
		t.Error("an alias that was not ours was changed")
	}
}
