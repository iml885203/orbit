//go:build linux

package platform

import (
	"fmt"
	"os"
	"strconv"
)

// ProcessArgs returns the live argv of pid as the kernel reports it, which
// may differ from what was exec'd when the program rewrites argv[0].
func ProcessArgs(pid int) ([]string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return nil, fmt.Errorf("reading argv of pid %d: %w", pid, err)
	}
	return parseProcCmdline(raw), nil
}

// ProcessWorkingDir returns the current working directory of pid.
func ProcessWorkingDir(pid int) (string, error) {
	dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return "", fmt.Errorf("reading cwd of pid %d: %w", pid, err)
	}
	return dir, nil
}
