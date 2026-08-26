package autoupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplacePreservesPreviousBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "orbit")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	backup, err := Replace(target, staged)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	previous, _ := os.ReadFile(backup)
	if string(got) != "new" || string(previous) != "old" {
		t.Fatalf("target=%q previous=%q", got, previous)
	}
}
