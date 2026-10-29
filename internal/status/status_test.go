package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadRemove(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := Write(dir, "infra/feature/a", Working, now); err != nil {
		t.Fatal(err)
	}
	got := ReadAll(dir)
	e, ok := got["infra/feature/a"]
	if !ok || e.State != Working {
		t.Fatalf("ReadAll = %+v", got)
	}
	if err := Remove(dir, "infra/feature/a"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, "infra/feature/a"); err != nil {
		t.Fatalf("removing a missing entry must not fail: %v", err)
	}
	if got := ReadAll(dir); len(got) != 0 {
		t.Fatalf("after Remove: %+v", got)
	}
}

func TestSinceSurvivesRepeatedState(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Now()
	_ = Write(dir, "s", Working, t0)
	_ = Write(dir, "s", Working, t0.Add(5*time.Second))
	e := ReadAll(dir)["s"]
	if !e.Since.Equal(t0.Truncate(0)) && e.Since.Sub(t0) > time.Millisecond {
		t.Errorf("Since moved to %v, want %v", e.Since, t0)
	}
	if e.Updated.Sub(t0) < 4*time.Second {
		t.Errorf("Updated = %v, should have advanced", e.Updated)
	}
	_ = Write(dir, "s", Waiting, t0.Add(6*time.Second))
	if e := ReadAll(dir)["s"]; e.State != Waiting || e.Since.Sub(t0) < 5*time.Second {
		t.Errorf("state change must reset Since: %+v", e)
	}
}

func TestDebounceSkipsRewrite(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Now()
	_ = Write(dir, "s", Working, t0)
	_ = Write(dir, "s", Working, t0.Add(100*time.Millisecond))
	if e := ReadAll(dir)["s"]; e.Updated.Sub(t0) > 10*time.Millisecond {
		t.Errorf("rewrite inside the debounce window: Updated = %v", e.Updated)
	}
}

func TestReadAllSkipsGarbage(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "weird.json"), []byte(`{"state":"exploding"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, ".tmp-123"), []byte(`{"state":"idle"}`), 0o600)
	_ = Write(dir, "ok", Idle, time.Now())
	got := ReadAll(dir)
	if len(got) != 1 || got["ok"].State != Idle {
		t.Fatalf("ReadAll = %+v, want only the valid entry", got)
	}
}

func TestReadAllMissingDir(t *testing.T) {
	if got := ReadAll(filepath.Join(t.TempDir(), "nope")); len(got) != 0 {
		t.Fatalf("missing dir: %+v", got)
	}
}
