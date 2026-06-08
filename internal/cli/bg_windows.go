//go:build windows

package cli

import (
	"os"
	"syscall"
)

// Windows process-creation flags (from winbase.h). DETACHED_PROCESS drops the
// console; CREATE_NEW_PROCESS_GROUP keeps the watcher alive after the launching
// console closes.
const (
	detachedProcess         = 0x00000008
	createNewProcessGroup   = 0x00000200
	processQueryLimitedInfo = 0x00001000
	stillActive             = 259
)

// detachSysProcAttr detaches the background watcher from the console.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

// processAlive reports whether a process with the given pid is currently running.
// Signal-based checks aren't available on Windows, so query the process exit code.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInfo, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// terminateProcess stops the watcher. Windows has no SIGTERM, so kill it.
func terminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
