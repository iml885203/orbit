package volume

import (
	"path/filepath"
	"strings"
)

func SplitShort(value string) (string, string) {
	if hasWindowsDrivePrefix(value) {
		if offset := strings.Index(value[2:], ":"); offset >= 0 {
			index := 2 + offset
			return value[:index], value[index:]
		}
	}
	if index := strings.Index(value, ":"); index >= 0 {
		return value[:index], value[index:]
	}
	return value, ""
}

func IsBindSource(source string) bool {
	return filepath.IsAbs(source) || hasWindowsDrivePrefix(source) ||
		strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~")
}

func IsRelativeBindSource(source string) bool {
	return strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") ||
		strings.HasPrefix(source, `.\`) || strings.HasPrefix(source, `..\`)
}

func hasWindowsDrivePrefix(path string) bool {
	if len(path) < 3 || path[1] != ':' || (path[2] != '/' && path[2] != '\\') {
		return false
	}
	letter := path[0]
	return letter >= 'a' && letter <= 'z' || letter >= 'A' && letter <= 'Z'
}
