package envsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	res, err := SyncFromRepo(url, "", dest, Options{})
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
	if res.Source.URL != url || res.Source.Commit == "" {
		t.Fatalf("Source = %+v, want URL and resolved commit", res.Source)
	}
	stored, err := ReadRepositorySource(dest)
	if err != nil {
		t.Fatal(err)
	}
	if stored != res.Source {
		t.Fatalf("stored source = %+v, want %+v", stored, res.Source)
	}
	// README.md at top of envs/ must NOT be written.
	if _, err := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(err) {
		t.Error("README.md should not be synced")
	}
}

func TestSyncFromRepo_PinnedRefDoesNotFollowMovingDefaultBranch(t *testing.T) {
	url := makeLocalRepo(t, map[string]string{
		"envs/development.yaml": "release: one\n",
	})
	repoDir := strings.TrimPrefix(url, "file://")
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("tag", "release-1")
	if err := os.WriteFile(filepath.Join(repoDir, "envs", "development.yaml"), []byte("release: two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-q", "-m", "move default branch")

	dest := t.TempDir()
	result, err := SyncFromRepo(url, "release-1", dest, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.Ref != "release-1" {
		t.Fatalf("source = %+v", result.Source)
	}
	data, err := os.ReadFile(filepath.Join(dest, "development.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "release: one\n" {
		t.Fatalf("pinned sync followed moving branch: %q", data)
	}
}

func TestSyncFromRepo_MissingEnvsDir(t *testing.T) {
	url := makeLocalRepo(t, map[string]string{
		"other-file.yaml": "version: 1\n",
	})
	dest := t.TempDir()

	_, err := SyncFromRepo(url, "", dest, Options{})
	if err == nil {
		t.Fatal("expected error when repo has no envs/ dir")
	}
}
