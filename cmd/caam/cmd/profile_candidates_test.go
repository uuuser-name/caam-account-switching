package cmd

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
)

func TestFilterEligibleRotationProfiles_RemovesSystemAndUnusable(t *testing.T) {
	vaultDir := setupCandidateFilterTestEnv(t)

	writeCodexProfileAuthFixture(t, vaultDir, "_system", "system@example.com", "acc-system")
	writeCodexProfileAuthFixture(t, vaultDir, "good", "good@example.com", "acc-good")
	writeCodexProfileAuthFixture(t, vaultDir, "wrong-name", "actual@example.com", "acc-mismatch")

	got := filterEligibleRotationProfiles("codex", []string{"_system", "good", "wrong-name"}, "")
	want := []string{"good"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterEligibleRotationProfiles() = %#v, want %#v", got, want)
	}
}

func TestFilterEligibleRotationProfiles_DedupesIdentityAliases(t *testing.T) {
	vaultDir := setupCandidateFilterTestEnv(t)

	writeCodexProfileAuthFixture(t, vaultDir, "alpha.backup", "alpha@example.com", "acc-shared")
	writeCodexProfileAuthFixture(t, vaultDir, "alpha", "alpha@example.com", "acc-shared")

	got := filterEligibleRotationProfiles("codex", []string{"alpha.backup", "alpha"}, "")
	want := []string{"alpha"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterEligibleRotationProfiles() = %#v, want %#v", got, want)
	}
}

func TestFilterEligibleRotationProfiles_PrefersCurrentAliasWhenDuplicated(t *testing.T) {
	vaultDir := setupCandidateFilterTestEnv(t)

	writeCodexProfileAuthFixture(t, vaultDir, "alpha", "alpha@example.com", "acc-shared")
	writeCodexProfileAuthFixture(t, vaultDir, "alpha.backup", "alpha@example.com", "acc-shared")

	got := filterEligibleRotationProfiles("codex", []string{"alpha", "alpha.backup"}, "alpha.backup")
	want := []string{"alpha.backup"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterEligibleRotationProfiles() = %#v, want %#v", got, want)
	}
}

func setupCandidateFilterTestEnv(t *testing.T) string {
	t.Helper()

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}

	vaultDir := t.TempDir()
	vault = authfile.NewVault(vaultDir)

	toolsCopy := make(map[string]func() authfile.AuthFileSet, len(originalTools))
	for k, v := range originalTools {
		toolsCopy[k] = v
	}
	toolsCopy["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "codex",
			Files: []authfile.AuthFileSpec{
				{Path: filepath.Join("/tmp", "codex", "auth.json"), Required: true},
			},
		}
	}
	tools = toolsCopy

	t.Cleanup(func() {
		vault = originalVault
		tools = originalTools
	})

	return vaultDir
}

func writeCodexProfileAuthFixture(t *testing.T, vaultDir, profileName, email, accountID string) {
	t.Helper()

	profileDir := filepath.Join(vaultDir, "codex", profileName)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}

	claims := map[string]any{
		"email": email,
	}
	if accountID != "" {
		claims["account_id"] = accountID
	}
	idToken := buildTestJWT(t, claims)

	auth := map[string]any{
		"tokens": map[string]any{
			"id_token":     idToken,
			"access_token": "token-" + profileName,
			"account_id":   accountID,
		},
	}

	data, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
}

func buildTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()

	headerJSON, err := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal jwt header: %v", err)
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal jwt payload: %v", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return headerB64 + "." + payloadB64 + ".sig"
}
