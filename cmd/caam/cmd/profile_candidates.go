package cmd

import (
	"strings"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/identity"
)

// filterEligibleRotationProfiles removes system/unusable profiles and de-duplicates
// aliases that resolve to the same underlying account identity.
func filterEligibleRotationProfiles(tool string, profiles []string, currentProfile string) []string {
	if vault == nil {
		vault = authfile.NewVault(authfile.DefaultVaultPath())
	}

	filtered := make([]string, 0, len(profiles))
	chosenByIdentity := make(map[string]string, len(profiles))
	identityByProfile := make(map[string]*identity.Identity, len(profiles))

	for _, profileName := range profiles {
		if authfile.IsSystemProfile(profileName) {
			continue
		}

		id := getVaultIdentity(tool, profileName)
		identityByProfile[profileName] = id

		usable, _ := assessProfileUsability(tool, profileName, id)
		if !usable {
			continue
		}

		key := identityDedupKey(id)
		if key == "" {
			filtered = append(filtered, profileName)
			continue
		}

		existing, seen := chosenByIdentity[key]
		if !seen {
			chosenByIdentity[key] = profileName
			filtered = append(filtered, profileName)
			continue
		}

		keep := choosePreferredIdentityAlias(existing, identityByProfile[existing], profileName, id, currentProfile)
		if keep == existing {
			continue
		}

		for i, selected := range filtered {
			if selected == existing {
				filtered[i] = profileName
				break
			}
		}
		chosenByIdentity[key] = profileName
	}

	return filtered
}

func identityDedupKey(id *identity.Identity) string {
	if id == nil {
		return ""
	}
	if accountID := strings.ToLower(strings.TrimSpace(id.AccountID)); accountID != "" {
		return "account:" + accountID
	}
	if email := strings.ToLower(strings.TrimSpace(id.Email)); email != "" {
		return "email:" + email
	}
	return ""
}

func choosePreferredIdentityAlias(existing string, existingID *identity.Identity, candidate string, candidateID *identity.Identity, currentProfile string) string {
	if currentProfile != "" {
		if existing == currentProfile {
			return existing
		}
		if candidate == currentProfile {
			return candidate
		}
	}

	existingScore := identityAliasScore(existing, existingID)
	candidateScore := identityAliasScore(candidate, candidateID)
	if candidateScore > existingScore {
		return candidate
	}
	if candidateScore < existingScore {
		return existing
	}

	if len(candidate) < len(existing) {
		return candidate
	}
	if len(candidate) > len(existing) {
		return existing
	}
	if candidate < existing {
		return candidate
	}
	return existing
}

func identityAliasScore(profileName string, id *identity.Identity) int {
	if id == nil || strings.TrimSpace(id.Email) == "" {
		return 0
	}

	profile := strings.ToLower(strings.TrimSpace(profileName))
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if profile == email {
		return 3
	}

	localPart, _, ok := strings.Cut(email, "@")
	if !ok || localPart == "" {
		return 0
	}
	if profile == localPart {
		return 2
	}
	for _, sep := range []string{".", "-", "_", "+"} {
		if strings.HasPrefix(profile, localPart+sep) {
			return 1
		}
	}
	return 0
}
