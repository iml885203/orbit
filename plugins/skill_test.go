package plugins

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Guards the embed pattern and the sub-root: a moved skill directory or a
// wrong `fs.Sub` path would still compile, and the installed copy would then
// be empty or rooted one level off.
func TestEmbeddedSkillMatchesPluginDirectory(t *testing.T) {
	source := filepath.Join("orbit", "skills", "orbit")
	embedded := map[string][]byte{}
	if err := fs.WalkDir(OrbitSkill(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(OrbitSkill(), path)
		if readErr != nil {
			return readErr
		}
		embedded[path] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	onDisk := map[string][]byte{}
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		onDisk[filepath.ToSlash(rel)] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := embedded["SKILL.md"]; !ok {
		t.Fatal("embedded skill has no SKILL.md at its root")
	}
	if len(embedded) != len(onDisk) {
		t.Fatalf("embedded %d files, plugin directory has %d", len(embedded), len(onDisk))
	}
	for path, want := range onDisk {
		if got, ok := embedded[path]; !ok || !bytes.Equal(got, want) {
			t.Errorf("embedded %s differs from the plugin directory", path)
		}
	}
}
