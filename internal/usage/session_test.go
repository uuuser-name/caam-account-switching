package usage

import (
	"sync"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/logs"
)

func TestNewSessionTracker(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		tracker := NewSessionTracker()
		if tracker.windowSize != 2*time.Hour {
			t.Errorf("windowSize = %v, expected 2h", tracker.windowSize)
		}
		if tracker.maxEntries != 10000 {
			t.Errorf("maxEntries = %d, expected 10000", tracker.maxEntries)
		}
	})

	t.Run("with options", func(t *testing.T) {
		tracker := NewSessionTracker(
			WithWindowSize(time.Hour),
			WithMaxEntries(100),
		)
		if tracker.windowSize != time.Hour {
			t.Errorf("windowSize = %v, expected 1h", tracker.windowSize)
		}
		if tracker.maxEntries != 100 {
			t.Errorf("maxEntries = %d, expected 100", tracker.maxEntries)
		}
	})

	t.Run("with callback", func(t *testing.T) {
		var called bool
		tracker := NewSessionTracker(
			WithOnUsage(func(e TokenEntry) {
				called = true
			}),
		)
		tracker.Record(TokenEntry{InputTokens: 100})
		if !called {
			t.Error("callback was not invoked")
		}
	})
}

func TestSessionTracker_Record(t *testing.T) {
	tracker := NewSessionTracker()

	entry := TokenEntry{
		Model:        "claude-3-opus",
		InputTokens:  100,
		OutputTokens: 200,
		Source:       "api_response",
	}

	tracker.Record(entry)

	if tracker.EntryCount() != 1 {
		t.Errorf("EntryCount = %d, expected 1", tracker.EntryCount())
	}

	if tracker.TotalTokens() != 300 {
		t.Errorf("TotalTokens = %d, expected 300", tracker.TotalTokens())
	}

	input, output, cache := tracker.TokensByType()
	if input != 100 {
		t.Errorf("InputTokens = %d, expected 100", input)
	}
	if output != 200 {
		t.Errorf("OutputTokens = %d, expected 200", output)
	}
	if cache != 0 {
		t.Errorf("CacheTokens = %d, expected 0", cache)
	}
}

func TestSessionTracker_RecordFromResponse(t *testing.T) {
	tracker := NewSessionTracker()

	tracker.RecordFromResponse("gpt-4", 500, 1000, "conv-123", "msg-456")

	if tracker.EntryCount() != 1 {
		t.Errorf("EntryCount = %d, expected 1", tracker.EntryCount())
	}

	stats := tracker.Stats()
	if stats.InputTokens != 500 {
		t.Errorf("InputTokens = %d, expected 500", stats.InputTokens)
	}
	if stats.OutputTokens != 1000 {
		t.Errorf("OutputTokens = %d, expected 1000", stats.OutputTokens)
	}
	if stats.BySource["api_response"] != 1500 {
		t.Errorf("BySource[api_response] = %d, expected 1500", stats.BySource["api_response"])
	}
}

func TestSessionTracker_EstimateFromStream(t *testing.T) {
	tracker := NewSessionTracker()

	// 40 chars should estimate to ~10 tokens (40/4)
	tracker.EstimateFromStream("claude-3-opus", "This is a test message with 40 chars...")

	if tracker.EntryCount() != 1 {
		t.Errorf("EntryCount = %d, expected 1", tracker.EntryCount())
	}

	stats := tracker.Stats()
	if stats.OutputTokens < 5 || stats.OutputTokens > 15 {
		t.Errorf("OutputTokens = %d, expected ~10", stats.OutputTokens)
	}
	if stats.BySource["stream_estimate"] == 0 {
		t.Error("expected stream_estimate source")
	}
}

func TestSessionTracker_EstimateFromStream_Empty(t *testing.T) {
	tracker := NewSessionTracker()
	tracker.EstimateFromStream("model", "")

	if tracker.EntryCount() != 0 {
		t.Errorf("EntryCount = %d, expected 0 for empty chunk", tracker.EntryCount())
	}
}

func TestSessionTracker_EstimateFromStream_MinimumToken(t *testing.T) {
	tracker := NewSessionTracker()

	// Very short string should still count as 1 token
	tracker.EstimateFromStream("model", "Hi")

	stats := tracker.Stats()
	if stats.OutputTokens != 1 {
		t.Errorf("OutputTokens = %d, expected 1 minimum", stats.OutputTokens)
	}
}

func TestSessionTracker_MaxEntriesLimit(t *testing.T) {
	tracker := NewSessionTracker(WithMaxEntries(5))

	// Add 10 entries
	for i := 0; i < 10; i++ {
		tracker.Record(TokenEntry{
			InputTokens: int64(i * 100),
		})
	}

	if tracker.EntryCount() != 5 {
		t.Errorf("EntryCount = %d, expected 5 (max limit)", tracker.EntryCount())
	}

	// Should have kept the most recent 5 (indices 5-9)
	// Tokens: 500 + 600 + 700 + 800 + 900 = 3500
	if tracker.TotalTokens() != 3500 {
		t.Errorf("TotalTokens = %d, expected 3500", tracker.TotalTokens())
	}
}

func TestSessionTracker_BurnRate(t *testing.T) {
	tracker := NewSessionTracker()
	now := time.Now()

	// Add entries spread over 30 minutes
	entries := []TokenEntry{
		{Timestamp: now.Add(-30 * time.Minute), InputTokens: 500, OutputTokens: 500},
		{Timestamp: now.Add(-20 * time.Minute), InputTokens: 500, OutputTokens: 500},
		{Timestamp: now.Add(-10 * time.Minute), InputTokens: 500, OutputTokens: 500},
	}

	for _, e := range entries {
		tracker.Record(e)
	}

	burnRate := tracker.BurnRate(time.Hour)
	if burnRate == nil {
		t.Fatal("expected non-nil burn rate")
	}

	// 3000 tokens over 20 minutes = 9000 tokens/hr
	if burnRate.TokensPerHour < 8000 || burnRate.TokensPerHour > 10000 {
		t.Errorf("TokensPerHour = %.2f, expected ~9000", burnRate.TokensPerHour)
	}

	t.Logf("BurnRate: %v", burnRate.String())
}

func TestSessionTracker_BurnRate_InsufficientData(t *testing.T) {
	tracker := NewSessionTracker()

	// Only 1 entry - not enough
	tracker.Record(TokenEntry{InputTokens: 1000})

	burnRate := tracker.BurnRate(time.Hour)
	if burnRate != nil {
		t.Error("expected nil burn rate with insufficient data")
	}
}

func TestSessionTracker_WindowFiltering(t *testing.T) {
	tracker := NewSessionTracker()
	now := time.Now()

	// Add entries at various times
	tracker.Record(TokenEntry{Timestamp: now.Add(-2 * time.Hour), InputTokens: 1000}) // Outside 1hr window
	tracker.Record(TokenEntry{Timestamp: now.Add(-30 * time.Minute), InputTokens: 500})
	tracker.Record(TokenEntry{Timestamp: now.Add(-10 * time.Minute), InputTokens: 500})

	// Should only count entries within window
	tokensIn1Hr := tracker.TokensInWindow(time.Hour)
	if tokensIn1Hr != 1000 {
		t.Errorf("TokensInWindow(1h) = %d, expected 1000", tokensIn1Hr)
	}

	entriesIn1Hr := tracker.EntriesInWindow(time.Hour)
	if entriesIn1Hr != 2 {
		t.Errorf("EntriesInWindow(1h) = %d, expected 2", entriesIn1Hr)
	}
}

func TestSessionTracker_ByModel(t *testing.T) {
	tracker := NewSessionTracker()

	tracker.Record(TokenEntry{Model: "claude-3-opus", InputTokens: 1000})
	tracker.Record(TokenEntry{Model: "claude-3-opus", InputTokens: 500})
	tracker.Record(TokenEntry{Model: "claude-3-sonnet", InputTokens: 200})

	byModel := tracker.ByModel()

	if byModel["claude-3-opus"] != 1500 {
		t.Errorf("opus tokens = %d, expected 1500", byModel["claude-3-opus"])
	}
	if byModel["claude-3-sonnet"] != 200 {
		t.Errorf("sonnet tokens = %d, expected 200", byModel["claude-3-sonnet"])
	}
}

func TestSessionTracker_BySource(t *testing.T) {
	tracker := NewSessionTracker()

	tracker.Record(TokenEntry{Source: "api_response", InputTokens: 1000})
	tracker.Record(TokenEntry{Source: "api_response", InputTokens: 500})
	tracker.Record(TokenEntry{Source: "stream_estimate", OutputTokens: 200})

	bySource := tracker.BySource()

	if bySource["api_response"] != 1500 {
		t.Errorf("api_response = %d, expected 1500", bySource["api_response"])
	}
	if bySource["stream_estimate"] != 200 {
		t.Errorf("stream_estimate = %d, expected 200", bySource["stream_estimate"])
	}
}

func TestSessionTracker_Clear(t *testing.T) {
	tracker := NewSessionTracker()

	tracker.Record(TokenEntry{InputTokens: 1000})
	tracker.Record(TokenEntry{InputTokens: 2000})

	if tracker.TotalTokens() != 3000 {
		t.Fatalf("expected 3000 tokens before clear")
	}

	tracker.Clear()

	if tracker.EntryCount() != 0 {
		t.Errorf("EntryCount after clear = %d, expected 0", tracker.EntryCount())
	}
	if tracker.TotalTokens() != 0 {
		t.Errorf("TotalTokens after clear = %d, expected 0", tracker.TotalTokens())
	}
}

func TestSessionTracker_Prune(t *testing.T) {
	tracker := NewSessionTracker(WithWindowSize(time.Hour))
	now := time.Now()

	// Add old and new entries
	tracker.Record(TokenEntry{Timestamp: now.Add(-2 * time.Hour), InputTokens: 1000})
	tracker.Record(TokenEntry{Timestamp: now.Add(-30 * time.Minute), InputTokens: 500})

	pruned := tracker.Prune()

	if pruned != 1 {
		t.Errorf("pruned = %d, expected 1", pruned)
	}
	if tracker.EntryCount() != 1 {
		t.Errorf("EntryCount after prune = %d, expected 1", tracker.EntryCount())
	}
	if tracker.TotalTokens() != 500 {
		t.Errorf("TotalTokens after prune = %d, expected 500", tracker.TotalTokens())
	}
}

func TestSessionTracker_MergeLogData(t *testing.T) {
	tracker := NewSessionTracker()
	now := time.Now()

	// Add an existing entry
	tracker.Record(TokenEntry{
		Timestamp:   now.Add(-30 * time.Minute),
		MessageID:   "msg-existing",
		InputTokens: 100,
		Source:      "api_response",
	})

	logEntries := []*logs.LogEntry{
		// New entry (should be added)
		{
			Timestamp:   now.Add(-20 * time.Minute),
			MessageID:   "msg-new",
			InputTokens: 500,
		},
		// Duplicate by message ID (should be skipped)
		{
			Timestamp:   now.Add(-30 * time.Minute),
			MessageID:   "msg-existing",
			InputTokens: 100,
		},
	}

	added := tracker.MergeLogData(logEntries)

	if added != 1 {
		t.Errorf("added = %d, expected 1", added)
	}
	if tracker.EntryCount() != 2 {
		t.Errorf("EntryCount = %d, expected 2", tracker.EntryCount())
	}
}

func TestSessionTracker_MergeLogData_UpdateEstimates(t *testing.T) {
	tracker := NewSessionTracker()
	now := time.Now()

	// Add a stream estimate
	tracker.Record(TokenEntry{
		Timestamp:    now.Add(-30 * time.Minute),
		MessageID:    "msg-123",
		OutputTokens: 50, // Estimate
		Source:       "stream_estimate",
	})

	// Merge log data with actual values
	logEntries := []*logs.LogEntry{
		{
			Timestamp:    now.Add(-30 * time.Minute),
			MessageID:    "msg-123",
			OutputTokens: 100, // Actual
		},
	}

	tracker.MergeLogData(logEntries)

	// Should still have 1 entry, but updated with log data
	if tracker.EntryCount() != 1 {
		t.Errorf("EntryCount = %d, expected 1", tracker.EntryCount())
	}

	// Should have the more accurate log value
	stats := tracker.Stats()
	if stats.OutputTokens != 100 {
		t.Errorf("OutputTokens = %d, expected 100 (from log)", stats.OutputTokens)
	}
	if stats.BySource["log_parse"] != 100 {
		t.Errorf("BySource[log_parse] = %d, expected 100", stats.BySource["log_parse"])
	}
}

func TestSessionTracker_MergeLogData_DedupByTimestamp(t *testing.T) {
	tracker := NewSessionTracker()
	now := time.Now()

	// Existing entry without message ID
	tracker.Record(TokenEntry{
		Timestamp:   now.Add(-10 * time.Minute),
		InputTokens: 100,
		Source:      "api_response",
	})

	logEntries := []*logs.LogEntry{
		// Within 1s of existing timestamp -> should be deduped
		{
			Timestamp:   now.Add(-10*time.Minute + 500*time.Millisecond),
			InputTokens: 200,
		},
		// Outside 1s window -> should be added
		{
			Timestamp:   now.Add(-8 * time.Minute),
			InputTokens: 300,
		},
	}

	added := tracker.MergeLogData(logEntries)
	if added != 1 {
		t.Errorf("added = %d, expected 1", added)
	}
	if tracker.EntryCount() != 2 {
		t.Errorf("EntryCount = %d, expected 2", tracker.EntryCount())
	}
}

func TestSessionTracker_Snapshot(t *testing.T) {
	tracker := NewSessionTracker()

	tracker.Record(TokenEntry{InputTokens: 100})
	tracker.Record(TokenEntry{InputTokens: 200})

	snapshot := tracker.Snapshot()

	if len(snapshot) != 2 {
		t.Errorf("snapshot length = %d, expected 2", len(snapshot))
	}

	// Modify snapshot shouldn't affect tracker
	snapshot[0].InputTokens = 999

	stats := tracker.Stats()
	if stats.InputTokens == 999 {
		t.Error("snapshot modification affected tracker")
	}
}

func TestSessionTracker_Stats(t *testing.T) {
	tracker := NewSessionTracker(WithWindowSize(time.Hour))
	now := time.Now()

	tracker.Record(TokenEntry{
		Timestamp:    now.Add(-30 * time.Minute),
		Model:        "claude-3-opus",
		InputTokens:  500,
		OutputTokens: 1000,
		Source:       "api_response",
	})
	tracker.Record(TokenEntry{
		Timestamp:    now.Add(-10 * time.Minute),
		Model:        "claude-3-sonnet",
		InputTokens:  200,
		OutputTokens: 400,
		CacheTokens:  50,
		Source:       "api_response",
	})

	stats := tracker.Stats()

	if stats.TotalEntries != 2 {
		t.Errorf("TotalEntries = %d, expected 2", stats.TotalEntries)
	}
	if stats.TotalTokens != 2150 {
		t.Errorf("TotalTokens = %d, expected 2150", stats.TotalTokens)
	}
	if stats.InputTokens != 700 {
		t.Errorf("InputTokens = %d, expected 700", stats.InputTokens)
	}
	if stats.OutputTokens != 1400 {
		t.Errorf("OutputTokens = %d, expected 1400", stats.OutputTokens)
	}
	if stats.CacheTokens != 50 {
		t.Errorf("CacheTokens = %d, expected 50", stats.CacheTokens)
	}
	if stats.WindowSize != time.Hour {
		t.Errorf("WindowSize = %v, expected 1h", stats.WindowSize)
	}
	if len(stats.ByModel) != 2 {
		t.Errorf("ByModel count = %d, expected 2", len(stats.ByModel))
	}
}

func TestSessionTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewSessionTracker()
	var wg sync.WaitGroup

	// Spawn multiple goroutines writing
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tracker.Record(TokenEntry{
					InputTokens: int64(id*100 + j),
					Source:      "test",
				})
			}
		}(i)
	}

	// Spawn readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = tracker.TotalTokens()
				_ = tracker.Stats()
				_ = tracker.BurnRate(time.Hour)
			}
		}()
	}

	wg.Wait()

	// Should have 1000 entries
	if tracker.EntryCount() != 1000 {
		t.Errorf("EntryCount = %d, expected 1000", tracker.EntryCount())
	}
}

func TestTokenEntry_TotalTokens(t *testing.T) {
	entry := TokenEntry{
		InputTokens:  100,
		OutputTokens: 200,
		CacheTokens:  50,
	}

	if entry.TotalTokens() != 350 {
		t.Errorf("TotalTokens = %d, expected 350", entry.TotalTokens())
	}
}

func TestSessionTracker_TimestampAutoSet(t *testing.T) {
	tracker := NewSessionTracker()

	before := time.Now()
	tracker.Record(TokenEntry{InputTokens: 100})
	after := time.Now()

	snapshot := tracker.Snapshot()
	if len(snapshot) != 1 {
		t.Fatal("expected 1 entry")
	}

	ts := snapshot[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Error("timestamp was not auto-set correctly")
	}
}

// TestSessionTracker_CallbackNoDeadlock verifies that the onUsage callback
// can safely call SessionTracker methods without deadlocking.
// This is a regression test for a bug where the callback was called
// while holding the mutex lock.
func TestSessionTracker_CallbackNoDeadlock(t *testing.T) {
	var callbackTokens int64
	var callbackCount int
	var tracker *SessionTracker

	tracker = NewSessionTracker(WithOnUsage(func(entry TokenEntry) {
		// This would deadlock if callback is called while holding the lock
		callbackTokens = tracker.TotalTokens()
		callbackCount = tracker.EntryCount()
	}))

	// Record some entries - if there's a deadlock, this will hang
	tracker.Record(TokenEntry{InputTokens: 100, OutputTokens: 200})
	tracker.Record(TokenEntry{InputTokens: 150, OutputTokens: 250})

	// Verify callback was invoked and could access tracker methods
	if callbackTokens != 700 { // 100+200+150+250
		t.Errorf("callbackTokens = %d, expected 700", callbackTokens)
	}
	if callbackCount != 2 {
		t.Errorf("callbackCount = %d, expected 2", callbackCount)
	}
}
