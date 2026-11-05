package autoupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
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
	digest := sha256.Sum256([]byte("new"))
	candidate, err := OpenVerifiedCandidate(staged, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	backup, err := ReplaceCandidate(target, candidate)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	previous, _ := os.ReadFile(backup)
	if string(got) != "new" || string(previous) != "old" {
		t.Fatalf("target=%q previous=%q", got, previous)
	}
}

func TestReplaceCandidateRejectsPathSwapAfterOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows prevents renaming an open file; native staged mutation is covered by the platform smoke test")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "orbit")
	staged := filepath.Join(dir, "staged")
	original := []byte("verified")
	if err := os.WriteFile(target, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, original, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	candidate, err := OpenVerifiedCandidate(staged, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := os.Rename(staged, staged+".verified"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("attacker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplaceCandidate(target, candidate); err == nil {
		t.Fatal("path-swapped candidate was accepted")
	}
	installed, err := os.ReadFile(target)
	if err != nil || string(installed) != "current" {
		t.Fatalf("installed=%q err=%v", installed, err)
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Fatalf("backup created before path-swap rejection: %v", err)
	}
}

func TestReplaceCandidateRejectsInPlaceMutationAfterOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "orbit")
	staged := filepath.Join(dir, "staged")
	original := []byte("verified")
	if err := os.WriteFile(target, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, original, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	candidate, err := OpenVerifiedCandidate(staged, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := os.WriteFile(staged, []byte("mutated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplaceCandidate(target, candidate); err == nil {
		t.Fatal("in-place mutation was accepted")
	}
	installed, err := os.ReadFile(target)
	if err != nil || string(installed) != "current" {
		t.Fatalf("installed=%q err=%v", installed, err)
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Fatalf("backup created before mutation rejection: %v", err)
	}
}
