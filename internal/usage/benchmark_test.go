package usage

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/logs"
)

// generateLogEntries creates test log entries for benchmarking.
func generateLogEntries(count int, interval time.Duration) []*logs.LogEntry {
	entries := make([]*logs.LogEntry, count)
	now := time.Now()

	for i := 0; i < count; i++ {
		entries[i] = &logs.LogEntry{
			Timestamp:    now.Add(-time.Duration(count-i-1) * interval),
			Model:        "claude-3.5-sonnet",
			InputTokens:  500 + int64(i%100),
			OutputTokens: 1000 + int64(i%200),
		}
	}

	return entries
}

// BenchmarkCalculateBurnRateSmall benchmarks burn rate with a small dataset.
func BenchmarkCalculateBurnRateSmall(b *testing.B) {
	entries := generateLogEntries(10, time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := CalculateBurnRate(entries, time.Hour, nil)
		if result == nil {
			b.Fatal("expected result")
		}
	}
}

// BenchmarkCalculateBurnRateMedium benchmarks burn rate with a medium dataset.
func BenchmarkCalculateBurnRateMedium(b *testing.B) {
	entries := generateLogEntries(100, time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := CalculateBurnRate(entries, 2*time.Hour, nil)
		if result == nil {
			b.Fatal("expected result")
		}
	}
}

// BenchmarkCalculateBurnRateLarge benchmarks burn rate with a large dataset.
func BenchmarkCalculateBurnRateLarge(b *testing.B) {
	entries := generateLogEntries(1000, 30*time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := CalculateBurnRate(entries, 24*time.Hour, nil)
		if result == nil {
			b.Fatal("expected result")
		}
	}
}

// BenchmarkCalculateBurnRateWithOptions benchmarks burn rate with custom options.
func BenchmarkCalculateBurnRateWithOptions(b *testing.B) {
	entries := generateLogEntries(500, time.Minute)
	opts := &BurnRateOptions{
		TokenLimit:         1000000,
		LimitWindow:        24 * time.Hour,
		MinSampleSize:      3,
		MinTimespanMinutes: 5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := CalculateBurnRate(entries, 8*time.Hour, opts)
		if result == nil {
			b.Fatal("expected result")
		}
	}
}

// BenchmarkCalculateBurnRateMixedModels benchmarks burn rate with multiple models.
func BenchmarkCalculateBurnRateMixedModels(b *testing.B) {
	models := []string{"claude-3.5-sonnet", "claude-3-opus", "claude-3-haiku", "gpt-4o"}
	entries := make([]*logs.LogEntry, 200)
	now := time.Now()

	for i := 0; i < len(entries); i++ {
		entries[i] = &logs.LogEntry{
			Timestamp:    now.Add(-time.Duration(len(entries)-i-1) * time.Minute),
			Model:        models[i%len(models)],
			InputTokens:  500 + int64(i%100),
			OutputTokens: 1000 + int64(i%200),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := CalculateBurnRate(entries, 4*time.Hour, nil)
		if result == nil {
			b.Fatal("expected result")
		}
	}
}

// BenchmarkCalculateBurnRateParallel benchmarks parallel burn rate calculation.
func BenchmarkCalculateBurnRateParallel(b *testing.B) {
	entries := generateLogEntries(100, time.Minute)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result := CalculateBurnRate(entries, 2*time.Hour, nil)
			if result == nil {
				b.Fatal("expected result")
			}
		}
	})
}

// BenchmarkBurnRateInfoProjectDepletion benchmarks the projection calculation.
func BenchmarkBurnRateInfoProjectDepletion(b *testing.B) {
	info := &BurnRateInfo{
		TokensPerHour:   10000,
		TokensPerMinute: 10000.0 / 60.0,
		Confidence:      0.9,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := info.ProjectDepletion(500000)
		if d == 0 {
			b.Fatal("expected duration")
		}
	}
}

// BenchmarkBurnRateInfoProjectUsageAt benchmarks the usage projection.
func BenchmarkBurnRateInfoProjectUsageAt(b *testing.B) {
	info := &BurnRateInfo{
		TokensPerHour:   10000,
		TokensPerMinute: 10000.0 / 60.0,
		Confidence:      0.9,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokens := info.ProjectUsageAt(24 * time.Hour)
		if tokens == 0 {
			b.Fatal("expected tokens")
		}
	}
}

// BenchmarkPredictDepletion benchmarks the depletion prediction function.
func BenchmarkPredictDepletion(b *testing.B) {
	burnRate := &BurnRateInfo{
		TokensPerHour:  15000,
		PercentPerHour: 5.0,
		Confidence:     0.85,
	}
	window := &UsageWindow{
		UsedPercent:    45,
		ResetsAt:       time.Now().Add(12 * time.Hour),
		WindowDuration: 24 * time.Hour,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := PredictDepletion(45.0, burnRate, window)
		if t.IsZero() {
			b.Fatal("expected non-zero time")
		}
	}
}
