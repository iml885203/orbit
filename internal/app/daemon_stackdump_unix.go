//go:build !windows

package app

import (
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// watchGoroutineDumpSignal writes a goroutine dump to w on every SIGUSR1.
// SIGQUIT also dumps stacks but kills the daemon; SIGUSR1 leaves it running.
// stop returns only after any dump in progress is written, so w can be
// closed right after.
func watchGoroutineDumpSignal(w io.Writer) (stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			case <-signals:
				slog.Info("writing goroutine dump", "component", "daemon")
				if err := writeGoroutineDump(w, time.Now()); err != nil {
					slog.Warn("goroutine dump failed", "component", "daemon", "err", err)
				}
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
		<-finished
	}
}
