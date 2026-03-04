package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDefaultWrapConfig(t *testing.T) {
	cfg := DefaultWrapConfig()

	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialDelay.Duration() != 30*time.Second {
		t.Errorf("InitialDelay = %v, want 30s", cfg.InitialDelay.Duration())
	}
	if cfg.MaxDelay.Duration() != 5*time.Minute {
		t.Errorf("MaxDelay = %v, want 5m", cfg.MaxDelay.Duration())
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", cfg.BackoffMultiplier)
	}
	if !cfg.Jitter {
		t.Errorf("Jitter = false, want true")
	}
	if cfg.CooldownDuration.Duration() != 60*time.Minute {
		t.Errorf("CooldownDuration = %v, want 60m", cfg.CooldownDuration.Duration())
	}
}

func TestWrapConfig_NextDelay_ExponentialBackoff(t *testing.T) {
	cfg := WrapConfig{
		InitialDelay:      Duration(10 * time.Second),
		MaxDelay:          Duration(5 * time.Minute),
		BackoffMultiplier: 2.0,
		Jitter:            false, // Disable jitter for predictable tests
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 10 * time.Second},  // 10 * 2^0 = 10s
		{1, 20 * time.Second},  // 10 * 2^1 = 20s
		{2, 40 * time.Second},  // 10 * 2^2 = 40s
		{3, 80 * time.Second},  // 10 * 2^3 = 80s
		{4, 160 * time.Second}, // 10 * 2^4 = 160s
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := cfg.NextDelay(tt.attempt)
			if got != tt.expected {
				t.Errorf("NextDelay(%d) = %v, want %v", tt.attempt, got, tt.expected)
			}
		})
	}
}

func TestWrapConfig_NextDelay_MaxDelayCap(t *testing.T) {
	cfg := WrapConfig{
		InitialDelay:      Duration(30 * time.Second),
		MaxDelay:          Duration(2 * time.Minute), // 120s max
		BackoffMultiplier: 2.0,
		Jitter:            false,
	}

	// After 3 attempts: 30 * 2^3 = 240s, which exceeds max of 120s
	got := cfg.NextDelay(3)
	if got != 2*time.Minute {
		t.Errorf("NextDelay(3) = %v, want 2m (capped at max)", got)
	}
}

func TestWrapConfig_NextDelay_Jitter(t *testing.T) {
	cfg := WrapConfig{
		InitialDelay:      Duration(100 * time.Second),
		MaxDelay:          Duration(5 * time.Minute),
		BackoffMultiplier: 2.0,
		Jitter:            true,
	}

	// With jitter enabled, delays should vary by ±20%
	// 100s * 0.8 = 80s minimum
	// 100s * 1.2 = 120s maximum
	minExpected := 80 * time.Second
	maxExpected := 120 * time.Second

	// Run multiple times to test jitter range
	for i := 0; i < 10; i++ {
		got := cfg.NextDelay(0)
		if got < minExpected || got > maxExpected {
			t.Errorf("NextDelay(0) with jitter = %v, want between %v and %v", got, minExpected, maxExpected)
		}
	}
}

func TestWrapConfig_NextDelay_NegativeAttempt(t *testing.T) {
	cfg := WrapConfig{
		InitialDelay:      Duration(10 * time.Second),
		MaxDelay:          Duration(5 * time.Minute),
		BackoffMultiplier: 2.0,
		Jitter:            false,
	}

	// Negative attempt should be treated as 0
	got := cfg.NextDelay(-1)
	if got != 10*time.Second {
		t.Errorf("NextDelay(-1) = %v, want 10s", got)
	}
}

func TestWrapConfig_NextDelay_ZeroMultiplier(t *testing.T) {
	cfg := WrapConfig{
		InitialDelay:      Duration(10 * time.Second),
		MaxDelay:          Duration(5 * time.Minute),
		BackoffMultiplier: 0, // Invalid, should default to 2.0
		Jitter:            false,
	}

	// Should default to 2.0 multiplier
	got := cfg.NextDelay(1)
	if got != 20*time.Second {
		t.Errorf("NextDelay(1) with zero multiplier = %v, want 20s (default multiplier 2.0)", got)
	}
}

func TestWrapConfig_ShouldRetry(t *testing.T) {
	cfg := WrapConfig{MaxRetries: 3}

	tests := []struct {
		attempt  int
		expected bool
	}{
		{0, true},
		{1, true},
		{2, true},
		{3, false}, // Reached max
		{4, false},
	}

	for _, tt := range tests {
		got := cfg.ShouldRetry(tt.attempt)
		if got != tt.expected {
			t.Errorf("ShouldRetry(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestWrapConfig_ForProvider_NoOverrides(t *testing.T) {
	cfg := WrapConfig{
		MaxRetries:        3,
		InitialDelay:      Duration(30 * time.Second),
		BackoffMultiplier: 2.0,
		Providers:         nil, // No provider overrides
	}

	result := cfg.ForProvider("claude")

	if result.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", result.MaxRetries)
	}
	if result.InitialDelay.Duration() != 30*time.Second {
		t.Errorf("InitialDelay = %v, want 30s", result.InitialDelay.Duration())
	}
}

func TestWrapConfig_ForProvider_WithOverrides(t *testing.T) {
	cfg := WrapConfig{
		MaxRetries:        3,
		InitialDelay:      Duration(30 * time.Second),
		MaxDelay:          Duration(5 * time.Minute),
		BackoffMultiplier: 2.0,
		Jitter:            true,
		CooldownDuration:  Duration(60 * time.Minute),
		Providers: map[string]*WrapConfig{
			"claude": {
				MaxRetries:   5,
				InitialDelay: Duration(60 * time.Second),
			},
		},
	}

	result := cfg.ForProvider("claude")

	// Overridden values
	if result.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", result.MaxRetries)
	}
	if result.InitialDelay.Duration() != 60*time.Second {
		t.Errorf("InitialDelay = %v, want 60s", result.InitialDelay.Duration())
	}

	// Non-overridden values should remain from base
	if result.MaxDelay.Duration() != 5*time.Minute {
		t.Errorf("MaxDelay = %v, want 5m", result.MaxDelay.Duration())
	}
	if result.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", result.BackoffMultiplier)
	}
}

func TestWrapConfig_ForProvider_UnknownProvider(t *testing.T) {
	cfg := WrapConfig{
		MaxRetries:   3,
		InitialDelay: Duration(30 * time.Second),
		Providers: map[string]*WrapConfig{
			"claude": {MaxRetries: 5},
		},
	}

	// Unknown provider should return base config
	result := cfg.ForProvider("unknown")

	if result.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", result.MaxRetries)
	}
}

func TestDuration_JSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected time.Duration
	}{
		{"30 seconds", `"30s"`, 30 * time.Second},
		{"5 minutes", `"5m"`, 5 * time.Minute},
		{"1 hour", `"1h"`, 1 * time.Hour},
		{"combined", `"1h30m"`, 90 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			if err := json.Unmarshal([]byte(tt.json), &d); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if d.Duration() != tt.expected {
				t.Errorf("Duration = %v, want %v", d.Duration(), tt.expected)
			}
		})
	}
}

func TestDuration_JSON_Roundtrip(t *testing.T) {
	original := Duration(2*time.Hour + 30*time.Minute)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Duration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Duration() != original.Duration() {
		t.Errorf("Roundtrip: got %v, want %v", decoded.Duration(), original.Duration())
	}
}

func TestWrapConfig_JSON_Roundtrip(t *testing.T) {
	original := WrapConfig{
		MaxRetries:        5,
		InitialDelay:      Duration(45 * time.Second),
		MaxDelay:          Duration(10 * time.Minute),
		BackoffMultiplier: 1.5,
		Jitter:            true,
		CooldownDuration:  Duration(30 * time.Minute),
		Providers: map[string]*WrapConfig{
			"claude": {MaxRetries: 10},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded WrapConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.MaxRetries != original.MaxRetries {
		t.Errorf("MaxRetries = %d, want %d", decoded.MaxRetries, original.MaxRetries)
	}
	if decoded.InitialDelay.Duration() != original.InitialDelay.Duration() {
		t.Errorf("InitialDelay = %v, want %v", decoded.InitialDelay.Duration(), original.InitialDelay.Duration())
	}
	if decoded.BackoffMultiplier != original.BackoffMultiplier {
		t.Errorf("BackoffMultiplier = %f, want %f", decoded.BackoffMultiplier, original.BackoffMultiplier)
	}
	if decoded.Providers["claude"].MaxRetries != 10 {
		t.Errorf("Providers[claude].MaxRetries = %d, want 10", decoded.Providers["claude"].MaxRetries)
	}
}
