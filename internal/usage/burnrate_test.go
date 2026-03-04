package usage

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/logs"
)

func TestCalculateBurnRate_BasicCalculation(t *testing.T) {
	now := time.Now()

	// Create entries spread over 30 minutes, 1000 tokens each
	entries := []*logs.LogEntry{
		{Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1000, Model: "claude-3-opus"},
		{Timestamp: now.Add(-20 * time.Minute), TotalTokens: 1000, Model: "claude-3-opus"},
		{Timestamp: now.Add(-10 * time.Minute), TotalTokens: 1000, Model: "claude-3-opus"},
		{Timestamp: now, TotalTokens: 1000, Model: "claude-3-opus"},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// 4000 tokens over 30 minutes = 133.33 tokens/min = 8000 tokens/hr
	expectedPerHour := 8000.0
	tolerance := 100.0 // Allow some floating point variance

	if result.TokensPerHour < expectedPerHour-tolerance || result.TokensPerHour > expectedPerHour+tolerance {
		t.Errorf("TokensPerHour = %.2f, expected ~%.2f", result.TokensPerHour, expectedPerHour)
	}

	if result.SampleSize != 4 {
		t.Errorf("SampleSize = %d, expected 4", result.SampleSize)
	}

	if result.TotalTokens != 4000 {
		t.Errorf("TotalTokens = %d, expected 4000", result.TotalTokens)
	}

	t.Logf("Burn rate: %.2f tokens/hr, confidence: %.2f", result.TokensPerHour, result.Confidence)
}

func TestCalculateBurnRate_EmptyEntries(t *testing.T) {
	result := CalculateBurnRate(nil, time.Hour, nil)
	if result != nil {
		t.Error("expected nil for empty entries")
	}

	result = CalculateBurnRate([]*logs.LogEntry{}, time.Hour, nil)
	if result != nil {
		t.Error("expected nil for empty slice")
	}
}

func TestCalculateBurnRate_InsufficientSampleSize(t *testing.T) {
	now := time.Now()

	// Only 2 entries, default min is 3
	entries := []*logs.LogEntry{
		{Timestamp: now.Add(-20 * time.Minute), TotalTokens: 1000},
		{Timestamp: now, TotalTokens: 1000},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)
	if result != nil {
		t.Error("expected nil for insufficient sample size")
	}
}

func TestCalculateBurnRate_InsufficientTimespan(t *testing.T) {
	now := time.Now()

	// Entries within 2 minutes, default min is 5
	entries := []*logs.LogEntry{
		{Timestamp: now.Add(-2 * time.Minute), TotalTokens: 1000},
		{Timestamp: now.Add(-1 * time.Minute), TotalTokens: 1000},
		{Timestamp: now, TotalTokens: 1000},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)
	if result != nil {
		t.Error("expected nil for insufficient timespan")
	}
}

func TestCalculateBurnRate_CustomOptions(t *testing.T) {
	now := time.Now()

	entries := []*logs.LogEntry{
		{Timestamp: now.Add(-3 * time.Minute), TotalTokens: 1000},
		{Timestamp: now, TotalTokens: 1000},
	}

	// Use lower thresholds
	opts := &BurnRateOptions{
		MinSampleSize:      2,
		MinTimespanMinutes: 2,
	}

	result := CalculateBurnRate(entries, time.Hour, opts)
	if result == nil {
		t.Fatal("expected non-nil result with custom options")
	}

	// 2000 tokens over 3 minutes = 40000 tokens/hr
	expectedPerHour := 40000.0
	tolerance := 1000.0

	if result.TokensPerHour < expectedPerHour-tolerance || result.TokensPerHour > expectedPerHour+tolerance {
		t.Errorf("TokensPerHour = %.2f, expected ~%.2f", result.TokensPerHour, expectedPerHour)
	}
}

func TestCalculateBurnRate_WindowFiltering(t *testing.T) {
	now := time.Now()

	entries := []*logs.LogEntry{
		// These should be filtered out (older than 1 hour)
		{Timestamp: now.Add(-2 * time.Hour), TotalTokens: 10000},
		{Timestamp: now.Add(-90 * time.Minute), TotalTokens: 10000},
		// These should be included
		{Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1000},
		{Timestamp: now.Add(-20 * time.Minute), TotalTokens: 1000},
		{Timestamp: now.Add(-10 * time.Minute), TotalTokens: 1000},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should only count 3000 tokens from the 3 entries within the hour
	if result.TotalTokens != 3000 {
		t.Errorf("TotalTokens = %d, expected 3000 (filtered)", result.TotalTokens)
	}

	if result.SampleSize != 3 {
		t.Errorf("SampleSize = %d, expected 3", result.SampleSize)
	}
}

func TestCalculateBurnRate_ByModel(t *testing.T) {
	now := time.Now()

	entries := []*logs.LogEntry{
		{Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1000, Model: "claude-3-opus"},
		{Timestamp: now.Add(-20 * time.Minute), TotalTokens: 2000, Model: "claude-3-sonnet"},
		{Timestamp: now.Add(-10 * time.Minute), TotalTokens: 1000, Model: "claude-3-opus"},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.ByModel) != 2 {
		t.Errorf("expected 2 models in ByModel, got %d", len(result.ByModel))
	}

	// Check opus rate (2000 tokens over 20 min = 6000/hr)
	opusRate := result.ByModel["claude-3-opus"]
	if opusRate < 5000 || opusRate > 7000 {
		t.Errorf("opus rate = %.2f, expected ~6000", opusRate)
	}

	// Check sonnet rate (2000 tokens over 20 min = 6000/hr)
	sonnetRate := result.ByModel["claude-3-sonnet"]
	if sonnetRate < 5000 || sonnetRate > 7000 {
		t.Errorf("sonnet rate = %.2f, expected ~6000", sonnetRate)
	}
}

func TestCalculateBurnRate_PercentPerHour(t *testing.T) {
	now := time.Now()

	entries := []*logs.LogEntry{
		{Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1000},
		{Timestamp: now.Add(-20 * time.Minute), TotalTokens: 1000},
		{Timestamp: now.Add(-10 * time.Minute), TotalTokens: 1000},
	}

	// Set a token limit: 10000 tokens per hour
	opts := &BurnRateOptions{
		TokenLimit:  10000,
		LimitWindow: time.Hour,
	}

	result := CalculateBurnRate(entries, time.Hour, opts)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// 3000 tokens over 20 minutes = 9000 tokens/hr = 90% of 10000 limit
	if result.PercentPerHour < 80 || result.PercentPerHour > 100 {
		t.Errorf("PercentPerHour = %.2f, expected ~90%%", result.PercentPerHour)
	}

	t.Logf("PercentPerHour: %.2f%%", result.PercentPerHour)
}

func TestCalculateBurnRate_CalculatedTotalTokens(t *testing.T) {
	now := time.Now()

	// Entries without TotalTokens set - should use component tokens
	entries := []*logs.LogEntry{
		{
			Timestamp:    now.Add(-30 * time.Minute),
			InputTokens:  500,
			OutputTokens: 500,
		},
		{
			Timestamp:    now.Add(-20 * time.Minute),
			InputTokens:  500,
			OutputTokens: 500,
		},
		{
			Timestamp:         now.Add(-10 * time.Minute),
			InputTokens:       300,
			OutputTokens:      300,
			CacheReadTokens:   200,
			CacheCreateTokens: 200,
		},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// 1000 + 1000 + 1000 = 3000 total
	if result.TotalTokens != 3000 {
		t.Errorf("TotalTokens = %d, expected 3000", result.TotalTokens)
	}
}

func TestCalculateBurnRate_Confidence(t *testing.T) {
	now := time.Now()

	// High sample size, recent data, consistent timing
	entries := make([]*logs.LogEntry, 20)
	for i := 0; i < 20; i++ {
		entries[i] = &logs.LogEntry{
			Timestamp:   now.Add(time.Duration(-i) * 3 * time.Minute),
			TotalTokens: 1000,
		}
	}

	result := CalculateBurnRate(entries, 2*time.Hour, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have high confidence with 20 samples and consistent timing
	if result.Confidence < 0.5 {
		t.Errorf("Confidence = %.2f, expected > 0.5 for good sample", result.Confidence)
	}

	t.Logf("Confidence with 20 samples: %.2f", result.Confidence)
}

func TestBurnRateInfo_ProjectDepletion(t *testing.T) {
	info := &BurnRateInfo{
		TokensPerHour: 1000,
	}

	// 5000 tokens remaining at 1000/hr = 5 hours
	depletion := info.ProjectDepletion(5000)
	expected := 5 * time.Hour

	if depletion < expected-time.Minute || depletion > expected+time.Minute {
		t.Errorf("ProjectDepletion = %v, expected ~%v", depletion, expected)
	}
}

func TestBurnRateInfo_ProjectDepletion_EdgeCases(t *testing.T) {
	// Nil receiver
	var nilInfo *BurnRateInfo
	if nilInfo.ProjectDepletion(1000) != 0 {
		t.Error("expected 0 for nil receiver")
	}

	// Zero burn rate
	zeroRate := &BurnRateInfo{TokensPerHour: 0}
	if zeroRate.ProjectDepletion(1000) != 0 {
		t.Error("expected 0 for zero burn rate")
	}

	// Zero remaining
	info := &BurnRateInfo{TokensPerHour: 1000}
	if info.ProjectDepletion(0) != 0 {
		t.Error("expected 0 for zero remaining")
	}
}

func TestBurnRateInfo_ProjectUsageAt(t *testing.T) {
	info := &BurnRateInfo{
		TokensPerMinute: 100,
	}

	// 100 tokens/min for 30 minutes = 3000 tokens
	usage := info.ProjectUsageAt(30 * time.Minute)
	if usage != 3000 {
		t.Errorf("ProjectUsageAt = %d, expected 3000", usage)
	}
}

func TestBurnRateInfo_String(t *testing.T) {
	tests := []struct {
		name     string
		info     *BurnRateInfo
		contains string
	}{
		{
			name:     "nil",
			info:     nil,
			contains: "no burn rate data",
		},
		{
			name:     "low rate",
			info:     &BurnRateInfo{TokensPerHour: 500},
			contains: "tokens/hr",
		},
		{
			name:     "medium rate",
			info:     &BurnRateInfo{TokensPerHour: 5000},
			contains: "K tokens/hr",
		},
		{
			name:     "high rate",
			info:     &BurnRateInfo{TokensPerHour: 2500000},
			contains: "M tokens/hr",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.info.String()
			if s == "" {
				t.Error("String() returned empty")
			}
			t.Logf("String() = %q", s)
		})
	}
}

func TestDefaultBurnRateOptions(t *testing.T) {
	opts := DefaultBurnRateOptions()

	if opts.MinSampleSize != 3 {
		t.Errorf("MinSampleSize = %d, expected 3", opts.MinSampleSize)
	}

	if opts.MinTimespanMinutes != 5 {
		t.Errorf("MinTimespanMinutes = %.1f, expected 5", opts.MinTimespanMinutes)
	}
}

func TestCalculateBurnRate_TimeOrdering(t *testing.T) {
	now := time.Now()

	// Entries in reverse order - should still work
	entries := []*logs.LogEntry{
		{Timestamp: now, TotalTokens: 1000},
		{Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1000},
		{Timestamp: now.Add(-15 * time.Minute), TotalTokens: 1000},
	}

	result := CalculateBurnRate(entries, time.Hour, nil)
	if result == nil {
		t.Fatal("expected non-nil result even with unordered entries")
	}

	// Should correctly identify first and last entry times
	if result.FirstEntry.After(result.LastEntry) {
		t.Error("FirstEntry should be before LastEntry")
	}

	t.Logf("FirstEntry: %v, LastEntry: %v", result.FirstEntry, result.LastEntry)
}
