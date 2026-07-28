package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightExplicitConfigDoesNotRequireSyncedEnvironments(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	dir := t.TempDir()
	envPath := filepath.Join(dir, "orbit.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousConfig := configFile
	configFile = envPath
	t.Cleanup(func() { configFile = previousConfig })

	if err := preflightOrAbort(true, nil); err != nil {
		t.Fatalf("explicit config blocked by env repository readiness: %v", err)
	}
}

func TestPreflightSelectedEnvironmentStillRequiresInitialization(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	if err := preflightOrAbort(false, nil); err == nil {
		t.Fatal("missing env repository accepted without an explicit config")
	}
}
