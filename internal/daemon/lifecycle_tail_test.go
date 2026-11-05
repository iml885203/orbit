package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestTailDaemonLog_ReturnsLinesAfterOffset(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())

	logPath := DefaultLogPath()
	if err := os.WriteFile(logPath, []byte("old-line-1\nold-line-2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	offset := daemonLogSize()
	// Append "new" lines after the marker.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("new-1\nnew-2\nnew-3\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	got := tailDaemonLog(offset, 10)
	want := "new-1\nnew-2\nnew-3"
	if got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
}

func TestTailDaemonLog_CapsAtMaxLines(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	lines := []string{"a", "b", "c", "d", "e"}
	if err := os.WriteFile(DefaultLogPath(), []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := tailDaemonLog(0, 3)
	want := "c\nd\ne"
	if got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
}

func TestTailDaemonLog_MissingFile(t *testing.T) {
	// Isolate via ORBIT_HOME (like the sibling tests) so DefaultLogPath points
	// into a fresh temp dir. Setting HOME alone doesn't isolate on Windows,
	// where the log path derives from ORBIT_HOME (%LOCALAPPDATA%\orbit).
	t.Setenv("ORBIT_HOME", t.TempDir())
	if got := tailDaemonLog(0, 10); got != "" {
		t.Errorf("tail of missing file = %q, want empty", got)
	}
}

func TestFormatLogTail_EmptyReturnsEmpty(t *testing.T) {
	if got := formatLogTail(""); got != "" {
		t.Errorf("formatLogTail(\"\") = %q, want empty", got)
	}
}
