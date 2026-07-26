package devdb

import (
	"net/http/httptest"
	"testing"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

func TestSettingsPUTHook_ImageChangeInvalidatesEveryDriftEntry(t *testing.T) {
	f := newTestDBFeatureWithRejectedConfigUpdate(t, testConfig())
	recordCurrent(t, f.drift, "FirstDB", &sqlpublish.DiffResult{DB: "FirstDB", InSync: true}, sqlpublish.CodeNone, nil)
	recordCurrent(t, f.drift, "SecondDB", &sqlpublish.DiffResult{DB: "SecondDB", Created: 1}, sqlpublish.CodeNone, nil)

	handled := f.settingsPUTHook(httptest.NewRecorder(), []daemon.SettingsChange{{
		Key: "sql_server_image", Old: "server:v1", New: "server:v2",
	}})

	if !handled {
		t.Fatal("image change was not handled")
	}
	for _, entry := range f.drift.snapshot() {
		if !entry.Stale {
			t.Errorf("%s remained fresh after image change", entry.DB)
		}
	}
}

func TestSettingsPUTHook_OtherSettingKeepsDriftCurrent(t *testing.T) {
	f := newTestDBFeature(t, testConfig())
	recordCurrent(t, f.drift, "AppDB", &sqlpublish.DiffResult{DB: "AppDB", InSync: true}, sqlpublish.CodeNone, nil)

	handled := f.settingsPUTHook(httptest.NewRecorder(), []daemon.SettingsChange{{
		Key: "sql_server_pull_policy", Old: "missing", New: "always",
	}})

	if handled {
		t.Fatal("non-image setting unexpectedly triggered SQL Server restart")
	}
	if snapshot := f.drift.snapshot(); len(snapshot) != 1 || snapshot[0].Stale {
		t.Fatalf("non-image setting invalidated drift: %+v", snapshot)
	}
}
