package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func writeSymlinkedCodexConfig(t *testing.T, root string) (string, string) {
	t.Helper()

	codexHome := os.Getenv("CODEX_HOME")
	configPath := filepath.Join(codexHome, "config.toml")
	canonicalDir := filepath.Join(root, "canonical-codex")
	canonicalConfigPath := filepath.Join(canonicalDir, "config.toml")

	if err := os.MkdirAll(canonicalDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(canonicalDir) error = %v", err)
	}
	if err := os.WriteFile(canonicalConfigPath, []byte("model_reasoning_effort = \"high\"\ncli_auth_credentials_store = \"keychain\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(canonicalConfigPath) error = %v", err)
	}
	if err := os.Symlink(canonicalConfigPath, configPath); err != nil {
		t.Fatalf("Symlink(config.toml) error = %v", err)
	}

	return configPath, canonicalConfigPath
}

func assertManagedCodexConfig(t *testing.T, configPath, canonicalConfigPath string) {
	t.Helper()

	info, err := os.Lstat(configPath)
	if err != nil {
		t.Fatalf("Lstat(config.toml) error = %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config.toml mode = %v, want symlink", info.Mode())
	}

	data, err := os.ReadFile(canonicalConfigPath)
	if err != nil {
		t.Fatalf("ReadFile(canonicalConfigPath) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`cli_auth_credentials_store = "file"`,
		`multi_agent = true`,
		`hide_rate_limit_model_nudge = true`,
	} {
		if !contains(text, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, text)
		}
	}
	if contains(text, `cli_auth_credentials_store = "keychain"`) {
		t.Fatalf("config.toml still contains keychain store:\n%s", text)
	}
}

func TestNext_CodexRepairsLiveConfigBeforeActivation(t *testing.T) {
	tmpDir, cleanup := setupNextTestEnv(t)
	defer cleanup()
	t.Setenv("HOME", tmpDir)

	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"access_token":"a"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(current auth) error = %v", err)
	}

	createTestProfiles(t, map[string]string{
		"a": "a",
		"b": "b",
	})

	configPath, canonicalConfigPath := writeSymlinkedCodexConfig(t, tmpDir)

	c := &cobra.Command{}
	c.Flags().Bool("dry-run", false, "")
	c.Flags().Bool("quiet", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().String("algorithm", "round_robin", "")
	_ = c.Flags().Set("algorithm", "round_robin")

	if err := runNext(c, []string{"codex"}); err != nil {
		t.Fatalf("runNext() error = %v", err)
	}

	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(active auth) error = %v", err)
	}
	if string(got) != `{"access_token":"b"}` {
		t.Fatalf("auth mismatch: got %q want %q", string(got), `{"access_token":"b"}`)
	}

	assertManagedCodexConfig(t, configPath, canonicalConfigPath)
}

func TestRunRobotAct_CodexRepairsLiveConfigBeforeActivation(t *testing.T) {
	tmpDir, cleanup := setupNextTestEnv(t)
	defer cleanup()
	t.Setenv("HOME", tmpDir)

	createTestProfiles(t, map[string]string{
		"main": "robot-token",
	})

	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	configPath, canonicalConfigPath := writeSymlinkedCodexConfig(t, tmpDir)

	c := &cobra.Command{}
	c.SetOut(io.Discard)

	if err := runRobotAct(c, []string{"activate", "codex", "main"}); err != nil {
		t.Fatalf("runRobotAct() error = %v", err)
	}

	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(active auth) error = %v", err)
	}
	if string(got) != `{"access_token":"robot-token"}` {
		t.Fatalf("auth mismatch: got %q want %q", string(got), `{"access_token":"robot-token"}`)
	}

	assertManagedCodexConfig(t, configPath, canonicalConfigPath)
}
