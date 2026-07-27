//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const windowsUninstallHelper = `param(
    [Parameter(Mandatory=$true)][int]$OrbitParentPID,
    [Parameter(ValueFromRemainingArguments=$true)][string[]]$OrbitPaths
)
Wait-Process -Id $OrbitParentPID -ErrorAction SilentlyContinue
foreach ($OrbitPath in $OrbitPaths) {
    Remove-Item -LiteralPath $OrbitPath -Recurse -Force -ErrorAction SilentlyContinue
}
Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
`

func removeUninstallArtifacts(paths []string) (bool, error) {
	helper, err := os.CreateTemp("", "orbit-uninstall-*.ps1")
	if err != nil {
		return false, fmt.Errorf("create Windows uninstall helper: %w", err)
	}
	helperPath := helper.Name()
	if _, err := helper.WriteString(windowsUninstallHelper); err != nil {
		_ = helper.Close()
		_ = os.Remove(helperPath)
		return false, fmt.Errorf("write Windows uninstall helper: %w", err)
	}
	if err := helper.Close(); err != nil {
		_ = os.Remove(helperPath)
		return false, fmt.Errorf("close Windows uninstall helper: %w", err)
	}

	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", helperPath, strconv.Itoa(os.Getpid())}
	args = append(args, paths...)
	cmd := exec.Command("powershell.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200,
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(helperPath)
		return false, fmt.Errorf("start Windows uninstall helper: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return false, fmt.Errorf("release Windows uninstall helper: %w", err)
	}
	return true, nil
}
