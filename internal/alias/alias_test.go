package alias

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetLoadClear(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "state"))
	if got := Load(path); len(got) != 0 {
		t.Fatalf("missing file: %v", got)
	}
	if err := Set(path, "infra/feature/a", "flux base"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "infra/main", "trunk"); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got["infra/feature/a"] != "flux base" || got["infra/main"] != "trunk" {
		t.Fatalf("Load = %v", got)
	}
	if err := Set(path, "infra/feature/a", ""); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); len(got) != 1 || got["infra/main"] != "trunk" {
		t.Fatalf("empty name must clear only that alias: %v", got)
	}
}

func TestLoadSurvivesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliases.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); len(got) != 0 {
		t.Fatalf("corrupt file: %v", got)
	}
	if err := Set(path, "s", "x"); err != nil {
		t.Fatalf("Set must recover from a corrupt file: %v", err)
	}
	if Load(path)["s"] != "x" {
		t.Fatal("alias not stored after recovery")
	}
}
