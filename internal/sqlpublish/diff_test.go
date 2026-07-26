package sqlpublish

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// parseDeployReport is pure XML → DiffResult, so it is unit-testable with
// captured sqlpackage output — no SQL Server needed. Samples below are
// real sqlpackage 170.4 DeployReport XML.

func TestParseDeployReport_InSync(t *testing.T) {
	xml := `<?xml version="1.0" encoding="utf-8"?><DeploymentReport xmlns="http://schemas.microsoft.com/sqlserver/dac/DeployReport/2012/02"><Alerts /></DeploymentReport>`
	res, code, err := parseDeployReport("DB", []byte(xml))
	if err != nil || code != CodeNone {
		t.Fatalf("parse: code=%s err=%v", code, err)
	}
	if !res.InSync {
		t.Errorf("empty report must be in sync; got %+v", res)
	}
	if res.Created+res.Altered+res.Dropped != 0 || len(res.Ops) != 0 {
		t.Errorf("in-sync must have zero ops; got %+v", res)
	}
	if res.DataLoss {
		t.Error("in-sync must not flag data loss")
	}
}

func TestParseDeployReport_DropWithDataLoss(t *testing.T) {
	// A Drop of a populated table: one Operation, one DataIssue alert
	// linked by Issue Id="1".
	xml := `<?xml version="1.0" encoding="utf-8"?><DeploymentReport xmlns="http://schemas.microsoft.com/sqlserver/dac/DeployReport/2012/02"><Alerts><Alert Name="DataIssue"><Issue Value="Table [dbo].[ExtraTable] is being dropped, data loss may occur." Id="1" /></Alert></Alerts><Operations><Operation Name="Drop"><Item Value="[dbo].[ExtraTable]" Type="SqlTable"><Issue Id="1" /></Item></Operation></Operations></DeploymentReport>`
	res, code, err := parseDeployReport("DB", []byte(xml))
	if err != nil || code != CodeNone {
		t.Fatalf("parse: code=%s err=%v", code, err)
	}
	if res.InSync {
		t.Error("report with a Drop must not be in sync")
	}
	if res.Dropped != 1 || res.Created != 0 || res.Altered != 0 {
		t.Errorf("want 1 drop; got created=%d altered=%d dropped=%d", res.Created, res.Altered, res.Dropped)
	}
	if len(res.Ops) != 1 || res.Ops[0].Action != "Drop" || res.Ops[0].ObjectType != "SqlTable" || res.Ops[0].Name != "[dbo].[ExtraTable]" {
		t.Errorf("op not parsed: %+v", res.Ops)
	}
	if !res.Ops[0].DataLoss {
		t.Error("the dropped populated table op must be flagged data_loss (linked via Issue Id)")
	}
	if !res.DataLoss {
		t.Error("result must flag data_loss when a DataIssue alert is present")
	}
	if len(res.Alerts) != 1 || res.Alerts[0].Kind != "DataIssue" {
		t.Errorf("alert not surfaced: %+v", res.Alerts)
	}
}

func TestParseDeployReport_OrderingDropFirst(t *testing.T) {
	xml := `<DeploymentReport xmlns="http://schemas.microsoft.com/sqlserver/dac/DeployReport/2012/02"><Alerts /><Operations>` +
		`<Operation Name="Create"><Item Value="[dbo].[NewA]" Type="SqlTable" /></Operation>` +
		`<Operation Name="Drop"><Item Value="[dbo].[OldZ]" Type="SqlTable" /></Operation>` +
		`<Operation Name="Alter"><Item Value="[dbo].[MidM]" Type="SqlTable" /></Operation>` +
		`</Operations></DeploymentReport>`
	res, _, err := parseDeployReport("DB", []byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Ops) != 3 {
		t.Fatalf("want 3 ops; got %d", len(res.Ops))
	}
	// Drop → Alter → Create.
	if res.Ops[0].Action != "Drop" || res.Ops[1].Action != "Alter" || res.Ops[2].Action != "Create" {
		t.Errorf("ops not ordered Drop→Alter→Create: %v", res.Ops)
	}
}

// runReportAction must mirror Publish's reactive composite retry: a first
// run failing with SQL72033 (a GRANT depends on a role a referenced project
// defines but the target DB lacks) retries once with
// /p:IncludeCompositeObjects=true. Verified with a fake sqlpackage on PATH:
// it fails without the flag and succeeds with it, recording each call's args.
func TestRunReportAction_RetriesWithComposite(t *testing.T) {
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
	code, err := runReportAction(context.Background(), opts, "fake.dacpac", "DeployReport", filepath.Join(dir, "out.xml"), io.Discard)
	if err != nil || code != CodeNone {
		t.Fatalf("retry must succeed: code=%s err=%v", code, err)
	}

	calls, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 sqlpackage calls (plain, then composite); got %d:\n%s", len(lines), calls)
	}
	if strings.Contains(lines[0], "IncludeCompositeObjects") {
		t.Errorf("first call must not include composite: %s", lines[0])
	}
	if !strings.Contains(lines[0], "/p:DropObjectsNotInSource=true") {
		t.Errorf("report must use schema-convergent drop semantics: %s", lines[0])
	}
	if !strings.Contains(lines[1], "/p:IncludeCompositeObjects=true") {
		t.Errorf("retry must include composite: %s", lines[1])
	}
}

// A failure that is NOT an unresolved reference must not be retried —
// the fake fails with a connection error; exactly one call is made.
func TestRunReportAction_NoRetryOnOtherFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sqlpackage is a shell script")
	}
	dir := t.TempDir()
	callLog := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		`echo "$@" >> "` + callLog + "\"\n" +
		`echo "could not connect to target"` + "\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlpackage"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	opts := Opts{DB: "DB", Host: "localhost", Port: 1433, User: "sa", Password: "x", OutDir: dir}
	code, err := runReportAction(context.Background(), opts, "fake.dacpac", "DeployReport", filepath.Join(dir, "out.xml"), io.Discard)
	if err == nil {
		t.Fatal("must fail")
	}
	if code != CodeSQLServerUnavailable {
		t.Errorf("want CodeSQLServerUnavailable; got %s", code)
	}
	calls, readErr := os.ReadFile(callLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if n := len(strings.Split(strings.TrimSpace(string(calls)), "\n")); n != 1 {
		t.Errorf("must not retry a non-reference failure; got %d calls", n)
	}
}

// Summary is the one-line string the CLI and dashboard show. It has several
// branch combinations (in-sync, any subset of create/alter/drop, plus the
// data-loss suffix) — table-driven so a wording or pluralization regression
// is caught.
func TestDiffResult_Summary(t *testing.T) {
	cases := []struct {
		name string
		r    DiffResult
		want string
	}{
		{"in sync", DiffResult{InSync: true}, "in sync — no schema changes"},
		{"only creates", DiffResult{Created: 2}, "2 to create"},
		{"only alters", DiffResult{Altered: 1}, "1 to alter"},
		{"only drops", DiffResult{Dropped: 3}, "3 to drop"},
		{"mixed order create-alter-drop", DiffResult{Created: 1, Altered: 2, Dropped: 3}, "1 to create, 2 to alter, 3 to drop"},
		{"data loss suffix", DiffResult{Dropped: 1, DataLoss: true}, "1 to drop — possible data loss"},
	}
	for _, tc := range cases {
		if got := tc.r.Summary(); got != tc.want {
			t.Errorf("%s: Summary() = %q; want %q", tc.name, got, tc.want)
		}
	}
}
