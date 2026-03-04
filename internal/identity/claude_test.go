package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractFromClaudeCredentials_AllFields(t *testing.T) {
	exp := time.Now().Add(90 * time.Minute).UTC()
	cred := map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accountId":        "acc-123",
			"subscriptionType": "max",
			"email":            "claude@example.com",
			"expiresAt":        exp.Unix() * 1000, // milliseconds
		},
	}
	path := writeClaudeFile(t, cred)

	identity, err := ExtractFromClaudeCredentials(path)
	if err != nil {
		t.Fatalf("ExtractFromClaudeCredentials error: %v", err)
	}
	if identity.AccountID != "acc-123" {
		t.Errorf("AccountID = %q, want %q", identity.AccountID, "acc-123")
	}
	if identity.PlanType != "max" {
		t.Errorf("PlanType = %q, want %q", identity.PlanType, "max")
	}
	if identity.Email != "claude@example.com" {
		t.Errorf("Email = %q, want %q", identity.Email, "claude@example.com")
	}
	if identity.ExpiresAt.Unix() != exp.Unix() {
		t.Errorf("ExpiresAt = %v, want unix %d", identity.ExpiresAt, exp.Unix())
	}
	if identity.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", identity.Provider, "claude")
	}
}

func TestExtractFromClaudeCredentials_Minimal(t *testing.T) {
	cred := map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accountId": "acc-min",
		},
	}
	path := writeClaudeFile(t, cred)

	identity, err := ExtractFromClaudeCredentials(path)
	if err != nil {
		t.Fatalf("ExtractFromClaudeCredentials error: %v", err)
	}
	if identity.AccountID != "acc-min" {
		t.Errorf("AccountID = %q, want %q", identity.AccountID, "acc-min")
	}
	if identity.PlanType != "" || identity.Email != "" {
		t.Errorf("Expected empty PlanType/Email, got %+v", identity)
	}
}

func TestExtractFromClaudeCredentials_MissingObject(t *testing.T) {
	cred := map[string]interface{}{
		"unrelated": "value",
	}
	path := writeClaudeFile(t, cred)

	identity, err := ExtractFromClaudeCredentials(path)
	if err != nil {
		t.Fatalf("ExtractFromClaudeCredentials error: %v", err)
	}
	if identity.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", identity.Provider, "claude")
	}
	if identity.AccountID != "" || identity.PlanType != "" || identity.Email != "" {
		t.Errorf("Expected empty identity fields, got %+v", identity)
	}
}

func TestExtractFromClaudeCredentials_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write credentials.json: %v", err)
	}

	if _, err := ExtractFromClaudeCredentials(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractFromClaudeCredentials_MissingFile(t *testing.T) {
	if _, err := ExtractFromClaudeCredentials("/nonexistent/claude.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func writeClaudeFile(t *testing.T, content map[string]interface{}) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write credentials.json: %v", err)
	}
	return path
}
