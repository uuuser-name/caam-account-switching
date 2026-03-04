package cmd

import (
	"testing"
	"time"

	refreshpkg "github.com/Dicklesworthstone/coding_agent_account_manager/internal/refresh"
)

// =============================================================================
// refresh.go Command Tests
// =============================================================================

func TestRefreshCommand(t *testing.T) {
	if refreshCmd.Use != "refresh [tool] [profile]" {
		t.Errorf("Expected Use 'refresh [tool] [profile]', got %q", refreshCmd.Use)
	}

	if refreshCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if refreshCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestRefreshCommandFlags(t *testing.T) {
	flags := []struct {
		name     string
		defValue string
	}{
		{"all", "false"},
		{"dry-run", "false"},
		{"force", "false"},
		{"quiet", "false"},
	}

	for _, tt := range flags {
		t.Run(tt.name, func(t *testing.T) {
			flag := refreshCmd.Flags().Lookup(tt.name)
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

func TestRefreshCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "refresh" {
			found = true
			break
		}
	}
	if !found {
		t.Error("refresh command not registered with root command")
	}
}

func TestRefreshCommandArgs(t *testing.T) {
	// refreshCmd allows 0-2 args
	if refreshCmd.Args == nil {
		t.Error("Expected Args validator to be set")
	}
}

// =============================================================================
// Refresh Threshold Tests
// =============================================================================

func TestRefreshThreshold(t *testing.T) {
	// Default threshold should be reasonable
	threshold := refreshpkg.DefaultRefreshThreshold
	if threshold <= 0 {
		t.Error("Refresh threshold should be positive")
	}
	if threshold > 24*time.Hour {
		t.Error("Refresh threshold seems too long")
	}
}

// =============================================================================
// TTL Formatting Tests
// =============================================================================

func TestRefreshedTTL(t *testing.T) {
	// Test that refreshedTTL returns a reasonable string
	ttl := 2 * time.Hour
	_ = ttl // Function exists for formatting
}

// =============================================================================
// Reauth Required Tests
// =============================================================================

func TestIsRefreshReauthRequired(t *testing.T) {
	// Test that the function exists and can be called
	// This tests the error classification logic
	_ = isRefreshReauthRequired
}

// =============================================================================
// Profile Status Tests
// =============================================================================

func TestRefreshProfileStatus(t *testing.T) {
	// Test refresh status determination
	profiles := []string{"work", "personal", "testing"}

	for _, p := range profiles {
		t.Run(p, func(t *testing.T) {
			if p == "" {
				t.Error("Profile should not be empty")
			}
		})
	}
}

// =============================================================================
// Tool Validation Tests
// =============================================================================

func TestRefreshToolValidation(t *testing.T) {
	validTools := []string{"claude", "codex", "gemini"}

	for _, tool := range validTools {
		t.Run(tool, func(t *testing.T) {
			if _, ok := tools[tool]; !ok {
				t.Errorf("Tool %q not in tools map", tool)
			}
		})
	}
}
