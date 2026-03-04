package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOrchestratorLogger tests that NewOrchestrator properly initializes the logger.
func TestOrchestratorLogger(t *testing.T) {
	// With nil logger, should use default
	orch := NewOrchestrator(Options{Logger: nil})
	if orch.logger == nil {
		t.Error("expected default logger to be set")
	}

	// Logger should be stored
	opts := DefaultOptions()
	orch2 := NewOrchestrator(opts)
	if orch2.logger != opts.Logger {
		t.Error("expected provided logger to be used")
	}
}

// TestOrchestratorOptionsImmutability tests that options are copied.
func TestOrchestratorOptionsImmutability(t *testing.T) {
	opts := DefaultOptions()
	orch := NewOrchestrator(opts)

	// Modify original
	opts.LocalPort = 9999

	// Orchestrator should retain original value
	if orch.opts.LocalPort == 9999 {
		t.Error("options should be copied, not referenced")
	}
}

// TestToSyncMachine tests the conversion to sync.Machine.
func TestToSyncMachineIntegration(t *testing.T) {
	tests := []struct {
		name        string
		opts        Options
		machine     *DiscoveredMachine
		wantAddr    string
		wantPort    int
		wantUser    string
		wantKeyPath string
	}{
		{
			name: "public IP when Tailscale disabled",
			opts: Options{UseTailscale: false},
			machine: &DiscoveredMachine{
				Name:         "server1",
				PublicIP:     "1.2.3.4",
				TailscaleIP:  "100.100.100.100",
				Port:         2222,
				Username:     "ubuntu",
				IdentityFile: "/home/user/.ssh/id_ed25519",
			},
			wantAddr:    "1.2.3.4",
			wantPort:    2222,
			wantUser:    "ubuntu",
			wantKeyPath: "/home/user/.ssh/id_ed25519",
		},
		{
			name: "Tailscale IP when enabled and available",
			opts: Options{UseTailscale: true},
			machine: &DiscoveredMachine{
				Name:         "server1",
				PublicIP:     "1.2.3.4",
				TailscaleIP:  "100.100.100.100",
				Port:         22,
				Username:     "root",
				IdentityFile: "/keys/id_rsa",
			},
			wantAddr:    "100.100.100.100",
			wantPort:    22,
			wantUser:    "root",
			wantKeyPath: "/keys/id_rsa",
		},
		{
			name: "public IP when Tailscale enabled but no Tailscale IP",
			opts: Options{UseTailscale: true},
			machine: &DiscoveredMachine{
				Name:        "server1",
				PublicIP:    "5.6.7.8",
				TailscaleIP: "", // No Tailscale IP
				Port:        22,
				Username:    "user",
			},
			wantAddr: "5.6.7.8",
			wantPort: 22,
			wantUser: "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := NewOrchestrator(tt.opts)
			result := orch.toSyncMachine(tt.machine)

			if result.Address != tt.wantAddr {
				t.Errorf("Address = %q, want %q", result.Address, tt.wantAddr)
			}
			if result.Port != tt.wantPort {
				t.Errorf("Port = %d, want %d", result.Port, tt.wantPort)
			}
			if result.SSHUser != tt.wantUser {
				t.Errorf("SSHUser = %q, want %q", result.SSHUser, tt.wantUser)
			}
			if result.SSHKeyPath != tt.wantKeyPath {
				t.Errorf("SSHKeyPath = %q, want %q", result.SSHKeyPath, tt.wantKeyPath)
			}
		})
	}
}

// TestBuildSSHCommand tests SSH command generation.
func TestBuildSSHCommandIntegration(t *testing.T) {
	tests := []struct {
		name     string
		machine  *DiscoveredMachine
		opts     ScriptOptions
		expected string
	}{
		{
			name: "basic command with defaults",
			machine: &DiscoveredMachine{
				PublicIP: "1.2.3.4",
				Username: "ubuntu",
				Port:     22,
			},
			opts:     ScriptOptions{UseTailscale: false},
			expected: "ssh ubuntu@1.2.3.4",
		},
		{
			name: "with custom port",
			machine: &DiscoveredMachine{
				PublicIP: "1.2.3.4",
				Username: "ubuntu",
				Port:     2222,
			},
			opts:     ScriptOptions{UseTailscale: false},
			expected: "ssh -p 2222 ubuntu@1.2.3.4",
		},
		{
			name: "with identity file",
			machine: &DiscoveredMachine{
				PublicIP:     "1.2.3.4",
				Username:     "ubuntu",
				Port:         22,
				IdentityFile: "/home/user/.ssh/id_ed25519",
			},
			opts:     ScriptOptions{UseTailscale: false},
			expected: "ssh -i /home/user/.ssh/id_ed25519 ubuntu@1.2.3.4",
		},
		{
			name: "with identity file containing space",
			machine: &DiscoveredMachine{
				PublicIP:     "1.2.3.4",
				Username:     "ubuntu",
				Port:         22,
				IdentityFile: "/home/user/my keys/id_ed25519",
			},
			opts:     ScriptOptions{UseTailscale: false},
			expected: "ssh -i '/home/user/my keys/id_ed25519' ubuntu@1.2.3.4",
		},
		{
			name: "with Tailscale IP preference",
			machine: &DiscoveredMachine{
				PublicIP:     "1.2.3.4",
				TailscaleIP:  "100.100.100.100",
				Username:     "ubuntu",
				Port:         2222,
				IdentityFile: "/keys/id_rsa",
			},
			opts:     ScriptOptions{UseTailscale: true},
			expected: "ssh -i /keys/id_rsa -p 2222 ubuntu@100.100.100.100",
		},
		{
			name: "public IP when Tailscale disabled even if available",
			machine: &DiscoveredMachine{
				PublicIP:    "1.2.3.4",
				TailscaleIP: "100.100.100.100",
				Username:    "ubuntu",
				Port:        22,
			},
			opts:     ScriptOptions{UseTailscale: false},
			expected: "ssh ubuntu@1.2.3.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildSSHCommand(tt.machine, tt.opts)
			if result != tt.expected {
				t.Errorf("buildSSHCommand() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestScriptOptions tests ScriptOptions defaults and behavior.
func TestScriptOptions(t *testing.T) {
	opts := ScriptOptions{
		UseTailscale: true,
		LocalPort:    7891,
		RemotePort:   7890,
		Remotes:      []string{"csd", "css"},
	}

	if !opts.UseTailscale {
		t.Error("expected UseTailscale=true")
	}
	if opts.LocalPort != 7891 {
		t.Errorf("LocalPort = %d, want 7891", opts.LocalPort)
	}
}

// TestBuildSetupScriptErrors tests error conditions in BuildSetupScript.
func TestBuildSetupScriptErrorsIntegration(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())

	// No local machine
	_, err := orch.BuildSetupScript(ScriptOptions{})
	if err == nil {
		t.Error("expected error when no local machine set")
	}

	// Local but no remotes
	orch.localMachine = &DiscoveredMachine{Name: "local"}
	_, err = orch.BuildSetupScript(ScriptOptions{})
	if err == nil {
		t.Error("expected error when no remote machines")
	}
}

// TestGenerateLocalConfig tests the local config generation.
func TestGenerateLocalConfigIntegration(t *testing.T) {
	// Create temp directory for config
	tmpDir, err := os.MkdirTemp("", "caam-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Set HOME to temp directory
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Set XDG_CONFIG_HOME to ensure consistent behavior
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{
			Name:          "server1",
			WezTermDomain: "srv1",
			PublicIP:      "1.2.3.4",
			TailscaleIP:   "100.100.100.100",
			Port:          22,
			Username:      "ubuntu",
		},
		{
			Name:          "server2",
			WezTermDomain: "srv2",
			PublicIP:      "5.6.7.8",
			Port:          22,
			Username:      "ubuntu",
		},
	}
	orch.opts.RemotePort = 7890
	orch.opts.LocalPort = 7891
	orch.opts.UseTailscale = true

	configPath, err := orch.generateLocalConfig()
	if err != nil {
		t.Fatalf("generateLocalConfig error: %v", err)
	}

	// Verify path
	if !strings.Contains(configPath, "caam") {
		t.Errorf("config path should contain 'caam': %s", configPath)
	}

	// Read and verify config
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Verify port
	if port, _ := config["port"].(float64); port != 7891 {
		t.Errorf("port = %v, want 7891", port)
	}

	// Verify coordinators
	coordinators, ok := config["coordinators"].([]interface{})
	if !ok {
		t.Fatal("coordinators should be an array")
	}
	if len(coordinators) != 2 {
		t.Errorf("expected 2 coordinators, got %d", len(coordinators))
	}

	// First coordinator should use Tailscale IP
	coord1 := coordinators[0].(map[string]interface{})
	if coord1["name"] != "srv1" {
		t.Errorf("first coordinator name = %v, want srv1", coord1["name"])
	}
	// URL should contain Tailscale IP
	url, _ := coord1["url"].(string)
	if !strings.Contains(url, "100.100.100.100") {
		t.Errorf("URL should contain Tailscale IP: %s", url)
	}
}

// TestGenerateLocalConfigDryRun tests dry-run mode.
func TestGenerateLocalConfigDryRunIntegration(t *testing.T) {
	orch := NewOrchestrator(Options{
		DryRun:    true,
		Logger:    nil,
		UseTailscale: true,
	})
	orch.localMachine = &DiscoveredMachine{Name: "local"}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "server1", WezTermDomain: "srv1", PublicIP: "1.2.3.4"},
	}

	configPath, err := orch.generateLocalConfig()
	if err != nil {
		t.Fatalf("generateLocalConfig error: %v", err)
	}

	// In dry-run mode, file should not actually exist
	// The path should still be returned for reference
	if !strings.Contains(configPath, "distributed-agent.json") {
		t.Errorf("config path should reference distributed-agent.json: %s", configPath)
	}
}

// TestSetupResultErrorAccumulation tests error handling in Setup.
func TestSetupResultErrorAccumulation(t *testing.T) {
	orch := NewOrchestrator(Options{
		DryRun: true,
	})
	orch.localMachine = &DiscoveredMachine{Name: "local"}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "server1", WezTermDomain: "srv1", PublicIP: "1.2.3.4"},
		{Name: "server2", WezTermDomain: "srv2", PublicIP: "5.6.7.8"},
	}

	// In dry-run mode, should succeed without errors
	result, err := orch.Setup(context.Background(), func(p *SetupProgress) {
		// Progress callback
	})

	if err != nil {
		t.Errorf("Setup in dry-run mode should not return error: %v", err)
	}

	// Dry-run should still have results
	if result == nil {
		t.Fatal("Setup should return result")
	}
}

// TestDiscoveredMachineRole tests role assignment.
func TestDiscoveredMachineRole(t *testing.T) {
	// Coordinator role
	coord := &DiscoveredMachine{
		Name: "coordinator-host",
		Role: RoleCoordinator,
	}
	if coord.Role != RoleCoordinator {
		t.Errorf("expected RoleCoordinator, got %s", coord.Role)
	}

	// Agent role
	agent := &DiscoveredMachine{
		Name: "agent-host",
		Role: RoleAgent,
	}
	if agent.Role != RoleAgent {
		t.Errorf("expected RoleAgent, got %s", agent.Role)
	}

	// Local machine is typically an agent
	local := &DiscoveredMachine{
		Name:    "local-machine",
		Role:    RoleAgent,
		IsLocal: true,
	}
	if !local.IsLocal {
		t.Error("expected IsLocal=true")
	}
	if local.Role != RoleAgent {
		t.Error("local machine should have agent role")
	}
}

// TestOrchestratorSetters tests machine setter methods.
func TestOrchestratorSetters(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())

	// Initially nil
	if orch.GetLocalMachine() != nil {
		t.Error("expected nil local machine initially")
	}
	if orch.GetRemoteMachines() != nil {
		t.Error("expected nil remote machines initially")
	}

	// Set local
	local := &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.localMachine = local
	if orch.GetLocalMachine() != local {
		t.Error("GetLocalMachine should return set machine")
	}

	// Set remotes
	remotes := []*DiscoveredMachine{
		{Name: "remote1"},
		{Name: "remote2"},
	}
	orch.remoteMachines = remotes
	if len(orch.GetRemoteMachines()) != 2 {
		t.Errorf("expected 2 remote machines")
	}
}

// TestPrintDiscoveryResults tests the output function.
func TestPrintDiscoveryResultsIntegration(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{
		Name:         "local-machine",
		TailscaleIP:  "100.100.100.1",
		Role:         RoleAgent,
		IsLocal:      true,
	}
	orch.remoteMachines = []*DiscoveredMachine{
		{
			Name:          "server1",
			WezTermDomain: "srv1",
			PublicIP:      "1.2.3.4",
			TailscaleIP:   "100.100.100.100",
			Username:      "ubuntu",
			Role:          RoleCoordinator,
		},
	}

	// Just verify it doesn't panic
	orch.PrintDiscoveryResults()
}

// TestOptionsDefaults verifies Options default behavior.
func TestOptionsDefaults(t *testing.T) {
	opts := DefaultOptions()

	// Verify all expected defaults
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"UseTailscale", opts.UseTailscale, true},
		{"LocalPort", opts.LocalPort, 7891},
		{"RemotePort", opts.RemotePort, 7890},
		{"DryRun", opts.DryRun, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestContainsEdgeCases tests the contains helper with edge cases.
func TestContainsEdgeCases(t *testing.T) {
	tests := []struct {
		slice    []string
		item     string
		expected bool
	}{
		{nil, "test", false},
		{[]string{}, "test", false},
		{[]string{"a", "b", "c"}, "", false},
		{[]string{"a", "b", ""}, "", true},
		{[]string{"TEST"}, "test", true}, // case insensitive
		{[]string{"a'b"}, "A'B", true},   // special chars, case insensitive
	}

	for _, tt := range tests {
		result := contains(tt.slice, tt.item)
		if result != tt.expected {
			t.Errorf("contains(%v, %q) = %v, want %v", tt.slice, tt.item, result, tt.expected)
		}
	}
}

// TestShellQuoteEdgeCases tests shell quoting edge cases.
func TestShellQuoteEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "''"},
		{"simple", "simple"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"with\"double", "'with\"double'"},
		{"with$var", "'with$var'"},
		{"with`backtick", "'with`backtick'"},
		{"with\nnewline", "'with\nnewline'"},
		{"with\ttab", "'with\ttab'"},
		{"a\\b", "'a\\b'"},
	}

	for _, tt := range tests {
		result := shellQuote(tt.input)
		if result != tt.expected {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestSetupProgressTransitions tests progress state transitions.
func TestSetupProgressTransitions(t *testing.T) {
	progress := &SetupProgress{
		Machine: "test-machine",
		Step:    "deploy",
		Status:  "pending",
	}

	// Transition to running
	progress.Status = "running"
	progress.Started = time.Now()

	if progress.Status != "running" {
		t.Error("status should be running")
	}

	// Transition to success
	progress.Status = "success"
	progress.Message = "deployed successfully"
	progress.Finished = time.Now()

	if progress.Status != "success" {
		t.Error("status should be success")
	}

	// Duration can be calculated
	duration := progress.Finished.Sub(progress.Started)
	if duration < 0 {
		t.Error("duration should be non-negative")
	}
}

// TestSetupResultFieldHandling tests SetupResult field handling.
func TestSetupResultFieldHandling(t *testing.T) {
	result := &SetupResult{
		LocalConfigPath: "/path/to/config.json",
		Errors:          []error{fmt.Errorf("test error")},
	}

	if result.LocalConfigPath != "/path/to/config.json" {
		t.Errorf("unexpected LocalConfigPath")
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error")
	}
	if result.Errors[0].Error() != "test error" {
		t.Errorf("unexpected error message")
	}
}

// TestScriptGenerationWithMultipleRemotes tests script generation with multiple remotes.
func TestScriptGenerationWithMultipleRemotes(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{
			Name:          "csd",
			WezTermDomain: "csd",
			PublicIP:      "1.2.3.4",
			TailscaleIP:   "100.100.118.85",
			Username:      "ubuntu",
			Port:          2222,
			IdentityFile:  "/home/user/.ssh/id_ed25519",
		},
		{
			Name:          "css",
			WezTermDomain: "css",
			PublicIP:      "5.6.7.8",
			TailscaleIP:   "100.90.148.85",
			Username:      "ubuntu",
			Port:          22,
		},
		{
			Name:          "trj",
			WezTermDomain: "trj",
			PublicIP:      "9.10.11.12",
			Username:      "admin",
			Port:          22,
		},
	}

	script, err := orch.BuildSetupScript(ScriptOptions{
		UseTailscale: true,
		RemotePort:   7890,
	})
	if err != nil {
		t.Fatalf("BuildSetupScript error: %v", err)
	}

	// Verify script contains all remotes
	if !strings.Contains(script, "csd") {
		t.Error("script should reference csd")
	}
	if !strings.Contains(script, "css") {
		t.Error("script should reference css")
	}
	if !strings.Contains(script, "trj") {
		t.Error("script should reference trj")
	}

	// Verify curl commands for each remote
	curlCount := strings.Count(script, "curl -fsS http://")
	if curlCount != 3 {
		t.Errorf("expected 3 curl commands, got %d", curlCount)
	}

	// Verify ssh commands for each remote
	sshCount := strings.Count(script, "ssh ")
	if sshCount != 3 {
		t.Errorf("expected 3 ssh commands, got %d", sshCount)
	}
}

// TestRealHTTPStatusEndpoint tests real HTTP behavior with httptest.
func TestRealHTTPStatusEndpoint(t *testing.T) {
	// This tests the actual HTTP behavior using httptest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"running":   true,
				"coordinator": "test",
			}); err != nil {
				t.Logf("encode response failed: %v", err)
			}
		}
	}))
	defer ts.Close()

	// Make real HTTP request
	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["running"] != true {
		t.Error("expected running=true")
	}
}

// TestLocalPortBinding tests actual port binding behavior.
func TestLocalPortBinding(t *testing.T) {
	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Verify we can bind to the same port
	listener2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to bind to port %d: %v", port, err)
	}
	listener2.Close()
}

// TestConfigFileAtomicWrite tests atomic file writing behavior.
func TestConfigFileAtomicWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "caam-atomic-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")

	// Write initial config
	data1 := map[string]string{"version": "1"}
	data1JSON, _ := json.Marshal(data1)

	// Write using atomic pattern (temp file + rename)
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data1JSON, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		t.Fatal(err)
	}

	// Verify content
	readData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(readData) != string(data1JSON) {
		t.Error("file content mismatch")
	}

	// Overwrite with new content
	data2 := map[string]string{"version": "2"}
	data2JSON, _ := json.Marshal(data2)
	tmpPath = configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data2JSON, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		t.Fatal(err)
	}

	// Verify new content
	readData, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(readData) != string(data2JSON) {
		t.Error("file content should be updated")
	}
}

// TestContextCancellation tests context cancellation handling.
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start a goroutine that respects context
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			t.Error("should have been cancelled")
		}
	}()

	// Cancel after a short delay
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("goroutine did not stop on cancellation")
	}
}
