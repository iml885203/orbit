//go:build windows

package app

import "io"

// watchGoroutineDumpSignal is a no-op: Windows has no SIGUSR1.
func watchGoroutineDumpSignal(_ io.Writer) (stop func()) {
	return func() {}
}
