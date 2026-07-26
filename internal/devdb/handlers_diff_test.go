package devdb

import (
	"errors"
	"net/http"
	"testing"

	"github.com/iml885203/orbit/internal/sqlpublish"
)

// diffHTTPStatus maps a diff failure code to an HTTP status. Precondition
// failures (missing toolchain/project, server unreachable) are 503 so the
// dashboard can tell "fix your setup" apart from a genuine 500. This pins
// that split.

func TestDiffHTTPStatus(t *testing.T) {
	cases := []struct {
		code sqlpublish.ErrorCode
		want int
	}{
		{sqlpublish.CodeToolchainMissing, http.StatusServiceUnavailable},
		{sqlpublish.CodeSQLProjectNotFound, http.StatusServiceUnavailable},
		{sqlpublish.CodeSQLServerUnavailable, http.StatusServiceUnavailable},
		{sqlpublish.CodeBuildFailed, http.StatusInternalServerError},
		{sqlpublish.CodePublishFailed, http.StatusInternalServerError},
		{sqlpublish.CodeNone, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := diffHTTPStatus(tc.code); got != tc.want {
			t.Errorf("diffHTTPStatus(%q) = %d; want %d", tc.code, got, tc.want)
		}
	}
}

func TestRunAndRecordDiff_PublishDuringDiffKeepsCacheStale(t *testing.T) {
	f := newTestDBFeature(t, testConfig())
	recordCurrent(t, f.drift, "AppDB", &sqlpublish.DiffResult{DB: "AppDB", InSync: true}, sqlpublish.CodeNone, nil)
	f.diffRunner = func(sqlpublish.Opts) (sqlpublish.DiffResult, sqlpublish.ErrorCode, error) {
		f.drift.markStale("AppDB")
		return sqlpublish.DiffResult{DB: "AppDB", Created: 1}, sqlpublish.CodeNone, nil
	}

	result, code, err := f.runAndRecordDiff(sqlpublish.Opts{DB: "AppDB"})

	if err != nil || code != sqlpublish.CodeNone || result.Created != 1 {
		t.Fatalf("diff result was not returned: result=%+v code=%s err=%v", result, code, err)
	}
	snapshot := f.drift.snapshot()
	if len(snapshot) != 1 || !snapshot[0].Stale || snapshot[0].Result.Created != 0 {
		t.Fatalf("diff that crossed publish invalidation overwrote cache: %+v", snapshot)
	}
}

func TestRunAndRecordDiff_ImageSwitchDuringFailureKeepsPriorEntry(t *testing.T) {
	f := newTestDBFeature(t, testConfig())
	recordCurrent(t, f.drift, "AppDB", &sqlpublish.DiffResult{DB: "AppDB", InSync: true}, sqlpublish.CodeNone, nil)
	f.diffRunner = func(sqlpublish.Opts) (sqlpublish.DiffResult, sqlpublish.ErrorCode, error) {
		f.drift.markAllStale()
		return sqlpublish.DiffResult{}, sqlpublish.CodePublishFailed, errors.New("diff failed")
	}

	_, _, _ = f.runAndRecordDiff(sqlpublish.Opts{DB: "AppDB"})

	snapshot := f.drift.snapshot()
	if len(snapshot) != 1 || !snapshot[0].Stale || snapshot[0].Error != "" {
		t.Fatalf("failure from the previous image overwrote cache: %+v", snapshot)
	}
}
