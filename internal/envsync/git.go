package envsync

import (
	"fmt"
	"net/url"
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
	parsed, err := url.Parse(e.URL)
	if err != nil || parsed.User == nil {
		return e.URL
	}
	parsed.User = nil
	return parsed.String()
}

func (e *CloneError) IsGitHub() bool {
	parsed, err := url.Parse(e.URL)
	if err == nil && strings.EqualFold(parsed.Hostname(), "github.com") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(e.URL), "git@github.com:")
}

// Clone performs a shallow clone of url into destDir. destDir must be empty
// (or nonexistent — git clone creates it).
func Clone(url, destDir string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", "--quiet", url, destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &CloneError{URL: url, Err: err, Output: string(out)}
	}
	return nil
}
