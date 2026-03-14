package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/spf13/cobra"
)

func installValidateGlobals(t *testing.T, jsonOutput, active bool) {
	t.Helper()

	oldActive := validateActive
	oldJSON := validateJSON
	oldAll := validateAll
	t.Cleanup(func() {
		validateActive = oldActive
		validateJSON = oldJSON
		validateAll = oldAll
	})

	validateActive = active
	validateJSON = jsonOutput
	validateAll = false
}

func TestRunValidateWithRealProfiles(t *testing.T) {
	_ = newStartupLayout(t)
	_ = setupPoolAgentTestEnv(t)
	store := profile.NewStore(profile.DefaultStorePath())

	validProfile, err := store.Create("codex", "valid", "oauth")
	if err != nil {
		t.Fatalf("store.Create(valid) error = %v", err)
	}
	if err := os.MkdirAll(validProfile.CodexHomePath(), 0o700); err != nil {
		t.Fatalf("MkdirAll(valid codex home) error = %v", err)
	}
	validAuth := `{"access_token":"tok-valid","expires_at":"2099-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(validProfile.CodexHomePath(), "auth.json"), []byte(validAuth), 0o600); err != nil {
		t.Fatalf("WriteFile(valid auth) error = %v", err)
	}

	invalidProfile, err := store.Create("codex", "missing", "oauth")
	if err != nil {
		t.Fatalf("store.Create(missing) error = %v", err)
	}
	if err := os.MkdirAll(invalidProfile.CodexHomePath(), 0o700); err != nil {
		t.Fatalf("MkdirAll(missing codex home) error = %v", err)
	}

	installValidateGlobals(t, true, false)
	jsonOut, err := captureStdout(t, func() error {
		return runValidate(&cobra.Command{}, []string{"codex", "valid"})
	})
	if err != nil {
		t.Fatalf("runValidate(single json) error = %v", err)
	}

	var results []ValidationOutput
	if err := json.Unmarshal([]byte(jsonOut), &results); err != nil {
		t.Fatalf("json.Unmarshal(validate output) error = %v\noutput=%s", err, jsonOut)
	}
	if len(results) != 1 || !results[0].Valid || results[0].Provider != "codex" || results[0].Profile != "valid" {
		t.Fatalf("unexpected validation results: %+v", results)
	}
	if results[0].Method != "passive" || results[0].CheckedAt.IsZero() {
		t.Fatalf("unexpected validation metadata: %+v", results[0])
	}

	allJSONOut, err := captureStdout(t, func() error {
		return runValidate(&cobra.Command{}, nil)
	})
	if err != nil {
		t.Fatalf("runValidate(all json) error = %v", err)
	}

	var allResults []ValidationOutput
	if err := json.Unmarshal([]byte(allJSONOut), &allResults); err != nil {
		t.Fatalf("json.Unmarshal(all validate output) error = %v\noutput=%s", err, allJSONOut)
	}
	if len(allResults) != 2 {
		t.Fatalf("len(allResults) = %d, want 2", len(allResults))
	}

	installValidateGlobals(t, false, false)
	textOut, err := captureStdout(t, func() error {
		return runValidate(&cobra.Command{}, []string{"codex"})
	})
	if err == nil || !strings.Contains(err.Error(), "invalid token(s) found") {
		t.Fatalf("runValidate(provider text) error = %v, want invalid-token summary", err)
	}
	for _, want := range []string{
		"Token Validation Results",
		"codex/valid",
		"codex/missing",
		"Summary: 1 valid, 1 invalid (method: passive)",
	} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("validate text output missing %q: %q", want, textOut)
		}
	}
}

func TestRunValidateErrorsAndHelpers(t *testing.T) {
	_ = newStartupLayout(t)
	_ = setupPoolAgentTestEnv(t)
	installValidateGlobals(t, false, true)

	if err := runValidate(&cobra.Command{}, []string{"mystery"}); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("runValidate(unknown provider) error = %v, want unknown-provider error", err)
	}

	installValidateGlobals(t, false, false)
	out, err := captureStdout(t, func() error {
		return outputHuman(nil)
	})
	if err != nil {
		t.Fatalf("outputHuman(nil) error = %v", err)
	}
	if !strings.Contains(out, "No profiles to validate.") {
		t.Fatalf("outputHuman(nil) output = %q", out)
	}

	output := &ValidationOutput{
		Provider:  "codex",
		Profile:   "valid",
		Valid:     true,
		Method:    "active",
		CheckedAt: time.Now(),
	}
	if output.Method != methodString(false) {
		t.Fatalf("method mismatch: output=%q methodString=%q", output.Method, methodString(false))
	}
}
