//go:build windows

package health

import (
	"os"
)

// No-op for Windows to allow compilation without complex syscall logic
func LockFile(f *os.File) error {
	return nil
}

func UnlockFile(f *os.File) error {
	return nil
}
