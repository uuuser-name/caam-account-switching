package workflows

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	execpkg "github.com/Dicklesworthstone/coding_agent_account_manager/internal/exec"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/notify"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	codexprovider "github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider/codex"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/runtimefixture"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
)

func TestE2E_Workflow_CodexSeamlessResumeContinuation(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	canonicalLogPath := filepath.Join(h.TempDir, "canonical.jsonl")
	if err := h.SetCanonicalOutputPath(canonicalLogPath); err != nil {
		t.Fatalf("set canonical output path: %v", err)
	}

	fixture := runtimefixture.NewRuntimeProfileVaultFixture(h)
	counterFile := filepath.Join(fixture.RootDir, "resume_count.txt")
	continuationFile := filepath.Join(fixture.RootDir, "continuation_prompt.txt")
	sessionID := "019b2e3d-b524-7c22-91da-47de9068d09a"

	var (
		notificationBuffer bytes.Buffer
		prof               *profile.Profile
		runErr             error
		runner             *execpkg.SmartRunner
		vault              *authfile.Vault
	)

	originalExec := execpkg.ExecCommand
	t.Cleanup(func() { execpkg.ExecCommand = originalExec })

	h.StartStep("setup", "Preparing Codex vault, profile, and runtime fixture for seamless resume")
	fixture.CreateVaultProfile(t, "codex", "active", `{"tokens":{"access_token":"token-a"}}`)
	fixture.CreateVaultProfile(t, "codex", "backup", `{"tokens":{"access_token":"token-b"}}`)
	fixture.SetActiveAuth(t, "codex", `{"tokens":{"access_token":"token-a"}}`)

	vault = fixture.NewVault()
	db := fixture.OpenDB(t)
	t.Cleanup(func() { db.Close() })

	execFixture := testutil.NewTestBinaryExecFixture()
	execFixture.WithTestRunPattern("^TestWorkflowSmartRunnerHelper$")
	execpkg.ExecCommand = execFixture.ExecCommand(map[string]string{
		"FIXTURE_CLI_MODE":          "rate_limit_exit_then_resume_requires_prompt",
		"FIXTURE_CLI_COUNTER_FILE":  counterFile,
		"FIXTURE_EXPECT_RESUME_ID":  sessionID,
		"FIXTURE_CONTINUATION_FILE": continuationFile,
	})

	cfg := config.DefaultSPMConfig().Handoff
	cfg.MaxRetries = 2
	selector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)
	runner = execpkg.NewSmartRunner(&execpkg.Runner{}, execpkg.SmartRunnerOptions{
		HandoffConfig: &cfg,
		Vault:         vault,
		DB:            db,
		Rotation:      selector,
		Notifier:      notify.NewTerminalNotifier(&notificationBuffer, false),
	})
	prof = fixture.CreateProfile(t, "codex", "active", "oauth")

	h.LogInfo("Runtime fixture prepared", "vault_dir", fixture.VaultDir, "profiles_dir", fixture.ProfilesDir, "db_path", fixture.DBPath)
	h.EndStep("setup")

	h.StartStep("run", "Running SmartRunner through a real seamless-resume continuation handoff")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runErr = runner.Run(ctx, execpkg.RunOptions{
		Profile:  prof,
		Provider: codexprovider.New(),
		Args:     []string{},
		Env: map[string]string{
			"GO_WANT_FIXTURE_CLI":       "1",
			"FIXTURE_CLI_MODE":          "rate_limit_exit_then_resume_requires_prompt",
			"FIXTURE_CLI_COUNTER_FILE":  counterFile,
			"FIXTURE_EXPECT_RESUME_ID":  sessionID,
			"FIXTURE_CONTINUATION_FILE": continuationFile,
		},
	})
	h.LogInfo("SmartRunner execution finished", "succeeded", runErr == nil)
	h.EndStep("run")

	h.StartStep("verify", "Verifying switched profile, resume replay, and continuation prompt")
	if runErr != nil {
		t.Fatalf("smart runner run failed: %v", runErr)
	}

	countData, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter file: %v", err)
	}
	if strings.TrimSpace(string(countData)) != "2" {
		t.Fatalf("resume count = %q, want %q", strings.TrimSpace(string(countData)), "2")
	}

	continuationData, err := os.ReadFile(continuationFile)
	if err != nil {
		t.Fatalf("read continuation file: %v", err)
	}
	continuationText := strings.ToLower(strings.TrimSpace(string(continuationData)))
	if !strings.Contains(continuationText, "continue exactly where you left off") {
		t.Fatalf("continuation text = %q, want injected continuation prompt", continuationText)
	}

	activeProfile, err := vault.ActiveProfile(authfile.CodexAuthFiles())
	if err != nil {
		t.Fatalf("active profile: %v", err)
	}
	if activeProfile != "backup" {
		t.Fatalf("vault active profile = %q, want %q", activeProfile, "backup")
	}

	if !strings.Contains(notificationBuffer.String(), "Switched to backup") {
		t.Fatalf("expected switch notification before seamless resume, got %q", notificationBuffer.String())
	}

	h.LogInfo("Workflow verified", "resume_runs", strings.TrimSpace(string(countData)), "active_profile", activeProfile)
	h.EndStep("verify")

	if err := h.ValidateCanonicalLogs(); err != nil {
		t.Fatalf("canonical log validation failed: %v", err)
	}

	t.Log("\n" + h.Summary())
}

func TestWorkflowSmartRunnerHelper(t *testing.T) {
	if os.Getenv("GO_WANT_FIXTURE_CLI") != "1" {
		return
	}

	mode := strings.TrimSpace(os.Getenv("FIXTURE_CLI_MODE"))
	if mode != "rate_limit_exit_then_resume_requires_prompt" {
		return
	}

	counterFile := strings.TrimSpace(os.Getenv("FIXTURE_CLI_COUNTER_FILE"))
	expectedSessionID := strings.TrimSpace(os.Getenv("FIXTURE_EXPECT_RESUME_ID"))
	continuationFile := strings.TrimSpace(os.Getenv("FIXTURE_CONTINUATION_FILE"))

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
		time.Sleep(200 * time.Millisecond)
		os.Exit(1)
		return
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
		lower := strings.ToLower(trimmed)
		if !strings.Contains(lower, "continue exactly where you left off") {
			fmt.Printf("Unexpected continuation prompt: %s\n", trimmed)
			os.Exit(1)
			return
		}
		fmt.Println("Resumed session continued")
		os.Exit(0)
	case <-time.After(10 * time.Second):
		fmt.Println("Resumed session idle awaiting user input")
		os.Exit(1)
		return
	}
}
