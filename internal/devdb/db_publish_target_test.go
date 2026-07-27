package devdb

import (
	"os"
	"testing"

	"github.com/iml885203/orbit/config"
)

func TestDBTargetDockerNameUsesOrbitNamespace(t *testing.T) {
	previous := os.Getenv("ORBIT_NAMESPACE")
	t.Cleanup(func() { _ = os.Setenv("ORBIT_NAMESPACE", previous) })
	if err := os.Setenv("ORBIT_NAMESPACE", "sandbox"); err != nil {
		t.Fatal(err)
	}
	if got := dbTargetDockerName("database"); got != "orbit-sandbox-database" {
		t.Fatalf("dbTargetDockerName = %q", got)
	}
}

// publishTargetHostPort picks the host port that reaches a container's SQL
// Server. The rule has three branches — an explicit 1433 target wins, a lone
// published port is taken by default, and anything ambiguous is an error —
// so it's exactly the kind of selection logic a test should pin.

func TestPublishTargetHostPort_PrefersTarget1433(t *testing.T) {
	c := &config.Container{Ports: map[string]config.PortDef{
		"metrics": {Host: 15000, Target: 5000},
		"sql":     {Host: 11433, Target: 1433},
	}}
	port, err := publishTargetHostPort(c)
	if err != nil {
		t.Fatal(err)
	}
	if port != 11433 {
		t.Errorf("want the host port mapped to 1433 (11433); got %d", port)
	}
}

func TestPublishTargetHostPort_SinglePortDefault(t *testing.T) {
	// One published port that is NOT 1433 — the default branch takes it.
	c := &config.Container{Ports: map[string]config.PortDef{"svc": {Host: 14330, Target: 5000}}}
	port, err := publishTargetHostPort(c)
	if err != nil {
		t.Fatal(err)
	}
	if port != 14330 {
		t.Errorf("a single published port must be taken by default; got %d", port)
	}
}

func TestPublishTargetHostPort_NoPorts(t *testing.T) {
	c := &config.Container{}
	if _, err := publishTargetHostPort(c); err == nil {
		t.Error("a container with no published port must error")
	}
}

func TestPublishTargetHostPort_AmbiguousMultiNon1433(t *testing.T) {
	c := &config.Container{Ports: map[string]config.PortDef{
		"a": {Host: 15000, Target: 5000},
		"b": {Host: 16000, Target: 6000},
	}}
	if _, err := publishTargetHostPort(c); err == nil {
		t.Error("multiple ports with none targeting 1433 must be ambiguous")
	}
}

func TestPublishTargetIdentity_ChangesAcrossEnvAndImage(t *testing.T) {
	base := publishTargetIdentity("/envs/a.yaml", "orbit-sql-server", "db:v1")
	if got := publishTargetIdentity("/envs/b.yaml", "orbit-sql-server", "db:v1"); got == base {
		t.Error("switching env must change the target identity")
	}
	if got := publishTargetIdentity("/envs/a.yaml", "orbit-sql-server", "db:v2"); got == base {
		t.Error("switching SQL Server image must change the target identity")
	}
}
