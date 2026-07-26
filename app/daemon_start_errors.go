package app

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

// renderDaemonStartError prints an actionable hint block to stderr for
// failures returned by daemon.EnsureDaemon, then returns an error that
// suppresses cobra's default "Error: ..." line (we already printed it).
//
// In --json mode it returns err unchanged so cli.WriteJSONError can
// produce the structured envelope.
//
// Callers should use it like:
//
//	client, err := daemon.EnsureDaemon(path, groups)
//	if err != nil {
//	    return renderDaemonStartError(err)
//	}
func renderDaemonStartError(err error) error {
	if err == nil {
		return nil
	}
	if cli.JSONOutput {
		return err
	}
	writeDaemonStartError(os.Stderr, err)
	return errCLIJSONAlreadyRendered{err: err}
}

// writeDaemonStartError formats the error + hint block for the given
// writer. Split out so tests can capture output without touching stderr.
func writeDaemonStartError(w io.Writer, err error) {
	_, _ = fmt.Fprintf(w, "Error: %v\n", err)
	hint := hintFor(err)
	if hint != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprint(w, hint)
	}
}

func hintFor(err error) string {
	switch {
	case errors.Is(err, daemon.ErrInvalidConfig):
		return "Next steps:\n" +
			"  orbit env list                # see available envs\n" +
			"  orbit switch <name>           # switch to a working env\n"
	case errors.Is(err, daemon.ErrDaemonExitedEarly):
		return "Next steps:\n" +
			"  cat " + daemon.DefaultLogPath() + "   # full daemon log\n" +
			"  orbit daemon status                       # confirm not running\n"
	case errors.Is(err, daemon.ErrDaemonNotReady):
		return "Next steps:\n" +
			"  cat " + daemon.DefaultLogPath() + "   # full daemon log\n" +
			"  orbit daemon stop                         # kill the stuck daemon\n" +
			"  orbit daemon restart                      # try again\n"
	}
	return ""
}
