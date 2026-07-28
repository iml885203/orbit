package app

import (
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestMergeInvokedOrbitVersionDetectsCurrentCLIUpdate(t *testing.T) {
	got := mergeInvokedOrbitVersion(
		&daemon.VersionResponse{Running: "v0.0.1 (2026-07-01T00:00:00Z)"},
		"v0.0.2 (2026-07-02T00:00:00Z)",
		"/opt/orbit",
	)
	if !got.UpdateAvailable ||
		got.OnDisk != "v0.0.2 (2026-07-02T00:00:00Z)" ||
		got.OnDiskPath != "/opt/orbit" {
		t.Fatalf("version = %+v", got)
	}
}

func TestMergeInvokedOrbitVersionIgnoresAnotherInstallation(t *testing.T) {
	version := &daemon.VersionResponse{
		Running:         "v0.0.1",
		OnDisk:          "v0.0.3",
		OnDiskPath:      "/active/orbit",
		UpdateAvailable: true,
	}
	got := mergeInvokedOrbitVersion(version, "v0.0.1", "/other/orbit")
	if got == version || got.UpdateAvailable || got.OnDisk != "" || got.OnDiskPath != "" {
		t.Fatalf("unrelated installation remained actionable: got %+v", got)
	}
}

func TestMergeInvokedOrbitVersionDoesNotRecommendDowngrade(t *testing.T) {
	version := &daemon.VersionResponse{Running: "v0.0.2 (2026-07-02T00:00:00Z)"}
	got := mergeInvokedOrbitVersion(
		version,
		"v0.0.1 (2026-07-01T00:00:00Z)",
		"/old/orbit",
	)
	if got == nil || got.UpdateAvailable || got.OnDisk != "" || got.OnDiskPath != "" {
		t.Fatalf("older invoked binary treated as update: %+v", got)
	}
}
