package sqlpublish

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPublish_UnavailableServerStopsBeforeBuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	project := filepath.Join(t.TempDir(), "NopeDB.sqlproj")
	if err := os.WriteFile(project, []byte("<Project />"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Publish(ctx, Opts{
		DB:      "NopeDB",
		SQLProj: project,
		OutDir:  t.TempDir(),
		Host:    "localhost", Port: 1, User: "sa", Password: "x",
	}, io.Discard)
	if res.OK {
		t.Fatal("expected failure for unavailable server")
	}
	if res.Code != CodeSQLServerUnavailable {
		t.Errorf("code = %q, want sql_server_unavailable", res.Code)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "probe database existence") {
		t.Errorf("Err = %v, want database existence probe context", res.Err)
	}
}

func TestPublish_MissingProjectStopsBeforeServerProbe(t *testing.T) {
	res := Publish(context.Background(), Opts{
		DB:      "NopeDB",
		SQLProj: filepath.Join(t.TempDir(), "NopeDB.sqlproj"),
		OutDir:  t.TempDir(),
		Host:    "localhost", Port: 1, User: "sa", Password: "x",
	}, io.Discard)
	if res.OK || res.Code != CodeSQLProjectNotFound {
		t.Fatalf("result = %+v, want sql_project_not_found", res)
	}
}

func TestClassifyPublish(t *testing.T) {
	cases := []struct {
		output string
		want   ErrorCode
	}{
		{"Error SQL72031: rows were detected. BlockOnPossibleDataLoss=true", CodePublishBlockedDataLoss},
		{"The schema update is terminating because possible data loss", CodePublishBlockedDataLoss},
		{"Warning SQL72015: 正在卸除資料行。\nError SQL72014: 訊息 50000。\nError SQL72045: 指令碼執行錯誤。", CodePublishBlockedDataLoss},
		{"A network-related or instance-specific error occurred", CodeSQLServerUnavailable},
		{"Login failed for user 'sa'", CodeSQLServerUnavailable},
		{"The connection was refused by the remote host", CodeSQLServerUnavailable},
		{"Transaction (Process ID 51) was deadlocked", CodeDatabaseBusy},
		{"Lock request time out period exceeded", CodeDatabaseBusy},
		{"Error SQL72033: An error occurred while attempting to publish the ExampleRole object", CodeReferenceUnresolved},
		{"Publish failed: unresolved reference to object [dbo].[SharedRole]", CodeReferenceUnresolved},
		{"some other unclassifiable failure", CodePublishFailed},
	}
	for _, c := range cases {
		if got := classifyPublish(c.output); got != c.want {
			t.Errorf("classify(%q) = %q, want %q", c.output[:30], got, c.want)
		}
	}
}

// publishWithCompositeRetry must mirror runReportAction's contract on the
// publish side: SQL72033 retries once with composite, other failures do
// not retry. PublishClean depends on this — its post-revert publish runs
// against a baseline that may predate a referenced project's shared
// objects (the reset partial-failure this guards against).
func TestPublishWithCompositeRetry_RetriesOnSQL72033(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sqlpackage is a shell script")
	}
	dir := t.TempDir()
	callLog := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		`echo "$@" >> "` + callLog + "\"\n" +
		`case "$@" in *IncludeCompositeObjects=true*) exit 0 ;; esac` + "\n" +
		`echo "Error SQL72033: permission depends on missing role"` + "\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlpackage"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	opts := Opts{DB: "DB", Host: "localhost", Port: 1433, User: "sa", Password: "x", OutDir: dir}
	code, err := publishWithCompositeRetry(context.Background(), opts, "fake.dacpac", io.Discard)
	if err != nil || code != CodeNone {
		t.Fatalf("retry must succeed: code=%s err=%v", code, err)
	}
	calls, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "/p:IncludeCompositeObjects=true") {
		t.Fatalf("want plain call then composite retry; got:\n%s", calls)
	}
	if !strings.Contains(lines[0], "/p:DropObjectsNotInSource=true") {
		t.Errorf("publish must converge target-only schema objects: %s", lines[0])
	}
}
