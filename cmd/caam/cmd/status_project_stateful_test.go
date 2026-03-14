package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/project"
	"github.com/stretchr/testify/require"
)

func captureCommandStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := fn()

	require.NoError(t, w.Close())
	os.Stdout = originalStdout

	output, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	require.NoError(t, r.Close())

	return string(output), runErr
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()

	absPath, err := filepath.Abs(path)
	require.NoError(t, err)
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved
	}
	return absPath
}

func TestStatusCommand_RealStateJSON(t *testing.T) {
	layout := newStartupLayout(t)

	originalVault := vault
	originalHealthStore := healthStore
	t.Cleanup(func() {
		vault = originalVault
		healthStore = originalHealthStore
		require.NoError(t, statusCmd.Flags().Set("json", "false"))
		require.NoError(t, statusCmd.Flags().Set("no-color", "false"))
	})

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	healthStore = health.NewStorage(filepath.Join(layout.caamHome, "health.json"))

	authPath := filepath.Join(layout.codexHome, "auth.json")
	require.NoError(t, os.WriteFile(authPath, []byte(`{"access_token":"status-token"}`), 0o600))
	require.NoError(t, vault.Backup(authfile.CodexAuthFiles(), "work"))

	var buf bytes.Buffer
	statusCmd.SetOut(&buf)
	statusCmd.SetErr(&buf)
	require.NoError(t, statusCmd.Flags().Set("json", "true"))
	require.NoError(t, statusCmd.Flags().Set("no-color", "true"))
	require.NoError(t, runStatus(statusCmd, []string{"codex"}))

	var output struct {
		Tools []struct {
			Tool          string `json:"tool"`
			ActiveProfile string `json:"active_profile"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &output))
	require.Len(t, output.Tools, 1)
	require.Equal(t, "codex", output.Tools[0].Tool)
	require.Equal(t, "work", output.Tools[0].ActiveProfile)
}

func TestProjectListCommand_RealStateJSON(t *testing.T) {
	layout := newStartupLayout(t)

	originalProjectStore := projectStore
	t.Cleanup(func() {
		projectStore = originalProjectStore
		require.NoError(t, projectListCmd.Flags().Set("json", "false"))
	})

	projectStore = project.NewStore(filepath.Join(layout.caamHome, "projects.json"))

	repoDir := filepath.Join(layout.root, "repo")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, projectStore.SetAssociation(repoDir, "codex", "work"))
	require.NoError(t, projectStore.SetAssociation(repoDir, "claude", "team"))
	canonicalRepoDir := canonicalPath(t, repoDir)

	var buf bytes.Buffer
	projectListCmd.SetOut(&buf)
	projectListCmd.SetErr(&buf)
	require.NoError(t, projectListCmd.Flags().Set("json", "true"))
	require.NoError(t, projectListCmd.RunE(projectListCmd, nil))

	var output struct {
		Projects []struct {
			Path      string            `json:"path"`
			Providers map[string]string `json:"providers"`
		} `json:"projects"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &output))
	require.Equal(t, 1, output.Count)
	require.Len(t, output.Projects, 1)
	require.Equal(t, canonicalRepoDir, output.Projects[0].Path)
	require.Equal(t, "work", output.Projects[0].Providers["codex"])
	require.Equal(t, "team", output.Projects[0].Providers["claude"])
}

func TestProjectShowCommand_RealStateOutput(t *testing.T) {
	layout := newStartupLayout(t)

	originalProjectStore := projectStore
	t.Cleanup(func() { projectStore = originalProjectStore })

	projectStore = project.NewStore(filepath.Join(layout.caamHome, "projects.json"))

	parentDir := filepath.Join(layout.root, "repo")
	childDir := filepath.Join(parentDir, "child")
	require.NoError(t, os.MkdirAll(childDir, 0o755))
	require.NoError(t, projectStore.SetAssociation(parentDir, "codex", "work"))
	require.NoError(t, projectStore.SetAssociation(childDir, "claude", "team"))
	canonicalParentDir := canonicalPath(t, parentDir)
	canonicalChildDir := canonicalPath(t, childDir)

	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(childDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWD)) })

	output, runErr := captureCommandStdout(t, func() error {
		return projectShowCmd.RunE(projectShowCmd, nil)
	})
	require.NoError(t, runErr)
	require.True(t, strings.Contains(output, "Project: "+canonicalChildDir))
	require.True(t, strings.Contains(output, "claude: team"))
	require.True(t, strings.Contains(output, "codex: work  (from "+canonicalParentDir+")"))
}
