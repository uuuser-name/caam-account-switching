package cmd

import (
	"fmt"

	codexprovider "github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider/codex"
)

func prepareToolActivation(tool string) error {
	if tool != "codex" {
		return nil
	}
	if err := codexprovider.EnsureFileCredentialStore(codexprovider.ResolveHome()); err != nil {
		return fmt.Errorf("configure codex credential store: %w", err)
	}
	return nil
}
