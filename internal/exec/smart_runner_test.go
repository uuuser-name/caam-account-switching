package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authpool"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/notify"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
)

// =============================================================================
// HandoffState Tests
// =============================================================================

func TestHandoffState_String(t *testing.T) {
	tests := []struct {
		state    HandoffState
		expected string
	}{
		{Running, "RUNNING"},
		{RateLimited, "RATE_LIMITED"},
		{SelectingBackup, "SELECTING_BACKUP"},
		{SwappingAuth, "SWAPPING_AUTH"},
		{LoggingIn, "LOGGING_IN"},
		{LoginComplete, "LOGIN_COMPLETE"},
		{HandoffFailed, "HANDOFF_FAILED"},
		{ManualMode, "MANUAL_MODE"},
		{HandoffState(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("HandoffState.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// SmartRunner Tests
// =============================================================================

func TestNewSmartRunner(t *testing.T) {
	t.Run("creates runner with defaults", func(t *testing.T) {
		registry := provider.NewRegistry()
		runner := NewRunner(registry)

		sr := NewSmartRunner(runner, SmartRunnerOptions{})

		if sr == nil {
			t.Fatal("NewSmartRunner returned nil")
		}
		if sr.Runner != runner {
			t.Error("Runner not set correctly")
		}
		if sr.state != Running {
			t.Errorf("initial state = %v, want %v", sr.state, Running)
		}
		if sr.notifier == nil {
			t.Error("notifier should have default value")
		}
	})

	t.Run("default notifier is safe to use", func(t *testing.T) {
		registry := provider.NewRegistry()
		runner := NewRunner(registry)
		sr := NewSmartRunner(runner, SmartRunnerOptions{})

		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("default notifier should not panic, got: %v", rec)
			}
		}()
		sr.notifyHandoff("profile-a", "profile-b")
	})

	t.Run("creates runner with custom options", func(t *testing.T) {
		registry := provider.NewRegistry()
		runner := NewRunner(registry)
		vault := authfile.NewVault(t.TempDir())
		pool := authpool.NewAuthPool()
		notifier := &notify.TerminalNotifier{}
		handoffCfg := &config.HandoffConfig{
			AutoTrigger:      true,
			MaxRetries:       3,
			FallbackToManual: true,
		}

		sr := NewSmartRunner(runner, SmartRunnerOptions{
			Vault:            vault,
			AuthPool:         pool,
			Notifier:         notifier,
			HandoffConfig:    handoffCfg,
			CooldownDuration: 30 * time.Minute,
		})

		if sr.vault != vault {
			t.Error("vault not set correctly")
		}
		if sr.authPool != pool {
			t.Error("authPool not set correctly")
		}
		if sr.notifier != notifier {
			t.Error("notifier not set correctly")
		}
		if sr.handoffConfig != handoffCfg {
			t.Error("handoffConfig not set correctly")
		}
		if sr.cooldownDuration != 30*time.Minute {
			t.Errorf("cooldownDuration = %v, want 30m", sr.cooldownDuration)
		}
	})
}

func TestSmartRunner_setState(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	states := []HandoffState{
		Running,
		RateLimited,
		SelectingBackup,
		SwappingAuth,
		LoggingIn,
		LoginComplete,
		HandoffFailed,
		ManualMode,
	}

	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			sr.setState(state)

			sr.mu.Lock()
			got := sr.state
			sr.mu.Unlock()

			if got != state {
				t.Errorf("setState() = %v, want %v", got, state)
			}
		})
	}
}

func TestSmartRunner_InitialState(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	if sr.handoffCount != 0 {
		t.Errorf("initial handoffCount = %d, want 0", sr.handoffCount)
	}
	if sr.currentProfile != "" {
		t.Errorf("initial currentProfile = %q, want empty", sr.currentProfile)
	}
	if sr.previousProfile != "" {
		t.Errorf("initial previousProfile = %q, want empty", sr.previousProfile)
	}
}

func TestSmartRunner_DrainLoginDone(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	sr.loginDone <- loginResult{success: true}
	sr.drainLoginDone()

	select {
	case <-sr.loginDone:
		t.Fatal("expected loginDone to be empty after drain")
	default:
	}

	// Ensure drain is safe on empty channel
	sr.drainLoginDone()
}

// =============================================================================
// Mock Notifier for Testing
// =============================================================================

type mockNotifier struct {
	alerts []*notify.Alert
}

func (m *mockNotifier) Notify(alert *notify.Alert) error {
	m.alerts = append(m.alerts, alert)
	return nil
}

func (m *mockNotifier) Name() string {
	return "mock"
}

func (m *mockNotifier) Available() bool {
	return true
}

func TestSmartRunner_NotifierIntegration(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	notifier := &mockNotifier{}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Notifier: notifier,
	})

	// Test notifyHandoff
	sr.notifyHandoff("profile1", "profile2")

	if len(notifier.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(notifier.alerts))
	}
	if notifier.alerts[0].Level != notify.Info {
		t.Errorf("expected Info level, got %v", notifier.alerts[0].Level)
	}
	if notifier.alerts[0].Title != "Switching profiles" {
		t.Errorf("unexpected title: %s", notifier.alerts[0].Title)
	}
}

func TestSmartRunner_FailWithManual(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	notifier := &mockNotifier{}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Notifier: notifier,
	})
	sr.currentProfile = "test-profile"

	sr.failWithManual("test error: %s", "details")

	if sr.state != HandoffFailed {
		t.Errorf("state = %v, want %v", sr.state, HandoffFailed)
	}
	if len(notifier.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(notifier.alerts))
	}
	if notifier.alerts[0].Level != notify.Warning {
		t.Errorf("expected Warning level, got %v", notifier.alerts[0].Level)
	}
}

func TestIsCodexReauthRequiredReason(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "refresh token reused", reason: "refresh_token_reused", want: true},
		{name: "already used", reason: "Your refresh token was already used", want: true},
		{name: "access token could not be refreshed", reason: "access token could not be refreshed", want: true},
		{name: "invalid grant", reason: "invalid_grant", want: true},
		{name: "token expired invalid", reason: "token expired or invalid", want: true},
		{name: "normal 429", reason: "429 too many requests", want: false},
		{name: "empty", reason: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isCodexReauthRequiredReason(tc.reason)
			if got != tc.want {
				t.Fatalf("isCodexReauthRequiredReason(%q) = %v, want %v", tc.reason, got, tc.want)
			}
		})
	}
}

func TestShouldSkipInteractiveLogin(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		reason   string
		want     bool
	}{
		{name: "codex generic rate limit", provider: "codex", reason: "rate limit exceeded", want: true},
		{name: "codex credits exhausted", provider: "codex", reason: "out of credits", want: true},
		{name: "codex auth refresh failure", provider: "codex", reason: "refresh_token_reused", want: false},
		{name: "claude never skips", provider: "claude", reason: "rate limit", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSkipInteractiveLogin(tc.provider, tc.reason)
			if got != tc.want {
				t.Fatalf("shouldSkipInteractiveLogin(%q,%q) = %v, want %v", tc.provider, tc.reason, got, tc.want)
			}
		})
	}
}

func TestPreferUserProfiles(t *testing.T) {
	t.Run("prefers non-system profiles when available", func(t *testing.T) {
		got := preferUserProfiles([]string{"_backup_1", "work", "_original"})
		if len(got) != 1 || got[0] != "work" {
			t.Fatalf("preferUserProfiles returned %v", got)
		}
	})

	t.Run("falls back to system profiles when no user profiles exist", func(t *testing.T) {
		in := []string{"_backup_1", "_backup_2"}
		got := preferUserProfiles(in)
		if len(got) != 2 || got[0] != "_backup_1" || got[1] != "_backup_2" {
			t.Fatalf("preferUserProfiles returned %v", got)
		}
	})
}

func TestSystemProfileFallbackHelpers(t *testing.T) {
	if allSystemProfiles([]string{}) {
		t.Fatal("expected empty slice to not be treated as all-system")
	}
	if !allSystemProfiles([]string{"_backup_a", "_backup_b"}) {
		t.Fatal("expected allSystemProfiles=true for system-only slice")
	}
	if allSystemProfiles([]string{"_backup_a", "work"}) {
		t.Fatal("expected allSystemProfiles=false for mixed slice")
	}

	selected := selectDeterministicSystemFallback([]string{"_backup_z", "_backup_a", "_backup_m"})
	if selected != "_backup_a" {
		t.Fatalf("deterministic system fallback selected %q, want _backup_a", selected)
	}
}

func TestSmartRunner_ExcludeSharedCredentialAliases(t *testing.T) {
	vaultDir := t.TempDir()
	writeAuth := func(profile, token string) {
		dir := filepath.Join(vaultDir, "codex", profile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		content := []byte(`{"tokens":{"access_token":"` + token + `"}}`)
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), content, 0o600); err != nil {
			t.Fatalf("write auth %s: %v", profile, err)
		}
	}

	writeAuth("active", "token-a")
	writeAuth("alias", "token-a")
	writeAuth("backup", "token-b")

	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{
		Vault: authfile.NewVault(vaultDir),
	})

	filtered := sr.excludeSharedCredentialAliases("codex", "active", []string{"alias", "backup", "missing"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 profiles after filtering, got %v", filtered)
	}
	if filtered[0] != "backup" || filtered[1] != "missing" {
		t.Fatalf("unexpected filtered profiles: %v", filtered)
	}
}

func TestSmartRunner_FilterSelectionPoolDetectsAliasOnlySet(t *testing.T) {
	vaultDir := t.TempDir()
	writeAuth := func(profile, token string) {
		dir := filepath.Join(vaultDir, "codex", profile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		content := []byte(`{"tokens":{"access_token":"` + token + `"}}`)
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), content, 0o600); err != nil {
			t.Fatalf("write auth %s: %v", profile, err)
		}
	}

	writeAuth("active", "token-a")
	writeAuth("alias-a", "token-a")
	writeAuth("alias-b", "token-a")

	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{
		Vault: authfile.NewVault(vaultDir),
	})

	filtered, aliasOnly := sr.filterSelectionPool("codex", "active", []string{"alias-a", "alias-b"})
	if len(filtered) != 0 {
		t.Fatalf("expected alias-only pool to be fully filtered, got %v", filtered)
	}
	if !aliasOnly {
		t.Fatal("expected aliasOnly=true when candidates all share the current credential")
	}
}

func TestSmartRunner_ExcludeActiveCooldownProfiles(t *testing.T) {
	registry := provider.NewRegistry()
	pool := authpool.NewAuthPool()
	pool.AddProfile("codex", "_backup_ready")
	pool.AddProfile("codex", "_backup_cooldown")
	pool.SetCooldown("codex", "_backup_cooldown", time.Hour)

	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{
		AuthPool: pool,
	})

	filtered := sr.excludeActiveCooldownProfiles("codex", []string{"_backup_ready", "_backup_cooldown"})
	if len(filtered) != 1 || filtered[0] != "_backup_ready" {
		t.Fatalf("unexpected cooldown filtered set: %v", filtered)
	}
}

func TestIsProviderSwitchCandidateForHandoff(t *testing.T) {
	t.Run("codex keeps credit-available profile as candidate", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: true, Unlimited: false},
			SecondaryWindow: &usage.UsageWindow{
				Utilization: 0.99,
				UsedPercent: 99,
			},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile with credits to remain candidate")
		}
	})

	t.Run("codex at 100% secondary usage is excluded", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: true, Unlimited: false},
			SecondaryWindow: &usage.UsageWindow{
				UsedPercent: 100,
			},
		}
		if isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile at 100% secondary usage to be excluded")
		}
	})

	t.Run("codex without explicit depletion remains candidate", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile without explicit depletion to remain candidate")
		}
	})

	t.Run("codex with zero balance but no windows remains candidate", func(t *testing.T) {
		zero := 0.0
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false, Balance: &zero},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile with zero balance and no windows to remain candidate")
		}
	})

	t.Run("codex with zero balance but active window remains candidate", func(t *testing.T) {
		zero := 0.0
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false, Balance: &zero},
			PrimaryWindow: &usage.UsageWindow{
				UsedPercent: 28,
			},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile with active window to remain candidate")
		}
	})

	t.Run("claude near-limit is excluded", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "claude",
			PrimaryWindow: &usage.UsageWindow{
				Utilization: 0.95,
				UsedPercent: 95,
			},
		}
		if isProviderSwitchCandidateForHandoff("claude", info, 0.8) {
			t.Fatal("expected near-limit claude profile to be excluded")
		}
	})
}

func TestSmartRunner_WithRotation(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	selector := rotation.NewSelector(rotation.AlgorithmSmart, nil, nil)

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Rotation: selector,
	})

	if sr.rotation != selector {
		t.Error("rotation selector not set correctly")
	}
}

func TestSmartRunner_ResumeHint(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	sr.providerID = "codex"
	sr.currentProfile = "work"
	sr.lastSessionID = "019b2e3d-b524-7c22-91da-47de9068d09a"

	if got := sr.resumeHint("backup"); got != "caam resume codex backup --session 019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected fallback resume hint: %q", got)
	}

	sr.updateResumeHint("codex resume 019b2e3d-b524-7c22-91da-47de9068d09a", "")
	if got := sr.resumeHint("backup"); got != "codex resume 019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected captured resume hint: %q", got)
	}
}

func TestResumeArgsFromHint(t *testing.T) {
	t.Run("parses codex resume hint", func(t *testing.T) {
		args, ok := resumeArgsFromHint("codex", "codex resume 019b2e3d-b524-7c22-91da-47de9068d09a", "")
		if !ok {
			t.Fatal("expected hint to parse")
		}
		if len(args) != 2 || args[0] != "resume" || args[1] != "019b2e3d-b524-7c22-91da-47de9068d09a" {
			t.Fatalf("unexpected resume args: %v", args)
		}
	})

	t.Run("falls back to session ID", func(t *testing.T) {
		args, ok := resumeArgsFromHint("codex", "", "fallback-session")
		if !ok {
			t.Fatal("expected fallback session to parse")
		}
		if len(args) != 2 || args[0] != "resume" || args[1] != "fallback-session" {
			t.Fatalf("unexpected fallback args: %v", args)
		}
	})

	t.Run("preserves quoted prompt and options from hint", func(t *testing.T) {
		hint := `To continue this session, run codex resume --model gpt-5 019b2e3d-b524-7c22-91da-47de9068d09a "continue from here"`
		args, ok := resumeArgsFromHint("codex", hint, "")
		if !ok {
			t.Fatal("expected quoted hint to parse")
		}
		if len(args) != 5 {
			t.Fatalf("unexpected arg count: %v", args)
		}
		if args[0] != "resume" || args[1] != "--model" || args[2] != "gpt-5" || args[3] != "019b2e3d-b524-7c22-91da-47de9068d09a" || args[4] != "continue from here" {
			t.Fatalf("unexpected parsed args: %v", args)
		}
	})

	t.Run("rejects non-codex hint without fallback", func(t *testing.T) {
		if args, ok := resumeArgsFromHint("claude", "caam resume codex backup --session id", ""); ok || len(args) > 0 {
			t.Fatalf("expected no args, got %v (ok=%v)", args, ok)
		}
	})
}

func TestResumeHasPrompt(t *testing.T) {
	if resumeHasPrompt([]string{"resume", "session-1"}, "session-1") {
		t.Fatal("expected bare resume command to require a continuation prompt")
	}
	if !resumeHasPrompt([]string{"resume", "session-1", "continue previous task"}, "session-1") {
		t.Fatal("expected resume command with trailing payload to be treated as already prompted")
	}
	if !resumeHasPrompt([]string{"resume", "--model", "gpt-5", "session-1", "continue previous task"}, "session-1") {
		t.Fatal("expected prompt detection to survive leading options")
	}
}

func TestSeamlessResumeDepth(t *testing.T) {
	t.Run("uses run option env first", func(t *testing.T) {
		depth := seamlessResumeDepth(RunOptions{
			Env: map[string]string{seamlessResumeDepthEnv: "1"},
		})
		if depth != 1 {
			t.Fatalf("depth = %d, want 1", depth)
		}
	})

	t.Run("falls back to process env", func(t *testing.T) {
		t.Setenv(seamlessResumeDepthEnv, "2")
		depth := seamlessResumeDepth(RunOptions{})
		if depth != 2 {
			t.Fatalf("depth = %d, want 2", depth)
		}
	})
}

func TestIsInterruptExitCode(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{code: 0, want: false},
		{code: 1, want: false},
		{code: 130, want: true},
		{code: 143, want: true},
	}
	for _, tc := range tests {
		if got := isInterruptExitCode(tc.code); got != tc.want {
			t.Fatalf("isInterruptExitCode(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestMaybeRunSeamlessResumeSkipsWhenWindowExpired(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now().Add(-(seamlessResumeMaxDelay + time.Second))

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 1, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected seamless resume to be skipped when arming window expired")
	}
}

func TestMaybeRunSeamlessResumeSkipsInterruptExit(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now()

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 130, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected seamless resume to be skipped for user-interrupt exit")
	}
}

func TestMaybeRunSeamlessResumeAllowsExitZeroAfterHandoff(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now()
	sr.seamlessResumeOutput = true

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 0, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected exit-zero run with post-handoff output to skip seamless resume")
	}
}

func TestMaybeRunSeamlessResumeSkipsWhenOutputContinuedAfterSwitch(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now()
	sr.seamlessResumeOutput = true

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 1, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected seamless resume to be skipped after post-handoff output was observed")
	}
}
