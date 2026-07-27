package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/envsync"
)

// setTestDistribution swaps the package distribution for one test.
func setTestDistribution(t *testing.T, d extension.Distribution) {
	t.Helper()
	prev := distribution
	distribution = d
	t.Cleanup(func() { distribution = prev })
}

// Pure helper test: resolveEnvRepoURL picks flag > settings > default.
func TestResolveEnvRepoURL(t *testing.T) {
	setTestDistribution(t, extension.Distribution{EnvRepoURL: "http://dist/default.git"})
	tests := []struct {
		name    string
		flag    string
		setting string
		want    string
	}{
		{"flag wins", "http://a/x.git", "http://b/y.git", "http://a/x.git"},
		{"setting fills in", "", "http://b/y.git", "http://b/y.git"},
		{"default fallback", "", "", "http://dist/default.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure no ambient env var interferes.
			t.Setenv("ORBIT_ENV_REPO_URL", "")
			got := resolveEnvRepoURL(tt.flag, tt.setting)
			if got != tt.want {
				t.Errorf("resolveEnvRepoURL(%q,%q) = %q, want %q", tt.flag, tt.setting, got, tt.want)
			}
		})
	}
}

func TestResolveEnvRepoURL_PrefersFlag(t *testing.T) {
	t.Setenv("ORBIT_ENV_REPO_URL", "env-url")
	got := resolveEnvRepoURL("flag-url", "setting-url")
	if got != "flag-url" {
		t.Errorf("got %q, want flag-url", got)
	}
}

func TestResolveEnvRepoURL_PrefersSettingOverEnv(t *testing.T) {
	t.Setenv("ORBIT_ENV_REPO_URL", "env-url")
	got := resolveEnvRepoURL("", "setting-url")
	if got != "setting-url" {
		t.Errorf("got %q, want setting-url", got)
	}
}

func TestResolveEnvRepoURL_FallsBackToEnvVar(t *testing.T) {
	t.Setenv("ORBIT_ENV_REPO_URL", "env-url")
	got := resolveEnvRepoURL("", "")
	if got != "env-url" {
		t.Errorf("got %q, want env-url", got)
	}
}

func TestResolveEnvRepoURL_DistributionDefault(t *testing.T) {
	setTestDistribution(t, extension.Distribution{EnvRepoURL: "http://dist/default.git"})
	t.Setenv("ORBIT_ENV_REPO_URL", "")
	got := resolveEnvRepoURL("", "")
	if got != "http://dist/default.git" {
		t.Errorf("got %q, want distribution default", got)
	}
}

// An unbranded build (no distribution) resolves to "" — the sync
// command reports the missing configuration instead of cloning.
func TestResolveEnvRepoURL_Unbranded(t *testing.T) {
	setTestDistribution(t, extension.Distribution{})
	t.Setenv("ORBIT_ENV_REPO_URL", "")
	if got := resolveEnvRepoURL("", ""); got != "" {
		t.Errorf("got %q, want empty for unbranded build", got)
	}
}

// Ensures the envs destination is ~/.orbit/envs (via OrbitDir()).
func TestEnvsDestDir_UsesOrbitHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	got := envsDestDir()
	want := filepath.Join(home, "envs")
	if got != want {
		t.Errorf("envsDestDir() = %q, want %q", got, want)
	}
	// Call must not create the dir itself — caller (sync/init) decides.
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("envsDestDir should not create the dir yet; stat err=%v", err)
	}
}

func TestEnvRepoSyncErrorIdentifiesPrivateRepoRemedy(t *testing.T) {
	source := &envsync.CloneError{
		URL: "https://github.com/example/private-env.git",
		Err: errors.New("exit status 128"),
	}
	err := envRepoSyncError(source)
	if !errors.Is(err, cli.ErrEnvRepoAccess) {
		t.Fatalf("error = %v, want ErrEnvRepoAccess", err)
	}
	for _, evidence := range []string{
		source.URL,
		"gh auth login",
		"gh auth setup-git",
		"orbit env sync",
	} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error missing %q: %v", evidence, err)
		}
	}
}
