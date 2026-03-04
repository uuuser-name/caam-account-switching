package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRecordWrapSession(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	session := WrapSession{
		Provider:     "claude",
		ProfileName:  "test-profile",
		StartedAt:    time.Now().Add(-5 * time.Minute),
		EndedAt:      time.Now(),
		ExitCode:     0,
		RateLimitHit: false,
		Notes:        "test session",
	}

	if err := db.RecordWrapSession(session); err != nil {
		t.Fatalf("RecordWrapSession() error = %v", err)
	}

	// Verify session was recorded
	sessions, err := db.GetWrapSessions("claude", time.Time{}, 10)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	if sessions[0].Provider != "claude" {
		t.Errorf("Provider = %s, want claude", sessions[0].Provider)
	}
	if sessions[0].ProfileName != "test-profile" {
		t.Errorf("ProfileName = %s, want test-profile", sessions[0].ProfileName)
	}
	if sessions[0].DurationSeconds < 299 || sessions[0].DurationSeconds > 301 {
		t.Errorf("DurationSeconds = %d, want ~300", sessions[0].DurationSeconds)
	}
}

func TestRecordWrapSession_RateLimitHit(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	session := WrapSession{
		Provider:     "codex",
		ProfileName:  "work",
		RateLimitHit: true,
		Notes:        "retries: 2",
	}

	if err := db.RecordWrapSession(session); err != nil {
		t.Fatalf("RecordWrapSession() error = %v", err)
	}

	sessions, err := db.GetWrapSessions("codex", time.Time{}, 10)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	if !sessions[0].RateLimitHit {
		t.Error("RateLimitHit = false, want true")
	}
}

func TestRecordWrapSession_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Missing provider
	err = db.RecordWrapSession(WrapSession{ProfileName: "test"})
	if err == nil {
		t.Error("Expected error for missing provider")
	}

	// Missing profile
	err = db.RecordWrapSession(WrapSession{Provider: "claude"})
	if err == nil {
		t.Error("Expected error for missing profile")
	}
}

func TestGetCostRate(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Check default rates are populated
	rate, err := db.GetCostRate("claude")
	if err != nil {
		t.Fatalf("GetCostRate() error = %v", err)
	}

	if rate == nil {
		t.Fatal("Expected rate for claude, got nil")
	}
	if rate.CentsPerMinute != 5 {
		t.Errorf("CentsPerMinute = %d, want 5", rate.CentsPerMinute)
	}
}

func TestSetCostRate(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Update existing rate
	if err := db.SetCostRate("claude", 10, 25); err != nil {
		t.Fatalf("SetCostRate() error = %v", err)
	}

	rate, err := db.GetCostRate("claude")
	if err != nil {
		t.Fatalf("GetCostRate() error = %v", err)
	}

	if rate.CentsPerMinute != 10 {
		t.Errorf("CentsPerMinute = %d, want 10", rate.CentsPerMinute)
	}
	if rate.CentsPerSession != 25 {
		t.Errorf("CentsPerSession = %d, want 25", rate.CentsPerSession)
	}

	// Add new provider rate
	if err := db.SetCostRate("new-provider", 1, 2); err != nil {
		t.Fatalf("SetCostRate() error = %v", err)
	}

	rate, err = db.GetCostRate("new-provider")
	if err != nil {
		t.Fatalf("GetCostRate() error = %v", err)
	}

	if rate == nil {
		t.Fatal("Expected rate for new-provider, got nil")
	}
}

func TestGetAllCostRates(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	rates, err := db.GetAllCostRates()
	if err != nil {
		t.Fatalf("GetAllCostRates() error = %v", err)
	}

	// Should have 3 default rates
	if len(rates) != 3 {
		t.Errorf("Expected 3 rates, got %d", len(rates))
	}

	// Should be sorted by provider
	providers := make([]string, len(rates))
	for i, r := range rates {
		providers[i] = r.Provider
	}
	if providers[0] != "claude" || providers[1] != "codex" || providers[2] != "gemini" {
		t.Errorf("Providers not sorted: %v", providers)
	}
}

func TestGetWrapSessions_Filter(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Insert sessions for different providers
	now := time.Now()
	sessions := []WrapSession{
		{Provider: "claude", ProfileName: "p1", StartedAt: now.Add(-10 * time.Minute)},
		{Provider: "claude", ProfileName: "p2", StartedAt: now.Add(-5 * time.Minute)},
		{Provider: "codex", ProfileName: "p1", StartedAt: now.Add(-3 * time.Minute)},
	}

	for _, s := range sessions {
		if err := db.RecordWrapSession(s); err != nil {
			t.Fatalf("RecordWrapSession() error = %v", err)
		}
	}

	// Get all sessions
	all, err := db.GetWrapSessions("", time.Time{}, 10)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(all))
	}

	// Filter by provider
	claudeSessions, err := db.GetWrapSessions("claude", time.Time{}, 10)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}
	if len(claudeSessions) != 2 {
		t.Errorf("Expected 2 claude sessions, got %d", len(claudeSessions))
	}

	// Filter by time
	recentSessions, err := db.GetWrapSessions("", now.Add(-6*time.Minute), 10)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}
	if len(recentSessions) != 2 {
		t.Errorf("Expected 2 recent sessions, got %d", len(recentSessions))
	}

	// Limit
	limited, err := db.GetWrapSessions("", time.Time{}, 2)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("Expected 2 limited sessions, got %d", len(limited))
	}
}

func TestGetCostSummary(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Insert sessions
	now := time.Now()
	sessions := []WrapSession{
		{Provider: "claude", ProfileName: "p1", StartedAt: now.Add(-10 * time.Minute), EndedAt: now.Add(-5 * time.Minute)},
		{Provider: "claude", ProfileName: "p2", StartedAt: now.Add(-5 * time.Minute), EndedAt: now, RateLimitHit: true},
		{Provider: "codex", ProfileName: "p1", StartedAt: now.Add(-3 * time.Minute), EndedAt: now},
	}

	for _, s := range sessions {
		if err := db.RecordWrapSession(s); err != nil {
			t.Fatalf("RecordWrapSession() error = %v", err)
		}
	}

	// Get summary for all providers
	summaries, err := db.GetCostSummary("", time.Time{})
	if err != nil {
		t.Fatalf("GetCostSummary() error = %v", err)
	}

	if len(summaries) != 2 {
		t.Fatalf("Expected 2 provider summaries, got %d", len(summaries))
	}

	// Find claude summary
	var claudeSummary *CostSummary
	for i := range summaries {
		if summaries[i].Provider == "claude" {
			claudeSummary = &summaries[i]
			break
		}
	}

	if claudeSummary == nil {
		t.Fatal("Expected claude summary")
	}

	if claudeSummary.TotalSessions != 2 {
		t.Errorf("TotalSessions = %d, want 2", claudeSummary.TotalSessions)
	}
	if claudeSummary.RateLimitHits != 1 {
		t.Errorf("RateLimitHits = %d, want 1", claudeSummary.RateLimitHits)
	}

	// Get summary for single provider
	claudeOnly, err := db.GetCostSummary("claude", time.Time{})
	if err != nil {
		t.Fatalf("GetCostSummary() error = %v", err)
	}
	if len(claudeOnly) != 1 {
		t.Errorf("Expected 1 claude summary, got %d", len(claudeOnly))
	}
}

func TestGetTotalCost(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Insert sessions with calculated costs
	now := time.Now()
	sessions := []WrapSession{
		{Provider: "claude", ProfileName: "p1", StartedAt: now.Add(-10 * time.Minute), EndedAt: now.Add(-5 * time.Minute)},
		{Provider: "claude", ProfileName: "p2", StartedAt: now.Add(-5 * time.Minute), EndedAt: now},
	}

	for _, s := range sessions {
		if err := db.RecordWrapSession(s); err != nil {
			t.Fatalf("RecordWrapSession() error = %v", err)
		}
	}

	total, err := db.GetTotalCost(time.Time{})
	if err != nil {
		t.Fatalf("GetTotalCost() error = %v", err)
	}

	// Each session is 5 minutes, claude rate is 5 cents/min
	// Total should be 2 * 5 * 5 = 50 cents
	if total != 50 {
		t.Errorf("TotalCost = %d, want 50", total)
	}
}

func TestRecordWrapSession_CalculatesCost(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Record a 10 minute claude session
	session := WrapSession{
		Provider:    "claude",
		ProfileName: "test",
		StartedAt:   time.Now().Add(-10 * time.Minute),
		EndedAt:     time.Now(),
	}

	if err := db.RecordWrapSession(session); err != nil {
		t.Fatalf("RecordWrapSession() error = %v", err)
	}

	sessions, err := db.GetWrapSessions("claude", time.Time{}, 10)
	if err != nil {
		t.Fatalf("GetWrapSessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Fatal("Expected 1 session")
	}

	// Claude default rate is 5 cents/min, 10 min = 50 cents
	if sessions[0].EstimatedCostCents != 50 {
		t.Errorf("EstimatedCostCents = %d, want 50", sessions[0].EstimatedCostCents)
	}
}

func TestCostRate_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	// Empty provider
	err = db.SetCostRate("", 5, 0)
	if err == nil {
		t.Error("Expected error for empty provider")
	}

	// Get empty provider
	_, err = db.GetCostRate("")
	if err == nil {
		t.Error("Expected error for empty provider")
	}
}

func TestGetCostRate_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenAt(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer db.Close()

	rate, err := db.GetCostRate("nonexistent")
	if err != nil {
		t.Fatalf("GetCostRate() error = %v", err)
	}

	if rate != nil {
		t.Errorf("Expected nil rate for nonexistent provider, got %+v", rate)
	}
}
