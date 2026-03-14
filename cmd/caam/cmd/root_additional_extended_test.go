package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/identity"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider/codex"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRootHelpers_FormatIdentityDisplayAndTruncateDescription(t *testing.T) {
	email, plan := formatIdentityDisplay(nil)
	require.Equal(t, "unknown", email)
	require.Equal(t, "unknown", plan)

	email, plan = formatIdentityDisplay(&identity.Identity{
		Email:    "hope@example.com",
		PlanType: "pro",
	})
	require.Equal(t, "hope@example.com", email)
	require.Equal(t, "Pro", plan)

	require.Equal(t, "", truncateDescription("", 20))
	require.Equal(t, "short", truncateDescription("short", 20))
	require.Equal(t, "t...", truncateDescription("toolong", 4))
	require.Equal(t, "tool...", truncateDescription("toolong description", 7))
}

func TestRootHelpers_ShowTokenWarningsGracefulWithoutDependencies(t *testing.T) {
	originalVault := vault
	originalRegistry := registry
	originalProfileStore := profileStore
	t.Cleanup(func() {
		vault = originalVault
		registry = originalRegistry
		profileStore = originalProfileStore
	})

	vault = nil
	registry = nil
	profileStore = nil

	showTokenWarnings(t.Context())
}

func TestRootHelpers_GetCooldownStringWithRealDB(t *testing.T) {
	layout := newStartupLayout(t)

	originalGlobalDB := globalDB
	t.Cleanup(func() {
		if globalDB != nil {
			globalDB.Close()
		}
		globalDB = originalGlobalDB
	})
	if globalDB != nil {
		globalDB.Close()
		globalDB = nil
	}

	dbHandle, err := getDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, dbHandle.Close())
		globalDB = nil
	})

	require.NoError(t, os.MkdirAll(layout.caamHome, 0o755))

	none := getCooldownString("codex", "work", health.FormatOptions{NoColor: true})
	require.Equal(t, "", none)

	_, err = dbHandle.SetCooldown("codex", "work", time.Now().UTC().Add(-5*time.Minute), time.Minute, "expired")
	require.NoError(t, err)
	expired := getCooldownString("codex", "work", health.FormatOptions{NoColor: true})
	require.Equal(t, "", expired)

	_, err = dbHandle.SetCooldown("codex", "work", time.Now().UTC(), 95*time.Minute, "active")
	require.NoError(t, err)

	plain := getCooldownString("codex", "work", health.FormatOptions{NoColor: true})
	require.Contains(t, plain, "cooldown:")
	require.Contains(t, plain, "remaining")
	require.Contains(t, plain, "1h")

	colored := getCooldownString("codex", "work", health.FormatOptions{NoColor: false})
	require.Contains(t, colored, "\x1b[")
}

func TestRunBackupJSONSuccessAndUnknownTool(t *testing.T) {
	layout := newStartupLayout(t)

	originalVault := vault
	originalProfileStore := profileStore
	t.Cleanup(func() {
		vault = originalVault
		profileStore = originalProfileStore
		require.NoError(t, backupCmd.Flags().Set("json", "false"))
	})

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	profileStore = profile.NewStore(profile.DefaultStorePath())

	authPath := filepath.Join(layout.codexHome, "auth.json")
	require.NoError(t, os.WriteFile(authPath, []byte(`{"access_token":"backup-token"}`), 0o600))

	var out bytes.Buffer
	backupCmd.SetOut(&out)
	backupCmd.SetErr(&out)
	require.NoError(t, backupCmd.Flags().Set("json", "true"))
	require.NoError(t, runBackup(backupCmd, []string{"codex", "work"}))

	var success struct {
		Success bool   `json:"success"`
		Tool    string `json:"tool"`
		Profile string `json:"profile"`
		Path    string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &success))
	require.True(t, success.Success)
	require.Equal(t, "codex", success.Tool)
	require.Equal(t, "work", success.Profile)
	require.FileExists(t, filepath.Join(success.Path, "auth.json"))

	out.Reset()
	require.NoError(t, runBackup(backupCmd, []string{"unknown", "nope"}))

	var failure struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &failure))
	require.False(t, failure.Success)
	require.Contains(t, failure.Error, "unknown tool")
}

func TestRunStatusJSONBranchesAndWarningsGate(t *testing.T) {
	layout := newStartupLayout(t)

	originalVault := vault
	originalHealthStore := healthStore
	originalRegistry := registry
	originalProfileStore := profileStore
	originalGlobalDB := globalDB
	t.Cleanup(func() {
		vault = originalVault
		healthStore = originalHealthStore
		registry = originalRegistry
		profileStore = originalProfileStore
		if globalDB != nil {
			globalDB.Close()
		}
		globalDB = originalGlobalDB
		require.NoError(t, statusCmd.Flags().Set("json", "false"))
		require.NoError(t, statusCmd.Flags().Set("no-color", "false"))
	})

	if globalDB != nil {
		globalDB.Close()
		globalDB = nil
	}

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	healthStore = health.NewStorage(filepath.Join(layout.caamHome, "health.json"))
	registry = provider.NewRegistry()
	registry.Register(codex.New())
	profileStore = profile.NewStore(profile.DefaultStorePath())

	authPath := filepath.Join(layout.codexHome, "auth.json")
	require.NoError(t, os.WriteFile(authPath, []byte(`{"access_token":"status-token"}`), 0o600))
	require.NoError(t, vault.Backup(authfile.CodexAuthFiles(), "work"))
	require.NoError(t, healthStore.SetPlanType("codex", "work", "team"))

	dbHandle, err := caamdb.Open()
	require.NoError(t, err)
	globalDB = dbHandle
	_, err = dbHandle.SetCooldown("codex", "work", time.Now().UTC(), 45*time.Minute, "cooldown")
	require.NoError(t, err)

	var out bytes.Buffer
	statusCmd.SetOut(&out)
	statusCmd.SetErr(&out)
	require.NoError(t, statusCmd.Flags().Set("json", "true"))
	require.NoError(t, statusCmd.Flags().Set("no-color", "true"))

	require.NoError(t, runStatus(statusCmd, nil))

	var output struct {
		Tools []struct {
			Tool     string `json:"tool"`
			LoggedIn bool   `json:"logged_in"`
			Health   struct {
				CooldownRemaining string `json:"cooldown_remaining"`
			} `json:"health"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &output))
	require.Len(t, output.Tools, 3)

	var codexToolFound bool
	var loggedOutCount int
	for _, tool := range output.Tools {
		if tool.Tool == "codex" {
			codexToolFound = true
			require.True(t, tool.LoggedIn)
			require.Contains(t, tool.Health.CooldownRemaining, "cooldown:")
			continue
		}
		if !tool.LoggedIn {
			loggedOutCount++
		}
	}
	require.True(t, codexToolFound)
	require.Equal(t, 2, loggedOutCount)

	err = runStatus(statusCmd, []string{"unknown"})
	require.EqualError(t, err, fmt.Sprintf("unknown tool: %s", sanitizeTerminalText("unknown")))

	jsonCmd := &cobra.Command{Use: "status"}
	jsonCmd.Flags().Bool("json", false, "")
	require.NoError(t, jsonCmd.Flags().Set("json", "true"))
	require.False(t, shouldShowWarnings(jsonCmd))

	versionLike := &cobra.Command{Use: "version"}
	versionLike.SetOut(bytes.NewBuffer(nil))
	versionLike.SetErr(bytes.NewBuffer(nil))
	require.False(t, shouldShowWarnings(versionLike))
}
