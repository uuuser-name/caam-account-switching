package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestParseAddress tests the ParseAddress function.
func TestParseAddress(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort int
		wantUser string
	}{
		// Simple hostname
		{"example.com", "example.com", 0, ""},
		{"192.168.1.100", "192.168.1.100", 0, ""},

		// With port
		{"example.com:22", "example.com", 22, ""},
		{"192.168.1.100:2222", "192.168.1.100", 2222, ""},

		// With user
		{"user@example.com", "example.com", 0, "user"},
		{"jeff@192.168.1.100", "192.168.1.100", 0, "jeff"},

		// With user and port
		{"user@example.com:22", "example.com", 22, "user"},
		{"jeff@192.168.1.100:2222", "192.168.1.100", 2222, "jeff"},

		// IPv6
		{"[::1]", "::1", 0, ""},
		{"[::1]:22", "::1", 22, ""},
		{"user@[::1]:22", "::1", 22, "user"},

		// Edge cases
		{"", "", 0, ""},
		{"  example.com  ", "example.com", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, port, user := ParseAddress(tt.input)
			if host != tt.wantHost {
				t.Errorf("ParseAddress(%q) host = %q, want %q", tt.input, host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("ParseAddress(%q) port = %d, want %d", tt.input, port, tt.wantPort)
			}
			if user != tt.wantUser {
				t.Errorf("ParseAddress(%q) user = %q, want %q", tt.input, user, tt.wantUser)
			}
		})
	}
}

// TestNormalizeAddress tests the NormalizeAddress function.
func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		host string
		port int
		user string
		want string
	}{
		{"example.com", 0, "", "example.com"},
		{"example.com", 22, "", "example.com"}, // Default port omitted
		{"example.com", 2222, "", "example.com:2222"},
		{"example.com", 0, "jeff", "jeff@example.com"},
		{"example.com", 2222, "jeff", "jeff@example.com:2222"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := NormalizeAddress(tt.host, tt.port, tt.user)
			if got != tt.want {
				t.Errorf("NormalizeAddress(%q, %d, %q) = %q, want %q",
					tt.host, tt.port, tt.user, got, tt.want)
			}
		})
	}
}

// TestMachine tests Machine operations.
func TestMachine(t *testing.T) {
	m := NewMachine("test-machine", "192.168.1.100")

	// Check defaults
	if m.Port != DefaultSSHPort {
		t.Errorf("New machine port = %d, want %d", m.Port, DefaultSSHPort)
	}
	if m.Status != StatusUnknown {
		t.Errorf("New machine status = %q, want %q", m.Status, StatusUnknown)
	}
	if m.Source != SourceManual {
		t.Errorf("New machine source = %q, want %q", m.Source, SourceManual)
	}
	if m.ID == "" {
		t.Error("New machine should have an ID")
	}

	// Test HostPort
	expected := "192.168.1.100:22"
	if got := m.HostPort(); got != expected {
		t.Errorf("HostPort() = %q, want %q", got, expected)
	}

	// Test status updates
	m.SetError("connection refused")
	if m.Status != StatusError {
		t.Errorf("After SetError, status = %q, want %q", m.Status, StatusError)
	}
	if m.LastError != "connection refused" {
		t.Errorf("After SetError, LastError = %q, want %q", m.LastError, "connection refused")
	}

	m.SetOnline()
	if m.Status != StatusOnline {
		t.Errorf("After SetOnline, status = %q, want %q", m.Status, StatusOnline)
	}
	if m.LastError != "" {
		t.Error("After SetOnline, LastError should be cleared")
	}

	// Test validation
	if err := m.Validate(); err != nil {
		t.Errorf("Valid machine should not error: %v", err)
	}

	emptyMachine := &Machine{}
	if err := emptyMachine.Validate(); err == nil {
		t.Error("Empty machine should fail validation")
	}
}

// TestMachineSetOffline tests the SetOffline method.
func TestMachineSetOffline(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")

	// Set to online first
	m.SetOnline()
	if m.Status != StatusOnline {
		t.Errorf("After SetOnline, status = %q, want %q", m.Status, StatusOnline)
	}

	// Now set offline
	m.SetOffline()
	if m.Status != StatusOffline {
		t.Errorf("After SetOffline, status = %q, want %q", m.Status, StatusOffline)
	}
}

// TestMachineRecordSync tests the RecordSync method.
func TestMachineRecordSync(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")

	// Set an error first
	m.SetError("connection refused")
	if m.Status != StatusError {
		t.Errorf("After SetError, status = %q, want %q", m.Status, StatusError)
	}
	if m.LastError != "connection refused" {
		t.Errorf("After SetError, LastError = %q, want %q", m.LastError, "connection refused")
	}

	// Record a successful sync
	beforeSync := time.Now()
	m.RecordSync()
	afterSync := time.Now()

	if m.Status != StatusOnline {
		t.Errorf("After RecordSync, status = %q, want %q", m.Status, StatusOnline)
	}
	if m.LastError != "" {
		t.Error("After RecordSync, LastError should be cleared")
	}
	if m.LastSync.Before(beforeSync) || m.LastSync.After(afterSync) {
		t.Errorf("RecordSync LastSync = %v, should be between %v and %v", m.LastSync, beforeSync, afterSync)
	}
}

// TestValidationError tests the ValidationError.Error method.
func TestValidationError(t *testing.T) {
	tests := []struct {
		field   string
		message string
		want    string
	}{
		{"name", "machine name is required", "name: machine name is required"},
		{"address", "machine address is required", "address: machine address is required"},
		{"port", "invalid port number", "port: invalid port number"},
		{"", "some message", ": some message"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			err := &ValidationError{Field: tt.field, Message: tt.message}
			if got := err.Error(); got != tt.want {
				t.Errorf("ValidationError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMachinesEqual tests the MachinesEqual function.
func TestMachinesEqual(t *testing.T) {
	m1 := NewMachine("m1", "192.168.1.100")
	m1.Port = 22

	m2 := NewMachine("m2", "192.168.1.100")
	m2.Port = 22

	m3 := NewMachine("m3", "192.168.1.100")
	m3.Port = 2222

	m4 := NewMachine("m4", "192.168.1.200")
	m4.Port = 22

	if !MachinesEqual(m1, m2) {
		t.Error("Machines with same address:port should be equal")
	}
	if MachinesEqual(m1, m3) {
		t.Error("Machines with different port should not be equal")
	}
	if MachinesEqual(m1, m4) {
		t.Error("Machines with different address should not be equal")
	}
	if MachinesEqual(nil, m1) || MachinesEqual(m1, nil) {
		t.Error("nil machines should not be equal to non-nil")
	}
	if !MachinesEqual(nil, nil) {
		t.Error("nil == nil should be true")
	}
}

// TestSyncPool tests SyncPool operations.
func TestSyncPool(t *testing.T) {
	pool := NewSyncPool()

	// Check defaults
	if pool.Enabled {
		t.Error("New pool should have Enabled = false")
	}
	if pool.AutoSync {
		t.Error("New pool should have AutoSync = false")
	}
	if !pool.IsEmpty() {
		t.Error("New pool should be empty")
	}

	// Add machine
	m := NewMachine("test", "192.168.1.100")
	if err := pool.AddMachine(m); err != nil {
		t.Errorf("AddMachine failed: %v", err)
	}

	if pool.IsEmpty() {
		t.Error("Pool should not be empty after AddMachine")
	}
	if pool.MachineCount() != 1 {
		t.Errorf("Pool count = %d, want 1", pool.MachineCount())
	}

	// Get by ID
	if got := pool.GetMachine(m.ID); got != m {
		t.Error("GetMachine should return added machine")
	}

	// Get by name
	if got := pool.GetMachineByName("test"); got != m {
		t.Error("GetMachineByName should return added machine")
	}

	// Duplicate name should fail
	m2 := NewMachine("test", "192.168.1.200")
	if err := pool.AddMachine(m2); err == nil {
		t.Error("Adding duplicate name should fail")
	}

	// Remove machine
	if err := pool.RemoveMachine(m.ID); err != nil {
		t.Errorf("RemoveMachine failed: %v", err)
	}
	if !pool.IsEmpty() {
		t.Error("Pool should be empty after RemoveMachine")
	}

	// Remove non-existent should fail
	if err := pool.RemoveMachine("nonexistent"); err == nil {
		t.Error("Removing non-existent machine should fail")
	}
}

// TestSyncPoolAdvanced tests additional SyncPool operations.
func TestSyncPoolAdvanced(t *testing.T) {
	pool := NewSyncPool()

	// Add multiple machines
	m1 := NewMachine("alpha", "192.168.1.1")
	m2 := NewMachine("beta", "192.168.1.2")
	m3 := NewMachine("gamma", "192.168.1.3")

	for _, m := range []*Machine{m1, m2, m3} {
		if err := pool.AddMachine(m); err != nil {
			t.Fatalf("AddMachine failed: %v", err)
		}
	}

	// Test ListMachines returns sorted list
	t.Run("ListMachines returns sorted", func(t *testing.T) {
		machines := pool.ListMachines()
		if len(machines) != 3 {
			t.Errorf("ListMachines() len = %d, want 3", len(machines))
		}
		// Should be sorted alphabetically: alpha, beta, gamma
		if machines[0].Name != "alpha" || machines[1].Name != "beta" || machines[2].Name != "gamma" {
			t.Error("ListMachines should return machines sorted by name")
		}
	})

	// Test Disable
	t.Run("Disable", func(t *testing.T) {
		pool.Enable()
		if !pool.Enabled {
			t.Error("Pool should be enabled after Enable()")
		}

		pool.Disable()
		if pool.Enabled {
			t.Error("Pool should be disabled after Disable()")
		}
	})

	// Test EnableAutoSync / DisableAutoSync
	t.Run("AutoSync toggle", func(t *testing.T) {
		pool.EnableAutoSync()
		if !pool.AutoSync {
			t.Error("AutoSync should be true after EnableAutoSync()")
		}

		pool.DisableAutoSync()
		if pool.AutoSync {
			t.Error("AutoSync should be false after DisableAutoSync()")
		}
	})

	// Test RecordFullSync
	t.Run("RecordFullSync", func(t *testing.T) {
		before := time.Now()
		pool.RecordFullSync()
		after := time.Now()

		if pool.LastFullSync.Before(before) || pool.LastFullSync.After(after) {
			t.Errorf("RecordFullSync timestamp = %v, should be between %v and %v",
				pool.LastFullSync, before, after)
		}
	})

	// Test OnlineMachines
	t.Run("OnlineMachines", func(t *testing.T) {
		// Set some machines online
		m1.SetOnline()
		m2.SetOffline()
		m3.SetError("test error")

		online := pool.OnlineMachines()
		if len(online) != 1 {
			t.Errorf("OnlineMachines() len = %d, want 1", len(online))
		}
		if len(online) > 0 && online[0].Name != "alpha" {
			t.Error("OnlineMachines should return only online machine")
		}
	})

	// Test OfflineMachines (includes offline and error statuses)
	t.Run("OfflineMachines", func(t *testing.T) {
		offline := pool.OfflineMachines()
		if len(offline) != 2 {
			t.Errorf("OfflineMachines() len = %d, want 2", len(offline))
		}
	})

	// Test UpdateMachine
	t.Run("UpdateMachine success", func(t *testing.T) {
		m1.Address = "10.0.0.1"
		if err := pool.UpdateMachine(m1); err != nil {
			t.Errorf("UpdateMachine failed: %v", err)
		}

		updated := pool.GetMachine(m1.ID)
		if updated.Address != "10.0.0.1" {
			t.Error("UpdateMachine should update the machine")
		}
	})

	t.Run("UpdateMachine not found", func(t *testing.T) {
		unknown := NewMachine("unknown", "1.2.3.4")
		if err := pool.UpdateMachine(unknown); err == nil {
			t.Error("UpdateMachine should fail for unknown machine")
		}
	})

	t.Run("UpdateMachine invalid", func(t *testing.T) {
		invalid := &Machine{ID: m1.ID, Name: ""} // Empty name is invalid
		if err := pool.UpdateMachine(invalid); err == nil {
			t.Error("UpdateMachine should fail for invalid machine")
		}
	})
}

// TestSyncPoolPersistence tests saving and loading SyncPool.
func TestSyncPoolPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME to redirect data storage
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Create and save pool
	pool := NewSyncPool()
	pool.Enable()

	m := NewMachine("test", "192.168.1.100")
	if err := pool.AddMachine(m); err != nil {
		t.Fatalf("AddMachine failed: %v", err)
	}

	if err := pool.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load pool
	loaded, err := LoadSyncPool()
	if err != nil {
		t.Fatalf("LoadSyncPool failed: %v", err)
	}

	if !loaded.Enabled {
		t.Error("Loaded pool should have Enabled = true")
	}
	if loaded.MachineCount() != 1 {
		t.Errorf("Loaded pool count = %d, want 1", loaded.MachineCount())
	}
	if loaded.GetMachineByName("test") == nil {
		t.Error("Loaded pool should have test machine")
	}
}

// TestLocalIdentity tests identity creation and loading.
func TestLocalIdentity(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME to redirect data storage
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Create identity
	identity, err := GetOrCreateLocalIdentity()
	if err != nil {
		t.Fatalf("GetOrCreateLocalIdentity failed: %v", err)
	}

	if identity.ID == "" {
		t.Error("Identity should have an ID")
	}
	if identity.Hostname == "" {
		t.Error("Identity should have a hostname")
	}
	if identity.CreatedAt.IsZero() {
		t.Error("Identity should have CreatedAt")
	}

	// Load same identity
	loaded, err := GetOrCreateLocalIdentity()
	if err != nil {
		t.Fatalf("Second GetOrCreateLocalIdentity failed: %v", err)
	}

	if loaded.ID != identity.ID {
		t.Error("Loading should return same identity")
	}
}

// TestSSHConfigParsing tests SSH config file parsing.
func TestSSHConfigParsing(t *testing.T) {
	sshConfig := `# Test SSH config
Host work-laptop
    HostName 192.168.1.100
    Port 22
    User jeff
    IdentityFile ~/.ssh/work_key

Host home-desktop
    HostName 10.0.0.50
    Port 2222
    User admin

Host github.com
    HostName github.com
    User git

Host *
    AddKeysToAgent yes

Host proxy-server
    HostName 192.168.1.200
    ProxyJump bastion
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte(sshConfig), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	machines, err := parseSSHConfig(configPath)
	if err != nil {
		t.Fatalf("parseSSHConfig failed: %v", err)
	}

	// Should have work-laptop and home-desktop (not github.com, not *, not proxy-server)
	if len(machines) != 2 {
		t.Errorf("Expected 2 machines, got %d", len(machines))
		for _, m := range machines {
			t.Logf("  Machine: %s (%s)", m.Name, m.Address)
		}
	}

	// Check work-laptop
	var workLaptop *Machine
	for _, m := range machines {
		if m.Name == "work-laptop" {
			workLaptop = m
			break
		}
	}

	if workLaptop == nil {
		t.Fatal("work-laptop not found")
	}
	if workLaptop.Address != "192.168.1.100" {
		t.Errorf("work-laptop address = %q, want %q", workLaptop.Address, "192.168.1.100")
	}
	if workLaptop.Port != 22 {
		t.Errorf("work-laptop port = %d, want 22", workLaptop.Port)
	}
	if workLaptop.SSHUser != "jeff" {
		t.Errorf("work-laptop user = %q, want %q", workLaptop.SSHUser, "jeff")
	}
	if workLaptop.Source != SourceSSHConfig {
		t.Errorf("work-laptop source = %q, want %q", workLaptop.Source, SourceSSHConfig)
	}
}

func TestSSHConfigParsing_MultiHost(t *testing.T) {
	sshConfig := `# Test SSH config with multiple hosts on one line
Host work-laptop work-tower
    HostName 10.0.0.1
    Port 22
    User jeff
    IdentityFile ~/.ssh/work_key
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte(sshConfig), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	machines, err := parseSSHConfig(configPath)
	if err != nil {
		t.Fatalf("parseSSHConfig failed: %v", err)
	}

	if len(machines) != 2 {
		t.Fatalf("Expected 2 machines, got %d", len(machines))
	}

	var workLaptop, workTower *Machine
	for _, m := range machines {
		switch m.Name {
		case "work-laptop":
			workLaptop = m
		case "work-tower":
			workTower = m
		}
	}

	if workLaptop == nil || workTower == nil {
		t.Fatalf("Missing expected machines: laptop=%v tower=%v", workLaptop != nil, workTower != nil)
	}

	for _, m := range []*Machine{workLaptop, workTower} {
		if m.Address != "10.0.0.1" {
			t.Errorf("%s address = %q, want %q", m.Name, m.Address, "10.0.0.1")
		}
		if m.Port != 22 {
			t.Errorf("%s port = %d, want 22", m.Name, m.Port)
		}
		if m.SSHUser != "jeff" {
			t.Errorf("%s user = %q, want %q", m.Name, m.SSHUser, "jeff")
		}
		if m.Source != SourceSSHConfig {
			t.Errorf("%s source = %q, want %q", m.Name, m.Source, SourceSSHConfig)
		}
	}
}

func TestSSHConfigParsing_InlineComments(t *testing.T) {
	sshConfig := `# Test SSH config with inline comments
Host work-laptop # office machine
    HostName 192.168.1.100 # private LAN
    User jeff
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte(sshConfig), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	machines, err := parseSSHConfig(configPath)
	if err != nil {
		t.Fatalf("parseSSHConfig failed: %v", err)
	}

	if len(machines) != 1 {
		t.Fatalf("Expected 1 machine, got %d", len(machines))
	}

	m := machines[0]
	if m.Name != "work-laptop" {
		t.Fatalf("Name = %q, want %q", m.Name, "work-laptop")
	}
	if m.Address != "192.168.1.100" {
		t.Fatalf("Address = %q, want %q", m.Address, "192.168.1.100")
	}
	if m.SSHUser != "jeff" {
		t.Fatalf("SSHUser = %q, want %q", m.SSHUser, "jeff")
	}
}

// TestCSVOperations tests CSV file operations.
func TestCSVOperations(t *testing.T) {
	tmpDir := t.TempDir()

	// Set HOME to redirect CSV file location
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create CSV directory
	csvDir := filepath.Join(tmpDir, ".caam")
	if err := os.MkdirAll(csvDir, 0700); err != nil {
		t.Fatalf("Failed to create CSV dir: %v", err)
	}

	// Write test CSV
	csvContent := `# Test CSV
machine_name,address,ssh_key_path
work-laptop,192.168.1.100,~/.ssh/id_ed25519
home-desktop,admin@10.0.0.50:2222,~/.ssh/home_key
`
	csvPath := filepath.Join(csvDir, CSVFileName)
	if err := os.WriteFile(csvPath, []byte(csvContent), 0600); err != nil {
		t.Fatalf("Failed to write CSV: %v", err)
	}

	// Load from CSV
	machines, err := LoadFromCSV()
	if err != nil {
		t.Fatalf("LoadFromCSV failed: %v", err)
	}

	if len(machines) != 2 {
		t.Errorf("Expected 2 machines, got %d", len(machines))
	}

	// Check work-laptop
	var found bool
	for _, m := range machines {
		if m.Name == "work-laptop" {
			found = true
			if m.Address != "192.168.1.100" {
				t.Errorf("work-laptop address = %q, want %q", m.Address, "192.168.1.100")
			}
			if m.Source != SourceCSV {
				t.Errorf("work-laptop source = %q, want %q", m.Source, SourceCSV)
			}
		}
	}
	if !found {
		t.Error("work-laptop not found in loaded machines")
	}
}

// TestSyncQueue tests queue operations.
func TestSyncQueue(t *testing.T) {
	state := NewSyncState(t.TempDir())

	// Add entries
	state.AddToQueue("claude", "alice@gmail.com", "m1", "connection error")
	state.AddToQueue("codex", "work@company.com", "m2", "timeout")

	if len(state.Queue.Entries) != 2 {
		t.Errorf("Queue len = %d, want 2", len(state.Queue.Entries))
	}

	// Adding same entry should update, not duplicate
	state.AddToQueue("claude", "alice@gmail.com", "m1", "retry error")
	if len(state.Queue.Entries) != 2 {
		t.Errorf("Queue len after duplicate = %d, want 2", len(state.Queue.Entries))
	}

	// Check that attempts was incremented
	for _, e := range state.Queue.Entries {
		if e.Provider == "claude" && e.Profile == "alice@gmail.com" {
			if e.Attempts != 2 {
				t.Errorf("Attempts = %d, want 2", e.Attempts)
			}
		}
	}

	// Remove from queue
	state.RemoveFromQueue("claude", "alice@gmail.com", "m1")
	if len(state.Queue.Entries) != 1 {
		t.Errorf("Queue len after remove = %d, want 1", len(state.Queue.Entries))
	}
}

// TestSyncHistory tests history operations.
func TestSyncHistory(t *testing.T) {
	state := NewSyncState(t.TempDir())

	// Add entries
	for i := 0; i < 5; i++ {
		state.AddToHistory(HistoryEntry{
			Timestamp: time.Now(),
			Provider:  "claude",
			Profile:   "test",
			Machine:   "test-machine",
			Success:   i%2 == 0,
		})
	}

	if len(state.History.Entries) != 5 {
		t.Errorf("History len = %d, want 5", len(state.History.Entries))
	}

	// Recent should return in reverse order
	recent := state.RecentHistory(3)
	if len(recent) != 3 {
		t.Errorf("RecentHistory(3) len = %d, want 3", len(recent))
	}

	// Most recent should be first
	if recent[0].Timestamp.Before(recent[1].Timestamp) {
		t.Error("Recent history should be in reverse chronological order")
	}
}

// TestSyncStatePersistence tests full state save/load cycle.
func TestSyncStatePersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Set XDG_DATA_HOME to redirect data storage
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Create state with base path in temp dir
	basePath := filepath.Join(tmpDir, "caam", "sync")
	state := NewSyncState(basePath)

	// First load to create identity
	if err := state.Load(); err != nil {
		t.Fatalf("Initial Load failed: %v", err)
	}

	// Add some data
	m := NewMachine("test", "192.168.1.100")
	if err := state.Pool.AddMachine(m); err != nil {
		t.Fatalf("AddMachine failed: %v", err)
	}
	state.Pool.Enable()

	state.AddToQueue("claude", "test", m.ID, "test error")
	state.AddToHistory(HistoryEntry{
		Timestamp: time.Now(),
		Provider:  "claude",
		Success:   true,
	})

	// Save
	if err := state.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into new state
	loaded := NewSyncState(basePath)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !loaded.Pool.Enabled {
		t.Error("Loaded pool should be enabled")
	}
	if loaded.Pool.MachineCount() != 1 {
		t.Errorf("Loaded pool count = %d, want 1", loaded.Pool.MachineCount())
	}
	if len(loaded.Queue.Entries) != 1 {
		t.Errorf("Loaded queue len = %d, want 1", len(loaded.Queue.Entries))
	}
	if len(loaded.History.Entries) != 1 {
		t.Errorf("Loaded history len = %d, want 1", len(loaded.History.Entries))
	}
}

// TestEnsureCSVFile tests the EnsureCSVFile function.
func TestEnsureCSVFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Set HOME to redirect CSV file location
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// First call should create the file
	created, err := EnsureCSVFile()
	if err != nil {
		t.Fatalf("EnsureCSVFile failed: %v", err)
	}
	if !created {
		t.Error("First call should return created=true")
	}

	// Verify file exists
	csvPath := filepath.Join(tmpDir, ".caam", CSVFileName)
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		t.Error("CSV file should exist after EnsureCSVFile")
	}

	// Second call should not create
	created, err = EnsureCSVFile()
	if err != nil {
		t.Fatalf("Second EnsureCSVFile failed: %v", err)
	}
	if created {
		t.Error("Second call should return created=false")
	}
}

// TestSaveToCSV tests the SaveToCSV function.
func TestSaveToCSV(t *testing.T) {
	tmpDir := t.TempDir()

	// Set HOME to redirect CSV file location
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create some machines to save
	machines := []*Machine{
		NewMachine("server1", "192.168.1.100"),
		NewMachine("server2", "10.0.0.50"),
	}
	machines[0].SSHUser = "admin"
	machines[0].Port = 2222
	machines[1].SSHKeyPath = filepath.Join(tmpDir, ".ssh", "id_ed25519")

	// Save to CSV
	if err := SaveToCSV(machines); err != nil {
		t.Fatalf("SaveToCSV failed: %v", err)
	}

	// Verify file exists
	csvPath := filepath.Join(tmpDir, ".caam", CSVFileName)
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		t.Error("CSV file should exist after SaveToCSV")
	}

	// Read back and verify
	content, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("Failed to read CSV file: %v", err)
	}

	// Should contain header and machine entries
	contentStr := string(content)
	if !containsAll(contentStr, "machine_name,address,ssh_key_path", "server1", "server2") {
		t.Error("CSV should contain header and machine entries")
	}
}

// TestSaveToCSVPreservesComments tests that SaveToCSV preserves existing comments.
func TestSaveToCSVPreservesComments(t *testing.T) {
	tmpDir := t.TempDir()

	// Set HOME to redirect CSV file location
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create CSV directory and file with custom comments
	csvDir := filepath.Join(tmpDir, ".caam")
	if err := os.MkdirAll(csvDir, 0700); err != nil {
		t.Fatalf("Failed to create CSV dir: %v", err)
	}

	customComment := "# My Custom Comment\n# Second line\nmachine_name,address,ssh_key_path\n"
	csvPath := filepath.Join(csvDir, CSVFileName)
	if err := os.WriteFile(csvPath, []byte(customComment), 0600); err != nil {
		t.Fatalf("Failed to write CSV: %v", err)
	}

	// Save new machines
	machines := []*Machine{NewMachine("test", "192.168.1.1")}
	if err := SaveToCSV(machines); err != nil {
		t.Fatalf("SaveToCSV failed: %v", err)
	}

	// Read and verify comments are preserved
	content, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("Failed to read CSV file: %v", err)
	}

	if !containsAll(string(content), "# My Custom Comment", "# Second line", "test") {
		t.Error("SaveToCSV should preserve existing comments")
	}
}

// TestClearOldQueueEntries tests the ClearOldQueueEntries function.
func TestClearOldQueueEntries(t *testing.T) {
	state := NewSyncState(t.TempDir())

	// Add entries with different ages
	now := time.Now()

	// Add entries directly to manipulate timestamps
	state.Queue.Entries = []QueueEntry{
		{Provider: "claude", Profile: "old1", Machine: "m1", AddedAt: now.Add(-48 * time.Hour)},
		{Provider: "claude", Profile: "old2", Machine: "m2", AddedAt: now.Add(-25 * time.Hour)},
		{Provider: "claude", Profile: "new1", Machine: "m3", AddedAt: now.Add(-1 * time.Hour)},
		{Provider: "codex", Profile: "new2", Machine: "m4", AddedAt: now.Add(-30 * time.Minute)},
	}

	// Clear entries older than 24 hours
	state.ClearOldQueueEntries(24 * time.Hour)

	// Should have 2 entries left (new1 and new2)
	if len(state.Queue.Entries) != 2 {
		t.Errorf("After ClearOldQueueEntries(24h), queue len = %d, want 2", len(state.Queue.Entries))
	}

	// Verify the right entries remain
	for _, e := range state.Queue.Entries {
		if e.Profile == "old1" || e.Profile == "old2" {
			t.Errorf("Old entry %q should have been removed", e.Profile)
		}
	}
}

// TestClearOldQueueEntriesNilQueue tests ClearOldQueueEntries with nil queue.
func TestClearOldQueueEntriesNilQueue(t *testing.T) {
	state := NewSyncState(t.TempDir())
	state.Queue = nil

	// Should not panic
	state.ClearOldQueueEntries(24 * time.Hour)

	if state.Queue != nil {
		t.Error("ClearOldQueueEntries should not create queue when nil")
	}
}

// containsAll checks if all substrings are in the string.
func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestLoadLocalIdentity tests loading identity without creating.
func TestLoadLocalIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Should return nil when no identity exists
	identity, err := LoadLocalIdentity()
	if err != nil {
		t.Fatalf("LoadLocalIdentity() error = %v", err)
	}
	if identity != nil {
		t.Error("LoadLocalIdentity() should return nil when no identity exists")
	}

	// Create an identity first
	created, err := GetOrCreateLocalIdentity()
	if err != nil {
		t.Fatalf("GetOrCreateLocalIdentity() error = %v", err)
	}

	// Now LoadLocalIdentity should return it
	loaded, err := LoadLocalIdentity()
	if err != nil {
		t.Fatalf("LoadLocalIdentity() after create error = %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadLocalIdentity() should return identity after creation")
	}
	if loaded.ID != created.ID {
		t.Errorf("Loaded ID = %q, want %q", loaded.ID, created.ID)
	}
}

// TestSyncDataDir tests SyncDataDir with different env settings.
func TestSyncDataDir(t *testing.T) {
	t.Run("with CAAM_HOME", func(t *testing.T) {
		oldCaam := os.Getenv("CAAM_HOME")
		oldXDG := os.Getenv("XDG_DATA_HOME")
		os.Setenv("CAAM_HOME", "/custom/caam")
		os.Setenv("XDG_DATA_HOME", "/custom/data")
		defer os.Setenv("CAAM_HOME", oldCaam)
		defer os.Setenv("XDG_DATA_HOME", oldXDG)

		dir := SyncDataDir()
		if dir != "/custom/caam/data/sync" {
			t.Errorf("SyncDataDir() = %q, want %q", dir, "/custom/caam/data/sync")
		}
	})

	t.Run("with XDG_DATA_HOME", func(t *testing.T) {
		oldCaam := os.Getenv("CAAM_HOME")
		oldXDG := os.Getenv("XDG_DATA_HOME")
		os.Unsetenv("CAAM_HOME")
		os.Setenv("XDG_DATA_HOME", "/custom/data")
		defer os.Setenv("CAAM_HOME", oldCaam)
		defer os.Setenv("XDG_DATA_HOME", oldXDG)

		dir := SyncDataDir()
		if !strings.Contains(dir, "/custom/data") {
			t.Errorf("SyncDataDir() = %q, want to contain /custom/data", dir)
		}
		if !strings.HasSuffix(dir, filepath.Join("caam", "sync")) {
			t.Errorf("SyncDataDir() = %q, want to end with caam/sync", dir)
		}
	})

	t.Run("without XDG_DATA_HOME", func(t *testing.T) {
		oldCaam := os.Getenv("CAAM_HOME")
		oldXDG := os.Getenv("XDG_DATA_HOME")
		os.Unsetenv("CAAM_HOME")
		os.Unsetenv("XDG_DATA_HOME")
		defer os.Setenv("CAAM_HOME", oldCaam)
		defer os.Setenv("XDG_DATA_HOME", oldXDG)

		dir := SyncDataDir()
		// Should use home directory
		if !strings.Contains(dir, ".local/share/caam/sync") {
			t.Errorf("SyncDataDir() = %q, want to contain .local/share/caam/sync", dir)
		}
	})
}

// TestExpandPath tests path expansion with tilde.
func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		input string
		want  string
	}{
		{"~/.ssh/id_rsa", filepath.Join(homeDir, ".ssh/id_rsa")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~/", filepath.Join(homeDir, "")},
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

// TestToMachinesEdgeCases tests edge cases for toMachines.
func TestToMachinesEdgeCases(t *testing.T) {
	t.Run("empty names", func(t *testing.T) {
		h := &sshHost{names: nil}
		if machines := h.toMachines(); len(machines) != 0 {
			t.Error("toMachines() with empty names should return no machines")
		}
	})

	t.Run("hostname is code hosting", func(t *testing.T) {
		h := &sshHost{
			names:    []string{"mygh"},
			hostname: "github.com",
		}
		if machines := h.toMachines(); len(machines) != 0 {
			t.Error("toMachines() with github hostname should return no machines")
		}
	})

	t.Run("no hostname uses name", func(t *testing.T) {
		h := &sshHost{
			names: []string{"my-server"},
			port:  "2222",
			user:  "admin",
		}
		machines := h.toMachines()
		if len(machines) != 1 {
			t.Fatalf("toMachines() len = %d, want 1", len(machines))
		}
		m := machines[0]
		if m.Address != "my-server" {
			t.Errorf("Address = %q, want %q", m.Address, "my-server")
		}
		if m.Port != 2222 {
			t.Errorf("Port = %d, want 2222", m.Port)
		}
		if m.SSHUser != "admin" {
			t.Errorf("SSHUser = %q, want %q", m.SSHUser, "admin")
		}
	})
}

// TestMergeDiscoveredMachines tests machine merging.
func TestMergeDiscoveredMachines(t *testing.T) {
	existing := []*Machine{
		NewMachine("m1", "192.168.1.100"),
		NewMachine("m2", "192.168.1.101"),
	}

	discovered := []*Machine{
		NewMachine("m1", "10.0.0.1"),      // Same name, different address - should be skipped
		NewMachine("m3", "192.168.1.102"), // New - should be added
	}

	merged := MergeDiscoveredMachines(existing, discovered)

	if len(merged) != 3 {
		t.Errorf("Merged len = %d, want 3", len(merged))
	}

	// m1 should have original address (existing takes precedence)
	for _, m := range merged {
		if m.Name == "m1" && m.Address != "192.168.1.100" {
			t.Errorf("m1 address = %q, want %q (existing should take precedence)", m.Address, "192.168.1.100")
		}
	}
}

// TestSyncPoolAddMachineNilMap tests AddMachine when Machines map is nil.
func TestSyncPoolAddMachineNilMap(t *testing.T) {
	pool := &SyncPool{
		Machines: nil, // Explicitly nil
	}

	m := NewMachine("test", "192.168.1.100")
	if err := pool.AddMachine(m); err != nil {
		t.Fatalf("AddMachine failed: %v", err)
	}

	if pool.Machines == nil {
		t.Error("AddMachine should initialize Machines map")
	}
	if pool.MachineCount() != 1 {
		t.Errorf("Pool count = %d, want 1", pool.MachineCount())
	}
}

// TestSyncPoolAddMachineDuplicateID tests adding a machine with duplicate ID.
func TestSyncPoolAddMachineDuplicateID(t *testing.T) {
	pool := NewSyncPool()
	m1 := NewMachine("test1", "192.168.1.100")

	if err := pool.AddMachine(m1); err != nil {
		t.Fatalf("AddMachine failed: %v", err)
	}

	// Create another machine with the same ID
	m2 := &Machine{
		ID:      m1.ID, // Same ID
		Name:    "different-name",
		Address: "192.168.1.200",
		Port:    22,
	}

	if err := pool.AddMachine(m2); err == nil {
		t.Error("AddMachine should fail for duplicate ID")
	}
}

// TestSyncPoolAddMachineInvalid tests adding an invalid machine.
func TestSyncPoolAddMachineInvalid(t *testing.T) {
	pool := NewSyncPool()

	// Machine with empty name
	m := &Machine{
		ID:      "some-id",
		Name:    "",
		Address: "192.168.1.100",
	}

	if err := pool.AddMachine(m); err == nil {
		t.Error("AddMachine should fail for invalid machine")
	}
}

// TestSyncPoolGetMachineNotFound tests GetMachine with non-existent ID.
func TestSyncPoolGetMachineNotFound(t *testing.T) {
	pool := NewSyncPool()

	if m := pool.GetMachine("nonexistent"); m != nil {
		t.Error("GetMachine should return nil for non-existent ID")
	}
}

// TestSyncPoolGetMachineByNameNotFound tests GetMachineByName with non-existent name.
func TestSyncPoolGetMachineByNameNotFound(t *testing.T) {
	pool := NewSyncPool()
	pool.AddMachine(NewMachine("test", "192.168.1.100"))

	if m := pool.GetMachineByName("nonexistent"); m != nil {
		t.Error("GetMachineByName should return nil for non-existent name")
	}
}

// TestSyncPoolGetMachineByNameCaseInsensitive tests case-insensitive name lookup.
func TestSyncPoolGetMachineByNameCaseInsensitive(t *testing.T) {
	pool := NewSyncPool()
	m := NewMachine("MyServer", "192.168.1.100")
	pool.AddMachine(m)

	tests := []string{"myserver", "MYSERVER", "MyServer", "mYsErVeR"}
	for _, name := range tests {
		if got := pool.GetMachineByName(name); got != m {
			t.Errorf("GetMachineByName(%q) failed to find machine", name)
		}
	}
}

// TestSyncPoolSaveWithBasePath tests Save with custom base path.
func TestSyncPoolSaveWithBasePath(t *testing.T) {
	tmpDir := t.TempDir()
	pool := NewSyncPool()
	pool.SetBasePath(tmpDir)
	pool.Enable()

	m := NewMachine("test", "192.168.1.100")
	pool.AddMachine(m)

	if err := pool.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file was created
	poolFile := filepath.Join(tmpDir, "pool.json")
	if _, err := os.Stat(poolFile); os.IsNotExist(err) {
		t.Error("Save should create pool.json file")
	}
}

// TestSyncPoolLoadWithBasePath tests Load with custom base path.
func TestSyncPoolLoadWithBasePath(t *testing.T) {
	tmpDir := t.TempDir()

	// First save a pool
	pool := NewSyncPool()
	pool.SetBasePath(tmpDir)
	pool.Enable()
	pool.AddMachine(NewMachine("test", "192.168.1.100"))
	pool.Save()

	// Now load it
	loaded := NewSyncPool()
	loaded.SetBasePath(tmpDir)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !loaded.Enabled {
		t.Error("Loaded pool should be enabled")
	}
	if loaded.MachineCount() != 1 {
		t.Errorf("Loaded pool count = %d, want 1", loaded.MachineCount())
	}
}

// TestSyncPoolLoadInvalidJSON tests Load with invalid JSON file.
func TestSyncPoolLoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Write invalid JSON
	poolFile := filepath.Join(tmpDir, "pool.json")
	os.WriteFile(poolFile, []byte("{invalid json"), 0600)

	pool := NewSyncPool()
	pool.SetBasePath(tmpDir)

	if err := pool.Load(); err == nil {
		t.Error("Load should fail with invalid JSON")
	}
}

// TestSyncPoolLoadSyncPoolError tests LoadSyncPool with error.
func TestSyncPoolLoadSyncPoolError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Write invalid JSON to pool file
	syncDir := filepath.Join(tmpDir, "caam", "sync")
	os.MkdirAll(syncDir, 0700)
	os.WriteFile(filepath.Join(syncDir, "pool.json"), []byte("{invalid"), 0600)

	_, err := LoadSyncPool()
	if err == nil {
		t.Error("LoadSyncPool should fail with invalid JSON")
	}
}

// TestSyncStateLoadInvalidIdentity tests Load with invalid identity file.
func TestSyncStateLoadInvalidIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Create invalid identity file
	syncDir := filepath.Join(tmpDir, "caam", "sync")
	os.MkdirAll(syncDir, 0700)
	os.WriteFile(filepath.Join(syncDir, "identity.json"), []byte("{invalid json"), 0600)

	state := NewSyncState(syncDir)
	if err := state.Load(); err == nil {
		t.Error("Load should fail with invalid identity file")
	}
}

// TestSyncStateSaveNilPool tests Save with nil pool.
func TestSyncStateSaveNilPool(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.Pool = nil

	// Should not panic
	if err := state.Save(); err != nil {
		t.Fatalf("Save with nil pool failed: %v", err)
	}
}

// TestSyncStateSaveQueueTrimming tests that Save trims queue to max size.
func TestSyncStateSaveQueueTrimming(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)

	// Set a small max size for testing
	state.Queue.MaxSize = 5

	// Add more entries than max
	for i := 0; i < 10; i++ {
		state.Queue.Entries = append(state.Queue.Entries, QueueEntry{
			Provider: "claude",
			Profile:  "test",
			Machine:  "m" + string(rune('0'+i)),
			AddedAt:  time.Now(),
		})
	}

	if err := state.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Queue should be trimmed
	if len(state.Queue.Entries) != 5 {
		t.Errorf("Queue len after save = %d, want 5", len(state.Queue.Entries))
	}
}

// TestSyncStateSaveHistoryTrimming tests that Save trims history to max size.
func TestSyncStateSaveHistoryTrimming(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)

	// Set a small max size for testing
	state.History.MaxSize = 5

	// Add more entries than max
	for i := 0; i < 10; i++ {
		state.History.Entries = append(state.History.Entries, HistoryEntry{
			Timestamp: time.Now(),
			Provider:  "claude",
			Profile:   "test",
		})
	}

	if err := state.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// History should be trimmed
	if len(state.History.Entries) != 5 {
		t.Errorf("History len after save = %d, want 5", len(state.History.Entries))
	}
}

// TestSyncStateAddToQueueNilQueue tests AddToQueue with nil queue.
func TestSyncStateAddToQueueNilQueue(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.Queue = nil

	state.AddToQueue("claude", "test", "m1", "error")

	if state.Queue == nil {
		t.Error("AddToQueue should create queue when nil")
	}
	if len(state.Queue.Entries) != 1 {
		t.Errorf("Queue len = %d, want 1", len(state.Queue.Entries))
	}
}

// TestSyncStateRemoveFromQueueNilQueue tests RemoveFromQueue with nil queue.
func TestSyncStateRemoveFromQueueNilQueue(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.Queue = nil

	// Should not panic
	state.RemoveFromQueue("claude", "test", "m1")
}

// TestSyncStateRemoveFromQueueNotFound tests RemoveFromQueue with non-existent entry.
func TestSyncStateRemoveFromQueueNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.AddToQueue("claude", "test", "m1", "error")

	initialLen := len(state.Queue.Entries)

	// Remove non-existent entry
	state.RemoveFromQueue("codex", "other", "m2")

	if len(state.Queue.Entries) != initialLen {
		t.Errorf("Queue len changed after removing non-existent entry")
	}
}

// TestSyncStateAddToHistoryNilHistory tests AddToHistory with nil history.
func TestSyncStateAddToHistoryNilHistory(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.History = nil

	state.AddToHistory(HistoryEntry{
		Timestamp: time.Now(),
		Provider:  "claude",
	})

	if state.History == nil {
		t.Error("AddToHistory should create history when nil")
	}
	if len(state.History.Entries) != 1 {
		t.Errorf("History len = %d, want 1", len(state.History.Entries))
	}
}

// TestSyncStateRecentHistoryNilHistory tests RecentHistory with nil history.
func TestSyncStateRecentHistoryNilHistory(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	state.History = nil

	recent := state.RecentHistory(10)
	if recent != nil {
		t.Errorf("RecentHistory should return nil when history is nil")
	}
}

// TestSyncStateRecentHistoryEmpty tests RecentHistory with empty history.
func TestSyncStateRecentHistoryEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewSyncState(tmpDir)
	// History exists but is empty

	recent := state.RecentHistory(10)
	if recent != nil {
		t.Errorf("RecentHistory should return nil when history is empty")
	}
}

// TestSyncStateLoadQueueMaxSizeDefault tests loadQueue sets default max size.
func TestSyncStateLoadQueueMaxSizeDefault(t *testing.T) {
	tmpDir := t.TempDir()

	// Write queue file with MaxSize = 0
	queueData := `{"entries": [], "max_size": 0}`
	os.WriteFile(filepath.Join(tmpDir, "queue.json"), []byte(queueData), 0600)

	state := NewSyncState(tmpDir)
	state.loadQueue()

	if state.Queue.MaxSize != DefaultQueueMaxSize {
		t.Errorf("Queue.MaxSize = %d, want %d", state.Queue.MaxSize, DefaultQueueMaxSize)
	}
}

// TestSyncStateLoadHistoryMaxSizeDefault tests loadHistory sets default max size.
func TestSyncStateLoadHistoryMaxSizeDefault(t *testing.T) {
	tmpDir := t.TempDir()

	// Write history file with MaxSize = 0
	historyData := `{"entries": [], "max_size": 0}`
	os.WriteFile(filepath.Join(tmpDir, "history.json"), []byte(historyData), 0600)

	state := NewSyncState(tmpDir)
	state.loadHistory()

	if state.History.MaxSize != DefaultHistoryMaxSize {
		t.Errorf("History.MaxSize = %d, want %d", state.History.MaxSize, DefaultHistoryMaxSize)
	}
}

// TestConnectionPoolRelease tests releasing a connection.
func TestConnectionPoolRelease(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{})

	// Add a mock client directly for testing
	m := NewMachine("test", "192.168.1.100")
	pool.mu.Lock()
	pool.clients[m.ID] = &SSHClient{machine: m} // No actual connection
	pool.mu.Unlock()

	if pool.Size() != 1 {
		t.Fatalf("Pool size = %d, want 1", pool.Size())
	}

	// Release should remove it
	pool.Release(m.ID)

	if pool.Size() != 0 {
		t.Errorf("Pool size after release = %d, want 0", pool.Size())
	}
}

// TestConnectionPoolReleaseNonExistent tests releasing non-existent connection.
func TestConnectionPoolReleaseNonExistent(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{})

	// Should not panic
	pool.Release("nonexistent")

	if pool.Size() != 0 {
		t.Errorf("Pool size = %d, want 0", pool.Size())
	}
}

// TestConnectionPoolCloseAll tests closing all connections.
func TestConnectionPoolCloseAll(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{})

	// Add mock clients directly
	for i := 0; i < 3; i++ {
		m := NewMachine("test"+string(rune('0'+i)), "192.168.1."+string(rune('0'+i)))
		pool.mu.Lock()
		pool.clients[m.ID] = &SSHClient{machine: m}
		pool.mu.Unlock()
	}

	if pool.Size() != 3 {
		t.Fatalf("Pool size = %d, want 3", pool.Size())
	}

	pool.CloseAll()

	if pool.Size() != 0 {
		t.Errorf("Pool size after CloseAll = %d, want 0", pool.Size())
	}
}

// TestConnectionPoolIsConnected tests checking connection status.
func TestConnectionPoolIsConnected(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{})

	// Non-existent should return false
	if pool.IsConnected("nonexistent") {
		t.Error("IsConnected should return false for non-existent")
	}

	// Add mock client (not actually connected)
	m := NewMachine("test", "192.168.1.100")
	pool.mu.Lock()
	pool.clients[m.ID] = &SSHClient{machine: m} // No actual connection
	pool.mu.Unlock()

	// Client exists but not connected (no actual SSH connection)
	if pool.IsConnected(m.ID) {
		t.Error("IsConnected should return false for unconnected client")
	}
}

// TestConnectionPoolSize tests Size method.
func TestConnectionPoolSize(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{})

	if pool.Size() != 0 {
		t.Errorf("Initial size = %d, want 0", pool.Size())
	}

	// Add some entries
	for i := 0; i < 5; i++ {
		m := NewMachine("test"+string(rune('0'+i)), "192.168.1.100")
		pool.mu.Lock()
		pool.clients[m.ID] = &SSHClient{machine: m}
		pool.mu.Unlock()
	}

	if pool.Size() != 5 {
		t.Errorf("Size = %d, want 5", pool.Size())
	}
}

// TestConnectionPoolConnectedMachines tests ConnectedMachines method.
func TestConnectionPoolConnectedMachines(t *testing.T) {
	pool := NewConnectionPool(ConnectOptions{})

	// Empty pool
	ids := pool.ConnectedMachines()
	if len(ids) != 0 {
		t.Errorf("ConnectedMachines len = %d, want 0", len(ids))
	}

	// Add mock clients (not actually connected)
	for i := 0; i < 3; i++ {
		m := NewMachine("test"+string(rune('0'+i)), "192.168.1."+string(rune('0'+i)))
		pool.mu.Lock()
		pool.clients[m.ID] = &SSHClient{machine: m}
		pool.mu.Unlock()
	}

	// None should be "connected" since they have no actual SSH connection
	ids = pool.ConnectedMachines()
	if len(ids) != 0 {
		t.Errorf("ConnectedMachines len = %d, want 0 (no real connections)", len(ids))
	}
}

// TestCSVPathFallback tests CSVPath when home dir is not available.
func TestCSVPathFallback(t *testing.T) {
	// CSVPath should always return a valid path
	path := CSVPath()
	if path == "" {
		t.Error("CSVPath should not return empty string")
	}
	if !strings.HasSuffix(path, CSVFileName) {
		t.Errorf("CSVPath() = %q, want to end with %q", path, CSVFileName)
	}
}

// TestLoadFromCSVNoFile tests LoadFromCSV when file doesn't exist.
func TestLoadFromCSVNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// File doesn't exist
	machines, err := LoadFromCSV()
	if err != nil {
		t.Fatalf("LoadFromCSV() error = %v", err)
	}
	if machines != nil {
		t.Errorf("LoadFromCSV() should return nil when file doesn't exist")
	}
}

// TestLoadFromCSVEmptyFile tests LoadFromCSV with empty file.
func TestLoadFromCSVEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create empty CSV
	csvDir := filepath.Join(tmpDir, ".caam")
	os.MkdirAll(csvDir, 0700)
	os.WriteFile(filepath.Join(csvDir, CSVFileName), []byte(""), 0600)

	machines, err := LoadFromCSV()
	if err != nil {
		t.Fatalf("LoadFromCSV() error = %v", err)
	}
	if len(machines) != 0 {
		t.Errorf("LoadFromCSV() len = %d, want 0", len(machines))
	}
}

// TestLoadFromCSVInvalidLines tests LoadFromCSV with malformed lines.
func TestLoadFromCSVInvalidLines(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	csvContent := `# Comment
machine_name,address,ssh_key_path
only-one-field
,missing-name,path
valid-machine,192.168.1.100,~/.ssh/id_rsa
`
	csvDir := filepath.Join(tmpDir, ".caam")
	os.MkdirAll(csvDir, 0700)
	os.WriteFile(filepath.Join(csvDir, CSVFileName), []byte(csvContent), 0600)

	machines, err := LoadFromCSV()
	if err != nil {
		t.Fatalf("LoadFromCSV() error = %v", err)
	}

	// Should only have the valid machine
	if len(machines) != 1 {
		t.Errorf("LoadFromCSV() len = %d, want 1", len(machines))
	}
	if len(machines) > 0 && machines[0].Name != "valid-machine" {
		t.Errorf("Machine name = %q, want %q", machines[0].Name, "valid-machine")
	}
}

// TestMachineValidationEmptyAddress tests validation with empty address.
func TestMachineValidationEmptyAddress(t *testing.T) {
	m := &Machine{
		ID:      "test-id",
		Name:    "test",
		Address: "",
		Port:    22,
	}

	if err := m.Validate(); err == nil {
		t.Error("Validate should fail for empty address")
	}
}

// TestMachineValidationEmptyName tests validation with empty name.
func TestMachineValidationEmptyName(t *testing.T) {
	m := &Machine{
		ID:      "test-id",
		Name:    "",
		Address: "192.168.1.100",
		Port:    22,
	}

	err := m.Validate()
	if err == nil {
		t.Error("Validate should fail for empty name")
	}

	// Check it's the right error
	if ve, ok := err.(*ValidationError); ok {
		if ve.Field != "name" {
			t.Errorf("Error field = %q, want 'name'", ve.Field)
		}
	}
}

// TestMachineHostPortNonStandard tests HostPort with non-standard port.
func TestMachineHostPortNonStandard(t *testing.T) {
	m := NewMachine("test", "192.168.1.100")
	m.Port = 2222

	expected := "192.168.1.100:2222"
	if got := m.HostPort(); got != expected {
		t.Errorf("HostPort() = %q, want %q", got, expected)
	}
}

// TestMachinesEqualDifferentUsers tests MachinesEqual with different users.
func TestMachinesEqualDifferentUsers(t *testing.T) {
	m1 := NewMachine("m1", "192.168.1.100")
	m1.SSHUser = "user1"

	m2 := NewMachine("m2", "192.168.1.100")
	m2.SSHUser = "user2"

	// Same address and port, different users - should still be equal
	// (MachinesEqual only compares address and port)
	if !MachinesEqual(m1, m2) {
		t.Error("MachinesEqual should only compare address and port")
	}
}

// TestNormalizeAddressIPv6 tests NormalizeAddress with IPv6.
func TestNormalizeAddressIPv6(t *testing.T) {
	// IPv6 addresses should be handled correctly
	got := NormalizeAddress("::1", 22, "")
	if got != "::1" { // Default port omitted
		t.Errorf("NormalizeAddress(::1, 22, '') = %q", got)
	}

	got = NormalizeAddress("::1", 2222, "admin")
	if got != "admin@::1:2222" {
		t.Errorf("NormalizeAddress(::1, 2222, admin) = %q, want admin@::1:2222", got)
	}
}

// TestParseAddressInvalidPort tests ParseAddress with invalid port.
func TestParseAddressInvalidPort(t *testing.T) {
	host, port, user := ParseAddress("example.com:abc")
	if port != 0 {
		t.Errorf("ParseAddress with invalid port: port = %d, want 0", port)
	}
	if host != "example.com" {
		t.Errorf("ParseAddress: host = %q, want 'example.com'", host)
	}
	if user != "" {
		t.Errorf("ParseAddress: user = %q, want ''", user)
	}
}

// TestThrottler tests the SyncThrottler functionality.
func TestThrottler(t *testing.T) {
	throttler := NewThrottler(50 * time.Millisecond)

	// First call should allow sync
	if !throttler.ShouldSync("claude", "test") {
		t.Error("First call should allow sync")
	}

	// Record sync
	throttler.RecordSync("claude", "test")

	// Immediate second call should not allow sync
	if throttler.ShouldSync("claude", "test") {
		t.Error("Immediate second call should not allow sync")
	}

	// Different profile should allow sync
	if !throttler.ShouldSync("codex", "test") {
		t.Error("Different provider should allow sync")
	}

	// Wait and try again
	time.Sleep(60 * time.Millisecond)
	if !throttler.ShouldSync("claude", "test") {
		t.Error("After waiting, should allow sync again")
	}
}

// TestThrottlerLastSyncTime tests LastSyncTime method.
func TestThrottlerLastSyncTime(t *testing.T) {
	throttler := NewThrottler(time.Minute)

	// No previous sync
	lastSync := throttler.LastSyncTime("claude", "test")
	if !lastSync.IsZero() {
		t.Error("LastSyncTime should return zero for never-synced profile")
	}

	// Record a sync
	before := time.Now()
	throttler.RecordSync("claude", "test")
	after := time.Now()

	lastSync = throttler.LastSyncTime("claude", "test")
	if lastSync.Before(before) || lastSync.After(after) {
		t.Errorf("LastSyncTime = %v, should be between %v and %v", lastSync, before, after)
	}
}

// TestThrottlerReset tests Reset method.
func TestThrottlerReset(t *testing.T) {
	throttler := NewThrottler(time.Hour)

	// Record some syncs
	throttler.RecordSync("claude", "test1")
	throttler.RecordSync("codex", "test2")

	if throttler.ShouldSync("claude", "test1") {
		t.Error("Should not allow sync immediately after recording")
	}

	// Reset
	throttler.Reset()

	// Should allow syncs again
	if !throttler.ShouldSync("claude", "test1") {
		t.Error("After Reset, should allow sync")
	}
	if !throttler.ShouldSync("codex", "test2") {
		t.Error("After Reset, should allow sync for all profiles")
	}
}

// TestThrottlerDefaultInterval tests NewThrottler with invalid interval.
func TestThrottlerDefaultInterval(t *testing.T) {
	// Zero interval should use default
	throttler := NewThrottler(0)
	if throttler.minInterval != DefaultThrottleInterval {
		t.Errorf("Zero interval should use default, got %v", throttler.minInterval)
	}

	// Negative interval should use default
	throttler = NewThrottler(-1 * time.Second)
	if throttler.minInterval != DefaultThrottleInterval {
		t.Errorf("Negative interval should use default, got %v", throttler.minInterval)
	}
}

// TestGlobalThrottler tests global throttler functions.
func TestGlobalThrottler(t *testing.T) {
	// Save current interval
	original := GetThrottleInterval()
	defer SetThrottleInterval(original)

	// Test SetThrottleInterval
	SetThrottleInterval(2 * time.Minute)
	if GetThrottleInterval() != 2*time.Minute {
		t.Errorf("GetThrottleInterval() = %v, want 2m", GetThrottleInterval())
	}

	// Test SetThrottleInterval with invalid value (should not change)
	current := GetThrottleInterval()
	SetThrottleInterval(0)
	if GetThrottleInterval() != current {
		t.Errorf("SetThrottleInterval(0) should not change interval")
	}

	// Test ResetThrottler
	ResetThrottler() // Should not panic
}

// TestGetSyncStatus tests GetSyncStatus function.
func TestGetSyncStatus(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Create state with various settings
	syncDir := filepath.Join(tmpDir, "caam", "sync")
	state := NewSyncState(syncDir)
	state.Load()
	state.Pool.Enable()
	state.Pool.EnableAutoSync()
	state.Pool.AddMachine(NewMachine("m1", "192.168.1.100"))
	state.Pool.AddMachine(NewMachine("m2", "192.168.1.101"))
	state.Pool.RecordFullSync()
	state.AddToQueue("claude", "test", "m1", "error")
	state.Save()

	status, err := GetSyncStatus()
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}

	if !status.Enabled {
		t.Error("status.Enabled should be true")
	}
	if !status.AutoSync {
		t.Error("status.AutoSync should be true")
	}
	if status.MachineCount != 2 {
		t.Errorf("status.MachineCount = %d, want 2", status.MachineCount)
	}
	if status.QueueCount != 1 {
		t.Errorf("status.QueueCount = %d, want 1", status.QueueCount)
	}
	if status.LastFullSync.IsZero() {
		t.Error("status.LastFullSync should not be zero")
	}
}

// TestGetSyncStatusNilPool tests GetSyncStatus with nil pool.
func TestGetSyncStatusNilPool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", "")
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	// Create state with empty pool
	syncDir := filepath.Join(tmpDir, "caam", "sync")
	state := NewSyncState(syncDir)
	state.Load()
	state.Save()

	status, err := GetSyncStatus()
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}

	// All should be defaults/zeros
	if status.Enabled {
		t.Error("status.Enabled should be false")
	}
	if status.MachineCount != 0 {
		t.Errorf("status.MachineCount = %d, want 0", status.MachineCount)
	}
}

// TestDefaultAutoSyncConfig tests DefaultAutoSyncConfig function.
func TestDefaultAutoSyncConfig(t *testing.T) {
	config := DefaultAutoSyncConfig()

	if config.ThrottleInterval != DefaultThrottleInterval {
		t.Errorf("ThrottleInterval = %v, want %v", config.ThrottleInterval, DefaultThrottleInterval)
	}
	if config.SyncTimeout != DefaultSyncTimeout {
		t.Errorf("SyncTimeout = %v, want %v", config.SyncTimeout, DefaultSyncTimeout)
	}
	if config.Verbose {
		t.Error("Verbose should default to false")
	}
}
