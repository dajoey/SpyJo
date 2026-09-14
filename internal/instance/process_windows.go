//go:build windows

package instance

import "os"

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p != nil
}

func isProcessExeDeleted(pid int) bool {
	return false
}

func terminateObsoleteProcess(pid int) {
}
