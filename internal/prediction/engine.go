// Package prediction provides depletion prediction for rate limit management.
// It combines multiple data sources (real-time session, historical logs, API)
// to forecast when rate limits will be hit.
package prediction

import (
	"context"
	"math"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/logs"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
)

// WarningLevel indicates the urgency of predicted depletion.
type WarningLevel int

const (
	// WarningNone means no imminent depletion.
	WarningNone WarningLevel = iota
	// WarningApproaching means depletion expected within 30 minutes.
	WarningApproaching
	// WarningImminent means depletion expected within 10 minutes.
	WarningImminent
)

// String returns a human-readable warning level.
func (w WarningLevel) String() string {
	switch w {
	case WarningApproaching:
		return "approaching"
	case WarningImminent:
		return "imminent"
	default:
		return "none"
	}
}

// Prediction contains the result of a depletion forecast.
type Prediction struct {
	// Profile is the profile name being predicted.
	Profile string `json:"profile"`

	// Provider is the provider (claude, codex, gemini).
	Provider string `json:"provider"`

	// CurrentPercent is the current utilization percentage (0-100).
	CurrentPercent float64 `json:"current_percent"`

	// BurnRate contains token consumption rate data.
	BurnRate *usage.BurnRateInfo `json:"burn_rate,omitempty"`

	// PredictedTime is when depletion is expected.
	PredictedTime time.Time `json:"predicted_time,omitempty"`

	// TimeToDepletion is the duration until depletion.
	TimeToDepletion time.Duration `json:"time_to_depletion"`

	// Confidence is how reliable the prediction is (0-1).
	Confidence float64 `json:"confidence"`

	// Warning indicates the urgency level.
	Warning WarningLevel `json:"warning"`

	// DataSources lists where burn rate data came from.
	// Values: "session", "logs", "api"
	DataSources []string `json:"data_sources"`

	// Error contains any error message from prediction.
	Error string `json:"error,omitempty"`
}

// IsValid returns true if the prediction has useful data.
func (p *Prediction) IsValid() bool {
	return p != nil && p.Error == "" && len(p.DataSources) > 0
}

// ShouldRotate returns true if rotation is recommended based on prediction.
func (p *Prediction) ShouldRotate(threshold time.Duration) bool {
	if p == nil || p.TimeToDepletion == 0 {
		return false
	}
	return p.TimeToDepletion < threshold && p.Confidence >= 0.3
}

// PredictionEngine combines multiple data sources for depletion prediction.
type PredictionEngine struct {
	logScanner     logs.Scanner
	sessionTracker *usage.SessionTracker

	// Configuration
	logWindow     time.Duration // How far back to look in logs
	sessionWindow time.Duration // Window for session burn rate
}

// EngineOption configures a PredictionEngine.
type EngineOption func(*PredictionEngine)

// WithLogScanner sets the log scanner for historical data.
func WithLogScanner(s logs.Scanner) EngineOption {
	return func(e *PredictionEngine) {
		e.logScanner = s
	}
}

// WithSessionTracker sets the session tracker for real-time data.
func WithSessionTracker(t *usage.SessionTracker) EngineOption {
	return func(e *PredictionEngine) {
		e.sessionTracker = t
	}
}

// WithLogWindow sets how far back to look in log data.
func WithLogWindow(d time.Duration) EngineOption {
	return func(e *PredictionEngine) {
		e.logWindow = d
	}
}

// WithSessionWindow sets the window for session burn rate calculation.
func WithSessionWindow(d time.Duration) EngineOption {
	return func(e *PredictionEngine) {
		e.sessionWindow = d
	}
}

// NewPredictionEngine creates a new prediction engine.
func NewPredictionEngine(opts ...EngineOption) *PredictionEngine {
	e := &PredictionEngine{
		logWindow:     2 * time.Hour,    // Default: look back 2 hours
		sessionWindow: 30 * time.Minute, // Default: 30 min window for session
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Predict forecasts when the rate limit will be hit for a profile.
// It tries data sources in order of preference:
// 1. Real-time session data (most accurate)
// 2. Historical log data (good accuracy)
// 3. API-reported burn rate (if available in usageInfo)
func (e *PredictionEngine) Predict(ctx context.Context, usageInfo *usage.UsageInfo) *Prediction {
	if usageInfo == nil {
		return &Prediction{
			Error: "no usage info provided",
		}
	}

	pred := &Prediction{
		Profile:     usageInfo.ProfileName,
		Provider:    usageInfo.Provider,
		DataSources: make([]string, 0),
	}

	// Get current utilization from most constrained window
	window := usageInfo.MostConstrainedWindow()
	if window == nil {
		pred.Error = "no usage window available"
		return pred
	}

	// Calculate current percent
	util := window.Utilization
	if util == 0 && window.UsedPercent > 0 {
		util = float64(window.UsedPercent) / 100.0
	}
	pred.CurrentPercent = util * 100

	// Already at limit?
	if pred.CurrentPercent >= 100 {
		pred.Warning = WarningImminent
		pred.PredictedTime = time.Now()
		pred.TimeToDepletion = 0
		pred.DataSources = append(pred.DataSources, "current_usage")
		pred.Confidence = 1.0 // Certain - already depleted
		return pred
	}

	// Try to get burn rate from multiple sources
	// Only use a source if it has valid PercentPerHour (requires TokenLimit)
	var burnRate *usage.BurnRateInfo

	// Priority 1: Real-time session data (most accurate, most recent)
	if e.sessionTracker != nil {
		if sessionRate := e.sessionTracker.BurnRate(e.sessionWindow); sessionRate != nil && sessionRate.PercentPerHour > 0 {
			burnRate = sessionRate
			pred.DataSources = append(pred.DataSources, "session")
		}
	}

	// Priority 2: Historical log data
	if burnRate == nil && e.logScanner != nil {
		if result, err := e.logScanner.Scan(ctx, "", time.Now().Add(-e.logWindow)); err == nil && len(result.Entries) > 0 {
			logRate := usage.CalculateBurnRate(result.Entries, e.logWindow, nil)
			if logRate != nil && logRate.PercentPerHour > 0 {
				burnRate = logRate
				pred.DataSources = append(pred.DataSources, "logs")
			}
		}
	}

	// Priority 3: API-reported burn rate (always has PercentPerHour if set)
	if burnRate == nil && usageInfo.BurnRate != nil && usageInfo.BurnRate.PercentPerHour > 0 {
		burnRate = usageInfo.BurnRate
		pred.DataSources = append(pred.DataSources, "api")
	}

	// If we have no burn rate data, we can't predict
	if burnRate == nil || burnRate.PercentPerHour <= 0 {
		pred.Error = "insufficient data for prediction"
		return pred
	}

	pred.BurnRate = burnRate

	// Calculate time to depletion
	remainingPercent := 100.0 - pred.CurrentPercent
	hoursUntilDepletion := remainingPercent / burnRate.PercentPerHour
	pred.TimeToDepletion = time.Duration(hoursUntilDepletion * float64(time.Hour))
	pred.PredictedTime = time.Now().Add(pred.TimeToDepletion)

	// Determine warning level
	// We only warn if depletion happens BEFORE the window resets.
	// If the window resets first, we are safe (quota is restored).
	if !window.ResetsAt.IsZero() && window.ResetsAt.Before(pred.PredictedTime) {
		pred.Warning = WarningNone
	} else if pred.TimeToDepletion > 0 && pred.TimeToDepletion < 10*time.Minute {
		pred.Warning = WarningImminent
	} else if pred.TimeToDepletion > 0 && pred.TimeToDepletion < 30*time.Minute {
		pred.Warning = WarningApproaching
	} else {
		pred.Warning = WarningNone
	}

	// Calculate confidence
	pred.Confidence = e.calculateConfidence(burnRate, len(pred.DataSources))

	return pred
}

// PredictWithBurnRate creates a prediction using an externally-provided burn rate.
// Useful when burn rate is already calculated elsewhere.
func (e *PredictionEngine) PredictWithBurnRate(usageInfo *usage.UsageInfo, burnRate *usage.BurnRateInfo, source string) *Prediction {
	if usageInfo == nil {
		return &Prediction{
			Error: "no usage info provided",
		}
	}

	pred := &Prediction{
		Profile:     usageInfo.ProfileName,
		Provider:    usageInfo.Provider,
		DataSources: []string{source},
		BurnRate:    burnRate,
	}

	// Get current utilization
	window := usageInfo.MostConstrainedWindow()
	if window == nil {
		pred.Error = "no usage window available"
		return pred
	}

	util := window.Utilization
	if util == 0 && window.UsedPercent > 0 {
		util = float64(window.UsedPercent) / 100.0
	}
	pred.CurrentPercent = util * 100

	// Can't predict without burn rate
	if burnRate == nil || burnRate.PercentPerHour <= 0 {
		pred.Error = "no burn rate data"
		return pred
	}

	// Calculate time to depletion
	remainingPercent := 100.0 - pred.CurrentPercent
	if remainingPercent <= 0 {
		pred.Warning = WarningImminent
		pred.PredictedTime = time.Now()
		pred.TimeToDepletion = 0
		pred.Confidence = 1.0 // Certain - already depleted
		return pred
	}

	hoursUntilDepletion := remainingPercent / burnRate.PercentPerHour
	pred.TimeToDepletion = time.Duration(hoursUntilDepletion * float64(time.Hour))
	pred.PredictedTime = time.Now().Add(pred.TimeToDepletion)

	// Warning level
	if !window.ResetsAt.IsZero() && window.ResetsAt.Before(pred.PredictedTime) {
		pred.Warning = WarningNone
	} else if pred.TimeToDepletion > 0 && pred.TimeToDepletion < 10*time.Minute {
		pred.Warning = WarningImminent
	} else if pred.TimeToDepletion > 0 && pred.TimeToDepletion < 30*time.Minute {
		pred.Warning = WarningApproaching
	} else {
		pred.Warning = WarningNone
	}

	pred.Confidence = e.calculateConfidence(burnRate, 1)

	return pred
}

// calculateConfidence determines prediction reliability based on data quality.
func (e *PredictionEngine) calculateConfidence(burnRate *usage.BurnRateInfo, sourceCount int) float64 {
	if burnRate == nil {
		return 0
	}

	// Base confidence from burn rate calculation
	confidence := burnRate.Confidence

	// Boost for multiple data sources (up to 20%)
	if sourceCount > 1 {
		boost := float64(sourceCount-1) * 0.1
		confidence = math.Min(1.0, confidence*(1+boost))
	}

	// Slight penalty if sample size is very small
	if burnRate.SampleSize < 5 {
		confidence *= 0.9
	}

	return math.Min(confidence, 1.0)
}

// PredictAll creates predictions for multiple profiles.
func (e *PredictionEngine) PredictAll(ctx context.Context, profiles []*usage.UsageInfo) []*Prediction {
	predictions := make([]*Prediction, len(profiles))
	for i, info := range profiles {
		predictions[i] = e.Predict(ctx, info)
	}
	return predictions
}

// MostUrgent returns the prediction with the shortest time to depletion.
// Only considers predictions with confidence >= minConfidence.
func MostUrgent(predictions []*Prediction, minConfidence float64) *Prediction {
	var urgent *Prediction

	for _, p := range predictions {
		if p == nil || p.Error != "" || p.Confidence < minConfidence {
			continue
		}
		if p.TimeToDepletion <= 0 {
			continue
		}
		if urgent == nil || p.TimeToDepletion < urgent.TimeToDepletion {
			urgent = p
		}
	}

	return urgent
}

// FilterByWarning returns predictions at or above the specified warning level.
func FilterByWarning(predictions []*Prediction, minWarning WarningLevel) []*Prediction {
	var result []*Prediction
	for _, p := range predictions {
		if p != nil && p.Warning >= minWarning {
			result = append(result, p)
		}
	}
	return result
}
