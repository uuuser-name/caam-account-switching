//go:build windows

package pty

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"testing"
	"time"
)

// TestWindowsNotSupported verifies that PTY operations correctly return
// ErrNotSupported on Windows platforms.
func TestWindowsNotSupported(t *testing.T) {
	t.Logf("[TEST] Running on Windows: %s/%s", runtime.GOOS, runtime.GOARCH)

	t.Run("NewController returns ErrNotSupported", func(t *testing.T) {
		cmd := exec.Command("cmd", "/c", "echo", "test")
		_, err := NewController(cmd, nil)
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
		t.Logf("[TEST] NewController error: %v", err)
	})

	t.Run("NewControllerFromArgs returns ErrNotSupported", func(t *testing.T) {
		_, err := NewControllerFromArgs("cmd", []string{"/c", "echo", "test"}, nil)
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
		t.Logf("[TEST] NewControllerFromArgs error: %v", err)
	})
}

// TestWindowsStubMethods verifies that the stub methods on windowsController
// all return ErrNotSupported (except Close which should be safe).
func TestWindowsStubMethods(t *testing.T) {
	t.Logf("[TEST] Testing Windows stub implementation")

	// Create a windowsController directly to test the stub methods
	ctrl := &windowsController{}

	t.Run("Start returns ErrNotSupported", func(t *testing.T) {
		err := ctrl.Start()
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("InjectCommand returns ErrNotSupported", func(t *testing.T) {
		err := ctrl.InjectCommand("test")
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("InjectRaw returns ErrNotSupported", func(t *testing.T) {
		err := ctrl.InjectRaw([]byte("test"))
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("ReadOutput returns ErrNotSupported", func(t *testing.T) {
		_, err := ctrl.ReadOutput()
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("ReadLine returns ErrNotSupported", func(t *testing.T) {
		ctx := context.Background()
		_, err := ctrl.ReadLine(ctx)
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("WaitForPattern returns ErrNotSupported", func(t *testing.T) {
		ctx := context.Background()
		pattern := regexp.MustCompile("test")
		_, err := ctrl.WaitForPattern(ctx, pattern, time.Second)
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("Wait returns ErrNotSupported", func(t *testing.T) {
		code, err := ctrl.Wait()
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
		if code != -1 {
			t.Errorf("expected exit code -1, got %d", code)
		}
	})

	t.Run("Signal returns ErrNotSupported", func(t *testing.T) {
		err := ctrl.Signal(SIGTERM)
		if err != ErrNotSupported {
			t.Errorf("expected ErrNotSupported, got %v", err)
		}
	})

	t.Run("Close returns nil", func(t *testing.T) {
		err := ctrl.Close()
		if err != nil {
			t.Errorf("expected nil error from Close, got %v", err)
		}
		t.Log("[TEST] Close succeeded (safe no-op)")
	})

	t.Run("Fd returns -1", func(t *testing.T) {
		fd := ctrl.Fd()
		if fd != -1 {
			t.Errorf("expected fd -1, got %d", fd)
		}
	})
}

// TestWindowsPlatformCheck verifies the platform detection.
func TestWindowsPlatformCheck(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("This test only runs on Windows")
	}

	t.Log("[TEST] Windows platform correctly detected")
	t.Logf("[TEST] OS: %s, Arch: %s", runtime.GOOS, runtime.GOARCH)
}
