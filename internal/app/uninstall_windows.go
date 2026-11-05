//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const windowsUninstallHelper = `param(
    [Parameter(Mandatory=$true)][string]$OrbitManifest
)
$OrbitPaths = Get-Content -LiteralPath $OrbitManifest -Raw | ConvertFrom-Json
$OrbitFailures = @()
foreach ($OrbitPath in $OrbitPaths) {
    $OrbitLastError = ""
    $OrbitRemoved = -not (Test-Path -LiteralPath $OrbitPath)
    for ($OrbitAttempt = 0; $OrbitAttempt -lt 100 -and -not $OrbitRemoved; $OrbitAttempt++) {
        try {
            Remove-Item -LiteralPath $OrbitPath -Recurse -Force -ErrorAction Stop
            $OrbitRemoved = $true
        } catch {
            $OrbitLastError = $_.Exception.Message
            Start-Sleep -Milliseconds 100
        }
    }
    if (-not $OrbitRemoved) {
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
		"-OrbitManifest", manifestPath,
	}
	cmd := exec.Command("powershell.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
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
