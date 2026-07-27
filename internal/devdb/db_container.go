package devdb

// Runtime docker-container concerns of the SQL Server workflow.

import (
	"os"
	"os/exec"
	"strings"

	"github.com/iml885203/orbit/container"
)

// dbTargetDockerName maps an env container name to the same runtime docker
// name used by the orchestrator.
func dbTargetDockerName(name string) string {
	return container.ContainerName(os.Getenv("ORBIT_NAMESPACE"), name)
}

// containerRunning reports whether the named docker container is up —
// the readiness gate every DB operation runs before touching a database.
func containerRunning(dockerName string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", dockerName).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}
