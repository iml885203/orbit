package envsync

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

type CloneError struct {
	URL    string
	Err    error
	Output string
}

func (e *CloneError) Error() string {
	output := strings.ReplaceAll(e.Output, e.URL, e.DisplayURL())
	return fmt.Sprintf("git clone %s: %v\n%s", e.DisplayURL(), e.Err, output)
}

func (e *CloneError) Unwrap() error {
	return e.Err
}

func (e *CloneError) DisplayURL() string {
	return displayURL(e.URL)
}

func (e *CloneError) IsGitHub() bool {
	parsed, err := url.Parse(e.URL)
	if err == nil && strings.EqualFold(parsed.Hostname(), "github.com") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(e.URL), "git@github.com:")
}

func (e *CloneError) ReportsAmbiguousGitHubAvailability() bool {
	if !e.IsGitHub() {
		return false
	}
	output := strings.ToLower(e.Output)
	return strings.Contains(output, "repository not found") ||
		strings.Contains(output, "repository does not exist") ||
		strings.Contains(output, "could not read username for 'https://github.com")
}

// Clone performs a shallow clone of url into destDir. destDir must be empty
// (or nonexistent — git clone creates it).
func Clone(url, destDir string) error {
	_, err := CloneAt(url, "", destDir)
	return err
}

// CloneAt resolves ref to one detached commit so a branch or tag cannot move
// between fetch and checkout. An empty ref preserves Git's default-branch
// behavior for user-managed repositories that intentionally track it.
func CloneAt(url, ref, destDir string) (string, error) {
	if ref == "" {
		cmd := exec.Command("git", "clone", "--depth", "1", "--quiet", url, destDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", &CloneError{URL: url, Err: err, Output: string(out)}
		}
		return repositoryCommit(url, destDir)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create clone destination: %w", err)
	}
	if out, err := gitIn(destDir, "init", "--quiet"); err != nil {
		return "", &CloneError{URL: url, Err: err, Output: string(out)}
	}
	if out, err := gitIn(destDir, "fetch", "--depth", "1", "--quiet", url, ref); err != nil {
		return "", &CloneError{URL: url, Err: err, Output: string(out)}
	}
	if out, err := gitIn(destDir, "checkout", "--detach", "--quiet", "FETCH_HEAD"); err != nil {
		return "", &CloneError{URL: url, Err: err, Output: string(out)}
	}
	return repositoryCommit(url, destDir)
}

func repositoryCommit(url, dir string) (string, error) {
	out, err := gitIn(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", &CloneError{URL: url, Err: err, Output: string(out)}
	}
	return strings.TrimSpace(string(out)), nil
}

func gitIn(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func displayURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}
