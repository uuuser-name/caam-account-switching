package cmd

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/stretchr/testify/require"
)

func TestPreventDuplicateUserProfile_BlocksDuplicateAccountIdentity(t *testing.T) {
	vaultDir, codexHome := setupDriftGuardTestEnv(t)
	fileSet := tools["codex"]()

	// Existing user profile for account A.
	writeVaultCodexProfile(t, vaultDir, "primary", "a@example.com", "acc-A", "token-a")

	// Current auth also belongs to account A.
	writeCurrentCodexAuth(t, codexHome, "a@example.com", "acc-A", "token-a-new")

	err := preventDuplicateUserProfile("codex", fileSet, "secondary")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists as profile")
}

func TestPreventDuplicateUserProfile_AllowsPromotionFromSystemSnapshot(t *testing.T) {
	vaultDir, codexHome := setupDriftGuardTestEnv(t)
	fileSet := tools["codex"]()

	// Only a system snapshot exists for this account identity.
	writeVaultCodexProfile(t, vaultDir, "_backup_20260304_123000", "a@example.com", "acc-A", "token-a")
	writeCurrentCodexAuth(t, codexHome, "a@example.com", "acc-A", "token-a")

	err := preventDuplicateUserProfile("codex", fileSet, "primary")
	require.NoError(t, err)
}

func TestPreventDuplicateUserProfile_IgnoresProfileListErrors(t *testing.T) {
	_, codexHome := setupDriftGuardTestEnv(t)
	fileSet := tools["codex"]()

	brokenBase := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(brokenBase, "codex"), []byte("not-a-directory"), 0o600))
	vault = authfile.NewVault(brokenBase)

	writeCurrentCodexAuth(t, codexHome, "a@example.com", "acc-A", "token-a")

	err := preventDuplicateUserProfile("codex", fileSet, "secondary")
	require.NoError(t, err)
}

func TestPreventDuplicateUserProfile_IgnoresMalformedCurrentIdentity(t *testing.T) {
	_, codexHome := setupDriftGuardTestEnv(t)
	fileSet := tools["codex"]()

	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte("{not-json"), 0o600))

	err := preventDuplicateUserProfile("codex", fileSet, "secondary")
	require.NoError(t, err)
}

func setupDriftGuardTestEnv(t *testing.T) (vaultDir string, codexHome string) {
	t.Helper()

	origVault := vault
	origTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		origTools[k] = v
	}

	base := t.TempDir()
	vaultDir = filepath.Join(base, "vault")
	codexHome = filepath.Join(base, "codex-home")
	require.NoError(t, os.MkdirAll(codexHome, 0o755))

	vault = authfile.NewVault(vaultDir)
	tools = map[string]func() authfile.AuthFileSet{
		"codex": func() authfile.AuthFileSet {
			return authfile.AuthFileSet{
				Tool: "codex",
				Files: []authfile.AuthFileSpec{
					{
						Tool:     "codex",
						Path:     filepath.Join(codexHome, "auth.json"),
						Required: true,
					},
				},
			}
		},
	}

	t.Cleanup(func() {
		vault = origVault
		tools = origTools
	})

	return vaultDir, codexHome
}

func writeVaultCodexProfile(t *testing.T, vaultDir, profileName, email, accountID, accessToken string) {
	t.Helper()

	profileDir := filepath.Join(vaultDir, "codex", profileName)
	require.NoError(t, os.MkdirAll(profileDir, 0o755))

	idToken := buildGuardJWT(t, map[string]any{
		"email":      email,
		"account_id": accountID,
	})
	payload := map[string]any{
		"tokens": map[string]any{
			"id_token":     idToken,
			"access_token": accessToken,
			"account_id":   accountID,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth.json"), raw, 0o600))
}

func writeCurrentCodexAuth(t *testing.T, codexHome, email, accountID, accessToken string) {
	t.Helper()

	idToken := buildGuardJWT(t, map[string]any{
		"email":      email,
		"account_id": accountID,
	})
	payload := map[string]any{
		"tokens": map[string]any{
			"id_token":     idToken,
			"access_token": accessToken,
			"account_id":   accountID,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), raw, 0o600))
}

func buildGuardJWT(t *testing.T, claims map[string]any) string {
	t.Helper()

	headerRaw, err := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	require.NoError(t, err)
	claimsRaw, err := json.Marshal(claims)
	require.NoError(t, err)

	return base64.RawURLEncoding.EncodeToString(headerRaw) + "." +
		base64.RawURLEncoding.EncodeToString(claimsRaw) + ".sig"
}
