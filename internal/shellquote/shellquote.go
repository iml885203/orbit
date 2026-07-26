package shellquote

import "strings"

// Quote returns a POSIX-shell-safe representation for a single argument.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !isSafeShellRune(r)
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func isSafeShellRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		strings.ContainsRune("_-./:=,@%", r)
}
