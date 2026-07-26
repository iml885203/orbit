package main

import "testing"

func TestOfficialDistributionDefaults(t *testing.T) {
	extensions := Extensions()
	if len(extensions) != 1 || extensions[0].Distribution == nil {
		t.Fatal("official distribution is not configured")
	}

	distribution := extensions[0].Distribution
	if distribution.EnvRepoURL != "https://github.com/iml885203/orbit-demo.git" {
		t.Errorf("env repo URL = %q", distribution.EnvRepoURL)
	}
	if distribution.DefaultEnv != "quickstart.yaml" {
		t.Errorf("default env = %q", distribution.DefaultEnv)
	}
	if distribution.InstallURL != "https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh" {
		t.Errorf("install URL = %q", distribution.InstallURL)
	}
}
