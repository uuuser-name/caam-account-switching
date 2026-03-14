//go:build !unix

package exec

import "os"

func duplicateInputSource(stdin *os.File) (*os.File, func(), error) {
	return stdin, func() {}, nil
}
