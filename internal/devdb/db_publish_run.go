package devdb

// The publish run lifecycle both entry points (CLI command, daemon
// handler) share: temp build dir, operation timeout, converge/clean
// dispatch. Progress prose and state recording stay with the callers —
// they differ by transport.

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/iml885203/orbit/internal/sqlpublish"
)

// withPublishScratch gives fn a temp build dir and a bounded context —
// the setup every host-side publish/reset shares — and cleans up after.
func withPublishScratch(opts sqlpublish.Opts, fn func(context.Context, sqlpublish.Opts) sqlpublish.Result) sqlpublish.Result {
	outDir, err := os.MkdirTemp("", "orbit-publish-*")
	if err != nil {
		return sqlpublish.Result{OK: false, Err: err, Code: sqlpublish.CodePublishFailed}
	}
	defer func() { _ = os.RemoveAll(outDir) }()
	opts.OutDir = outDir

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	return fn(ctx, opts)
}

func runSQLPublish(opts sqlpublish.Opts, clean bool, out io.Writer) sqlpublish.Result {
	return withPublishScratch(opts, func(ctx context.Context, opts sqlpublish.Opts) sqlpublish.Result {
		if clean {
			return sqlpublish.PublishClean(ctx, opts, out)
		}
		return sqlpublish.Publish(ctx, opts, out)
	})
}

func runSQLReset(opts sqlpublish.Opts, allowRecreate bool, out io.Writer) sqlpublish.Result {
	return withPublishScratch(opts, func(ctx context.Context, opts sqlpublish.Opts) sqlpublish.Result {
		return sqlpublish.Reset(ctx, opts, allowRecreate, out)
	})
}
