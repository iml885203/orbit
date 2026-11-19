package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/iml885203/orbit/internal/cli"
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

type lifecycleCommandScopeKey struct{}

type lifecycleCommandScope struct {
	started        time.Time
	cancelDeadline context.CancelFunc
	stopSignals    context.CancelFunc
}

func lifecycleCommandHasBudget(cmd *cobra.Command) bool {
	if environmentApplyPreviewCommand(cmd) {
		return false
	}
	if cmd.Parent() == cmd.Root() {
		switch cmd.Name() {
		case "down", "restart":
			return true
		case "up":
			return cli.JSONOutput
		}
	}
	return cli.JSONOutput && cmd.Name() == "apply" && cmd.Parent() != nil && cmd.Parent().Name() == "env"
}

func prepareLifecycleCommandContext(cmd *cobra.Command) bool {
	if environmentApplyPreviewCommand(cmd) {
		return false
	}
	hasBudget := lifecycleCommandHasBudget(cmd)
	humanUp := cmd.Name() == "up" && cmd.Parent() == cmd.Root()
	humanApply := cmd.Name() == "apply" && cmd.Parent() != nil && cmd.Parent().Name() == "env"
	if !hasBudget && !humanUp && !humanApply {
		return false
	}
	ctx, stopSignals := lifecycleSignalContext(cmd.Context())
	var cancelDeadline context.CancelFunc = func() {}
	if hasBudget {
		ctx, cancelDeadline = lifecycleOperationContext(ctx)
	}
	scope := &lifecycleCommandScope{started: time.Now(), cancelDeadline: cancelDeadline, stopSignals: stopSignals}
	ctx = context.WithValue(ctx, lifecycleCommandScopeKey{}, scope)
	cmd.Root().SetContext(ctx)
	cmd.SetContext(ctx)
	return true
}

func closeLifecycleCommand(cmd *cobra.Command) {
	if cmd == nil || cmd.Context() == nil {
		return
	}
	scope, _ := cmd.Context().Value(lifecycleCommandScopeKey{}).(*lifecycleCommandScope)
	if scope != nil {
		scope.cancelDeadline()
		scope.stopSignals()
	}
}

func lifecycleCommandStarted(ctx context.Context) time.Time {
	if scope, _ := ctx.Value(lifecycleCommandScopeKey{}).(*lifecycleCommandScope); scope != nil {
		return scope.started
	}
	return time.Now()
}
