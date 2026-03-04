package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/identity"
)

// preventDuplicateUserProfile blocks creating duplicate non-system profiles for
// an account identity that is already represented by another non-system profile.
func preventDuplicateUserProfile(tool string, fileSet authfile.AuthFileSet, targetProfile string) error {
	if authfile.IsSystemProfile(targetProfile) {
		return nil
	}
	if vault == nil {
		vault = authfile.NewVault(authfile.DefaultVaultPath())
	}

	active, err := vault.ActiveProfile(fileSet)
	if err == nil && active != "" && active != targetProfile && !authfile.IsSystemProfile(active) {
		return fmt.Errorf(
			"current %s auth already matches existing profile %q; refusing to create duplicate profile %q for the same account",
			tool, active, targetProfile,
		)
	}

	currentIdentity := getCurrentAuthIdentity(tool)
	currentKey := identityDedupKey(currentIdentity)
	if currentKey == "" {
		return nil
	}

	profiles, err := vault.List(tool)
	if err != nil {
		return nil
	}
	for _, profileName := range profiles {
		if profileName == targetProfile || authfile.IsSystemProfile(profileName) {
			continue
		}
		if identityDedupKey(getVaultIdentity(tool, profileName)) == currentKey {
			return fmt.Errorf(
				"current %s account identity already exists as profile %q; refusing duplicate profile %q",
				tool, profileName, targetProfile,
			)
		}
	}

	return nil
}

func getCurrentAuthIdentity(tool string) *identity.Identity {
	getFileSet, ok := tools[tool]
	if !ok {
		return nil
	}
	fileSet := getFileSet()
	if len(fileSet.Files) == 0 {
		return nil
	}

	switch tool {
	case "codex":
		id, err := identity.ExtractFromCodexAuth(fileSet.Files[0].Path)
		if err != nil {
			return nil
		}
		normalizeIdentityPlan(id)
		return id
	case "claude":
		for _, spec := range fileSet.Files {
			path := spec.Path
			base := filepath.Base(path)
			if base != ".credentials.json" && base != ".claude.json" && base != "auth.json" {
				continue
			}
			id, err := identity.ExtractFromClaudeCredentials(path)
			if err != nil {
				continue
			}
			normalizeIdentityPlan(id)
			return id
		}
	case "gemini":
		for _, spec := range fileSet.Files {
			path := spec.Path
			if base := filepath.Base(path); !strings.EqualFold(base, "settings.json") && !strings.EqualFold(base, "oauth_credentials.json") {
				continue
			}
			id, err := identity.ExtractFromGeminiConfig(path)
			if err != nil {
				continue
			}
			normalizeIdentityPlan(id)
			return id
		}
	}

	return nil
}
