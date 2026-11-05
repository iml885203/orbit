package container

import (
	"os/exec"
	"strings"
)

func newCredHelperCmd(helper, registry string) *exec.Cmd {
	cmd := exec.Command(helper, "get")
	cmd.Stdin = strings.NewReader(registry)
	return cmd
}
