package identity

import (
	"encoding/json"
	"fmt"
	"os"
)

// ExtractFromGeminiConfig reads Gemini/Google auth config and extracts identity.
func ExtractFromGeminiConfig(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gemini config: %w", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse gemini config: %w", err)
	}

	identity := &Identity{Provider: "gemini"}

	identity.Email = pickString(root, "client_email", "user_email", "email")
	if identity.Email == "" {
		if account, ok := root["account"].(map[string]interface{}); ok {
			identity.Email = pickString(account, "email", "user_email")
		}
	}
	if identity.Email == "" {
		if user, ok := root["user"].(map[string]interface{}); ok {
			identity.Email = pickString(user, "email", "user_email")
		}
	}

	identity.Organization = pickString(root, "project_id", "projectId", "quota_project_id")
	return identity, nil
}
