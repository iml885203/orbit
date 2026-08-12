package sqlpublish

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The dacpac a publish pushes is the one `dotnet build` actually emitted —
// named after the .sqlproj, never after the target database. A project
// declaring `databases:` publishes one artifact to several databases, so
// deriving the path from the database name looks for a file no build writes.
//
// This runs a real build on purpose: the bug it guards shipped because every
// other test in this package stubs the dacpac path and never builds anything.
func TestBuildDacpac_ResolvesArtifactBuiltUnderProjectName(t *testing.T) {
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	if _, err := SqlpackagePath(); err != nil {
		t.Skip("sqlpackage not installed")
	}
	t.Setenv("ORBIT_HOME", t.TempDir())

	opts := Opts{
		DB:      "AccountsDev", // deliberately unequal to the .sqlproj name
		SQLProj: writeBuildableProj(t, "Accounts"),
		OutDir:  t.TempDir(),
	}

	var log strings.Builder
	dacpac, _, code, err := buildDacpac(context.Background(), opts, &log)
	if err != nil {
		t.Fatalf("buildDacpac: %v (code %s)\n%s", err, code, log.String())
	}
	if _, err := os.Stat(dacpac); err != nil {
		t.Fatalf("returned dacpac %q does not exist: %v", dacpac, err)
	}
	if got := filepath.Base(dacpac); got != "Accounts.dacpac" {
		t.Errorf("dacpac = %q, want the build's Accounts.dacpac", got)
	}
}

// A project that redirects its own output must fail naming what the build
// actually wrote — the one override that defeats filename derivation, and
// silently publishing some other project's dacpac would be far worse.
func TestBuildDacpac_RedirectedOutputNamesWhatTheBuildWrote(t *testing.T) {
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	if _, err := SqlpackagePath(); err != nil {
		t.Skip("sqlpackage not installed")
	}
	t.Setenv("ORBIT_HOME", t.TempDir())

	proj := writeBuildableProj(t, "Ledger")
	redirect := strings.Replace(buildableSQLProj,
		"<PropertyGroup>", "<PropertyGroup>\n    <SqlTargetName>Renamed</SqlTargetName>", 1)
	if err := os.WriteFile(proj, []byte(redirect), 0o644); err != nil {
		t.Fatal(err)
	}

	var log strings.Builder
	_, _, _, err := buildDacpac(context.Background(), Opts{
		DB: "LedgerDev", SQLProj: proj, OutDir: t.TempDir(),
	}, &log)
	if err == nil {
		t.Fatal("a redirected build output must not be silently accepted")
	}
	if !strings.Contains(err.Error(), "Renamed.dacpac") {
		t.Errorf("error must name what the build wrote, got: %v", err)
	}
}

// writeBuildableProj lays down a .sqlproj that `dotnet build` can really
// build — unlike writeProj's stub, which only needs to be readable.
func writeBuildableProj(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, name+".sqlproj")
	if err := os.WriteFile(proj, []byte(buildableSQLProj), 0o644); err != nil {
		t.Fatal(err)
	}
	table := "CREATE TABLE [dbo].[Users] ([Id] INT NOT NULL PRIMARY KEY);\n"
	if err := os.WriteFile(filepath.Join(dir, "Users.sql"), []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

const buildableSQLProj = `<Project Sdk="Microsoft.Build.Sql/2.1.0">
  <PropertyGroup>
    <DSP>Microsoft.Data.Tools.Schema.Sql.Sql150DatabaseSchemaProvider</DSP>
    <ModelCollation>1033, CI</ModelCollation>
  </PropertyGroup>
</Project>
`
