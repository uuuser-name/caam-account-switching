package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
)

func TestResolveInvokedProviderBinary(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() {
		os.Args = origArgs
	})

	dirA := t.TempDir()
	dirB := t.TempDir()
	invoked := filepath.Join(dirA, "codex")
	alternate := filepath.Join(dirB, "codex")

	if err := os.WriteFile(invoked, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write invoked binary: %v", err)
	}
	if err := os.WriteFile(alternate, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write alternate binary: %v", err)
	}

	os.Args = []string{invoked}
	t.Setenv("PATH", strings.Join([]string{dirA, dirB}, string(os.PathListSeparator)))

	got := resolveInvokedProviderBinary("codex")
	if got == "" {
		t.Fatal("expected alternate provider binary")
	}
	if filepath.Clean(got) != filepath.Clean(alternate) {
		t.Fatalf("expected %q, got %q", alternate, got)
	}
}

func TestResolveInvokedProviderBinaryNotInvokedAsTool(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"/tmp/caam"}
	if got := resolveInvokedProviderBinary("codex"); got != "" {
		t.Fatalf("expected empty result, got %q", got)
	}
}

func TestInvocationHelpers(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"/usr/local/bin/codex"}
	if !isInvokedAsProviderTool("codex") {
		t.Fatal("expected codex invocation to be detected")
	}
	if isInvokedAsProviderTool("claude") {
		t.Fatal("expected non-matching invocation to be false")
	}
	if !samePath("/tmp/../tmp/a", "/tmp/a") {
		t.Fatal("expected cleaned paths to match")
	}
	if samePath("", "/tmp/a") {
		t.Fatal("expected empty path compare to be false")
	}

	os.Args = []string{"/usr/local/bin/openclaw"}
	if !isInvokedAsProviderTool("codex") {
		t.Fatal("expected openclaw invocation to map to codex provider")
	}
}

func TestProviderBinaryCandidates(t *testing.T) {
	candidates := providerBinaryCandidates("codex", "openclaw")
	if len(candidates) == 0 {
		t.Fatal("expected providerBinaryCandidates to return at least one candidate")
	}
	if candidates[0] != "codex" {
		t.Fatalf("expected canonical provider first, got %q", candidates[0])
	}
	foundAlias := false
	for _, candidate := range candidates {
		if candidate == "openclaw" {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Fatal("expected openclaw alias candidate to be included")
	}
}

func TestRunWrapValidationErrors(t *testing.T) {
	t.Run("missing tool", func(t *testing.T) {
		if err := runWrap(runCmd, nil); err == nil || !strings.Contains(err.Error(), "tool name required") {
			t.Fatalf("expected missing-tool error, got %v", err)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		if err := runWrap(runCmd, []string{"not-a-tool"}); err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Fatalf("expected unknown-tool error, got %v", err)
		}
	})

	t.Run("openclaw alias is accepted", func(t *testing.T) {
		old := runCmd.Flags().Lookup("algorithm").Value.String()
		t.Cleanup(func() { _ = runCmd.Flags().Set("algorithm", old) })
		_ = runCmd.Flags().Set("algorithm", "not-real")
		err := runWrap(runCmd, []string{"openclaw"})
		if err == nil || !strings.Contains(err.Error(), "unknown algorithm") {
			t.Fatalf("expected alias to pass tool validation and fail later on algorithm, got %v", err)
		}
	})

	t.Run("unknown algorithm", func(t *testing.T) {
		old := runCmd.Flags().Lookup("algorithm").Value.String()
		t.Cleanup(func() { _ = runCmd.Flags().Set("algorithm", old) })
		_ = runCmd.Flags().Set("algorithm", "not-real")
		err := runWrap(runCmd, []string{"codex"})
		if err == nil || !strings.Contains(err.Error(), "unknown algorithm") {
			t.Fatalf("expected unknown-algorithm error, got %v", err)
		}
	})
}

func TestRunPrecheckNoActiveProfile(t *testing.T) {
	origVault := vault
	t.Cleanup(func() { vault = origVault })

	vault = nil
	switched, selected := runPrecheck("codex", 0.8, true, (*caamdb.DB)(nil), rotation.AlgorithmSmart)
	if switched {
		t.Fatal("expected no precheck switch without active profile")
	}
	if selected != "" {
		t.Fatalf("expected empty selected profile, got %q", selected)
	}
}
