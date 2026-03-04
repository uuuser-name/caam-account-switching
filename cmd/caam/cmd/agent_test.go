package cmd

import (
	"testing"
	"time"
)

// =============================================================================
// agent.go Command Tests
// =============================================================================

func TestAgentCommand(t *testing.T) {
	if agentCmd.Use != "auth-agent" {
		t.Errorf("Expected Use 'auth-agent', got %q", agentCmd.Use)
	}

	if agentCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if agentCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestAgentCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "auth-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Error("agent command not registered with root command")
	}
}

// =============================================================================
// truncateCode Tests
// =============================================================================

func TestTruncateCode(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"short", "shor"},
		{"this is a very long code snippet", "this"},
		{"exact", "exac"},
		{"", ""},
		{"a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			result := truncateCode(tt.code)
			if result != tt.expected {
				t.Errorf("truncateCode(%q) = %q, want %q", tt.code, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// parseStrategy Tests
// =============================================================================

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		input       string
		expected    string
		expectError bool
	}{
		{"lru", "lru", false},
		{"round_robin", "round_robin", false},
		{"random", "random", false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseStrategy(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("parseStrategy(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStrategy(%q) unexpected error: %v", tt.input, err)
			}
			if string(result) != tt.expected {
				t.Errorf("parseStrategy(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// parseOptionalDuration Tests
// =============================================================================

func TestParseOptionalDuration(t *testing.T) {
	tests := []struct {
		input       string
		expected    time.Duration
		expectError bool
	}{
		{"30m", 30 * time.Minute, false},
		{"1h", 1 * time.Hour, false},
		{"", 0, false},
		{"invalid", 0, true},
		{"2h30m", 2*time.Hour + 30*time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseOptionalDuration(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("parseOptionalDuration(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOptionalDuration(%q) unexpected error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("parseOptionalDuration(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// firstNonEmpty Tests
// =============================================================================

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		values   []string
		expected string
	}{
		{[]string{"", "", "found", "later"}, "found"},
		{[]string{"first", "second"}, "first"},
		{[]string{"", "", ""}, ""},
		{[]string{}, ""},
		{[]string{"only"}, "only"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := firstNonEmpty(tt.values...)
			if result != tt.expected {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.values, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Agent Command Flags Tests
// =============================================================================

func TestAgentCommandFlags(t *testing.T) {
	// Check that flags are defined
	flags := []string{"port", "coordinator", "accounts", "strategy", "chrome-profile", "headless", "verbose", "config"}

	for _, flagName := range flags {
		t.Run(flagName, func(t *testing.T) {
			flag := agentCmd.Flags().Lookup(flagName)
			if flag == nil {
				t.Errorf("Expected flag --%s", flagName)
			}
		})
	}
}
