//go:build !windows

package instance

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func isProcessExeDeleted(pid int) bool {
	if pid <= 0 {
		return false
	}
	exeLink, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	return strings.Contains(exeLink, "(deleted)")
}

func terminateObsoleteProcess(pid int) {
	if pid <= 0 || pid == os.Getpid() {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
}
