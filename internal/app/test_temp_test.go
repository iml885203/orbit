package app

import "os"

func shortTestTempRoot() string {
	if root := os.Getenv("ORBIT_TEST_SOCKET_ROOT"); root != "" {
		return root
	}
	return os.TempDir()
}
