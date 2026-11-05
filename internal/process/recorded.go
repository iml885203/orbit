package process

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/platform"
	"github.com/mattn/go-shellwords"
)

// RecordedProcess is a service process a daemon persisted before it exited.
// Command and Dir are the service's configured command and working directory.
type RecordedProcess struct {
	Name    string
	PID     int
	PGID    int
	Command string
	Dir     string
}

// OwnershipMismatchError explains why a live PID cannot be proven to still be
// the recorded service. PIDs are reused after a reboot, and retiring kills the
// whole process group, so anything short of proof must be left alone.
type OwnershipMismatchError struct {
	Process RecordedProcess
	Reason  string
}

func (e *OwnershipMismatchError) Error() string {
	return fmt.Sprintf("pid %d no longer matches service %q: %s", e.Process.PID, e.Process.Name, e.Reason)
}

// RetireRecorded stops a recorded process group without a running daemon.
// It returns (false, nil) when the process already exited, and an
// *OwnershipMismatchError, without signalling anything, when the live process
// is not provably the recorded one.
func RetireRecorded(rec RecordedProcess, gracePeriod time.Duration) (bool, error) {
	if !platform.IsProcessAlive(rec.PID) {
		return false, nil
	}
	if reason := recordMismatch(rec); reason != "" {
		return false, &OwnershipMismatchError{Process: rec, Reason: reason}
	}
	if err := KillGroup(rec.PGID, gracePeriod); err != nil {
		return false, fmt.Errorf("stopping %q (pgid %d): %w", rec.Name, rec.PGID, err)
	}
	return true, nil
}

func recordMismatch(rec RecordedProcess) string {
	if rec.PGID <= 0 || rec.Command == "" || rec.Dir == "" {
		return "record lacks the pgid, command, or directory needed to prove ownership"
	}
	// Orbit starts every service as its own process group leader.
	if rec.PGID != rec.PID {
		return fmt.Sprintf("recorded pid %d does not lead its group %d", rec.PID, rec.PGID)
	}
	if pgid, err := platform.GetProcessGroup(rec.PID); err != nil || pgid != rec.PGID {
		return fmt.Sprintf("process group is not the recorded %d", rec.PGID)
	}
	cwd, err := platform.ProcessWorkingDir(rec.PID)
	if err != nil {
		return err.Error()
	}
	if canonicalDir(cwd) != canonicalDir(rec.Dir) {
		return fmt.Sprintf("working directory %q is not %q", cwd, rec.Dir)
	}
	argv, err := platform.ProcessArgs(rec.PID)
	if err != nil {
		return err.Error()
	}
	recorded, err := shellwords.Parse(rec.Command)
	if err != nil || len(recorded) == 0 {
		return fmt.Sprintf("recorded command %q cannot be parsed", rec.Command)
	}
	if !argvRunsCommand(argv, recorded) {
		return fmt.Sprintf("command line %q does not run %q", argv, rec.Command)
	}
	return ""
}

// argvRunsCommand compares the recorded arguments rather than the executable
// when there are any, because interpreters rewrite argv[0] (Homebrew python3
// reports its framework binary) while keeping the script and its arguments.
// A shebang script shows up as "interpreter script", so the recorded tokens
// are matched as the live argv's tail, and a lone recorded token as its last
// element. The recorded command is the unexpanded config text and the
// service's environment is gone, so a token referencing a variable matches
// any argument. The process group and working directory checks carry the
// rest of the proof.
func argvRunsCommand(argv, recorded []string) bool {
	if len(recorded) > 1 {
		tail := recorded[1:]
		if len(argv) <= len(tail) {
			return false
		}
		live := argv[len(argv)-len(tail):]
		for i, token := range tail {
			if !strings.Contains(token, "$") && live[i] != token {
				return false
			}
		}
		return true
	}
	return len(argv) > 0 && filepath.Base(argv[len(argv)-1]) == filepath.Base(recorded[0])
}

func canonicalDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Clean(dir)
}
