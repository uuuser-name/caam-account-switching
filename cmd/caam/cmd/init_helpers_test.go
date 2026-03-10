package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/discovery"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/stretchr/testify/require"
)

func withTestStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString(input)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	os.Stdin = r
	defer func() {
		_ = r.Close()
		os.Stdin = oldStdin
	}()
	fn()
}

func TestInitPromptAndShellHelpers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Setenv("SHELL", "/bin/zsh")
	require.Equal(t, "zsh", detectCurrentShell())
	t.Setenv("SHELL", "/bin/fish")
	require.Equal(t, "fish", detectCurrentShell())
	t.Setenv("SHELL", "/bin/bash")
	require.Equal(t, "bash", detectCurrentShell())
	t.Setenv("SHELL", "")
	require.Equal(t, "bash", detectCurrentShell())

	require.Equal(t, `eval "$(caam shell init)"`, getShellInitLine("zsh"))
	require.Equal(t, "caam shell init --fish | source", getShellInitLine("fish"))

	require.NoError(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# existing\n"), 0o600))
	require.Equal(t, filepath.Join(home, ".bashrc"), getShellRCFile("bash"))
	require.Equal(t, filepath.Join(home, ".zshrc"), getShellRCFile("zsh"))
	require.Equal(t, filepath.Join(home, ".config", "fish", "config.fish"), getShellRCFile("fish"))

	rcFile := filepath.Join(home, ".zshrc")
	require.NoError(t, appendToShellRC(rcFile, `eval "$(caam shell init)"`))
	data, err := os.ReadFile(rcFile)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "caam shell integration")
	require.Contains(t, content, `eval "$(caam shell init)"`)
	// idempotent when already configured
	require.NoError(t, appendToShellRC(rcFile, `eval "$(caam shell init)"`))
	data2, err := os.ReadFile(rcFile)
	require.NoError(t, err)
	require.Equal(t, content, string(data2))

	fishRC := filepath.Join(home, ".config", "fish", "config.fish")
	require.NoError(t, appendToShellRC(fishRC, "caam shell init --fish | source"))
	_, err = os.Stat(fishRC)
	require.NoError(t, err)

	withTestStdin(t, "\n", func() {
		require.Equal(t, "fallback", promptWithDefault("Profile:", "fallback"))
	})
	withTestStdin(t, "work\n", func() {
		require.Equal(t, "work", promptWithDefault("Profile:", "fallback"))
	})
	withTestStdin(t, "\n", func() {
		require.True(t, promptYesNo("Question", true))
	})
	withTestStdin(t, "n\n", func() {
		require.False(t, promptYesNo("Question", true))
	})
	withTestStdin(t, "2\n", func() {
		require.Equal(t, 2, promptNumber("Pick:", 0, 3))
	})
	withTestStdin(t, "oops\n", func() {
		require.Equal(t, 0, promptNumber("Pick:", 0, 3))
	})
}

func TestInitDiscoveryAndSummaryPrinters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out, err := captureStdout(t, func() error {
		printWelcomeBanner()
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "CAAM - Coding Agent Account Manager")

	empty := &discovery.ScanResult{Found: nil, NotFound: []discovery.Tool{discovery.ToolClaude}}
	out, err = captureStdout(t, func() error {
		printDiscoveryResults(empty)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "No existing sessions found")

	found := &discovery.ScanResult{
		Found: []discovery.DiscoveredAuth{
			{Tool: discovery.ToolCodex, Path: "/tmp/auth.json", Identity: "dev@example.com", Valid: true},
		},
		NotFound: []discovery.Tool{discovery.ToolGemini},
	}
	out, err = captureStdout(t, func() error {
		printDiscoveryResults(found)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "[OK]")
	require.Contains(t, out, "logged in as dev@example.com")

	detections := []ProviderAuthDetection{
		{
			ProviderID:  "codex",
			DisplayName: "Codex",
			Detection: &provider.AuthDetection{
				Found: true,
				Primary: &provider.AuthLocation{
					Path:         filepath.Join(home, ".codex", "auth.json"),
					IsValid:      true,
					LastModified: time.Now().Add(-1 * time.Hour),
				},
			},
		},
		{
			ProviderID:  "claude",
			DisplayName: "Claude",
			Detection:   &provider.AuthDetection{Found: false},
		},
	}
	out, err = captureStdout(t, func() error {
		printProviderDetectionResults(detections)
		printSetupSummary(found, 1, true)
		printSetupSummaryV2(detections, 1, false)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "Checking for existing auth credentials")
	require.Contains(t, out, "Setup Complete!")
	require.Contains(t, out, "Quick commands:")
}

func TestInitHelpersAndSaveDiscoveredSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CAAM_HOME", filepath.Join(root, "caam"))
	t.Setenv("CODEX_HOME", filepath.Join(root, ".codex"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))

	require.Equal(t, "dev", suggestProfileName(discovery.DiscoveredAuth{Identity: "dev@example.com"}))
	require.Equal(t, "main", suggestProfileName(discovery.DiscoveredAuth{}))
	require.NotNil(t, getAuthFileSetForTool("codex"))
	require.Nil(t, getAuthFileSetForTool("unknown"))
	require.True(t, strings.HasPrefix(shortenHomePath(filepath.Join(root, "x")), "~"))

	// Prepare a source auth file for codex so backup succeeds.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"t"}`), 0o600))

	found := []discovery.DiscoveredAuth{
		{Tool: discovery.ToolCodex, Path: filepath.Join(root, ".codex", "auth.json"), Identity: "dev@example.com", Valid: true},
	}
	saved := saveDiscoveredSessions(found, true)
	require.Equal(t, 1, saved)

	backupPath := filepath.Join(authfile.DefaultVaultPath(), "codex", "dev", "auth.json")
	_, err := os.Stat(backupPath)
	require.NoError(t, err)
}

func TestInitCreateDirectoriesAndDetectTools(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CAAM_HOME", filepath.Join(root, "caam"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	config.SetConfigPath(filepath.Join(root, "cfg", "config.json"))
	t.Cleanup(func() { config.SetConfigPath("") })

	require.NoError(t, createDirectories(false))
	require.NoError(t, createDirectories(true))
	_, err := os.Stat(authfile.DefaultVaultPath())
	require.NoError(t, err)

	// Simulate one installed tool in PATH.
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	codexBin := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(codexBin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", binDir)

	out, err := captureStdout(t, func() error {
		detectTools(false)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "codex found")
}
