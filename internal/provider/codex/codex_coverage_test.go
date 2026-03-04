package codex

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

func newCoverageProfile(base string, mode provider.AuthMode) *profile.Profile {
	return &profile.Profile{
		Name:     "coverage",
		Provider: "codex",
		AuthMode: string(mode),
		BasePath: base,
	}
}

func TestResolveHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/codex-custom")
	if got := ResolveHome(); got != "/tmp/codex-custom" {
		t.Fatalf("ResolveHome() = %q, want %q", got, "/tmp/codex-custom")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	want := filepath.Join(home, ".codex")
	if got := ResolveHome(); got != want {
		t.Fatalf("ResolveHome() with fallback = %q, want %q", got, want)
	}
}

func TestEnsureFileCredentialStore(t *testing.T) {
	t.Run("rejects empty home", func(t *testing.T) {
		if err := EnsureFileCredentialStore("   "); err == nil {
			t.Fatal("expected error for empty codex home")
		}
	})

	t.Run("creates config when missing", func(t *testing.T) {
		home := t.TempDir()
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		if !strings.Contains(string(data), `cli_auth_credentials_store = "file"`) {
			t.Fatalf("config.toml missing file credential store:\n%s", string(data))
		}
	})

	t.Run("replaces existing non-file store", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		if err := os.WriteFile(path, []byte("cli_auth_credentials_store = \"keychain\"\n"), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		if !strings.Contains(string(data), `cli_auth_credentials_store = "file"`) {
			t.Fatalf("config.toml did not switch to file store:\n%s", string(data))
		}
	})

	t.Run("appends setting when absent", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		if err := os.WriteFile(path, []byte("[defaults]\nmodel = \"gpt-5\"\n"), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		if !strings.Contains(string(data), `cli_auth_credentials_store = "file"`) {
			t.Fatalf("config.toml missing appended setting:\n%s", string(data))
		}
	})
}

func TestLoginDispatchAndCommandFailures(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()

	t.Run("oauth mode reaches command path", func(t *testing.T) {
		prof := newCoverageProfile(t.TempDir(), provider.AuthModeOAuth)
		if err := p.Login(ctx, prof); err == nil {
			t.Fatal("expected login command error for canceled context")
		}
	})

	t.Run("device-code mode reaches command path", func(t *testing.T) {
		prof := newCoverageProfile(t.TempDir(), provider.AuthModeDeviceCode)
		if err := p.Login(ctx, prof); err == nil {
			t.Fatal("expected device code command error for canceled context")
		}
	})

	t.Run("api-key mode reaches command path", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-test-coverage")
		prof := newCoverageProfile(t.TempDir(), provider.AuthModeAPIKey)
		if err := p.Login(ctx, prof); err == nil {
			t.Fatal("expected API key login command error for canceled context")
		}
	})
}

func TestSupportsDeviceCode(t *testing.T) {
	if !New().SupportsDeviceCode() {
		t.Fatal("SupportsDeviceCode() = false, want true")
	}
}

func TestValidateTokenBranches(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name       string
		authJSON   string
		wantValid  bool
		wantMethod string
		wantErrSub string
		passive    bool
	}{
		{
			name:       "missing auth file",
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "auth.json not found",
			passive:    true,
		},
		{
			name:       "invalid JSON",
			authJSON:   "{",
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "invalid JSON",
			passive:    true,
		},
		{
			name:       "missing token fields",
			authJSON:   `{"expires_at":"` + "2099-01-01T00:00:00Z" + `"}`,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "no access token",
			passive:    true,
		},
		{
			name:       "expired token string",
			authJSON:   `{"access_token":"tok","expires_at":"2000-01-01T00:00:00Z"}`,
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "expired",
			passive:    true,
		},
		{
			name:       "expired token unix seconds",
			authJSON:   fmt.Sprintf(`{"accessToken":"tok","expires":%d}`, now.Add(-time.Hour).Unix()),
			wantValid:  false,
			wantMethod: "passive",
			wantErrSub: "expired",
			passive:    true,
		},
		{
			name:       "valid passive token",
			authJSON:   `{"access_token":"tok","expires_at":"2099-01-01T00:00:00Z"}`,
			wantValid:  true,
			wantMethod: "passive",
			passive:    true,
		},
		{
			name:       "valid active token",
			authJSON:   `{"access_token":"tok","expires_at":"2099-01-01T00:00:00Z"}`,
			wantValid:  true,
			wantMethod: "active",
			passive:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			prof := newCoverageProfile(base, provider.AuthModeOAuth)
			p := New()

			if tc.authJSON != "" {
				if err := os.MkdirAll(prof.CodexHomePath(), 0o700); err != nil {
					t.Fatalf("MkdirAll codex home: %v", err)
				}
				path := filepath.Join(prof.CodexHomePath(), "auth.json")
				if err := os.WriteFile(path, []byte(tc.authJSON), 0o600); err != nil {
					t.Fatalf("Write auth.json: %v", err)
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

func TestParseCodexTimeHelpers(t *testing.T) {
	if _, err := parseCodexExpiryTime("not-a-time"); err == nil {
		t.Fatal("expected parseCodexExpiryTime to fail for invalid input")
	}

	t1, err := parseCodexExpiryTime("2026-03-02T19:30:00Z")
	if err != nil {
		t.Fatalf("parseCodexExpiryTime() error = %v", err)
	}
	if t1.Year() != 2026 || t1.Month() != time.March {
		t.Fatalf("unexpected parsed time: %s", t1)
	}

	sec := float64(time.Now().Add(time.Hour).Unix())
	ms := float64(time.Now().Add(2 * time.Hour).UnixMilli())
	if got := parseCodexUnixTime(sec); got.Unix() != int64(sec) {
		t.Fatalf("parseCodexUnixTime(seconds) mismatch: got %d want %d", got.Unix(), int64(sec))
	}
	if got := parseCodexUnixTime(ms); got.UnixMilli() != int64(ms) {
		t.Fatalf("parseCodexUnixTime(milliseconds) mismatch: got %d want %d", got.UnixMilli(), int64(ms))
	}
}

func TestSharedFixtureCorpusCodex(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "schema_drift", "codex_auth_valid.json"))
	if err != nil {
		t.Fatalf("Read shared fixture: %v", err)
	}

	prof := newCoverageProfile(t.TempDir(), provider.AuthModeOAuth)
	if err := os.MkdirAll(prof.CodexHomePath(), 0o700); err != nil {
		t.Fatalf("MkdirAll codex home: %v", err)
	}
	authPath := filepath.Join(prof.CodexHomePath(), "auth.json")
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("Write auth.json: %v", err)
	}

	result, err := New().ValidateToken(context.Background(), prof, true)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("shared fixture token should validate, got error=%q", result.Error)
	}
}
