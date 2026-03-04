package watcher

import (
	"testing"
	"time"
)

func TestDebouncer_ShouldEmit(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	if got := d.ShouldEmit("codex/work"); !got {
		t.Fatalf("first ShouldEmit() = false, want true")
	}
	if got := d.ShouldEmit("codex/work"); got {
		t.Fatalf("second ShouldEmit() = true, want false")
	}

	time.Sleep(120 * time.Millisecond)
	if got := d.ShouldEmit("codex/work"); !got {
		t.Fatalf("after delay ShouldEmit() = false, want true")
	}
}

// Test newDebouncer with zero delay - should use default
func TestNewDebouncer_ZeroDelay(t *testing.T) {
	d := newDebouncer(0)
	if d == nil {
		t.Fatal("newDebouncer(0) returned nil")
	}
	if d.delay != 100*time.Millisecond {
		t.Errorf("delay = %v, want 100ms default", d.delay)
	}
}

// Test newDebouncer with negative delay - should use default
func TestNewDebouncer_NegativeDelay(t *testing.T) {
	d := newDebouncer(-50 * time.Millisecond)
	if d == nil {
		t.Fatal("newDebouncer(-50ms) returned nil")
	}
	if d.delay != 100*time.Millisecond {
		t.Errorf("delay = %v, want 100ms default", d.delay)
	}
}

// Test ShouldEmit with nil debouncer - should return true
func TestDebouncer_ShouldEmit_Nil(t *testing.T) {
	var d *debouncer
	if got := d.ShouldEmit("codex/work"); !got {
		t.Error("ShouldEmit() on nil debouncer = false, want true")
	}
}

// Test ShouldEmit with empty key - should always return true
func TestDebouncer_ShouldEmit_EmptyKey(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	// First call with empty key
	if got := d.ShouldEmit(""); !got {
		t.Error("ShouldEmit(\"\") first call = false, want true")
	}
	// Second call with empty key - should still be true (empty keys not tracked)
	if got := d.ShouldEmit(""); !got {
		t.Error("ShouldEmit(\"\") second call = false, want true")
	}
}

// Test that cleanupLocked is triggered after 100 calls
func TestDebouncer_CleanupTriggered(t *testing.T) {
	d := newDebouncer(1 * time.Millisecond)

	// Add some entries that will be old enough to clean up
	d.mu.Lock()
	d.last["old/entry1"] = time.Now().Add(-2 * time.Minute)
	d.last["old/entry2"] = time.Now().Add(-2 * time.Minute)
	d.mu.Unlock()

	// Call ShouldEmit 100 times to trigger cleanup
	for i := 0; i < 100; i++ {
		d.ShouldEmit("key" + string(rune(i)))
	}

	// After cleanup, old entries should be removed
	d.mu.Lock()
	_, hasOld1 := d.last["old/entry1"]
	_, hasOld2 := d.last["old/entry2"]
	d.mu.Unlock()

	if hasOld1 {
		t.Error("old/entry1 should have been cleaned up")
	}
	if hasOld2 {
		t.Error("old/entry2 should have been cleaned up")
	}
}

// Test cleanupLocked preserves recent entries
func TestDebouncer_CleanupPreservesRecent(t *testing.T) {
	d := newDebouncer(1 * time.Millisecond)

	// Add a recent entry
	recentKey := "recent/entry"
	d.ShouldEmit(recentKey)

	// Call ShouldEmit 100 times to trigger cleanup
	for i := 0; i < 100; i++ {
		d.ShouldEmit("key" + string(rune(i)))
	}

	// Recent entry should still be present
	d.mu.Lock()
	_, hasRecent := d.last[recentKey]
	d.mu.Unlock()

	if !hasRecent {
		t.Error("recent entry should have been preserved")
	}
}

// Test debouncer with multiple different keys
func TestDebouncer_MultipleKeys(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	// Different keys should not interfere
	if got := d.ShouldEmit("codex/work"); !got {
		t.Error("ShouldEmit(codex/work) = false, want true")
	}
	if got := d.ShouldEmit("claude/home"); !got {
		t.Error("ShouldEmit(claude/home) = false, want true")
	}
	if got := d.ShouldEmit("gemini/proj"); !got {
		t.Error("ShouldEmit(gemini/proj) = false, want true")
	}

	// Same keys should be debounced
	if got := d.ShouldEmit("codex/work"); got {
		t.Error("second ShouldEmit(codex/work) = true, want false")
	}
	if got := d.ShouldEmit("claude/home"); got {
		t.Error("second ShouldEmit(claude/home) = true, want false")
	}
}
