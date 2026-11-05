//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FindPortOwner returns the PID and process name holding the given port.
// Uses netstat + tasklist on Windows.
func FindPortOwner(port int) (pid string, process string) {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return "?", "?"
	}

	suffix := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "LISTENING") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Match exact port at end of local address (e.g. "0.0.0.0:5056")
		if !strings.HasSuffix(fields[1], suffix) {
			continue
		}
		pid = fields[4]
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		process = findProcessName(pid)
		return pid, process
	}

	return "?", "?"
}

// FindPortHolderPIDs returns PIDs holding the given port in LISTEN state.
func FindPortHolderPIDs(port int) []int {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return nil
	}

	suffix := fmt.Sprintf(":%d", port)
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "LISTENING") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.HasSuffix(fields[1], suffix) {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil || pid <= 1 {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func findProcessName(pid string) string {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %s", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return "?"
	}
	// Output: "process.exe","1234","Console","1","12,345 K"
	line := strings.TrimSpace(string(out))
	if strings.HasPrefix(line, "\"") {
		parts := strings.SplitN(line, ",", 2)
		name := strings.Trim(parts[0], "\"")
		return name
	}
	return "?"
}

// OpenBrowser opens the given URL in the default browser.
func OpenBrowser(url string) error {
	return exec.Command("cmd", "/c", "start", url).Start()
}
