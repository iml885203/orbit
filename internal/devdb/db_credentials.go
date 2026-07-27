package devdb

// Credential resolution for host-side DB commands. The configured environment
// key is read from the target container so connection details always come from
// the same explicit SQL Server target.

import (
	"fmt"
	"os/exec"
	"strings"
)

func resolveContainerPassword(dockerName, passwordEnv string) (string, error) {
	out, err := exec.Command("docker", "inspect", dockerName,
		"--format", "{{range .Config.Env}}{{println .}}{{end}}").Output()
	if err != nil {
		return "", fmt.Errorf("the %s container does not exist — start the configured SQL Server target first", dockerName)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, passwordEnv+"="); ok && v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s is not available in the %s container environment", passwordEnv, dockerName)
}
