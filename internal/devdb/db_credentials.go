package devdb

// SA credential resolution for the host-side DB commands (reset,
// publish, snapshot): the CLI runs outside the daemon and reads the
// password from the environment or the target container itself.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// resolveSAPassword reads SA_PASSWORD from the environment, falling back
// to the named container's own config env so the user doesn't have to
// re-export what orbit already has. dockerName is the runtime docker
// name of the SQL Server the operation targets — credentials must come
// from the same container the operation connects to.
func resolveSAPassword(dockerName string) (string, error) {
	if v := os.Getenv("SA_PASSWORD"); v != "" {
		return v, nil
	}
	out, err := exec.Command("docker", "inspect", dockerName,
		"--format", "{{range .Config.Env}}{{println .}}{{end}}").Output()
	if err != nil {
		// inspect works on stopped containers too, so failure means the
		// container does not exist at all — the environment was never
		// brought up (or was torn down). Lead with that; the SA_PASSWORD
		// escape hatch is for pointing at an unmanaged server.
		return "", fmt.Errorf("the %s container does not exist — bring the environment up first (orbit up %s), or export SA_PASSWORD to target an unmanaged SQL Server", dockerName, SQLServerContainerName)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "SA_PASSWORD="); ok && v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("SA_PASSWORD not set and not found in %s env", dockerName)
}
