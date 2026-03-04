package refresh

import (
	"testing"
)

// =============================================================================
// validateTokenEndpoint Tests
// =============================================================================

func TestValidateTokenEndpoint_EmptyURL(t *testing.T) {
	err := validateTokenEndpoint("", nil)
	if err == nil {
		t.Error("validateTokenEndpoint('') should error")
	}
}

func TestValidateTokenEndpoint_WhitespaceOnly(t *testing.T) {
	err := validateTokenEndpoint("   ", nil)
	if err == nil {
		t.Error("validateTokenEndpoint('   ') should error")
	}
}

func TestValidateTokenEndpoint_InvalidURL(t *testing.T) {
	err := validateTokenEndpoint("://invalid", nil)
	if err == nil {
		t.Error("validateTokenEndpoint with invalid URL should error")
	}
}

func TestValidateTokenEndpoint_HTTPSAllowed(t *testing.T) {
	allowHosts := []string{"oauth.example.com"}
	err := validateTokenEndpoint("https://oauth.example.com/token", allowHosts)
	if err != nil {
		t.Errorf("validateTokenEndpoint(https) error: %v", err)
	}
}

func TestValidateTokenEndpoint_HTTPNotAllowed(t *testing.T) {
	allowHosts := []string{"oauth.example.com"}
	err := validateTokenEndpoint("http://oauth.example.com/token", allowHosts)
	if err == nil {
		t.Error("validateTokenEndpoint(http) should error for non-loopback")
	}
}

func TestValidateTokenEndpoint_HTTPLoopbackAllowed(t *testing.T) {
	// HTTP is allowed for localhost
	err := validateTokenEndpoint("http://localhost:8080/token", nil)
	if err != nil {
		t.Errorf("validateTokenEndpoint(http://localhost) error: %v", err)
	}
}

func TestValidateTokenEndpoint_HTTPLoopbackIPAllowed(t *testing.T) {
	// HTTP is allowed for 127.0.0.1
	err := validateTokenEndpoint("http://127.0.0.1:8080/token", nil)
	if err != nil {
		t.Errorf("validateTokenEndpoint(http://127.0.0.1) error: %v", err)
	}
}

func TestValidateTokenEndpoint_HTTPSLoopbackAllowed(t *testing.T) {
	// HTTPS is also allowed for localhost
	err := validateTokenEndpoint("https://localhost:8443/token", nil)
	if err != nil {
		t.Errorf("validateTokenEndpoint(https://localhost) error: %v", err)
	}
}

func TestValidateTokenEndpoint_HostNotAllowlisted(t *testing.T) {
	allowHosts := []string{"oauth.example.com"}
	err := validateTokenEndpoint("https://evil.example.org/token", allowHosts)
	if err == nil {
		t.Error("validateTokenEndpoint should error for non-allowlisted host")
	}
}

func TestValidateTokenEndpoint_SubdomainAllowed(t *testing.T) {
	allowHosts := []string{"example.com"}
	// Subdomains should be allowed when parent is allowlisted
	err := validateTokenEndpoint("https://api.example.com/token", allowHosts)
	if err != nil {
		t.Errorf("validateTokenEndpoint(subdomain) error: %v", err)
	}
}

func TestValidateTokenEndpoint_ExactMatchAllowed(t *testing.T) {
	allowHosts := []string{"oauth.example.com"}
	err := validateTokenEndpoint("https://oauth.example.com/oauth/token", allowHosts)
	if err != nil {
		t.Errorf("validateTokenEndpoint(exact match) error: %v", err)
	}
}

func TestValidateTokenEndpoint_MissingHost(t *testing.T) {
	err := validateTokenEndpoint("file:///etc/passwd", nil)
	if err == nil {
		t.Error("validateTokenEndpoint should error for missing host")
	}
}

func TestValidateTokenEndpoint_EmptyAllowList(t *testing.T) {
	// With empty allow list, only loopback should work
	err := validateTokenEndpoint("https://example.com/token", []string{})
	if err == nil {
		t.Error("validateTokenEndpoint should error with empty allow list for non-loopback")
	}
}

func TestValidateTokenEndpoint_AllowListWithEmptyStrings(t *testing.T) {
	// Empty strings in allow list should be skipped
	allowHosts := []string{"", "  ", "example.com"}
	err := validateTokenEndpoint("https://example.com/token", allowHosts)
	if err != nil {
		t.Errorf("validateTokenEndpoint should work with empty strings in allow list: %v", err)
	}
}

func TestValidateTokenEndpoint_CaseInsensitive(t *testing.T) {
	allowHosts := []string{"EXAMPLE.COM"}
	err := validateTokenEndpoint("https://example.com/token", allowHosts)
	if err != nil {
		t.Errorf("validateTokenEndpoint should be case-insensitive: %v", err)
	}
}

func TestValidateTokenEndpoint_IPv6Loopback(t *testing.T) {
	// IPv6 loopback ::1
	err := validateTokenEndpoint("http://[::1]:8080/token", nil)
	if err != nil {
		t.Errorf("validateTokenEndpoint(http://[::1]) error: %v", err)
	}
}

// =============================================================================
// isLoopbackHost Tests
// =============================================================================

func TestIsLoopbackHost_Localhost(t *testing.T) {
	if !isLoopbackHost("localhost") {
		t.Error("isLoopbackHost('localhost') = false, want true")
	}
}

func TestIsLoopbackHost_127_0_0_1(t *testing.T) {
	if !isLoopbackHost("127.0.0.1") {
		t.Error("isLoopbackHost('127.0.0.1') = false, want true")
	}
}

func TestIsLoopbackHost_127_x_x_x(t *testing.T) {
	if !isLoopbackHost("127.0.0.254") {
		t.Error("isLoopbackHost('127.0.0.254') = false, want true")
	}
	if !isLoopbackHost("127.1.2.3") {
		t.Error("isLoopbackHost('127.1.2.3') = false, want true")
	}
}

func TestIsLoopbackHost_IPv6Loopback(t *testing.T) {
	if !isLoopbackHost("::1") {
		t.Error("isLoopbackHost('::1') = false, want true")
	}
}

func TestIsLoopbackHost_NonLoopback(t *testing.T) {
	tests := []string{
		"192.168.1.1",
		"10.0.0.1",
		"8.8.8.8",
		"example.com",
		"localhost.localdomain",
		"my-localhost",
	}

	for _, host := range tests {
		if isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = true, want false", host)
		}
	}
}

func TestIsLoopbackHost_EmptyString(t *testing.T) {
	if isLoopbackHost("") {
		t.Error("isLoopbackHost('') = true, want false")
	}
}

func TestIsLoopbackHost_InvalidIP(t *testing.T) {
	if isLoopbackHost("not.an.ip.address") {
		t.Error("isLoopbackHost('not.an.ip.address') = true, want false")
	}
}
