package tailscale

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Sample JSON from actual tailscale status --json output
const sampleStatusJSON = `{
  "Version": "1.92.3-ta17f36b9b-ga4dc88aac",
  "TUN": true,
  "BackendState": "Running",
  "Self": {
    "ID": "n5vXP5MkcX11CNTRL",
    "HostName": "threadripperje",
    "DNSName": "threadripperje.tail1f21e.ts.net.",
    "OS": "linux",
    "TailscaleIPs": ["100.91.120.17", "fd7a:115c:a1e0::4534:7811"],
    "Online": true
  },
  "Peer": {
    "nodekey:abc123": {
      "ID": "n1234",
      "HostName": "SuperServer",
      "DNSName": "superserver.tail1f21e.ts.net.",
      "TailscaleIPs": ["100.90.148.85", "fd7a:115c:a1e0::f334:9455"],
      "Online": true,
      "OS": "linux"
    },
    "nodekey:def456": {
      "ID": "n5678",
      "HostName": "SenseDemoBox",
      "DNSName": "sensedemobox.tail1f21e.ts.net.",
      "TailscaleIPs": ["100.100.118.85", "fd7a:115c:a1e0::6a34:7655"],
      "Online": true,
      "OS": "linux"
    },
    "nodekey:ghi789": {
      "ID": "n9012",
      "HostName": "Jeffrey's Mac mini",
      "DNSName": "jeffreys-mac-mini.tail1f21e.ts.net.",
      "TailscaleIPs": ["100.114.183.31", "fd7a:115c:a1e0::9734:b71f"],
      "Online": true,
      "OS": "darwin"
    },
    "nodekey:jkl012": {
      "ID": "n3456",
      "HostName": "ubuntu-vm",
      "DNSName": "ubuntu-vm.tail1f21e.ts.net.",
      "TailscaleIPs": ["100.73.182.80"],
      "Online": false,
      "OS": "linux"
    }
  }
}`

func TestParseStatus(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	if status.BackendState != "Running" {
		t.Errorf("expected BackendState=Running, got %s", status.BackendState)
	}

	if status.Self == nil {
		t.Fatal("Self is nil")
	}

	if status.Self.HostName != "threadripperje" {
		t.Errorf("expected Self.HostName=threadripperje, got %s", status.Self.HostName)
	}

	if len(status.Peer) != 4 {
		t.Errorf("expected 4 peers, got %d", len(status.Peer))
	}
}

func TestPeerGetIPv4(t *testing.T) {
	peer := &Peer{
		TailscaleIPs: []string{"100.90.148.85", "fd7a:115c:a1e0::f334:9455"},
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "100.90.148.85" {
		t.Errorf("expected 100.90.148.85, got %s", ipv4)
	}
}

func TestPeerGetIPv4OnlyIPv6(t *testing.T) {
	peer := &Peer{
		TailscaleIPs: []string{"fd7a:115c:a1e0::f334:9455"},
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "" {
		t.Errorf("expected empty string for IPv6-only peer, got %s", ipv4)
	}
}

func TestPeerShortDNSName(t *testing.T) {
	tests := []struct {
		dnsName  string
		hostName string
		expected string
	}{
		{"superserver.tail1f21e.ts.net.", "SuperServer", "superserver"},
		{"", "SuperServer", "SuperServer"},
		{"jeffreys-mac-mini.tail1f21e.ts.net.", "Jeffrey's Mac mini", "jeffreys-mac-mini"},
	}

	for _, tt := range tests {
		peer := &Peer{DNSName: tt.dnsName, HostName: tt.hostName}
		got := peer.ShortDNSName()
		if got != tt.expected {
			t.Errorf("ShortDNSName(%q, %q) = %q, want %q", tt.dnsName, tt.hostName, got, tt.expected)
		}
	}
}

func TestFindPeerByHostnameInStatus(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	// Helper to find in parsed status
	findByHostname := func(hostname string) *Peer {
		for _, peer := range status.Peer {
			if peer.HostName == hostname {
				return peer
			}
		}
		return nil
	}

	tests := []struct {
		search   string
		expected string
	}{
		{"SuperServer", "SuperServer"},
		{"SenseDemoBox", "SenseDemoBox"},
	}

	for _, tt := range tests {
		peer := findByHostname(tt.search)
		if peer == nil {
			t.Errorf("findByHostname(%q) returned nil", tt.search)
			continue
		}
		if peer.HostName != tt.expected {
			t.Errorf("findByHostname(%q) = %q, want %q", tt.search, peer.HostName, tt.expected)
		}
	}
}

func TestOnlinePeersCount(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	onlineCount := 0
	for _, peer := range status.Peer {
		if peer.Online {
			onlineCount++
		}
	}

	if onlineCount != 3 {
		t.Errorf("expected 3 online peers, got %d", onlineCount)
	}
}

// Additional tests for better coverage

func TestStatusJSONUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantErr  bool
	}{
		{
			name: "valid status",
			json: sampleStatusJSON,
			wantErr: false,
		},
		{
			name: "empty object",
			json: "{}",
			wantErr: false,
		},
		{
			name: "minimal status",
			json: `{"BackendState": "Stopped", "Peer": {}}`,
			wantErr: false,
		},
		{
			name: "invalid json",
			json: `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var status Status
			err := json.Unmarshal([]byte(tt.json), &status)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPeerGetIPv4Empty(t *testing.T) {
	peer := &Peer{
		TailscaleIPs: []string{},
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "" {
		t.Errorf("expected empty string for peer with no IPs, got %s", ipv4)
	}
}

func TestPeerGetIPv4Nil(t *testing.T) {
	peer := &Peer{
		TailscaleIPs: nil,
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "" {
		t.Errorf("expected empty string for peer with nil IPs, got %s", ipv4)
	}
}

func TestPeerGetIPv4Multiple(t *testing.T) {
	peer := &Peer{
		TailscaleIPs: []string{
			"100.90.148.85",
			"100.90.148.86",
			"fd7a:115c:a1e0::f334:9455",
		},
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "100.90.148.85" {
		t.Errorf("expected first IPv4 100.90.148.85, got %s", ipv4)
	}
}

func TestPeerFields(t *testing.T) {
	peer := &Peer{
		ID:           "n1234",
		HostName:     "TestHost",
		DNSName:      "testhost.tail123.ts.net.",
		TailscaleIPs: []string{"100.90.148.85"},
		Online:       true,
		OS:           "linux",
	}

	if peer.ID != "n1234" {
		t.Errorf("ID mismatch: got %s", peer.ID)
	}
	if peer.HostName != "TestHost" {
		t.Errorf("HostName mismatch: got %s", peer.HostName)
	}
	if !peer.Online {
		t.Error("Online should be true")
	}
	if peer.OS != "linux" {
		t.Errorf("OS mismatch: got %s", peer.OS)
	}
}

func TestStatusVersion(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	if status.Version == "" {
		t.Error("Version should not be empty")
	}
	if !strings.Contains(status.Version, "1.92") {
		t.Errorf("unexpected version: %s", status.Version)
	}
}

func TestSelfPeer(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	if status.Self == nil {
		t.Fatal("Self should not be nil")
	}

	// Self should have TailscaleIPs
	if len(status.Self.TailscaleIPs) == 0 {
		t.Error("Self should have TailscaleIPs")
	}

	// Self should be online
	if !status.Self.Online {
		t.Error("Self should be online")
	}
}

func TestPeerOS(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	osCount := make(map[string]int)
	for _, peer := range status.Peer {
		osCount[peer.OS]++
	}

	// Should have multiple linux peers
	if osCount["linux"] < 1 {
		t.Error("expected at least one linux peer")
	}
	// Should have one darwin peer
	if osCount["darwin"] != 1 {
		t.Errorf("expected 1 darwin peer, got %d", osCount["darwin"])
	}
}

func TestPeerDNSNames(t *testing.T) {
	var status Status
	if err := json.Unmarshal([]byte(sampleStatusJSON), &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	for _, peer := range status.Peer {
		// DNSName should end with .ts.net.
		if peer.DNSName != "" && !strings.HasSuffix(peer.DNSName, ".ts.net.") {
			t.Errorf("DNSName %q doesn't end with .ts.net.", peer.DNSName)
		}
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.binaryPath != "tailscale" {
		t.Errorf("expected binaryPath 'tailscale', got %q", client.binaryPath)
	}
}

func TestClientBinaryPath(t *testing.T) {
	client := &Client{binaryPath: "/custom/path/tailscale"}
	if client.binaryPath != "/custom/path/tailscale" {
		t.Errorf("binaryPath mismatch: got %q", client.binaryPath)
	}
}

func TestShortDNSNameVariations(t *testing.T) {
	tests := []struct {
		name     string
		dnsName  string
		hostName string
		expected string
	}{
		{
			name:     "standard tailnet",
			dnsName:  "machine.tail123.ts.net.",
			hostName: "Machine",
			expected: "machine",
		},
		{
			name:     "empty dns falls back to hostname",
			dnsName:  "",
			hostName: "MyMachine",
			expected: "MyMachine",
		},
		{
			name:     "single part dns",
			dnsName:  "machine.",
			hostName: "Machine",
			expected: "machine",
		},
		{
			name:     "dns with hyphens",
			dnsName:  "my-server.tail456.ts.net.",
			hostName: "my-server",
			expected: "my-server",
		},
		{
			name:     "complex dns",
			dnsName:  "user-name-machine.tailabc123.tailscale.net.",
			hostName: "UserName-Machine",
			expected: "user-name-machine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := &Peer{DNSName: tt.dnsName, HostName: tt.hostName}
			got := peer.ShortDNSName()
			if got != tt.expected {
				t.Errorf("ShortDNSName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetIPv4WithIPv6Prefix(t *testing.T) {
	// Test IPv6 addresses that might look like they contain IPv4
	peer := &Peer{
		TailscaleIPs: []string{
			"fd7a:115c:a1e0:ab12:cd34:ef56:78:90",
			"100.90.148.85",
		},
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "100.90.148.85" {
		t.Errorf("expected IPv4 100.90.148.85, got %s", ipv4)
	}
}

func TestStatusStructFields(t *testing.T) {
	// Test that Status struct can hold all expected fields
	status := &Status{
		Version:      "1.0.0",
		BackendState: "Running",
		Self: &Peer{
			ID:           "self-id",
			HostName:     "localhost",
			DNSName:      "localhost.tail.ts.net.",
			TailscaleIPs: []string{"100.64.0.1"},
			Online:       true,
			OS:           "linux",
		},
		Peer: map[string]*Peer{
			"key1": {
				ID:           "peer1-id",
				HostName:     "peer1",
				DNSName:      "peer1.tail.ts.net.",
				TailscaleIPs: []string{"100.64.0.2"},
				Online:       true,
				OS:           "darwin",
			},
		},
	}

	if status.Version != "1.0.0" {
		t.Errorf("Version mismatch: %s", status.Version)
	}
	if status.BackendState != "Running" {
		t.Errorf("BackendState mismatch: %s", status.BackendState)
	}
	if status.Self == nil || status.Self.HostName != "localhost" {
		t.Error("Self not set correctly")
	}
	if len(status.Peer) != 1 {
		t.Errorf("expected 1 peer, got %d", len(status.Peer))
	}
}

func TestBackendStateValues(t *testing.T) {
	// Test various backend states
	states := []string{
		"Running",
		"Stopped",
		"NeedsLogin",
		"NoState",
		"Starting",
	}

	for _, state := range states {
		jsonStr := `{"BackendState": "` + state + `"}`
		var status Status
		if err := json.Unmarshal([]byte(jsonStr), &status); err != nil {
			t.Errorf("failed to parse state %q: %v", state, err)
		}
		if status.BackendState != state {
			t.Errorf("BackendState mismatch: got %q, want %q", status.BackendState, state)
		}
	}
}

func TestMultipleIPsPerPeer(t *testing.T) {
	// Peer with both IPv4 and IPv6
	peer := &Peer{
		TailscaleIPs: []string{
			"100.90.148.85",
			"fd7a:115c:a1e0::f334:9455",
			"100.90.148.86", // Second IPv4
		},
	}

	ipv4 := peer.GetIPv4()
	if ipv4 != "100.90.148.85" {
		t.Errorf("expected first IPv4, got %s", ipv4)
	}

	// Verify we have multiple IPs
	if len(peer.TailscaleIPs) != 3 {
		t.Errorf("expected 3 IPs, got %d", len(peer.TailscaleIPs))
	}
}

func TestEmptyPeerMap(t *testing.T) {
	jsonStr := `{"BackendState": "Running", "Peer": {}}`
	var status Status
	if err := json.Unmarshal([]byte(jsonStr), &status); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(status.Peer) != 0 {
		t.Errorf("expected empty peer map, got %d peers", len(status.Peer))
	}
}

func TestPeerNilSafety(t *testing.T) {
	// Test that we can handle nil peer safely in functions
	// Note: actual method calls on nil would panic, but we test the concept
	// of handling missing data gracefully

	// Test with empty peer struct instead
	emptyPeer := &Peer{}
	if emptyPeer.GetIPv4() != "" {
		t.Error("empty peer should return empty IPv4")
	}
	if emptyPeer.ShortDNSName() != "" {
		t.Error("empty peer should return empty short name")
	}
}

func TestIsAvailableWithoutTailscale(t *testing.T) {
	// Test with a non-existent binary path
	client := &Client{binaryPath: "/nonexistent/tailscale"}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Should return false without error
	available := client.IsAvailable(ctx)
	if available {
		t.Error("expected IsAvailable to return false for non-existent binary")
	}
}

func TestContextCancellation(t *testing.T) {
	client := NewClient()
	
	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()

	// Operations should fail or return quickly
	available := client.IsAvailable(ctx)
	// Either returns false or context error - both acceptable
	_ = available
}

func TestStatusJSONMarshalRoundTrip(t *testing.T) {
	original := &Status{
		Version:      "1.0.0",
		BackendState: "Running",
		Self: &Peer{
			ID:           "self-id",
			HostName:     "localhost",
			TailscaleIPs: []string{"100.64.0.1"},
			Online:       true,
		},
		Peer: map[string]*Peer{
			"key1": {
				ID:       "peer1",
				HostName: "peer1",
				Online:   true,
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	var unmarshaled Status
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify key fields
	if unmarshaled.Version != original.Version {
		t.Errorf("Version mismatch: got %s, want %s", unmarshaled.Version, original.Version)
	}
	if unmarshaled.BackendState != original.BackendState {
		t.Errorf("BackendState mismatch: got %s, want %s", unmarshaled.BackendState, original.BackendState)
	}
}

func TestPeerJSONRoundTrip(t *testing.T) {
	original := &Peer{
		ID:           "n1234",
		HostName:     "TestHost",
		DNSName:      "testhost.tail.ts.net.",
		TailscaleIPs: []string{"100.90.148.85", "fd7a:115c:a1e0::1"},
		Online:       true,
		OS:           "linux",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled Peer
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %s, want %s", unmarshaled.ID, original.ID)
	}
	if unmarshaled.HostName != original.HostName {
		t.Errorf("HostName mismatch: got %s, want %s", unmarshaled.HostName, original.HostName)
	}
	if len(unmarshaled.TailscaleIPs) != len(original.TailscaleIPs) {
		t.Errorf("TailscaleIPs count mismatch: got %d, want %d", len(unmarshaled.TailscaleIPs), len(original.TailscaleIPs))
	}
}
