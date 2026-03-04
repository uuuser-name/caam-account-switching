//go:build windows

package cmd

import "syscall"

// getSysProcAttr returns process attributes for daemonization on Windows.
func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// Windows doesn't have the same daemonization concept as Unix
		// The process will run detached when started this way
	}
}
