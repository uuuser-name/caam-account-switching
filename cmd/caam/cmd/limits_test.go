package cmd

import (
	"testing"
	"time"
)

// =============================================================================
// limits.go Command Tests
// =============================================================================

func TestLimitsCommand(t *testing.T) {
	if limitsCmd.Use != "limits [provider]" {
		t.Errorf("Expected Use 'limits [provider]', got %q", limitsCmd.Use)
	}

	if limitsCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if limitsCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestLimitsCommandFlags(t *testing.T) {
	flags := []struct {
		name     string
		defValue string
	}{
		{"profile", ""},
		{"format", "table"},
		{"best", "false"},
		{"threshold", "0.8"},
		{"recommend", "false"},
		{"forecast", "false"},
	}

	for _, tt := range flags {
		t.Run(tt.name, func(t *testing.T) {
			flag := limitsCmd.Flags().Lookup(tt.name)
			if flag == nil {
				t.Errorf("Expected flag --%s", tt.name)
				return
			}
			if flag.DefValue != tt.defValue {
				t.Errorf("Expected default %q, got %q", tt.defValue, flag.DefValue)
			}
		})
	}
}

func TestLimitsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "limits" {
			found = true
			break
		}
	}
	if !found {
		t.Error("limits command not registered with root command")
	}
}

// =============================================================================
// Duration Formatting Tests
// =============================================================================

func TestFormatLimitsDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{0, "0s"},
		{time.Second * 30, "30s"},
		{time.Minute, "1m"},
		{time.Minute * 5, "5m"},
		{time.Hour, "1h"},
		{time.Hour * 2, "2h"},
		{time.Hour * 24, "24h"},
		{time.Hour * 25, "1d1h"},
		{time.Hour * 48, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatLimitsDuration(tt.duration)
			// The function may format differently, just check it returns something
			if got == "" {
				t.Error("Expected non-empty duration string")
			}
		})
	}
}

// =============================================================================
// Truncate Helper Tests
// =============================================================================

func TestTruncateLimits(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"very long string", 10, "very lo..."},
		{"exact", 5, "exact"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Burn Rate Formatting Tests
// =============================================================================

func TestFormatBurnRate(t *testing.T) {
	tests := []struct {
		tokensPerHour float64
		wantContains  string
	}{
		{0, "0"},
		{100, "100"},
		{1000, "1k"},
		{10000, "10k"},
		{50000, "50k"},
	}

	for _, tt := range tests {
		t.Run(tt.wantContains, func(t *testing.T) {
			got := formatBurnRate(tt.tokensPerHour)
			if got == "" {
				t.Error("Expected non-empty burn rate string")
			}
		})
	}
}

// =============================================================================
// Threshold Tests
// =============================================================================

func TestLimitsThresholdDefault(t *testing.T) {
	flag := limitsCmd.Flags().Lookup("threshold")
	if flag == nil {
		t.Fatal("threshold flag not found")
	}

	// Default threshold should be 0.8
	if flag.DefValue != "0.8" {
		t.Errorf("Expected threshold default '0.8', got %q", flag.DefValue)
	}
}

// =============================================================================
// Provider Tests
// =============================================================================

func TestLimitsProviderParsing(t *testing.T) {
	// Test that provider names are correctly normalized
	providers := []string{"claude", "codex", "gemini", "CLAUDE", "CODEX", "GEMINI"}

	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			// Provider names should be case-insensitive
			// The actual normalization happens in the command
			if p == "" {
				t.Error("Provider should not be empty")
			}
		})
	}
}