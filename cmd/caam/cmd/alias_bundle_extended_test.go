package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/bundle"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/project"
	syncstate "github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func setupCmdTestHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CAAM_HOME", filepath.Join(root, "caam-home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	config.SetConfigPath(filepath.Join(root, "config", "config.json"))
	t.Cleanup(func() { config.SetConfigPath("") })
	return root
}

func createVaultProfileFile(t *testing.T, tool, profile string) {
	t.Helper()
	profileDir := filepath.Join(authfile.DefaultVaultPath(), tool, profile)
	require.NoError(t, os.MkdirAll(profileDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth.json"), []byte(`{"token":"ok"}`), 0o600))
}

func newAliasTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("list", false, "")
	c.Flags().String("remove", "", "")
	c.Flags().Bool("json", false, "")
	return c
}

func newFavoriteTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("list", false, "")
	c.Flags().Bool("clear", false, "")
	c.Flags().Bool("json", false, "")
	return c
}

func newBundleExportTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().StringP("output", "o", "", "")
	c.Flags().Bool("verbose-filename", false, "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().BoolP("encrypt", "e", false, "")
	c.Flags().StringP("password", "p", "", "")
	c.Flags().StringSlice("providers", nil, "")
	c.Flags().StringSlice("profiles", nil, "")
	c.Flags().Bool("no-config", false, "")
	c.Flags().Bool("no-projects", false, "")
	c.Flags().Bool("no-health", false, "")
	c.Flags().Bool("include-database", false, "")
	c.Flags().Bool("include-sync", true, "")
	return c
}

func newBundleImportTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("mode", "smart", "")
	c.Flags().StringP("password", "p", "", "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("skip-config", false, "")
	c.Flags().Bool("skip-projects", false, "")
	c.Flags().Bool("skip-health", false, "")
	c.Flags().Bool("skip-database", false, "")
	c.Flags().Bool("skip-sync", false, "")
	c.Flags().StringSlice("providers", nil, "")
	c.Flags().StringSlice("profiles", nil, "")
	c.Flags().Bool("json", false, "")
	return c
}

func TestAliasAndFavoriteCommandFlows(t *testing.T) {
	_ = setupCmdTestHome(t)

	origVault := vault
	vault = authfile.NewVault(authfile.DefaultVaultPath())
	t.Cleanup(func() { vault = origVault })

	createVaultProfileFile(t, "claude", "work")
	createVaultProfileFile(t, "claude", "personal")

	aliasCmd := newAliasTestCmd()
	require.NoError(t, aliasCmd.Flags().Set("list", "true"))
	out, err := captureStdout(t, func() error { return runAlias(aliasCmd, nil) })
	require.NoError(t, err)
	require.Contains(t, out, "No aliases configured")

	aliasCmd = newAliasTestCmd()
	out, err = captureStdout(t, func() error { return runAlias(aliasCmd, []string{"claude", "work", "wk"}) })
	require.NoError(t, err)
	require.Contains(t, out, "Added alias: wk -> claude/work")

	aliasCmd = newAliasTestCmd()
	out, err = captureStdout(t, func() error { return runAlias(aliasCmd, []string{"claude", "work"}) })
	require.NoError(t, err)
	require.Contains(t, out, "Aliases for claude/work")
	require.Contains(t, out, "wk")

	aliasCmd = newAliasTestCmd()
	require.NoError(t, aliasCmd.Flags().Set("remove", "wk"))
	out, err = captureStdout(t, func() error { return runAlias(aliasCmd, nil) })
	require.NoError(t, err)
	require.Contains(t, out, "Removed alias: wk")

	aliasCmd = newAliasTestCmd()
	err = runAlias(aliasCmd, []string{"nope", "work"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")

	favCmd := newFavoriteTestCmd()
	out, err = captureStdout(t, func() error { return runFavorite(favCmd, []string{"claude", "work", "personal"}) })
	require.NoError(t, err)
	require.Contains(t, out, "Set favorites for claude")

	favCmd = newFavoriteTestCmd()
	out, err = captureStdout(t, func() error { return runFavorite(favCmd, []string{"claude"}) })
	require.NoError(t, err)
	require.Contains(t, out, "Favorites for claude")
	require.Contains(t, out, "1. work")

	favCmd = newFavoriteTestCmd()
	require.NoError(t, favCmd.Flags().Set("clear", "true"))
	out, err = captureStdout(t, func() error { return runFavorite(favCmd, []string{"claude"}) })
	require.NoError(t, err)
	require.Contains(t, out, "Cleared favorites for claude")
}

func TestBundleExportAndImportCommandFlows(t *testing.T) {
	base := setupCmdTestHome(t)

	createVaultProfileFile(t, "claude", "work")
	require.NoError(t, os.MkdirAll(filepath.Dir(project.DefaultPath()), 0o755))
	require.NoError(t, os.WriteFile(project.DefaultPath(), []byte(`{"version":1}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(health.DefaultHealthPath()), 0o755))
	require.NoError(t, os.WriteFile(health.DefaultHealthPath(), []byte(`{"version":1}`), 0o600))
	require.NoError(t, os.MkdirAll(syncstate.SyncDataDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(syncstate.SyncDataDir(), "identity.json"), []byte(`{"id":"x"}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(caamdb.DefaultPath()), 0o755))
	require.NoError(t, os.WriteFile(caamdb.DefaultPath(), []byte("not-a-real-db"), 0o600))

	exportCmd := newBundleExportTestCmd()
	var exportOut bytes.Buffer
	exportCmd.SetOut(&exportOut)
	require.NoError(t, exportCmd.Flags().Set("output", filepath.Join(base, "out")))
	require.NoError(t, exportCmd.Flags().Set("dry-run", "true"))
	require.NoError(t, exportCmd.Flags().Set("encrypt", "true"))
	require.NoError(t, exportCmd.Flags().Set("password", "pw123"))
	require.NoError(t, runBundleExport(exportCmd, nil))
	require.Contains(t, exportOut.String(), "Export Preview")
	require.Contains(t, exportOut.String(), "Vault profiles:")
	require.Contains(t, exportOut.String(), "Encryption: AES-256-GCM")

	exporter := &bundle.VaultExporter{
		VaultPath:    authfile.DefaultVaultPath(),
		DataPath:     filepath.Dir(authfile.DefaultVaultPath()),
		ConfigPath:   config.ConfigPath(),
		ProjectsPath: project.DefaultPath(),
		HealthPath:   health.DefaultHealthPath(),
		DatabasePath: caamdb.DefaultPath(),
		SyncPath:     syncstate.SyncDataDir(),
	}
	opts := bundle.DefaultExportOptions()
	opts.OutputDir = filepath.Join(base, "out")
	opts.IncludeDatabase = false
	result, err := exporter.Export(opts)
	require.NoError(t, err)

	importCmd := newBundleImportTestCmd()
	var importOut bytes.Buffer
	importCmd.SetOut(&importOut)
	require.NoError(t, importCmd.Flags().Set("mode", "smart"))
	require.NoError(t, importCmd.Flags().Set("dry-run", "true"))
	require.NoError(t, importCmd.Flags().Set("force", "true"))
	require.NoError(t, runBundleImport(importCmd, []string{result.OutputPath}))
	require.Contains(t, importOut.String(), "Import Preview")
	require.Contains(t, importOut.String(), "Summary (would):")
}

func TestBundleImportRejectsInvalidMode(t *testing.T) {
	_ = setupCmdTestHome(t)
	cmd := newBundleImportTestCmd()
	require.NoError(t, cmd.Flags().Set("mode", "not-a-mode"))
	err := runBundleImport(cmd, []string{"does-not-matter.zip"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid mode")
}

func TestBundlePrintContracts(t *testing.T) {
	manifest := bundle.NewManifest()
	manifest.Contents.Vault.TotalProfiles = 2
	manifest.Contents.Vault.Profiles["claude"] = []string{"work", "personal"}
	manifest.Contents.Config = bundle.OptionalContent{Included: true, Count: 1}
	manifest.Contents.Projects = bundle.OptionalContent{Included: false, Reason: "excluded"}
	manifest.Contents.Health = bundle.OptionalContent{Included: true, Note: "3 checks"}
	manifest.Contents.Database = bundle.OptionalContent{Included: false, Reason: "large"}
	manifest.Contents.SyncConfig = bundle.OptionalContent{Included: true}

	exportResult := &bundle.ExportResult{
		OutputPath:     "/tmp/test.zip",
		Manifest:       manifest,
		Encrypted:      false,
		CompressedSize: 1024,
	}
	c := &cobra.Command{}
	var out bytes.Buffer
	c.SetOut(&out)
	printExportResult(c, exportResult, false)
	require.Contains(t, out.String(), "Export Complete")
	require.Contains(t, out.String(), "Vault profiles: 2")
	require.Contains(t, out.String(), "Bundle contains OAuth tokens")

	importResult := &bundle.ImportResult{
		Manifest:           manifest,
		Encrypted:          true,
		VerificationResult: &bundle.VerificationResult{Valid: false, Missing: []string{"x"}, Extra: []string{"y"}},
		ProfileActions:     []bundle.ProfileAction{{Provider: "claude", Profile: "work", Action: "add", Reason: "new"}},
		OptionalActions:    []bundle.OptionalAction{{Name: "config", Action: "import", Reason: "included"}},
		NewProfiles:        1,
		UpdatedProfiles:    0,
		SkippedProfiles:    0,
		Errors:             []string{"sample error"},
	}
	out.Reset()
	printImportResult(c, importResult)
	require.Contains(t, out.String(), "Import Complete")
	require.Contains(t, out.String(), "Added: 1")
	require.Contains(t, out.String(), "Errors:")

	out.Reset()
	printImportPreview(c, importResult)
	preview := out.String()
	require.Contains(t, preview, "Import Preview")
	require.Contains(t, preview, "Bundle Info:")
	require.Contains(t, preview, "Checksum Verification:")
}

func TestBundlePasswordPromptHelpersNonTTY(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString("secret\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	os.Stdin = r
	pw, err := promptPassword("")
	require.NoError(t, err)
	require.Equal(t, "secret", strings.TrimSpace(pw))
	require.NoError(t, r.Close())

	r2, w2, err := os.Pipe()
	require.NoError(t, err)
	_, err = w2.WriteString("secret2\n")
	require.NoError(t, err)
	require.NoError(t, w2.Close())
	os.Stdin = r2
	pw2, err := promptPasswordImport("")
	require.NoError(t, err)
	require.Equal(t, "secret2", strings.TrimSpace(pw2))
	require.NoError(t, r2.Close())
}
