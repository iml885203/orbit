package sqlpublish

// Server connectivity for the publish path: the master-database
// connection every snapshot/readiness operation runs through, and the
// readiness wait bootstrap needs before touching a fresh container.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"
)

// openMasterDB connects to the instance's master database — snapshot
// DDL and metadata queries all run from there, never from inside the
// target database.
func openMasterDB(opts Opts) (*sql.DB, error) {
	u := url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(opts.User, opts.Password),
		Host:     net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)),
		RawQuery: url.Values{"database": {"master"}, "TrustServerCertificate": {"true"}}.Encode(),
	}
	return sql.Open("sqlserver", u.String())
}

// DatabaseExists reports whether the target database (opts.DB) exists
// on the instance. Publish uses it to tell a first-time create — which
// needs composite deployment and earns a fresh baseline — from a
// steady-state converge. DB_ID returns NULL for an absent database, so
// a non-Valid scan means "does not exist".
func DatabaseExists(ctx context.Context, opts Opts) (bool, error) {
	conn, err := openMasterDB(opts)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()

	var id sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT DB_ID(@p1)", opts.DB).Scan(&id); err != nil {
		return false, err
	}
	return id.Valid, nil
}

// WaitReady blocks until the server accepts master-database queries or
// ctx expires — freshly started SQL Server containers listen on the
// port well before logins succeed, and bootstrap must not race that.
func WaitReady(ctx context.Context, opts Opts, out io.Writer) error {
	conn, err := openMasterDB(opts)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	fmt.Fprintf(out, "[wait] waiting for SQL Server at %s:%d\n", opts.Host, opts.Port)
	for {
		var one int
		if err := conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("SQL Server at %s:%d not ready: %w", opts.Host, opts.Port, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}
