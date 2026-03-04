//go:build !windows

package cmd

import "syscall"

// getSysProcAttr returns process attributes for daemonization on Unix systems.
func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true, // Create new session, detach from terminal
	}
}
