package daemon

import (
	"os"
	"path/filepath"

	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
)

func loadEnvironmentSourceRegistry() (*envsource.Registry, error) {
	legacyEnvs := filepath.Join(OrbitDir(), "envs")
	settings := LoadSettings(DefaultSettingsPath())
	provenance, _ := envsync.ReadRepositorySource(legacyEnvs)
	url := settings.Get("env_repo_url")
	if url == "" {
		url = provenance.URL
	}
	if url == "" {
		url = os.Getenv("ORBIT_ENV_REPO_URL")
	}
	ref := settings.Get("env_repo_ref")
	if ref == "" {
		ref = provenance.Ref
	}
	return envsource.LoadMigratingLegacy(OrbitDir(), envsource.LegacyMigration{
		URL: url, Ref: ref, Workspace: settings.Get("workspace_root"), EnvsDir: legacyEnvs,
		Selection: ReadCurrentEnv(), SelectionFile: CurrentEnvPath(), Clear: settings.ClearLegacyEnvironmentSettings,
	})
}
