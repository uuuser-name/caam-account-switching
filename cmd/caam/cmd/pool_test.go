package cmd

import (
	"testing"
)

// =============================================================================
// pool.go Command Tests
// =============================================================================

func TestPoolCommand(t *testing.T) {
	if poolCmd.Use != "pool <command>" {
		t.Errorf("Expected Use 'pool <command>', got %q", poolCmd.Use)
	}

	if poolCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if poolCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestPoolCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "pool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pool command not registered with root command")
	}
}

// =============================================================================
// Pool Subcommand Tests
// =============================================================================

func TestPoolStatusCommand(t *testing.T) {
	if poolStatusCmd.Use != "status" {
		t.Errorf("Expected Use 'status', got %q", poolStatusCmd.Use)
	}

	if poolStatusCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}
}

func TestPoolRefreshCommand(t *testing.T) {
	if poolRefreshCmd.Use != "refresh [provider/profile]" {
		t.Errorf("Expected Use 'refresh [provider/profile]', got %q", poolRefreshCmd.Use)
	}

	if poolRefreshCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}
}

func TestPoolListCommand(t *testing.T) {
	if poolListCmd.Use != "list" {
		t.Errorf("Expected Use 'list', got %q", poolListCmd.Use)
	}

	if poolListCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}
}

// =============================================================================
// parseProfileArg Tests
// =============================================================================

func TestParseProfileArg(t *testing.T) {
	tests := []struct {
		input       string
		wantProfile string
		wantEmpty   bool
	}{
		{"claude/work", "work", false},
		{"codex/personal", "personal", false},
		{"work", "work", false},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			provider, profile, _ := parseProfileArg(tt.input)
			_ = provider
			_ = profile
		})
	}
}
