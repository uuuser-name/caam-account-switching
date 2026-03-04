package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/bundle"
	"github.com/spf13/cobra"
)

// =============================================================================
// bundle_import.go Command Definition Tests
// =============================================================================

func TestBundleImportCommand(t *testing.T) {
	if bundleImportCmd.Use != "import <bundle.zip>" {
		t.Errorf("Expected Use 'import <bundle.zip>', got %q", bundleImportCmd.Use)
	}

	if bundleImportCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if bundleImportCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestBundleImportCommandArgs(t *testing.T) {
	// bundleImportCmd requires exactly 1 arg
	err := bundleImportCmd.Args(bundleImportCmd, []string{})
	if err == nil {
		t.Error("Expected error for 0 args")
	}

	err = bundleImportCmd.Args(bundleImportCmd, []string{"bundle.zip"})
	if err != nil {
		t.Errorf("Expected no error for 1 arg, got %v", err)
	}

	err = bundleImportCmd.Args(bundleImportCmd, []string{"bundle.zip", "extra"})
	if err == nil {
		t.Error("Expected error for 2 args")
	}
}

func TestBundleImportCommandFlags(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		defValue string
	}{
		{"mode", "mode", "smart"},
		{"password", "password", ""},
		{"dry-run", "dry-run", "false"},
		{"force", "force", "false"},
		{"skip-config", "skip-config", "false"},
		{"skip-projects", "skip-projects", "false"},
		{"skip-health", "skip-health", "false"},
		{"skip-database", "skip-database", "false"},
		{"skip-sync", "skip-sync", "false"},
		{"json", "json", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := bundleImportCmd.Flags().Lookup(tt.flagName)
			if flag == nil {
				t.Errorf("Expected flag --%s", tt.flagName)
				return
			}

			if flag.DefValue != tt.defValue {
				t.Errorf("Flag --%s: expected default %q, got %q", tt.flagName, tt.defValue, flag.DefValue)
			}
		})
	}
}

func TestBundleImportModeFlag(t *testing.T) {
	validModes := []string{"smart", "merge", "replace"}

	for _, mode := range validModes {
		t.Run(mode, func(t *testing.T) {
			err := bundleImportCmd.Flags().Set("mode", mode)
			if err != nil {
				t.Fatalf("Failed to set mode flag: %v", err)
			}

			val, err := bundleImportCmd.Flags().GetString("mode")
			if err != nil {
				t.Fatalf("Failed to get mode: %v", err)
			}

			if val != mode {
				t.Errorf("Expected mode %q, got %q", mode, val)
			}
		})
	}
}

func TestBundleImportSkipFlags(t *testing.T) {
	skipFlags := []string{
		"skip-config",
		"skip-projects",
		"skip-health",
		"skip-database",
		"skip-sync",
	}

	for _, flagName := range skipFlags {
		t.Run(flagName, func(t *testing.T) {
			err := bundleImportCmd.Flags().Set(flagName, "true")
			if err != nil {
				t.Fatalf("Failed to set %s: %v", flagName, err)
			}

			val, err := bundleImportCmd.Flags().GetBool(flagName)
			if err != nil {
				t.Fatalf("Failed to get %s: %v", flagName, err)
			}

			if !val {
				t.Errorf("Expected %s to be true", flagName)
			}
		})
	}
}

func TestBundleImportProviderFilter(t *testing.T) {
	err := bundleImportCmd.Flags().Set("providers", "claude,gemini")
	if err != nil {
		t.Fatalf("Failed to set providers flag: %v", err)
	}

	val, err := bundleImportCmd.Flags().GetStringSlice("providers")
	if err != nil {
		t.Fatalf("Failed to get providers: %v", err)
	}

	if len(val) != 2 || val[0] != "claude" || val[1] != "gemini" {
		t.Errorf("Expected [claude gemini], got %v", val)
	}
}

func TestBundleImportProfileFilter(t *testing.T) {
	err := bundleImportCmd.Flags().Set("profiles", "work,team")
	if err != nil {
		t.Fatalf("Failed to set profiles flag: %v", err)
	}

	val, err := bundleImportCmd.Flags().GetStringSlice("profiles")
	if err != nil {
		t.Fatalf("Failed to get profiles: %v", err)
	}

	if len(val) != 2 || val[0] != "work" || val[1] != "team" {
		t.Errorf("Expected [work team], got %v", val)
	}
}

// =============================================================================
// printImportPreview Tests
// =============================================================================

func TestPrintImportPreview(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion:   1,
		CAAMVersion:     "test-version",
		ExportTimestamp: time.Now(),
	}
	manifest.Source.Hostname = "test-host"
	manifest.Source.Platform = "linux"
	manifest.Source.Arch = "amd64"

	verificationResult := &bundle.VerificationResult{
		Valid:    true,
		Verified: []string{"file1.txt", "file2.txt"},
	}

	profileActions := []bundle.ProfileAction{
		{Provider: "claude", Profile: "alice@example.com", Action: "add", Reason: "new profile"},
		{Provider: "claude", Profile: "bob@example.com", Action: "update", Reason: "fresher token"},
		{Provider: "codex", Profile: "work", Action: "skip", Reason: "local is fresher"},
	}

	optionalActions := []bundle.OptionalAction{
		{Name: "config.yaml", Action: "import", Reason: "imported"},
		{Name: "projects.json", Action: "skip", Reason: "excluded by flag"},
		{Name: "health.db", Action: "error", Reason: "corrupt", Details: "checksum mismatch"},
	}

	result := &bundle.ImportResult{
		Manifest:           manifest,
		VerificationResult: verificationResult,
		ProfileActions:     profileActions,
		OptionalActions:    optionalActions,
		NewProfiles:        1,
		UpdatedProfiles:    1,
		SkippedProfiles:    1,
		Encrypted:          false,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printImportPreview(cmd, result)

	output := buf.String()

	// Verify key sections appear
	expectedStrings := []string{
		"Import Preview",
		"Bundle Info:",
		"Created:",
		"Source:",
		"CAAM Version:",
		"Checksum Verification:",
		"Profiles:",
		"claude:",
		"codex:",
		"+",           // add symbol
		"↑",           // update symbol
		"-",           // skip symbol
		"Optional Files:",
		"Summary (would):",
		"Add: 1 new profiles",
		"Update: 1 profiles",
		"Skip: 1 profiles",
		"This is a preview",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q", expected)
		}
	}
}

func TestPrintImportPreviewWithEncryption(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion: 1,
		CAAMVersion:   "test-version",
	}
	manifest.Source.Hostname = "test-host"
	manifest.Source.Platform = "darwin"
	manifest.Source.Arch = "arm64"

	result := &bundle.ImportResult{
		Manifest:    manifest,
		Encrypted:   true,
		NewProfiles: 2,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printImportPreview(cmd, result)

	output := buf.String()

	if !strings.Contains(output, "Encrypted: yes") {
		t.Error("Expected 'Encrypted: yes' for encrypted bundle")
	}
}

// =============================================================================
// printImportResult Tests
// =============================================================================

func TestPrintImportResult(t *testing.T) {
	profileActions := []bundle.ProfileAction{
		{Provider: "claude", Profile: "alice@example.com", Action: "add", Reason: "new profile"},
		{Provider: "claude", Profile: "bob@example.com", Action: "update", Reason: "fresher token"},
		{Provider: "codex", Profile: "work", Action: "skip", Reason: "local is fresher"},
	}

	optionalActions := []bundle.OptionalAction{
		{Name: "config.yaml", Action: "import", Reason: "imported"},
		{Name: "projects.json", Action: "skip", Reason: "excluded"},
		{Name: "health.db", Action: "error", Reason: "failed to read"},
	}

	result := &bundle.ImportResult{
		NewProfiles:     1,
		UpdatedProfiles: 1,
		SkippedProfiles: 1,
		ProfileActions:  profileActions,
		OptionalActions: optionalActions,
		Errors:          []string{"Warning: partial import"},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printImportResult(cmd, result)

	output := buf.String()

	// Verify key sections
	expectedStrings := []string{
		"Import Complete",
		"Profiles:",
		"Added: 1",
		"Updated: 1",
		"Skipped: 1",
		"claude/alice@example.com",
		"claude/bob@example.com",
		"Optional Files:",
		"config.yaml",
		"Errors:",
		"partial import",
		"Import complete",
		"caam status",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q", expected)
		}
	}
}

func TestPrintImportResultWithVerificationFailure(t *testing.T) {
	verificationResult := &bundle.VerificationResult{
		Valid:    false,
		Mismatch: []bundle.ChecksumMismatch{{Path: "file.txt", Expected: "abc", Actual: "def"}},
	}

	result := &bundle.ImportResult{
		NewProfiles:        0,
		UpdatedProfiles:    0,
		SkippedProfiles:    0,
		VerificationResult: verificationResult,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printImportResult(cmd, result)

	output := buf.String()

	// Should show verification warning
	if !strings.Contains(output, "Warning:") {
		t.Error("Expected verification warning in output")
	}
}

func TestPrintImportResultMinimal(t *testing.T) {
	// Test with minimal result (no actions, no errors)
	result := &bundle.ImportResult{
		NewProfiles:     0,
		UpdatedProfiles: 0,
		SkippedProfiles: 5,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printImportResult(cmd, result)

	output := buf.String()

	// Should still show complete message
	if !strings.Contains(output, "Import Complete") {
		t.Error("Expected 'Import Complete' header")
	}

	if !strings.Contains(output, "Skipped: 5") {
		t.Error("Expected 'Skipped: 5' in output")
	}
}

// =============================================================================
// Import Mode Tests
// =============================================================================

func TestImportModeValues(t *testing.T) {
	// Verify the import mode constants
	modes := map[bundle.ImportMode]string{
		bundle.ImportModeSmart:   "smart",
		bundle.ImportModeMerge:   "merge",
		bundle.ImportModeReplace: "replace",
	}

	for mode, expected := range modes {
		if string(mode) != expected {
			t.Errorf("ImportMode %q = %q, want %q", mode, string(mode), expected)
		}
	}
}

// =============================================================================
// Profile Action Tests
// =============================================================================

func TestProfileActionSymbols(t *testing.T) {
	actions := []struct {
		action     string
		wantSymbol string
	}{
		{"add", "+"},
		{"update", "↑"},
		{"skip", "-"},
	}

	for _, tt := range actions {
		t.Run(tt.action, func(t *testing.T) {
			profileActions := []bundle.ProfileAction{
				{Provider: "test", Profile: "profile", Action: tt.action, Reason: "test"},
			}

			result := &bundle.ImportResult{
				ProfileActions: profileActions,
			}

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			printImportPreview(cmd, result)

			if !strings.Contains(buf.String(), tt.wantSymbol) {
				t.Errorf("Expected symbol %q for action %q", tt.wantSymbol, tt.action)
			}
		})
	}
}

// =============================================================================
// Optional File Action Tests
// =============================================================================

func TestOptionalFileActionSymbols(t *testing.T) {
	actions := []struct {
		action     string
		wantSymbol string
	}{
		{"import", "✓"},
		{"merge", "✓"},
		{"skip", "✗"},
		{"error", "!"},
	}

	for _, tt := range actions {
		t.Run(tt.action, func(t *testing.T) {
			optionalActions := []bundle.OptionalAction{
				{Name: "test.yaml", Action: tt.action, Reason: "test"},
			}

			result := &bundle.ImportResult{
				OptionalActions: optionalActions,
				Manifest:        &bundle.ManifestV1{},
			}

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			printImportPreview(cmd, result)

			if !strings.Contains(buf.String(), tt.wantSymbol) {
				t.Errorf("Expected symbol %q for action %q", tt.wantSymbol, tt.action)
			}
		})
	}
}

func TestOptionalFileActionWithDetails(t *testing.T) {
	optionalActions := []bundle.OptionalAction{
		{Name: "config.yaml", Action: "error", Reason: "failed", Details: "permission denied"},
	}

	result := &bundle.ImportResult{
		OptionalActions: optionalActions,
		Manifest:        &bundle.ManifestV1{},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printImportPreview(cmd, result)

	output := buf.String()

	if !strings.Contains(output, "permission denied") {
		t.Error("Expected details to be shown in output")
	}
}

// =============================================================================
// promptPasswordImport Tests
// =============================================================================

func TestPromptPasswordImportExists(t *testing.T) {
	// Verify the function exists and has correct signature
	_ = promptPasswordImport
}
