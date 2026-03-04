package setup

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if !opts.UseTailscale {
		t.Error("expected UseTailscale=true by default")
	}
	if opts.LocalPort != 7891 {
		t.Errorf("expected LocalPort=7891, got %d", opts.LocalPort)
	}
	if opts.RemotePort != 7890 {
		t.Errorf("expected RemotePort=7890, got %d", opts.RemotePort)
	}
	if opts.DryRun {
		t.Error("expected DryRun=false by default")
	}
}

func TestContains(t *testing.T) {
	slice := []string{"csd", "css", "trj"}

	tests := []struct {
		item     string
		expected bool
	}{
		{"csd", true},
		{"CSS", true}, // Case insensitive
		{"TRJ", true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		result := contains(slice, tt.item)
		if result != tt.expected {
			t.Errorf("contains(%v, %q) = %v, want %v", slice, tt.item, result, tt.expected)
		}
	}
}

func TestDiscoveredMachineFields(t *testing.T) {
	m := &DiscoveredMachine{
		Name:          "SuperServer",
		WezTermDomain: "css",
		PublicIP:      "1.2.3.4",
		TailscaleIP:   "100.90.148.85",
		Username:      "ubuntu",
		Port:          22,
		IdentityFile:  "/home/user/.ssh/id_ed25519",
		Role:          RoleCoordinator,
		IsReachable:   true,
		IsLocal:       false,
	}

	if m.Name != "SuperServer" {
		t.Errorf("expected name SuperServer, got %s", m.Name)
	}
	if m.Role != RoleCoordinator {
		t.Errorf("expected role coordinator, got %s", m.Role)
	}
	if m.IsLocal {
		t.Error("expected IsLocal=false")
	}
	if !m.IsReachable {
		t.Error("expected IsReachable=true")
	}
}

func TestRoleConstants(t *testing.T) {
	if RoleCoordinator != "coordinator" {
		t.Errorf("expected RoleCoordinator=coordinator, got %s", RoleCoordinator)
	}
	if RoleAgent != "agent" {
		t.Errorf("expected RoleAgent=agent, got %s", RoleAgent)
	}
}

func TestOrchestratorCreation(t *testing.T) {
	opts := DefaultOptions()
	opts.DryRun = true

	orch := NewOrchestrator(opts)

	if orch == nil {
		t.Fatal("NewOrchestrator returned nil")
	}
	if orch.logger == nil {
		t.Error("expected logger to be set")
	}
	if !orch.opts.DryRun {
		t.Error("expected DryRun=true")
	}
}

func TestGetAddress(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())

	// With Tailscale enabled and available
	m := &DiscoveredMachine{
		PublicIP:    "1.2.3.4",
		TailscaleIP: "100.100.100.100",
	}

	addr := orch.getAddress(m)
	if addr != "100.100.100.100" {
		t.Errorf("expected Tailscale IP, got %s", addr)
	}

	// Without Tailscale IP
	m2 := &DiscoveredMachine{
		PublicIP:    "5.6.7.8",
		TailscaleIP: "",
	}

	addr2 := orch.getAddress(m2)
	if addr2 != "5.6.7.8" {
		t.Errorf("expected public IP, got %s", addr2)
	}

	// With Tailscale disabled
	opts := DefaultOptions()
	opts.UseTailscale = false
	orch2 := NewOrchestrator(opts)

	addr3 := orch2.getAddress(m)
	if addr3 != "1.2.3.4" {
		t.Errorf("expected public IP when Tailscale disabled, got %s", addr3)
	}
}

func TestSetupProgressFields(t *testing.T) {
	progress := &SetupProgress{
		Machine: "test-machine",
		Step:    "deploy",
		Status:  "running",
		Message: "uploading binary",
	}

	if progress.Machine != "test-machine" {
		t.Errorf("expected machine test-machine, got %s", progress.Machine)
	}
	if progress.Step != "deploy" {
		t.Errorf("expected step deploy, got %s", progress.Step)
	}
	if progress.Status != "running" {
		t.Errorf("expected status running, got %s", progress.Status)
	}
}

func TestSetupResultFields(t *testing.T) {
	result := &SetupResult{
		LocalConfigPath:   "/home/user/.config/caam/distributed-agent.json",
		CoordinatorConfig: "{}",
	}

	if result.LocalConfigPath == "" {
		t.Error("expected LocalConfigPath to be set")
	}
	if result.DeployResults != nil && len(result.DeployResults) > 0 {
		t.Error("expected empty DeployResults initially")
	}
}

func TestGetDiscoveredMachines(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())

	// Set up local and remote machines
	orch.localMachine = &DiscoveredMachine{
		Name:    "local",
		IsLocal: true,
	}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "remote1", IsLocal: false},
		{Name: "remote2", IsLocal: false},
	}

	all := orch.GetDiscoveredMachines()
	if len(all) != 3 {
		t.Errorf("expected 3 machines, got %d", len(all))
	}

	// First should be local
	if !all[0].IsLocal {
		t.Error("expected first machine to be local")
	}

	remotes := orch.GetRemoteMachines()
	if len(remotes) != 2 {
		t.Errorf("expected 2 remote machines, got %d", len(remotes))
	}

	local := orch.GetLocalMachine()
	if local == nil {
		t.Fatal("expected local machine")
	}
	if local.Name != "local" {
		t.Errorf("expected local machine name 'local', got %s", local.Name)
	}
}

func TestRemotesFilter(t *testing.T) {
	// Test that the contains function works correctly for filtering
	remotes := []string{"csd", "css"}

	// Should match
	if !contains(remotes, "csd") {
		t.Error("expected csd to be in remotes")
	}
	if !contains(remotes, "CSS") { // Case insensitive
		t.Error("expected CSS to be in remotes (case insensitive)")
	}

	// Should not match
	if contains(remotes, "trj") {
		t.Error("expected trj to not be in remotes")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"a b", "'a b'"},
		{"a'b", "'a'\\''b'"},
		{"", "''"},
	}

	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.expected {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBuildSetupScript(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{
		Name:    "local",
		IsLocal: true,
	}
	orch.remoteMachines = []*DiscoveredMachine{
		{
			Name:          "csd",
			WezTermDomain: "csd",
			PublicIP:      "1.2.3.4",
			TailscaleIP:   "100.100.118.85",
			Username:      "ubuntu",
			Port:          2222,
			IdentityFile:  "/tmp/id_ed25519",
		},
	}

	script, err := orch.BuildSetupScript(ScriptOptions{
		UseTailscale: true,
		RemotePort:   7890,
	})
	if err != nil {
		t.Fatalf("BuildSetupScript error: %v", err)
	}
	if !strings.Contains(script, "caam setup distributed --yes") {
		t.Error("expected setup command in script")
	}
	if !strings.Contains(script, "curl -fsS http://100.100.118.85:7890/status") {
		t.Error("expected tailscale status curl in script")
	}
	if !strings.Contains(script, "ssh -i /tmp/id_ed25519 -p 2222 ubuntu@100.100.118.85") {
		t.Error("expected ssh status command in script")
	}
	if !strings.Contains(script, "caam auth-agent --config \"$CONFIG_PATH\"") {
		t.Error("expected auth-agent start in script")
	}
}

func TestBuildSetupScriptWithMultipleRemotes(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{
		Name:    "local",
		IsLocal: true,
	}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "csd", WezTermDomain: "csd", PublicIP: "1.1.1.1", TailscaleIP: "100.100.1.1", Username: "user", Port: 22, IdentityFile: "/tmp/key1"},
		{Name: "css", WezTermDomain: "css", PublicIP: "2.2.2.2", TailscaleIP: "100.100.2.2", Username: "user", Port: 22, IdentityFile: "/tmp/key2"},
		{Name: "trj", WezTermDomain: "trj", PublicIP: "3.3.3.3", TailscaleIP: "100.100.3.3", Username: "user", Port: 22, IdentityFile: "/tmp/key3"},
	}

	script, err := orch.BuildSetupScript(ScriptOptions{
		UseTailscale: true,
		RemotePort:   7890,
	})
	if err != nil {
		t.Fatalf("BuildSetupScript error: %v", err)
	}

	// Should contain status checks for all remotes
	if !strings.Contains(script, "# remote: csd") {
		t.Error("expected remote csd section")
	}
	if !strings.Contains(script, "# remote: css") {
		t.Error("expected remote css section")
	}
	if !strings.Contains(script, "# remote: trj") {
		t.Error("expected remote trj section")
	}
}

func TestBuildSetupScriptNoTailscale(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "csd", WezTermDomain: "csd", PublicIP: "1.2.3.4", Username: "ubuntu", Port: 22},
	}

	script, err := orch.BuildSetupScript(ScriptOptions{
		UseTailscale: false,
		RemotePort:   7890,
	})
	if err != nil {
		t.Fatalf("BuildSetupScript error: %v", err)
	}
	// Should use public IP when tailscale disabled
	if !strings.Contains(script, "curl -fsS http://1.2.3.4:7890/status") {
		t.Error("expected public IP curl when tailscale disabled")
	}
	if strings.Contains(script, "100.") {
		t.Error("should not use tailscale IP when tailscale disabled")
	}
}

func TestBuildSetupScriptNoTailscaleIP(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "csd", WezTermDomain: "csd", PublicIP: "5.6.7.8", TailscaleIP: "", Username: "ubuntu", Port: 22},
	}

	script, err := orch.BuildSetupScript(ScriptOptions{
		UseTailscale: true,
		RemotePort:   7890,
	})
	if err != nil {
		t.Fatalf("BuildSetupScript error: %v", err)
	}
	// Should fall back to public IP when no tailscale IP
	if !strings.Contains(script, "curl -fsS http://5.6.7.8:7890/status") {
		t.Error("expected public IP fallback when no tailscale IP")
	}
}

func TestSetupDryRun(t *testing.T) {
	orch := NewOrchestrator(Options{
		UseTailscale: true,
		LocalPort:    7891,
		RemotePort:   7890,
		DryRun:       true,
	})
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "csd", WezTermDomain: "csd", PublicIP: "1.2.3.4", TailscaleIP: "100.100.1.1", Username: "ubuntu", Port: 22},
		{Name: "css", WezTermDomain: "css", PublicIP: "5.6.7.8", Username: "user", Port: 22},
	}

	var progressCalls []*SetupProgress
	result, err := orch.Setup(context.Background(), func(p *SetupProgress) {
		progressCalls = append(progressCalls, p)
	})

	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// In dry-run mode, no actual deployment happens
	if len(result.DeployResults) != 0 {
		t.Errorf("expected no deploy results in dry-run, got %d", len(result.DeployResults))
	}
	// Should have progress calls for each machine
	if len(progressCalls) < 2 {
		t.Errorf("expected at least 2 progress calls, got %d", len(progressCalls))
	}
}

func TestSetupNoRemotes(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = nil

	_, err := orch.Setup(context.Background(), nil)
	if err == nil {
		t.Error("expected error when no remote machines")
	}
	if !strings.Contains(err.Error(), "no remote machines") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetupProgressTracking(t *testing.T) {
	orch := NewOrchestrator(Options{DryRun: true})
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "machine1", WezTermDomain: "m1", PublicIP: "1.1.1.1", Username: "user", Port: 22},
		{Name: "machine2", WezTermDomain: "m2", PublicIP: "2.2.2.2", Username: "user", Port: 22},
	}

	var statuses []string
	_, err := orch.Setup(context.Background(), func(p *SetupProgress) {
		statuses = append(statuses, p.Status)
	})
	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}

	// Should have running and success for each machine
	if len(statuses) < 4 {
		t.Errorf("expected at least 4 status updates, got %d", len(statuses))
	}
}

func TestDiscoverLocalHostname(t *testing.T) {
	// Test that discoverLocal sets up local machine with hostname
	orch := NewOrchestrator(Options{UseTailscale: false})

	// Without tailscale, discoverLocal should just use os.Hostname()
	err := orch.discoverLocal(context.Background())
	if err != nil {
		t.Fatalf("discoverLocal error: %v", err)
	}
	if orch.localMachine == nil {
		t.Fatal("expected localMachine to be set")
	}
	if orch.localMachine.Name == "" {
		t.Error("expected local machine name to be set")
	}
	if !orch.localMachine.IsLocal {
		t.Error("expected IsLocal=true")
	}
	if orch.localMachine.Role != RoleAgent {
		t.Errorf("expected RoleAgent, got %s", orch.localMachine.Role)
	}
}

func TestOrchestratorWithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	opts := Options{
		DryRun: true,
		Logger: logger,
	}
	orch := NewOrchestrator(opts)
	if orch.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

func TestOrchestratorNilLogger(t *testing.T) {
	opts := Options{
		DryRun: true,
		Logger: nil,
	}
	orch := NewOrchestrator(opts)
	if orch.logger == nil {
		t.Error("expected default logger to be set when nil provided")
	}
}

func TestGenerateLocalConfigReal(t *testing.T) {
	// Create temp config dir
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	orch := NewOrchestrator(Options{
		UseTailscale: true,
		LocalPort:    7891,
		RemotePort:   7890,
		DryRun:       false,
	})
	orch.localMachine = &DiscoveredMachine{Name: "local", IsLocal: true}
	orch.remoteMachines = []*DiscoveredMachine{
		{Name: "csd", WezTermDomain: "csd", PublicIP: "1.2.3.4", TailscaleIP: "100.100.1.1"},
		{Name: "css", WezTermDomain: "css", PublicIP: "5.6.7.8"},
	}

	path, err := orch.generateLocalConfig()
	if err != nil {
		t.Fatalf("generateLocalConfig error: %v", err)
	}
	if !strings.Contains(path, "distributed-agent.json") {
		t.Errorf("unexpected config path: %s", path)
	}

	// Verify file was created
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	// Parse and verify contents
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config JSON: %v", err)
	}
	if config["port"].(float64) != 7891 {
		t.Errorf("expected port 7891, got %v", config["port"])
	}
	coordinators := config["coordinators"].([]interface{})
	if len(coordinators) != 2 {
		t.Errorf("expected 2 coordinators, got %d", len(coordinators))
	}
}

func TestPrintDiscoveryResultsOutput(t *testing.T) {
	orch := NewOrchestrator(DefaultOptions())
	orch.localMachine = &DiscoveredMachine{
		Name:        "local-machine",
		TailscaleIP: "100.100.1.1",
		Role:        RoleAgent,
		IsLocal:     true,
	}
	orch.remoteMachines = []*DiscoveredMachine{
		{
			Name:          "csd",
			WezTermDomain: "csd",
			PublicIP:      "1.2.3.4",
			TailscaleIP:   "100.100.2.2",
			Username:      "ubuntu",
			Role:          RoleCoordinator,
		},
		{
			Name:          "css",
			WezTermDomain: "css",
			PublicIP:      "5.6.7.8",
			Username:      "user",
			Role:          RoleCoordinator,
		},
	}

	// Capture output
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	orch.PrintDiscoveryResults()

	w.Close()
	os.Stdout = old

	var buf strings.Builder
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "local-machine") {
		t.Error("expected local machine name in output")
	}
	if !strings.Contains(output, "csd") || !strings.Contains(output, "css") {
		t.Error("expected remote machines in output")
	}
	if !strings.Contains(output, "100.100.1.1") {
		t.Error("expected tailscale IP in output")
	}
}

func TestToSyncMachineReal(t *testing.T) {
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
			name: "with tailscale IP",
			opts: Options{UseTailscale: true},
			machine: &DiscoveredMachine{
				Name:         "test",
				PublicIP:     "1.2.3.4",
				TailscaleIP:  "100.100.1.1",
				Username:     "ubuntu",
				Port:         2222,
				IdentityFile: "/tmp/key",
			},
			wantAddr:    "100.100.1.1",
			wantPort:    2222,
			wantUser:    "ubuntu",
			wantKeyPath: "/tmp/key",
		},
		{
			name: "without tailscale IP",
			opts: Options{UseTailscale: true},
			machine: &DiscoveredMachine{
				Name:         "test",
				PublicIP:     "5.6.7.8",
				TailscaleIP:  "",
				Username:     "user",
				Port:         22,
				IdentityFile: "/home/user/.ssh/id_ed25519",
			},
			wantAddr:    "5.6.7.8",
			wantPort:    22,
			wantUser:    "user",
			wantKeyPath: "/home/user/.ssh/id_ed25519",
		},
		{
			name: "tailscale disabled",
			opts: Options{UseTailscale: false},
			machine: &DiscoveredMachine{
				Name:         "test",
				PublicIP:     "1.2.3.4",
				TailscaleIP:  "100.100.1.1",
				Username:     "ubuntu",
				Port:         22,
				IdentityFile: "/tmp/key",
			},
			wantAddr:    "1.2.3.4",
			wantPort:    22,
			wantUser:    "ubuntu",
			wantKeyPath: "/tmp/key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := NewOrchestrator(tt.opts)
			syncMachine := orch.toSyncMachine(tt.machine)
			if syncMachine.Address != tt.wantAddr {
				t.Errorf("address: got %s, want %s", syncMachine.Address, tt.wantAddr)
			}
			if syncMachine.Port != tt.wantPort {
				t.Errorf("port: got %d, want %d", syncMachine.Port, tt.wantPort)
			}
			if syncMachine.SSHUser != tt.wantUser {
				t.Errorf("user: got %s, want %s", syncMachine.SSHUser, tt.wantUser)
			}
			if syncMachine.SSHKeyPath != tt.wantKeyPath {
				t.Errorf("key path: got %s, want %s", syncMachine.SSHKeyPath, tt.wantKeyPath)
			}
		})
	}
}

func TestBuildSSHCommandReal(t *testing.T) {
	tests := []struct {
		name     string
		machine  *DiscoveredMachine
		opts     ScriptOptions
		contains string
	}{
		{
			name: "basic ssh command",
			machine: &DiscoveredMachine{
				Username: "ubuntu",
				PublicIP: "1.2.3.4",
				Port:     22,
			},
			opts:     ScriptOptions{UseTailscale: false},
			contains: "ssh ubuntu@1.2.3.4",
		},
		{
			name: "with identity file",
			machine: &DiscoveredMachine{
				Username:     "user",
				PublicIP:     "5.6.7.8",
				Port:         22,
				IdentityFile: "/home/user/.ssh/id_ed25519",
			},
			opts:     ScriptOptions{UseTailscale: false},
			contains: "-i /home/user/.ssh/id_ed25519",
		},
		{
			name: "with custom port",
			machine: &DiscoveredMachine{
				Username: "ubuntu",
				PublicIP: "1.2.3.4",
				Port:     2222,
			},
			opts:     ScriptOptions{UseTailscale: false},
			contains: "-p 2222",
		},
		{
			name: "tailscale preferred",
			machine: &DiscoveredMachine{
				Username:    "ubuntu",
				PublicIP:    "1.2.3.4",
				TailscaleIP: "100.100.1.1",
				Port:        22,
			},
			opts:     ScriptOptions{UseTailscale: true},
			contains: "ubuntu@100.100.1.1",
		},
		{
			name: "fallback to public IP when no tailscale IP",
			machine: &DiscoveredMachine{
				Username:    "ubuntu",
				PublicIP:    "5.6.7.8",
				TailscaleIP: "",
				Port:        22,
			},
			opts:     ScriptOptions{UseTailscale: true},
			contains: "ubuntu@5.6.7.8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := buildSSHCommand(tt.machine, tt.opts)
			if !strings.Contains(cmd, tt.contains) {
				t.Errorf("buildSSHCommand = %q, want to contain %q", cmd, tt.contains)
			}
		})
	}
}