//go:build !windows

package app

import (
	"os"
	"os/exec"
)

func configureSignalTestProcess(_ *exec.Cmd) (func(), error) {
	return func() {}, nil
}

func signalTestProcess(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Interrupt)
}
