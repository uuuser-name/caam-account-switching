package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/stretchr/testify/require"
)

type startupLayout struct {
	root      string
	home      string
	xdgData   string
	xdgConfig string
	caamHome  string
	codexHome string
	binDir    string
}

func newStartupLayout(t *testing.T) startupLayout {
	t.Helper()

	root := t.TempDir()
	layout := startupLayout{
		root:      root,
		home:      filepath.Join(root, "home"),
		xdgData:   filepath.Join(root, "xdg-data"),
		xdgConfig: filepath.Join(root, "xdg-config"),
		caamHome:  filepath.Join(root, "caam-home"),
		codexHome: filepath.Join(root, "codex-home"),
		binDir:    filepath.Join(root, "bin"),
	}

	for _, dir := range []string{layout.home, layout.xdgData, layout.xdgConfig, layout.caamHome, layout.codexHome, layout.binDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	t.Setenv("HOME", layout.home)
	t.Setenv("XDG_DATA_HOME", layout.xdgData)
	t.Setenv("XDG_CONFIG_HOME", layout.xdgConfig)
	t.Setenv("CAAM_HOME", layout.caamHome)
	t.Setenv("CODEX_HOME", layout.codexHome)

	return layout
}

func runStartupCommand(t *testing.T, layout startupLayout, extraEnv map[string]string, args ...string) (string, error) {
	t.Helper()

	exe, err := os.Executable()
	require.NoError(t, err)

	cmdArgs := append([]string{"-test.run=^TestStartupCLIHelper$", "--"}, args...)
	cmd := exec.Command(exe, cmdArgs...)
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env, "GO_WANT_STARTUP_CLI_HELPER=1")
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeFakeCodexBinary(t *testing.T, layout startupLayout, capturePath string) {
	t.Helper()

	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + capturePath + "\"\nexit 0\n"
	binaryPath := filepath.Join(layout.binDir, "codex")
	require.NoError(t, os.WriteFile(binaryPath, []byte(script), 0o755))
	t.Setenv("PATH", layout.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func seedCodexProfile(t *testing.T, profileName, token string) *profile.Profile {
	t.Helper()

	fileSet := authfile.CodexAuthFiles()
	for _, spec := range fileSet.Files {
		if !spec.Required {
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(spec.Path), 0o755))
		require.NoError(t, os.WriteFile(spec.Path, []byte(token), 0o600))
	}

	v := authfile.NewVault(authfile.DefaultVaultPath())
	require.NoError(t, v.Backup(fileSet, profileName))

	store := profile.NewStore(profile.DefaultStorePath())
	prof, err := store.Create("codex", profileName, "oauth")
	require.NoError(t, err)
	return prof
}

func seedCodexSession(t *testing.T, profileName, token, sessionID string) {
	t.Helper()

	prof := seedCodexProfile(t, profileName, token)
	prof.LastSessionID = sessionID
	require.NoError(t, prof.Save())
}

func TestStartup_InitQuietCreatesDirectories(t *testing.T) {
	layout := newStartupLayout(t)

	output, err := runStartupCommand(t, layout, nil, "init", "--quiet", "--no-shell")
	require.NoError(t, err, output)

	for _, dir := range []string{
		config.DefaultDataPath(),
		authfile.DefaultVaultPath(),
		profile.DefaultStorePath(),
		filepath.Dir(config.ConfigPath()),
	} {
		info, statErr := os.Stat(dir)
		require.NoError(t, statErr, dir)
		require.True(t, info.IsDir(), dir)
	}
}

func TestStartup_ShellInitZshEmitsInitScript(t *testing.T) {
	layout := newStartupLayout(t)

	output, err := runStartupCommand(t, layout, nil, "shell", "init", "--zsh")
	require.NoError(t, err, output)
	require.Contains(t, output, "# caam shell integration for zsh")
	require.Contains(t, output, "shell completion zsh")
	require.NotContains(t, output, "_init_completion")
	require.NotContains(t, output, "complete -F")
}

func TestStartup_ResumeCodexUsesStoredSessionID(t *testing.T) {
	layout := newStartupLayout(t)
	capturePath := filepath.Join(layout.root, "resume-args.txt")

	writeFakeCodexBinary(t, layout, capturePath)
	seedCodexSession(t, "work", `{"access_token":"resume-token"}`, "sess-123")

	output, err := runStartupCommand(t, layout, nil, "resume", "codex", "work", "continue-now")
	require.NoError(t, err, output)

	captured, readErr := os.ReadFile(capturePath)
	require.NoError(t, readErr)
	got := strings.Fields(string(captured))
	require.Equal(t, []string{"resume", "sess-123", "continue-now"}, got)
}

func TestStartup_RunCodexUsesActiveProfileFromTempHome(t *testing.T) {
	layout := newStartupLayout(t)
	capturePath := filepath.Join(layout.root, "run-args.txt")

	writeFakeCodexBinary(t, layout, capturePath)
	seedCodexProfile(t, "work", `{"access_token":"run-token"}`)

	output, err := runStartupCommand(t, layout, nil, "run", "codex", "status", "check")
	require.NoError(t, err, output)

	captured, readErr := os.ReadFile(capturePath)
	require.NoError(t, readErr)
	require.Equal(t, []string{"status", "check"}, strings.Fields(string(captured)))
}
