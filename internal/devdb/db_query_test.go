package devdb

import (
	"slices"
	"strings"
	"testing"
)

func TestDBQueryDockerArgsKeepsPasswordOutOfHostArguments(t *testing.T) {
	meta := &DevDBMetaResponse{
		SQLServerTarget:      "sandbox-database",
		SQLServerUsername:    "developer",
		SQLServerPasswordEnv: "DB_PASSWORD",
	}
	args := dbQueryDockerArgs(meta, []string{"SELECT", "1"})

	if slices.Contains(args, "secret") {
		t.Fatal("password appeared in docker arguments")
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{"sandbox-database", "DB_PASSWORD", "developer", "SELECT 1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("docker args missing %q: %+v", want, args)
		}
	}
	for _, arg := range args {
		if strings.Contains(arg, "-P") {
			t.Fatalf("sqlcmd password flag leaked into host arguments: %+v", args)
		}
	}
}
