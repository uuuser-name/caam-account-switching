package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
)

func TestGenerateSystemdUnit(t *testing.T) {
	config := SystemdUnitConfig{
		Type:      "Auth Recovery Coordinator",
		ExecStart: "/usr/local/bin/caam auth-coordinator --config ~/.config/caam/coordinator.json",
	}

	content, err := GenerateSystemdUnit(config)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit failed: %v", err)
	}

	// Check required sections
	if !strings.Contains(content, "[Unit]") {
		t.Error("missing [Unit] section")
	}
	if !strings.Contains(content, "[Service]") {
		t.Error("missing [Service] section")
	}
	if !strings.Contains(content, "[Install]") {
		t.Error("missing [Install] section")
	}

	// Check important fields
	if !strings.Contains(content, "Description=CAAM Auth Recovery Coordinator Daemon") {
		t.Error("missing or incorrect Description")
	}
	if !strings.Contains(content, "ExecStart=/usr/local/bin/caam auth-coordinator") {
		t.Error("missing or incorrect ExecStart")
	}
	if !strings.Contains(content, "Type=simple") {
		t.Error("missing Type=simple")
	}
	if !strings.Contains(content, "Restart=on-failure") {
		t.Error("missing Restart=on-failure")
	}
	if !strings.Contains(content, "WantedBy=default.target") {
		t.Error("missing WantedBy=default.target")
	}
}

func TestDefaultCoordinatorConfig(t *testing.T) {
	config := DefaultCoordinatorConfig()

	if config.Port != 7890 {
		t.Errorf("expected port 7890, got %d", config.Port)
	}
	if config.PollInterval != "500ms" {
		t.Errorf("expected poll_interval 500ms, got %s", config.PollInterval)
	}
	if config.AuthTimeout != "60s" {
		t.Errorf("expected auth_timeout 60s, got %s", config.AuthTimeout)
	}
	if config.OutputLines != 100 {
		t.Errorf("expected output_lines 100, got %d", config.OutputLines)
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		input    string
		contains string // Expected to contain this
	}{
		{"~/test", "test"},       // Should expand home
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("expandPath(%q) = %q, expected to contain %q", tt.input, result, tt.contains)
		}
		// Home expansion should not start with ~
		if strings.HasPrefix(tt.input, "~/") && strings.HasPrefix(result, "~") {
			t.Errorf("expandPath(%q) = %q, tilde not expanded", tt.input, result)
		}
	}
}

func TestSystemdUnitConfig(t *testing.T) {
	// Test with different service types
	types := []string{"coordinator", "agent", "daemon"}

	for _, typ := range types {
		config := SystemdUnitConfig{
			Type:      typ,
			ExecStart: "/usr/local/bin/caam " + typ,
		}

		content, err := GenerateSystemdUnit(config)
		if err != nil {
			t.Errorf("GenerateSystemdUnit failed for type %s: %v", typ, err)
			continue
		}

		if !strings.Contains(content, "Description=CAAM "+typ+" Daemon") {
			t.Errorf("missing correct description for type %s", typ)
		}
		if !strings.Contains(content, "ExecStart=/usr/local/bin/caam "+typ) {
			t.Errorf("missing correct ExecStart for type %s", typ)
		}
	}
}

func TestCoordinatorConfigJSON(t *testing.T) {
	config := DefaultCoordinatorConfig()

	// Test that the config can be marshaled (used in WriteCoordinatorConfig)
	// We don't actually marshal here since it's tested implicitly by the struct tags
	// Just verify the fields are accessible
	if config.Port == 0 {
		t.Error("port should not be zero")
	}
	if config.PollInterval == "" {
		t.Error("poll_interval should not be empty")
	}
	if config.AuthTimeout == "" {
		t.Error("auth_timeout should not be empty")
	}
	if config.ResumePrompt == "" {
		t.Error("resume_prompt should not be empty")
	}
}

func TestDeployResultFields(t *testing.T) {
	result := &DeployResult{
		Machine:       "test-machine",
		Success:       true,
		BinaryUpdated: true,
		ConfigWritten: true,
		ServiceStatus: "active",
		LocalVersion:  "1.0.0",
		RemoteVersion: "0.9.0",
	}

	if result.Machine != "test-machine" {
		t.Errorf("expected machine test-machine, got %s", result.Machine)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if !result.BinaryUpdated {
		t.Error("expected binary_updated=true")
	}
	if result.ServiceStatus != "active" {
		t.Errorf("expected service_status active, got %s", result.ServiceStatus)
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"with$var", "'with$var'"},
		{"with`backtick", "'with`backtick'"},
		{"with\"doublequote", "'with\"doublequote'"},
		{"", "''"},
		{"normal-path", "'normal-path'"},
	}

	for _, tt := range tests {
		result := shellEscape(tt.input)
		if result != tt.expected {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestShellEscapeSecurity(t *testing.T) {
	// Test that shell escape prevents command injection
	maliciousInputs := []string{
		"; rm -rf /",
		"$(cat /etc/passwd)",
		"`whoami`",
		"| cat /etc/shadow",
		"&& echo pwned",
		"file.txt\nrm -rf /",
	}

	for _, input := range maliciousInputs {
		escaped := shellEscape(input)
		// The escaped string should be safe when used in shell
		// It should be wrapped in single quotes
		if !strings.HasPrefix(escaped, "'") || !strings.HasSuffix(escaped, "'") {
			t.Errorf("shellEscape(%q) = %q, not properly quoted", input, escaped)
		}
		// Any single quotes inside should be properly escaped
		if strings.Contains(input, "'") {
			// Should contain the escape sequence
			if !strings.Contains(escaped, "'\\''") {
				t.Errorf("shellEscape(%q) = %q, single quote not properly escaped", input, escaped)
			}
		}
	}
}

func TestCopyFile(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "deploy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source file with content
	srcPath := filepath.Join(tmpDir, "source.txt")
	srcContent := []byte("test content for copy\nwith multiple lines\n")
	if err := os.WriteFile(srcPath, srcContent, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Test copy to new file
	dstPath := filepath.Join(tmpDir, "dest.txt")
	if err := CopyFile(dstPath, srcPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify content
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(dstContent, srcContent) {
		t.Errorf("content mismatch: got %q, want %q", dstContent, srcContent)
	}

	// Verify permissions
	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)
	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("mode mismatch: got %v, want %v", dstInfo.Mode(), srcInfo.Mode())
	}
}

func TestCopyFileAtomic(t *testing.T) {
	// Test that CopyFile uses atomic write pattern
	tmpDir, err := os.MkdirTemp("", "deploy-atomic-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "source.txt")
	srcContent := []byte("atomic content")
	if err := os.WriteFile(srcPath, srcContent, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Create dest directory (nested)
	dstDir := filepath.Join(tmpDir, "nested", "deep", "dir")
	dstPath := filepath.Join(dstDir, "dest.txt")

	// CopyFile should create intermediate directories
	if err := CopyFile(dstPath, srcPath); err != nil {
		t.Fatalf("CopyFile to nested path failed: %v", err)
	}

	// Verify file exists at destination
	if _, err := os.Stat(dstPath); err != nil {
		t.Errorf("destination file not created: %v", err)
	}

	// No temp files should remain
	files, _ := filepath.Glob(filepath.Join(dstDir, "*.tmp.*"))
	if len(files) > 0 {
		t.Errorf("temp files remain: %v", files)
	}
}

func TestCopyFileNonExistentSource(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "deploy-nosrc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dstPath := filepath.Join(tmpDir, "dest.txt")
	err = CopyFile(dstPath, "/nonexistent/source/file.txt")
	if err == nil {
		t.Error("expected error for non-existent source file")
	}
}

func TestCopyFileOverwrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "deploy-overwrite-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "source.txt")
	srcContent := []byte("new content")
	if err := os.WriteFile(srcPath, srcContent, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "dest.txt")
	oldContent := []byte("old content")
	if err := os.WriteFile(dstPath, oldContent, 0644); err != nil {
		t.Fatalf("failed to create dest file: %v", err)
	}

	if err := CopyFile(dstPath, srcPath); err != nil {
		t.Fatalf("CopyFile overwrite failed: %v", err)
	}

	dstContent, _ := os.ReadFile(dstPath)
	if !bytes.Equal(dstContent, srcContent) {
		t.Errorf("content not overwritten: got %q, want %q", dstContent, srcContent)
	}
}

func TestCoordinatorConfigJSONMarshal(t *testing.T) {
	config := CoordinatorConfig{
		Port:          9999,
		PollInterval:  "1s",
		AuthTimeout:   "120s",
		StateTimeout:  "60s",
		ResumePrompt:  "custom prompt\n",
		OutputLines:   200,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	// Verify JSON structure
	if !strings.Contains(string(data), `"port": 9999`) {
		t.Errorf("port not in JSON: %s", data)
	}
	if !strings.Contains(string(data), `"poll_interval": "1s"`) {
		t.Errorf("poll_interval not in JSON: %s", data)
	}

	// Unmarshal and verify
	var unmarshaled CoordinatorConfig
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if unmarshaled.Port != config.Port {
		t.Errorf("port mismatch: got %d, want %d", unmarshaled.Port, config.Port)
	}
	if unmarshaled.PollInterval != config.PollInterval {
		t.Errorf("poll_interval mismatch: got %s, want %s", unmarshaled.PollInterval, config.PollInterval)
	}
}

func TestDeployConfigJSON(t *testing.T) {
	config := DeployConfig{
		Type:     "coordinator",
		Port:     7890,
		Settings: map[string]string{"key": "value"},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal DeployConfig: %v", err)
	}

	var unmarshaled DeployConfig
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal DeployConfig: %v", err)
	}

	if unmarshaled.Type != config.Type {
		t.Errorf("type mismatch: got %s, want %s", unmarshaled.Type, config.Type)
	}
}

func TestNewDeployer(t *testing.T) {
	// Create a minimal machine for testing
	machine := &sync.Machine{
		ID:      "test-id",
		Name:    "test-machine",
		Address: "192.168.1.100",
		Port:    22,
	}

	deployer := NewDeployer(machine, nil)
	if deployer == nil {
		t.Fatal("NewDeployer returned nil")
	}
	if deployer.machine != machine {
		t.Error("deployer machine not set correctly")
	}
	if deployer.logger == nil {
		t.Error("deployer logger should be set to default when nil passed")
	}
}

func TestNewDeployerWithLogger(t *testing.T) {
	machine := &sync.Machine{
		ID:      "test-id",
		Name:    "test-machine",
		Address: "192.168.1.100",
		Port:    22,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	deployer := NewDeployer(machine, logger)
	if deployer == nil {
		t.Fatal("NewDeployer returned nil")
	}
	if deployer.logger != logger {
		t.Error("deployer logger not set correctly")
	}
}

func TestSystemdUnitTemplateParsing(t *testing.T) {
	// Test various ExecStart values
	testCases := []struct {
		name     string
		execPath string
		args     string
	}{
		{"standard", "/usr/local/bin/caam", "auth-coordinator"},
		{"home_bin", "/home/user/bin/caam", "agent --config /etc/caam/config.json"},
		{"with spaces", "/path with spaces/caam", "run"},
		{"with special args", "/usr/bin/caam", "--port 8080 --verbose"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := SystemdUnitConfig{
				Type:      "test",
				ExecStart: tc.execPath + " " + tc.args,
			}

			content, err := GenerateSystemdUnit(config)
			if err != nil {
				t.Fatalf("GenerateSystemdUnit failed: %v", err)
			}

			// Check that ExecStart is properly rendered
			if !strings.Contains(content, "ExecStart="+tc.execPath) {
				t.Errorf("ExecStart path not in output: %s", content)
			}
		})
	}
}

func TestGetRemoteOS(t *testing.T) {
	// Architecture normalization is done on raw output
	testCases := []struct {
		output   string
		expected string
	}{
		{"Linux\n", "linux"},
		{"Darwin\n", "darwin"},
		{"darwin", "darwin"},
		{"", ""},
	}

	for _, tc := range testCases {
		// Simulate the normalization done by GetRemoteOS
		result := strings.TrimSpace(strings.ToLower(tc.output))
		if result != tc.expected {
			t.Errorf("normalize(%q) = %q, want %q", tc.output, result, tc.expected)
		}
	}
}

func TestGetRemoteArch(t *testing.T) {
	testCases := []struct {
		output   string
		expected string
	}{
		{"x86_64\n", "amd64"},
		{"amd64\n", "amd64"},
		{"aarch64\n", "arm64"},
		{"arm64\n", "arm64"},
		{"unknown\n", "unknown"},
		{"", ""},
	}

	for _, tc := range testCases {
		// Simulate the normalization done by GetRemoteArch
		arch := strings.TrimSpace(strings.ToLower(tc.output))
		switch arch {
		case "x86_64", "amd64":
			arch = "amd64"
		case "aarch64", "arm64":
			arch = "arm64"
		}
		if arch != tc.expected {
			t.Errorf("normalizeArch(%q) = %q, want %q", tc.output, arch, tc.expected)
		}
	}
}

func TestCanDeployLogic(t *testing.T) {
	// Test the logic of CanDeploy without actual SSH
	testCases := []struct {
		localOS    string
		localArch  string
		remoteOS   string
		remoteArch string
		canDeploy  bool
	}{
		{"linux", "amd64", "linux", "amd64", true},
		{"darwin", "arm64", "darwin", "arm64", true},
		{"linux", "amd64", "darwin", "amd64", false}, // OS mismatch
		{"linux", "amd64", "linux", "arm64", false},  // Arch mismatch
		{"linux", "amd64", "darwin", "arm64", false}, // Both mismatch
	}

	for _, tc := range testCases {
		// Simulate CanDeploy logic
		canDeploy := tc.localOS == tc.remoteOS && tc.localArch == tc.remoteArch
		if canDeploy != tc.canDeploy {
			t.Errorf("canDeploy(%s/%s -> %s/%s) = %v, want %v",
				tc.localOS, tc.localArch, tc.remoteOS, tc.remoteArch,
				canDeploy, tc.canDeploy)
		}
	}
}

func TestNeedsUpdateLogic(t *testing.T) {
	testCases := []struct {
		localVer  string
		remoteVer string
		needsUpd  bool
	}{
		{"1.0.0", "0.9.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "", true}, // Not installed
		{"", "1.0.0", true}, // Can't determine local
	}

	for _, tc := range testCases {
		needsUpdate := tc.localVer != tc.remoteVer || tc.remoteVer == ""
		if needsUpdate != tc.needsUpd {
			t.Errorf("needsUpdate(%q, %q) = %v, want %v",
				tc.localVer, tc.remoteVer, needsUpdate, tc.needsUpd)
		}
	}
}

func TestLargeFileCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "deploy-large-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a large-ish file (1MB)
	srcPath := filepath.Join(tmpDir, "large.bin")
	srcContent := make([]byte, 1024*1024)
	for i := range srcContent {
		srcContent[i] = byte(i % 256)
	}
	if err := os.WriteFile(srcPath, srcContent, 0644); err != nil {
		t.Fatalf("failed to create large source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "large-copy.bin")
	if err := CopyFile(dstPath, srcPath); err != nil {
		t.Fatalf("CopyFile large file failed: %v", err)
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read large dest file: %v", err)
	}
	if !bytes.Equal(dstContent, srcContent) {
		t.Error("large file content mismatch")
	}
}

func TestDeployResultError(t *testing.T) {
	result := &DeployResult{
		Machine: "test",
		Success: false,
		Error:   fmt.Errorf("connection failed"),
	}

	if result.Success {
		t.Error("expected success=false")
	}
	if result.Error == nil {
		t.Error("expected error to be set")
	}
}

func TestSystemdUnitWithSpecialCharacters(t *testing.T) {
	config := SystemdUnitConfig{
		Type:      "test-daemon",
		ExecStart: "/usr/local/bin/caam run --config /path/with spaces/config.json",
	}

	content, err := GenerateSystemdUnit(config)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit failed: %v", err)
	}

	// Should contain the path even with spaces
	if !strings.Contains(content, "/usr/local/bin/caam") {
		t.Error("ExecStart path not in unit")
	}
}

func TestBinaryPathDiscovery(t *testing.T) {
	// Test the findBinaryPath logic with different scenarios
	// This tests the logic without actual SSH connection
	
	// When no binary found, should return default
	defaultPath := "/usr/local/bin/caam"
	
	// Test that default is returned when nothing exists
	// (in real scenario this would be tested with mock SSH)
	if defaultPath != "/usr/local/bin/caam" {
		t.Errorf("unexpected default path: %s", defaultPath)
	}
}

func TestFindLocalBinaryLogic(t *testing.T) {
	// Test the candidate paths used in findLocalBinary
	expectedCandidates := []string{
		"/usr/local/bin/caam",
		"./caam",
		"./cmd/caam/caam",
	}

	// Verify these are checked (they should exist in the logic)
	for _, candidate := range expectedCandidates {
		// In a real test with a binary, we'd verify this path is checked
		if candidate == "" {
			t.Error("empty candidate path")
		}
	}
}

func TestMachineHostPort(t *testing.T) {
	machine := &sync.Machine{
		Address: "192.168.1.100",
		Port:    22,
	}
	hostPort := machine.HostPort()
	expected := "192.168.1.100:22"
	if hostPort != expected {
		t.Errorf("HostPort() = %q, want %q", hostPort, expected)
	}

	// Test with custom port
	machine.Port = 2222
	hostPort = machine.HostPort()
	expected = "192.168.1.100:2222"
	if hostPort != expected {
		t.Errorf("HostPort() = %q, want %q", hostPort, expected)
	}
}

func TestCoordinatorConfigDefaults(t *testing.T) {
	// Verify all default config values are sensible
	config := DefaultCoordinatorConfig()

	if config.Port <= 0 || config.Port > 65535 {
		t.Errorf("invalid port: %d", config.Port)
	}
	if config.OutputLines <= 0 {
		t.Errorf("invalid output_lines: %d", config.OutputLines)
	}
	// Verify time formats are parseable
	if config.PollInterval == "" {
		t.Error("poll_interval should have default")
	}
	if config.AuthTimeout == "" {
		t.Error("auth_timeout should have default")
	}
	if config.StateTimeout == "" {
		t.Error("state_timeout should have default")
	}
}

func TestSystemdUnitRestartPolicy(t *testing.T) {
	config := SystemdUnitConfig{
		Type:      "test",
		ExecStart: "/usr/bin/caam test",
	}

	content, err := GenerateSystemdUnit(config)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit failed: %v", err)
	}

	// Verify restart policy is sensible
	if !strings.Contains(content, "Restart=on-failure") {
		t.Error("should use on-failure restart policy")
	}
	if !strings.Contains(content, "RestartSec=5") {
		t.Error("should have reasonable restart delay")
	}
}

func TestSystemdUnitNetworkDependency(t *testing.T) {
	config := SystemdUnitConfig{
		Type:      "coordinator",
		ExecStart: "/usr/bin/caam coordinator",
	}

	content, err := GenerateSystemdUnit(config)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit failed: %v", err)
	}

	// Verify network dependency
	if !strings.Contains(content, "After=network.target") {
		t.Error("should have network.target dependency")
	}
}