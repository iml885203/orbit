package devdb

// Runtime docker-container concerns of the DB workflow: the container
// naming convention's runtime name and the is-it-up readiness check
// every DB operation runs before touching a database.

import (
	"os/exec"
	"strings"

	"github.com/iml885203/orbit/container"
)

// dbTargetDockerName maps an env container name to its runtime docker
// name. Deliberately namespace-blind: the DB workflow's exec paths
// never honored ORBIT_NAMESPACE (unlike container seeding) —
// centralising the policy must not silently change that. Single owner
// of the rule for every DB-workflow docker interaction.
func dbTargetDockerName(name string) string {
	return container.ContainerName("", name)
}

// SQLServerDockerContainer is the runtime docker name of the sql-server
// container ("orbit-sql-server").
func SQLServerDockerContainer() string {
	return dbTargetDockerName(SQLServerContainerName)
}

// sqlServerContainer memoizes the legacy sql-server runtime name for
// the flows that are hard-wired to it (reset, the pre-target
// CLI fallback).
var sqlServerContainer = SQLServerDockerContainer()

// containerRunning reports whether the named docker container is up —
// the readiness gate both publish (against its declared target) and the
// legacy sql-server ops check before touching the database.
func containerRunning(dockerName string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", dockerName).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}
