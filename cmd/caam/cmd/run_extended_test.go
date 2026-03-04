package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	caamexec "github.com/Dicklesworthstone/coding_agent_account_manager/internal/exec"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider/claude"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess_Run is the entry point for the mock process for run tests.
func TestHelperProcess_Run(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mode := os.Getenv("MOCK_RUN_MODE")
	switch mode {
	case "success":
		fmt.Println("Command success")
		os.Exit(0)
	case "ratelimit":
		fmt.Println("Error: rate limit exceeded")
		os.Exit(1)
	case "failover":
		// Fail first time (if profile is work), succeed second time (if profile is personal)
		authPath := os.Getenv("MOCK_AUTH_PATH")
		content, _ := os.ReadFile(authPath)
		if string(content) == `{"token":"work"}` {
			fmt.Println("Error: rate limit exceeded")
			os.Exit(1)
		} else {
			fmt.Println("Command success (failover)")
			os.Exit(0)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", mode)
		os.Exit(1)
	}
}

func TestRunCommand_Extended(t *testing.T) {
	// TODO: This test needs redesign. The SmartRunner uses PTY-based monitoring
	// and expects long-running interactive CLI sessions where it can:
	// 1. Detect rate limit patterns in output
	// 2. Inject login commands via PTY
	// 3. Wait for login completion
	// The current mock process exits immediately, which doesn't allow the
	// handoff flow to complete. The test needs a mock that simulates a
	// long-running process with rate limit output patterns.
	t.Skip("Test requires redesign: SmartRunner PTY flow incompatible with immediate-exit mocks")

	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Create vault and mock globals")

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")

	// Create database path
	dbDir := filepath.Join(rootDir, "db")
	require.NoError(t, os.MkdirAll(dbDir, 0755))

	// Override DB path
	h.SetEnv("HOME", rootDir)
	h.SetEnv("XDG_DATA_HOME", rootDir)
	configDir := filepath.Join(rootDir, "caam")
	h.SetEnv("CAAM_HOME", configDir)
	h.SetEnv("XDG_CONFIG_HOME", rootDir)

	// Write config
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "caam"), 0755))
	configPath := filepath.Join(rootDir, "caam", "config.json")
	configJSON := `{"wrap": {"initial_delay": "10ms"}}`
	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0600))

	// Override vault, tools, and profileStore
	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	originalRegistry := registry
	originalExecCommand := caamexec.ExecCommand
	originalGetWd := getWd
	originalProfileStore := profileStore
	defer func() {
		vault = originalVault
		tools = originalTools
		registry = originalRegistry
		caamexec.ExecCommand = originalExecCommand
		getWd = originalGetWd
		profileStore = originalProfileStore
	}()

	vault = authfile.NewVault(vaultDir)

	// Create isolated profile store in temp directory
	profilesDir := filepath.Join(rootDir, "caam", "profiles")
	require.NoError(t, os.MkdirAll(profilesDir, 0755))
	profileStore = profile.NewStore(profilesDir)

	// Setup registry
	registry = provider.NewRegistry()
	registry.Register(claude.New())

	// Define target location for restore
	homeDir := filepath.Join(rootDir, "home")
	targetPath := filepath.Join(homeDir, "auth.json")
	require.NoError(t, os.MkdirAll(homeDir, 0755))

	// Setup profiles in vault
	// 1. Active profile (alphabetically first)
	activeDir := filepath.Join(vaultDir, "claude", "active_profile")
	require.NoError(t, os.MkdirAll(activeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(activeDir, "auth.json"), []byte(`{"token":"work"}`), 0600))

	// 2. Backup profile
	backupDir := filepath.Join(vaultDir, "claude", "backup_profile")
	require.NoError(t, os.MkdirAll(backupDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "auth.json"), []byte(`{"token":"personal"}`), 0600))

	tools["claude"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "claude",
			Files: []authfile.AuthFileSpec{
				{Path: targetPath, Required: true},
			},
		}
	}

	getWd = func() (string, error) {
		return rootDir, nil
	}

	h.EndStep("Setup")

	// 2. Test Success
	h.StartStep("Success", "Test successful run")

	caamexec.ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestHelperProcess_Run$", "--", name}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "MOCK_RUN_MODE=success")
		return cmd
	}

	runCmd.Flags().Set("quiet", "true")
	err := runWrap(runCmd, []string{"claude", "prompt"})
	require.NoError(t, err)

	h.EndStep("Success")

	// 3. Test Failover
	h.StartStep("Failover", "Test rate limit failover")

	runCmd.Flags().Set("max-retries", "1")
	runCmd.Flags().Set("cooldown", "30m")
	runCmd.Flags().Set("quiet", "true")
	runCmd.Flags().Set("algorithm", "round_robin")

	db, _ := caamdb.Open()
	db.ClearAllCooldowns()
	db.Close()

	caamexec.ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestHelperProcess_Run$", "--", name}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"MOCK_RUN_MODE=failover",
			"MOCK_AUTH_PATH="+targetPath,
		)
		return cmd
	}

	err = runWrap(runCmd, []string{"claude", "prompt"})
	require.NoError(t, err)

	db, err = caamdb.Open()
	require.NoError(t, err)
	defer db.Close()

	ev, err := db.ActiveCooldown("claude", "active_profile", time.Now())
	require.NoError(t, err)
	require.NotNil(t, ev, "Active profile should be in cooldown")

	h.EndStep("Failover")
}

func TestIsProviderAuthFailure(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		errMsg  string
		wantHit bool
	}{
		{name: "codex refresh token reused", tool: "codex", errMsg: "refresh_token_reused", wantHit: true},
		{name: "codex access token refresh message", tool: "codex", errMsg: "Your access token could not be refreshed", wantHit: true},
		{name: "codex invalid_grant", tool: "codex", errMsg: "invalid_grant", wantHit: true},
		{name: "codex generic 429", tool: "codex", errMsg: "429 too many requests", wantHit: false},
		{name: "claude ignored", tool: "claude", errMsg: "refresh_token_reused", wantHit: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isProviderAuthFailure(tc.tool, tc.errMsg)
			if got != tc.wantHit {
				t.Fatalf("isProviderAuthFailure(%q,%q)=%v want %v", tc.tool, tc.errMsg, got, tc.wantHit)
			}
		})
	}
}

func TestProviderSwitchCandidateAndHardBlocked(t *testing.T) {
	t.Run("codex with credits remains candidate even when near threshold", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: true, Unlimited: false},
			SecondaryWindow: &usage.UsageWindow{
				Utilization: 0.95,
				UsedPercent: 95,
			},
		}
		if isProviderHardBlocked("codex", info) {
			t.Fatal("expected codex profile with credits to not be hard-blocked")
		}
		if !isProviderSwitchCandidate("codex", info, 0.8) {
			t.Fatal("expected codex profile with credits to remain a switch candidate")
		}
	})

	t.Run("codex with exhausted secondary window is excluded", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: true, Unlimited: false},
			SecondaryWindow: &usage.UsageWindow{
				Utilization: 1.0,
				UsedPercent: 100,
			},
		}
		if isProviderSwitchCandidate("codex", info, 0.8) {
			t.Fatal("expected codex profile at 100% secondary usage to be excluded")
		}
	})

	t.Run("codex with no credits but no depletion signal is still eligible", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false},
		}
		if isProviderHardBlocked("codex", info) {
			t.Fatal("expected codex profile without explicit depletion to stay eligible")
		}
		if !isProviderSwitchCandidate("codex", info, 0.8) {
			t.Fatal("expected codex profile without explicit depletion to remain candidate")
		}
	})

	t.Run("codex with zero balance but no windows remains eligible", func(t *testing.T) {
		zero := 0.0
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false, Balance: &zero},
		}
		if isProviderHardBlocked("codex", info) {
			t.Fatal("expected codex profile with zero balance and no windows to stay eligible")
		}
		if !isProviderSwitchCandidate("codex", info, 0.8) {
			t.Fatal("expected codex profile with zero balance and no windows to remain candidate")
		}
	})

	t.Run("codex with zero balance but active window stays eligible", func(t *testing.T) {
		zero := 0.0
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false, Balance: &zero},
			PrimaryWindow: &usage.UsageWindow{
				UsedPercent: 28,
			},
		}
		if isProviderHardBlocked("codex", info) {
			t.Fatal("expected codex profile with active window to avoid hard block")
		}
		if !isProviderSwitchCandidate("codex", info, 0.8) {
			t.Fatal("expected codex profile with active window to remain candidate")
		}
	})

	t.Run("claude with exhausted primary window is hard-blocked", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "claude",
			PrimaryWindow: &usage.UsageWindow{
				UsedPercent: 100,
			},
		}
		if !isProviderHardBlocked("claude", info) {
			t.Fatal("expected exhausted claude profile to be hard-blocked")
		}
		if isProviderSwitchCandidate("claude", info, 0.8) {
			t.Fatal("expected exhausted claude profile to be excluded from candidates")
		}
	})

	t.Run("claude with explicit 429 error is hard-blocked", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "claude",
			Error:    "API error: status 429",
		}
		if !isProviderHardBlocked("claude", info) {
			t.Fatal("expected rate-limited claude profile to be hard-blocked")
		}
	})

	t.Run("openclaw alias inherits codex exhaustion candidate filtering", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			SecondaryWindow: &usage.UsageWindow{
				UsedPercent: 100,
			},
		}
		if isProviderSwitchCandidate("openclaw", info, 0.8) {
			t.Fatal("expected openclaw alias to be excluded from switch candidates when exhausted")
		}
	})
}

func TestIsProfileLockedErr(t *testing.T) {
	if !isProfileLockedErr(fmt.Errorf("lock profile: profile demo is already locked")) {
		t.Fatal("expected lock error to be detected")
	}
	if isProfileLockedErr(fmt.Errorf("other error")) {
		t.Fatal("expected non-lock error to be ignored")
	}
}
