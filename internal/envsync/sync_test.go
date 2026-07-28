package envsync

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// writeTree creates files at paths with given content under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestSync_CopiesAllYamlFiles(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{
		"development.yaml":       "version: 1\n",
		"example.yaml":           "version: 1\n",
		"data/kafka-topics.yaml": "topics: []\n",
	})

	res, err := Sync(src, dest, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := readFile(t, filepath.Join(dest, "development.yaml")); got != "version: 1\n" {
		t.Errorf("development content = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "data", "kafka-topics.yaml")); got != "topics: []\n" {
		t.Errorf("data/kafka-topics content = %q", got)
	}
	if len(res.Written) != 3 {
		t.Errorf("Written count = %d, want 3: %v", len(res.Written), res.Written)
	}
}

func TestSync_OverwritesExisting(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{"development.yaml": "version: 2\n"})
	writeTree(t, dest, map[string]string{"development.yaml": "version: 1\n"})

	if _, err := Sync(src, dest, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := readFile(t, filepath.Join(dest, "development.yaml")); got != "version: 2\n" {
		t.Errorf("not overwritten: %q", got)
	}
}

func TestSync_UnchangedFilesAreNotWritten(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	files := map[string]string{
		"development.yaml":  "version: 2\n",
		"seeds/demo/app.py": "print('ready')\n",
	}
	writeTree(t, src, files)
	writeTree(t, dest, files)
	destination := filepath.Join(dest, "development.yaml")
	originalTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(destination, originalTime, originalTime); err != nil {
		t.Fatal(err)
	}

	res, err := Sync(src, dest, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.Written) != 0 {
		t.Fatalf("Written = %v, want no unchanged files", res.Written)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(originalTime) {
		t.Fatalf("unchanged destination was rewritten: modtime = %s", info.ModTime())
	}
}

func TestSync_DryRunReportsOnlyChangedFiles(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{
		"unchanged.yaml": "version: 2\n",
		"changed.yaml":   "version: 2\n",
	})
	writeTree(t, dest, map[string]string{
		"unchanged.yaml": "version: 2\n",
		"changed.yaml":   "version: 1\n",
	})

	res, err := Sync(src, dest, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.Written) != 1 || res.Written[0] != "changed.yaml" {
		t.Fatalf("Written = %v, want changed.yaml", res.Written)
	}
	if got := readFile(t, filepath.Join(dest, "changed.yaml")); got != "version: 1\n" {
		t.Fatalf("dry run changed destination: %q", got)
	}
}

func TestSync_SkipsNonYaml(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{
		"development.yaml": "version: 1\n",
		"README.md":        "hello",
		"script.sh":        "#!/bin/sh",
	})

	res, err := Sync(src, dest, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for _, unwanted := range []string{"README.md", "script.sh"} {
		if _, err := os.Stat(filepath.Join(dest, unwanted)); !os.IsNotExist(err) {
			t.Errorf("%s should not be copied", unwanted)
		}
	}
	if len(res.Written) != 1 {
		t.Errorf("Written = %v, want only development.yaml", res.Written)
	}
}

func TestSync_CopiesSeedScripts(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{
		"development.yaml":             "version: 1\n",
		"seeds/sql-server/01-init.sql": "SELECT 1;\n",
		"seeds/mongodb/01-data.js":     "db.x.insertOne({})\n",
		"README.md":                    "hello", // non-yaml, outside seeds/ → skipped
	})

	res, err := Sync(src, dest, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Seed scripts (non-yaml, under seeds/) must ride along — the envs
	// reference them, so a sync without them leaves `orbit seed` broken.
	for _, want := range []string{"seeds/sql-server/01-init.sql", "seeds/mongodb/01-data.js"} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("seed script %s not copied: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(err) {
		t.Error("README.md (non-yaml, outside seeds/) should not be copied")
	}
	if len(res.Written) != 3 {
		t.Errorf("Written = %v, want development.yaml + 2 seed scripts", res.Written)
	}
}

func TestSync_PreservesSubdirStructure(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{
		"data/kafka-topics.yaml":      "topics: []\n",
		"seeds/platform/01-init.yaml": "init: true\n",
	})

	if _, err := Sync(src, dest, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for _, want := range []string{"data/kafka-topics.yaml", "seeds/platform/01-init.yaml"} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestSync_ReturnsChangedFilesSorted(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{
		"zebra.yaml":    "v: 1\n",
		"alpha.yaml":    "v: 1\n",
		"data/mid.yaml": "v: 1\n",
	})

	res, err := Sync(src, dest, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got := append([]string{}, res.Written...)
	sort.Strings(got)
	want := []string{"alpha.yaml", "data/mid.yaml", "zebra.yaml"}
	if len(got) != len(want) {
		t.Fatalf("Written = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Written[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	writeTree(t, src, map[string]string{"development.yaml": "version: 1\n"})

	res, err := Sync(src, dest, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "development.yaml")); !os.IsNotExist(err) {
		t.Error("DryRun should not write files")
	}
	if len(res.Written) != 1 {
		t.Errorf("Written = %v, want 1 (dry run still reports what would be written)", res.Written)
	}
}

func TestSync_EmptySrcIsEmptyResult(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	res, err := Sync(src, dest, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.Written) != 0 {
		t.Errorf("Written = %v, want empty", res.Written)
	}
}
