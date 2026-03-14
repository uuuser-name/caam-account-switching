package refresh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
)

func TestRefreshProfile_Codex_RestoresUpdatedActiveFiles(t *testing.T) {
	rootDir := t.TempDir()
	codexHome := filepath.Join(rootDir, ".codex")
	vaultDir := filepath.Join(rootDir, "vault")
	liveAuthPath := filepath.Join(codexHome, "auth.json")
	vaultAuthPath := filepath.Join(vaultDir, "codex", "work", "auth.json")
	initialContent := `{"refresh_token":"old-refresh","access_token":"old-access"}`

	t.Setenv("HOME", rootDir)
	t.Setenv("CODEX_HOME", codexHome)

	for _, path := range []string{codexHome, filepath.Dir(vaultAuthPath)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}
	if err := os.WriteFile(liveAuthPath, []byte(initialContent), 0600); err != nil {
		t.Fatalf("WriteFile(live) error = %v", err)
	}
	if err := os.WriteFile(vaultAuthPath, []byte(initialContent), 0600); err != nil {
		t.Fatalf("WriteFile(vault) error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if got := body["refresh_token"]; got != "old-refresh" {
			t.Fatalf("refresh_token = %q, want %q", got, "old-refresh")
		}
		if err := json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer server.Close()

	originalURL := CodexTokenURL
	CodexTokenURL = server.URL
	defer func() { CodexTokenURL = originalURL }()

	vault := authfile.NewVault(vaultDir)
	if err := RefreshProfile(context.Background(), "codex", "work", vault, nil); err != nil {
		t.Fatalf("RefreshProfile() error = %v", err)
	}

	liveData, err := os.ReadFile(liveAuthPath)
	if err != nil {
		t.Fatalf("ReadFile(live) error = %v", err)
	}
	if !strings.Contains(string(liveData), "new-access") {
		t.Fatalf("live auth not restored with new access token: %s", string(liveData))
	}
	if !strings.Contains(string(liveData), "new-refresh") {
		t.Fatalf("live auth not restored with new refresh token: %s", string(liveData))
	}
}

func TestRefreshProfile_Codex_DoesNotOverwriteChangedLiveFiles(t *testing.T) {
	rootDir := t.TempDir()
	codexHome := filepath.Join(rootDir, ".codex")
	vaultDir := filepath.Join(rootDir, "vault")
	liveAuthPath := filepath.Join(codexHome, "auth.json")
	vaultAuthPath := filepath.Join(vaultDir, "codex", "work", "auth.json")
	initialContent := `{"refresh_token":"old-refresh","access_token":"old-access"}`
	changedLiveContent := `{"refresh_token":"external-refresh","access_token":"external-access"}`

	t.Setenv("HOME", rootDir)
	t.Setenv("CODEX_HOME", codexHome)

	for _, path := range []string{codexHome, filepath.Dir(vaultAuthPath)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}
	if err := os.WriteFile(liveAuthPath, []byte(initialContent), 0600); err != nil {
		t.Fatalf("WriteFile(live) error = %v", err)
	}
	if err := os.WriteFile(vaultAuthPath, []byte(initialContent), 0600); err != nil {
		t.Fatalf("WriteFile(vault) error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(liveAuthPath, []byte(changedLiveContent), 0600); err != nil {
			t.Fatalf("WriteFile(live mutated) error = %v", err)
		}
		if err := json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer server.Close()

	originalURL := CodexTokenURL
	CodexTokenURL = server.URL
	defer func() { CodexTokenURL = originalURL }()

	vault := authfile.NewVault(vaultDir)
	if err := RefreshProfile(context.Background(), "codex", "work", vault, nil); err != nil {
		t.Fatalf("RefreshProfile() error = %v", err)
	}

	liveData, err := os.ReadFile(liveAuthPath)
	if err != nil {
		t.Fatalf("ReadFile(live) error = %v", err)
	}
	if string(liveData) != changedLiveContent {
		t.Fatalf("live auth was unexpectedly overwritten: %s", string(liveData))
	}

	vaultData, err := os.ReadFile(vaultAuthPath)
	if err != nil {
		t.Fatalf("ReadFile(vault) error = %v", err)
	}
	if !strings.Contains(string(vaultData), "new-access") {
		t.Fatalf("vault auth not updated: %s", string(vaultData))
	}
}
