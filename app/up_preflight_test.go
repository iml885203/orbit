package app

import "testing"

func TestPreflightExplicitConfigDoesNotRequireSyncedEnvironments(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	if err := preflightOrAbort(true); err != nil {
		t.Fatalf("explicit config blocked by env repository readiness: %v", err)
	}
}

func TestPreflightSelectedEnvironmentStillRequiresInitialization(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	if err := preflightOrAbort(false); err == nil {
		t.Fatal("missing env repository accepted without an explicit config")
	}
}
