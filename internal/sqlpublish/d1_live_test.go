package sqlpublish

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestD1Live exercises the D1 auto-heal behaviour against a real, EMPTY
// SQL Server. It is guarded by ORBIT_D1_LIVE so it never runs in CI or a
// normal `go test` — point it at an isolated throwaway container only:
//
//	ORBIT_D1_LIVE=1 ORBIT_D1_PORT=14333 ORBIT_D1_PW='...' \
//	  ORBIT_D1_PROJ=/abs/App.sqlproj ORBIT_D1_DB=ApplicationSetting \
//	  go test ./internal/sqlpublish/ -run TestD1Live -v
func TestD1Live(t *testing.T) {
	if os.Getenv("ORBIT_D1_LIVE") == "" {
		t.Skip("set ORBIT_D1_LIVE to run the live D1 verification")
	}
	port, err := strconv.Atoi(os.Getenv("ORBIT_D1_PORT"))
	if err != nil {
		t.Fatalf("ORBIT_D1_PORT: %v", err)
	}
	opts := Opts{
		DB:       os.Getenv("ORBIT_D1_DB"),
		SQLProj:  os.Getenv("ORBIT_D1_PROJ"),
		OutDir:   t.TempDir(),
		Host:     "localhost",
		Port:     port,
		User:     "sa",
		Password: os.Getenv("ORBIT_D1_PW"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Empty server: the target DB must not exist yet.
	if exists, err := DatabaseExists(ctx, opts); err != nil {
		t.Fatalf("DatabaseExists (pre): %v", err)
	} else if exists {
		t.Fatalf("DB %q already exists — not an empty server", opts.DB)
	}

	// 2. Publish must create it, auto-heal composite (the project
	//    references CommonFiles' shared objects), and report Created.
	res := Publish(ctx, opts, os.Stdout)
	if !res.OK {
		t.Fatalf("Publish failed: %v (code=%s)", res.Err, res.Code)
	}
	if !res.Created {
		t.Errorf("expected Created=true on first publish to an empty server")
	}

	// 3. The DB now exists.
	if exists, err := DatabaseExists(ctx, opts); err != nil || !exists {
		t.Fatalf("DatabaseExists (post) = %v, err=%v; want true", exists, err)
	}

	// 4. Auto-baseline mechanism succeeds on the fresh, clean DB.
	if err := RefreshBaseline(ctx, opts, opts.DB, os.Stdout); err != nil {
		t.Fatalf("RefreshBaseline: %v", err)
	}

	// 5. Steady state: re-publishing an existing DB reports Created=false
	//    (no forced composite, no re-baseline) and still converges.
	res2 := Publish(ctx, opts, os.Stdout)
	if !res2.OK {
		t.Fatalf("second Publish failed: %v (code=%s)", res2.Err, res2.Code)
	}
	if res2.Created {
		t.Errorf("expected Created=false on re-publish of an existing DB")
	}
}
