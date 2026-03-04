package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/bundle"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Bundle Export/Import Round-Trip Tests with Real Filesystem
// =============================================================================

func TestBundleExportImportRoundTripWithRealFS(t *testing.T) {
	// Create real temp directory structure
	tmpDir := t.TempDir()
	vaultPath := filepath.Join(tmpDir, "vault")
	dataPath := tmpDir
	configPath := filepath.Join(tmpDir, "config.yaml")
	projectsPath := filepath.Join(tmpDir, "projects.json")
	syncPath := filepath.Join(tmpDir, "sync")

	// Create directory structure
	require.NoError(t, os.MkdirAll(vaultPath, 0755))
	require.NoError(t, os.MkdirAll(syncPath, 0755))

	// Create test vault profiles
	profileDir := filepath.Join(vaultPath, "claude", "work")
	require.NoError(t, os.MkdirAll(profileDir, 0755))

	// Write test auth file with realistic content
	authContent := `# Claude work profile
CLAUDE_ACCESS_TOKEN=sk_test_abc123
CLAUDE_REFRESH_TOKEN=refresh_test_xyz
CLAUDE_TOKEN_EXPIRY=2099-12-31T23:59:59Z
`
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth"), []byte(authContent), 0600))

	// Create a second profile
	profileDir2 := filepath.Join(vaultPath, "codex", "personal")
	require.NoError(t, os.MkdirAll(profileDir2, 0755))
	authContent2 := `# Codex personal profile
CODEX_API_KEY=cdx_test_key
CODEX_TOKEN_EXPIRY=2099-06-15T12:00:00Z
`
	require.NoError(t, os.WriteFile(filepath.Join(profileDir2, "auth"), []byte(authContent2), 0600))

	// Create config file
	configContent := `default_provider: claude
default_profile: work
auto_backup: true
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Create projects file
	projectsContent := `{"projects": [{"name": "test-project", "path": "/tmp/test"}]}`
	require.NoError(t, os.WriteFile(projectsPath, []byte(projectsContent), 0644))

	// Create sync machines file
	syncContent := `name,address,port,ssh_user,ssh_key_path
machine1,192.168.1.10,22,testuser,
`
	require.NoError(t, os.WriteFile(filepath.Join(syncPath, "machines.csv"), []byte(syncContent), 0644))

	// Create exporter
	exporter := &bundle.VaultExporter{
		VaultPath:    vaultPath,
		DataPath:     dataPath,
		ConfigPath:   configPath,
		ProjectsPath: projectsPath,
		HealthPath:   "", // no health data
		DatabasePath: "", // no database
		SyncPath:     syncPath,
	}

	// Export to temp output directory
	outputDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	opts := bundle.DefaultExportOptions()
	opts.OutputDir = outputDir
	opts.IncludeSyncConfig = true

	result, err := exporter.Export(opts)
	require.NoError(t, err, "Export should succeed")
	require.NotNil(t, result, "Export result should not be nil")
	require.FileExists(t, result.OutputPath, "Bundle file should exist")
	require.NotNil(t, result.Manifest, "Manifest should be present")

	// Verify manifest content
	require.Equal(t, 2, result.Manifest.Contents.Vault.TotalProfiles, "Should have 2 profiles")
	require.True(t, result.Manifest.Contents.Config.Included, "Config should be included")
	require.True(t, result.Manifest.Contents.Projects.Included, "Projects should be included")
	require.True(t, result.Manifest.Contents.SyncConfig.Included, "Sync config should be included")

	// Verify bundle is a valid zip file
	reader, err := zip.OpenReader(result.OutputPath)
	require.NoError(t, err, "Bundle should be a valid zip file")
	reader.Close()

	// Now test import to a new vault location
	importVaultPath := filepath.Join(tmpDir, "imported_vault")
	importConfigPath := filepath.Join(tmpDir, "imported_config.yaml")
	importProjectsPath := filepath.Join(tmpDir, "imported_projects.json")
	importSyncPath := filepath.Join(tmpDir, "imported_sync")

	importer := &bundle.VaultImporter{
		BundlePath: result.OutputPath,
	}

	importOpts := bundle.DefaultImportOptions()
	importOpts.VaultPath = importVaultPath
	importOpts.ConfigPath = importConfigPath
	importOpts.ProjectsPath = importProjectsPath
	importOpts.SyncPath = importSyncPath
	importOpts.DryRun = true // First do a dry-run

	importResult, err := importer.Import(importOpts)
	require.NoError(t, err, "Import dry-run should succeed")
	require.NotNil(t, importResult, "Import result should not be nil")
	require.Equal(t, 2, importResult.NewProfiles, "Should plan to add 2 new profiles")

	// Now do real import
	importOpts.DryRun = false
	importResult, err = importer.Import(importOpts)
	require.NoError(t, err, "Import should succeed")
	require.Equal(t, 2, importResult.NewProfiles, "Should add 2 new profiles")

	// Verify imported files exist
	require.FileExists(t, filepath.Join(importVaultPath, "claude", "work", "auth"), "Claude work profile should be imported")
	require.FileExists(t, filepath.Join(importVaultPath, "codex", "personal", "auth"), "Codex personal profile should be imported")
	require.FileExists(t, importConfigPath, "Config should be imported")
	require.FileExists(t, importProjectsPath, "Projects should be imported")
	require.FileExists(t, filepath.Join(importSyncPath, "machines.csv"), "Sync config should be imported")

	// Verify imported auth content
	importedAuth, err := os.ReadFile(filepath.Join(importVaultPath, "claude", "work", "auth"))
	require.NoError(t, err)
	require.Contains(t, string(importedAuth), "CLAUDE_ACCESS_TOKEN=sk_test_abc123", "Auth content should match")
}

// =============================================================================
// Bundle Integrity Check Tests
// =============================================================================

func TestBundleIntegrityVerification(t *testing.T) {
	tmpDir := t.TempDir()
	vaultPath := filepath.Join(tmpDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0755))

	// Create test profile
	profileDir := filepath.Join(vaultPath, "claude", "test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth"), []byte("TOKEN=test123"), 0600))

	exporter := &bundle.VaultExporter{
		VaultPath:    vaultPath,
		DataPath:     tmpDir,
		ConfigPath:   "",
		ProjectsPath: "",
		HealthPath:   "",
		DatabasePath: "",
		SyncPath:     "",
	}

	outputDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	opts := bundle.DefaultExportOptions()
	opts.OutputDir = outputDir
	opts.IncludeConfig = false
	opts.IncludeProjects = false
	opts.IncludeHealth = false
	opts.IncludeSyncConfig = false

	result, err := exporter.Export(opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify manifest has checksums
	require.NotNil(t, result.Manifest, "Manifest should not be nil")
	require.NotEmpty(t, result.Manifest.Checksums, "Checksums should be populated")

	// Test verification passes on valid bundle
	importer := &bundle.VaultImporter{BundlePath: result.OutputPath}
	importOpts := bundle.DefaultImportOptions()
	importOpts.VaultPath = filepath.Join(tmpDir, "imported")
	importOpts.DryRun = true

	importResult, err := importer.Import(importOpts)
	require.NoError(t, err)
	require.NotNil(t, importResult.VerificationResult, "Verification result should be present")
	require.True(t, importResult.VerificationResult.Valid, "Verification should pass for valid bundle")
}

func TestBundleCorruptedDetection(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake corrupted bundle
	bundlePath := filepath.Join(tmpDir, "corrupted.zip")

	// Write invalid zip content
	require.NoError(t, os.WriteFile(bundlePath, []byte("not a valid zip file content"), 0644))

	importer := &bundle.VaultImporter{BundlePath: bundlePath}
	importOpts := bundle.DefaultImportOptions()
	importOpts.VaultPath = filepath.Join(tmpDir, "imported")
	importOpts.DryRun = true

	_, err := importer.Import(importOpts)
	require.Error(t, err, "Import should fail for corrupted bundle")
}

func TestBundleMissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Test import of non-existent bundle
	bundlePath := filepath.Join(tmpDir, "nonexistent.zip")

	importer := &bundle.VaultImporter{BundlePath: bundlePath}
	importOpts := bundle.DefaultImportOptions()
	importOpts.VaultPath = filepath.Join(tmpDir, "imported")

	_, err := importer.Import(importOpts)
	require.Error(t, err, "Import should fail for missing bundle")
	require.Contains(t, err.Error(), "not found", "Error should indicate bundle not found")
}

// =============================================================================
// Bundle Encryption Tests
// =============================================================================

func TestBundleEncryptionRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	vaultPath := filepath.Join(tmpDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0755))

	// Create test profile with sensitive data
	profileDir := filepath.Join(vaultPath, "claude", "secure")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth"), []byte("TOKEN=secret_token_xyz"), 0600))

	exporter := &bundle.VaultExporter{
		VaultPath:    vaultPath,
		DataPath:     tmpDir,
		ConfigPath:   "",
		ProjectsPath: "",
		HealthPath:   "",
		DatabasePath: "",
		SyncPath:     "",
	}

	outputDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	opts := bundle.DefaultExportOptions()
	opts.OutputDir = outputDir
	opts.Encrypt = true
	opts.Password = "test-password-123"
	opts.IncludeConfig = false
	opts.IncludeProjects = false
	opts.IncludeHealth = false
	opts.IncludeSyncConfig = false

	result, err := exporter.Export(opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Encrypted, "Result should indicate encryption")

	// Verify bundle has .enc.zip extension
	require.True(t, strings.HasSuffix(result.OutputPath, ".enc.zip"), "Encrypted bundle should have .enc.zip extension")

	// Test decryption with correct password
	importer := &bundle.VaultImporter{BundlePath: result.OutputPath}
	importOpts := bundle.DefaultImportOptions()
	importOpts.Password = "test-password-123"
	importOpts.VaultPath = filepath.Join(tmpDir, "imported")
	importOpts.DryRun = true

	importResult, err := importer.Import(importOpts)
	require.NoError(t, err, "Import with correct password should succeed")
	require.True(t, importResult.Encrypted, "Import result should indicate encrypted bundle")

	// Test decryption with wrong password
	importOptsWrong := bundle.DefaultImportOptions()
	importOptsWrong.Password = "wrong-password"
	importOptsWrong.VaultPath = filepath.Join(tmpDir, "imported_wrong")
	importOptsWrong.DryRun = true

	_, err = importer.Import(importOptsWrong)
	require.Error(t, err, "Import with wrong password should fail")
}

func TestBundleEncryptionRequiresPassword(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal encrypted bundle
	vaultPath := filepath.Join(tmpDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0755))

	profileDir := filepath.Join(vaultPath, "claude", "test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth"), []byte("TOKEN=test"), 0600))

	exporter := &bundle.VaultExporter{
		VaultPath:    vaultPath,
		DataPath:     tmpDir,
		ConfigPath:   "",
		ProjectsPath: "",
		HealthPath:   "",
		DatabasePath: "",
		SyncPath:     "",
	}

	outputDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	opts := bundle.DefaultExportOptions()
	opts.OutputDir = outputDir
	opts.Encrypt = true
	opts.Password = "password123"
	opts.IncludeConfig = false
	opts.IncludeProjects = false
	opts.IncludeHealth = false
	opts.IncludeSyncConfig = false

	result, err := exporter.Export(opts)
	require.NoError(t, err)

	// Try to import without password
	importer := &bundle.VaultImporter{BundlePath: result.OutputPath}
	importOpts := bundle.DefaultImportOptions()
	importOpts.VaultPath = filepath.Join(tmpDir, "imported")
	importOpts.DryRun = true
	// Password is empty by default

	_, err = importer.Import(importOpts)
	require.Error(t, err, "Import without password should fail for encrypted bundle")
	require.Contains(t, err.Error(), "password", "Error should mention password requirement")
}

// =============================================================================
// Import Mode Tests
// =============================================================================

func TestBundleImportModesWithRealFS(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source vault
	sourceVault := filepath.Join(tmpDir, "source_vault")
	require.NoError(t, os.MkdirAll(filepath.Join(sourceVault, "claude", "profile1"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceVault, "claude", "profile1", "auth"),
		[]byte("TOKEN=token1\nEXPIRY=2099-01-01T00:00:00Z"), 0600))

	exporter := &bundle.VaultExporter{
		VaultPath:    sourceVault,
		DataPath:     tmpDir,
		ConfigPath:   "",
		ProjectsPath: "",
		HealthPath:   "",
		DatabasePath: "",
		SyncPath:     "",
	}

	outputDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	opts := bundle.DefaultExportOptions()
	opts.OutputDir = outputDir
	opts.IncludeConfig = false
	opts.IncludeProjects = false
	opts.IncludeHealth = false
	opts.IncludeSyncConfig = false

	result, err := exporter.Export(opts)
	require.NoError(t, err)

	// Test each import mode
	modes := []bundle.ImportMode{
		bundle.ImportModeSmart,
		bundle.ImportModeMerge,
		bundle.ImportModeReplace,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			importVault := filepath.Join(tmpDir, "import_"+string(mode))
			require.NoError(t, os.MkdirAll(importVault, 0755))

			importer := &bundle.VaultImporter{BundlePath: result.OutputPath}
			importOpts := bundle.DefaultImportOptions()
			importOpts.Mode = mode
			importOpts.VaultPath = importVault
			importOpts.DryRun = true

			importResult, err := importer.Import(importOpts)
			require.NoError(t, err)
			require.NotNil(t, importResult)
			require.Equal(t, mode, importOpts.Mode)
		})
	}
}

// =============================================================================
// Sync Status JSON Tests
// =============================================================================

func TestSyncStatusJSONWithEmptyPool(t *testing.T) {
	state := sync.NewSyncState(t.TempDir())
	state.Pool.AutoSync = false

	var buf bytes.Buffer
	err := runSyncStatusJSON(state, &buf)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	require.False(t, result["auto_sync"].(bool))
	require.Empty(t, result["machines"])
	require.Equal(t, float64(0), result["queue_pending"])
	require.Equal(t, float64(0), result["history_count"])
}

func TestSyncStatusJSONWithMultipleMachines(t *testing.T) {
	state := sync.NewSyncState(t.TempDir())
	state.Identity = &sync.LocalIdentity{Hostname: "test-machine"}
	state.Pool.AutoSync = true
	state.Pool.LastFullSync = time.Now().Add(-2 * time.Hour)

	// Add machines with different states
	m1 := sync.NewMachine("work-laptop", "192.168.1.10")
	m1.Status = sync.StatusOnline
	m1.LastSync = time.Now().Add(-30 * time.Minute)
	require.NoError(t, state.Pool.AddMachine(m1))

	m2 := sync.NewMachine("home-desktop", "192.168.1.20")
	m2.Status = sync.StatusOffline
	require.NoError(t, state.Pool.AddMachine(m2))

	m3 := sync.NewMachine("cloud-vm", "10.0.0.5")
	m3.Status = sync.StatusError
	m3.LastSync = time.Now().Add(-24 * time.Hour)
	require.NoError(t, state.Pool.AddMachine(m3))

	// Add queue and history entries
	state.Queue.Entries = append(state.Queue.Entries,
		sync.QueueEntry{Provider: "claude", Profile: "work", Machine: m1.ID},
		sync.QueueEntry{Provider: "codex", Profile: "personal", Machine: m2.ID},
	)
	state.History.Entries = append(state.History.Entries,
		sync.HistoryEntry{Trigger: "auto", Provider: "claude", Profile: "work", Machine: m1.Name, Success: true},
		sync.HistoryEntry{Trigger: "manual", Provider: "codex", Profile: "personal", Machine: m2.Name, Success: false, Error: "connection refused"},
	)

	var buf bytes.Buffer
	err := runSyncStatusJSON(state, &buf)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	require.Equal(t, "test-machine", result["local_machine"])
	require.True(t, result["auto_sync"].(bool))
	require.Equal(t, float64(2), result["queue_pending"])
	require.Equal(t, float64(2), result["history_count"])

	machines := result["machines"].([]interface{})
	require.Len(t, machines, 3)
}

// =============================================================================
// Remote Vault Path Tests
// =============================================================================

func TestRemoteVaultPathVariants(t *testing.T) {
	// Default path when machine is nil
	require.Equal(t, sync.DefaultSyncerConfig().RemoteVaultPath, remoteVaultPath(nil))

	// Default path when machine has empty RemotePath
	m := &sync.Machine{RemotePath: ""}
	require.Equal(t, sync.DefaultSyncerConfig().RemoteVaultPath, remoteVaultPath(m))

	// Custom path with vault subdirectory
	m = &sync.Machine{RemotePath: "/data/caam"}
	require.Equal(t, "/data/caam/vault", remoteVaultPath(m))

	// Custom path with trailing slash
	m = &sync.Machine{RemotePath: "/opt/caam/"}
	require.Equal(t, "/opt/caam/vault", remoteVaultPath(m))
}

// =============================================================================
// Time Formatting Tests
// =============================================================================

func TestFormatTimeAgoAllCases(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"seconds", 45 * time.Second, "just now"},
		{"one_minute", 1 * time.Minute, "1 min ago"},
		{"few_minutes", 5 * time.Minute, "5 mins ago"},
		{"one_hour", 1 * time.Hour, "1 hour ago"},
		{"multiple_hours", 3 * time.Hour, "3 hours ago"},
		{"one_day", 24 * time.Hour, "1 day ago"},
		{"multiple_days", 5 * 24 * time.Hour, "5 days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeAgo(time.Now().Add(-tt.duration))
			require.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Print Export/Import Result Tests
// =============================================================================

func TestPrintExportResultWithAllContent(t *testing.T) {
	manifest := &bundle.ManifestV1{
		SchemaVersion:   1,
		CAAMVersion:     "test-v1.0.0",
		ExportTimestamp: time.Now(),
	}
	manifest.Contents.Vault.TotalProfiles = 5
	manifest.Contents.Vault.Profiles = map[string][]string{
		"claude": {"work", "personal"},
		"codex":  {"default", "team", "test"},
	}
	manifest.Contents.Config = bundle.OptionalContent{Included: true, Count: 1}
	manifest.Contents.Projects = bundle.OptionalContent{Included: true, Count: 3}
	manifest.Contents.Health = bundle.OptionalContent{Included: false, Reason: "not found"}
	manifest.Contents.Database = bundle.OptionalContent{Included: false, Reason: "skipped"}
	manifest.Contents.SyncConfig = bundle.OptionalContent{Included: true, Note: "2 machines"}

	result := &bundle.ExportResult{
		OutputPath:      "/test/bundle.zip",
		Manifest:        manifest,
		Encrypted:       false,
		CompressedSize:  10240,
	}

	var buf bytes.Buffer
	cmd := newTestCommand(&buf)
	printExportResult(cmd, result, false)

	output := buf.String()
	require.Contains(t, output, "Export Complete")
	require.Contains(t, output, "Vault profiles: 5")
	require.Contains(t, output, "claude:")
	require.Contains(t, output, "codex:")
	require.Contains(t, output, "Config: ✓")
	require.Contains(t, output, "Projects: ✓")
	require.Contains(t, output, "Health: ✗")
	require.Contains(t, output, "OAuth tokens")
}

func TestPrintImportResultWithActions(t *testing.T) {
	profileActions := []bundle.ProfileAction{
		{Provider: "claude", Profile: "new@example.com", Action: "add", Reason: "new profile"},
		{Provider: "claude", Profile: "existing@example.com", Action: "update", Reason: "fresher token in bundle"},
		{Provider: "codex", Profile: "work", Action: "skip", Reason: "local token fresher"},
	}

	optionalActions := []bundle.OptionalAction{
		{Name: "config.yaml", Action: "import", Reason: "updated"},
		{Name: "projects.json", Action: "skip", Reason: "no changes"},
	}

	result := &bundle.ImportResult{
		NewProfiles:     1,
		UpdatedProfiles: 1,
		SkippedProfiles: 1,
		ProfileActions:  profileActions,
		OptionalActions: optionalActions,
		Errors:          []string{"Warning: minor issue with health data"},
	}

	var buf bytes.Buffer
	cmd := newTestCommand(&buf)
	printImportResult(cmd, result)

	output := buf.String()
	require.Contains(t, output, "Import Complete")
	require.Contains(t, output, "Added: 1")
	require.Contains(t, output, "Updated: 1")
	require.Contains(t, output, "Skipped: 1")
	require.Contains(t, output, "claude/new@example.com")
	require.Contains(t, output, "claude/existing@example.com")
	require.Contains(t, output, "config.yaml")
	require.Contains(t, output, "Errors:")
	require.Contains(t, output, "minor issue")
}

// =============================================================================
// Helper Functions
// =============================================================================

func newTestCommand(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	return cmd
}
