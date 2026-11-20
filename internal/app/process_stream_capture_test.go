package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLifecycleProcessStreamCaptureHandlesLargeOutput(t *testing.T) {
	const childEnv = "ORBIT_TEST_LARGE_STREAM_CAPTURE"
	if os.Getenv(childEnv) == "1" {
		restore := captureLifecycleProcessStreams(t)
		defer restore()
		stdout := bytes.Repeat([]byte("o"), 1<<20)
		stderr := bytes.Repeat([]byte("e"), 1<<20)
		if _, err := os.Stdout.Write(stdout); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stderr.Write(stderr); err != nil {
			t.Fatal(err)
		}
		gotOut, gotErr := restore()
		if !bytes.Equal(gotOut, stdout) || !bytes.Equal([]byte(gotErr), stderr) {
			t.Fatalf("captured stdout=%d stderr=%d bytes, want %d each", len(gotOut), len(gotErr), len(stdout))
		}
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestLifecycleProcessStreamCaptureHandlesLargeOutput$", "-test.timeout=5s")
	child.Env = append(os.Environ(), childEnv+"=1")
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("large stream capture did not complete: %v\n%s", err, output)
	}
}
