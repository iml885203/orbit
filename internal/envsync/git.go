package envsync

import (
	"fmt"
	"os/exec"
)

// Clone performs a shallow clone of url into destDir. destDir must be empty
// (or nonexistent — git clone creates it).
func Clone(url, destDir string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", "--quiet", url, destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s: %w\n%s", url, err, out)
	}
	return nil
}
