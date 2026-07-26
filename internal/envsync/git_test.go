package envsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeLocalRepo creates a bare-ish git repo at a tmp dir and returns the
// file:// URL. Works on any machine with git installed.
func makeLocalRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	return "file://" + dir
}

func TestClone_ShallowCheckout(t *testing.T) {
	url := makeLocalRepo(t, map[string]string{
		"envs/development.yaml": "version: 1\n",
		"README.md":             "hi",
	})
	dest := t.TempDir()

	if err := Clone(url, dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Normalize line endings: a Windows checkout with core.autocrlf=true
	// rewrites "\n" to "\r\n", which is harmless for the YAML orbit syncs.
	if got := readFile(t, filepath.Join(dest, "envs", "development.yaml")); strings.ReplaceAll(got, "\r\n", "\n") != "version: 1\n" {
		t.Errorf("development.yaml = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "README.md")); got != "hi" {
		t.Errorf("README.md = %q", got)
	}
}

func TestClone_NonExistentFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dest := t.TempDir()
	err := Clone("file:///nonexistent/repo", dest)
	if err == nil {
		t.Error("expected error for non-existent repo, got nil")
	}
}
