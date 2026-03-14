//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package pty

import "golang.org/x/sys/unix"

func termiosReadIoctl() uint {
	return unix.TIOCGETA
}

func termiosWriteIoctl() uint {
	return unix.TIOCSETA
}
