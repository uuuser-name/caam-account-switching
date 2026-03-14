package cmd

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentpkg "github.com/Dicklesworthstone/coding_agent_account_manager/internal/agent"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authpool"
	"github.com/spf13/cobra"
)

type poolAgentTestEnv struct {
	caamHome string
	vault    *authfile.Vault
}

func setupPoolAgentTestEnv(t *testing.T) poolAgentTestEnv {
	t.Helper()

	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	xdgConfigHome := filepath.Join(root, "xdg-config")
	xdgDataHome := filepath.Join(root, "xdg-data")
	caamHome := filepath.Join(root, "caam-home")

	for _, dir := range []string{homeDir, xdgConfigHome, xdgDataHome, caamHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	t.Setenv("CAAM_HOME", caamHome)

	return poolAgentTestEnv{
		caamHome: caamHome,
		vault:    authfile.NewVault(authfile.DefaultVaultPath()),
	}
}

func writePoolAgentVaultProfile(t *testing.T, env poolAgentTestEnv, tool, profile string) {
	t.Helper()

	profileDir := env.vault.ProfilePath(tool, profile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", profileDir, err)
	}
	authPath := filepath.Join(profileDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"access_token":"test"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", authPath, err)
	}
}

func writePoolAgentState(t *testing.T, env poolAgentTestEnv, mutate func(*authpool.AuthPool)) {
	t.Helper()

	pool := authpool.NewAuthPool(authpool.WithVault(env.vault))
	mutate(pool)
	if err := pool.Save(authpool.PersistOptions{}); err != nil {
		t.Fatalf("pool.Save() error = %v", err)
	}
}

func newPoolFlagCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("provider", "", "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Duration("timeout", 30*time.Second, "")
	return cmd
}

func installAgentGlobals(t *testing.T) {
	t.Helper()

	oldPort := agentPort
	oldCoordinator := agentCoordinator
	oldAccounts := append([]string(nil), agentAccounts...)
	oldStrategy := agentStrategy
	oldChromeProfile := agentChromeProfile
	oldHeadless := agentHeadless
	oldVerbose := agentVerbose
	oldConfigPath := agentConfigPath

	t.Cleanup(func() {
		agentPort = oldPort
		agentCoordinator = oldCoordinator
		agentAccounts = oldAccounts
		agentStrategy = oldStrategy
		agentChromeProfile = oldChromeProfile
		agentHeadless = oldHeadless
		agentVerbose = oldVerbose
		agentConfigPath = oldConfigPath
	})
}

func writeAgentConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent-config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func TestPoolCommandsUseVaultAndPersistedState(t *testing.T) {
	env := setupPoolAgentTestEnv(t)

	writePoolAgentVaultProfile(t, env, "codex", "ready")
	writePoolAgentVaultProfile(t, env, "codex", "cooldown")
	writePoolAgentVaultProfile(t, env, "claude", "broken")

	writePoolAgentState(t, env, func(pool *authpool.AuthPool) {
		pool.AddProfile("codex", "ready")
		pool.MarkRefreshed("codex", "ready", time.Now().Add(2*time.Hour))

		pool.AddProfile("codex", "cooldown")
		pool.SetCooldown("codex", "cooldown", 20*time.Minute)

		pool.AddProfile("claude", "broken")
		for i := 0; i < 3; i++ {
			pool.SetError("claude", "broken", io.ErrUnexpectedEOF)
		}
	})

	t.Run("status json", func(t *testing.T) {
		cmd := newPoolFlagCommand(t)
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatalf("Flags().Set(json) error = %v", err)
		}

		out, err := captureStdout(t, func() error {
			return runPoolStatus(cmd, nil)
		})
		if err != nil {
			t.Fatalf("runPoolStatus(json) error = %v", err)
		}

		var summary authpool.PoolSummary
		if err := json.Unmarshal([]byte(out), &summary); err != nil {
			t.Fatalf("json.Unmarshal(summary) error = %v\noutput=%s", err, out)
		}
		if summary.TotalProfiles != 3 {
			t.Fatalf("TotalProfiles = %d, want 3", summary.TotalProfiles)
		}
		if summary.ReadyCount != 1 || summary.CooldownCount != 1 || summary.ErrorCount != 1 {
			t.Fatalf("unexpected summary counts: %+v", summary)
		}
		if summary.ByProvider["codex"] != 2 || summary.ByProvider["claude"] != 1 {
			t.Fatalf("unexpected provider counts: %+v", summary.ByProvider)
		}
	})

	t.Run("status text", func(t *testing.T) {
		out, err := captureStdout(t, func() error {
			return runPoolStatus(newPoolFlagCommand(t), nil)
		})
		if err != nil {
			t.Fatalf("runPoolStatus(text) error = %v", err)
		}
		for _, want := range []string{
			"Pool Summary:",
			"Total profiles: 3",
			"Ready: 1",
			"Cooldown: 1",
			"Error: 1",
			"By Provider:",
			"By Status:",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("status output missing %q: %q", want, out)
			}
		}
	})

	t.Run("list filters and json", func(t *testing.T) {
		cmd := newPoolFlagCommand(t)
		if err := cmd.Flags().Set("provider", "codex"); err != nil {
			t.Fatalf("Flags().Set(provider) error = %v", err)
		}
		if err := cmd.Flags().Set("status", "cooldown"); err != nil {
			t.Fatalf("Flags().Set(status) error = %v", err)
		}

		out, err := captureStdout(t, func() error {
			return runPoolList(cmd, nil)
		})
		if err != nil {
			t.Fatalf("runPoolList(filtered text) error = %v", err)
		}
		if !strings.Contains(out, "cooldown") || !strings.Contains(out, "codex") {
			t.Fatalf("filtered list output missing cooldown codex row: %q", out)
		}
		if strings.Contains(out, "broken") || strings.Contains(out, "ready") {
			t.Fatalf("filtered list output unexpectedly included other profiles: %q", out)
		}

		jsonCmd := newPoolFlagCommand(t)
		if err := jsonCmd.Flags().Set("json", "true"); err != nil {
			t.Fatalf("Flags().Set(json) error = %v", err)
		}

		jsonOut, err := captureStdout(t, func() error {
			return runPoolList(jsonCmd, nil)
		})
		if err != nil {
			t.Fatalf("runPoolList(json) error = %v", err)
		}

		var profiles []authpool.PooledProfile
		if err := json.Unmarshal([]byte(jsonOut), &profiles); err != nil {
			t.Fatalf("json.Unmarshal(profiles) error = %v\noutput=%s", err, jsonOut)
		}
		if len(profiles) != 3 {
			t.Fatalf("len(profiles) = %d, want 3", len(profiles))
		}
	})
}

func TestPoolRefreshAndParseProfileArgBranches(t *testing.T) {
	_ = setupPoolAgentTestEnv(t)

	if provider, profile, err := parseProfileArg("codex/work"); err != nil || provider != "codex" || profile != "work" {
		t.Fatalf("parseProfileArg(valid) = (%q,%q,%v), want (codex,work,nil)", provider, profile, err)
	}
	for _, input := range []string{"work", "/bad", "bad/"} {
		if _, _, err := parseProfileArg(input); err == nil {
			t.Fatalf("parseProfileArg(%q) expected error", input)
		}
	}

	t.Run("missing target", func(t *testing.T) {
		err := runPoolRefresh(newPoolFlagCommand(t), nil)
		if err == nil || !strings.Contains(err.Error(), "specify a profile") {
			t.Fatalf("runPoolRefresh() error = %v, want missing-target error", err)
		}
	})

	t.Run("refresh all empty pool", func(t *testing.T) {
		cmd := newPoolFlagCommand(t)
		if err := cmd.Flags().Set("all", "true"); err != nil {
			t.Fatalf("Flags().Set(all) error = %v", err)
		}

		out, err := captureStdout(t, func() error {
			return runPoolRefresh(cmd, nil)
		})
		if err != nil {
			t.Fatalf("runPoolRefresh(--all) error = %v", err)
		}
		if !strings.Contains(out, "Refreshing all profiles...") || !strings.Contains(out, "Refresh triggered for all profiles") {
			t.Fatalf("unexpected --all output: %q", out)
		}
	})

	t.Run("missing profile prints error", func(t *testing.T) {
		out, err := captureStdout(t, func() error {
			return runPoolRefresh(newPoolFlagCommand(t), []string{"codex/missing"})
		})
		if err != nil {
			t.Fatalf("runPoolRefresh(missing profile) error = %v", err)
		}
		if !strings.Contains(out, "Refreshing codex/missing...") || !strings.Contains(out, "Error:") {
			t.Fatalf("unexpected refresh output: %q", out)
		}
	})

	t.Run("invalid profile arg", func(t *testing.T) {
		err := runPoolRefresh(newPoolFlagCommand(t), []string{"bad"})
		if err == nil || !strings.Contains(err.Error(), "expected provider/profile") {
			t.Fatalf("runPoolRefresh(invalid arg) error = %v, want provider/profile error", err)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		out, err := captureStdout(t, func() error {
			return runPoolList(newPoolFlagCommand(t), nil)
		})
		if err != nil {
			t.Fatalf("runPoolList(empty) error = %v", err)
		}
		if !strings.Contains(out, "No profiles in pool") {
			t.Fatalf("unexpected empty pool output: %q", out)
		}
	})
}

func TestLoadAgentConfigVariants(t *testing.T) {
	t.Run("single config", func(t *testing.T) {
		path := writeAgentConfigFile(t, `{
  "port": 9001,
  "coordinator": "http://example.test:7890",
  "poll_interval": "3s",
  "chrome_profile": "/tmp/chrome-profile",
  "headless": true,
  "strategy": "round_robin",
  "accounts": ["alpha@example.com", "beta@example.com"]
}`)

		useMulti, singleCfg, multiCfg, err := loadAgentConfig(path)
		if err != nil {
			t.Fatalf("loadAgentConfig(single) error = %v", err)
		}
		if useMulti {
			t.Fatal("expected single config, got multi")
		}
		if singleCfg.Port != 9001 || singleCfg.CoordinatorURL != "http://example.test:7890" {
			t.Fatalf("unexpected single config basics: %+v", singleCfg)
		}
		if singleCfg.PollInterval != 3*time.Second || singleCfg.ChromeUserDataDir != "/tmp/chrome-profile" {
			t.Fatalf("unexpected single config timing/profile: %+v", singleCfg)
		}
		if singleCfg.AccountStrategy != agentpkg.StrategyRoundRobin || len(singleCfg.Accounts) != 2 {
			t.Fatalf("unexpected single config strategy/accounts: %+v", singleCfg)
		}
		if multiCfg.Port != 0 || multiCfg.PollInterval != 0 || multiCfg.ChromeUserDataDir != "" ||
			multiCfg.Headless || multiCfg.AccountStrategy != "" || len(multiCfg.Accounts) != 0 ||
			len(multiCfg.Coordinators) != 0 {
			t.Fatalf("expected zero multi config, got %+v", multiCfg)
		}
	})

	t.Run("multi config", func(t *testing.T) {
		path := writeAgentConfigFile(t, `{
  "poll_interval": "5s",
  "chrome_user_data_dir": "/tmp/multi-profile",
  "strategy": "random",
  "accounts": ["multi@example.com"],
  "coordinators": [
    {
      "name": "local",
      "url": "http://127.0.0.1:1",
      "display_name": "Local"
    }
  ]
}`)

		useMulti, _, multiCfg, err := loadAgentConfig(path)
		if err != nil {
			t.Fatalf("loadAgentConfig(multi) error = %v", err)
		}
		if !useMulti {
			t.Fatal("expected multi config, got single")
		}
		if multiCfg.PollInterval != 5*time.Second || multiCfg.ChromeUserDataDir != "/tmp/multi-profile" {
			t.Fatalf("unexpected multi config timing/profile: %+v", multiCfg)
		}
		if multiCfg.AccountStrategy != agentpkg.StrategyRandom || len(multiCfg.Coordinators) != 1 {
			t.Fatalf("unexpected multi config strategy/coordinators: %+v", multiCfg)
		}
	})

	t.Run("invalid duration", func(t *testing.T) {
		path := writeAgentConfigFile(t, `{"poll_interval":"definitely-not-a-duration"}`)
		if _, _, _, err := loadAgentConfig(path); err == nil || !strings.Contains(err.Error(), "parse poll_interval") {
			t.Fatalf("loadAgentConfig(invalid duration) error = %v, want parse error", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		path := writeAgentConfigFile(t, `{`)
		if _, _, _, err := loadAgentConfig(path); err == nil || !strings.Contains(err.Error(), "parse config") {
			t.Fatalf("loadAgentConfig(invalid json) error = %v, want parse error", err)
		}
	})
}

func TestRunAgentBranchesStopOnContextCancel(t *testing.T) {
	_ = setupPoolAgentTestEnv(t)
	installAgentGlobals(t)

	t.Run("inline flags", func(t *testing.T) {
		agentPort = 0
		agentCoordinator = ""
		agentAccounts = []string{"alpha@example.com", "beta@example.com"}
		agentStrategy = "lru"
		agentChromeProfile = filepath.Join(t.TempDir(), "chrome-profile")
		agentHeadless = true
		agentVerbose = true
		agentConfigPath = ""

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(150*time.Millisecond, cancel)

		cmd := &cobra.Command{}
		cmd.SetContext(ctx)

		out, err := captureStdout(t, func() error {
			return runAgent(cmd, nil)
		})
		if err != nil {
			t.Fatalf("runAgent(inline) error = %v", err)
		}
		for _, want := range []string{
			"Auth agent started",
			"Strategy: lru",
			"Accounts: [alpha@example.com beta@example.com]",
			"Chrome profile:",
			"Agent stopped.",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("runAgent output missing %q: %q", want, out)
			}
		}
	})

	t.Run("config path multi", func(t *testing.T) {
		agentPort = 0
		agentCoordinator = ""
		agentAccounts = nil
		agentStrategy = "lru"
		agentChromeProfile = ""
		agentHeadless = false
		agentVerbose = false
		agentConfigPath = writeAgentConfigFile(t, `{
  "port": 0,
  "poll_interval": "50ms",
  "chrome_profile": "/tmp/agent-multi",
  "accounts": ["multi@example.com"],
  "coordinators": [
    {
      "name": "local",
      "url": "http://127.0.0.1:1",
      "display_name": "Local"
    }
  ]
}`)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(150*time.Millisecond, cancel)

		cmd := &cobra.Command{}
		cmd.SetContext(ctx)

		out, err := captureStdout(t, func() error {
			return runAgent(cmd, nil)
		})
		if err != nil {
			t.Fatalf("runAgent(config multi) error = %v", err)
		}
		for _, want := range []string{
			"Auth agent started (multi-coordinator)",
			"Coordinators: 1",
			"Strategy: lru",
			"Accounts: [multi@example.com]",
			"Agent stopped.",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("runAgent(config multi) output missing %q: %q", want, out)
			}
		}
	})

	t.Run("config path single", func(t *testing.T) {
		agentPort = 0
		agentCoordinator = ""
		agentAccounts = nil
		agentStrategy = "lru"
		agentChromeProfile = ""
		agentHeadless = false
		agentVerbose = false
		agentConfigPath = writeAgentConfigFile(t, `{
  "port": 0,
  "coordinator_url": "http://127.0.0.1:1",
  "poll_interval": "50ms",
  "chrome_profile": "/tmp/agent-single",
  "accounts": ["single@example.com"],
  "strategy": "round_robin"
}`)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(150*time.Millisecond, cancel)

		cmd := &cobra.Command{}
		cmd.SetContext(ctx)

		out, err := captureStdout(t, func() error {
			return runAgent(cmd, nil)
		})
		if err != nil {
			t.Fatalf("runAgent(config single) error = %v", err)
		}
		for _, want := range []string{
			"Auth agent started",
			"Coordinator: http://127.0.0.1:1",
			"Strategy: round_robin",
			"Accounts: [single@example.com]",
			"Agent stopped.",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("runAgent(config single) output missing %q: %q", want, out)
			}
		}
	})

	t.Run("invalid strategy", func(t *testing.T) {
		agentPort = 0
		agentCoordinator = ""
		agentAccounts = nil
		agentStrategy = "bogus"
		agentChromeProfile = ""
		agentHeadless = false
		agentVerbose = false
		agentConfigPath = ""

		err := runAgent(&cobra.Command{}, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown strategy") {
			t.Fatalf("runAgent(invalid strategy) error = %v, want unknown-strategy error", err)
		}
	})
}

func TestRunAgentFromConfigMissingCoordinator(t *testing.T) {
	_ = setupPoolAgentTestEnv(t)

	path := writeAgentConfigFile(t, `{"port":0,"poll_interval":"10ms"}`)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	err := runAgentFromConfig(&cobra.Command{}, logger, path)
	if err == nil || !strings.Contains(err.Error(), "config missing coordinator URL") {
		t.Fatalf("runAgentFromConfig(missing coordinator) error = %v, want missing-coordinator error", err)
	}
}
