package envsync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncFromRepo_E2E(t *testing.T) {
	url := makeLocalRepo(t, map[string]string{
		"envs/development.yaml":       "version: 1\n",
		"envs/example.yaml":           "version: 1\n",
		"envs/data/kafka-topics.yaml": "topics: []\n",
		"envs/README.md":              "ignored",
		"other-file.txt":              "ignored too",
	})
	dest := t.TempDir()

	res, err := SyncFromRepo(url, dest, Options{})
	if err != nil {
		t.Fatalf("SyncFromRepo: %v", err)
	}

	for _, want := range []string{"development.yaml", "example.yaml", "data/kafka-topics.yaml"} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	if len(res.Written) != 3 {
		t.Errorf("Written count = %d, want 3: %v", len(res.Written), res.Written)
	}
	// README.md at top of envs/ must NOT be written.
	if _, err := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(err) {
		t.Error("README.md should not be synced")
	}
}

func TestSyncFromRepo_MissingEnvsDir(t *testing.T) {
	url := makeLocalRepo(t, map[string]string{
		"other-file.yaml": "version: 1\n",
	})
	dest := t.TempDir()

	_, err := SyncFromRepo(url, dest, Options{})
	if err == nil {
		t.Fatal("expected error when repo has no envs/ dir")
	}
}
