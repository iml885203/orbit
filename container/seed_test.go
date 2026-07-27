package container

import (
	"strings"
	"testing"
)

func TestSQLServerSeedDockerArgsUseContainerCredentialKey(t *testing.T) {
	args := sqlServerSeedDockerArgs("sandbox-database", "developer", "DB_PASSWORD")
	joined := strings.Join(args, "\n")
	for _, want := range []string{"sandbox-database", "developer", "DB_PASSWORD"} {
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
