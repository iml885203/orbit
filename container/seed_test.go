package container

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestRunSeedUsesContainerCommandAndTracksOutcome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	docker := filepath.Join(binDir, "docker")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > \"$ORBIT_TEST_SEED_ARGS\"\n/bin/cat > \"$ORBIT_TEST_SEED_STDIN\"\n"
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("ORBIT_HOME", t.TempDir())
	t.Setenv("ORBIT_NAMESPACE", "seed-journey")
	t.Setenv("ORBIT_TEST_SEED_ARGS", argsFile)
	t.Setenv("ORBIT_TEST_SEED_STDIN", stdinFile)

	seedFile := filepath.Join(t.TempDir(), "seed.sql")
	if err := os.WriteFile(seedFile, []byte("SELECT 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := `SQLCMDPASSWORD="$MSSQL_SA_PASSWORD" /opt/mssql-tools18/bin/sqlcmd -S localhost -U sa -C -I`
	cfg := &config.Container{Seed: &config.SeedConfig{
		Command: command,
		Files:   []string{seedFile},
	}}

	results := RunSeed("database", cfg, false)
	if len(results) != 1 || results[0].Status != "executed" {
		t.Fatalf("first seed = %+v", results)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"seed-journey", "database", "/bin/sh -c", command} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("docker args missing %q: %s", want, args)
		}
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "SELECT 1" {
		t.Fatalf("seed stdin = %q", stdin)
	}

	results = RunSeed("database", cfg, false)
	if len(results) != 1 || results[0].Status != "skipped" {
		t.Fatalf("unchanged seed = %+v", results)
	}
	cfg.Seed.Command = "psql -U app -d other"
	results = RunSeed("database", cfg, false)
	if len(results) != 1 || results[0].Status != "changed" {
		t.Fatalf("retargeted seed = %+v", results)
	}
	if strings.Contains(string(args), "secret") {
		t.Fatalf("database secret leaked into host arguments: %s", args)
	}
}
