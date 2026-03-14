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
	assertManagedDefaults := func(t *testing.T, data []byte) {
		t.Helper()
		text := string(data)
		if !strings.Contains(text, `cli_auth_credentials_store = "file"`) {
			t.Fatalf("config.toml missing file credential store:\n%s", text)
		}
		if !strings.Contains(text, "[features]") {
			t.Fatalf("config.toml missing [features] table:\n%s", text)
		}
		if !strings.Contains(text, `multi_agent = true`) {
			t.Fatalf("config.toml missing multi_agent default:\n%s", text)
		}
		if !strings.Contains(text, "[notice]") {
			t.Fatalf("config.toml missing [notice] table:\n%s", text)
		}
		if !strings.Contains(text, `hide_rate_limit_model_nudge = true`) {
			t.Fatalf("config.toml missing rate-limit nudge suppression:\n%s", text)
		}
	}

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
		assertManagedDefaults(t, data)
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
		assertManagedDefaults(t, data)
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
		assertManagedDefaults(t, data)
	})

	t.Run("reuses existing features table and flips false to true", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			"[defaults]",
			`model = "gpt-5"`,
			"",
			"[features]",
			"experimental_resume = true",
			"multi_agent = false",
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		assertManagedDefaults(t, data)
		if strings.Count(string(data), "[features]") != 1 {
			t.Fatalf("expected a single [features] table:\n%s", string(data))
		}
	})

	t.Run("reuses existing notice table and flips false to true", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			"[notice]",
			"hide_full_access_warning = true",
			"hide_rate_limit_model_nudge = false",
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		assertManagedDefaults(t, data)
		if strings.Count(string(data), "[notice]") != 1 {
			t.Fatalf("expected a single [notice] table:\n%s", string(data))
		}
	})

	t.Run("normalizes inline managed settings", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			`cli_auth_credentials_store = "keychain"`,
			"[features]multi_agent = false",
			"[notice]hide_rate_limit_model_nudge = false",
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		assertManagedDefaults(t, data)
		text := string(data)
		if strings.Contains(text, "[features]multi_agent") {
			t.Fatalf("inline [features] setting should be normalized:\n%s", text)
		}
		if strings.Contains(text, "[notice]hide_rate_limit_model_nudge") {
			t.Fatalf("inline [notice] setting should be normalized:\n%s", text)
		}
	})

	t.Run("repairs collapsed table header after notice value", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			"[notice]",
			"hide_full_access_warning = true",
			"hide_rate_limit_model_nudge = true[assistant_principles]",
			`values = ["keep"]`,
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}
		text := string(data)
		assertManagedDefaults(t, data)
		if !strings.Contains(text, "hide_rate_limit_model_nudge = true\n[assistant_principles]") {
			t.Fatalf("collapsed section header should be repaired:\n%s", text)
		}
	})

	t.Run("repairs real malformed backup snapshot without duplicate managed settings", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			`model_reasoning_effort = "xhigh"`,
			`approval_policy = "never"`,
			`sandbox_mode = "danger-full-access"`,
			`cli_auth_credentials_store = "file"`,
			`project_doc_fallback_filenames = ["CLAUDE.md", "VISION.md"]`,
			`project_doc_max_bytes = 65536`,
			`model = "gpt-5.4"`,
			"",
			`[projects."/Users/hope"]`,
			`trust_level = "trusted"`,
			`sandbox_mode = "danger-full-access"`,
			`approval_policy = "never"`,
			"",
			`[notice]`,
			`hide_full_access_warning = true`,
			`hide_rate_limit_model_nudge = true[assistant_principles]`,
			`values = ["progress_and_learning_speed_over_perfection"]`,
			`hide_rate_limit_model_nudge = true[notice.model_migrations]`,
			`"gpt-5.2" = "gpt-5.2"`,
			`hide_rate_limit_model_nudge = true`,
			`[features]multi_agent = true`,
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Read config.toml: %v", err)
		}

		assertManagedDefaults(t, data)
		text := string(data)
		if strings.Contains(text, "hide_rate_limit_model_nudge = true[assistant_principles]") {
			t.Fatalf("assistant_principles boundary should be repaired:\n%s", text)
		}
		if strings.Contains(text, "hide_rate_limit_model_nudge = true[notice.model_migrations]") {
			t.Fatalf("notice.model_migrations boundary should be repaired:\n%s", text)
		}
		if strings.Contains(text, "[features]multi_agent") {
			t.Fatalf("inline [features] setting should be normalized:\n%s", text)
		}
		if strings.Count(text, "hide_rate_limit_model_nudge = true") != 1 {
			t.Fatalf("expected one canonical rate-limit nudge setting, got %d:\n%s", strings.Count(text, "hide_rate_limit_model_nudge = true"), text)
		}
		if strings.Count(text, "multi_agent = true") != 1 {
			t.Fatalf("expected one canonical multi_agent setting, got %d:\n%s", strings.Count(text, "multi_agent = true"), text)
		}
	})

	t.Run("preserves symlinked config target", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, "codex-home")
		canonicalDir := filepath.Join(root, "canonical")
		path := filepath.Join(home, "config.toml")
		target := filepath.Join(canonicalDir, "config.toml")

		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatalf("MkdirAll home: %v", err)
		}
		if err := os.MkdirAll(canonicalDir, 0o700); err != nil {
			t.Fatalf("MkdirAll canonicalDir: %v", err)
		}
		if err := os.WriteFile(target, []byte("cli_auth_credentials_store = \"keychain\"\n"), 0o600); err != nil {
			t.Fatalf("Write target config.toml: %v", err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("Symlink config.toml: %v", err)
		}

		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}

		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("Lstat config.toml: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected config.toml to remain a symlink, mode=%v", info.Mode())
		}

		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("Read target config.toml: %v", err)
		}
		assertManagedDefaults(t, data)
	})
}

func TestManagedConfigProblems(t *testing.T) {
	t.Run("rejects empty home", func(t *testing.T) {
		if _, err := ManagedConfigProblems(" "); err == nil {
			t.Fatal("expected error for empty codex home")
		}
	})

	t.Run("reports missing config", func(t *testing.T) {
		problems, err := ManagedConfigProblems(t.TempDir())
		if err != nil {
			t.Fatalf("ManagedConfigProblems() error = %v", err)
		}
		if len(problems) != 1 || problems[0] != "config.toml missing" {
			t.Fatalf("ManagedConfigProblems() = %#v, want missing config", problems)
		}
	})

	t.Run("reports malformed inline managed settings", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			`cli_auth_credentials_store = "keychain"`,
			"[features]multi_agent = false",
			"[notice]hide_rate_limit_model_nudge = false",
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}

		problems, err := ManagedConfigProblems(home)
		if err != nil {
			t.Fatalf("ManagedConfigProblems() error = %v", err)
		}
		if len(problems) < 4 {
			t.Fatalf("expected multiple managed config problems, got %#v", problems)
		}
	})

	t.Run("reports invalid toml", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			"[notice]",
			"hide_full_access_warning = true",
			"hide_rate_limit_model_nudge = true[assistant_principles]",
			`values = ["broken"]`,
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}

		problems, err := ManagedConfigProblems(home)
		if err != nil {
			t.Fatalf("ManagedConfigProblems() error = %v", err)
		}
		if len(problems) == 0 || !strings.Contains(problems[0], "config.toml is not valid TOML") {
			t.Fatalf("expected invalid TOML problem, got %#v", problems)
		}
	})

	t.Run("healthy config reports no problems", func(t *testing.T) {
		home := t.TempDir()
		if err := EnsureFileCredentialStore(home); err != nil {
			t.Fatalf("EnsureFileCredentialStore() error = %v", err)
		}

		problems, err := ManagedConfigProblems(home)
		if err != nil {
			t.Fatalf("ManagedConfigProblems() error = %v", err)
		}
		if len(problems) != 0 {
			t.Fatalf("ManagedConfigProblems() = %#v, want none", problems)
		}
	})

	t.Run("wrong section does not count as healthy", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		content := strings.Join([]string{
			`cli_auth_credentials_store = "file"`,
			"",
			"[features]",
			"experimental_resume = true",
			"",
			"[other]",
			"multi_agent = true",
			"",
			"[notice]",
			"hide_full_access_warning = true",
			"",
			"[other_notice]",
			"hide_rate_limit_model_nudge = true",
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("Write config.toml: %v", err)
		}

		problems, err := ManagedConfigProblems(home)
		if err != nil {
			t.Fatalf("ManagedConfigProblems() error = %v", err)
		}
		if !containsProblem(problems, "managed [features] multi_agent = true is missing") {
			t.Fatalf("expected missing managed features problem, got %#v", problems)
		}
		if !containsProblem(problems, "managed [notice] hide_rate_limit_model_nudge = true is missing") {
			t.Fatalf("expected missing managed notice problem, got %#v", problems)
		}
	})
}

func TestRepairCollapsedTableHeaders(t *testing.T) {
	input := []byte("[notice]\nhide_rate_limit_model_nudge = true[assistant_principles]\nvalues = [\"keep\"]\n")
	repaired, changed := repairCollapsedTableHeaders(input)
	if !changed {
		t.Fatal("expected collapsed table header repair to report a change")
	}
	text := string(repaired)
	if !strings.Contains(text, "hide_rate_limit_model_nudge = true\n[assistant_principles]") {
		t.Fatalf("expected repaired boundary, got:\n%s", text)
	}
	if strings.Contains(text, "values = \n") {
		t.Fatalf("array value should not be split into a fake section header:\n%s", text)
	}
}

func containsProblem(problems []string, want string) bool {
	for _, problem := range problems {
		if problem == want {
			return true
		}
	}
	return false
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
