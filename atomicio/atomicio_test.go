package atomicio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile_CreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")

	if err := WriteFile(target, []byte("one"), 0644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFile(target, []byte("two"), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "two" {
		t.Errorf("got %q, want %q", got, "two")
	}
}

func TestWriteFile_LeavesNoTmpOnSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	if err := WriteFile(target, []byte("ok"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != "state.json" {
			t.Errorf("stray file: %q", name)
		}
	}
}
