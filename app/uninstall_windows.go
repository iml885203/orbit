//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const windowsUninstallHelper = `param(
    [Parameter(Mandatory=$true)][int]$OrbitParentPID,
    [Parameter(Mandatory=$true)][string]$OrbitManifest
)
$OrbitPaths = @(Get-Content -LiteralPath $OrbitManifest -Raw | ConvertFrom-Json)
Wait-Process -Id $OrbitParentPID -ErrorAction SilentlyContinue
$OrbitFailures = @()
foreach ($OrbitPath in $OrbitPaths) {
    $OrbitLastError = ""
    for ($OrbitAttempt = 0; $OrbitAttempt -lt 100 -and (Test-Path -LiteralPath $OrbitPath); $OrbitAttempt++) {
        try {
            Remove-Item -LiteralPath $OrbitPath -Recurse -Force -ErrorAction Stop
        } catch {
            $OrbitLastError = $_.Exception.Message
        }
        if (Test-Path -LiteralPath $OrbitPath) {
            Start-Sleep -Milliseconds 100
        }
    }
    if (Test-Path -LiteralPath $OrbitPath) {
        $OrbitFailures += "$OrbitPath :: $OrbitLastError"
    }
}
if ($OrbitFailures.Count -gt 0) {
    Set-Content -LiteralPath "$PSCommandPath.failed" -Value $OrbitFailures
    exit 1
}
Remove-Item -LiteralPath $OrbitManifest -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
`

func removeUninstallArtifacts(paths []string) (bool, error) {
	manifest, err := os.CreateTemp("", "orbit-uninstall-*.json")
	if err != nil {
		return false, fmt.Errorf("create Windows uninstall manifest: %w", err)
	}
	manifestPath := manifest.Name()
	if err := json.NewEncoder(manifest).Encode(paths); err != nil {
		_ = manifest.Close()
		_ = os.Remove(manifestPath)
		return false, fmt.Errorf("write Windows uninstall manifest: %w", err)
	}
	if err := manifest.Close(); err != nil {
		_ = os.Remove(manifestPath)
		return false, fmt.Errorf("close Windows uninstall manifest: %w", err)
	}

	helper, err := os.CreateTemp("", "orbit-uninstall-*.ps1")
	if err != nil {
		_ = os.Remove(manifestPath)
		return false, fmt.Errorf("create Windows uninstall helper: %w", err)
	}
	helperPath := helper.Name()
	if _, err := helper.WriteString(windowsUninstallHelper); err != nil {
		_ = helper.Close()
		_ = os.Remove(helperPath)
		_ = os.Remove(manifestPath)
		return false, fmt.Errorf("write Windows uninstall helper: %w", err)
	}
	if err := helper.Close(); err != nil {
		_ = os.Remove(helperPath)
		_ = os.Remove(manifestPath)
		return false, fmt.Errorf("close Windows uninstall helper: %w", err)
	}

	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", helperPath,
		"-OrbitParentPID", strconv.Itoa(os.Getpid()),
		"-OrbitManifest", manifestPath,
	}
	cmd := exec.Command("powershell.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200,
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(helperPath)
		_ = os.Remove(manifestPath)
		return false, fmt.Errorf("start Windows uninstall helper: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return false, fmt.Errorf("release Windows uninstall helper: %w", err)
	}
	return true, nil
}
