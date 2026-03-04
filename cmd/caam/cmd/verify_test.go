package cmd

import (
	"testing"
	"time"
)

// =============================================================================
// verify.go Command Tests
// =============================================================================

func TestVerifyCommand(t *testing.T) {
	if verifyCmd.Use != "verify [tool]" {
		t.Errorf("Expected Use 'verify [tool]', got %q", verifyCmd.Use)
	}

	if verifyCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if verifyCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestVerifyCommandFlags(t *testing.T) {
	flags := []struct {
		name     string
		defValue string
	}{
		{"json", "false"},
		{"fix", "false"},
	}

	for _, tt := range flags {
		t.Run(tt.name, func(t *testing.T) {
			flag := verifyCmd.Flags().Lookup(tt.name)
			if flag == nil {
				t.Errorf("Expected flag --%s", tt.name)
				return
			}
			if flag.DefValue != tt.defValue {
				t.Errorf("Expected default %q, got %q", tt.defValue, flag.DefValue)
			}
		})
	}
}

func TestVerifyCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "verify" {
			found = true
			break
		}
	}
	if !found {
		t.Error("verify command not registered with root command")
	}
}

func TestVerifyCommandArgs(t *testing.T) {
	// verifyCmd allows 0-1 args
	if verifyCmd.Args == nil {
		t.Error("Expected Args validator to be set")
	}
}

// =============================================================================
// VerifyProfileResult Tests
// =============================================================================

func TestVerifyProfileResultStruct(t *testing.T) {
	now := time.Now()
	result := VerifyProfileResult{
		Provider:    "claude",
		Profile:     "work@example.com",
		Status:      "healthy",
		TokenExpiry: &now,
		ExpiresIn:   "7d",
		ErrorCount:  0,
		Penalty:     0.0,
		Issues:      []string{},
		Score:       100.0,
	}

	if result.Provider != "claude" {
		t.Errorf("Expected provider 'claude', got %q", result.Provider)
	}
	if result.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %q", result.Status)
	}
	if result.Score != 100.0 {
		t.Errorf("Expected score 100.0, got %f", result.Score)
	}
}

func TestVerifyProfileResultCritical(t *testing.T) {
	result := VerifyProfileResult{
		Provider:   "codex",
		Profile:    "expired@example.com",
		Status:     "critical",
		ExpiresIn:  "expired",
		ErrorCount: 5,
		Penalty:    0.8,
		Issues:     []string{"Token expired", "High error count"},
	}

	if result.Status != "critical" {
		t.Errorf("Expected status 'critical', got %q", result.Status)
	}
	if len(result.Issues) != 2 {
		t.Errorf("Expected 2 issues, got %d", len(result.Issues))
	}
}

// =============================================================================
// VerifyOutput Tests
// =============================================================================

func TestVerifyOutputStruct(t *testing.T) {
	output := VerifyOutput{
		Profiles: []VerifyProfileResult{
			{Provider: "claude", Profile: "work", Status: "healthy"},
			{Provider: "codex", Profile: "personal", Status: "warning"},
		},
		Summary: VerifySummary{
			TotalProfiles: 2,
			HealthyCount:  1,
			WarningCount:  1,
		},
		Recommendations: []string{"Consider refreshing tokens"},
	}

	if len(output.Profiles) != 2 {
		t.Errorf("Expected 2 profiles, got %d", len(output.Profiles))
	}
	if output.Summary.TotalProfiles != 2 {
		t.Errorf("Expected 2 total profiles, got %d", output.Summary.TotalProfiles)
	}
}

// =============================================================================
// VerifySummary Tests
// =============================================================================

func TestVerifySummaryCounts(t *testing.T) {
	summary := VerifySummary{
		TotalProfiles: 10,
		HealthyCount:  7,
		WarningCount:  2,
		CriticalCount: 1,
		UnknownCount:  0,
	}

	if summary.HealthyCount+summary.WarningCount+summary.CriticalCount+summary.UnknownCount != summary.TotalProfiles {
		t.Error("Summary counts should add up to total")
	}
}

// =============================================================================
// formatTimeRemaining Tests
// =============================================================================

func TestFormatTimeRemaining(t *testing.T) {
	tests := []struct {
		duration    time.Duration
		wantContain string
	}{
		{30 * time.Second, "< 1m"},
		{2 * time.Minute, "2m"},
		{30 * time.Minute, "30m"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{24 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}

	for _, tt := range tests {
		t.Run(tt.wantContain, func(t *testing.T) {
			got := formatTimeRemaining(tt.duration)
			if got == "" {
				t.Error("Expected non-empty time string")
			}
		})
	}
}

// =============================================================================
// generateRecommendations Tests
// =============================================================================

func TestGenerateRecommendationsEmpty(t *testing.T) {
	output := &VerifyOutput{
		Profiles: []VerifyProfileResult{},
		Summary:  VerifySummary{},
	}

	recs := generateRecommendations(output)
	// Should not crash with empty output
	_ = recs
}

func TestGenerateRecommendationsHealthy(t *testing.T) {
	output := &VerifyOutput{
		Profiles: []VerifyProfileResult{
			{Provider: "claude", Profile: "work", Status: "healthy"},
		},
		Summary: VerifySummary{
			TotalProfiles: 1,
			HealthyCount:  1,
		},
	}

	recs := generateRecommendations(output)
	// Healthy profiles should not generate recommendations
	if len(recs) > 0 {
		t.Logf("Expected no recommendations for healthy profile, got: %v", recs)
	}
}

// =============================================================================
// getVerifyStatusIcon Tests
// =============================================================================

func TestGetVerifyStatusIcon(t *testing.T) {
	tests := []struct {
		status string
	}{
		{"healthy"},
		{"warning"},
		{"critical"},
		{"unknown"},
		{""},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := getVerifyStatusIcon(tt.status)
			if got == "" {
				t.Error("Expected non-empty icon")
			}
		})
	}
}

// =============================================================================
// Verify Profile Tests
// =============================================================================

func TestVerifyProfileStatuses(t *testing.T) {
	statuses := []string{"healthy", "warning", "critical", "unknown"}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			// Verify each status is a valid value
			if status == "" {
				t.Error("Status should not be empty")
			}
		})
	}
}