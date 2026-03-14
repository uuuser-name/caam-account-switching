package refresh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshProfile_Claude_Extended(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Seed Claude refresh server and vault")
	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	vault := authfile.NewVault(vaultDir)
	
	// Create Claude profile
	profileDir := filepath.Join(vaultDir, "claude", "test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	
	// Write auth file
	authPath := filepath.Join(profileDir, ".claude.json")
	// Use format that is parsed by getRefreshTokenFromJSON
	initialContent := `{"refreshToken": "old-refresh-token", "accessToken": "old-access", "expiresAt": "2020-01-01T00:00:00Z"}`
	require.NoError(t, os.WriteFile(authPath, []byte(initialContent), 0600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		assert.Equal(t, "old-refresh-token", r.Form.Get("refresh_token"))

		require.NoError(t, json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		}))
	}))
	defer server.Close()

	originalURL := ClaudeTokenURL
	ClaudeTokenURL = server.URL
	defer func() { ClaudeTokenURL = originalURL }()
	
	h.EndStep("Setup")
	
	// 2. Refresh
	h.StartStep("Refresh", "Call RefreshProfile")
	err := RefreshProfile(context.Background(), "claude", "test", vault, nil)
	require.NoError(t, err)
	h.EndStep("Refresh")
	
	// 3. Verify
	h.StartStep("Verify", "Check updated auth file")
	content, err := os.ReadFile(authPath)
	require.NoError(t, err)
	
	assert.Contains(t, string(content), "new-access-token")
	assert.Contains(t, string(content), "new-refresh-token")
	h.EndStep("Verify")
}

func TestRefreshProfile_Codex_Extended(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Seed Codex refresh server and vault")
	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	vault := authfile.NewVault(vaultDir)
	
	// Create Codex profile
	profileDir := filepath.Join(vaultDir, "codex", "test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	
	authPath := filepath.Join(profileDir, "auth.json")
	initialContent := `{"refresh_token": "old-codex-refresh", "access_token": "old-codex-access"}`
	require.NoError(t, os.WriteFile(authPath, []byte(initialContent), 0600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "refresh_token", body["grant_type"])
		assert.Equal(t, "old-codex-refresh", body["refresh_token"])
		assert.Equal(t, CodexClientID, body["client_id"])

		require.NoError(t, json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-codex-access",
			RefreshToken: "new-codex-refresh",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		}))
	}))
	defer server.Close()

	originalURL := CodexTokenURL
	CodexTokenURL = server.URL
	defer func() { CodexTokenURL = originalURL }()
	
	h.EndStep("Setup")
	
	// 2. Refresh
	h.StartStep("Refresh", "Call RefreshProfile")
	err := RefreshProfile(context.Background(), "codex", "test", vault, nil)
	require.NoError(t, err)
	h.EndStep("Refresh")
	
	// 3. Verify
	h.StartStep("Verify", "Check updated auth file")
	content, err := os.ReadFile(authPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "new-codex-access")
	assert.Contains(t, string(content), "new-codex-refresh")
	h.EndStep("Verify")
}

func TestRefreshProfile_Gemini_Extended(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Seed Gemini refresh server and vault")
	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	vault := authfile.NewVault(vaultDir)
	
	// Create Gemini profile
	profileDir := filepath.Join(vaultDir, "gemini", "test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	
	settingsPath := filepath.Join(profileDir, "settings.json")
	initialSettings := `{"accessToken": "old-gemini-access"}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(initialSettings), 0600))
	
	// Gemini needs oauth_credentials.json for client info
	credsPath := filepath.Join(profileDir, "oauth_credentials.json")
	credsContent := `{"client_id": "test-id", "client_secret": "test-secret", "refresh_token": "gemini-refresh"}`
	require.NoError(t, os.WriteFile(credsPath, []byte(credsContent), 0600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "test-id", r.Form.Get("client_id"))
		assert.Equal(t, "test-secret", r.Form.Get("client_secret"))
		assert.Equal(t, "gemini-refresh", r.Form.Get("refresh_token"))

		require.NoError(t, json.NewEncoder(w).Encode(GoogleTokenResponse{
			AccessToken: "new-gemini-access",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		}))
	}))
	defer server.Close()

	originalURL := GeminiTokenURL
	GeminiTokenURL = server.URL
	defer func() { GeminiTokenURL = originalURL }()
	
	h.EndStep("Setup")
	
	// 2. Refresh
	h.StartStep("Refresh", "Call RefreshProfile")
	err := RefreshProfile(context.Background(), "gemini", "test", vault, nil)
	require.NoError(t, err)
	h.EndStep("Refresh")
	
	// 3. Verify
	h.StartStep("Verify", "Check updated auth file")
	content, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "new-gemini-access")
	// Verify it contains updated expiry if possible (ExpiresIn was set)
	// UpdateGeminiAuth sets expires_at/expiry etc.
	h.EndStep("Verify")
}
