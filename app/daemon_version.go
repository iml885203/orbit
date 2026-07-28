package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
)

func currentDaemonVersion(client *daemon.Client) (*daemon.VersionResponse, error) {
	version, err := client.Version()
	if err != nil {
		return nil, err
	}
	executable, _ := os.Executable()
	return mergeInvokedOrbitVersion(version, buildVersion(), executable), nil
}

func mergeInvokedOrbitVersion(version *daemon.VersionResponse, invoked, executable string) *daemon.VersionResponse {
	if version == nil {
		return version
	}
	out := *version
	out.OnDisk = ""
	out.OnDiskPath = ""
	out.UpdateAvailable = false
	if !daemonsrv.IsNewerBuild(invoked, version.Running) {
		return &out
	}
	out.OnDisk = invoked
	out.OnDiskPath = executable
	out.UpdateAvailable = true
	return &out
}

func orbitRestartCommand(jsonOutput bool) string {
	command := orbitCommandPrefix()
	command += " daemon restart"
	if jsonOutput {
		command += " --json"
	}
	return command
}

func orbitCommandPrefix() string {
	executable, err := os.Executable()
	if err != nil || filepath.Base(executable) != "orbit" {
		return "orbit"
	}
	winner, lookErr := exec.LookPath("orbit")
	if lookErr == nil && sameExecutable(executable, winner) {
		return "orbit"
	}
	if runtime.GOOS == "windows" {
		return "& " + strconv.Quote(executable)
	}
	return strconv.Quote(executable)
}

func sameExecutable(left, right string) bool {
	left = resolvedExecutablePath(left)
	right = resolvedExecutablePath(right)
	return left == right
}

func resolvedExecutablePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func checkCurrentDaemonVersion(version *daemon.VersionResponse) error {
	err := daemon.CheckDaemonCurrent(version)
	var update *daemon.UpdateRequiredError
	if !errors.As(err, &update) {
		return err
	}
	update.RestartCommand = orbitRestartCommand(false)
	update.RestartJSONCommand = orbitRestartCommand(true)
	return update
}
