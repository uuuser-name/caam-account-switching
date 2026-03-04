package identity

import (
	"encoding/json"
	"fmt"
	"os"
)

// ExtractFromCodexAuth reads a Codex auth.json file and extracts identity from the JWT.
func ExtractFromCodexAuth(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read codex auth.json: %w", err)
	}

	var auth map[string]interface{}
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse codex auth.json: %w", err)
	}

	candidates := codexTokenCandidates(auth)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no token found in auth.json")
	}

	var lastErr error
	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		identity, err := ExtractFromJWT(candidate.value)
		if err != nil {
			lastErr = fmt.Errorf("parse jwt from %s: %w", candidate.source, err)
			continue
		}
		identity.Provider = "codex"
		return identity, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no token found in auth.json")
}

type tokenCandidate struct {
	value  string
	source string
}

func codexTokenCandidates(auth map[string]interface{}) []tokenCandidate {
	candidates := []tokenCandidate{
		{value: stringFromMap(auth, "id_token"), source: "id_token"},
		{value: stringFromMap(auth, "idToken"), source: "idToken"},
	}

	rawTokens, ok := auth["tokens"]
	if ok {
		if tokenMap, ok := rawTokens.(map[string]interface{}); ok {
			candidates = append(candidates,
				tokenCandidate{value: stringFromMap(tokenMap, "id_token"), source: "tokens.id_token"},
				tokenCandidate{value: stringFromMap(tokenMap, "idToken"), source: "tokens.idToken"},
			)
		}
	}

	candidates = append(candidates,
		tokenCandidate{value: stringFromMap(auth, "access_token"), source: "access_token"},
		tokenCandidate{value: stringFromMap(auth, "accessToken"), source: "accessToken"},
		tokenCandidate{value: stringFromMap(auth, "token"), source: "token"},
	)

	if ok {
		if tokenMap, ok := rawTokens.(map[string]interface{}); ok {
			candidates = append(candidates,
				tokenCandidate{value: stringFromMap(tokenMap, "access_token"), source: "tokens.access_token"},
				tokenCandidate{value: stringFromMap(tokenMap, "accessToken"), source: "tokens.accessToken"},
				tokenCandidate{value: stringFromMap(tokenMap, "token"), source: "tokens.token"},
			)
		}
	}

	return candidates
}

func stringFromMap(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return ""
}
