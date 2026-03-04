package sync

import (
	"os"
	"testing"
	"time"
)

// TestSyncThrottler tests the SyncThrottler functionality.
func TestSyncThrottler(t *testing.T) {
	t.Run("NewThrottler defaults", func(t *testing.T) {
		throttler := NewThrottler(0)
		if throttler.minInterval != DefaultThrottleInterval {
			t.Errorf("Expected default interval %v, got %v", DefaultThrottleInterval, throttler.minInterval)
		}
	})

	t.Run("NewThrottler custom interval", func(t *testing.T) {
		interval := 1 * time.Minute
		throttler := NewThrottler(interval)
		if throttler.minInterval != interval {
			t.Errorf("Expected interval %v, got %v", interval, throttler.minInterval)
		}
	})

	t.Run("ShouldSync returns true for new profile", func(t *testing.T) {
		throttler := NewThrottler(30 * time.Second)
		if !throttler.ShouldSync("claude", "test@example.com") {
			t.Error("ShouldSync should return true for new profile")
		}
	})

	t.Run("ShouldSync returns false within interval", func(t *testing.T) {
		throttler := NewThrottler(1 * time.Hour) // Long interval
		throttler.RecordSync("claude", "test@example.com")

		if throttler.ShouldSync("claude", "test@example.com") {
			t.Error("ShouldSync should return false within interval")
		}
	})

	t.Run("ShouldSync returns true after interval", func(t *testing.T) {
		throttler := NewThrottler(1 * time.Millisecond)
		throttler.RecordSync("claude", "test@example.com")

		time.Sleep(2 * time.Millisecond)

		if !throttler.ShouldSync("claude", "test@example.com") {
			t.Error("ShouldSync should return true after interval")
		}
	})

	t.Run("RecordSync updates timestamp", func(t *testing.T) {
		throttler := NewThrottler(30 * time.Second)

		before := throttler.LastSyncTime("claude", "test@example.com")
		throttler.RecordSync("claude", "test@example.com")
		after := throttler.LastSyncTime("claude", "test@example.com")

		if after.Before(before) || after.Equal(before) {
			t.Error("LastSyncTime should be updated after RecordSync")
		}
	})

	t.Run("Reset clears all records", func(t *testing.T) {
		throttler := NewThrottler(1 * time.Hour)
		throttler.RecordSync("claude", "profile1")
		throttler.RecordSync("codex", "profile2")

		throttler.Reset()

		if !throttler.ShouldSync("claude", "profile1") {
			t.Error("ShouldSync should return true after Reset")
		}
		if !throttler.ShouldSync("codex", "profile2") {
			t.Error("ShouldSync should return true after Reset")
		}
	})

	t.Run("Different profiles are independent", func(t *testing.T) {
		throttler := NewThrottler(1 * time.Hour)
		throttler.RecordSync("claude", "profile1")

		if !throttler.ShouldSync("claude", "profile2") {
			t.Error("Different profiles should be independent")
		}
		if !throttler.ShouldSync("codex", "profile1") {
			t.Error("Different providers should be independent")
		}
	})
}

// TestProfileKey tests the profileKey helper.
func TestProfileKey(t *testing.T) {
	tests := []struct {
		provider string
		profile  string
		want     string
	}{
		{"claude", "test@example.com", "claude/test@example.com"},
		{"codex", "work", "codex/work"},
		{"gemini", "", "gemini/"},
		{"", "profile", "/profile"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := profileKey(tt.provider, tt.profile)
			if got != tt.want {
				t.Errorf("profileKey(%q, %q) = %q, want %q", tt.provider, tt.profile, got, tt.want)
			}
		})
	}
}

// TestAutoSyncConfig tests the AutoSyncConfig.
func TestAutoSyncConfig(t *testing.T) {
	config := DefaultAutoSyncConfig()

	if config.ThrottleInterval != DefaultThrottleInterval {
		t.Errorf("ThrottleInterval = %v, want %v", config.ThrottleInterval, DefaultThrottleInterval)
	}
	if config.SyncTimeout != DefaultSyncTimeout {
		t.Errorf("SyncTimeout = %v, want %v", config.SyncTimeout, DefaultSyncTimeout)
	}
	if config.VaultPath == "" {
		t.Error("VaultPath should not be empty")
	}
}

// TestGetFailedMachines tests the getFailedMachines helper.
func TestGetFailedMachines(t *testing.T) {
	m1 := NewMachine("machine1", "192.168.1.100")
	m2 := NewMachine("machine2", "192.168.1.101")

	tests := []struct {
		name    string
		results []*SyncResult
		want    int
	}{
		{
			name:    "empty results",
			results: []*SyncResult{},
			want:    0,
		},
		{
			name: "all success",
			results: []*SyncResult{
				{Success: true, Operation: &SyncOperation{Machine: m1}},
				{Success: true, Operation: &SyncOperation{Machine: m2}},
			},
			want: 0,
		},
		{
			name: "one failure",
			results: []*SyncResult{
				{Success: true, Operation: &SyncOperation{Machine: m1}},
				{Success: false, Operation: &SyncOperation{Machine: m2}},
			},
			want: 1,
		},
		{
			name: "multiple failures same machine",
			results: []*SyncResult{
				{Success: false, Operation: &SyncOperation{Machine: m1}},
				{Success: false, Operation: &SyncOperation{Machine: m1}},
			},
			want: 1, // Deduplicated
		},
		{
			name: "all failures",
			results: []*SyncResult{
				{Success: false, Operation: &SyncOperation{Machine: m1}},
				{Success: false, Operation: &SyncOperation{Machine: m2}},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFailedMachines(tt.results)
			if len(got) != tt.want {
				t.Errorf("getFailedMachines() = %d machines, want %d", len(got), tt.want)
			}
		})
	}
}

// TestGetErrorForMachine tests the getErrorForMachine helper.
func TestGetErrorForMachine(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")
	err := os.ErrNotExist

	results := []*SyncResult{
		{Success: false, Operation: &SyncOperation{Machine: m}, Error: err},
	}

	got := getErrorForMachine(results, m.ID)
	if got != err.Error() {
		t.Errorf("getErrorForMachine() = %q, want %q", got, err.Error())
	}

	// Test for unknown machine
	got = getErrorForMachine(results, "unknown")
	if got != "unknown error" {
		t.Errorf("getErrorForMachine() for unknown = %q, want %q", got, "unknown error")
	}
}

// TestGlobalThrottlerFunctions tests the global throttler helper functions.
func TestGlobalThrottlerFunctions(t *testing.T) {
	// Save original interval
	originalInterval := GetThrottleInterval()
	defer SetThrottleInterval(originalInterval)

	t.Run("SetThrottleInterval", func(t *testing.T) {
		newInterval := 1 * time.Minute
		SetThrottleInterval(newInterval)

		if GetThrottleInterval() != newInterval {
			t.Errorf("GetThrottleInterval() = %v, want %v", GetThrottleInterval(), newInterval)
		}
	})

	t.Run("SetThrottleInterval ignores zero", func(t *testing.T) {
		current := GetThrottleInterval()
		SetThrottleInterval(0)

		if GetThrottleInterval() != current {
			t.Error("SetThrottleInterval(0) should not change interval")
		}
	})

	t.Run("ResetThrottler", func(t *testing.T) {
		// Record some syncs
		globalThrottler.RecordSync("test", "profile")

		// Reset
		ResetThrottler()

		// Should allow sync again
		if !globalThrottler.ShouldSync("test", "profile") {
			t.Error("After ResetThrottler, ShouldSync should return true")
		}
	})
}

// TestSyncStatus tests the SyncStatus struct and GetSyncStatus.
func TestSyncStatus(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME for sync state
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	t.Run("GetSyncStatus with no state", func(t *testing.T) {
		status, err := GetSyncStatus()
		if err != nil {
			t.Fatalf("GetSyncStatus failed: %v", err)
		}

		if status.Enabled {
			t.Error("Enabled should be false by default")
		}
		if status.AutoSync {
			t.Error("AutoSync should be false by default")
		}
		if status.MachineCount != 0 {
			t.Errorf("MachineCount = %d, want 0", status.MachineCount)
		}
	})
}

// TestIsSyncEnabled tests the IsSyncEnabled helper.
func TestIsSyncEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME for sync state
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// By default, sync should be disabled
	if IsSyncEnabled() {
		t.Error("IsSyncEnabled should return false by default")
	}
}

// TestHasMachinesConfigured tests the HasMachinesConfigured helper.
func TestHasMachinesConfigured(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME for sync state
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// By default, no machines should be configured
	if HasMachinesConfigured() {
		t.Error("HasMachinesConfigured should return false by default")
	}
}

// TestTriggerSyncIfEnabled tests that trigger respects enabled/disabled state.
func TestTriggerSyncIfEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME for sync state
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// This should not panic and should return early (sync disabled)
	TriggerSyncIfEnabled("claude", "test@example.com")

	// Give any goroutine time to fail (it shouldn't do anything)
	time.Sleep(10 * time.Millisecond)
}

// TestTriggerSyncIfEnabledWithConfig tests trigger with custom config.
func TestTriggerSyncIfEnabledWithConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME for sync state
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	originalInterval := GetThrottleInterval()
	defer SetThrottleInterval(originalInterval)

	config := AutoSyncConfig{
		ThrottleInterval: 1 * time.Second,
		SyncTimeout:      1 * time.Minute,
		VaultPath:        tmpDir,
		RemoteVaultPath:  ".local/share/caam/vault",
		Verbose:          false,
	}

	// This should not panic and should return early (sync disabled)
	TriggerSyncIfEnabledWithConfig("claude", "test@example.com", config)

	if GetThrottleInterval() != config.ThrottleInterval {
		t.Errorf("Throttle interval not applied: got %v want %v", GetThrottleInterval(), config.ThrottleInterval)
	}

	// Give any goroutine time to fail (it shouldn't do anything)
	time.Sleep(10 * time.Millisecond)
}

// TestProcessQueueIfNeeded tests that queue processing respects enabled state.
func TestProcessQueueIfNeeded(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME for sync state
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// This should not panic and should return early (sync disabled)
	ProcessQueueIfNeeded()

	// Give any goroutine time to fail (it shouldn't do anything)
	time.Sleep(10 * time.Millisecond)
}
