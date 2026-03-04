package db

import (
	"testing"
	"time"
)

func TestSessionTracker_StartEnd(t *testing.T) {
	s := NewSessionTracker()

	s.Start("codex", "work")
	time.Sleep(10 * time.Millisecond)
	d := s.End("codex", "work")
	if d <= 0 {
		t.Fatalf("duration = %v, want > 0", d)
	}

	// Ending again should return 0 since the session was cleared.
	if d2 := s.End("codex", "work"); d2 != 0 {
		t.Fatalf("second End() = %v, want 0", d2)
	}
}
