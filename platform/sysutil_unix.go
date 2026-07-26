//go:build !windows

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const sysutilExecTimeout = 3 * time.Second

// runWithTimeout uses a fresh context per call; callers that need to share a
// budget across multiple execs must build their own ctx.
func runWithTimeout(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sysutilExecTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// FindPortOwner returns the PID and process name holding the given port.
// Returns "?", "?" if the owner cannot be determined. Both the port-lookup
// and the ps name-lookup share a single sysutilExecTimeout budget so the
// total wall time cannot exceed it regardless of which sub-step stalls.
func FindPortOwner(port int) (pid string, process string) {
	ctx, cancel := context.WithTimeout(context.Background(), sysutilExecTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "lsof", "-ti", fmt.Sprintf(":%d", port))
	case "linux":
		cmd = exec.CommandContext(ctx, "fuser", fmt.Sprintf("%d/tcp", port))
	default:
		return "?", "?"
	}

	out, err := cmd.Output()
	if err != nil {
		return "?", "?"
	}

	pid = strings.TrimSpace(string(out))
	if pid == "" {
		return "?", "?"
	}

	nameOut, err := exec.CommandContext(ctx, "ps", "-p", strings.Split(pid, "\n")[0], "-o", "comm=").Output()
	if err != nil {
		return pid, "?"
	}

	return pid, strings.TrimSpace(string(nameOut))
}

// FindPortHolderPIDs returns PIDs holding the given port in LISTEN state.
func FindPortHolderPIDs(port int) []int {
	var out []byte
	var err error

	switch runtime.GOOS {
	case "darwin":
		out, err = runWithTimeout("lsof", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN")
	default: // linux
		out, err = runWithTimeout("ss", "-tlnp", fmt.Sprintf("sport = :%d", port))
	}
	if err != nil || len(out) == 0 {
		return nil
	}

	var pids []int
	if runtime.GOOS == "darwin" {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || pid <= 1 {
				continue
			}
			pids = append(pids, pid)
		}
	} else {
		// ss output: parse "pid=NNN" from the process column
		for _, line := range strings.Split(string(out), "\n") {
			if idx := strings.Index(line, "pid="); idx >= 0 {
				s := line[idx+4:]
				if end := strings.IndexAny(s, ",) "); end > 0 {
					s = s[:end]
				}
				pid, err := strconv.Atoi(s)
				if err != nil || pid <= 1 {
					continue
				}
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// KillProcess forcefully kills the process with the given PID string.
func KillProcess(pid string) error {
	_, err := runWithTimeout("kill", "-9", pid)
	return err
}

// OpenBrowser opens the given URL in the default browser. No timeout — the
// helper forks the browser and we must not kill it after spawning.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
