package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/spf13/cobra"
)

type workspaceCommandTestEnv struct {
	codexHome string
	vault     *authfile.Vault
}

func setupWorkspaceCommandTestEnv(t *testing.T) workspaceCommandTestEnv {
	t.Helper()

	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex-home")
	configPath := filepath.Join(root, "config", "config.json")
	vaultPath := filepath.Join(root, "vault")

	for _, dir := range []string{homeDir, codexHome, filepath.Dir(configPath), vaultPath} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CAAM_HOME", filepath.Join(root, "caam-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))

	config.SetConfigPath(configPath)
	t.Cleanup(func() { config.SetConfigPath("") })

	oldVault := vault
	testVault := authfile.NewVault(vaultPath)
	vault = testVault
	t.Cleanup(func() { vault = oldVault })

	return workspaceCommandTestEnv{
		codexHome: codexHome,
		vault:     testVault,
	}
}

func writeCodexProfileToVault(t *testing.T, testVault *authfile.Vault, name, content string) {
	t.Helper()

	profileDir := testVault.ProfilePath("codex", name)
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", profileDir, err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "auth.json"), []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile(profile %q) error = %v", name, err)
	}
}

func newWorkspaceCreateTestCommand(t *testing.T, mappings map[string]string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().String("claude", "", "")
	cmd.Flags().String("codex", "", "")
	cmd.Flags().String("gemini", "", "")
	for key, value := range mappings {
		if err := cmd.Flags().Set(key, value); err != nil {
			t.Fatalf("Flags().Set(%q) error = %v", key, err)
		}
	}
	return cmd
}

func newWorkspaceListTestCommand(t *testing.T, jsonOutput bool) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	if jsonOutput {
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatalf("Flags().Set(json) error = %v", err)
		}
	}
	return cmd
}

func TestWorkspaceCommandFlow_CreateUpdateSwitchListDelete(t *testing.T) {
	env := setupWorkspaceCommandTestEnv(t)

	writeCodexProfileToVault(t, env.vault, "work-codex", `{"access_token":"work"}`)
	writeCodexProfileToVault(t, env.vault, "alt-codex", `{"access_token":"alt"}`)

	createOut, err := captureStdout(t, func() error {
		return runWorkspaceCreate(newWorkspaceCreateTestCommand(t, map[string]string{
			"codex": "work-codex",
		}), []string{"work"})
	})
	if err != nil {
		t.Fatalf("runWorkspaceCreate(create) error = %v", err)
	}
	if !strings.Contains(createOut, "Created workspace 'work':") {
		t.Fatalf("create output missing workspace header: %q", createOut)
	}
	if !strings.Contains(createOut, "codex: work-codex") {
		t.Fatalf("create output missing codex mapping: %q", createOut)
	}

	updateOut, err := captureStdout(t, func() error {
		return runWorkspaceCreate(newWorkspaceCreateTestCommand(t, map[string]string{
			"codex": "alt-codex",
		}), []string{"work"})
	})
	if err != nil {
		t.Fatalf("runWorkspaceCreate(update) error = %v", err)
	}
	if !strings.Contains(updateOut, "Updating existing workspace 'work'") {
		t.Fatalf("update output missing existing-workspace notice: %q", updateOut)
	}
	if !strings.Contains(updateOut, "codex: alt-codex") {
		t.Fatalf("update output missing updated codex mapping: %q", updateOut)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	gotProfiles := cfg.GetWorkspace("work")
	if gotProfiles["codex"] != "alt-codex" {
		t.Fatalf("workspace codex mapping = %q, want %q", gotProfiles["codex"], "alt-codex")
	}

	authPath := filepath.Join(env.codexHome, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"access_token":"old"}`), 0600); err != nil {
		t.Fatalf("WriteFile(active auth) error = %v", err)
	}

	switchOut, err := captureStdout(t, func() error {
		return runWorkspace(&cobra.Command{}, []string{"work"})
	})
	if err != nil {
		t.Fatalf("runWorkspace(switch) error = %v", err)
	}
	if !strings.Contains(switchOut, "Switching to workspace 'work'") {
		t.Fatalf("switch output missing start banner: %q", switchOut)
	}
	if !strings.Contains(switchOut, "Switched to workspace 'work':") {
		t.Fatalf("switch output missing completion banner: %q", switchOut)
	}
	if !strings.Contains(switchOut, "codex: alt-codex") {
		t.Fatalf("switch output missing activated profile: %q", switchOut)
	}

	gotAuth, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(active auth) error = %v", err)
	}
	if string(gotAuth) != `{"access_token":"alt"}` {
		t.Fatalf("active auth = %q, want %q", string(gotAuth), `{"access_token":"alt"}`)
	}

	listOut, err := captureStdout(t, func() error {
		return runWorkspaceList(newWorkspaceListTestCommand(t, false), nil)
	})
	if err != nil {
		t.Fatalf("runWorkspaceList(text) error = %v", err)
	}
	if !strings.Contains(listOut, "* work") {
		t.Fatalf("list output missing current workspace marker: %q", listOut)
	}
	if !strings.Contains(listOut, "Current: work") {
		t.Fatalf("list output missing current workspace footer: %q", listOut)
	}

	deleteOut, err := captureStdout(t, func() error {
		return runWorkspaceDelete(&cobra.Command{}, []string{"work"})
	})
	if err != nil {
		t.Fatalf("runWorkspaceDelete() error = %v", err)
	}
	if !strings.Contains(deleteOut, "Deleted workspace 'work'") {
		t.Fatalf("delete output missing confirmation: %q", deleteOut)
	}

	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load() after delete error = %v", err)
	}
	if cfg.GetWorkspace("work") != nil {
		t.Fatalf("workspace should be deleted, got %+v", cfg.GetWorkspace("work"))
	}
}

func TestWorkspaceListBranches_EmptyAndJSON(t *testing.T) {
	_ = setupWorkspaceCommandTestEnv(t)

	emptyOut, err := captureStdout(t, func() error {
		return runWorkspace(newWorkspaceListTestCommand(t, false), nil)
	})
	if err != nil {
		t.Fatalf("runWorkspace(empty list) error = %v", err)
	}
	if !strings.Contains(emptyOut, "No workspaces defined.") {
		t.Fatalf("empty list output missing guidance: %q", emptyOut)
	}

	cfg := config.DefaultConfig()
	cfg.CreateWorkspace("work", map[string]string{"codex": "work-codex"})
	cfg.SetCurrentWorkspace("work")
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	jsonOut, err := captureStdout(t, func() error {
		return runWorkspace(newWorkspaceListTestCommand(t, true), nil)
	})
	if err != nil {
		t.Fatalf("runWorkspace(json list) error = %v", err)
	}

	var listed []struct {
		Name     string            `json:"name"`
		Current  bool              `json:"current"`
		Profiles map[string]string `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &listed); err != nil {
		t.Fatalf("json.Unmarshal(list output) error = %v\noutput=%s", err, jsonOut)
	}
	if len(listed) != 1 {
		t.Fatalf("listed workspaces = %d, want 1", len(listed))
	}
	if listed[0].Name != "work" || !listed[0].Current {
		t.Fatalf("unexpected listed workspace = %+v", listed[0])
	}
	if listed[0].Profiles["codex"] != "work-codex" {
		t.Fatalf("listed codex profile = %q, want %q", listed[0].Profiles["codex"], "work-codex")
	}
}

func TestWorkspaceCreateValidationErrors(t *testing.T) {
	env := setupWorkspaceCommandTestEnv(t)
	_ = env

	if err := runWorkspaceCreate(newWorkspaceCreateTestCommand(t, nil), []string{"work"}); err == nil || !strings.Contains(err.Error(), "at least one profile mapping is required") {
		t.Fatalf("runWorkspaceCreate(no mappings) error = %v, want missing-mapping error", err)
	}

	if err := runWorkspaceCreate(newWorkspaceCreateTestCommand(t, map[string]string{
		"codex": "missing",
	}), []string{"work"}); err == nil || !strings.Contains(err.Error(), "profile codex/missing does not exist") {
		t.Fatalf("runWorkspaceCreate(missing profile) error = %v, want missing-profile error", err)
	}

	writeCodexProfileToVault(t, env.vault, "reserved-codex", `{"access_token":"reserved"}`)
	if err := runWorkspaceCreate(newWorkspaceCreateTestCommand(t, map[string]string{
		"codex": "reserved-codex",
	}), []string{"_reserved"}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("runWorkspaceCreate(reserved name) error = %v, want reserved-name error", err)
	}
}
