package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/spf13/cobra"
)

type refreshCommandTestEnv struct {
	root      string
	testVault *authfile.Vault
}

func setupRefreshCommandTestEnv(t *testing.T) refreshCommandTestEnv {
	t.Helper()

	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	caamHome := filepath.Join(root, "caam-home")
	vaultPath := filepath.Join(root, "vault")
	healthPath := filepath.Join(root, "health.json")

	for _, dir := range []string{homeDir, caamHome, vaultPath} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("CAAM_HOME", caamHome)

	oldVault := vault
	oldHealthStore := healthStore
	testVault := authfile.NewVault(vaultPath)
	vault = testVault
	healthStore = health.NewStorage(healthPath)
	t.Cleanup(func() {
		vault = oldVault
		healthStore = oldHealthStore
	})

	return refreshCommandTestEnv{
		root:      root,
		testVault: testVault,
	}
}

func newRefreshTestCommand(t *testing.T, values map[string]string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().Bool("quiet", false, "")

	for key, value := range values {
		if err := cmd.Flags().Set(key, value); err != nil {
			t.Fatalf("Flags().Set(%q): %v", key, err)
		}
	}

	return cmd
}

func writeRefreshCodexAuth(t *testing.T, testVault *authfile.Vault, profile string, expiresAt time.Time, includeRefreshToken bool) {
	t.Helper()

	profileDir := testVault.ProfilePath("codex", profile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", profileDir, err)
	}

	refreshToken := ""
	if includeRefreshToken {
		refreshToken = `"refresh_token":"refresh-token",`
	}

	content := `{"access_token":"access-token",` + refreshToken + `"expires_at":` + strconv.FormatInt(expiresAt.Unix(), 10) + `}`
	if err := os.WriteFile(filepath.Join(profileDir, "auth.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(auth.json): %v", err)
	}
}

func TestRunRefresh_DefaultAllAndToolViews(t *testing.T) {
	env := setupRefreshCommandTestEnv(t)
	_ = env

	writeRefreshCodexAuth(t, env.testVault, "expiring", time.Now().Add(5*time.Minute), true)
	writeRefreshCodexAuth(t, env.testVault, "stable", time.Now().Add(2*time.Hour), true)
	writeRefreshCodexAuth(t, env.testVault, "no-refresh-token", time.Now().Add(5*time.Minute), false)

	out, err := captureStdout(t, func() error {
		return runRefresh(newRefreshTestCommand(t, nil), nil)
	})
	if err != nil {
		t.Fatalf("runRefresh(default): %v", err)
	}
	if !strings.Contains(out, "Would refresh:") {
		t.Fatalf("default refresh output missing header: %q", out)
	}
	if !strings.Contains(out, "codex/expiring") || !strings.Contains(out, "would refresh") {
		t.Fatalf("default refresh output missing expiring profile: %q", out)
	}
	if !strings.Contains(out, "codex/stable") || !strings.Contains(out, "skip (expires") {
		t.Fatalf("default refresh output missing stable skip: %q", out)
	}
	if !strings.Contains(out, "codex/no-refresh-token") || !strings.Contains(out, "skip (no refresh token)") {
		t.Fatalf("default refresh output missing no-refresh-token skip: %q", out)
	}
	if !strings.Contains(out, "Would refresh 1, would skip 2") {
		t.Fatalf("default refresh summary mismatch: %q", out)
	}

	out, err = captureStdout(t, func() error {
		return runRefresh(newRefreshTestCommand(t, nil), []string{"codex"})
	})
	if err != nil {
		t.Fatalf("runRefresh(tool view): %v", err)
	}
	if !strings.Contains(out, "Would refresh codex profiles:") {
		t.Fatalf("tool refresh output missing header: %q", out)
	}
	if !strings.Contains(out, "codex/expiring") || !strings.Contains(out, "codex/stable") {
		t.Fatalf("tool refresh output missing profiles: %q", out)
	}
}

func TestRunRefresh_SingleProfileAndUnknownTool(t *testing.T) {
	env := setupRefreshCommandTestEnv(t)

	writeRefreshCodexAuth(t, env.testVault, "stable", time.Now().Add(2*time.Hour), true)

	out, err := captureStdout(t, func() error {
		return runRefresh(newRefreshTestCommand(t, nil), []string{"codex", "stable"})
	})
	if err != nil {
		t.Fatalf("runRefresh(single stable): %v", err)
	}
	if !strings.Contains(out, "codex/stable skipped (expires") {
		t.Fatalf("single-profile refresh output mismatch: %q", out)
	}

	out, err = captureStdout(t, func() error {
		return runRefresh(newRefreshTestCommand(t, map[string]string{"dry-run": "true", "force": "true"}), []string{"codex", "stable"})
	})
	if err != nil {
		t.Fatalf("runRefresh(single forced dry-run): %v", err)
	}
	if !strings.Contains(out, "codex/stable would be refreshed (forced)") {
		t.Fatalf("forced dry-run output mismatch: %q", out)
	}

	err = runRefresh(newRefreshTestCommand(t, nil), []string{"mystery"})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("runRefresh(unknown tool) error = %v, want unknown-tool error", err)
	}
}

func TestRefreshHelpers_CoverageBranches(t *testing.T) {
	env := setupRefreshCommandTestEnv(t)

	writeRefreshCodexAuth(t, env.testVault, "expiring", time.Now().Add(2*time.Minute), true)
	writeRefreshCodexAuth(t, env.testVault, "stable", time.Now().Add(2*time.Hour), true)

	should, reason, err := shouldRefreshProfile("codex", "expiring", 10*time.Minute, false)
	if err != nil || !should || reason == "" {
		t.Fatalf("shouldRefreshProfile(expiring) = (%v, %q, %v)", should, reason, err)
	}

	should, reason, err = shouldRefreshProfile("codex", "stable", 10*time.Minute, true)
	if err != nil || !should || reason != "forced" {
		t.Fatalf("shouldRefreshProfile(force) = (%v, %q, %v), want (true, forced, nil)", should, reason, err)
	}

	ttl := refreshedTTL("codex", "stable")
	if ttl == "" {
		t.Fatal("refreshedTTL(codex, stable) should not be empty")
	}

	geminiHealth := &health.ProfileHealth{TokenExpiresAt: time.Now().Add(90 * time.Minute)}
	if err := healthStore.UpdateProfile("gemini", "g1", geminiHealth); err != nil {
		t.Fatalf("healthStore.UpdateProfile(gemini): %v", err)
	}
	if got := refreshedTTL("gemini", "g1"); got == "" {
		t.Fatal("refreshedTTL(gemini, g1) should use health metadata")
	}

	if !isRefreshReauthRequired(errors.New("invalid_grant")) {
		t.Fatal("invalid_grant should be treated as reauth-required")
	}
	if isRefreshReauthRequired(nil) {
		t.Fatal("nil error should not require reauth")
	}

	filePath := env.testVault.ProfilePath("codex", "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile(non-dir profile path): %v", err)
	}
	if err := ensureVaultProfileDir("codex", "missing"); err == nil || !strings.Contains(err.Error(), "not found in vault") {
		t.Fatalf("ensureVaultProfileDir(missing) error = %v, want not-found error", err)
	}
	if err := ensureVaultProfileDir("codex", "not-a-dir"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("ensureVaultProfileDir(not-a-dir) error = %v, want non-directory error", err)
	}

	if _, err := loadExpiryInfo("unknown", "p1"); err == nil || !strings.Contains(err.Error(), "refresh not supported") {
		t.Fatalf("loadExpiryInfo(unknown) error = %v, want unsupported error", err)
	}
}
