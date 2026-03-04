package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
)

func newGeminiCoverageProfile(base string, mode provider.AuthMode) *profile.Profile {
	return &profile.Profile{
		Name:     "coverage",
		Provider: "gemini",
		AuthMode: string(mode),
		BasePath: base,
	}
}

func TestLoginWithAPIKeyWritesEnvFile(t *testing.T) {
	base := t.TempDir()
	prof := newGeminiCoverageProfile(base, provider.AuthModeAPIKey)
	p := New()

	in := filepath.Join(base, "stdin.txt")
	if err := os.WriteFile(in, []byte("gemini-test-key\n"), 0o600); err != nil {
		t.Fatalf("Write stdin file: %v", err)
	}

	f, err := os.Open(in)
	if err != nil {
		t.Fatalf("Open stdin file: %v", err)
	}
	defer f.Close()

	origStdin := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = origStdin }()

	if err := p.loginWithAPIKey(context.Background(), prof); err != nil {
		t.Fatalf("loginWithAPIKey() error = %v", err)
	}

	envPath := filepath.Join(prof.HomePath(), ".gemini", ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("Read .env: %v", err)
	}
	if !strings.Contains(string(data), "GEMINI_API_KEY=gemini-test-key") {
		t.Fatalf(".env missing expected key, got:\n%s", string(data))
	}
}

func TestLoginWithAPIKeyEmptyInputSkipsWrite(t *testing.T) {
	base := t.TempDir()
	prof := newGeminiCoverageProfile(base, provider.AuthModeAPIKey)
	p := New()

	in := filepath.Join(base, "stdin-empty.txt")
	if err := os.WriteFile(in, []byte("\n"), 0o600); err != nil {
		t.Fatalf("Write stdin file: %v", err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatalf("Open stdin file: %v", err)
	}
	defer f.Close()

	origStdin := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = origStdin }()

	if err := p.loginWithAPIKey(context.Background(), prof); err != nil {
		t.Fatalf("loginWithAPIKey() error = %v", err)
	}

	envPath := filepath.Join(prof.HomePath(), ".gemini", ".env")
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("expected .env to be absent for empty input, stat err=%v", err)
	}
}

func TestLoginDispatchHitsExternalCommandPaths(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()

	t.Run("oauth mode", func(t *testing.T) {
		prof := newGeminiCoverageProfile(t.TempDir(), provider.AuthModeOAuth)
		if err := p.Login(ctx, prof); err == nil {
			t.Fatal("expected oauth login command error for canceled context")
		}
	})

	t.Run("vertex mode", func(t *testing.T) {
		prof := newGeminiCoverageProfile(t.TempDir(), provider.AuthModeVertexADC)
		if err := p.Login(ctx, prof); err == nil {
			t.Fatal("expected vertex login command error for canceled context")
		}
	})
}

func TestGeminiValidateTokenBranches(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name         string
		mode         provider.AuthMode
		settingsJSON string
		oauthJSON    string
		envContent   string
		envVarKey    string
		passive      bool
		wantValid    bool
		wantMethod   string
		wantErrSub   string
	}{
		{
			name:       "api key mode missing all keys",
			mode:       provider.AuthModeAPIKey,
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "no API key",
		},
		{
			name:       "api key mode env file missing gemini key",
			mode:       provider.AuthModeAPIKey,
			envContent: "FOO=bar\n",
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "GEMINI_API_KEY not found",
		},
		{
			name:       "api key mode env var key is valid",
			mode:       provider.AuthModeAPIKey,
			envVarKey:  "from-env-var",
			passive:    true,
			wantValid:  true,
			wantMethod: "passive",
		},
		{
			name:       "oauth mode no files",
			mode:       provider.AuthModeOAuth,
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "no auth files found",
		},
		{
			name:       "oauth mode invalid oauth json",
			mode:       provider.AuthModeOAuth,
			oauthJSON:  "{",
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "invalid JSON in oauth_credentials.json",
		},
		{
			name:       "oauth mode expired token",
			mode:       provider.AuthModeOAuth,
			oauthJSON:  fmt.Sprintf(`{"access_token":"tok","expires_at":%d}`, now.Add(-2*time.Hour).Unix()),
			passive:    true,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "expired",
		},
		{
			name:         "oauth mode invalid settings json",
			mode:         provider.AuthModeOAuth,
			settingsJSON: "{",
			passive:      true,
			wantValid:    false,
			wantMethod:   "passive",
			wantErrSub:   "invalid JSON in settings.json",
		},
		{
			name:         "oauth mode settings without auth",
			mode:         provider.AuthModeOAuth,
			settingsJSON: `{"theme":"dark"}`,
			passive:      true,
			wantValid:    false,
			wantMethod:   "passive",
			wantErrSub:   "no authentication configured",
		},
		{
			name:         "oauth mode valid via settings oauth",
			mode:         provider.AuthModeOAuth,
			settingsJSON: `{"oauth":{"account":"ok"}}`,
			passive:      true,
			wantValid:    true,
			wantMethod:   "passive",
		},
		{
			name:         "active mode remains valid after passive",
			mode:         provider.AuthModeOAuth,
			settingsJSON: `{"oauth":{"account":"ok"}}`,
			passive:      false,
			wantValid:    true,
			wantMethod:   "active",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			prof := newGeminiCoverageProfile(base, tc.mode)
			p := New()

			geminiDir := filepath.Join(prof.HomePath(), ".gemini")
			if err := os.MkdirAll(geminiDir, 0o700); err != nil {
				t.Fatalf("MkdirAll .gemini: %v", err)
			}

			if tc.settingsJSON != "" {
				if err := os.WriteFile(filepath.Join(geminiDir, "settings.json"), []byte(tc.settingsJSON), 0o600); err != nil {
					t.Fatalf("Write settings.json: %v", err)
				}
			}
			if tc.oauthJSON != "" {
				if err := os.WriteFile(filepath.Join(geminiDir, "oauth_credentials.json"), []byte(tc.oauthJSON), 0o600); err != nil {
					t.Fatalf("Write oauth_credentials.json: %v", err)
				}
			}
			if tc.envContent != "" {
				if err := os.WriteFile(filepath.Join(geminiDir, ".env"), []byte(tc.envContent), 0o600); err != nil {
					t.Fatalf("Write .env: %v", err)
				}
			}
			t.Setenv("GEMINI_API_KEY", tc.envVarKey)

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

func TestGeminiTimeParsers(t *testing.T) {
	if _, err := parseGeminiExpiryTime("nope"); err == nil {
		t.Fatal("expected parseGeminiExpiryTime to fail for invalid input")
	}

	tm, err := parseGeminiExpiryTime("2026-03-02T20:00:00Z")
	if err != nil {
		t.Fatalf("parseGeminiExpiryTime() error = %v", err)
	}
	if tm.Year() != 2026 {
		t.Fatalf("unexpected year parsed: %d", tm.Year())
	}

	sec := float64(1_800_000_000)
	ms := float64(1_800_000_000_000)
	if got := parseGeminiUnixTime(sec); got.Unix() != int64(sec) {
		t.Fatalf("parseGeminiUnixTime(sec) mismatch: got %d want %d", got.Unix(), int64(sec))
	}
	if got := parseGeminiUnixTime(ms); got.UnixMilli() != int64(ms) {
		t.Fatalf("parseGeminiUnixTime(ms) mismatch: got %d want %d", got.UnixMilli(), int64(ms))
	}
}

func TestSharedFixtureCorpusGemini(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "schema_drift", "gemini_oauth_valid.json"))
	if err != nil {
		t.Fatalf("Read shared fixture: %v", err)
	}

	prof := newGeminiCoverageProfile(t.TempDir(), provider.AuthModeOAuth)
	geminiDir := filepath.Join(prof.HomePath(), ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll .gemini: %v", err)
	}
	if err := os.WriteFile(filepath.Join(geminiDir, "oauth_credentials.json"), data, 0o600); err != nil {
		t.Fatalf("Write oauth_credentials.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(geminiDir, "settings.json"), []byte(`{"oauth":{"state":"fixture"}}`), 0o600); err != nil {
		t.Fatalf("Write settings.json: %v", err)
	}

	result, err := New().ValidateToken(context.Background(), prof, true)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("shared fixture token should validate, got error=%q", result.Error)
	}
}
