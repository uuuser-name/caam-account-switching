package identity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ExtractFromClaudeCredentials reads Claude .credentials.json and extracts identity.
func ExtractFromClaudeCredentials(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read claude credentials: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var root map[string]interface{}
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse claude credentials: %w", err)
	}

	identity := &Identity{Provider: "claude"}

	raw, ok := root["claudeAiOauth"].(map[string]interface{})
	if !ok {
		return identity, nil
	}

	identity.AccountID = valueAsString(raw["accountId"])
	identity.PlanType = valueAsString(raw["subscriptionType"])
	identity.Email = valueAsString(raw["email"])
	if exp, ok := parseEpoch(raw["expiresAt"]); ok {
		identity.ExpiresAt = exp
	}

	return identity, nil
}

func parseEpoch(value interface{}) (time.Time, bool) {
	secs, ok := epochSeconds(value)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(secs, 0).UTC(), true
}

func epochSeconds(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return normalizeEpoch(n), true
	case float64:
		return normalizeEpoch(int64(v)), true
	case float32:
		return normalizeEpoch(int64(v)), true
	case int64:
		return normalizeEpoch(v), true
	case int:
		return normalizeEpoch(int64(v)), true
	case string:
		n, err := json.Number(v).Int64()
		if err != nil {
			return 0, false
		}
		return normalizeEpoch(n), true
	default:
		return 0, false
	}
}

func normalizeEpoch(value int64) int64 {
	// Treat values in milliseconds (13+ digits) as ms since epoch.
	if value > 1_000_000_000_000 {
		return value / 1000
	}
	return value
}
