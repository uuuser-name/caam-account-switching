package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/project"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/signals"
	"github.com/spf13/cobra"
)

func TestUninstall_RestoresOriginalAndRemovesData(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "xdg_data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "xdg_config"))
	t.Setenv("CAAM_HOME", filepath.Join(tmpDir, "caam_home"))
	t.Setenv("CODEX_HOME", filepath.Join(tmpDir, "codex_home"))

	if err := os.MkdirAll(os.Getenv("CODEX_HOME"), 0700); err != nil {
		t.Fatalf("MkdirAll(CODEX_HOME) error = %v", err)
	}

	// Current auth differs from _original; uninstall should restore _original.
	currentAuthPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	if err := os.WriteFile(currentAuthPath, []byte(`{"token":"current"}`), 0600); err != nil {
		t.Fatalf("WriteFile(current auth) error = %v", err)
	}

	oldVault := vault
	vault = authfile.NewVault(authfile.DefaultVaultPath())
	t.Cleanup(func() { vault = oldVault })

	originalDir := vault.ProfilePath("codex", "_original")
	if err := os.MkdirAll(originalDir, 0700); err != nil {
		t.Fatalf("MkdirAll(_original) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(originalDir, "auth.json"), []byte(`{"token":"original"}`), 0600); err != nil {
		t.Fatalf("WriteFile(_original auth) error = %v", err)
	}

	// Create various caam files to verify cleanup.
	writeFile(t, health.DefaultHealthPath(), "health")
	writeFile(t, filepath.Join(profile.DefaultStorePath(), "codex", "work", "profile.json"), "{}")
	writeFile(t, config.ConfigPath(), "{}")
	writeFile(t, config.SPMConfigPath(), "version: 1\n")
	writeFile(t, project.DefaultPath(), "{}")
	writeFile(t, caamdb.DefaultPath(), "sqlite")
	writeFile(t, signals.DefaultLogFilePath(), "log")
	writeFile(t, signals.DefaultPIDFilePath(), "123\n")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("keep-backups", false, "")
	cmd.Flags().Bool("force", false, "")
	_ = cmd.Flags().Set("force", "true")

	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall() error = %v", err)
	}

	got, err := os.ReadFile(currentAuthPath)
	if err != nil {
		t.Fatalf("ReadFile(auth.json) error = %v", err)
	}
	if string(got) != `{"token":"original"}` {
		t.Fatalf("auth.json = %q, want %q", string(got), `{"token":"original"}`)
	}

	// Vault should be removed (keep-backups=false).
	if _, err := os.Stat(vault.ProfilePath("codex", "_original")); !os.IsNotExist(err) {
		t.Fatalf("expected vault to be removed, stat error = %v", err)
	}

	// Config dir should be removed.
	if _, err := os.Stat(filepath.Dir(config.ConfigPath())); !os.IsNotExist(err) {
		t.Fatalf("expected config dir removed, stat error = %v", err)
	}

	// CAAM_HOME artifacts should be removed.
	for _, p := range []string{
		config.SPMConfigPath(),
		project.DefaultPath(),
		caamdb.DefaultPath(),
		signals.DefaultLogFilePath(),
		signals.DefaultPIDFilePath(),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat error = %v", p, err)
		}
	}
}

func TestUninstall_KeepBackupsKeepsVault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "xdg_data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "xdg_config"))
	t.Setenv("CAAM_HOME", filepath.Join(tmpDir, "caam_home"))
	t.Setenv("CODEX_HOME", filepath.Join(tmpDir, "codex_home"))

	if err := os.MkdirAll(os.Getenv("CODEX_HOME"), 0700); err != nil {
		t.Fatalf("MkdirAll(CODEX_HOME) error = %v", err)
	}

	currentAuthPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	if err := os.WriteFile(currentAuthPath, []byte(`{"token":"current"}`), 0600); err != nil {
		t.Fatalf("WriteFile(current auth) error = %v", err)
	}

	oldVault := vault
	vault = authfile.NewVault(authfile.DefaultVaultPath())
	t.Cleanup(func() { vault = oldVault })

	originalDir := vault.ProfilePath("codex", "_original")
	if err := os.MkdirAll(originalDir, 0700); err != nil {
		t.Fatalf("MkdirAll(_original) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(originalDir, "auth.json"), []byte(`{"token":"original"}`), 0600); err != nil {
		t.Fatalf("WriteFile(_original auth) error = %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("keep-backups", false, "")
	cmd.Flags().Bool("force", false, "")
	_ = cmd.Flags().Set("keep-backups", "true")
	_ = cmd.Flags().Set("force", "true")

	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall() error = %v", err)
	}

	if _, err := os.Stat(vault.ProfilePath("codex", "_original")); err != nil {
		t.Fatalf("expected vault to remain, stat error = %v", err)
	}
}

func TestUninstall_DryRunDoesNotModify(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "xdg_data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "xdg_config"))
	t.Setenv("CAAM_HOME", filepath.Join(tmpDir, "caam_home"))
	t.Setenv("CODEX_HOME", filepath.Join(tmpDir, "codex_home"))

	if err := os.MkdirAll(os.Getenv("CODEX_HOME"), 0700); err != nil {
		t.Fatalf("MkdirAll(CODEX_HOME) error = %v", err)
	}

	currentAuthPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	if err := os.WriteFile(currentAuthPath, []byte(`{"token":"current"}`), 0600); err != nil {
		t.Fatalf("WriteFile(current auth) error = %v", err)
	}

	oldVault := vault
	vault = authfile.NewVault(authfile.DefaultVaultPath())
	t.Cleanup(func() { vault = oldVault })

	originalDir := vault.ProfilePath("codex", "_original")
	if err := os.MkdirAll(originalDir, 0700); err != nil {
		t.Fatalf("MkdirAll(_original) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(originalDir, "auth.json"), []byte(`{"token":"original"}`), 0600); err != nil {
		t.Fatalf("WriteFile(_original auth) error = %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("keep-backups", false, "")
	cmd.Flags().Bool("force", false, "")
	_ = cmd.Flags().Set("dry-run", "true")
	_ = cmd.Flags().Set("force", "true")

	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall() error = %v", err)
	}

	got, err := os.ReadFile(currentAuthPath)
	if err != nil {
		t.Fatalf("ReadFile(auth.json) error = %v", err)
	}
	if string(got) != `{"token":"current"}` {
		t.Fatalf("auth.json = %q, want %q", string(got), `{"token":"current"}`)
	}

	if _, err := os.Stat(vault.ProfilePath("codex", "_original")); err != nil {
		t.Fatalf("expected vault to remain, stat error = %v", err)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
