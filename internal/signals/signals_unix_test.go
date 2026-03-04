//go:build !windows

package signals

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestHandler_ReloadOnHUP(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if err := SendHUP(os.Getpid()); err != nil {
		t.Fatalf("SendHUP: %v", err)
	}

	select {
	case <-h.Reload():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reload event")
	}
}

func TestHandler_DumpOnUSR1(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case <-h.DumpStats():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dump event")
	}
}

func TestHandler_ShutdownOnTERM(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Close()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case <-h.Shutdown():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shutdown event")
	}
}

func TestNilHandler(t *testing.T) {
	var h *Handler

	// All methods should return nil/no-op on nil handler
	if ch := h.Reload(); ch != nil {
		t.Error("Reload on nil handler should return nil")
	}
	if ch := h.Shutdown(); ch != nil {
		t.Error("Shutdown on nil handler should return nil")
	}
	if ch := h.DumpStats(); ch != nil {
		t.Error("DumpStats on nil handler should return nil")
	}
	if err := h.Close(); err != nil {
		t.Errorf("Close on nil handler should not error: %v", err)
	}
}

func TestHandler_CloseWithNilStop(t *testing.T) {
	// Create a handler but set stop to nil to test that branch
	h := &Handler{
		reload:   make(chan struct{}, 1),
		shutdown: make(chan os.Signal, 1),
		dump:     make(chan struct{}, 1),
		stop:     nil, // nil stop function
	}

	if err := h.Close(); err != nil {
		t.Errorf("Close with nil stop should not error: %v", err)
	}
}
