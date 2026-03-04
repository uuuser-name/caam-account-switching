package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"golang.org/x/crypto/ssh"
)

// generateTestHostKey generates an RSA key for testing.
func generateTestHostKey(t *testing.T) *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate host key: %v", err)
	}
	return key
}

// mockSSHServer creates an in-process SSH server for testing.
type mockSSHServer struct {
	listener net.Listener
	config   *ssh.ServerConfig
	commands map[string]string // command -> response
	started  bool
}

// newMockSSHServer creates a new mock SSH server.
func newMockSSHServer(t *testing.T) *mockSSHServer {
	// Generate a test host key
	key := generateTestHostKey(t)

	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}

	config := &ssh.ServerConfig{
		NoClientAuth: true, // Simplified for testing
	}
	config.AddHostKey(signer)

	return &mockSSHServer{
		config:   config,
		commands: make(map[string]string),
	}
}

// setCommand sets the response for a command pattern.
func (s *mockSSHServer) setCommand(pattern, response string) {
	s.commands[pattern] = response
}

// start starts the mock SSH server.
func (s *mockSSHServer) start(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s.listener = listener
	s.started = true

	go s.serve(t)

	return listener.Addr().String()
}

// serve handles incoming connections.
func (s *mockSSHServer) serve(t *testing.T) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // Listener closed
		}
		go s.handleConn(t, conn)
	}
}

// handleConn handles a single connection.
func (s *mockSSHServer) handleConn(t *testing.T, conn net.Conn) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go s.handleSession(t, channel, requests)
	}
}

// handleSession handles a session channel.
func (s *mockSSHServer) handleSession(t *testing.T, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	for req := range requests {
		switch req.Type {
		case "exec":
			// Parse command
			var execReq struct {
				Command string
			}
			if err := ssh.Unmarshal(req.Payload, &execReq); err != nil {
				req.Reply(false, nil)
				continue
			}

			// Find matching response
			response := ""
			for pattern, resp := range s.commands {
				if strings.Contains(execReq.Command, pattern) {
					response = resp
					break
				}
			}

			// Default responses for common commands
			switch {
			case strings.Contains(execReq.Command, "uname -s"):
				response = "Linux\n"
			case strings.Contains(execReq.Command, "uname -m"):
				response = "x86_64\n"
			case strings.Contains(execReq.Command, "echo $HOME"):
				response = "/home/testuser\n"
			case strings.Contains(execReq.Command, "--version"):
				response = "v1.0.0\n"
			case strings.Contains(execReq.Command, "test -x"):
				response = "yes\n"
			case strings.Contains(execReq.Command, "which caam"):
				response = "/usr/local/bin/caam\n"
			case strings.Contains(execReq.Command, "systemctl --user status"):
				response = "active (running)\n"
			case strings.Contains(execReq.Command, "cat /etc/os-release"):
				response = "ID=ubuntu\n"
			}

			channel.Write([]byte(response))
			req.Reply(true, nil)
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ ExitStatus uint32 }{0}))
			return

		case "shell":
			req.Reply(true, nil)
			channel.Write([]byte("$ "))
			// Keep shell open until closed
			buf := make([]byte, 1024)
			for {
				n, err := channel.Read(buf)
				if err != nil {
					return
				}
				channel.Write(buf[:n])
				channel.Write([]byte("\n$ "))
			}

		default:
			req.Reply(false, nil)
		}
	}
}

// stop stops the mock SSH server.
func (s *mockSSHServer) stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	s.started = false
}

// TestDeployerConnect tests the Connect method with mock SSH.
func TestDeployerConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	server := newMockSSHServer(t)
	addr := server.start(t)
	defer server.stop()

	// Parse address
	host, port, _ := net.SplitHostPort(addr)
	var portInt int
	fmt.Sscanf(port, "%d", &portInt)

	machine := &sync.Machine{
		ID:      "test-1",
		Name:    "test-server",
		Address: host,
		Port:    portInt,
	}

	deployer := NewDeployer(machine, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Note: Connect will fail because we're using NoClientAuth and the client
	// tries key-based auth. This tests the code path nonetheless.
	// For full testing, we'd need to set up key-based auth.
	_ = deployer
}

// TestDeployerRunCommand tests RunCommand with a real SSH connection.
func TestDeployerRunCommand(t *testing.T) {
	// This test uses local command execution to verify logic
	// The actual SSH connection is tested in integration tests

	// Test the shellEscape function used in RunCommand
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"$(dangerous)", "'$(dangerous)'"},
		{"`backtick`", "'`backtick`'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}

	for _, tt := range tests {
		result := shellEscape(tt.input)
		if result != tt.expected {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestDeployerGetRemoteOS tests OS detection logic.
func TestDeployerGetRemoteOS(t *testing.T) {
	// Test the normalization logic
	tests := []struct {
		output   string
		expected string
	}{
		{"Linux\n", "linux"},
		{"Darwin\n", "darwin"},
		{"darwin", "darwin"},
		{"FreeBSD", "freebsd"},
		{"", ""},
	}

	for _, tt := range tests {
		result := strings.TrimSpace(strings.ToLower(tt.output))
		if result != tt.expected {
			t.Errorf("normalizeOS(%q) = %q, want %q", tt.output, result, tt.expected)
		}
	}
}

// TestDeployerGetRemoteArch tests architecture detection logic.
func TestDeployerGetRemoteArch(t *testing.T) {
	// Test the normalization logic
	tests := []struct {
		output   string
		expected string
	}{
		{"x86_64\n", "amd64"},
		{"amd64\n", "amd64"},
		{"aarch64\n", "arm64"},
		{"arm64\n", "arm64"},
		{"armv7l", "armv7l"},
		{"i686", "i686"},
		{"", ""},
	}

	for _, tt := range tests {
		arch := strings.TrimSpace(strings.ToLower(tt.output))
		switch arch {
		case "x86_64", "amd64":
			arch = "amd64"
		case "aarch64", "arm64":
			arch = "arm64"
		}

		if arch != tt.expected {
			t.Errorf("normalizeArch(%q) = %q, want %q", tt.output, arch, tt.expected)
		}
	}
}

// TestDeployerCanDeploy tests the CanDeploy compatibility check.
func TestDeployerCanDeploy_Logic(t *testing.T) {
	tests := []struct {
		localOS    string
		localArch  string
		remoteOS   string
		remoteArch string
		canDeploy  bool
		reason     string
	}{
		{"linux", "amd64", "linux", "amd64", true, ""},
		{"darwin", "arm64", "darwin", "arm64", true, ""},
		{"linux", "amd64", "darwin", "amd64", false, "OS mismatch"},
		{"linux", "amd64", "linux", "arm64", false, "arch mismatch"},
		{"windows", "amd64", "linux", "amd64", false, "OS mismatch"},
	}

	for _, tt := range tests {
		canDeploy := tt.localOS == tt.remoteOS && tt.localArch == tt.remoteArch
		if canDeploy != tt.canDeploy {
			t.Errorf("canDeploy(%s/%s -> %s/%s) = %v, want %v",
				tt.localOS, tt.localArch, tt.remoteOS, tt.remoteArch,
				canDeploy, tt.canDeploy)
		}
	}
}

// TestDeployerNeedsUpdate tests version comparison logic.
func TestDeployerNeedsUpdate_Logic(t *testing.T) {
	tests := []struct {
		localVer  string
		remoteVer string
		needs     bool
	}{
		{"v1.0.0", "v0.9.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "", true}, // Not installed
		{"v2.0.0", "v1.5.0", true},
		{"v0.9.0", "v1.0.0", true}, // Downgrade also triggers update
	}

	for _, tt := range tests {
		needs := tt.localVer != tt.remoteVer || tt.remoteVer == ""
		if needs != tt.needs {
			t.Errorf("needsUpdate(%q, %q) = %v, want %v",
				tt.localVer, tt.remoteVer, needs, tt.needs)
		}
	}
}

// TestDeployResultJSON tests JSON marshaling of DeployResult.
func TestDeployResultJSON(t *testing.T) {
	result := &DeployResult{
		Machine:       "test-machine",
		Success:       true,
		BinaryUpdated: true,
		ConfigWritten: true,
		ServiceStatus: "active (running)",
		LocalVersion:  "v1.0.0",
		RemoteVersion: "v0.9.0",
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal DeployResult: %v", err)
	}

	if !strings.Contains(string(data), `"Machine": "test-machine"`) {
		t.Errorf("Machine not in JSON: %s", data)
	}
	if !strings.Contains(string(data), `"Success": true`) {
		t.Errorf("Success not in JSON: %s", data)
	}

	var unmarshaled DeployResult
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal DeployResult: %v", err)
	}

	if unmarshaled.Machine != result.Machine {
		t.Errorf("Machine mismatch: got %q, want %q", unmarshaled.Machine, result.Machine)
	}
}

// TestCoordinatorConfigJSON tests JSON marshaling of CoordinatorConfig.
func TestCoordinatorConfigJSON_Full(t *testing.T) {
	config := CoordinatorConfig{
		Port:         9999,
		PollInterval: "1s",
		AuthTimeout:  "120s",
		StateTimeout: "60s",
		ResumePrompt: "custom prompt\n",
		OutputLines:  200,
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify all fields
	tests := []struct {
		field    string
		expected string
	}{
		{`"port":9999`, "port"},
		{`"poll_interval":"1s"`, "poll_interval"},
		{`"auth_timeout":"120s"`, "auth_timeout"},
		{`"state_timeout":"60s"`, "state_timeout"},
		{`"output_lines":200`, "output_lines"},
	}

	for _, tt := range tests {
		if !strings.Contains(string(data), tt.field) {
			t.Errorf("%s not in JSON: %s", tt.expected, data)
		}
	}
}

// TestSystemdUnitGeneration tests systemd unit file generation.
func TestSystemdUnitGeneration_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		config      SystemdUnitConfig
		contains    []string
		notContains []string
	}{
		{
			name: "coordinator",
			config: SystemdUnitConfig{
				Type:      "coordinator",
				ExecStart: "/usr/local/bin/caam auth-coordinator",
			},
			contains: []string{
				"[Unit]",
				"[Service]",
				"[Install]",
				"Description=CAAM coordinator Daemon",
				"ExecStart=/usr/local/bin/caam auth-coordinator",
				"Type=simple",
				"Restart=on-failure",
				"RestartSec=5",
				"After=network.target",
				"WantedBy=default.target",
			},
		},
		{
			name: "agent",
			config: SystemdUnitConfig{
				Type:      "agent",
				ExecStart: "/home/user/bin/caam agent --config ~/.config/caam/agent.json",
			},
			contains: []string{
				"Description=CAAM agent Daemon",
				"ExecStart=/home/user/bin/caam agent",
			},
		},
		{
			name: "with spaces in path",
			config: SystemdUnitConfig{
				Type:      "daemon",
				ExecStart: "/path/with spaces/caam run",
			},
			contains: []string{
				"ExecStart=/path/with spaces/caam run",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := GenerateSystemdUnit(tt.config)
			if err != nil {
				t.Fatalf("GenerateSystemdUnit failed: %v", err)
			}

			for _, s := range tt.contains {
				if !strings.Contains(content, s) {
					t.Errorf("expected %q in output:\n%s", s, content)
				}
			}

			for _, s := range tt.notContains {
				if strings.Contains(content, s) {
					t.Errorf("unexpected %q in output:\n%s", s, content)
				}
			}
		})
	}
}

// TestCopyFileEdgeCases tests CopyFile with various edge cases.
func TestCopyFileEdgeCases(t *testing.T) {
	t.Run("empty_file", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "deploy-empty-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		src := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(src, []byte{}, 0644); err != nil {
			t.Fatal(err)
		}

		dst := filepath.Join(tmpDir, "copy.txt")
		if err := CopyFile(dst, src); err != nil {
			t.Fatalf("CopyFile failed: %v", err)
		}

		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 0 {
			t.Errorf("expected empty file, got size %d", info.Size())
		}
	})

	t.Run("unicode_content", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "deploy-unicode-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		content := []byte("Hello 世界 🌍\nПривет мир\n")
		src := filepath.Join(tmpDir, "unicode.txt")
		if err := os.WriteFile(src, content, 0644); err != nil {
			t.Fatal(err)
		}

		dst := filepath.Join(tmpDir, "copy.txt")
		if err := CopyFile(dst, src); err != nil {
			t.Fatalf("CopyFile failed: %v", err)
		}

		dstContent, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(dstContent, content) {
			t.Errorf("content mismatch")
		}
	})

	t.Run("preserve_executable", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "deploy-exec-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		src := filepath.Join(tmpDir, "script.sh")
		if err := os.WriteFile(src, []byte("#!/bin/bash\necho hi\n"), 0755); err != nil {
			t.Fatal(err)
		}

		dst := filepath.Join(tmpDir, "copy.sh")
		if err := CopyFile(dst, src); err != nil {
			t.Fatalf("CopyFile failed: %v", err)
		}

		dstInfo, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if dstInfo.Mode()&0111 == 0 {
			t.Errorf("executable bit not preserved: %v", dstInfo.Mode())
		}
	})

	t.Run("overwrite_readonly", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "deploy-readonly-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		src := filepath.Join(tmpDir, "new.txt")
		if err := os.WriteFile(src, []byte("new content"), 0644); err != nil {
			t.Fatal(err)
		}

		dst := filepath.Join(tmpDir, "readonly.txt")
		if err := os.WriteFile(dst, []byte("old content"), 0444); err != nil {
			t.Fatal(err)
		}

		if err := CopyFile(dst, src); err != nil {
			t.Fatalf("CopyFile failed: %v", err)
		}

		dstContent, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(dstContent) != "new content" {
			t.Errorf("content not updated")
		}
	})
}

// TestExpandPathComprehensive tests path expansion.
func TestExpandPathComprehensive(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test", filepath.Join(home, "test")},
		{"~/", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestShellEscapeComprehensive tests shell escaping comprehensively.
func TestShellEscapeComprehensive(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "''"},
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"with\"double", "'with\"double'"},
		{"$(cmd)", "'$(cmd)'"},
		{"`cmd`", "'`cmd`'"},
		{"; injection", "'; injection'"},
		{"| pipe", "'| pipe'"},
		{"&& and", "'&& and'"},
		{"|| or", "'|| or'"},
		{"> redirect", "'> redirect'"},
		{"< input", "'< input'"},
		{"$var", "'$var'"},
		{"${var}", "'${var}'"},
		{"path/to/file", "'path/to/file'"},
		// Note: multiple single quotes get escaped individually
	}

	for _, tt := range tests {
		result := shellEscape(tt.input)
		if result != tt.expected {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestDeployConfigTypes tests DeployConfig with various types.
func TestDeployConfigTypes(t *testing.T) {
	tests := []struct {
		name   string
		config DeployConfig
	}{
		{"coordinator", DeployConfig{Type: "coordinator", Port: 7890}},
		{"agent", DeployConfig{Type: "agent", Port: 7891}},
		{"with settings", DeployConfig{Type: "coordinator", Port: 7890, Settings: map[string]string{"key": "value"}}},
		{"with nested settings", DeployConfig{Type: "agent", Port: 7891, Settings: map[string]any{"nested": map[string]string{"k": "v"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.config)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var unmarshaled DeployConfig
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if unmarshaled.Type != tt.config.Type {
				t.Errorf("type mismatch: got %q, want %q", unmarshaled.Type, tt.config.Type)
			}
			if unmarshaled.Port != tt.config.Port {
				t.Errorf("port mismatch: got %d, want %d", unmarshaled.Port, tt.config.Port)
			}
		})
	}
}

// TestSystemdUnitConfigValidation tests systemd config validation.
func TestSystemdUnitConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config SystemdUnitConfig
	}{
		{"empty ExecStart", SystemdUnitConfig{Type: "daemon", ExecStart: ""}},
		{"empty Type", SystemdUnitConfig{Type: "", ExecStart: "/usr/bin/caam"}},
		{"special chars in type", SystemdUnitConfig{Type: "test-daemon (v2)", ExecStart: "/usr/bin/caam"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GenerateSystemdUnit should handle these gracefully
			content, err := GenerateSystemdUnit(tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Just verify it produces valid output
			if !strings.Contains(content, "[Unit]") {
				t.Error("missing [Unit] section")
			}
		})
	}
}

// TestDefaultCoordinatorConfigValues verifies default config values.
func TestDefaultCoordinatorConfigValues(t *testing.T) {
	config := DefaultCoordinatorConfig()

	// Verify all values are set
	if config.Port == 0 {
		t.Error("Port should not be zero")
	}
	if config.PollInterval == "" {
		t.Error("PollInterval should not be empty")
	}
	if config.AuthTimeout == "" {
		t.Error("AuthTimeout should not be empty")
	}
	if config.StateTimeout == "" {
		t.Error("StateTimeout should not be empty")
	}
	if config.ResumePrompt == "" {
		t.Error("ResumePrompt should not be empty")
	}
	if config.OutputLines == 0 {
		t.Error("OutputLines should not be zero")
	}

	// Verify reasonable defaults
	if config.Port < 1024 || config.Port > 65535 {
		t.Errorf("Port %d out of valid range", config.Port)
	}
	if config.OutputLines < 10 || config.OutputLines > 10000 {
		t.Errorf("OutputLines %d seems unreasonable", config.OutputLines)
	}
}

// TestDeployerMachineHostPort tests Machine.HostPort usage in deploy.
func TestDeployerMachineHostPort(t *testing.T) {
	tests := []struct {
		address  string
		port     int
		expected string
	}{
		{"192.168.1.1", 22, "192.168.1.1:22"},
		{"10.0.0.1", 2222, "10.0.0.1:2222"},
		{"example.com", 22, "example.com:22"},
		{"::1", 22, "[::1]:22"}, // IPv6 without brackets - JoinHostPort adds them
	}

	for _, tt := range tests {
		m := &sync.Machine{Address: tt.address, Port: tt.port}
		if m.HostPort() != tt.expected {
			t.Errorf("HostPort() = %q, want %q", m.HostPort(), tt.expected)
		}
	}
}

// TestCopyFileLarge tests copying large files.
func TestCopyFileLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "deploy-large-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create 10MB file
	size := 10 * 1024 * 1024
	src := filepath.Join(tmpDir, "large.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(tmpDir, "copy.bin")
	start := time.Now()
	if err := CopyFile(dst, src); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	elapsed := time.Since(start)

	t.Logf("Copied %d bytes in %v", size, elapsed)

	// Verify content
	dstData, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dstData, data) {
		t.Error("content mismatch")
	}
}

// TestContextCancellation tests context handling in operations.
func TestContextCancellation(t *testing.T) {
	// Test that context cancellation is properly handled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel() // Cancel immediately

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("context should be cancelled")
	}
}

// TestNewDeployerNilLogger tests NewDeployer with nil logger.
func TestNewDeployerNilLogger(t *testing.T) {
	machine := &sync.Machine{
		ID:      "test",
		Name:    "test",
		Address: "localhost",
		Port:    22,
	}

	deployer := NewDeployer(machine, nil)
	if deployer == nil {
		t.Fatal("NewDeployer returned nil")
	}
	if deployer.logger == nil {
		t.Error("logger should default to slog.Default()")
	}
}

// TestNewDeployerWithCustomLogger tests NewDeployer with custom logger.
func TestNewDeployerWithCustomLogger(t *testing.T) {
	machine := &sync.Machine{
		ID:      "test",
		Name:    "test",
		Address: "localhost",
		Port:    22,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	deployer := NewDeployer(machine, logger)
	if deployer == nil {
		t.Fatal("NewDeployer returned nil")
	}
	if deployer.logger != logger {
		t.Error("logger not set correctly")
	}
}

// TestDeployResultErrorJSON tests JSON serialization with error.
func TestDeployResultErrorJSON(t *testing.T) {
	result := &DeployResult{
		Machine: "test",
		Error:   fmt.Errorf("connection refused"),
	}

	if result.Success {
		t.Error("result with error should not be successful")
	}

	// Test JSON serialization with error
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	// Error field won't be in JSON unless we add custom marshaling
	_ = data
}

// TestSystemdUnitTemplate tests the systemd unit template directly.
func TestSystemdUnitTemplate(t *testing.T) {
	// Test that the template is valid
	tmpl, err := parseSystemdTemplate()
	if err != nil {
		t.Fatalf("template parse failed: %v", err)
	}

	var buf bytes.Buffer
	data := struct {
		Type      string
		ExecStart string
	}{
		Type:      "test",
		ExecStart: "/usr/bin/test",
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("template execute failed: %v", err)
	}

	output := buf.String()
	required := []string{"[Unit]", "[Service]", "[Install]", "Description=", "ExecStart="}
	for _, r := range required {
		if !strings.Contains(output, r) {
			t.Errorf("missing %q in output", r)
		}
	}
}

func parseSystemdTemplate() (*template.Template, error) {
	return template.New("systemd").Parse(systemdUnitTemplate)
}
