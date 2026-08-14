package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The daemon can create the home again after waitForDaemonStop returns: the
// pid stops being alive before its last write lands. A single sweep can
// therefore run while the directory is absent, report nothing to do, and be
// followed by the write that recreates it — which is what a user saw as
// "Cleaned instance" leaving a directory behind, but only for instances that
// had actually started a daemon.
func TestSweepInstanceHomeOutlastsALateRecreate(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "late")

	// Recreate the home shortly after the sweep starts, standing in for the
	// daemon's parting write.
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = os.MkdirAll(home, 0o755)
	}()

	if err := sweepInstanceHome(base, "late"); err != nil {
		t.Fatalf("sweepInstanceHome: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("home survived the sweep: %v", err)
	}
}

// An instance that never started a daemon has nothing writing behind the
// sweep, and must not cost the extra passes' worth of waiting.
func TestSweepInstanceHomeReturnsPromptlyWhenAlreadyGone(t *testing.T) {
	base := t.TempDir()

	start := time.Now()
	if err := sweepInstanceHome(base, "never-existed"); err != nil {
		t.Fatalf("sweepInstanceHome: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Errorf("sweep took %v with nothing to do", elapsed)
	}
}
