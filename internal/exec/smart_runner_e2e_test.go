package exec

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
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/notify"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockCLI_Handoff simulates a CLI tool that hits a rate limit and then accepts login.
func TestMockCLI_Handoff(t *testing.T) {
	if os.Getenv("GO_WANT_MOCK_CLI") != "1" {
		return
	}
	mode := strings.TrimSpace(os.Getenv("MOCK_CLI_MODE"))
	if mode == "" {
		mode = "handoff_success"
	}

	if mode == "rate_limit_exit" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("Error: rate limit exceeded")
		os.Exit(1)
		return
	}
	if mode == "refresh_token_reused_exit" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("To continue this session, run codex resume 019b2e3d-b524-7c22-91da-47de9068d09a")
		fmt.Println("Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.")
		os.Exit(1)
		return
	}
	if mode == "rate_limit_exit_zero" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("Error: rate limit exceeded")
		// Exit success to simulate CLIs that print limit errors but still return 0.
		return
	}
	if mode == "refresh_token_reused_exit_zero" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("To continue this session, run codex resume 019b2e3d-b524-7c22-91da-47de9068d09a")
		fmt.Println("Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.")
		// Exit success to ensure SmartRunner still reports handoff failure when
		// auth recovery requires interactive login.
		return
	}
	if mode == "rate_limit_exit_then_resume_success" {
		counterFile := strings.TrimSpace(os.Getenv("MOCK_CLI_COUNTER_FILE"))
		expectedSessionID := strings.TrimSpace(os.Getenv("MOCK_EXPECT_RESUME_ID"))
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
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("To continue this session, run codex resume %s\n", expectedSessionID)
			fmt.Println("Error: rate limit exceeded")
			os.Exit(1)
			return
		}

		if expectedSessionID != "" {
			joinedArgs := strings.Join(os.Args, " ")
			if !strings.Contains(joinedArgs, "resume "+expectedSessionID) {
				fmt.Printf("Missing expected resume args: %s\n", expectedSessionID)
				os.Exit(1)
				return
			}
		}

		fmt.Println("Resumed session OK")
		return
	}
	if mode == "codex_rate_limit_no_login_needed" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("Error: out of credits")

		// Simulate a CLI that can continue after auth swap without interactive re-login.
		lineCh := make(chan string, 1)
		go func() {
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			lineCh <- line
		}()

		select {
		case line := <-lineCh:
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				fmt.Printf("Unexpected command during quota handoff: %s\n", trimmed)
				os.Exit(1)
			}
		case <-time.After(500 * time.Millisecond):
			// No input is expected; continue success path.
		}

		fmt.Println("Continuing after auth swap")
		time.Sleep(100 * time.Millisecond)
		return
	}
	if mode == "double_handoff_success" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("Error: rate limit exceeded")

		reader := bufio.NewReader(os.Stdin)
		for i := 0; i < 2; i++ {
			line, _ := reader.ReadString('\n')
			if strings.TrimSpace(line) != "/login" {
				fmt.Printf("Unknown command: %s", line)
				os.Exit(1)
			}
			fmt.Println("Logging in...")
			time.Sleep(100 * time.Millisecond)
			fmt.Println("successfully logged in")

			if i == 0 {
				time.Sleep(100 * time.Millisecond)
				fmt.Println("Error: rate limit exceeded")
			}
		}
		time.Sleep(300 * time.Millisecond)
		return
	}

	// 1. Output rate limit message
	fmt.Println("Processing...")
	time.Sleep(100 * time.Millisecond)
	fmt.Println("Error: rate limit exceeded")

	// 2. Wait for login command
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')

	if strings.TrimSpace(line) == "/login" {
		fmt.Println("Logging in...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("successfully logged in")
	} else {
		fmt.Printf("Unknown command: %s", line)
		os.Exit(1)
	}

	// Keep running a bit
	time.Sleep(500 * time.Millisecond)
}

type MockNotifier struct {
	Alerts []*notify.Alert
}

func (m *MockNotifier) Notify(alert *notify.Alert) error {
	m.Alerts = append(m.Alerts, alert)
	return nil
}
func (m *MockNotifier) Name() string    { return "mock" }
func (m *MockNotifier) Available() bool { return true }

func TestSmartRunner_E2E(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Initialize environment")
	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")

	// Setup profiles
	// Profile 1 (Current): "active"
	// Profile 2 (Backup): "backup"
	createProfile := func(name string) {
		dir := filepath.Join(vaultDir, "claude", name)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude.json"), []byte("{}"), 0600))
	}
	createProfile("active")
	createProfile("backup")

	vault := authfile.NewVault(vaultDir)

	// Setup Temp DB
	dbPath := filepath.Join(rootDir, "caam.db")
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Mock ExecCommand
	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		fmt.Println("DEBUG: ExecCommand called")
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_MOCK_CLI=1")
		return cmd
	}

	// Setup SmartRunner
	cfg := config.DefaultSPMConfig().Handoff
	notifier := &MockNotifier{}

	// Need mock provider registry?
	// SmartRunner.Run uses opts.Provider.
	// We need a provider that returns "claude" ID.
	// We can use the real Claude provider, but we need to ensure it uses our mocked paths?
	// `DetectExistingAuth` etc. are not used by `SmartRunner.Run` directly, only `Runner.Run`.
	// `Runner.Run` calls `opts.Provider.Env`.
	// `internal/provider/claude/claude.go` implements `Env`.

	// Using a mock provider is safer to avoid file system dependency issues.
	mockProv := &MockProvider{id: "claude"} // Reuse MockProvider if exported or define locally

	// SmartRunner needs rotation selector
	// Selector needs health store and db
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)

	runner := &Runner{}

	opts := SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notifier,
	}

	sr := NewSmartRunner(runner, opts)

	// Prepare RunOptions
	prof, err := profile.NewStore(filepath.Join(rootDir, "profiles")).Create("claude", "active", "oauth")
	require.NoError(t, err)

	runOpts := RunOptions{
		Profile:  prof,
		Provider: mockProv,
		Args:     []string{},
		Env:      map[string]string{"GO_WANT_MOCK_CLI": "1"},
	}

	h.EndStep("Setup")

	// 2. Run
	h.StartStep("Run", "Execute SmartRunner")

	// Run should:
	// 1. Start mock CLI
	// 2. Detect "rate limit exceeded"
	// 3. Trigger handoff
	// 4. Select "backup"
	// 5. Swap auth (mocked vault works on temp dir)
	// 6. Inject "/login"
	// 7. Detect "successfully logged in"
	// 8. Notify user

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = sr.Run(ctx, runOpts)
	require.NoError(t, err)

	h.EndStep("Run")

	// 3. Verify
	h.StartStep("Verify", "Check state and notifications")

	assert.Equal(t, "backup", sr.currentProfile)
	assert.Equal(t, 1, sr.handoffCount)

	// Check notifications
	require.NotEmpty(t, notifier.Alerts)
	foundSwitch := false
	for _, a := range notifier.Alerts {
		if strings.Contains(a.Message, "Switched to backup") {
			foundSwitch = true
		}
	}
	assert.True(t, foundSwitch, "Did not notify about switch")

	// Check DB for Activation Event
	activations, err := db.GetEvents("claude", "active", time.Now().Add(-1*time.Hour), 10)
	require.NoError(t, err)
	assert.NotEmpty(t, activations, "Should have logged activation event")
	assert.Equal(t, caamdb.EventActivate, activations[0].Type)

	// Check DB for Wrap Session
	sessions, err := db.GetWrapSessions("claude", time.Now().Add(-1*time.Hour), 10)
	require.NoError(t, err)
	assert.NotEmpty(t, sessions, "Should have recorded wrap session")
	assert.Equal(t, "backup", sessions[0].ProfileName, "Session should record final profile")
	assert.True(t, sessions[0].RateLimitHit, "Session should mark rate limit hit")

	h.EndStep("Verify")
}

func TestSmartRunner_E2E_RateLimitExitPreservesSwitchedProfile(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	h.SetEnv("HOME", rootDir)
	h.SetEnv("XDG_CONFIG_HOME", filepath.Join(rootDir, ".config"))

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "backup"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "claude", "active", ".claude.json"), []byte(`{"profile":"active"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "claude", "backup", ".claude.json"), []byte(`{"profile":"backup"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, ".claude.json"), []byte(`{"profile":"active"}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=rate_limit_exit",
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 3
	notifier := &MockNotifier{}
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notifier,
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("claude", "active", "oauth")
	require.NoError(t, err)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "claude"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
			"MOCK_CLI_MODE":    "rate_limit_exit",
		},
	})

	// The wrapped command exits non-zero; the key behavior is that CAAM fails fast
	// and keeps the switched profile active instead of rolling back to exhausted auth.
	var exitErr *ExitCodeError
	require.Error(t, runErr)
	require.ErrorAs(t, runErr, &exitErr)
	require.Equal(t, 1, exitErr.Code)

	elapsed := time.Since(start)
	require.Less(t, elapsed, 12*time.Second, "handoff should fail fast when session exits (no 30s login timeout)")

	assert.Equal(t, "backup", sr.currentProfile, "switched profile should be preserved after failed handoff")
	activeProfile, activeErr := vault.ActiveProfile(authfile.ClaudeAuthFiles())
	require.NoError(t, activeErr)
	assert.Equal(t, "backup", activeProfile, "vault active profile should remain on backup (no rollback)")
}

func TestSmartRunner_E2E_CodexRefreshTokenReusedTriggersSwitch(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0755))
	require.NoError(t, os.MkdirAll(codexHome, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"profile":"active"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"profile":"backup"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"profile":"active"}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=refresh_token_reused_exit",
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 3
	notifier := &MockNotifier{}
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notifier,
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("codex", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "codex"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
			"MOCK_CLI_MODE":    "refresh_token_reused_exit",
		},
	})

	var exitErr *ExitCodeError
	require.Error(t, runErr)
	require.ErrorAs(t, runErr, &exitErr)
	require.Equal(t, 1, exitErr.Code)

	assert.Equal(t, "backup", sr.currentProfile, "switched profile should be preserved after refresh-token failure")
	activeProfile, activeErr := vault.ActiveProfile(authfile.CodexAuthFiles())
	require.NoError(t, activeErr)
	assert.Equal(t, "backup", activeProfile, "vault active profile should remain on backup after terminal auth failure")

	foundResumeHint := false
	for _, alert := range notifier.Alerts {
		if strings.Contains(alert.Action, "codex resume") {
			foundResumeHint = true
			break
		}
	}
	assert.True(t, foundResumeHint, "failure path should include resume hint when codex resume command is observed")
}

func TestSmartRunner_E2E_CodexRateLimitSwitchSkipsInteractiveLogin(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0755))
	require.NoError(t, os.MkdirAll(codexHome, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=codex_rate_limit_no_login_needed",
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 2
	notifier := &MockNotifier{}
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notifier,
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("codex", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "codex"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
			"MOCK_CLI_MODE":    "codex_rate_limit_no_login_needed",
		},
	})

	require.NoError(t, runErr)
	assert.Equal(t, "backup", sr.currentProfile, "runner should switch to backup profile")
	assert.Equal(t, 1, sr.handoffCount, "runner should complete exactly one handoff")

	activeProfile, activeErr := vault.ActiveProfile(authfile.CodexAuthFiles())
	require.NoError(t, activeErr)
	assert.Equal(t, "backup", activeProfile, "vault active profile should remain on backup")

	foundNoReloginMsg := false
	for _, alert := range notifier.Alerts {
		if strings.Contains(strings.ToLower(alert.Message), "no re-login needed") {
			foundNoReloginMsg = true
			break
		}
	}
	assert.True(t, foundNoReloginMsg, "expected success notification to mention no re-login path")
}

func TestSmartRunner_E2E_CodexFallsBackToSystemProfileWhenOnlyDistinctAlternative(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "_backup_distinct"), 0755))
	require.NoError(t, os.MkdirAll(codexHome, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "_backup_distinct", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=codex_rate_limit_no_login_needed",
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 2
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      &MockNotifier{},
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("codex", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "codex"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
			"MOCK_CLI_MODE":    "codex_rate_limit_no_login_needed",
		},
	})

	require.NoError(t, runErr)
	assert.Equal(t, "_backup_distinct", sr.currentProfile, "runner should fall back to system backup when it is the only distinct credential")
}

func TestSmartRunner_E2E_CodexRateLimitExitAutoResumesOnSwitchedProfile(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")
	counterFile := filepath.Join(rootDir, "resume_count.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"

	h.SetEnv("HOME", rootDir)
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0755))
	require.NoError(t, os.MkdirAll(codexHome, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=rate_limit_exit_then_resume_success",
			"MOCK_CLI_COUNTER_FILE="+counterFile,
			"MOCK_EXPECT_RESUME_ID="+sessionID,
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 2
	notifier := &MockNotifier{}
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notifier,
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("codex", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "codex"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI":      "1",
			"MOCK_CLI_MODE":         "rate_limit_exit_then_resume_success",
			"MOCK_CLI_COUNTER_FILE": counterFile,
			"MOCK_EXPECT_RESUME_ID": sessionID,
		},
	})

	require.NoError(t, runErr)
	assert.Equal(t, "backup", sr.currentProfile, "runner should preserve switched profile after seamless resume")
	assert.Equal(t, 1, sr.handoffCount, "runner should record one handoff before auto-resume")

	countData, readErr := os.ReadFile(counterFile)
	require.NoError(t, readErr)
	assert.Equal(t, "2", strings.TrimSpace(string(countData)), "mock command should run twice (initial + seamless resume)")

	activeProfile, activeErr := vault.ActiveProfile(authfile.CodexAuthFiles())
	require.NoError(t, activeErr)
	assert.Equal(t, "backup", activeProfile, "vault active profile should remain on backup after seamless resume")

	foundSwitchNotice := false
	for _, alert := range notifier.Alerts {
		if strings.Contains(alert.Message, "Switched to backup") {
			foundSwitchNotice = true
			break
		}
	}
	assert.True(t, foundSwitchNotice, "expected handoff notification before seamless resume")
}

func TestSmartRunner_E2E_MultiProfileChainUntilHealthy(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("XDG_CONFIG_HOME", filepath.Join(rootDir, ".config"))

	for _, profileName := range []string{"active", "backup1", "backup2"} {
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", profileName), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "claude", profileName, ".claude.json"), []byte(fmt.Sprintf(`{"profile":"%s"}`, profileName)), 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, ".claude.json"), []byte(`{"profile":"active"}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=double_handoff_success",
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 1 // Auto-expansion should still allow traversing all available profiles.
	notifier := &MockNotifier{}
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notifier,
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("claude", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "claude"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
			"MOCK_CLI_MODE":    "double_handoff_success",
		},
	})
	require.NoError(t, runErr)

	assert.Equal(t, "backup2", sr.currentProfile, "runner should advance across exhausted profiles until a healthy profile succeeds")
	assert.Equal(t, 2, sr.handoffCount, "runner should complete two handoffs in one session")
	activeProfile, activeErr := vault.ActiveProfile(authfile.ClaudeAuthFiles())
	require.NoError(t, activeErr)
	assert.Equal(t, "backup2", activeProfile, "final active profile should be the last healthy candidate")
}

func TestSmartRunner_E2E_HandoffFailureStillErrorsWhenWrappedCommandExitsZero(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	h.SetEnv("HOME", rootDir)
	h.SetEnv("CODEX_HOME", codexHome)

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0755))
	require.NoError(t, os.MkdirAll(codexHome, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"profile":"active"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"profile":"backup"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"profile":"active"}`), 0600))

	vault := authfile.NewVault(vaultDir)
	db, err := caamdb.OpenAt(dbPath)
	require.NoError(t, err)
	defer db.Close()

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()
	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=refresh_token_reused_exit_zero",
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 2
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      &MockNotifier{},
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("codex", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "codex"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
			"MOCK_CLI_MODE":    "refresh_token_reused_exit_zero",
		},
	})

	require.Error(t, runErr)
	require.Contains(t, runErr.Error(), "auto-handoff failed")
}

// Local MockProvider (minimal)
type MockProvider struct {
	id string
}

func (m *MockProvider) ID() string          { return m.id }
func (m *MockProvider) DisplayName() string { return "Mock" }
func (m *MockProvider) DefaultBin() string  { return "mock-bin" }
func (m *MockProvider) Env(ctx context.Context, p *profile.Profile) (map[string]string, error) {
	return nil, nil
}

// Other methods needed for interface compliance...
func (m *MockProvider) SupportedAuthModes() []provider.AuthMode                      { return nil }
func (m *MockProvider) AuthFiles() []provider.AuthFileSpec                           { return nil }
func (m *MockProvider) PrepareProfile(ctx context.Context, p *profile.Profile) error { return nil }
func (m *MockProvider) Login(ctx context.Context, p *profile.Profile) error          { return nil }
func (m *MockProvider) Logout(ctx context.Context, p *profile.Profile) error         { return nil }
func (m *MockProvider) Status(ctx context.Context, p *profile.Profile) (*provider.ProfileStatus, error) {
	return nil, nil
}
func (m *MockProvider) ValidateProfile(ctx context.Context, p *profile.Profile) error { return nil }
func (m *MockProvider) DetectExistingAuth() (*provider.AuthDetection, error)          { return nil, nil }
func (m *MockProvider) ImportAuth(ctx context.Context, s string, p *profile.Profile) ([]string, error) {
	return nil, nil
}
func (m *MockProvider) ValidateToken(ctx context.Context, p *profile.Profile, passive bool) (*provider.ValidationResult, error) {
	return nil, nil
}
