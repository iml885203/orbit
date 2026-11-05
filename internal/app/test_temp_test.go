package app

import (
	"os"
	"runtime"
)

// shortTestTempRoot keeps Unix socket fixtures below macOS's path limit while
// using the native temporary directory on platforms where /tmp does not exist.
func shortTestTempRoot() string {
	if runtime.GOOS == "darwin" {
		return "/tmp"
	}
	return os.TempDir()
}
