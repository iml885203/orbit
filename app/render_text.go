package app

import (
	"strings"
	"unicode/utf8"
)

// truncate clips s to n runes, marking the cut with "…".
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// padRight and padLeft pad to n runes; fmt's %-Ns counts bytes, which
// misaligns any cell holding a multi-byte rune such as "…" or "→".
func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-utf8.RuneCountInString(s)))
}

func padLeft(s string, n int) string {
	return strings.Repeat(" ", max(0, n-utf8.RuneCountInString(s))) + s
}
