package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	case "codex_rate_limit_resume_continue":
		counterFile := os.Getenv("MOCK_COUNTER_FILE")
		authPath := os.Getenv("MOCK_AUTH_PATH")
		expectedSessionID := os.Getenv("MOCK_EXPECT_RESUME_ID")
		continuationFile := os.Getenv("MOCK_CONTINUATION_FILE")

		count := 0
		if counterFile != "" {
			if data, err := os.ReadFile(counterFile); err == nil {
				if parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); parseErr == nil {
					count = parsed
				}
			}
		}
		count++
		if counterFile != "" {
			_ = os.WriteFile(counterFile, []byte(strconv.Itoa(count)), 0600)
		}

		if count == 1 {
			fmt.Println("Processing...")
			time.Sleep(1 * time.Second)
			fmt.Println("■ You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again")
			fmt.Println("at 3:05 PM.")
			fmt.Printf("To continue this session, run codex resume %s\n", expectedSessionID)
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				if authPath != "" {
					if content, err := os.ReadFile(authPath); err == nil && strings.TrimSpace(string(content)) == `{"token":"backup"}` {
						break
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
			time.Sleep(500 * time.Millisecond)
			os.Exit(1)
			return
		}

		joinedArgs := strings.Join(os.Args, " ")
		if expectedSessionID != "" && !strings.Contains(joinedArgs, "resume "+expectedSessionID) {
			fmt.Printf("Missing expected resume args: %s\n", expectedSessionID)
			os.Exit(1)
			return
		}
		if authPath != "" {
			content, _ := os.ReadFile(authPath)
			if strings.TrimSpace(string(content)) != `{"token":"backup"}` {
				fmt.Printf("Expected switched backup auth, got: %s\n", strings.TrimSpace(string(content)))
				os.Exit(1)
				return
			}
		}
		fmt.Println("Resumed session idle, awaiting continuation input")
		lineCh := make(chan string, 1)
		go func() {
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			lineCh <- line
		}()
		select {
		case line := <-lineCh:
			trimmed := strings.TrimSpace(line)
			if continuationFile != "" {
				_ = os.WriteFile(continuationFile, []byte(trimmed), 0600)
			}
			if !strings.Contains(strings.ToLower(trimmed), "continue exactly where you left off") {
				fmt.Printf("Unexpected continuation prompt: %s\n", trimmed)
				os.Exit(1)
				return
			}
			fmt.Println("Resumed session continued on switched profile")
			os.Exit(0)
		case <-time.After(3 * time.Second):
			fmt.Println("Resumed session idle awaiting user input")
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "Unknown mode")
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

	require.NoError(t, runCmd.Flags().Set("quiet", "true"))
	err := runWrap(runCmd, []string{"claude", "prompt"})
	require.NoError(t, err)

	h.EndStep("Success")

	// 3. Test Failover
	h.StartStep("Failover", "Test rate limit failover")

	require.NoError(t, runCmd.Flags().Set("max-retries", "1"))
	require.NoError(t, runCmd.Flags().Set("cooldown", "30m"))
	require.NoError(t, runCmd.Flags().Set("quiet", "true"))
	require.NoError(t, runCmd.Flags().Set("algorithm", "round_robin"))

	db, _ := caamdb.Open()
	_, err = db.ClearAllCooldowns()
	require.NoError(t, err)
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

func TestRunWrap_CodexRateLimitResumeContinuesOnSwitchedProfile(t *testing.T) {
	resetCommandTreeForExecute(rootCmd)
	defer resetCommandTreeForExecute(rootCmd)

	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	codexHome := filepath.Join(rootDir, ".codex")
	profilesDir := filepath.Join(rootDir, "caam", "profiles")
	binDir := filepath.Join(rootDir, "bin")
	counterFile := filepath.Join(rootDir, "resume_count.txt")
	continuationFile := filepath.Join(rootDir, "continuation_args.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"
	liveAuthPath := filepath.Join(codexHome, "auth.json")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("XDG_DATA_HOME", rootDir)
	h.SetEnv("XDG_CONFIG_HOME", rootDir)
	h.SetEnv("CAAM_HOME", filepath.Join(rootDir, "caam"))
	h.SetEnv("CODEX_HOME", codexHome)
	h.SetEnv("MOCK_RUN_MODE", "codex_rate_limit_resume_continue")
	h.SetEnv("MOCK_COUNTER_FILE", counterFile)
	h.SetEnv("MOCK_AUTH_PATH", liveAuthPath)
	h.SetEnv("MOCK_EXPECT_RESUME_ID", sessionID)
	h.SetEnv("MOCK_CONTINUATION_FILE", continuationFile)
	h.SetEnv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0755))
	require.NoError(t, os.MkdirAll(codexHome, 0755))
	require.NoError(t, os.MkdirAll(profilesDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"token":"active"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"token":"backup"}`), 0600))
	require.NoError(t, os.WriteFile(liveAuthPath, []byte(`{"token":"active"}`), 0600))

	codexScript := `#!/usr/bin/env python3
import os
import sys
import time

counter_file = os.getenv("MOCK_COUNTER_FILE", "").strip()
auth_path = os.getenv("MOCK_AUTH_PATH", "").strip()
expected_session = os.getenv("MOCK_EXPECT_RESUME_ID", "").strip()
continuation_file = os.getenv("MOCK_CONTINUATION_FILE", "").strip()

count = 0
if counter_file:
    try:
        with open(counter_file, "r", encoding="utf-8") as fh:
            count = int(fh.read().strip() or "0")
    except FileNotFoundError:
        count = 0
count += 1
if counter_file:
    with open(counter_file, "w", encoding="utf-8") as fh:
        fh.write(str(count))

if count == 1:
    print("Processing...", flush=True)
    time.sleep(1.0)
    print("■ You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again", flush=True)
    print("at 3:05 PM.", flush=True)
    print(f"To continue this session, run codex resume {expected_session}", flush=True)
    deadline = time.time() + 15.0
    while time.time() < deadline:
        if auth_path:
            try:
                with open(auth_path, "r", encoding="utf-8") as fh:
                    if fh.read().strip() == "{\"token\":\"backup\"}":
                        break
            except FileNotFoundError:
                pass
        time.sleep(0.05)
    time.sleep(0.5)
    sys.exit(1)

joined_args = " ".join(sys.argv[1:])
if expected_session and f"resume {expected_session}" not in joined_args:
    print(f"Missing expected resume args: {expected_session}", flush=True)
    sys.exit(1)
if auth_path:
    with open(auth_path, "r", encoding="utf-8") as fh:
        auth_text = fh.read().strip()
    if auth_text != '{"token":"backup"}':
        print(f"Expected switched backup auth, got: {auth_text}", flush=True)
        sys.exit(1)
print("Resumed session idle, awaiting continuation input", flush=True)
continuation = sys.stdin.readline().strip()
if continuation_file:
    with open(continuation_file, "w", encoding="utf-8") as fh:
        fh.write(continuation)
if "continue exactly where you left off" not in continuation.lower():
    print(f"Unexpected continuation prompt: {continuation}", flush=True)
    sys.exit(1)
print("Resumed session continued on switched profile", flush=True)
sys.exit(0)
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "codex"), []byte(codexScript), 0755))

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	originalRegistry := registry
	originalGetWd := getWd
	originalProfileStore := profileStore
	originalRunner := runner
	defer func() {
		vault = originalVault
		tools = originalTools
		registry = originalRegistry
		getWd = originalGetWd
		profileStore = originalProfileStore
		runner = originalRunner
		_ = runCmd.Flags().Set("quiet", "false")
		_ = runCmd.Flags().Set("algorithm", "smart")
		_ = runCmd.Flags().Set("cooldown", "1h")
		_ = runCmd.Flags().Set("max-retries", "1")
	}()

	vault = authfile.NewVault(vaultDir)
	tools["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "codex",
			Files: []authfile.AuthFileSpec{
				{Tool: "codex", Path: liveAuthPath, Required: true},
			},
		}
	}
	registry = provider.NewRegistry()
	registry.Register(&MockProvider{id: "codex"})
	profileStore = profile.NewStore(profilesDir)
	getWd = func() (string, error) { return rootDir, nil }
	runner = nil

	_, err := profileStore.Create("codex", "active", "oauth")
	require.NoError(t, err)
	_, err = profileStore.Create("codex", "backup", "oauth")
	require.NoError(t, err)

	require.NoError(t, runCmd.Flags().Set("quiet", "true"))
	require.NoError(t, runCmd.Flags().Set("algorithm", "round_robin"))
	require.NoError(t, runCmd.Flags().Set("cooldown", "30m"))
	require.NoError(t, runCmd.Flags().Set("max-retries", "2"))

	err = runWrap(runCmd, []string{"codex", "--model", "gpt-5.4", "continue working"})
	require.NoError(t, err)

	countData, readErr := os.ReadFile(counterFile)
	require.NoError(t, readErr)
	require.Equal(t, "2", strings.TrimSpace(string(countData)), "expected one initial run plus one seamless resume")

	continuationData, continuationErr := os.ReadFile(continuationFile)
	require.NoError(t, continuationErr)
	continuationText := strings.ToLower(strings.TrimSpace(string(continuationData)))
	require.Contains(t, continuationText, "continue exactly where you left off", "expected seamless continuation prompt to be injected after resume")

	activeProfile, activeErr := vault.ActiveProfile(tools["codex"]())
	require.NoError(t, activeErr)
	require.Equal(t, "backup", activeProfile, "expected switched backup profile to remain active after seamless resume")

	authData, authErr := os.ReadFile(liveAuthPath)
	require.NoError(t, authErr)
	require.Equal(t, `{"token":"backup"}`, strings.TrimSpace(string(authData)), "expected live auth file to stay on backup profile")
}

func TestRunWrap_CodexExplicitResumeBypassesLockedProfileFallback(t *testing.T) {
	resetCommandTreeForExecute(rootCmd)
	defer resetCommandTreeForExecute(rootCmd)

	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	codexHome := filepath.Join(rootDir, ".codex")
	profilesDir := filepath.Join(rootDir, "caam", "profiles")
	liveAuthPath := filepath.Join(codexHome, "auth.json")
	sessionID := "019cd767-e334-7162-a2c3-80699b6dd4bd"

	h.SetEnv("HOME", rootDir)
	h.SetEnv("XDG_DATA_HOME", rootDir)
	h.SetEnv("XDG_CONFIG_HOME", rootDir)
	h.SetEnv("CAAM_HOME", filepath.Join(rootDir, "caam"))
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
	require.NoError(t, os.MkdirAll(codexHome, 0o755))
	require.NoError(t, os.MkdirAll(profilesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"token":"active"}`), 0o600))
	require.NoError(t, os.WriteFile(liveAuthPath, []byte(`{"token":"active"}`), 0o600))

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	originalRegistry := registry
	originalGetWd := getWd
	originalProfileStore := profileStore
	originalRunner := runner
	originalExecCommand := caamexec.ExecCommand
	defer func() {
		vault = originalVault
		tools = originalTools
		registry = originalRegistry
		getWd = originalGetWd
		profileStore = originalProfileStore
		runner = originalRunner
		caamexec.ExecCommand = originalExecCommand
		_ = runCmd.Flags().Set("quiet", "false")
		_ = runCmd.Flags().Set("algorithm", "smart")
		_ = runCmd.Flags().Set("cooldown", "1h")
		_ = runCmd.Flags().Set("max-retries", "1")
	}()

	vault = authfile.NewVault(vaultDir)
	tools["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "codex",
			Files: []authfile.AuthFileSpec{
				{Tool: "codex", Path: liveAuthPath, Required: true},
			},
		}
	}
	registry = provider.NewRegistry()
	registry.Register(&MockProvider{id: "codex"})
	profileStore = profile.NewStore(profilesDir)
	getWd = func() (string, error) { return rootDir, nil }
	runner = nil

	caamexec.ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestHelperProcess_Run$", "--", name}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "MOCK_RUN_MODE=success")
		return cmd
	}

	activeProfile, err := profileStore.Create("codex", "active", "oauth")
	require.NoError(t, err)
	require.NoError(t, activeProfile.LockWithCleanup())
	defer func() {
		if err := activeProfile.Unlock(); err != nil && !os.IsNotExist(err) {
			t.Fatalf("Unlock() error = %v", err)
		}
	}()

	require.NoError(t, runCmd.Flags().Set("quiet", "true"))
	require.NoError(t, runCmd.Flags().Set("algorithm", "round_robin"))
	require.NoError(t, runCmd.Flags().Set("cooldown", "30m"))
	require.NoError(t, runCmd.Flags().Set("max-retries", "2"))

	err = runWrap(runCmd, []string{"codex", "resume", sessionID})
	require.NoError(t, err, "explicit codex resume should not be blocked by CAAM profile locks")
}

func TestRunWrap_CodexRepairsLiveConfigBeforeLaunch(t *testing.T) {
	resetCommandTreeForExecute(rootCmd)
	defer resetCommandTreeForExecute(rootCmd)

	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	codexHome := filepath.Join(rootDir, ".codex")
	profilesDir := filepath.Join(rootDir, "caam", "profiles")
	liveAuthPath := filepath.Join(codexHome, "auth.json")
	canonicalDir := filepath.Join(rootDir, "canonical")
	configPath := filepath.Join(codexHome, "config.toml")
	canonicalConfigPath := filepath.Join(canonicalDir, "config.toml")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("XDG_DATA_HOME", rootDir)
	h.SetEnv("XDG_CONFIG_HOME", rootDir)
	h.SetEnv("CAAM_HOME", filepath.Join(rootDir, "caam"))
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
	require.NoError(t, os.MkdirAll(codexHome, 0o755))
	require.NoError(t, os.MkdirAll(profilesDir, 0o755))
	require.NoError(t, os.MkdirAll(canonicalDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"token":"active"}`), 0o600))
	require.NoError(t, os.WriteFile(liveAuthPath, []byte(`{"token":"active"}`), 0o600))
	require.NoError(t, os.WriteFile(canonicalConfigPath, []byte("model_reasoning_effort = \"high\"\ncli_auth_credentials_store = \"keychain\"\n"), 0o600))
	require.NoError(t, os.Symlink(canonicalConfigPath, configPath))

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	originalRegistry := registry
	originalGetWd := getWd
	originalProfileStore := profileStore
	originalRunner := runner
	originalExecCommand := caamexec.ExecCommand
	defer func() {
		vault = originalVault
		tools = originalTools
		registry = originalRegistry
		getWd = originalGetWd
		profileStore = originalProfileStore
		runner = originalRunner
		caamexec.ExecCommand = originalExecCommand
		_ = runCmd.Flags().Set("quiet", "false")
		_ = runCmd.Flags().Set("algorithm", "smart")
		_ = runCmd.Flags().Set("cooldown", "1h")
		_ = runCmd.Flags().Set("max-retries", "1")
	}()

	vault = authfile.NewVault(vaultDir)
	tools["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "codex",
			Files: []authfile.AuthFileSpec{
				{Tool: "codex", Path: liveAuthPath, Required: true},
			},
		}
	}
	registry = provider.NewRegistry()
	registry.Register(&MockProvider{id: "codex"})
	profileStore = profile.NewStore(profilesDir)
	getWd = func() (string, error) { return rootDir, nil }
	runner = nil

	caamexec.ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestHelperProcess_Run$", "--", name}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "MOCK_RUN_MODE=success")
		return cmd
	}

	_, err := profileStore.Create("codex", "active", "oauth")
	require.NoError(t, err)

	require.NoError(t, runCmd.Flags().Set("quiet", "true"))
	require.NoError(t, runCmd.Flags().Set("algorithm", "round_robin"))
	require.NoError(t, runCmd.Flags().Set("cooldown", "30m"))
	require.NoError(t, runCmd.Flags().Set("max-retries", "2"))

	err = runWrap(runCmd, []string{"codex", "--model", "gpt-5.4"})
	require.NoError(t, err)

	configInfo, err := os.Lstat(configPath)
	require.NoError(t, err)
	require.NotZero(t, configInfo.Mode()&os.ModeSymlink, "run should preserve a symlinked config.toml")

	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	configText := string(configData)
	require.Contains(t, configText, `model_reasoning_effort = "high"`)
	require.Contains(t, configText, `cli_auth_credentials_store = "file"`)
	require.Contains(t, configText, "[features]")
	require.Contains(t, configText, `multi_agent = true`)
	require.Contains(t, configText, "[notice]")
	require.Contains(t, configText, `hide_rate_limit_model_nudge = true`)
	require.NotContains(t, configText, `cli_auth_credentials_store = "keychain"`)
}

func TestIsProfileLockedErr(t *testing.T) {
	if !isProfileLockedErr(fmt.Errorf("lock profile: profile demo is already locked")) {
		t.Fatal("expected lock error to be detected")
	}
	if isProfileLockedErr(fmt.Errorf("other error")) {
		t.Fatal("expected non-lock error to be ignored")
	}
}
