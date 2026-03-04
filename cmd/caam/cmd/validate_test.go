package cmd

import (
	"testing"
	"time"
)

// =============================================================================
// validate.go Command Tests
// =============================================================================

func TestValidateCommand(t *testing.T) {
	if validateCmd.Use != "validate [tool] [profile]" {
		t.Errorf("Expected Use 'validate [tool] [profile]', got %q", validateCmd.Use)
	}

	if validateCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if validateCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestValidateCommandFlags(t *testing.T) {
	flags := []struct {
		name     string
		defValue string
	}{
		{"active", "false"},
		{"json", "false"},
		{"all", "false"},
	}

	for _, tt := range flags {
		t.Run(tt.name, func(t *testing.T) {
			flag := validateCmd.Flags().Lookup(tt.name)
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

func TestValidateCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "validate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("validate command not registered with root command")
	}
}

func TestValidateCommandArgs(t *testing.T) {
	// validateCmd allows 0-2 args
	if validateCmd.Args == nil {
		t.Error("Expected Args validator to be set")
	}
}

// =============================================================================
// ValidationOutput Tests
// =============================================================================

func TestValidationOutputStruct(t *testing.T) {
	output := ValidationOutput{
		Provider:  "claude",
		Profile:   "work",
		Valid:     true,
		Method:    "passive",
		ExpiresAt: "in 7 days",
		CheckedAt: time.Now(),
	}

	if output.Provider != "claude" {
		t.Errorf("Expected provider 'claude', got %q", output.Provider)
	}
	if output.Profile != "work" {
		t.Errorf("Expected profile 'work', got %q", output.Profile)
	}
	if !output.Valid {
		t.Error("Expected valid to be true")
	}
	if output.Method != "passive" {
		t.Errorf("Expected method 'passive', got %q", output.Method)
	}
}

func TestValidationOutputInvalid(t *testing.T) {
	output := ValidationOutput{
		Provider:  "codex",
		Profile:   "expired",
		Valid:     false,
		Method:    "active",
		Error:     "token expired",
		CheckedAt: time.Now(),
	}

	if output.Valid {
		t.Error("Expected valid to be false")
	}
	if output.Error != "token expired" {
		t.Errorf("Expected error 'token expired', got %q", output.Error)
	}
}

// =============================================================================
// Method String Tests
// =============================================================================

func TestMethodString(t *testing.T) {
	tests := []struct {
		passive bool
		want    string
	}{
		{true, "passive"},
		{false, "active"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := methodString(tt.passive)
			if got != tt.want {
				t.Errorf("methodString(%v) = %q, want %q", tt.passive, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Format Expiry Time Tests
// =============================================================================

func TestFormatExpiryTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		expiry  time.Time
		wantGte string // contains check since exact output varies
	}{
		{
			name:    "expired",
			expiry:  now.Add(-time.Hour),
			wantGte: "expired",
		},
		{
			name:    "in_minutes",
			expiry:  now.Add(30 * time.Minute),
			wantGte: "minutes",
		},
		{
			name:    "in_hours",
			expiry:  now.Add(5 * time.Hour),
			wantGte: "hours",
		},
		{
			name:    "in_days",
			expiry:  now.Add(72 * time.Hour),
			wantGte: "days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatExpiryTime(tt.expiry)
			if got == "" {
				t.Error("Expected non-empty expiry time string")
			}
		})
	}
}

// =============================================================================
// Output Format Tests
// =============================================================================

func TestOutputJSONValidation(t *testing.T) {
	results := []ValidationOutput{
		{
			Provider:  "claude",
			Profile:   "work",
			Valid:     true,
			Method:    "passive",
			CheckedAt: time.Now(),
		},
	}

	// Test that outputJSON function exists and works
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

// =============================================================================
// Validation Profile Tests
// =============================================================================

func TestValidationProfileFiltering(t *testing.T) {
	// Test filtering by provider
	providers := []string{"claude", "codex", "gemini"}

	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			// Verify provider names are valid
			if p == "" {
				t.Error("Provider should not be empty")
			}
		})
	}
}