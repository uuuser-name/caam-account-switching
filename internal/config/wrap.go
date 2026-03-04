// Package config wrap configuration for rate limit retry behavior.
package config

import (
	"math"
	"math/rand"
	"time"
)

// WrapConfig holds configuration for the wrap command's retry and backoff behavior.
type WrapConfig struct {
	// MaxRetries is the maximum number of retry attempts on rate limit.
	// Set to 0 for no retries. Default: 3
	MaxRetries int `json:"max_retries"`

	// InitialDelay is the delay before the first retry.
	// Default: 30s
	InitialDelay Duration `json:"initial_delay"`

	// MaxDelay is the maximum delay between retries.
	// Default: 5m
	MaxDelay Duration `json:"max_delay"`

	// BackoffMultiplier is the factor by which delay increases after each retry.
	// Default: 2.0
	BackoffMultiplier float64 `json:"backoff_multiplier"`

	// Jitter adds randomization to delays to prevent thundering herd.
	// When true, delays vary by ±20%. Default: true
	Jitter bool `json:"jitter"`

	// CooldownDuration is how long a profile stays in cooldown after hitting a rate limit.
	// Default: 60m
	CooldownDuration Duration `json:"cooldown_duration"`

	// Providers contains per-provider overrides.
	// Example: {"claude": {"max_retries": 5}}
	Providers map[string]*WrapConfig `json:"providers,omitempty"`
}

// DefaultWrapConfig returns a WrapConfig with sensible defaults.
func DefaultWrapConfig() WrapConfig {
	return WrapConfig{
		MaxRetries:        3,
		InitialDelay:      Duration(30 * time.Second),
		MaxDelay:          Duration(5 * time.Minute),
		BackoffMultiplier: 2.0,
		Jitter:            true,
		CooldownDuration:  Duration(60 * time.Minute),
	}
}

// ForProvider returns the effective config for a specific provider.
// It merges per-provider overrides with the base config.
func (c *WrapConfig) ForProvider(provider string) WrapConfig {
	// Start with a copy of the base config
	result := *c
	result.Providers = nil // Don't copy nested providers

	// Apply per-provider overrides if they exist
	if c.Providers == nil {
		return result
	}

	override, exists := c.Providers[provider]
	if !exists || override == nil {
		return result
	}

	// Merge non-zero override values
	if override.MaxRetries > 0 {
		result.MaxRetries = override.MaxRetries
	}
	if override.InitialDelay > 0 {
		result.InitialDelay = override.InitialDelay
	}
	if override.MaxDelay > 0 {
		result.MaxDelay = override.MaxDelay
	}
	if override.BackoffMultiplier > 0 {
		result.BackoffMultiplier = override.BackoffMultiplier
	}
	// Jitter is a bool - can't distinguish "not set" from "false"
	// We'll use the override value if providers entry exists
	result.Jitter = override.Jitter
	if override.CooldownDuration > 0 {
		result.CooldownDuration = override.CooldownDuration
	}

	return result
}

// NextDelay calculates the delay before the next retry attempt.
// The delay uses exponential backoff with optional jitter.
//
// Formula: delay = min(initial * multiplier^attempt, max)
// With jitter: delay *= (0.8 + random*0.4) for ±20% variation
func (c *WrapConfig) NextDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// Calculate base delay with exponential backoff
	initial := float64(c.InitialDelay.Duration())
	multiplier := c.BackoffMultiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	delay := initial * math.Pow(multiplier, float64(attempt))

	// Cap at max delay
	maxDelay := float64(c.MaxDelay.Duration())
	if delay > maxDelay {
		delay = maxDelay
	}

	// Apply jitter if enabled (±20%)
	if c.Jitter {
		jitterFactor := 0.8 + rand.Float64()*0.4
		delay *= jitterFactor
	}

	return time.Duration(delay)
}

// ShouldRetry returns true if another retry should be attempted.
func (c *WrapConfig) ShouldRetry(attempt int) bool {
	return attempt < c.MaxRetries
}
