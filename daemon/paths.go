package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// OrbitDir returns the orbit data directory, creating it if necessary.
// ORBIT_HOME overrides the default location (useful for isolated e2e tests).
// Unix: ~/.orbit, Windows: %LOCALAPPDATA%\orbit (falls back to %APPDATA%\orbit).
func OrbitDir() string {
	if override := os.Getenv("ORBIT_HOME"); override != "" {
		_ = os.MkdirAll(override, 0755)
		return override
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".orbit")

	// On Windows, prefer LOCALAPPDATA for consistency with other CLI tools.
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		dir = filepath.Join(localApp, "orbit")
	}

	_ = os.MkdirAll(dir, 0755)
	return dir
}

// DefaultSocketPath returns ~/.orbit/orbit.sock.
func DefaultSocketPath() string {
	return filepath.Join(OrbitDir(), "orbit.sock")
}

// sunPathLimit is the kernel's sockaddr_un.sun_path budget, including the
// trailing NUL: 104 bytes on darwin/BSD, 108 on Linux and Windows.
func sunPathLimit() int {
	if runtime.GOOS == "darwin" {
		return 104
	}
	return 108
}

// ValidateSocketPath rejects socket paths that exceed the OS's unix socket
// path budget before bind/dial turns them into an opaque "invalid argument".
// The limit includes the trailing NUL, so the longest usable path is one
// byte shorter. Overlong paths come from long ORBIT_HOME overrides.
func ValidateSocketPath(path string) error {
	limit := sunPathLimit()
	if len(path) >= limit {
		return fmt.Errorf("%s is %d bytes, over the %d-byte OS limit for unix sockets — set ORBIT_HOME to a shorter path", path, len(path), limit)
	}
	return nil
}
