package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/iml885203/orbit/cli"
)

func lifecycleOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, effectiveTimeout(timeout))
}

func lifecycleSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func lifecycleOperationError(ctx context.Context, err error, reconcileDispatched bool) error {
	cause := err
	if ctxErr := ctx.Err(); ctxErr != nil {
		cause = ctxErr
	}
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		return cli.NewTimeoutError(fmt.Sprintf("lifecycle operation exceeded its %s timeout", effectiveTimeout(timeout)))
	case errors.Is(cause, context.Canceled):
		message := "lifecycle operation canceled"
		if reconcileDispatched {
			message += "; the daemon may have accepted the environment reconcile and can continue it — inspect current state with 'orbit status --json'"
		}
		return cli.NewCanceledError(message)
	default:
		return err
	}
}
