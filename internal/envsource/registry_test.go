package envsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryMaintainsExactlyOneExplicitDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	registry, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(Source{Name: "team", Type: TypeGit, URL: "https://example.com/team.git"}, false); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(Source{Name: "local", Type: TypeLocal, Path: "/work/envs"}, false); err != nil {
		t.Fatal(err)
	}

	first, err := registry.Default()
	if err != nil || first.Name != "team" {
		t.Fatalf("default = %#v, %v", first, err)
	}
	if err := registry.SetDefault("local"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Default()
	if err != nil || got.Name != "local" {
		t.Fatalf("reloaded default = %#v, %v", got, err)
	}
	if listed := reloaded.List(); len(listed) != 2 || listed[0].Name != "local" || !listed[0].Default {
		t.Fatalf("ordered sources = %#v", listed)
	}
}

func TestRemoveOwnedRollsBackCacheAndSelectionWhenRegistryCommitFails(t *testing.T) {
	orbitHome := t.TempDir()
	registryParent := t.TempDir()
	registryPath := filepath.Join(registryParent, "sources.json")
	registry, err := Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{Name: "team", Type: TypeLocal, Path: t.TempDir()}
	if err := registry.Add(source, true); err != nil {
		t.Fatal(err)
	}
	cache := SourceDir(orbitHome, source.Name)
	if err := os.MkdirAll(EnvsDir(orbitHome, source.Name), 0755); err != nil {
		t.Fatal(err)
	}
	selection := filepath.Join(EnvsDir(orbitHome, source.Name), "dev.yaml")
	if err := os.WriteFile(selection, []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	selectionFile := filepath.Join(orbitHome, "current")
	if err := os.WriteFile(selectionFile, []byte(selection+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(registryParent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryParent, []byte("blocks registry parent"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveOwned(registry, orbitHome, source.Name, selectionFile, selection); err == nil {
		t.Fatal("RemoveOwned succeeded despite unavailable registry path")
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("cache was not restored: %v", err)
	}
	data, err := os.ReadFile(selectionFile)
	if err != nil || string(data) != selection+"\n" {
		t.Fatalf("selection was not restored: %q, %v", data, err)
	}
	if _, err := registry.Get(source.Name); err != nil {
		t.Fatalf("registry memory was not restored: %v", err)
	}
}

func TestRegistryRejectsNamesThatCouldEscapeOwnedStorage(t *testing.T) {
	registry, err := Load(filepath.Join(t.TempDir(), "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../team", ".", "..", "team/dev", `team\dev`, ""} {
		err := registry.Add(Source{Name: name, Type: TypeLocal, Path: "/work/envs"}, false)
		if err == nil {
			t.Fatalf("Add(%q) succeeded", name)
		}
	}
}

func TestRegistryRemovalNeverGuessesAnotherDefault(t *testing.T) {
	registry, err := Load(filepath.Join(t.TempDir(), "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(Source{Name: "one", Type: TypeLocal, Path: "/one"}, false); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(Source{Name: "two", Type: TypeLocal, Path: "/two"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Remove("one"); err == nil {
		t.Fatal("removing the default with another source should preserve the registry invariant")
	}
	if _, err := registry.Remove("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing removal error = %v", err)
	}
}

func TestManagedIdentityIsQualifiedAndPathDistinct(t *testing.T) {
	if got := Identity("company", "e2e.yaml"); got != "company/e2e" {
		t.Fatalf("identity = %q", got)
	}
	source, environment, err := ParseIdentity("company/e2e")
	if err != nil || source != "company" || environment != "e2e" {
		t.Fatalf("ParseIdentity = %q, %q, %v", source, environment, err)
	}
	for _, input := range []string{"/tmp/e2e.yaml", `C:\\tmp\\e2e.yaml`, "e2e", "company/team/e2e"} {
		if _, _, err := ParseIdentity(input); err == nil {
			t.Fatalf("ParseIdentity(%q) succeeded", input)
		}
	}
}
