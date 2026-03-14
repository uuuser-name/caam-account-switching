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
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/runtimefixture"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

const (
	mockRateLimitExitPause = 8 * time.Second
	smartRunnerE2ETimeout  = 20 * time.Second
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
		os.Exit(0)
	}
	if mode == "refresh_token_reused_exit_zero" {
		fmt.Println("Processing...")
		time.Sleep(100 * time.Millisecond)
		fmt.Println("To continue this session, run codex resume 019b2e3d-b524-7c22-91da-47de9068d09a")
		fmt.Println("Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.")
		// Exit success to ensure SmartRunner still reports handoff failure when
		// auth recovery requires interactive login.
		os.Exit(0)
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
			time.Sleep(1 * time.Second)
			fmt.Println("■ You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again")
			fmt.Println("at 3:05 PM.")
			fmt.Printf("To continue this session, run codex resume %s\n", expectedSessionID)
			time.Sleep(mockRateLimitExitPause)
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
		os.Exit(0)
	}
	if mode == "rate_limit_exit_then_resume_requires_prompt" {
		counterFile := strings.TrimSpace(os.Getenv("MOCK_CLI_COUNTER_FILE"))
		expectedSessionID := strings.TrimSpace(os.Getenv("MOCK_EXPECT_RESUME_ID"))
		continuationFile := strings.TrimSpace(os.Getenv("MOCK_CONTINUATION_FILE"))
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
			time.Sleep(mockRateLimitExitPause)
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
			lower := strings.ToLower(trimmed)
			if !strings.Contains(lower, "continue exactly where you left off") {
				fmt.Printf("Unexpected continuation prompt: %s\n", trimmed)
				os.Exit(1)
				return
			}
			fmt.Println("Resumed session continued")
			os.Exit(0)
		case <-time.After(3 * time.Second):
			fmt.Println("Resumed session idle awaiting user input")
			os.Exit(1)
			return
		}
	}
	if mode == "rate_limit_exit_then_resume_requires_prompt_arg" {
		counterFile := strings.TrimSpace(os.Getenv("MOCK_CLI_COUNTER_FILE"))
		expectedSessionID := strings.TrimSpace(os.Getenv("MOCK_EXPECT_RESUME_ID"))
		continuationFile := strings.TrimSpace(os.Getenv("MOCK_CONTINUATION_FILE"))
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
			time.Sleep(mockRateLimitExitPause)
			os.Exit(1)
			return
		}

		joinedArgs := strings.Join(os.Args, " ")
		if expectedSessionID != "" && !strings.Contains(joinedArgs, "resume "+expectedSessionID) {
			fmt.Printf("Missing expected resume args: %s\n", expectedSessionID)
			os.Exit(1)
			return
		}
		if strings.Contains(strings.ToLower(joinedArgs), "continue exactly where you left off") {
			if continuationFile != "" {
				_ = os.WriteFile(continuationFile, []byte(joinedArgs), 0600)
			}
			fmt.Println("Resumed session continued from argv prompt")
			os.Exit(0)
		}
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
			fmt.Println("Resumed session continued from stdin prompt")
			os.Exit(0)
		case <-time.After(3 * time.Second):
			fmt.Println("Resumed session idle awaiting user input")
			os.Exit(1)
			return
		}
	}
	if mode == "rate_limit_exit_with_stale_duplicate_then_resume_prompt_arg" {
		counterFile := strings.TrimSpace(os.Getenv("MOCK_CLI_COUNTER_FILE"))
		expectedSessionID := strings.TrimSpace(os.Getenv("MOCK_EXPECT_RESUME_ID"))
		continuationFile := strings.TrimSpace(os.Getenv("MOCK_CONTINUATION_FILE"))
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
			time.Sleep(mockRateLimitExitPause)
			fmt.Println("■ You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again")
			os.Exit(1)
			return
		}

		joinedArgs := strings.Join(os.Args, " ")
		if expectedSessionID != "" && !strings.Contains(joinedArgs, "resume "+expectedSessionID) {
			fmt.Printf("Missing expected resume args: %s\n", expectedSessionID)
			os.Exit(1)
			return
		}
		if strings.Contains(strings.ToLower(joinedArgs), "continue exactly where you left off") {
			if continuationFile != "" {
				_ = os.WriteFile(continuationFile, []byte(joinedArgs), 0600)
			}
			fmt.Println("Resumed session continued after stale duplicate output")
			os.Exit(0)
		}
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
			fmt.Println("Resumed session continued after stale duplicate output")
			os.Exit(0)
		case <-time.After(3 * time.Second):
			fmt.Println("Resumed session idle awaiting user input")
			os.Exit(1)
			return
		}
	}
	if mode == "rate_limit_exit_zero_then_resume_requires_prompt" {
		counterFile := strings.TrimSpace(os.Getenv("MOCK_CLI_COUNTER_FILE"))
		expectedSessionID := strings.TrimSpace(os.Getenv("MOCK_EXPECT_RESUME_ID"))
		continuationFile := strings.TrimSpace(os.Getenv("MOCK_CONTINUATION_FILE"))
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
			_ = os.WriteFile(counterFile, []byte(strconv.Itoa(count)), 0o600)
		}
		if count == 1 {
			fmt.Println("Processing...")
			time.Sleep(100 * time.Millisecond)
			fmt.Println("■ You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again")
			fmt.Println("at 3:05 PM.")
			fmt.Printf("To continue this session, run codex resume %s\n", expectedSessionID)
			os.Exit(0)
		}
		joinedArgs := strings.Join(os.Args, " ")
		if expectedSessionID != "" && !strings.Contains(joinedArgs, "resume "+expectedSessionID) {
			fmt.Printf("Missing expected resume args: %s\n", expectedSessionID)
			os.Exit(1)
			return
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
				_ = os.WriteFile(continuationFile, []byte(trimmed), 0o600)
			}
			if !strings.Contains(strings.ToLower(trimmed), "continue exactly where you left off") {
				fmt.Printf("Unexpected continuation prompt: %s\n", trimmed)
				os.Exit(1)
				return
			}
			fmt.Println("Resumed session continued after exit-zero path")
			os.Exit(0)
		case <-time.After(3 * time.Second):
			fmt.Println("Resumed session idle awaiting user input")
			os.Exit(1)
			return
		}
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
		os.Exit(0)
	}
	if mode == "interactive_input_echo" {
		inputFile := strings.TrimSpace(os.Getenv("MOCK_CLI_INPUT_FILE"))
		fmt.Println("Ready for input")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if inputFile != "" {
			_ = os.WriteFile(inputFile, []byte(trimmed), 0o600)
		}
		fmt.Printf("Received: %s\n", trimmed)
		os.Exit(0)
	}
	if mode == "terminal_query_reply" {
		responseFile := strings.TrimSpace(os.Getenv("MOCK_CLI_RESPONSE_FILE"))
		if term.IsTerminal(int(os.Stdin.Fd())) {
			oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err == nil {
				defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
			}
		}
		fmt.Print("\x1b[6n")
		reader := bufio.NewReader(os.Stdin)
		reply, _ := reader.ReadString('R')
		if responseFile != "" {
			_ = os.WriteFile(responseFile, []byte(reply), 0o600)
		}
		fmt.Println("terminal reply received")
		os.Exit(0)
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
		os.Exit(0)
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
	os.Exit(0)
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

func writeSymlinkedCodexConfig(t *testing.T, rootDir, codexHome string) (string, string) {
	t.Helper()

	configPath := filepath.Join(codexHome, "config.toml")
	canonicalDir := filepath.Join(rootDir, "canonical-codex")
	canonicalConfigPath := filepath.Join(canonicalDir, "config.toml")

	require.NoError(t, os.MkdirAll(canonicalDir, 0o755))
	require.NoError(t, os.WriteFile(canonicalConfigPath, []byte("model_reasoning_effort = \"high\"\ncli_auth_credentials_store = \"keychain\"\n"), 0o600))
	require.NoError(t, os.Symlink(canonicalConfigPath, configPath))

	return configPath, canonicalConfigPath
}

func assertManagedCodexConfig(t *testing.T, configPath, canonicalConfigPath string) {
	t.Helper()

	configInfo, err := os.Lstat(configPath)
	require.NoError(t, err)
	assert.NotZero(t, configInfo.Mode()&os.ModeSymlink, "config.toml should remain symlinked")

	configData, err := os.ReadFile(canonicalConfigPath)
	require.NoError(t, err)
	configText := string(configData)
	assert.Contains(t, configText, `cli_auth_credentials_store = "file"`)
	assert.NotContains(t, configText, `cli_auth_credentials_store = "keychain"`)
	assert.Contains(t, configText, `multi_agent = true`)
	assert.Contains(t, configText, `hide_rate_limit_model_nudge = true`)
}

func runHarnessStep(h *testutil.ExtendedHarness, name, description string, fn func()) {
	h.StartStep(name, description)
	defer h.EndStep(name)
	fn()
}

func configureCanonicalLogPath(t *testing.T, h *testutil.ExtendedHarness) string {
	t.Helper()

	canonicalLogPath := filepath.Join(h.TempDir, "canonical.jsonl")
	require.NoError(t, h.SetCanonicalOutputPath(canonicalLogPath))
	return canonicalLogPath
}

func validateCanonicalLogsWithFailureCheck(t *testing.T, h *testutil.ExtendedHarness, canonicalLogPath string) {
	t.Helper()

	if err := h.ValidateCanonicalLogs(); err != nil {
		t.Fatalf("canonical log validation failed: %v", err)
	}

	f, err := os.OpenFile(canonicalLogPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("{not-json}\n")
	closeErr := f.Close()
	require.NoError(t, closeErr)
	require.NoError(t, err)

	if err := h.ValidateCanonicalLogs(); err == nil {
		t.Fatal("expected canonical log validation to fail for corrupted on-disk canonical artifact")
	}
}

func TestSmartRunner_E2E(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

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

	ctx, cancel := context.WithTimeout(context.Background(), smartRunnerE2ETimeout)
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
	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_RateLimitExitPreservesSwitchedProfile(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)
	runtimeFixture := runtimefixture.NewRuntimeProfileVaultFixture(h)

	var (
		db     *caamdb.DB
		prof   *profile.Profile
		runErr error
		sr     *SmartRunner
		start  time.Time
		vault  *authfile.Vault
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Claude failed-handoff fixture", func() {
		runtimeFixture.CreateVaultProfile(t, "claude", "active", `{"profile":"active"}`)
		runtimeFixture.CreateVaultProfile(t, "claude", "backup", `{"profile":"backup"}`)
		runtimeFixture.SetActiveAuth(t, "claude", `{"profile":"active"}`)

		vault = runtimeFixture.NewVault()
		db = runtimeFixture.OpenDB(t)
		t.Cleanup(func() { db.Close() })

		execFixture := testutil.NewTestBinaryExecFixture()
		ExecCommand = execFixture.ExecCommand(map[string]string{
			"MOCK_CLI_MODE": "rate_limit_exit",
		})

		cfg := config.DefaultSPMConfig().Handoff
		cfg.MaxRetries = 3
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      &MockNotifier{},
		})

		prof = runtimeFixture.CreateProfile(t, "claude", "active", "oauth")
	})

	runHarnessStep(h, "run", "Run SmartRunner through a failing Claude handoff", func() {
		start = time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "claude"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "rate_limit_exit",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify switched profile remains active after failed handoff", func() {
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
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRefreshTokenReusedTriggersSwitch(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	var (
		db       *caamdb.DB
		err      error
		prof     *profile.Profile
		runErr   error
		sr       *SmartRunner
		vault    *authfile.Vault
		notifier *MockNotifier
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex refresh-token reuse handoff fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"profile":"active"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"profile":"backup"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"profile":"active"}`), 0o600))

		vault = authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

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
		notifier = &MockNotifier{}
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      notifier,
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner until refresh-token reuse triggers a profile switch", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "refresh_token_reused_exit",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify switched profile and resume hint after refresh-token reuse failure", func() {
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
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRateLimitSwitchSkipsInteractiveLogin(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	var (
		db       *caamdb.DB
		err      error
		prof     *profile.Profile
		runErr   error
		sr       *SmartRunner
		vault    *authfile.Vault
		notifier *MockNotifier
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex no-relogin handoff fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))

		vault = authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

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
		notifier = &MockNotifier{}
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      notifier,
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through Codex rate-limit switch without interactive login", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "codex_rate_limit_no_login_needed",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify switched profile and no-relogin notification", func() {
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
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRateLimitSwitchRepairsManagedConfig(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	var (
		canonicalConfigPath string
		configPath          string
		db                  *caamdb.DB
		err                 error
		prof                *profile.Profile
		runErr              error
		sr                  *SmartRunner
		vault               *authfile.Vault
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex managed-config handoff fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))

		configPath, canonicalConfigPath = writeSymlinkedCodexConfig(t, rootDir, codexHome)

		vault = authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

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
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      &MockNotifier{},
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through a Codex config-repair handoff", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "codex_rate_limit_no_login_needed",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify Codex config remains symlinked and repaired after handoff", func() {
		require.NoError(t, runErr)
		assert.Equal(t, "backup", sr.currentProfile, "runner should switch to backup profile")

		activeProfile, activeErr := vault.ActiveProfile(authfile.CodexAuthFiles())
		require.NoError(t, activeErr)
		assert.Equal(t, "backup", activeProfile, "vault active profile should remain on backup")

		assertManagedCodexConfig(t, configPath, canonicalConfigPath)
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_ForwardsInteractiveInputToPTY(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	inputFile := filepath.Join(rootDir, "stdin.txt")
	profilesDir := filepath.Join(rootDir, "profiles")

	originalExec := ExecCommand
	originalStdin := os.Stdin
	defer func() {
		ExecCommand = originalExec
		os.Stdin = originalStdin
	}()

	readPipe, writePipe, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = readPipe
	defer readPipe.Close()

	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=interactive_input_echo",
			"MOCK_CLI_INPUT_FILE="+inputFile,
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Notifier:      &MockNotifier{},
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("claude", "active", "oauth")
	require.NoError(t, err)

	go func() {
		time.Sleep(150 * time.Millisecond)
		_, _ = writePipe.Write([]byte("hello from stdin relay\n"))
		_ = writePipe.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "claude"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI":    "1",
			"MOCK_CLI_MODE":       "interactive_input_echo",
			"MOCK_CLI_INPUT_FILE": inputFile,
		},
	})

	require.NoError(t, runErr)

	inputData, readErr := os.ReadFile(inputFile)
	require.NoError(t, readErr)
	assert.Equal(t, "hello from stdin relay", strings.TrimSpace(string(inputData)))
}

func TestSmartRunner_E2E_DoesNotDropBufferedInputBeforePTYReady(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	inputFile := filepath.Join(rootDir, "stdin-early.txt")
	profilesDir := filepath.Join(rootDir, "profiles")

	originalExec := ExecCommand
	originalStdin := os.Stdin
	defer func() {
		ExecCommand = originalExec
		os.Stdin = originalStdin
	}()

	readPipe, writePipe, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = readPipe
	defer readPipe.Close()

	_, err = writePipe.Write([]byte("early buffered input\n"))
	require.NoError(t, err)
	require.NoError(t, writePipe.Close())

	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=interactive_input_echo",
			"MOCK_CLI_INPUT_FILE="+inputFile,
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Notifier:      &MockNotifier{},
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("claude", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "claude"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI":    "1",
			"MOCK_CLI_MODE":       "interactive_input_echo",
			"MOCK_CLI_INPUT_FILE": inputFile,
		},
	})

	require.NoError(t, runErr)

	inputData, readErr := os.ReadFile(inputFile)
	require.NoError(t, readErr)
	assert.Equal(t, "early buffered input", strings.TrimSpace(string(inputData)))
}

func TestSmartRunner_E2E_DoesNotEchoTerminalRepliesBackToOutput(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	rootDir := h.TempDir
	responseFile := filepath.Join(rootDir, "terminal-response.txt")
	profilesDir := filepath.Join(rootDir, "profiles")

	originalExec := ExecCommand
	originalStdin := os.Stdin
	originalStdout := os.Stdout
	defer func() {
		ExecCommand = originalExec
		os.Stdin = originalStdin
		os.Stdout = originalStdout
	}()

	readPipe, writePipe, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = readPipe
	defer readPipe.Close()

	stdoutRead, stdoutWrite, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = stdoutWrite

	var stdoutBuf strings.Builder
	doneReading := make(chan struct{})
	go func() {
		defer close(doneReading)
		buf := make([]byte, 256)
		responded := false
		for {
			n, readErr := stdoutRead.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				stdoutBuf.WriteString(chunk)
				if !responded && strings.Contains(stdoutBuf.String(), "\x1b[6n") {
					responded = true
					_, _ = writePipe.Write([]byte("\x1b[8;1R"))
					_ = writePipe.Close()
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_MOCK_CLI=1",
			"MOCK_CLI_MODE=terminal_query_reply",
			"MOCK_CLI_RESPONSE_FILE="+responseFile,
		)
		return cmd
	}

	cfg := config.DefaultSPMConfig().Handoff
	sr := NewSmartRunner(&Runner{}, SmartRunnerOptions{
		HandoffConfig: &cfg,
		Notifier:      &MockNotifier{},
	})

	store := profile.NewStore(profilesDir)
	prof, err := store.Create("claude", "active", "oauth")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := sr.Run(ctx, RunOptions{
		Profile:  prof,
		Provider: &MockProvider{id: "claude"},
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_MOCK_CLI":       "1",
			"MOCK_CLI_MODE":          "terminal_query_reply",
			"MOCK_CLI_RESPONSE_FILE": responseFile,
		},
	})
	require.NoError(t, runErr)

	_ = stdoutWrite.Close()
	<-doneReading

	responseData, readErr := os.ReadFile(responseFile)
	require.NoError(t, readErr)
	assert.Contains(t, string(responseData), "\x1b[8;1R", "child process should receive the terminal reply")
	assert.NotContains(t, stdoutBuf.String(), "8;1R", "terminal reply bytes should not be echoed back to visible output")
	assert.Contains(t, stdoutBuf.String(), "terminal reply received")
}

func TestSmartRunner_E2E_CodexFallsBackToSystemProfileWhenOnlyDistinctAlternative(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	var (
		db     *caamdb.DB
		err    error
		prof   *profile.Profile
		runErr error
		sr     *SmartRunner
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex system-profile fallback fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "_backup_distinct"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "_backup_distinct", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))

		vault := authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

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
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      &MockNotifier{},
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through system-profile fallback handoff", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "codex_rate_limit_no_login_needed",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify the only distinct Codex fallback is selected", func() {
		require.NoError(t, runErr)
		assert.Equal(t, "_backup_distinct", sr.currentProfile, "runner should fall back to system backup when it is the only distinct credential")
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRateLimitExitAutoResumesOnSwitchedProfile(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)
	runtimeFixture := runtimefixture.NewRuntimeProfileVaultFixture(h)
	counterFile := filepath.Join(runtimeFixture.RootDir, "resume_count.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"

	var (
		db       *caamdb.DB
		prof     *profile.Profile
		runErr   error
		sr       *SmartRunner
		vault    *authfile.Vault
		notifier *MockNotifier
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex seamless-resume handoff fixture", func() {
		runtimeFixture.CreateVaultProfile(t, "codex", "active", `{"tokens":{"access_token":"token-a"}}`)
		runtimeFixture.CreateVaultProfile(t, "codex", "backup", `{"tokens":{"access_token":"token-b"}}`)
		runtimeFixture.SetActiveAuth(t, "codex", `{"tokens":{"access_token":"token-a"}}`)

		vault = runtimeFixture.NewVault()
		db = runtimeFixture.OpenDB(t)
		t.Cleanup(func() { db.Close() })

		execFixture := testutil.NewTestBinaryExecFixture()
		ExecCommand = execFixture.ExecCommand(map[string]string{
			"MOCK_CLI_MODE":         "rate_limit_exit_then_resume_success",
			"MOCK_CLI_COUNTER_FILE": counterFile,
			"MOCK_EXPECT_RESUME_ID": sessionID,
		})

		cfg := config.DefaultSPMConfig().Handoff
		cfg.MaxRetries = 2
		notifier = &MockNotifier{}
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      notifier,
		})

		prof = runtimeFixture.CreateProfile(t, "codex", "active", "oauth")
	})

	runHarnessStep(h, "run", "Run SmartRunner through rate-limit exit and seamless resume", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smartRunnerE2ETimeout)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
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
	})

	runHarnessStep(h, "verify", "Verify switched profile and seamless resume replay", func() {
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
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRateLimitExitAutoResumesAndContinuesOnSwitchedProfile(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")
	counterFile := filepath.Join(rootDir, "resume_count.txt")
	continuationFile := filepath.Join(rootDir, "continuation_prompt.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"

	var (
		db       *caamdb.DB
		err      error
		notifier *MockNotifier
		prof     *profile.Profile
		runErr   error
		sr       *SmartRunner
		vault    *authfile.Vault
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex seamless-resume continuation fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))

		vault = authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		execFixture := testutil.NewTestBinaryExecFixture()
		ExecCommand = execFixture.ExecCommand(map[string]string{
			"MOCK_CLI_MODE":          "rate_limit_exit_then_resume_requires_prompt",
			"MOCK_CLI_COUNTER_FILE":  counterFile,
			"MOCK_EXPECT_RESUME_ID":  sessionID,
			"MOCK_CONTINUATION_FILE": continuationFile,
		})

		cfg := config.DefaultSPMConfig().Handoff
		cfg.MaxRetries = 2
		notifier = &MockNotifier{}
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      notifier,
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through resume and continuation prompt injection", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smartRunnerE2ETimeout)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI":       "1",
				"MOCK_CLI_MODE":          "rate_limit_exit_then_resume_requires_prompt",
				"MOCK_CLI_COUNTER_FILE":  counterFile,
				"MOCK_EXPECT_RESUME_ID":  sessionID,
				"MOCK_CONTINUATION_FILE": continuationFile,
			},
		})
	})

	runHarnessStep(h, "verify", "Verify continuation prompt and switched profile after resume", func() {
		require.NoError(t, runErr)
		assert.Equal(t, "backup", sr.currentProfile, "runner should preserve switched profile after seamless resume")
		assert.Equal(t, 1, sr.handoffCount, "runner should record one handoff before auto-resume")

		countData, readErr := os.ReadFile(counterFile)
		require.NoError(t, readErr)
		assert.Equal(t, "2", strings.TrimSpace(string(countData)), "mock command should run twice (initial + seamless resume)")

		continuationData, promptErr := os.ReadFile(continuationFile)
		require.NoError(t, promptErr)
		continuationText := strings.ToLower(strings.TrimSpace(string(continuationData)))
		assert.Contains(t, continuationText, "continue exactly where you left off", "expected SmartRunner to inject a continuation prompt after seamless resume")

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
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRateLimitExitIgnoresStaleRedispatchBeforeSeamlessResume(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")
	counterFile := filepath.Join(rootDir, "resume_count.txt")
	continuationFile := filepath.Join(rootDir, "continuation_prompt.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"

	var (
		db       *caamdb.DB
		err      error
		prof     *profile.Profile
		runErr   error
		sr       *SmartRunner
		vault    *authfile.Vault
		notifier *MockNotifier
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex stale-redispatch handoff fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))

		vault = authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		execFixture := testutil.NewTestBinaryExecFixture()
		ExecCommand = execFixture.ExecCommand(map[string]string{
			"MOCK_CLI_MODE":          "rate_limit_exit_with_stale_duplicate_then_resume_prompt_arg",
			"MOCK_CLI_COUNTER_FILE":  counterFile,
			"MOCK_EXPECT_RESUME_ID":  sessionID,
			"MOCK_CONTINUATION_FILE": continuationFile,
		})

		cfg := config.DefaultSPMConfig().Handoff
		cfg.MaxRetries = 2
		notifier = &MockNotifier{}
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      notifier,
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through stale redispatch noise and seamless resume", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smartRunnerE2ETimeout)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI":       "1",
				"MOCK_CLI_MODE":          "rate_limit_exit_with_stale_duplicate_then_resume_prompt_arg",
				"MOCK_CLI_COUNTER_FILE":  counterFile,
				"MOCK_EXPECT_RESUME_ID":  sessionID,
				"MOCK_CONTINUATION_FILE": continuationFile,
			},
		})
	})

	runHarnessStep(h, "verify", "Verify stale redispatch is ignored and switched profile is preserved", func() {
		require.NoError(t, runErr)
		assert.Equal(t, "backup", sr.currentProfile, "runner should keep the switched profile instead of bouncing back after stale duplicate rate-limit output")

		continuationData, promptErr := os.ReadFile(continuationFile)
		require.NoError(t, promptErr)
		continuationText := strings.ToLower(strings.TrimSpace(string(continuationData)))
		assert.Contains(t, continuationText, "continue exactly where you left off")

		activeProfile, activeErr := vault.ActiveProfile(authfile.CodexAuthFiles())
		require.NoError(t, activeErr)
		assert.Equal(t, "backup", activeProfile, "vault active profile should remain on backup after stale duplicate output")
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_CodexRateLimitExitZeroStillSeamlesslyResumes(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")
	counterFile := filepath.Join(rootDir, "resume_count.txt")
	continuationFile := filepath.Join(rootDir, "continuation_prompt.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"

	var (
		db     *caamdb.DB
		err    error
		prof   *profile.Profile
		runErr error
		sr     *SmartRunner
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex exit-zero seamless-resume fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"tokens":{"access_token":"token-b"}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"token-a"}}`), 0o600))

		vault := authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		ExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			cs := []string{"-test.run=^TestMockCLI_Handoff$", "--"}
			cs = append(cs, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = append(os.Environ(),
				"GO_WANT_MOCK_CLI=1",
				"MOCK_CLI_MODE=rate_limit_exit_zero_then_resume_requires_prompt",
				"MOCK_CLI_COUNTER_FILE="+counterFile,
				"MOCK_EXPECT_RESUME_ID="+sessionID,
				"MOCK_CONTINUATION_FILE="+continuationFile,
			)
			return cmd
		}

		cfg := config.DefaultSPMConfig().Handoff
		cfg.MaxRetries = 2
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db),
			Notifier:      &MockNotifier{},
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through exit-zero rate limit and seamless resume", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smartRunnerE2ETimeout)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI":       "1",
				"MOCK_CLI_MODE":          "rate_limit_exit_zero_then_resume_requires_prompt",
				"MOCK_CLI_COUNTER_FILE":  counterFile,
				"MOCK_EXPECT_RESUME_ID":  sessionID,
				"MOCK_CONTINUATION_FILE": continuationFile,
			},
		})
	})

	runHarnessStep(h, "verify", "Verify exit-zero rate limit path still resumes on the switched profile", func() {
		require.NoError(t, runErr)
		countData, readErr := os.ReadFile(counterFile)
		require.NoError(t, readErr)
		assert.Equal(t, "2", strings.TrimSpace(string(countData)), "expected exit-zero rate limit path to still auto-resume")

		continuationData, promptErr := os.ReadFile(continuationFile)
		require.NoError(t, promptErr)
		assert.Contains(t, strings.ToLower(strings.TrimSpace(string(continuationData))), "continue exactly where you left off")
		assert.Equal(t, "backup", sr.currentProfile)
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_MultiProfileChainUntilHealthy(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")

	var (
		db       *caamdb.DB
		err      error
		prof     *profile.Profile
		runErr   error
		sr       *SmartRunner
		vault    *authfile.Vault
		notifier *MockNotifier
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare multi-profile handoff chain fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("XDG_CONFIG_HOME", filepath.Join(rootDir, ".config"))

		for _, profileName := range []string{"active", "backup1", "backup2"} {
			require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", profileName), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "claude", profileName, ".claude.json"), []byte(fmt.Sprintf(`{"profile":"%s"}`, profileName)), 0o600))
		}
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, ".claude.json"), []byte(`{"profile":"active"}`), 0o600))

		vault = authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

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
		notifier = &MockNotifier{}
		selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      notifier,
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("claude", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner across multiple exhausted profiles until a healthy candidate succeeds", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smartRunnerE2ETimeout)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "claude"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "double_handoff_success",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify the final healthy profile after chained handoffs", func() {
		require.NoError(t, runErr)

		assert.Equal(t, "backup2", sr.currentProfile, "runner should advance across exhausted profiles until a healthy profile succeeds")
		assert.Equal(t, 2, sr.handoffCount, "runner should complete two handoffs in one session")
		activeProfile, activeErr := vault.ActiveProfile(authfile.ClaudeAuthFiles())
		require.NoError(t, activeErr)
		assert.Equal(t, "backup2", activeProfile, "final active profile should be the last healthy candidate")
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
}

func TestSmartRunner_E2E_HandoffFailureStillErrorsWhenWrappedCommandExitsZero(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	canonicalLogPath := configureCanonicalLogPath(t, h)

	rootDir := h.TempDir
	vaultDir := filepath.Join(rootDir, "vault")
	dbPath := filepath.Join(rootDir, "caam.db")
	profilesDir := filepath.Join(rootDir, "profiles")
	codexHome := filepath.Join(rootDir, ".codex")

	var (
		db     *caamdb.DB
		err    error
		prof   *profile.Profile
		runErr error
		sr     *SmartRunner
	)

	originalExec := ExecCommand
	defer func() { ExecCommand = originalExec }()

	runHarnessStep(h, "setup", "Prepare Codex exit-zero handoff-failure fixture", func() {
		h.SetEnv("HOME", rootDir)
		h.SetEnv("CODEX_HOME", codexHome)

		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "active"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "backup"), 0o755))
		require.NoError(t, os.MkdirAll(codexHome, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "active", "auth.json"), []byte(`{"profile":"active"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "codex", "backup", "auth.json"), []byte(`{"profile":"backup"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"profile":"active"}`), 0o600))

		vault := authfile.NewVault(vaultDir)
		db, err = caamdb.OpenAt(dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

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
		sr = NewSmartRunner(&Runner{}, SmartRunnerOptions{
			HandoffConfig: &cfg,
			Vault:         vault,
			DB:            db,
			Rotation:      selector,
			Notifier:      &MockNotifier{},
		})

		store := profile.NewStore(profilesDir)
		prof, err = store.Create("codex", "active", "oauth")
		require.NoError(t, err)
	})

	runHarnessStep(h, "run", "Run SmartRunner through exit-zero handoff failure", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runErr = sr.Run(ctx, RunOptions{
			Profile:  prof,
			Provider: &MockProvider{id: "codex"},
			Args:     []string{},
			Env: map[string]string{
				"GO_WANT_MOCK_CLI": "1",
				"MOCK_CLI_MODE":    "refresh_token_reused_exit_zero",
			},
		})
	})

	runHarnessStep(h, "verify", "Verify SmartRunner still surfaces the handoff failure", func() {
		require.Error(t, runErr)
		require.Contains(t, runErr.Error(), "auto-handoff failed")
	})

	validateCanonicalLogsWithFailureCheck(t, h, canonicalLogPath)
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
