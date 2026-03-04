//go:build !windows

package signals

import (
	"os"
	"testing"
)

func TestIsProcessAlive(t *testing.T) {
	if isProcessAlive(-1) {
		t.Fatal("expected negative pid to be treated as not alive")
	}
	if !isProcessAlive(os.Getpid()) {
		t.Fatal("expected current pid to be treated as alive")
	}
}
