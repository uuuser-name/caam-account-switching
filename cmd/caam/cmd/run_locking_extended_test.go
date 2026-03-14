package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/stretchr/testify/require"
)

func seedGeminiRunProfile(t *testing.T, profileName, marker string) *profile.Profile {
	t.Helper()

	fileSet := authfile.GeminiAuthFiles()
	for _, spec := range fileSet.Files {
		if !spec.Required {
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(spec.Path), 0o755))
		require.NoError(t, os.WriteFile(spec.Path, []byte(`{"marker":"`+marker+`"}`), 0o600))
	}

	require.NoError(t, vault.Backup(fileSet, profileName))

	prof, err := profileStore.Create("gemini", profileName, "oauth")
	require.NoError(t, err)
	return prof
}

func TestLoadOrCreateRunProfileCreatesFallbackAndLoadsStoredProfile(t *testing.T) {
	newStartupLayout(t)

	originalProfileStore := profileStore
	t.Cleanup(func() { profileStore = originalProfileStore })

	profileStore = nil
	created, err := loadOrCreateRunProfile("gemini", "missing")
	require.NoError(t, err)
	require.Equal(t, "missing", created.Name)
	require.Equal(t, "gemini", created.Provider)
	require.Equal(t, "oauth", created.AuthMode)
	require.Equal(t, filepath.Join(profile.DefaultStorePath(), "gemini", "missing"), created.BasePath)

	profileStore = profile.NewStore(profile.DefaultStorePath())
	stored, err := profileStore.Create("gemini", "existing", "oauth")
	require.NoError(t, err)

	loaded, err := loadOrCreateRunProfile("gemini", "existing")
	require.NoError(t, err)
	require.Equal(t, stored.Name, loaded.Name)
	require.Equal(t, stored.Provider, loaded.Provider)
	require.Equal(t, stored.BasePath, loaded.BasePath)
}

func TestProfileReadyForLockAndSwitchToUnlockedProfile(t *testing.T) {
	newStartupLayout(t)

	originalVault := vault
	originalProfileStore := profileStore
	t.Cleanup(func() {
		vault = originalVault
		profileStore = originalProfileStore
	})

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	profileStore = profile.NewStore(profile.DefaultStorePath())

	busy := seedGeminiRunProfile(t, "busy", "busy")
	ready := seedGeminiRunProfile(t, "ready", "ready")

	require.True(t, profileReadyForLock(ready))
	require.True(t, profileReadyForLock(busy))

	require.NoError(t, busy.LockWithCleanup())
	t.Cleanup(func() { _ = busy.Unlock() })
	require.False(t, profileReadyForLock(busy))

	selected, nextProf, err := switchToUnlockedProfile(
		"gemini",
		"busy",
		authfile.GeminiAuthFiles(),
		rotation.NewSelector(rotation.AlgorithmSmart, nil, nil),
		true,
	)
	require.NoError(t, err)
	require.Equal(t, "ready", selected)
	require.Equal(t, "ready", nextProf.Name)

	active, err := vault.ActiveProfile(authfile.GeminiAuthFiles())
	require.NoError(t, err)
	require.Equal(t, "ready", active)
}

func TestSwitchToUnlockedProfileFailsWithoutUnlockedCandidates(t *testing.T) {
	newStartupLayout(t)

	originalVault := vault
	originalProfileStore := profileStore
	t.Cleanup(func() {
		vault = originalVault
		profileStore = originalProfileStore
	})

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	profileStore = profile.NewStore(profile.DefaultStorePath())

	_ = seedGeminiRunProfile(t, "busy", "busy")
	locked := seedGeminiRunProfile(t, "locked", "locked")
	require.NoError(t, locked.LockWithCleanup())
	t.Cleanup(func() { _ = locked.Unlock() })

	_, _, err := switchToUnlockedProfile(
		"gemini",
		"busy",
		authfile.GeminiAuthFiles(),
		rotation.NewSelector(rotation.AlgorithmSmart, nil, nil),
		true,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no unlocked candidate profiles")
}
