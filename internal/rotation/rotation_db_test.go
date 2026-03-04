package rotation

import (
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
)

func TestSelectSmart_UsesLastActivationFromDB(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := caamdb.OpenAt(filepath.Join(tmpDir, "caam.db"))
	if err != nil {
		t.Fatalf("db.OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Truncate(time.Second)

	// Recently used profile "a".
	if err := db.LogEvent(caamdb.Event{
		Type:        caamdb.EventActivate,
		Provider:    "codex",
		ProfileName: "a",
		Timestamp:   now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("LogEvent(a) error = %v", err)
	}

	// Older use for profile "b".
	if err := db.LogEvent(caamdb.Event{
		Type:        caamdb.EventActivate,
		Provider:    "codex",
		ProfileName: "b",
		Timestamp:   now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("LogEvent(b) error = %v", err)
	}

	s := NewSelector(AlgorithmSmart, nil, db)
	result, err := s.Select("codex", []string{"a", "b"}, "")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selected != "b" {
		t.Fatalf("Selected = %q, want %q", result.Selected, "b")
	}
}

func TestSelectSmart_LastActivationNotLostWhenManyNonActivateEventsExist(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := caamdb.OpenAt(filepath.Join(tmpDir, "caam.db"))
	if err != nil {
		t.Fatalf("db.OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Truncate(time.Second)

	// Profile "a": recently activated (should be penalized), then many non-activate events.
	aActivated := now.Add(-5 * time.Minute)
	if err := db.LogEvent(caamdb.Event{
		Type:        caamdb.EventActivate,
		Provider:    "codex",
		ProfileName: "a",
		Timestamp:   aActivated,
	}); err != nil {
		t.Fatalf("LogEvent(a activate) error = %v", err)
	}

	// Ensure there are >25 newer events that are not activations, so a naive
	// GetEvents(limit=25)+filter(activate) approach would miss the activation.
	for i := 1; i <= 30; i++ {
		if err := db.LogEvent(caamdb.Event{
			Type:        caamdb.EventRefresh,
			Provider:    "codex",
			ProfileName: "a",
			Timestamp:   aActivated.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("LogEvent(a refresh %d) error = %v", i, err)
		}
	}

	// Profile "b": older activation (should be preferred).
	if err := db.LogEvent(caamdb.Event{
		Type:        caamdb.EventActivate,
		Provider:    "codex",
		ProfileName: "b",
		Timestamp:   now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("LogEvent(b activate) error = %v", err)
	}

	s := NewSelector(AlgorithmSmart, nil, db)
	s.SetRNG(rand.New(rand.NewSource(1)))

	result, err := s.Select("codex", []string{"a", "b"}, "")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selected != "b" {
		t.Fatalf("Selected = %q, want %q", result.Selected, "b")
	}
}
