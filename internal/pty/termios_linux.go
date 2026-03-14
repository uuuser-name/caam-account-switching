//go:build linux

package pty

import "golang.org/x/sys/unix"

func termiosReadIoctl() uint {
	return unix.TCGETS
}

func termiosWriteIoctl() uint {
	return unix.TCSETS
}
