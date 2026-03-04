package cmd

import (
	"testing"
)

// =============================================================================
// watch.go Command Tests
// =============================================================================

func TestWatchCommand(t *testing.T) {
	if watchCmd.Use != "watch" {
		t.Errorf("Expected Use 'watch', got %q", watchCmd.Use)
	}

	if watchCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if watchCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestWatchCommandFlags(t *testing.T) {
	flags := []struct {
		name     string
		defValue string
	}{
		{"once", "false"},
		{"providers", "[]"},
		{"verbose", "false"},
	}

	for _, tt := range flags {
		t.Run(tt.name, func(t *testing.T) {
			flag := watchCmd.Flags().Lookup(tt.name)
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

func TestWatchCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "watch" {
			found = true
			break
		}
	}
	if !found {
		t.Error("watch command not registered with root command")
	}
}

// =============================================================================
// timeNow Tests
// =============================================================================

func TestTimeNow(t *testing.T) {
	// Test that timeNow returns a valid time string
	now := timeNow()
	if now == "" {
		t.Error("Expected non-empty time string")
	}

	// Should be in HH:MM:SS format
	if len(now) != 8 {
		t.Errorf("Expected 8 character time string (HH:MM:SS), got %q (len %d)", now, len(now))
	}
}

// =============================================================================
// Watch Providers Tests
// =============================================================================

func TestWatchProvidersFlagDefault(t *testing.T) {
	flag := watchCmd.Flags().Lookup("providers")
	if flag == nil {
		t.Fatal("providers flag not found")
	}

	// Default providers should be empty slice (interpreted as "all providers")
	if flag.DefValue != "[]" {
		t.Errorf("Expected providers default '[]', got %q", flag.DefValue)
	}
}

// =============================================================================
// Watch Once Tests
// =============================================================================

func TestWatchOnceFlag(t *testing.T) {
	flag := watchCmd.Flags().Lookup("once")
	if flag == nil {
		t.Fatal("once flag not found")
	}

	// Default should be false
	if flag.DefValue != "false" {
		t.Errorf("Expected once default 'false', got %q", flag.DefValue)
	}
}
