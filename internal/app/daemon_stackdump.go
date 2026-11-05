package app

import (
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"time"
)

// writeGoroutineDump writes every goroutine's stack in panic-traceback form,
// so a daemon that looks stuck can be diagnosed while it keeps running.
func writeGoroutineDump(w io.Writer, now time.Time) error {
	if _, err := fmt.Fprintf(w, "=== orbit daemon goroutine dump pid=%d at %s ===\n", os.Getpid(), now.Format(time.RFC3339)); err != nil {
		return err
	}
	return pprof.Lookup("goroutine").WriteTo(w, 2)
}
