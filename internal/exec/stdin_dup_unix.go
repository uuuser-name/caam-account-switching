//go:build unix

package exec

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func duplicateInputSource(stdin *os.File) (*os.File, func(), error) {
	if stdin == nil {
		return nil, func() {}, nil
	}

	dupFD, err := unix.Dup(int(stdin.Fd()))
	if err != nil {
		return nil, nil, fmt.Errorf("dup stdin: %w", err)
	}
	unix.CloseOnExec(dupFD)

	relayInput := os.NewFile(uintptr(dupFD), stdin.Name()+"-relay")
	if relayInput == nil {
		_ = unix.Close(dupFD)
		return nil, nil, fmt.Errorf("wrap duplicated stdin fd")
	}

	return relayInput, func() {
		_ = relayInput.Close()
	}, nil
}
