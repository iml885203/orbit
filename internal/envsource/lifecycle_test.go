package envsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshUsesCredentialedTransportURLButReturnsRedactedMetadata(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GIT_LOG"
case "$1" in
  clone)
    for arg do destination="$arg"; done
    mkdir -p "$destination/envs"
    printf 'version: "3"\n' > "$destination/envs/dev.yaml"
    ;;
  rev-parse) printf '0123456789abcdef\n' ;;
  symbolic-ref) printf 'main\n' ;;
esac
`
	gitPath := filepath.Join(bin, "git")
	if err := os.WriteFile(gitPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GIT_LOG", logPath)
	orbitHome := t.TempDir()
	registry, err := Load(RegistryPath(orbitHome))
	if err != nil {
		t.Fatal(err)
	}
	rawURL := "https://user:secret@example.com/envs.git"
	updated, _, err := Refresh(registry, Source{Name: "team", Type: TypeGit, URL: rawURL}, orbitHome, false, false)
	if err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), rawURL) {
		t.Fatalf("git did not receive credentialed transport URL: %s", logData)
	}
	if strings.Contains(updated.URL, "secret") || updated.URL != "https://example.com/envs.git" {
		t.Fatalf("persistable URL was not redacted: %q", updated.URL)
	}
}

func TestRefreshFailurePreservesStoredLocationAndRollsBackActivatedCache(t *testing.T) {
	orbitHome := t.TempDir()
	registryParent := t.TempDir()
	registry, err := Load(filepath.Join(registryParent, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "envs"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(local, "envs", "dev.yaml")
	if err := os.WriteFile(configPath, []byte("version: \"3\"\n# old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := Source{Name: "team", Type: TypeLocal, Path: local}
	refreshed, _, err := Refresh(registry, source, orbitHome, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(refreshed, true); err != nil {
		t.Fatal(err)
	}

	bad := refreshed
	bad.Path = filepath.Join(t.TempDir(), "missing")
	if _, _, err := Refresh(registry, bad, orbitHome, false, true); err == nil {
		t.Fatal("invalid update succeeded")
	}
	stored, err := registry.Get(source.Name)
	if err != nil || stored.Path != local || stored.LastSyncError == "" {
		t.Fatalf("failed update changed stored source: %+v, %v", stored, err)
	}

	if err := os.WriteFile(configPath, []byte("version: \"3\"\n# new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(registryParent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryParent, []byte("block registry"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Refresh(registry, stored, orbitHome, false, true); err == nil {
		t.Fatal("refresh succeeded despite unavailable registry")
	}
	active, err := os.ReadFile(filepath.Join(EnvsDir(orbitHome, source.Name), "dev.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(active), "# new") {
		t.Fatalf("new cache remained active after registry failure: %s", active)
	}
}
