package sqlpublish

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallHint is the one-line fix for a missing sqlpackage — surfaced
// verbatim by doctor and CLI errors.
const InstallHint = "dotnet tool install -g microsoft.sqlpackage"

// SqlpackagePath resolves the host sqlpackage binary: PATH first, then
// the dotnet global-tools directory (~/.dotnet/tools), which is where
// `dotnet tool install -g` puts it but which is often not on PATH.
func SqlpackagePath() (string, error) {
	if p, err := exec.LookPath("sqlpackage"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".dotnet", "tools", "sqlpackage")
		if _, statErr := os.Stat(p); statErr == nil {
			return p, nil
		}
		if _, statErr := os.Stat(p + ".exe"); statErr == nil {
			return p + ".exe", nil
		}
	}
	return "", fmt.Errorf("sqlpackage not found on PATH or in ~/.dotnet/tools — install with: %s", InstallHint)
}

// DotnetVersion returns the host dotnet SDK version, or an error when
// the SDK is absent. Used by doctor.
func DotnetVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "dotnet", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("dotnet SDK not found on PATH")
	}
	return strings.TrimSpace(string(out)), nil
}

// SqlpackageVersion returns the host sqlpackage version, or an error
// when the tool is absent. Used by doctor.
func SqlpackageVersion(ctx context.Context) (string, error) {
	p, err := SqlpackagePath()
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, p, "/Version").Output()
	if err != nil {
		return "", fmt.Errorf("sqlpackage found at %s but not runnable: %w", p, err)
	}
	return strings.TrimSpace(string(out)), nil
}
