package preflight

import (
	"os"
	"path/filepath"
	"testing"
)

func findByName(checks []Check, name string) *Check {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func TestCheckEnvsReady_NoDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	checks := CheckEnvsReady(missing, "")
	c := findByName(checks, "Envs directory")
	if c == nil || c.OK {
		t.Fatalf("expected Envs directory FAIL, got %+v", c)
	}
	if c.Fix == "" {
		t.Error("missing directory check should provide a Fix hint")
	}
}

func TestCheckEnvsReady_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	checks := CheckEnvsReady(dir, "")
	c := findByName(checks, "Env configs")
	if c == nil || c.OK {
		t.Fatalf("empty dir should FAIL Env configs, got %+v", c)
	}
}

func TestCheckEnvsReady_HasYamls(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "development.yaml"), []byte("version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	checks := CheckEnvsReady(dir, "")
	c := findByName(checks, "Env configs")
	if c == nil || !c.OK {
		t.Fatalf("should PASS Env configs, got %+v", c)
	}
}

func TestCheckEnvsReady_InvalidActiveEnv(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "development.yaml"), []byte("version: 1\n"), 0644)
	checks := CheckEnvsReady(dir, filepath.Join(dir, "nope.yaml"))
	c := findByName(checks, "Active env")
	if c == nil || c.OK {
		t.Fatalf("pointing to missing file should FAIL Active env, got %+v", c)
	}
}

func TestCheckEnvsReady_ValidActiveEnv(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "development.yaml")
	_ = os.WriteFile(active, []byte("version: 1\n"), 0644)
	checks := CheckEnvsReady(dir, active)
	c := findByName(checks, "Active env")
	if c == nil || !c.OK {
		t.Fatalf("should PASS Active env, got %+v", c)
	}
}

func TestCheckEnvsReady_AllOK(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "development.yaml")
	_ = os.WriteFile(active, []byte("version: 1\n"), 0644)
	checks := CheckEnvsReady(dir, active)
	for _, c := range checks {
		if !c.OK {
			t.Errorf("check %q failed: %s", c.Name, c.Message)
		}
	}
}
