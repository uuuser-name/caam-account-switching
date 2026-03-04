package usage

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/logs"
)

// BurnRateInfo contains token consumption rate metrics calculated from log data.
type BurnRateInfo struct {
	// TokensPerMinute is the average tokens consumed per minute.
	TokensPerMinute float64 `json:"tokens_per_minute"`

	// TokensPerHour is the average tokens consumed per hour.
	TokensPerHour float64 `json:"tokens_per_hour"`

	// PercentPerHour is the estimated percentage of limit consumed per hour.
	// This requires knowing the account's token limit.
	PercentPerHour float64 `json:"percent_per_hour,omitempty"`

	// SamplePeriod is how far back the calculation looked.
	SamplePeriod time.Duration `json:"sample_period"`

	// SampleSize is the number of log entries used in the calculation.
	SampleSize int `json:"sample_size"`

	// Confidence is a 0-1 score based on sample quality.
	// Higher values indicate more reliable burn rate estimates.
	Confidence float64 `json:"confidence"`

	// ByModel contains per-model burn rates.
	ByModel map[string]float64 `json:"by_model,omitempty"`

	// TotalTokens is the total tokens consumed in the sample period.
	TotalTokens int64 `json:"total_tokens"`

	// FirstEntry is the timestamp of the oldest entry in the sample.
	FirstEntry time.Time `json:"first_entry,omitempty"`

	// LastEntry is the timestamp of the newest entry in the sample.
	LastEntry time.Time `json:"last_entry,omitempty"`
}

// BurnRateOptions configures burn rate calculation.
type BurnRateOptions struct {
	// TokenLimit is the account's token limit per window (for PercentPerHour).
	// If zero, PercentPerHour will not be calculated.
	TokenLimit int64

	// LimitWindow is the window duration for TokenLimit (e.g., 24h).
	LimitWindow time.Duration

	// MinSampleSize is the minimum entries required for a valid calculation.
	// Default: 3
	MinSampleSize int

	// MinTimespanMinutes is the minimum time span required between first and last entry.
	// Default: 5
	MinTimespanMinutes float64
}

// DefaultBurnRateOptions returns sensible defaults.
func DefaultBurnRateOptions() *BurnRateOptions {
	return &BurnRateOptions{
		MinSampleSize:      3,
		MinTimespanMinutes: 5,
	}
}

// CalculateBurnRate computes token consumption rate from log entries.
// The window parameter specifies how far back to consider entries.
// Entries with timestamps older than (now - window) are excluded.
//
// Returns nil if there are insufficient entries or time span for calculation.
func CalculateBurnRate(entries []*logs.LogEntry, window time.Duration, opts *BurnRateOptions) *BurnRateInfo {
	if len(entries) == 0 {
		return nil
	}

	if opts == nil {
		opts = DefaultBurnRateOptions()
	}

	now := time.Now()
	cutoff := now.Add(-window)

	// Filter entries to the window and find time bounds
	var filtered []*logs.LogEntry
	var firstEntry, lastEntry time.Time
	var totalTokens int64
	modelTokens := make(map[string]int64)

	for _, entry := range entries {
		if entry.Timestamp.Before(cutoff) {
			continue
		}

		filtered = append(filtered, entry)

		// Track time bounds
		if firstEntry.IsZero() || entry.Timestamp.Before(firstEntry) {
			firstEntry = entry.Timestamp
		}
		if lastEntry.IsZero() || entry.Timestamp.After(lastEntry) {
			lastEntry = entry.Timestamp
		}

		// Sum tokens
		entryTokens := entry.TotalTokens
		if entryTokens == 0 {
			entryTokens = entry.CalculateTotalTokens()
		}
		totalTokens += entryTokens

		// Track per-model
		if entry.Model != "" {
			modelTokens[entry.Model] += entryTokens
		}
	}

	// Check minimum sample size
	minSampleSize := opts.MinSampleSize
	if minSampleSize <= 0 {
		minSampleSize = 3
	}
	if len(filtered) < minSampleSize {
		return nil
	}

	// Calculate elapsed time
	elapsed := lastEntry.Sub(firstEntry)
	minTimespan := opts.MinTimespanMinutes
	if minTimespan <= 0 {
		minTimespan = 5
	}
	if elapsed.Minutes() < minTimespan {
		return nil
	}

	// Calculate rates
	elapsedMinutes := elapsed.Minutes()
	tokensPerMinute := float64(totalTokens) / elapsedMinutes
	tokensPerHour := tokensPerMinute * 60

	// Calculate per-model rates
	byModel := make(map[string]float64)
	for model, tokens := range modelTokens {
		byModel[model] = float64(tokens) / elapsedMinutes * 60 // tokens per hour
	}

	// Calculate percent per hour if limit is known
	var percentPerHour float64
	if opts.TokenLimit > 0 && opts.LimitWindow > 0 {
		// Scale to hourly percentage
		limitPerHour := float64(opts.TokenLimit) / opts.LimitWindow.Hours()
		if limitPerHour > 0 {
			percentPerHour = (tokensPerHour / limitPerHour) * 100
		}
	}

	// Calculate confidence score
	confidence := calculateConfidence(filtered, elapsed, window, opts)

	return &BurnRateInfo{
		TokensPerMinute: tokensPerMinute,
		TokensPerHour:   tokensPerHour,
		PercentPerHour:  percentPerHour,
		SamplePeriod:    window,
		SampleSize:      len(filtered),
		Confidence:      confidence,
		ByModel:         byModel,
		TotalTokens:     totalTokens,
		FirstEntry:      firstEntry,
		LastEntry:       lastEntry,
	}
}

// calculateConfidence computes a 0-1 confidence score based on sample quality.
func calculateConfidence(entries []*logs.LogEntry, elapsed, window time.Duration, opts *BurnRateOptions) float64 {
	if len(entries) == 0 {
		return 0
	}

	// Component 1: Sample size factor (0-0.4)
	// More entries = higher confidence, diminishing returns after 20
	sampleFactor := math.Min(float64(len(entries))/20.0, 1.0) * 0.4

	// Component 2: Time coverage factor (0-0.3)
	// Longer elapsed time relative to window = higher confidence
	coverageFactor := 0.0
	if window > 0 {
		coverage := elapsed.Minutes() / window.Minutes()
		coverageFactor = math.Min(coverage, 1.0) * 0.3
	}

	// Component 3: Recency factor (0-0.2)
	// More recent data = higher confidence
	recencyFactor := 0.0
	if len(entries) > 0 {
		lastEntry := entries[len(entries)-1]
		for _, e := range entries {
			if e.Timestamp.After(lastEntry.Timestamp) {
				lastEntry = e
			}
		}
		age := time.Since(lastEntry.Timestamp)
		if age < 5*time.Minute {
			recencyFactor = 0.2
		} else if age < 30*time.Minute {
			recencyFactor = 0.15
		} else if age < time.Hour {
			recencyFactor = 0.1
		} else if age < 2*time.Hour {
			recencyFactor = 0.05
		}
	}

	// Component 4: Consistency factor (0-0.1)
	// Low variance in inter-entry times = higher confidence
	consistencyFactor := calculateConsistencyFactor(entries)

	confidence := sampleFactor + coverageFactor + recencyFactor + consistencyFactor
	return math.Min(confidence, 1.0)
}

// calculateConsistencyFactor measures how consistent the entry timing is.
func calculateConsistencyFactor(entries []*logs.LogEntry) float64 {
	if len(entries) < 3 {
		return 0.05 // Minimal consistency with few samples
	}

	// Sort entries by timestamp (they may not be in order)
	sorted := make([]*logs.LogEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	// Calculate inter-entry gaps
	var gaps []float64
	for i := 1; i < len(sorted); i++ {
		gap := sorted[i].Timestamp.Sub(sorted[i-1].Timestamp).Seconds()
		if gap > 0 {
			gaps = append(gaps, gap)
		}
	}

	if len(gaps) < 2 {
		return 0.05
	}

	// Calculate mean and standard deviation
	var sum float64
	for _, g := range gaps {
		sum += g
	}
	mean := sum / float64(len(gaps))

	var variance float64
	for _, g := range gaps {
		diff := g - mean
		variance += diff * diff
	}
	variance /= float64(len(gaps))
	stddev := math.Sqrt(variance)

	// Coefficient of variation (lower = more consistent)
	cv := stddev / mean
	if cv < 0.5 {
		return 0.1 // Very consistent
	} else if cv < 1.0 {
		return 0.07
	} else if cv < 2.0 {
		return 0.04
	}
	return 0.02 // Highly variable
}

// ProjectDepletion estimates when the token limit will be exhausted
// based on the current burn rate.
func (b *BurnRateInfo) ProjectDepletion(remaining int64) time.Duration {
	if b == nil || b.TokensPerHour <= 0 || remaining <= 0 {
		return 0
	}

	hoursRemaining := float64(remaining) / b.TokensPerHour
	ns := hoursRemaining * float64(time.Hour)

	if ns > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}

	return time.Duration(ns)
}

// ProjectUsageAt estimates token consumption after the given duration.
func (b *BurnRateInfo) ProjectUsageAt(d time.Duration) int64 {
	if b == nil || b.TokensPerMinute <= 0 {
		return 0
	}

	return int64(b.TokensPerMinute * d.Minutes())
}

// String returns a human-readable summary of the burn rate.
func (b *BurnRateInfo) String() string {
	if b == nil {
		return "no burn rate data"
	}

	return formatBurnRate(b.TokensPerHour)
}

// formatBurnRate formats tokens per hour in a readable way.
func formatBurnRate(tokensPerHour float64) string {
	if tokensPerHour >= 1_000_000 {
		return fmt.Sprintf("%.1fM tokens/hr", tokensPerHour/1_000_000)
	} else if tokensPerHour >= 1_000 {
		return fmt.Sprintf("%.1fK tokens/hr", tokensPerHour/1_000)
	}
	return fmt.Sprintf("%.0f tokens/hr", tokensPerHour)
}
