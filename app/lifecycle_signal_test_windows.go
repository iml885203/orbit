//go:build windows

package app

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	allocConsole          = kernel32.NewProc("AllocConsole")
	freeConsole           = kernel32.NewProc("FreeConsole")
)

func configureSignalTestProcess(cmd *exec.Cmd) (func(), error) {
	releaseController, err := ensureSignalTestConsole()
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return releaseController, nil
}

func signalTestProcess(cmd *exec.Cmd) error {
	if cmd.Process.Pid == 0 {
		return fmt.Errorf("refusing to signal process group 0")
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

func ensureSignalTestConsole() (func(), error) {
	var processID uint32
	if count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&processID)), 1); count != 0 {
		return func() {}, nil
	}
	if ok, _, err := allocConsole.Call(); ok == 0 {
		return nil, fmt.Errorf("allocating console for Ctrl+Break test: %w", err)
	}
	return func() { _, _, _ = freeConsole.Call() }, nil
}
