package main

import (
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/devdb"
	"github.com/iml885203/orbit/internal/tunnel"
)

func TestOfficialDistributionDefaults(t *testing.T) {
	extensions := Extensions()
	if len(extensions) != 1 || extensions[0].Distribution == nil {
		t.Fatal("official distribution is not configured")
	}
	if extensions[0].CLIInit != nil {
		t.Fatal("optional feature settings must not appear in the general init flow")
	}

	distribution := extensions[0].Distribution
	if distribution.EnvRepoURL != "https://github.com/iml885203/orbit-demo.git" {
		t.Errorf("env repo URL = %q", distribution.EnvRepoURL)
	}
	if distribution.EnvRepoRef != "v0.0.40" {
		t.Errorf("env repo ref = %q", distribution.EnvRepoRef)
	}
	if distribution.DefaultEnv != "quickstart.yaml" {
		t.Errorf("default env = %q", distribution.DefaultEnv)
	}
	if distribution.InstallURL != "https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh" {
		t.Errorf("install URL = %q", distribution.InstallURL)
	}
}

func TestOfficialOptionalCommandsFollowSelectedEnvironment(t *testing.T) {
	visibility := Extensions()[0].CommandVisibility
	if visibility == nil {
		t.Fatal("official command visibility is not configured")
	}
	if got := visibility(nil); got["db"] || got["tunnel"] {
		t.Fatalf("optional commands visible without environment: %v", got)
	}

	cfg := (&config.Config{}).
		WithExtension("sqlserver", &devdb.SQLServerConfig{}).
		WithExtension("claim", &tunnel.ClaimConfig{})
	got := visibility(cfg)
	if !got["db"] || !got["tunnel"] {
		t.Fatalf("configured optional commands hidden: %v", got)
	}
}
