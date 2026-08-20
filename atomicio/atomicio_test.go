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

// A write is where the containing directory has to exist.
func TestWriteFile_CreatesMissingParent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "state.json")

	if err := WriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf("content = %q", got)
	}
}

// The created directory's mode must not depend on which file lands in it
// first: writers into one directory disagree about their own file modes, and
// deriving the directory's from them made the same home come out 0700 or 0755
// depending on write order.
func TestWriteFile_CreatedDirModeIsIndependentOfFilePerm(t *testing.T) {
	base := t.TempDir()

	for _, perm := range []os.FileMode{0o600, 0o644} {
		dir := filepath.Join(base, perm.String())
		if err := WriteFile(filepath.Join(dir, "f.json"), []byte("{}"), perm); err != nil {
			t.Fatalf("WriteFile(perm=%o): %v", perm, err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Errorf("file perm %o produced dir mode %o, want 0755", perm, got)
		}
	}
}
