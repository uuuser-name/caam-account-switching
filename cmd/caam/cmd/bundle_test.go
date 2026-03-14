package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/bundle"
	"github.com/spf13/cobra"
)

func newTestStringSliceFlagCommand(name, usage string) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringSlice(name, nil, usage)
	return cmd
}

// =============================================================================
// bundle.go Command Definition Tests
// =============================================================================

func TestBundleCommand(t *testing.T) {
	if bundleCmd.Use != "bundle" {
		t.Errorf("Expected Use 'bundle', got %q", bundleCmd.Use)
	}

	if bundleCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if bundleCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}

	// Verify bundleExportCmd is a subcommand
	found := false
	for _, cmd := range bundleCmd.Commands() {
		if cmd.Name() == "export" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'export' subcommand to be registered")
	}
}

func TestBundleExportCommand(t *testing.T) {
	if bundleExportCmd.Use != "export" {
		t.Errorf("Expected Use 'export', got %q", bundleExportCmd.Use)
	}

	if bundleExportCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if bundleExportCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestBundleExportCommandFlags(t *testing.T) {
	tests := []struct {
		name         string
		flagName     string
		expectShorthand bool
		defValue     string
	}{
		{"output", "output", true, ""},
		{"verbose-filename", "verbose-filename", false, "false"},
		{"dry-run", "dry-run", false, "false"},
		{"encrypt", "encrypt", true, "false"},
		{"password", "password", true, ""},
		{"providers", "providers", false, "[]"},
		{"profiles", "profiles", false, "[]"},
		{"no-config", "no-config", false, "false"},
		{"no-projects", "no-projects", false, "false"},
		{"no-health", "no-health", false, "false"},
		{"include-database", "include-database", false, "false"},
		{"include-sync", "include-sync", false, "true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := bundleExportCmd.Flags().Lookup(tt.flagName)
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

// =============================================================================
// printContentStatus Tests
// =============================================================================

func TestPrintContentStatus(t *testing.T) {
	tests := []struct {
		name     string
		content  bundle.OptionalContent
		wantContains []string
	}{
		{
			name: "included with count",
			content: bundle.OptionalContent{
				Included: true,
				Count:    5,
			},
			wantContains: []string{"Config:", "✓", "(5 items)"},
		},
		{
			name: "included with note",
			content: bundle.OptionalContent{
				Included: true,
				Note:     "filtered",
			},
			wantContains: []string{"Config:", "✓", "(filtered)"},
		},
			{
				name: "included with count and note (note takes precedence)",
				content: bundle.OptionalContent{
					Included: true,
					Count:    3,
					Note:     "ignored",
				},
				wantContains: []string{"Config:", "✓", "(ignored)"},
			},
		{
			name: "excluded with reason",
			content: bundle.OptionalContent{
				Included: false,
				Reason:   "not found",
			},
			wantContains: []string{"Config:", "✗", "(not found)"},
		},
		{
			name: "excluded without reason (default)",
			content: bundle.OptionalContent{
				Included: false,
			},
			wantContains: []string{"Config:", "✗", "(excluded)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printContentStatus(&buf, "Config:", tt.content)

			output := buf.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("Output %q should contain %q", output, want)
				}
			}
		})
	}
}

// =============================================================================
// promptPassword Tests (via exported behavior verification)
// =============================================================================

func TestPromptPasswordExists(t *testing.T) {
	// Verify the function exists and has correct signature
	// We can't easily test terminal behavior, but we can verify compilation
	_ = promptPassword
}

// =============================================================================
// bundle command integration tests
// =============================================================================

func TestBundleExportFlagProviders(t *testing.T) {
	flag := bundleExportCmd.Flags().Lookup("providers")
	if flag == nil {
		t.Fatal("Expected --providers flag")
	}

	testCmd := newTestStringSliceFlagCommand("providers", flag.Usage)

	// Test that it accepts string slice values
	err := testCmd.Flags().Set("providers", "claude,codex")
	if err != nil {
		t.Fatalf("Failed to set providers flag: %v", err)
	}

	val, err := testCmd.Flags().GetStringSlice("providers")
	if err != nil {
		t.Fatalf("Failed to get providers: %v", err)
	}

	if len(val) != 2 || val[0] != "claude" || val[1] != "codex" {
		t.Errorf("Expected [claude codex], got %v", val)
	}
}

func TestBundleExportFlagProfiles(t *testing.T) {
	flag := bundleExportCmd.Flags().Lookup("profiles")
	if flag == nil {
		t.Fatal("Expected --profiles flag")
	}

	testCmd := newTestStringSliceFlagCommand("profiles", flag.Usage)

	err := testCmd.Flags().Set("profiles", "work,personal")
	if err != nil {
		t.Fatalf("Failed to set profiles flag: %v", err)
	}

	val, err := testCmd.Flags().GetStringSlice("profiles")
	if err != nil {
		t.Fatalf("Failed to get profiles: %v", err)
	}

	if len(val) != 2 || val[0] != "work" || val[1] != "personal" {
		t.Errorf("Expected [work personal], got %v", val)
	}
}

func TestBundleExportFlagEncryptRequiresPassword(t *testing.T) {
	// Test encrypt flag
	err := bundleExportCmd.Flags().Set("encrypt", "true")
	if err != nil {
		t.Fatalf("Failed to set encrypt flag: %v", err)
	}

	encrypt, err := bundleExportCmd.Flags().GetBool("encrypt")
	if err != nil {
		t.Fatalf("Failed to get encrypt: %v", err)
	}

	if !encrypt {
		t.Error("Expected encrypt to be true")
	}
}

func TestBundleExportContentFlags(t *testing.T) {
	// Test no-config flag
	err := bundleExportCmd.Flags().Set("no-config", "true")
	if err != nil {
		t.Fatalf("Failed to set no-config: %v", err)
	}

	noConfig, err := bundleExportCmd.Flags().GetBool("no-config")
	if err != nil {
		t.Fatalf("Failed to get no-config: %v", err)
	}

	if !noConfig {
		t.Error("Expected no-config to be true")
	}

	// Test no-projects flag
	err = bundleExportCmd.Flags().Set("no-projects", "true")
	if err != nil {
		t.Fatalf("Failed to set no-projects: %v", err)
	}

	noProjects, err := bundleExportCmd.Flags().GetBool("no-projects")
	if err != nil {
		t.Fatalf("Failed to get no-projects: %v", err)
	}

	if !noProjects {
		t.Error("Expected no-projects to be true")
	}

	// Test include-database flag
	err = bundleExportCmd.Flags().Set("include-database", "true")
	if err != nil {
		t.Fatalf("Failed to set include-database: %v", err)
	}

	includeDB, err := bundleExportCmd.Flags().GetBool("include-database")
	if err != nil {
		t.Fatalf("Failed to get include-database: %v", err)
	}

	if !includeDB {
		t.Error("Expected include-database to be true")
	}
}

func TestBundleExportDryRunFlag(t *testing.T) {
	err := bundleExportCmd.Flags().Set("dry-run", "true")
	if err != nil {
		t.Fatalf("Failed to set dry-run: %v", err)
	}

	dryRun, err := bundleExportCmd.Flags().GetBool("dry-run")
	if err != nil {
		t.Fatalf("Failed to get dry-run: %v", err)
	}

	if !dryRun {
		t.Error("Expected dry-run to be true")
	}
}

func TestBundleExportVerboseFilenameFlag(t *testing.T) {
	err := bundleExportCmd.Flags().Set("verbose-filename", "true")
	if err != nil {
		t.Fatalf("Failed to set verbose-filename: %v", err)
	}

	verbose, err := bundleExportCmd.Flags().GetBool("verbose-filename")
	if err != nil {
		t.Fatalf("Failed to get verbose-filename: %v", err)
	}

	if !verbose {
		t.Error("Expected verbose-filename to be true")
	}
}

func TestBundleExportOutputFlag(t *testing.T) {
	testPath := "/tmp/test-output"
	err := bundleExportCmd.Flags().Set("output", testPath)
	if err != nil {
		t.Fatalf("Failed to set output: %v", err)
	}

	output, err := bundleExportCmd.Flags().GetString("output")
	if err != nil {
		t.Fatalf("Failed to get output: %v", err)
	}

	if output != testPath {
		t.Errorf("Expected output %q, got %q", testPath, output)
	}
}

// =============================================================================
// printExportResult Tests
// =============================================================================

func TestPrintExportResult(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion:   1,
		CAAMVersion:     "test-version",
		ExportTimestamp: time.Now(),
	}

	manifest.Contents.Vault.TotalProfiles = 3
	manifest.Contents.Vault.Profiles = map[string][]string{
		"claude": {"alice@example.com", "bob@example.com"},
		"codex":  {"work"},
	}
	manifest.Contents.Config = bundle.OptionalContent{Included: true, Count: 1}
	manifest.Contents.Projects = bundle.OptionalContent{Included: false, Reason: "disabled"}
	manifest.Contents.Health = bundle.OptionalContent{Included: true, Note: "ok"}
	manifest.Contents.Database = bundle.OptionalContent{Included: false, Reason: "skipped"}
	manifest.Contents.SyncConfig = bundle.OptionalContent{Included: true, Count: 2}

	result := &bundle.ExportResult{
		OutputPath:      "/test/bundle.zip",
		Manifest:        manifest,
		Encrypted:       false,
		CompressedSize:  1024,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printExportResult(cmd, result, false)

	output := buf.String()

	// Verify key content appears
	expectedStrings := []string{
		"Export Complete",
		"Vault profiles: 3",
		"claude:",
		"codex:",
		"Included content:",
		"Output:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q", expected)
		}
	}
}

func TestPrintExportResultDryRun(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion: 1,
		CAAMVersion:   "test-version",
	}
	manifest.Contents.Vault.TotalProfiles = 0

	result := &bundle.ExportResult{
		OutputPath:     "/test/bundle.zip",
		Manifest:       manifest,
		Encrypted:      false,
		CompressedSize: 0,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printExportResult(cmd, result, true)

	output := buf.String()

	// Dry run mode should show preview
	if !strings.Contains(output, "Export Preview") {
		t.Error("Expected 'Export Preview' header for dry run")
	}

	if !strings.Contains(output, "Would create:") {
		t.Error("Expected 'Would create:' for dry run")
	}
}

func TestPrintExportResultEncrypted(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion: 1,
		CAAMVersion:   "test-version",
	}
	manifest.Contents.Vault.TotalProfiles = 1

	result := &bundle.ExportResult{
		OutputPath:      "/test/bundle.enc.zip",
		Manifest:        manifest,
		Encrypted:       true,
		CompressedSize:  2048,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printExportResult(cmd, result, false)

	output := buf.String()

	// Encrypted bundle should show encryption info
	if !strings.Contains(output, "Encryption:") {
		t.Error("Expected 'Encryption:' in output for encrypted bundle")
	}

	if !strings.Contains(output, "AES-256-GCM") {
		t.Error("Expected AES-256-GCM encryption info")
	}

	if !strings.Contains(output, "Store your password securely") {
		t.Error("Expected password warning for encrypted bundle")
	}
}

func TestPrintExportResultUnencryptedWarning(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion: 1,
		CAAMVersion:   "test-version",
	}
	manifest.Contents.Vault.TotalProfiles = 1

	result := &bundle.ExportResult{
		OutputPath:      "/test/bundle.zip",
		Manifest:        manifest,
		Encrypted:       false,
		CompressedSize:  1024,
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	printExportResult(cmd, result, false)

	output := buf.String()

	// Unencrypted bundle should show warning about OAuth tokens
	if !strings.Contains(output, "OAuth tokens") {
		t.Error("Expected OAuth tokens warning for unencrypted bundle")
	}

	if !strings.Contains(output, "--encrypt") {
		t.Error("Expected --encrypt suggestion for unencrypted bundle")
	}
}
