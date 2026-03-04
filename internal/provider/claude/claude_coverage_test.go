package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
)

func newClaudeCoverageProfile(base string, mode provider.AuthMode) *profile.Profile {
	return &profile.Profile{
		Name:     "coverage",
		Provider: "claude",
		AuthMode: string(mode),
		BasePath: base,
	}
}

func TestClaudeLoginDispatchAndModes(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()

	t.Run("oauth mode reaches command path", func(t *testing.T) {
		prof := newClaudeCoverageProfile(t.TempDir(), provider.AuthModeOAuth)
		if err := p.Login(ctx, prof); err == nil {
			t.Fatal("expected oauth login command error for canceled context")
		}
	})

	t.Run("api key mode returns guidance", func(t *testing.T) {
		prof := newClaudeCoverageProfile(t.TempDir(), provider.AuthModeAPIKey)
		if err := p.Login(context.Background(), prof); err != nil {
			t.Fatalf("api-key Login() error = %v", err)
		}
	})
}

func TestClaudeSettingsHasAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if _, err := claudeSettingsHasAPIKey(path); err == nil {
		t.Fatal("expected error for missing settings.json")
	}

	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("Write invalid settings.json: %v", err)
	}
	if _, err := claudeSettingsHasAPIKey(path); err == nil {
		t.Fatal("expected JSON parse error")
	}

	for _, field := range []string{"apiKeyHelper", "apiKey", "api_key"} {
		if err := os.WriteFile(path, []byte(`{"`+field+`":"value"}`), 0o600); err != nil {
			t.Fatalf("Write settings.json: %v", err)
		}
		ok, err := claudeSettingsHasAPIKey(path)
		if err != nil {
			t.Fatalf("claudeSettingsHasAPIKey() error = %v", err)
		}
		if !ok {
			t.Fatalf("expected API key detection for field %q", field)
		}
	}
}

func TestClaudeValidateTokenBranches(t *testing.T) {
	origNow := timeNow
	fixedNow := time.Date(2026, 3, 2, 20, 0, 0, 0, time.UTC)
	timeNow = func() time.Time { return fixedNow }
	defer func() { timeNow = origNow }()

	tests := []struct {
		name              string
		mode              provider.AuthMode
		credentialsJSON   string
		claudeJSON        string
		authJSON          string
		settingsJSON      string
		passive           bool
		wantValid         bool
		wantMethod        string
		wantErrSub        string
		skipCreateClaude  bool
		skipCreateXDGAuth bool
	}{
		{
			name:       "oauth no auth files",
			mode:       provider.AuthModeOAuth,
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "no auth files found",
		},
		{
			name:            "api key mode with valid settings only",
			mode:            provider.AuthModeAPIKey,
			settingsJSON:    `{"apiKeyHelper":"/tmp/helper.sh"}`,
			passive:         true,
			wantValid:       true,
			wantMethod:      "passive",
			skipCreateClaude: false,
		},
		{
			name:       "api key mode invalid settings json",
			mode:       provider.AuthModeAPIKey,
			settingsJSON: "{",
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "invalid settings.json",
		},
		{
			name:       "api key mode settings missing key field",
			mode:       provider.AuthModeAPIKey,
			settingsJSON: `{"theme":"dark"}`,
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "no API key settings found",
		},
		{
			name:            "invalid credentials json",
			mode:            provider.AuthModeOAuth,
			credentialsJSON: "{",
			passive:         true,
			wantValid:       false,
			wantMethod:      "passive",
			wantErrSub:      "invalid .credentials.json",
		},
		{
			name:            "expired credentials",
			mode:            provider.AuthModeOAuth,
			credentialsJSON: `{"claudeAiOauth":{"accessToken":"tok","expiresAt":946684800000}}`,
			passive:         true,
			wantValid:       false,
			wantMethod:      "passive",
			wantErrSub:      "expired",
		},
		{
			name:       "invalid .claude.json",
			mode:       provider.AuthModeOAuth,
			claudeJSON: "{",
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "invalid JSON in .claude.json",
		},
		{
			name:       "invalid auth.json",
			mode:       provider.AuthModeOAuth,
			authJSON:   "{",
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "invalid JSON in auth.json",
		},
		{
			name:       "expired from .claude.json unix millis",
			mode:       provider.AuthModeOAuth,
			claudeJSON: `{"oauthToken":"tok","expiresAt":946684800000}`,
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "expired",
		},
		{
			name:       "valid oauth passive",
			mode:       provider.AuthModeOAuth,
			claudeJSON: `{"oauthToken":"tok","expiresAt":"2099-01-01T00:00:00Z"}`,
			authJSON:   `{"access_token":"tok","expires_at":"2099-01-01T00:00:00Z"}`,
			passive:    true,
			wantValid:  true,
			wantMethod: "passive",
		},
		{
			name:       "valid oauth active",
			mode:       provider.AuthModeOAuth,
			claudeJSON: `{"oauthToken":"tok","expiresAt":"2099-01-01T00:00:00Z"}`,
			passive:    false,
			wantValid:  true,
			wantMethod: "active",
		},
		{
			name:       "valid api key active",
			mode:       provider.AuthModeAPIKey,
			settingsJSON: `{"api_key":"abc"}`,
			passive:    false,
			wantValid:  true,
			wantMethod: "active",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			prof := newClaudeCoverageProfile(base, tc.mode)
			p := New()

			claudeDir := filepath.Join(prof.HomePath(), ".claude")
			if !tc.skipCreateClaude {
				if err := os.MkdirAll(claudeDir, 0o700); err != nil {
					t.Fatalf("MkdirAll .claude: %v", err)
				}
			}
			xdgDir := filepath.Join(prof.XDGConfigPath(), "claude-code")
			if !tc.skipCreateXDGAuth {
				if err := os.MkdirAll(xdgDir, 0o700); err != nil {
					t.Fatalf("MkdirAll xdg/claude-code: %v", err)
				}
			}

			if tc.credentialsJSON != "" {
				if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(tc.credentialsJSON), 0o600); err != nil {
					t.Fatalf("Write .credentials.json: %v", err)
				}
			}
			if tc.claudeJSON != "" {
				if err := os.WriteFile(filepath.Join(prof.HomePath(), ".claude.json"), []byte(tc.claudeJSON), 0o600); err != nil {
					t.Fatalf("Write .claude.json: %v", err)
				}
			}
			if tc.authJSON != "" {
				if err := os.WriteFile(filepath.Join(xdgDir, "auth.json"), []byte(tc.authJSON), 0o600); err != nil {
					t.Fatalf("Write auth.json: %v", err)
				}
			}
			if tc.settingsJSON != "" {
				if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(tc.settingsJSON), 0o600); err != nil {
					t.Fatalf("Write settings.json: %v", err)
				}
			}

			result, err := p.ValidateToken(context.Background(), prof, tc.passive)
			if err != nil {
				t.Fatalf("ValidateToken() error = %v", err)
			}
			if result.Valid != tc.wantValid {
				t.Fatalf("ValidateToken().Valid = %v, want %v (error=%q)", result.Valid, tc.wantValid, result.Error)
			}
			if result.Method != tc.wantMethod {
				t.Fatalf("ValidateToken().Method = %q, want %q", result.Method, tc.wantMethod)
			}
			if tc.wantErrSub != "" && !strings.Contains(result.Error, tc.wantErrSub) {
				t.Fatalf("ValidateToken().Error = %q, want substring %q", result.Error, tc.wantErrSub)
			}
		})
	}
}

func TestSharedFixtureCorpusClaude(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "schema_drift", "claude_credentials_valid.json"))
	if err != nil {
		t.Fatalf("Read shared fixture: %v", err)
	}

	prof := newClaudeCoverageProfile(t.TempDir(), provider.AuthModeOAuth)
	claudeDir := filepath.Join(prof.HomePath(), ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), data, 0o600); err != nil {
		t.Fatalf("Write .credentials.json: %v", err)
	}

	result, err := New().ValidateToken(context.Background(), prof, true)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("shared fixture token should validate, got error=%q", result.Error)
	}
}
