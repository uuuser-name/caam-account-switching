package logs

import (
	"context"
	"testing"
	"time"
)

// mockScanner is a test implementation of Scanner
type mockScanner struct {
	logDir  string
	entries []*LogEntry
	err     error
}

func (m *mockScanner) Scan(ctx context.Context, logDir string, since time.Time) (*ScanResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ScanResult{
		Provider:      "mock",
		TotalEntries:  len(m.entries),
		ParsedEntries: len(m.entries),
		Since:         since,
		Until:         time.Now(),
		Entries:       m.entries,
	}, nil
}

func (m *mockScanner) LogDir() string {
	return m.logDir
}

func TestScannerInterface(t *testing.T) {
	// Verify mockScanner implements Scanner
	var _ Scanner = (*mockScanner)(nil)
}

func TestMultiScanner_Register(t *testing.T) {
	ms := NewMultiScanner()

	ms.Register("claude", &mockScanner{logDir: "/claude/logs"})
	ms.Register("codex", &mockScanner{logDir: "/codex/logs"})

	providers := ms.Providers()
	if len(providers) != 2 {
		t.Errorf("Providers() returned %d, want 2", len(providers))
	}
}

func TestMultiScanner_Scanner(t *testing.T) {
	ms := NewMultiScanner()
	mock := &mockScanner{logDir: "/test/logs"}
	ms.Register("test", mock)

	scanner := ms.Scanner("test")
	if scanner == nil {
		t.Fatal("Scanner() returned nil for registered provider")
	}

	if scanner.LogDir() != "/test/logs" {
		t.Errorf("Scanner().LogDir() = %q, want /test/logs", scanner.LogDir())
	}

	// Non-existent provider
	if ms.Scanner("nonexistent") != nil {
		t.Error("Scanner() should return nil for unregistered provider")
	}
}

func TestMultiScanner_ScanAll(t *testing.T) {
	ms := NewMultiScanner()

	ms.Register("claude", &mockScanner{
		logDir: "/claude/logs",
		entries: []*LogEntry{
			{Model: "claude-3-opus", InputTokens: 100, OutputTokens: 200},
		},
	})
	ms.Register("codex", &mockScanner{
		logDir: "/codex/logs",
		entries: []*LogEntry{
			{Model: "gpt-4o", InputTokens: 50, OutputTokens: 100},
		},
	})

	ctx := context.Background()
	since := time.Now().Add(-1 * time.Hour)

	results, err := ms.ScanAll(ctx, since)
	if err != nil {
		t.Fatalf("ScanAll() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("ScanAll() returned %d results, want 2", len(results))
	}

	claudeResult := results["claude"]
	if claudeResult == nil {
		t.Fatal("No result for claude")
	}
	if len(claudeResult.Entries) != 1 {
		t.Errorf("claude result has %d entries, want 1", len(claudeResult.Entries))
	}
}

func TestMultiScanner_ScanAllWithError(t *testing.T) {
	ms := NewMultiScanner()

	ms.Register("good", &mockScanner{
		entries: []*LogEntry{{Model: "test", InputTokens: 100}},
	})
	ms.Register("bad", &mockScanner{
		err: context.DeadlineExceeded,
	})

	ctx := context.Background()
	results, err := ms.ScanAll(ctx, time.Now())

	// ScanAll should not return error, just empty result for failed scanner
	if err != nil {
		t.Errorf("ScanAll() error = %v, want nil", err)
	}

	// Good scanner should have results
	goodResult := results["good"]
	if goodResult == nil || len(goodResult.Entries) != 1 {
		t.Error("Good scanner should have 1 entry")
	}

	// Bad scanner should have error tracked
	badResult := results["bad"]
	if badResult == nil {
		t.Fatal("Bad scanner should still have a result entry")
	}
	if badResult.ParseErrors != 1 {
		t.Errorf("Bad scanner ParseErrors = %d, want 1", badResult.ParseErrors)
	}
}

func TestMultiScanner_CombinedTokenUsage(t *testing.T) {
	ms := NewMultiScanner()

	ms.Register("claude", &mockScanner{
		entries: []*LogEntry{
			{Model: "claude-3-opus", InputTokens: 100, OutputTokens: 200},
		},
	})
	ms.Register("codex", &mockScanner{
		entries: []*LogEntry{
			{Model: "gpt-4o", InputTokens: 50, OutputTokens: 100},
		},
	})

	ctx := context.Background()
	usage, err := ms.CombinedTokenUsage(ctx, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("CombinedTokenUsage() error = %v", err)
	}

	// 100+200 from claude + 50+100 from codex = 450
	if usage.TotalTokens != 450 {
		t.Errorf("CombinedTokenUsage().TotalTokens = %d, want 450", usage.TotalTokens)
	}

	// Should have 2 models
	if len(usage.ByModel) != 2 {
		t.Errorf("CombinedTokenUsage() has %d models, want 2", len(usage.ByModel))
	}
}

func TestMultiScanner_Empty(t *testing.T) {
	ms := NewMultiScanner()

	providers := ms.Providers()
	if len(providers) != 0 {
		t.Errorf("Empty MultiScanner has %d providers, want 0", len(providers))
	}

	ctx := context.Background()
	results, err := ms.ScanAll(ctx, time.Now())
	if err != nil {
		t.Errorf("ScanAll() on empty scanner error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("ScanAll() on empty scanner has %d results, want 0", len(results))
	}
}

func TestNewMultiScanner(t *testing.T) {
	ms := NewMultiScanner()
	if ms == nil {
		t.Fatal("NewMultiScanner() returned nil")
	}
	if ms.scanners == nil {
		t.Error("NewMultiScanner().scanners is nil")
	}
}
