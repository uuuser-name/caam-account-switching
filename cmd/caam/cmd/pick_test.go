package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/spf13/cobra"
)

// =============================================================================
// pick.go Command Tests
// =============================================================================

func TestPickCommand(t *testing.T) {
	if pickCmd.Use != "pick [tool]" {
		t.Errorf("Expected Use 'pick [tool]', got %q", pickCmd.Use)
	}

	if pickCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if pickCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}

	if pickCmd.Args == nil {
		t.Fatal("Expected Args validator")
	}
	if err := pickCmd.Args(pickCmd, []string{"codex"}); err != nil {
		t.Fatalf("expected 1 arg allowed, got %v", err)
	}
	if err := pickCmd.Args(pickCmd, []string{"codex", "extra"}); err == nil {
		t.Fatal("expected too-many-args error")
	}
}

func TestPickCommandFlags(t *testing.T) {
	noLongerExists := []string{"fzf", "json", "brief"}
	for _, name := range noLongerExists {
		if flag := pickCmd.Flags().Lookup(name); flag != nil {
			t.Fatalf("did not expect removed flag --%s", name)
		}
	}
}

func TestPickCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "pick" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pick command not registered with root command")
	}
}

// =============================================================================
// hasFzf Tests
// =============================================================================

func TestHasFzf(t *testing.T) {
	// Test that hasFzf function works
	got := hasFzf()
	// Just verify it returns a boolean without panicking
	_ = got
}

// =============================================================================
// inferPickProvider Tests
// =============================================================================

func TestInferPickProviderNoProfiles(t *testing.T) {
	origVault := vault
	tmp := t.TempDir()
	vault = authfile.NewVault(filepath.Join(tmp, "vault"))
	t.Cleanup(func() { vault = origVault })

	inferred, providers, err := inferPickProvider()
	if err == nil {
		t.Fatal("expected error when no profiles exist")
	}
	if inferred != "" {
		t.Fatalf("expected empty inferred provider, got %q", inferred)
	}
	if len(providers) != 0 {
		t.Fatalf("expected no available providers, got %v", providers)
	}
	if !strings.Contains(err.Error(), "no profiles found") {
		t.Fatalf("expected no-profiles error, got %v", err)
	}
}

func TestInferPickProviderSingleProvider(t *testing.T) {
	origVault := vault
	tmp := t.TempDir()
	vault = authfile.NewVault(filepath.Join(tmp, "vault"))
	t.Cleanup(func() { vault = origVault })

	if err := os.MkdirAll(filepath.Join(tmp, "vault", "codex", "work"), 0o755); err != nil {
		t.Fatalf("mkdir codex profile: %v", err)
	}

	inferred, providers, err := inferPickProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferred != "codex" {
		t.Fatalf("expected codex, got %q", inferred)
	}
	if len(providers) != 1 || providers[0] != "codex" {
		t.Fatalf("expected providers [codex], got %v", providers)
	}
}

func TestInferPickProviderMultipleProviders(t *testing.T) {
	origVault := vault
	tmp := t.TempDir()
	vault = authfile.NewVault(filepath.Join(tmp, "vault"))
	t.Cleanup(func() { vault = origVault })

	dirs := []string{
		filepath.Join(tmp, "vault", "codex", "work"),
		filepath.Join(tmp, "vault", "claude", "team"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	inferred, providers, err := inferPickProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferred != "" {
		t.Fatalf("expected empty inferred provider for ambiguous set, got %q", inferred)
	}
	sort.Strings(providers)
	if strings.Join(providers, ",") != "claude,codex" {
		t.Fatalf("expected providers [claude codex], got %v", providers)
	}
}

// =============================================================================
// pickWithPrompt Tests
// =============================================================================

func TestPickWithPromptByNumber(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	withTestStdin(t, "2\n", func() {
		got, err := pickWithPrompt(cmd, "codex", []string{"alpha", "beta"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "beta" {
			t.Fatalf("expected beta, got %q", got)
		}
	})
}

func TestPickWithPromptCanceled(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	withTestStdin(t, "\n", func() {
		_, err := pickWithPrompt(cmd, "codex", []string{"alpha", "beta"}, nil)
		if !errors.Is(err, errPickCanceled) {
			t.Fatalf("expected errPickCanceled, got %v", err)
		}
	})
}

func TestPickWithPromptAliasResolution(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	cfg := config.DefaultConfig()
	cfg.AddAlias("codex", "work-profile", "work")

	withTestStdin(t, "work\n", func() {
		got, err := pickWithPrompt(cmd, "codex", []string{"work-profile", "other"}, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "work-profile" {
			t.Fatalf("expected work-profile, got %q", got)
		}
	})
}

func TestPickWithPromptOutOfRange(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	withTestStdin(t, "99\n", func() {
		_, err := pickWithPrompt(cmd, "codex", []string{"alpha", "beta"}, nil)
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("expected out-of-range error, got %v", err)
		}
	})
}

func TestRunPickUnknownTool(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runPick(cmd, []string{"not-a-tool"})
	if err == nil {
		t.Fatal("expected unknown-tool error")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown-tool error, got %v", err)
	}
}
