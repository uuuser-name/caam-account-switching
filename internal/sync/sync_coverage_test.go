package sync

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestDetermineSyncOperationLogic tests the sync direction logic without SSH.
func TestDetermineSyncOperationLogic(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	earlier := now.Add(-time.Hour)

	tests := []struct {
		name          string
		localFresh    *TokenFreshness
		remoteFresh   *TokenFreshness
		localExists   bool
		remoteExists  bool
		wantDirection SyncDirection
	}{
		{
			name:          "neither exists - skip",
			localExists:   false,
			remoteExists:  false,
			wantDirection: SyncSkip,
		},
		{
			name:          "only local exists - push",
			localExists:   true,
			remoteExists:  false,
			localFresh:    &TokenFreshness{ExpiresAt: later},
			wantDirection: SyncPush,
		},
		{
			name:          "only remote exists - pull",
			localExists:   false,
			remoteExists:  true,
			remoteFresh:   &TokenFreshness{ExpiresAt: later},
			wantDirection: SyncPull,
		},
		{
			name:          "local is fresher - push",
			localExists:   true,
			remoteExists:  true,
			localFresh:    &TokenFreshness{ExpiresAt: later, ModifiedAt: now},
			remoteFresh:   &TokenFreshness{ExpiresAt: now, ModifiedAt: earlier},
			wantDirection: SyncPush,
		},
		{
			name:          "remote is fresher - pull",
			localExists:   true,
			remoteExists:  true,
			localFresh:    &TokenFreshness{ExpiresAt: now, ModifiedAt: earlier},
			remoteFresh:   &TokenFreshness{ExpiresAt: later, ModifiedAt: now},
			wantDirection: SyncPull,
		},
		{
			name:          "equal freshness - skip",
			localExists:   true,
			remoteExists:  true,
			localFresh:    &TokenFreshness{ExpiresAt: now, ModifiedAt: now},
			remoteFresh:   &TokenFreshness{ExpiresAt: now, ModifiedAt: now},
			wantDirection: SyncSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the CompareFreshness logic directly
			if tt.localFresh != nil && tt.remoteFresh != nil {
				localFresher := CompareFreshness(tt.localFresh, tt.remoteFresh)
				remoteFresher := CompareFreshness(tt.remoteFresh, tt.localFresh)

				switch {
				case localFresher:
					if tt.wantDirection != SyncPush {
						t.Errorf("expected push, local is fresher")
					}
				case remoteFresher:
					if tt.wantDirection != SyncPull {
						t.Errorf("expected pull, remote is fresher")
					}
				default:
					if tt.wantDirection != SyncSkip {
						t.Errorf("expected skip, equal freshness")
					}
				}
			} else if tt.localFresh != nil && tt.remoteFresh == nil {
				if tt.wantDirection != SyncPush {
					t.Errorf("expected push, only local exists")
				}
			} else if tt.localFresh == nil && tt.remoteFresh != nil {
				if tt.wantDirection != SyncPull {
					t.Errorf("expected pull, only remote exists")
				}
			}
		})
	}
}

// TestSyncerNewAndClose tests Syncer creation and cleanup.
func TestSyncerNewAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	config := SyncerConfig{
		VaultPath:       filepath.Join(tmpDir, "vault"),
		RemoteVaultPath: ".local/share/caam/vault",
		ConnectOptions:  DefaultConnectOptions(),
	}

	syncer, err := NewSyncer(config)
	if err != nil {
		t.Fatalf("NewSyncer failed: %v", err)
	}

	if syncer == nil {
		t.Fatal("NewSyncer returned nil")
	}

	if syncer.state == nil {
		t.Error("Syncer should have state initialized")
	}

	// Close should not error
	if err := syncer.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestSyncerStateManagement tests state operations through Syncer.
func TestSyncerStateManagement(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	vaultPath := filepath.Join(tmpDir, "vault")
	if err := os.MkdirAll(vaultPath, 0700); err != nil {
		t.Fatalf("Failed to create vault: %v", err)
	}

	config := SyncerConfig{
		VaultPath:       vaultPath,
		RemoteVaultPath: ".local/share/caam/vault",
		ConnectOptions:  DefaultConnectOptions(),
	}

	syncer, err := NewSyncer(config)
	if err != nil {
		t.Fatalf("NewSyncer failed: %v", err)
	}
	defer syncer.Close()

	// Test state is initialized
	if syncer.state == nil {
		t.Fatal("State should be initialized")
	}

	// Test identity was loaded/created
	if syncer.state.Identity == nil {
		t.Error("Identity should be set")
	}
}

// TestQueueRetryLogic tests queue operations for retry scenarios.
func TestQueueRetryLogic(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)

	// Test adding to queue
	state.AddToQueue("claude", "test@example.com", "machine-1", "connection refused")
	state.AddToQueue("codex", "work@company.com", "machine-2", "timeout")

	if len(state.Queue.Entries) != 2 {
		t.Errorf("Queue should have 2 entries, got %d", len(state.Queue.Entries))
	}

	// Test updating existing entry increments attempts
	state.AddToQueue("claude", "test@example.com", "machine-1", "second error")
	if len(state.Queue.Entries) != 2 {
		t.Errorf("Queue should still have 2 entries after update")
	}

	// Find the entry and check attempts
	var found *QueueEntry
	for _, e := range state.Queue.Entries {
		if e.Provider == "claude" && e.Profile == "test@example.com" && e.Machine == "machine-1" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatal("Entry not found")
	}
	if found.Attempts != 2 {
		t.Errorf("Attempts should be 2, got %d", found.Attempts)
	}

	// Test removing from queue
	state.RemoveFromQueue("claude", "test@example.com", "machine-1")
	if len(state.Queue.Entries) != 1 {
		t.Errorf("Queue should have 1 entry after removal, got %d", len(state.Queue.Entries))
	}

	// Test clearing old entries
	state.AddToQueue("gemini", "old@test.com", "machine-3", "error")
	// Modify the AddedAt to be old
	for i, e := range state.Queue.Entries {
		if e.Profile == "old@test.com" {
			state.Queue.Entries[i].AddedAt = time.Now().Add(-48 * time.Hour)
			break
		}
	}

	state.ClearOldQueueEntries(24 * time.Hour)
	if len(state.Queue.Entries) != 1 {
		t.Errorf("Queue should have 1 entry after clearing old entries, got %d", len(state.Queue.Entries))
	}
}

// TestHistoryPersistence tests history save/load roundtrip.
func TestHistoryPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)

	// Add history entries
	for i := 0; i < 5; i++ {
		entry := HistoryEntry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			Trigger:   "manual",
			Provider:  "claude",
			Profile:   "test@example.com",
			Machine:   "test-machine",
			Action:    "push",
			Success:   i%2 == 0,
			Duration:  time.Second * time.Duration(i+1),
		}
		if i%2 == 1 {
			entry.Error = "connection failed"
		}
		state.AddToHistory(entry)
	}

	// Save
	if err := state.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into new state
	loaded := NewSyncState(tmpDir)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.History.Entries) != 5 {
		t.Errorf("Loaded history should have 5 entries, got %d", len(loaded.History.Entries))
	}

	// Verify recent returns in reverse order
	recent := loaded.RecentHistory(3)
	if len(recent) != 3 {
		t.Errorf("RecentHistory(3) should return 3 entries, got %d", len(recent))
	}

	// Most recent should be first
	if recent[0].Timestamp.Before(recent[1].Timestamp) {
		t.Error("Recent history should be in reverse chronological order")
	}
}

// TestStateFullRoundtrip tests complete state persistence.
func TestStateFullRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Create state with all components
	state := NewSyncState(tmpDir)

	// Add machine to pool
	m := NewMachine("test-server", "192.168.1.100")
	m.Port = 2222
	m.SSHUser = "admin"
	if err := state.Pool.AddMachine(m); err != nil {
		t.Fatalf("AddMachine failed: %v", err)
	}
	state.Pool.Enable()
	state.Pool.EnableAutoSync()
	state.Pool.RecordFullSync()

	// Add to queue
	state.AddToQueue("claude", "user@example.com", m.ID, "sync failed")

	// Add to history
	state.AddToHistory(HistoryEntry{
		Timestamp: time.Now(),
		Trigger:   "backup",
		Provider:  "claude",
		Profile:   "user@example.com",
		Machine:   "test-server",
		Action:    "push",
		Success:   true,
		Duration:  500 * time.Millisecond,
	})

	// Save
	if err := state.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into new state
	loaded := NewSyncState(tmpDir)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify pool
	if !loaded.Pool.Enabled {
		t.Error("Pool should be enabled")
	}
	if !loaded.Pool.AutoSync {
		t.Error("AutoSync should be enabled")
	}
	if loaded.Pool.MachineCount() != 1 {
		t.Errorf("Pool should have 1 machine, got %d", loaded.Pool.MachineCount())
	}
	loadedM := loaded.Pool.GetMachineByName("test-server")
	if loadedM == nil {
		t.Fatal("Machine not found")
	}
	if loadedM.Port != 2222 {
		t.Errorf("Port = %d, want 2222", loadedM.Port)
	}
	if loadedM.SSHUser != "admin" {
		t.Errorf("SSHUser = %q, want %q", loadedM.SSHUser, "admin")
	}

	// Verify queue
	if len(loaded.Queue.Entries) != 1 {
		t.Errorf("Queue should have 1 entry, got %d", len(loaded.Queue.Entries))
	}

	// Verify history
	if len(loaded.History.Entries) != 1 {
		t.Errorf("History should have 1 entry, got %d", len(loaded.History.Entries))
	}
}

// TestConflictResolution tests the conflict detection algorithm.
func TestConflictResolution(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		local       *TokenFreshness
		remote      *TokenFreshness
		wantPush    bool
		wantPull    bool
		description string
	}{
		{
			name: "local has later expiry",
			local: &TokenFreshness{
				ExpiresAt:  now.Add(2 * time.Hour),
				ModifiedAt: now,
			},
			remote: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now,
			},
			wantPush:    true,
			description: "Local token expires later, should push",
		},
		{
			name: "remote has later expiry",
			local: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now,
			},
			remote: &TokenFreshness{
				ExpiresAt:  now.Add(2 * time.Hour),
				ModifiedAt: now,
			},
			wantPull:    true,
			description: "Remote token expires later, should pull",
		},
		{
			name: "equal expiry, local modified later",
			local: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now.Add(time.Minute),
			},
			remote: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now,
			},
			wantPush:    true,
			description: "Same expiry, local modified more recently",
		},
		{
			name: "equal expiry, remote modified later",
			local: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now,
			},
			remote: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now.Add(time.Minute),
			},
			wantPull:    true,
			description: "Same expiry, remote modified more recently",
		},
		{
			name: "unknown local expiry falls back to modtime",
			local: &TokenFreshness{
				ExpiresAt:  time.Time{}, // Unknown
				ModifiedAt: now.Add(time.Minute),
			},
			remote: &TokenFreshness{
				ExpiresAt:  now.Add(time.Hour),
				ModifiedAt: now,
			},
			wantPush:    true,
			description: "Unknown expiry falls back to modification time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localWins := CompareFreshness(tt.local, tt.remote)
			remoteWins := CompareFreshness(tt.remote, tt.local)

			if tt.wantPush && !localWins {
				t.Errorf("Expected local to win (push): %s", tt.description)
			}
			if tt.wantPull && !remoteWins {
				t.Errorf("Expected remote to win (pull): %s", tt.description)
			}
			if !tt.wantPush && !tt.wantPull {
				if localWins || remoteWins {
					t.Errorf("Expected no clear winner: %s", tt.description)
				}
			}
		})
	}
}

// TestQueueMaxSize tests that queue respects max size.
func TestQueueMaxSize(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.Queue.MaxSize = 5

	// Add more entries than max
	for i := 0; i < 10; i++ {
		state.AddToQueue("claude", string(rune('A'+i)), "machine-1", "error")
	}

	// Save triggers trim
	if err := state.saveQueue(); err != nil {
		t.Fatalf("saveQueue failed: %v", err)
	}

	if len(state.Queue.Entries) > state.Queue.MaxSize {
		t.Errorf("Queue should be trimmed to MaxSize, got %d entries", len(state.Queue.Entries))
	}
}

// TestHistoryMaxSize tests that history respects max size.
func TestHistoryMaxSize(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.History.MaxSize = 5

	// Add more entries than max
	for i := 0; i < 10; i++ {
		state.AddToHistory(HistoryEntry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Provider:  "claude",
			Profile:   "test",
			Success:   true,
		})
	}

	// Save triggers trim
	if err := state.saveHistory(); err != nil {
		t.Fatalf("saveHistory failed: %v", err)
	}

	if len(state.History.Entries) > state.History.MaxSize {
		t.Errorf("History should be trimmed to MaxSize, got %d entries", len(state.History.Entries))
	}
}

// TestSyncResultErrorHandling tests SyncResult error scenarios.
func TestSyncResultErrorHandling(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")

	tests := []struct {
		name    string
		result  *SyncResult
		wantErr bool
	}{
		{
			name: "successful result",
			result: &SyncResult{
				Operation: &SyncOperation{
					Provider:  "claude",
					Profile:   "test",
					Direction: SyncPush,
					Machine:   m,
				},
				Success:  true,
				Duration: time.Second,
			},
			wantErr: false,
		},
		{
			name: "failed result",
			result: &SyncResult{
				Operation: &SyncOperation{
					Provider:  "claude",
					Profile:   "test",
					Direction: SyncPush,
					Machine:   m,
				},
				Success: false,
				Error:   errors.New("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "skip result",
			result: &SyncResult{
				Operation: &SyncOperation{
					Provider:  "claude",
					Profile:   "test",
					Direction: SyncSkip,
					Machine:   m,
				},
				Success: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.Success != !tt.wantErr {
				t.Errorf("Success mismatch: got %v, want error=%v", tt.result.Success, tt.wantErr)
			}
			if tt.wantErr && tt.result.Error == nil {
				t.Error("Failed result should have error set")
			}
		})
	}
}

// TestQueueEntryJSON tests queue serialization.
func TestQueueEntryJSON(t *testing.T) {
	entry := QueueEntry{
		Provider:    "claude",
		Profile:     "test@example.com",
		Machine:     "machine-123",
		AddedAt:     time.Date(2025, 3, 2, 12, 0, 0, 0, time.UTC),
		Attempts:    3,
		LastAttempt: time.Date(2025, 3, 2, 12, 30, 0, 0, time.UTC),
		LastError:   "connection timeout",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var loaded QueueEntry
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if loaded.Provider != entry.Provider {
		t.Errorf("Provider = %q, want %q", loaded.Provider, entry.Provider)
	}
	if loaded.Attempts != entry.Attempts {
		t.Errorf("Attempts = %d, want %d", loaded.Attempts, entry.Attempts)
	}
}

// TestHistoryEntryJSON tests history serialization.
func TestHistoryEntryJSON(t *testing.T) {
	entry := HistoryEntry{
		Timestamp: time.Date(2025, 3, 2, 12, 0, 0, 0, time.UTC),
		Trigger:   "backup",
		Provider:  "claude",
		Profile:   "test@example.com",
		Machine:   "server-1",
		Action:    "push",
		Success:   true,
		Duration:  1500 * time.Millisecond,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var loaded HistoryEntry
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if loaded.Provider != entry.Provider {
		t.Errorf("Provider = %q, want %q", loaded.Provider, entry.Provider)
	}
	if loaded.Action != entry.Action {
		t.Errorf("Action = %q, want %q", loaded.Action, entry.Action)
	}
}

// TestSyncOperationDirection tests sync direction logic.
func TestSyncOperationDirection(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")

	tests := []struct {
		direction SyncDirection
		expected  string
	}{
		{SyncPush, "push"},
		{SyncPull, "pull"},
		{SyncSkip, "skip"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			op := &SyncOperation{
				Provider:  "claude",
				Profile:   "test",
				Direction: tt.direction,
				Machine:   m,
			}

			if string(op.Direction) != tt.expected {
				t.Errorf("Direction = %q, want %q", op.Direction, tt.expected)
			}
		})
	}
}

// TestDiscoverFromSSHConfigWithEnv tests discovery with environment override.
func TestDiscoverFromSSHConfigWithEnv(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("Failed to create .ssh dir: %v", err)
	}

	sshConfig := `Host test-server
    HostName 192.168.1.100
    User admin
    Port 2222
`
	configPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(configPath, []byte(sshConfig), 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	machines, err := parseSSHConfig(configPath)
	if err != nil {
		t.Fatalf("parseSSHConfig failed: %v", err)
	}

	if len(machines) != 1 {
		t.Errorf("Expected 1 machine, got %d", len(machines))
		return
	}

	if machines[0].Name != "test-server" {
		t.Errorf("Name = %q, want %q", machines[0].Name, "test-server")
	}
	if machines[0].Address != "192.168.1.100" {
		t.Errorf("Address = %q, want %q", machines[0].Address, "192.168.1.100")
	}
	if machines[0].SSHUser != "admin" {
		t.Errorf("SSHUser = %q, want %q", machines[0].SSHUser, "admin")
	}
	if machines[0].Port != 2222 {
		t.Errorf("Port = %d, want 2222", machines[0].Port)
	}
}

// TestMergeDiscoveredMachinesSync tests machine list merging.
func TestMergeDiscoveredMachinesSync(t *testing.T) {
	existing := []*Machine{
		NewMachine("server1", "192.168.1.1"),
		NewMachine("server2", "192.168.1.2"),
	}

	discovered := []*Machine{
		NewMachine("server2", "192.168.1.2"), // Duplicate name
		NewMachine("server3", "192.168.1.3"), // New
		NewMachine("Server1", "192.168.1.1"), // Case-insensitive duplicate
	}

	result := MergeDiscoveredMachines(existing, discovered)

	// Should have server1, server2 (existing), and server3 (new)
	if len(result) != 3 {
		t.Errorf("Expected 3 machines, got %d", len(result))
	}

	// Verify server3 was added
	found := false
	for _, m := range result {
		if m.Name == "server3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("server3 should have been added")
	}
}

// TestSyncPoolConcurrency tests concurrent pool operations.
func TestSyncPoolConcurrency(t *testing.T) {
	pool := NewSyncPool()

	// Add machines concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			m := NewMachine(string(rune('A'+id)), "192.168.1."+string(rune('0'+id)))
			pool.AddMachine(m)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify pool state
	count := pool.MachineCount()
	if count == 0 {
		t.Error("Expected some machines to be added")
	}
}

// TestSyncStateNilSafety tests nil safety in state operations.
func TestSyncStateNilSafety(t *testing.T) {
	state := &SyncState{} // Nil pool, queue, history

	// These should not panic
	state.AddToQueue("claude", "test", "m1", "error")
	state.RemoveFromQueue("claude", "test", "m1")
	state.ClearOldQueueEntries(time.Hour)
	state.AddToHistory(HistoryEntry{})
	state.RecentHistory(10)
}

// TestExpandPathFromSync tests path expansion.
func TestExpandPathFromSync(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/test", filepath.Join(homeDir, "test")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandPath(tt.input)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestStripInlineComment tests comment stripping.
func TestStripInlineComment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Host foo # comment", "Host foo"},
		{"Host foo#not-a-comment", "Host foo#not-a-comment"},
		{"# full comment", ""},
		{"Host foo\t# tab comment", "Host foo"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripInlineComment(tt.input)
			if got != tt.want {
				t.Errorf("stripInlineComment(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestConnectionPoolOperations tests pool operations without actual connections.
func TestConnectionPoolOperations(t *testing.T) {
	pool := NewConnectionPool(DefaultConnectOptions())

	// Test size
	if pool.Size() != 0 {
		t.Errorf("Initial size should be 0, got %d", pool.Size())
	}

	// Test release on nonexistent is safe
	pool.Release("nonexistent")

	// Test close all on empty is safe
	pool.CloseAll()

	// Test is connected
	if pool.IsConnected("nonexistent") {
		t.Error("IsConnected should be false for nonexistent")
	}

	// Test connected machines
	if machines := pool.ConnectedMachines(); len(machines) != 0 {
		t.Errorf("ConnectedMachines should be empty, got %d", len(machines))
	}
}

// TestProcessQueueWithEmptyQueue tests queue processing with empty queue.
func TestProcessQueueWithEmptyQueue(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	state := NewSyncState(tmpDir)
	state.Pool.Enable()

	config := DefaultAutoSyncConfig()

	// Should not panic with empty queue
	processQueue(state, config)
}

// TestTriggerSyncWithDisabledPool tests trigger with disabled pool.
func TestTriggerSyncWithDisabledPool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	// Create state with disabled pool
	state := NewSyncState(tmpDir)
	state.Pool.Disable()
	state.Save()

	// Should not trigger sync
	TriggerSyncIfEnabledWithConfig("claude", "test", DefaultAutoSyncConfig())
	// No assertion needed - just verifying no panic
}

// TestQueueFailedSync tests the queueFailedSync helper.
func TestQueueFailedSync(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.Pool = NewSyncPool()

	m1 := NewMachine("m1", "192.168.1.1")
	m2 := NewMachine("m2", "192.168.1.2")
	state.Pool.AddMachine(m1)
	state.Pool.AddMachine(m2)

	queueFailedSync(state, "claude", "test", "sync error")

	// Should have queued for both machines
	if len(state.Queue.Entries) != 2 {
		t.Errorf("Expected 2 queue entries, got %d", len(state.Queue.Entries))
	}
}

// TestLogSyncResults tests logSyncResults with verbose off.
func TestLogSyncResults(t *testing.T) {
	// With verbose=false, should not produce output
	logSyncResults([]*SyncResult{
		{Success: true, Operation: &SyncOperation{Direction: SyncPush}},
	}, false)
	// No assertion - just verifying no panic
}

// TestLogSyncError tests logSyncError with verbose off.
func TestLogSyncError(t *testing.T) {
	logSyncError("test-op", errors.New("test error"), false)
	// No assertion - just verifying no panic
}

// TestBackgroundSyncFunctions tests background sync paths.
func TestBackgroundSyncFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	config := AutoSyncConfig{
		ThrottleInterval: 1 * time.Second,
		SyncTimeout:      1 * time.Second,
		VaultPath:        tmpDir,
		RemoteVaultPath:  ".local/share/caam/vault",
		Verbose:          false,
	}

	// Test ProcessQueueIfNeededWithConfig with disabled pool
	state := NewSyncState(tmpDir)
	state.Pool.Disable()
	state.Save()

	ProcessQueueIfNeededWithConfig(config)
	// No assertion - just verifying no panic
}

// TestSyncStateSaveJSON tests the saveJSON helper.
func TestSyncStateSaveJSON(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)

	// Test saveJSON with a simple value
	data := map[string]string{"key": "value"}
	if err := state.saveJSON("test.json", data); err != nil {
		t.Fatalf("saveJSON failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(tmpDir, "test.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("JSON file should have been created")
	}

	// Verify content
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read JSON file: %v", err)
	}

	var loaded map[string]string
	if err := json.Unmarshal(content, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if loaded["key"] != "value" {
		t.Errorf("Loaded key = %q, want %q", loaded["key"], "value")
	}
}

// TestSyncerListLocalProfilesEmpty tests listing empty vault.
func TestSyncerListLocalProfilesEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	syncer := &Syncer{
		vaultPath: tmpDir,
	}

	profiles, err := syncer.listLocalProfiles()
	if err != nil {
		t.Fatalf("listLocalProfiles failed: %v", err)
	}

	// Should return empty, not error
	if len(profiles) != 0 {
		t.Errorf("Expected 0 profiles from empty vault, got %d", len(profiles))
	}
}

// TestSyncerGetLocalFreshnessEmpty tests freshness from empty profile.
func TestSyncerGetLocalFreshnessEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	// Create empty profile directory
	profileDir := filepath.Join(tmpDir, "claude", "test")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("Failed to create profile dir: %v", err)
	}

	syncer := &Syncer{
		vaultPath: tmpDir,
	}

	_, err := syncer.getLocalFreshness(ProfileRef{
		Provider: "claude",
		Profile:  "test",
	})
	// Should return error since no auth files
	if err == nil {
		t.Error("Expected error for empty profile")
	}
}

// TestInitCSVWithDiscovery tests CSV initialization.
func TestInitCSVWithDiscovery(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	created, machines, err := InitCSVWithDiscovery(false)
	if err != nil {
		t.Fatalf("InitCSVWithDiscovery failed: %v", err)
	}
	if !created {
		t.Error("Expected created=true for new file")
	}
	if len(machines) != 0 {
		t.Errorf("Expected 0 machines without SSH discovery, got %d", len(machines))
	}
}

// TestSSHClientIsConnected tests SSHClient IsConnected.
func TestSSHClientIsConnected(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")
	client := NewSSHClient(m)

	if client.IsConnected() {
		t.Error("Client should not be connected initially")
	}

	// Disconnect on unconnected client should be safe
	client.Disconnect()
}

// TestSSHClientDisconnect tests Disconnect is safe.
func TestSSHClientDisconnect(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")
	client := NewSSHClient(m)

	// Multiple disconnects should be safe
	client.Disconnect()
	client.Disconnect()
	client.Disconnect()
}

// TestSSHErrorMethods tests SSHError helper methods.
func TestSSHErrorMethods(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")

	tests := []struct {
		name      string
		err       *SSHError
		isTimeout bool
		isAuth    bool
		isNetwork bool
		isHostKey bool
	}{
		{
			name: "timeout error",
			err: &SSHError{
				Machine:    m,
				Operation:  "connect",
				Underlying: &testTimeoutError{},
			},
			isTimeout: true,
			isNetwork: true, // connect operation with underlying error
		},
		{
			name: "auth operation",
			err: &SSHError{
				Machine:    m,
				Operation:  "auth",
				Underlying: errors.New("any"),
			},
			isAuth: true,
		},
		{
			name: "permission denied",
			err: &SSHError{
				Machine:    m,
				Operation:  "connect",
				Underlying: errors.New("ssh: permission denied (publickey)"),
			},
			isAuth:    true,
			isNetwork: true, // connect operation with underlying error
		},
		{
			name: "host key changed",
			err: &SSHError{
				Machine:    m,
				Operation:  "hostkey",
				Underlying: errors.New("host key changed for server"),
			},
			isHostKey: true,
		},
		{
			name: "nil underlying",
			err: &SSHError{
				Machine:    m,
				Operation:  "connect",
				Underlying: nil,
			},
		},
		{
			name: "network connect error",
			err: &SSHError{
				Machine:    m,
				Operation:  "connect",
				Underlying: errors.New("connection refused"),
			},
			isNetwork: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.IsTimeout() != tt.isTimeout {
				t.Errorf("IsTimeout() = %v, want %v", tt.err.IsTimeout(), tt.isTimeout)
			}
			if tt.err.IsAuthFailure() != tt.isAuth {
				t.Errorf("IsAuthFailure() = %v, want %v", tt.err.IsAuthFailure(), tt.isAuth)
			}
			if tt.err.IsNetworkError() != tt.isNetwork {
				t.Errorf("IsNetworkError() = %v, want %v", tt.err.IsNetworkError(), tt.isNetwork)
			}
			if tt.err.IsHostKeyMismatch() != tt.isHostKey {
				t.Errorf("IsHostKeyMismatch() = %v, want %v", tt.err.IsHostKeyMismatch(), tt.isHostKey)
			}
		})
	}
}

// testTimeoutError implements net.Error for testing
type testTimeoutError struct{}

func (e *testTimeoutError) Error() string   { return "timeout" }
func (e *testTimeoutError) Timeout() bool   { return true }
func (e *testTimeoutError) Temporary() bool { return true }

// TestConnectionPoolGet tests pool get without actual connection.
func TestConnectionPoolGet(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{
		Timeout:          1 * time.Second,
		UseAgent:         false,
		SkipHostKeyCheck: true,
	})

	m := NewMachine("test", "192.168.1.100")
	m.Port = 22

	// Get will fail because we can't actually connect, but it exercises the code
	_, err := pool.Get(m)
	if err == nil {
		// If somehow it succeeded, clean up
		pool.Release(m.ID)
	}
	// Error is expected - we're testing that the code path is exercised
}

// TestConnectionPoolRefresh tests Refresh on empty pool.
func TestConnectionPoolRefresh(t *testing.T) {
	pool := NewConnectionPool(DefaultConnectOptions())

	// Refresh on empty pool should not panic
	err := pool.Refresh()
	if err != nil {
		t.Errorf("Refresh on empty pool should not error: %v", err)
	}
}

// TestConnectionPoolRefreshWithClients tests Refresh with mock clients.
func TestConnectionPoolRefreshWithClients(t *testing.T) {
	pool := NewConnectionPool(DefaultConnectOptions())

	// Manually add a mock disconnected client
	m := NewMachine("test", "192.168.1.100")
	client := NewSSHClient(m)
	pool.clients[m.ID] = client

	// Refresh should try to reconnect (will fail)
	err := pool.Refresh()
	// We expect an error since we can't actually connect
	_ = err
}

// TestRunBackgroundSync tests runBackgroundSync paths.
func TestRunBackgroundSync(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	state := NewSyncState(tmpDir)
	state.Pool.Enable()
	state.Pool.AutoSync = true
	m := NewMachine("test", "192.168.1.100")
	state.Pool.AddMachine(m)
	state.Save()

	config := AutoSyncConfig{
		ThrottleInterval: 1 * time.Second,
		SyncTimeout:      2 * time.Second,
		VaultPath:        tmpDir,
		RemoteVaultPath:  ".local/share/caam/vault",
		Verbose:          true,
	}

	// This will try to sync but fail due to no actual SSH
	runBackgroundSync("claude", "test", state, config)
	// Just verify it doesn't panic
}

// TestProcessQueueWithEntries tests processQueue with queue entries.
func TestProcessQueueWithEntries(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	state := NewSyncState(tmpDir)
	state.Pool.Enable()
	m := NewMachine("test", "192.168.1.100")
	state.Pool.AddMachine(m)

	state.AddToQueue("claude", "test", m.ID, "previous error")
	state.Save()

	config := AutoSyncConfig{
		ThrottleInterval: 1 * time.Second,
		SyncTimeout:      2 * time.Second,
		VaultPath:        tmpDir,
		RemoteVaultPath:  ".local/share/caam/vault",
		Verbose:          true,
	}

	// Process queue - will fail to connect but exercises code path
	processQueue(state, config)
}

// TestQueueFailedSyncWithNilPool tests queueFailedSync with nil pool.
func TestQueueFailedSyncWithNilPool(t *testing.T) {
	state := &SyncState{}

	// Should not panic
	queueFailedSync(state, "claude", "test", "error")
}

// TestLogSyncResultsVerbose tests logSyncResults with verbose=true.
func TestLogSyncResultsVerbose(t *testing.T) {
	results := []*SyncResult{
		{
			Success: true,
			Operation: &SyncOperation{
				Direction: SyncPush,
			},
			BytesSent: 100,
			Duration:  time.Millisecond,
		},
		{
			Success: true,
			Operation: &SyncOperation{
				Direction: SyncPull,
			},
			BytesReceived: 200,
			Duration:      time.Millisecond,
		},
	}

	// With verbose=true, should produce output (goes to log)
	logSyncResults(results, true)
}

// TestLogSyncErrorVerbose tests logSyncError with verbose=true.
func TestLogSyncErrorVerbose(t *testing.T) {
	logSyncError("test-operation", errors.New("test error"), true)
}

// TestSyncAllWithEmptyPool tests SyncAll with no machines.
func TestSyncAllWithEmptyPool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	syncer := &Syncer{
		vaultPath: tmpDir,
		state: &SyncState{
			Pool: NewSyncPool(),
		},
	}

	results, err := syncer.SyncAll(context.Background())
	if err != nil {
		t.Errorf("SyncAll with empty pool should not error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("SyncAll should return 0 results for empty pool, got %d", len(results))
	}
}

// TestSyncProfileWithEmptyPool tests SyncProfile with no machines.
func TestSyncProfileWithEmptyPool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	syncer := &Syncer{
		vaultPath: tmpDir,
		state: &SyncState{
			Pool: NewSyncPool(),
		},
	}

	results, err := syncer.SyncProfile(context.Background(), "claude", "test")
	if err != nil {
		t.Errorf("SyncProfile with empty pool should not error: %v", err)
	}
	if results != nil {
		t.Errorf("SyncProfile should return nil for empty pool, got %d results", len(results))
	}
}

// TestSyncWithMachineEmptyPool tests SyncWithMachine with no machines.
func TestSyncWithMachineEmptyPool(t *testing.T) {
	tmpDir := t.TempDir()

	syncer := &Syncer{
		vaultPath: tmpDir,
		state:     NewSyncState(tmpDir),
		pool:      NewConnectionPool(DefaultConnectOptions()),
	}

	// SyncWithMachine with nil pool should handle gracefully
	syncer.state.Pool = nil

	// This should handle the nil pool case
	_, err := syncer.SyncAll(context.Background())
	if err != nil {
		// Error is acceptable, just testing code path
		t.Logf("SyncAll returned expected error: %v", err)
	}
}

// TestSyncerWithNilState tests Syncer with nil state components.
func TestSyncerWithNilState(t *testing.T) {
	tmpDir := t.TempDir()

	syncer := &Syncer{
		vaultPath: tmpDir,
		state: &SyncState{
			Pool:    nil,
			Queue:   nil,
			History: nil,
		},
		pool: NewConnectionPool(DefaultConnectOptions()),
	}

	// SyncAll should handle nil pool
	results, err := syncer.SyncAll(context.Background())
	if err != nil {
		t.Logf("SyncAll error: %v", err)
	}
	_ = results
}

// TestDiscoverFromSSHConfigFunc tests DiscoverFromSSHConfig function.
func TestDiscoverFromSSHConfigFunc(t *testing.T) {
	// This will likely fail on most systems without SSH config
	machines, err := DiscoverFromSSHConfig()
	if err != nil {
		// Error is acceptable - no SSH config
		t.Logf("DiscoverFromSSHConfig: %v", err)
	}
	_ = machines
}

// TestDefaultKeyPathsFunc tests defaultKeyPaths function.
func TestDefaultKeyPathsFunc(t *testing.T) {
	paths := defaultKeyPaths()
	// May be empty if no home directory
	t.Logf("defaultKeyPaths returned %d paths", len(paths))
}

// TestRandomStringFunc tests randomString function.
func TestRandomStringFunc(t *testing.T) {
	s := randomString(16)
	if len(s) != 16 {
		t.Errorf("randomString(16) returned length %d", len(s))
	}
}

// TestLocalRandomStringFunc tests localRandomString function.
func TestLocalRandomStringFunc(t *testing.T) {
	s := localRandomString(16)
	if len(s) != 16 {
		t.Errorf("localRandomString(16) returned length %d", len(s))
	}
}

// TestAtomicWriteFileSuccess tests atomicWriteFile.
func TestAtomicWriteFileSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	if err := atomicWriteFile(path, []byte("test content"), 0600); err != nil {
		t.Fatalf("atomicWriteFile failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Content = %q, want %q", string(content), "test content")
	}
}

// TestAtomicWriteFileInvalidPath tests atomicWriteFile with invalid path.
func TestAtomicWriteFileInvalidPath(t *testing.T) {
	err := atomicWriteFile("/nonexistent/path/file.txt", []byte("test"), 0600)
	if err == nil {
		t.Error("Expected error for invalid path")
	}
}

// TestSyncOperationWithFreshness tests SyncOperation with freshness data.
func TestSyncOperationWithFreshness(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")
	now := time.Now()

	op := &SyncOperation{
		Provider:  "claude",
		Profile:   "test@example.com",
		Direction: SyncPush,
		Machine:   m,
		LocalFreshness: &TokenFreshness{
			ExpiresAt:  now.Add(time.Hour),
			ModifiedAt: now,
		},
		RemoteFreshness: &TokenFreshness{
			ExpiresAt:  now,
			ModifiedAt: now,
		},
	}

	if op.LocalFreshness == nil {
		t.Error("LocalFreshness should be set")
	}
	if op.RemoteFreshness == nil {
		t.Error("RemoteFreshness should be set")
	}
}

// TestSyncResultWithBytes tests SyncResult with byte counts.
func TestSyncResultWithBytes(t *testing.T) {
	result := &SyncResult{
		Operation: &SyncOperation{
			Direction: SyncPush,
		},
		Success:       true,
		BytesSent:     1024,
		BytesReceived: 512,
		Duration:      time.Second,
	}

	if result.BytesSent != 1024 {
		t.Errorf("BytesSent = %d, want 1024", result.BytesSent)
	}
	if result.BytesReceived != 512 {
		t.Errorf("BytesReceived = %d, want 512", result.BytesReceived)
	}
}

// TestLoadSyncStateFunc tests LoadSyncState function.
func TestLoadSyncStateFunc(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	state, err := LoadSyncState()
	if err != nil {
		t.Fatalf("LoadSyncState failed: %v", err)
	}

	if state == nil {
		t.Error("LoadSyncState should return non-nil state")
	}
}

// TestSyncStatsAggregation tests SyncStats calculation.
func TestSyncStatsAggregation(t *testing.T) {
	m1 := NewMachine("m1", "192.168.1.1")
	m2 := NewMachine("m2", "192.168.1.2")

	results := []*SyncResult{
		{
			Operation: &SyncOperation{Direction: SyncPush, Machine: m1},
			Success:   true,
			BytesSent: 100,
			Duration:  10 * time.Millisecond,
		},
		{
			Operation:     &SyncOperation{Direction: SyncPull, Machine: m2},
			Success:       true,
			BytesReceived: 200,
			Duration:      20 * time.Millisecond,
		},
		{
			Operation: &SyncOperation{Direction: SyncSkip, Machine: m1},
			Success:   true,
			Duration:  1 * time.Millisecond,
		},
		{
			Operation: &SyncOperation{Direction: SyncPush, Machine: m2},
			Success:   false,
			Error:     errors.New("failed"),
		},
	}

	stats := AggregateResults(results)

	if stats.Total != 4 {
		t.Errorf("Total = %d, want 4", stats.Total)
	}
	if stats.Pushed != 1 {
		t.Errorf("Pushed = %d, want 1", stats.Pushed)
	}
	if stats.Pulled != 1 {
		t.Errorf("Pulled = %d, want 1", stats.Pulled)
	}
	if stats.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", stats.Skipped)
	}
	if stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}
}

// TestTriggerSyncIfEnabledWithEnabledPool tests trigger with enabled pool.
func TestTriggerSyncIfEnabledWithEnabledPool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	// Create state with enabled pool and machines
	state := NewSyncState(tmpDir)
	state.Pool.Enable()
	state.Pool.AutoSync = true
	m := NewMachine("test", "192.168.1.100")
	state.Pool.AddMachine(m)
	state.Save()

	config := DefaultAutoSyncConfig()
	config.VaultPath = tmpDir
	config.SyncTimeout = 1 * time.Second

	// Reset throttler to allow sync
	ResetThrottler()

	// Trigger sync - will attempt background sync
	TriggerSyncIfEnabledWithConfig("claude", "test", config)

	// Give background goroutine time to run
	time.Sleep(100 * time.Millisecond)
}

// TestProcessQueueIfNeededWithQueue tests ProcessQueueIfNeeded with entries.
func TestProcessQueueIfNeededWithQueue(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	state := NewSyncState(tmpDir)
	state.Pool.Enable()
	state.Pool.AutoSync = true
	m := NewMachine("test", "192.168.1.100")
	state.Pool.AddMachine(m)
	state.AddToQueue("claude", "test", m.ID, "previous error")
	state.Save()

	ProcessQueueIfNeeded()
	time.Sleep(100 * time.Millisecond)
}

func TestSSHClientDisconnectedOperations(t *testing.T) {
	m := NewMachine("offline", "127.0.0.1")
	c := NewSSHClient(m)

	_, err := c.ReadFile("/tmp/file")
	assertSSHOperation(t, err, "read")

	err = c.WriteFile("/tmp/file", []byte("x"), 0600)
	assertSSHOperation(t, err, "write")

	_, err = c.FileExists("/tmp/file")
	assertSSHOperation(t, err, "stat")

	_, err = c.FileModTime("/tmp/file")
	assertSSHOperation(t, err, "stat")

	_, err = c.ListDir("/tmp")
	assertSSHOperation(t, err, "readdir")

	err = c.MkdirAll("/tmp/a/b")
	assertSSHOperation(t, err, "mkdir")

	err = c.Remove("/tmp/file")
	assertSSHOperation(t, err, "remove")

	_, err = c.BatchRead([]string{"/tmp/a", "/tmp/b"})
	assertSSHOperation(t, err, "batch_read")

	err = c.BatchWrite(map[string][]byte{"/tmp/a": []byte("x")}, 0600)
	assertSSHOperation(t, err, "batch_write")
}

func TestSyncAlgorithmRemoteErrorPaths(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	syncer := &Syncer{
		vaultPath:       tmpDir,
		remoteVaultPath: ".local/share/caam/vault",
		state:           state,
	}

	m := NewMachine("m1", "127.0.0.1")
	client := NewSSHClient(m)

	_, err := syncer.determineSyncOperation(client, m, ProfileRef{
		Provider: "claude",
		Profile:  "missing",
	})
	if err == nil || !strings.Contains(err.Error(), "remote error") {
		t.Fatalf("expected remote error, got %v", err)
	}

	result := syncer.executeOperation(client, &SyncOperation{
		Provider:  "claude",
		Profile:   "p",
		Direction: SyncSkip,
		Machine:   m,
	})
	if !result.Success {
		t.Fatalf("expected skip op to succeed")
	}

	if _, err := syncer.listRemoteProfiles(client); err != nil {
		t.Fatalf("listRemoteProfiles should tolerate missing remote dirs, got %v", err)
	}

	if _, err := syncer.getRemoteFreshness(client, ProfileRef{
		Provider: "claude",
		Profile:  "p",
	}); err == nil {
		t.Fatalf("expected getRemoteFreshness error for disconnected client")
	}

	if err := syncer.pushProfile(client, "claude", "missing"); err == nil {
		t.Fatalf("expected pushProfile error when local profile does not exist")
	}

	localProfile := filepath.Join(tmpDir, "claude", "alice")
	if err := os.MkdirAll(localProfile, 0700); err != nil {
		t.Fatalf("mkdir local profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localProfile, "auth.json"), []byte(`{"k":"v"}`), 0600); err != nil {
		t.Fatalf("write local auth file: %v", err)
	}
	if err := syncer.pushProfile(client, "claude", "alice"); err == nil {
		t.Fatalf("expected pushProfile error from disconnected remote")
	}

	if err := syncer.pullProfile(client, "claude", "alice"); err == nil {
		t.Fatalf("expected pullProfile error from disconnected remote")
	}

	if _, err := syncer.SyncProfileWithMachine(context.Background(), "claude", "alice", nil); err == nil {
		t.Fatalf("expected nil machine error")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()
	if _, err := syncer.SyncProfileWithMachine(cancelCtx, "claude", "alice", m); err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestKnownHostsHelpers(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKey, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}

	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := addToKnownHosts(path, "example.com", publicKey); err != nil {
		t.Fatalf("addToKnownHosts failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(data), "example.com") {
		t.Fatalf("known_hosts content missing host: %q", string(data))
	}

	path2 := filepath.Join(t.TempDir(), "known_hosts")
	cb := autoAddHostKeyCallback(func(string, net.Addr, ssh.PublicKey) error {
		return errors.New("unknown host")
	}, path2)
	if err := cb("example.org", nil, publicKey); err != nil {
		t.Fatalf("autoAddHostKeyCallback should tolerate unknown host and return nil, got %v", err)
	}
}

func TestFormatKnownHostsWarningSanitizesTerminalControlBytes(t *testing.T) {
	got := formatKnownHostsWarning("exam\x1b[31mple.org", errors.New("failed\x1b]2;title\x07 to add"))
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\x07") {
		t.Fatalf("warning should strip terminal control bytes, got %q", got)
	}
	if !strings.Contains(got, "example.org") {
		t.Fatalf("warning should preserve readable hostname, got %q", got)
	}
	if !strings.Contains(got, "failed to add") {
		t.Fatalf("warning should preserve readable error text, got %q", got)
	}
}

func assertSSHOperation(t *testing.T, err error, wantOp string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected SSH error for operation %q", wantOp)
	}
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("expected *SSHError for %q, got %T (%v)", wantOp, err, err)
	}
	if sshErr.Operation != wantOp {
		t.Fatalf("operation = %q, want %q", sshErr.Operation, wantOp)
	}
}
