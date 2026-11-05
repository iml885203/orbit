//go:build !darwin && !linux

package platform

// ProcessArgs is unavailable here; see ErrProcessIdentityUnsupported.
func ProcessArgs(int) ([]string, error) { return nil, ErrProcessIdentityUnsupported }

// ProcessWorkingDir is unavailable here; see ErrProcessIdentityUnsupported.
func ProcessWorkingDir(int) (string, error) { return "", ErrProcessIdentityUnsupported }
