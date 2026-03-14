package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/spf13/cobra"
)

func installVerifyGlobals(t *testing.T, env poolAgentTestEnv) {
	t.Helper()

	oldVault := vault
	oldHealthStore := healthStore
	t.Cleanup(func() {
		vault = oldVault
		healthStore = oldHealthStore
	})

	vault = env.vault
	healthStore = health.NewStorage("")
}

func setVerifyHealth(t *testing.T, provider, profile string, ph *health.ProfileHealth) {
	t.Helper()
	if err := healthStore.UpdateProfile(provider, profile, ph); err != nil {
		t.Fatalf("healthStore.UpdateProfile(%s/%s) error = %v", provider, profile, err)
	}
}

func newVerifyTestCommand(t *testing.T, jsonOutput, fix bool) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().Bool("fix", false, "")
	if jsonOutput {
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatalf("Flags().Set(json) error = %v", err)
		}
	}
	if fix {
		if err := cmd.Flags().Set("fix", "true"); err != nil {
			t.Fatalf("Flags().Set(fix) error = %v", err)
		}
	}
	return cmd
}

func TestVerifyProfileAndCommandOutputs(t *testing.T) {
	env := setupPoolAgentTestEnv(t)
	installVerifyGlobals(t, env)

	writePoolAgentVaultProfile(t, env, "codex", "healthy")
	writePoolAgentVaultProfile(t, env, "claude", "expired")
	writePoolAgentVaultProfile(t, env, "gemini", "warning")

	setVerifyHealth(t, "codex", "healthy", &health.ProfileHealth{
		TokenExpiresAt: time.Now().Add(2 * time.Hour),
		PlanType:       "pro",
	})
	setVerifyHealth(t, "claude", "expired", &health.ProfileHealth{
		TokenExpiresAt: time.Now().Add(-30 * time.Minute),
	})
	setVerifyHealth(t, "gemini", "warning", &health.ProfileHealth{
		TokenExpiresAt: time.Now().Add(30 * time.Minute),
		ErrorCount1h:   1,
	})

	healthy := verifyProfile("codex", "healthy")
	if healthy.Status != "healthy" || healthy.ExpiresIn == "" {
		t.Fatalf("unexpected healthy verify result: %+v", healthy)
	}

	expired := verifyProfile("claude", "expired")
	if expired.Status != "critical" || expired.ExpiresIn != "expired" {
		t.Fatalf("unexpected expired verify result: %+v", expired)
	}

	warning := verifyProfile("gemini", "warning")
	if warning.Status != "warning" {
		t.Fatalf("unexpected warning verify result: %+v", warning)
	}

	jsonOut, err := captureStdout(t, func() error {
		return runVerify(newVerifyTestCommand(t, true, false), []string{"codex"})
	})
	if err != nil {
		t.Fatalf("runVerify(json) error = %v", err)
	}

	var output VerifyOutput
	if err := json.Unmarshal([]byte(jsonOut), &output); err != nil {
		t.Fatalf("json.Unmarshal(runVerify output) error = %v\noutput=%s", err, jsonOut)
	}
	if output.Summary.TotalProfiles != 1 || output.Summary.HealthyCount != 1 {
		t.Fatalf("unexpected verify summary: %+v", output.Summary)
	}
	if len(output.Profiles) != 1 || output.Profiles[0].Profile != "healthy" {
		t.Fatalf("unexpected verify profiles: %+v", output.Profiles)
	}

	textOut, err := captureStdout(t, func() error {
		return runVerify(newVerifyTestCommand(t, false, false), nil)
	})
	if err != nil {
		t.Fatalf("runVerify(text) error = %v", err)
	}
	for _, want := range []string{
		"Profile Health Verification",
		"Codex:",
		"Claude:",
		"Gemini:",
		"Summary:",
		"Recommendations:",
	} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("verify text output missing %q: %q", want, textOut)
		}
	}
}

func TestRunVerifyErrorsAndEmptyOutput(t *testing.T) {
	env := setupPoolAgentTestEnv(t)
	installVerifyGlobals(t, env)

	if err := runVerify(newVerifyTestCommand(t, false, true), nil); err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("runVerify(--fix) error = %v, want not-implemented error", err)
	}

	if err := runVerify(newVerifyTestCommand(t, false, false), []string{"mystery"}); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("runVerify(unknown tool) error = %v, want unknown-tool error", err)
	}

	out, err := captureStdout(t, func() error {
		return runVerify(newVerifyTestCommand(t, false, false), []string{"codex"})
	})
	if err != nil {
		t.Fatalf("runVerify(empty provider) error = %v", err)
	}
	if !strings.Contains(out, "No profiles found.") {
		t.Fatalf("empty verify output = %q, want no-profiles message", out)
	}
}
