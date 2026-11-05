//go:build darwin

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ProcessArgs returns the live argv of pid as the kernel reports it, which
// may differ from what was exec'd when the program rewrites argv[0].
func ProcessArgs(pid int) ([]string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, fmt.Errorf("reading argv of pid %d: %w", pid, err)
	}
	return parseProcArgs2(raw)
}

// ProcessWorkingDir returns the current working directory of pid.
func ProcessWorkingDir(pid int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sysutilExecTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return "", fmt.Errorf("reading cwd of pid %d: %w", pid, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:], nil
		}
	}
	return "", fmt.Errorf("reading cwd of pid %d: lsof reported none", pid)
}
