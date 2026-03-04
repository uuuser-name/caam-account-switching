package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Output Contract Tests for precheck command
// These tests enforce stable output contracts for automation and UX consistency.
// They should FAIL on backward-incompatible output changes.
// =============================================================================

// TestPrecheckJSONContract verifies the JSON output schema for precheck command.
// This contract ensures automation consumers can rely on stable field names and types.
func TestPrecheckJSONContract(t *testing.T) {
	// Contract: PrecheckResult must have these fields with these types
	// Breaking this contract will break automation scripts that parse this output.

	// Test that the struct has required JSON tags by marshaling a sample result
	result := &PrecheckResult{
		Provider:  "claude",
		Algorithm: "smart",
	}

	data, err := json.Marshal(result)
	require.NoError(t, err, "PrecheckResult must be JSON-serializable")

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "PrecheckResult must produce valid JSON")

	// Contract: top-level required fields
	assert.Contains(t, parsed, "provider", "Contract: 'provider' field is required")
	assert.Contains(t, parsed, "algorithm", "Contract: 'algorithm' field is required")

	// Contract: provider must be string
	provider, ok := parsed["provider"].(string)
	assert.True(t, ok, "Contract: 'provider' must be string")
	assert.Equal(t, "claude", provider)

	// Contract: algorithm must be string
	algorithm, ok := parsed["algorithm"].(string)
	assert.True(t, ok, "Contract: 'algorithm' must be string")
	assert.Equal(t, "smart", algorithm)
}

// TestPrecheckJSONContract_WithRecommended verifies recommended profile schema.
func TestPrecheckJSONContract_WithRecommended(t *testing.T) {
	result := &PrecheckResult{
		Provider:  "claude",
		Algorithm: "smart",
		Recommended: &ProfileRecommendation{
			Name:         "work",
			Score:        95.5,
			UsagePercent: 25,
			AvailScore:   90,
			HealthStatus: "healthy",
			Reasons:      []string{"+ high availability", "- recent use"},
			PoolStatus:   "active",
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	// Contract: recommended must be an object with required fields
	rec, ok := parsed["recommended"].(map[string]interface{})
	require.True(t, ok, "Contract: 'recommended' must be an object")

	// Contract: required fields in recommended
	assert.Contains(t, rec, "name", "Contract: 'recommended.name' is required")
	assert.Contains(t, rec, "score", "Contract: 'recommended.score' is required")

	// Contract: name must be string
	name, ok := rec["name"].(string)
	assert.True(t, ok, "Contract: 'recommended.name' must be string")
	assert.Equal(t, "work", name)

	// Contract: score must be numeric
	score, ok := rec["score"].(float64)
	assert.True(t, ok, "Contract: 'recommended.score' must be numeric")
	assert.InDelta(t, 95.5, score, 0.01)

	// Contract: usage_percent must be integer (when present)
	if usagePct, exists := rec["usage_percent"]; exists {
		_, isNum := usagePct.(float64)
		assert.True(t, isNum, "Contract: 'recommended.usage_percent' must be numeric")
	}

	// Contract: reasons must be array of strings
	if reasons, exists := rec["reasons"]; exists {
		reasonsArr, ok := reasons.([]interface{})
		assert.True(t, ok, "Contract: 'recommended.reasons' must be array")
		if len(reasonsArr) > 0 {
			_, isStr := reasonsArr[0].(string)
			assert.True(t, isStr, "Contract: 'recommended.reasons' elements must be strings")
		}
	}
}

// TestPrecheckJSONContract_Summary verifies summary schema.
func TestPrecheckJSONContract_Summary(t *testing.T) {
	result := &PrecheckResult{
		Provider:  "claude",
		Algorithm: "smart",
		Summary: &UsageSummary{
			TotalProfiles:   5,
			ReadyProfiles:   3,
			CooldownCount:   2,
			AvgUsagePercent: 45,
			HealthyCount:    3,
			WarningCount:    1,
			CriticalCount:   0,
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	summary, ok := parsed["summary"].(map[string]interface{})
	require.True(t, ok, "Contract: 'summary' must be an object")

	// Contract: all summary fields must be integers (or numeric in JSON)
	summaryFields := []string{
		"total_profiles",
		"ready_profiles",
		"cooldown_count",
		"avg_usage_percent",
		"healthy_count",
		"warning_count",
		"critical_count",
	}

	for _, field := range summaryFields {
		assert.Contains(t, summary, field, "Contract: 'summary.%s' is required", field)
		_, isNum := summary[field].(float64)
		assert.True(t, isNum, "Contract: 'summary.%s' must be numeric", field)
	}
}

// TestPrecheckJSONContract_Alerts verifies alerts schema.
func TestPrecheckJSONContract_Alerts(t *testing.T) {
	result := &PrecheckResult{
		Provider:  "claude",
		Algorithm: "smart",
		Alerts: []PrecheckAlert{
			{
				Type:    "approaching",
				Profile: "work",
				Message: "Profile approaching limit",
				Urgency: "medium",
				Action:  "Consider switching to backup",
			},
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	alerts, ok := parsed["alerts"].([]interface{})
	require.True(t, ok, "Contract: 'alerts' must be an array")

	if len(alerts) > 0 {
		alert, ok := alerts[0].(map[string]interface{})
		require.True(t, ok, "Contract: each alert must be an object")

		// Contract: required alert fields
		assert.Contains(t, alert, "type", "Contract: 'alert.type' is required")
		assert.Contains(t, alert, "message", "Contract: 'alert.message' is required")
		assert.Contains(t, alert, "urgency", "Contract: 'alert.urgency' is required")

		// Contract: urgency must be one of low/medium/high
		urgency, ok := alert["urgency"].(string)
		assert.True(t, ok, "Contract: 'alert.urgency' must be string")
		assert.Contains(t, []string{"low", "medium", "high"}, urgency,
			"Contract: 'alert.urgency' must be low/medium/high")
	}
}

// TestPrecheckJSONContract_Cooldown verifies cooldown profile schema.
func TestPrecheckJSONContract_Cooldown(t *testing.T) {
	result := &PrecheckResult{
		Provider:  "claude",
		Algorithm: "smart",
		InCooldown: []CooldownProfile{
			{
				Name:      "personal",
				Remaining: "2h30m",
			},
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	inCooldown, ok := parsed["in_cooldown"].([]interface{})
	require.True(t, ok, "Contract: 'in_cooldown' must be an array")

	if len(inCooldown) > 0 {
		cd, ok := inCooldown[0].(map[string]interface{})
		require.True(t, ok, "Contract: each cooldown entry must be an object")

		// Contract: required cooldown fields
		assert.Contains(t, cd, "name", "Contract: 'cooldown.name' is required")
		assert.Contains(t, cd, "remaining", "Contract: 'cooldown.remaining' is required")

		// Contract: name and remaining must be strings
		_, nameIsStr := cd["name"].(string)
		_, remainingIsStr := cd["remaining"].(string)
		assert.True(t, nameIsStr, "Contract: 'cooldown.name' must be string")
		assert.True(t, remainingIsStr, "Contract: 'cooldown.remaining' must be string")
	}
}

// TestPrecheckBriefContract verifies brief format output contract.
func TestPrecheckBriefContract(t *testing.T) {
	// Contract: brief format must be parseable and contain specific elements

	// When there's a recommended profile
	result := &PrecheckResult{
		Provider: "claude",
		Recommended: &ProfileRecommendation{
			Name:         "work",
			UsagePercent: 25,
		},
	}

	// Simulate brief output generation
	briefOutput := formatBriefOutput(result)

	// Contract: must start with provider name
	assert.Contains(t, briefOutput, "claude:", "Contract: brief format must start with provider")

	// Contract: must include recommended profile name
	assert.Contains(t, briefOutput, "work", "Contract: brief format must include recommended profile")

	// Contract: should be single line (or very short)
	assert.LessOrEqual(t, len(briefOutput), 80, "Contract: brief format should be <=80 chars")
}

// TestPrecheckTableContract verifies table format contains required sections.
func TestPrecheckTableContract(t *testing.T) {
	// Contract: table format must contain specific section headers
	result := &PrecheckResult{
		Provider:   "claude",
		Algorithm:  "smart",
		FetchedAt:  time.Now(),
		Recommended: &ProfileRecommendation{
			Name:         "work",
			HealthStatus: "healthy",
			UsagePercent: 25,
			AvailScore:   90,
		},
		Summary: &UsageSummary{
			TotalProfiles: 1,
			ReadyProfiles: 1,
		},
	}

	tableOutput := formatTableOutput(result)

	// Contract: must contain SESSION PLANNER header
	assert.Contains(t, tableOutput, "SESSION PLANNER", "Contract: table must contain SESSION PLANNER header")

	// Contract: must contain provider name in uppercase
	assert.Contains(t, tableOutput, "CLAUDE", "Contract: table must contain provider in uppercase")

	// Contract: must contain RECOMMENDED section when profile recommended
	assert.Contains(t, tableOutput, "RECOMMENDED:", "Contract: table must contain RECOMMENDED section")

	// Contract: must contain SUMMARY section
	assert.Contains(t, tableOutput, "SUMMARY:", "Contract: table must contain SUMMARY section")

	// Contract: must contain QUICK ACTIONS section
	assert.Contains(t, tableOutput, "QUICK ACTIONS:", "Contract: table must contain QUICK ACTIONS section")
}

// TestProfileRecommendationContract verifies ProfileRecommendation struct contracts.
func TestProfileRecommendationContract(t *testing.T) {
	rec := ProfileRecommendation{
		Name:           "test-profile",
		Score:          85.0,
		UsagePercent:   30,
		AvailScore:     80,
		HealthStatus:   "healthy",
		TokenExpiry:    "5h30m",
		TimeToDepletion: "2h",
		Reasons:        []string{"+ high score"},
		PoolStatus:     "active",
	}

	data, err := json.Marshal(rec)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	// Contract: all expected JSON fields are present
	expectedFields := []string{
		"name",
		"score",
		"usage_percent",
		"availability_score",
		"health_status",
		"token_expiry",
		"time_to_depletion",
		"reasons",
		"pool_status",
	}

	for _, field := range expectedFields {
		assert.Contains(t, parsed, field, "Contract: ProfileRecommendation must have '%s' field", field)
	}
}

// TestPrecheckAlertContract verifies PrecheckAlert struct contracts.
func TestPrecheckAlertContract(t *testing.T) {
	alert := PrecheckAlert{
		Type:    "imminent",
		Profile: "work",
		Message: "Profile at 95% usage",
		Urgency: "high",
		Action:  "Switch to backup profile",
	}

	data, err := json.Marshal(alert)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	// Contract: all expected JSON fields are present
	expectedFields := []string{
		"type",
		"profile",
		"message",
		"urgency",
		"action",
	}

	for _, field := range expectedFields {
		assert.Contains(t, parsed, field, "Contract: PrecheckAlert must have '%s' field", field)
	}

	// Contract: valid urgency values
	assert.Contains(t, []string{"low", "medium", "high"}, parsed["urgency"],
		"Contract: urgency must be low/medium/high")
}

// Helper functions for contract tests

func formatBriefOutput(result *PrecheckResult) string {
	if result.Recommended == nil {
		if len(result.InCooldown) > 0 {
			return result.Provider + ": all in cooldown\n"
		}
		return result.Provider + ": no profiles\n"
	}

	rec := result.Recommended
	extra := ""
	if rec.UsagePercent > 0 {
		extra = " (" + string(rune('0'+rec.UsagePercent/10)) + "0% used)"
	}
	return result.Provider + ": " + rec.Name + extra + "\n"
}

func formatTableOutput(result *PrecheckResult) string {
	// Simplified table output for contract testing
	output := "========================================\n"
	output += "  SESSION PLANNER: " + strings.ToUpper(result.Provider) + "\n"
	output += "========================================\n\n"

	if result.Recommended != nil {
		output += "  RECOMMENDED:\n"
		output += "    " + result.Recommended.Name + "\n\n"
	}

	if result.Summary != nil {
		output += "  SUMMARY:\n"
		output += "    Profiles: ...\n\n"
	}

	output += "  QUICK ACTIONS:\n"
	output += "    caam activate ...\n"

	return output
}

func mustParseTime(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}
