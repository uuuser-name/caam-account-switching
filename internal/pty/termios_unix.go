//go:build unix

package pty

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func disableTTYEcho(tty *os.File) error {
	if tty == nil {
		return nil
	}

	state, err := unix.IoctlGetTermios(int(tty.Fd()), termiosReadIoctl())
	if err != nil {
		return fmt.Errorf("get termios: %w", err)
	}
	state.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(int(tty.Fd()), termiosWriteIoctl(), state); err != nil {
		return fmt.Errorf("set termios: %w", err)
	}
	return nil
}
