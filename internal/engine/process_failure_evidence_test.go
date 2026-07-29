package engine

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestProcessFailureEvidenceUsesLatestStderr(t *testing.T) {
	got := processFailureEvidence(
		errors.New("exit status 1"),
		[]string{"", "[orbit] started", "ModuleNotFoundError: No module named 'example'"},
	)
	if got != "ModuleNotFoundError: No module named 'example'" {
		t.Fatalf("failure evidence = %q", got)
	}
}

func TestProcessFailureEvidenceTruncatesLongStderr(t *testing.T) {
	got := processFailureEvidence(errors.New("exit status 1"), []string{strings.Repeat("x", 300)})
	if len([]rune(got)) != 240 || !strings.HasSuffix(got, "…") {
		t.Fatalf("failure evidence length/suffix = %d/%q", len([]rune(got)), got)
	}
}

func TestProcessFailureEvidenceOmitsOutputForSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows process termination does not expose a Unix signal")
	}
	cmd := exec.Command("sh", "-c", "kill -KILL $$")
	err := cmd.Run()
	var processExit *exec.ExitError
	if !errors.As(err, &processExit) || processExit.ExitCode() >= 0 {
		t.Fatalf("test process error = %v, want signaled ExitError", err)
	}

	got := processFailureEvidence(err, []string{`127.0.0.1 - "GET /health HTTP/1.1" 200 -`})
	if got != "" {
		t.Fatalf("signal failure evidence = %q, want empty", got)
	}
}
