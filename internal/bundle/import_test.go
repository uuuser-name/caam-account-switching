package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultImportOptions(t *testing.T) {
	opts := DefaultImportOptions()
	if opts == nil {
		t.Fatal("DefaultImportOptions returned nil")
	}

	if opts.Mode != ImportModeSmart {
		t.Errorf("expected default mode 'smart', got %q", opts.Mode)
	}
}

func TestVaultImporter_Import_BundleNotFound(t *testing.T) {
	importer := &VaultImporter{
		BundlePath: "/nonexistent/bundle.zip",
	}

	_, err := importer.Import(nil)
	if err == nil {
		t.Error("expected error for nonexistent bundle")
	}
}

func TestVaultImporter_Import_EncryptedWithoutPassword(t *testing.T) {
	// Create a fake encrypted bundle (just need the filename pattern)
	tempDir := t.TempDir()
	bundlePath := filepath.Join(tempDir, "test.enc.zip")

	// Create empty file (won't be read, just needs to exist)
	if err := os.WriteFile(bundlePath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	importer := &VaultImporter{
		BundlePath: bundlePath,
	}

	opts := DefaultImportOptions()
	opts.Password = "" // No password

	_, err := importer.Import(opts)
	if err == nil {
		t.Error("expected error for encrypted bundle without password")
	}
}

func TestVaultImporter_Import_DryRun(t *testing.T) {
	// Create a test bundle with export first
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create vault structure
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create auth file with expiry
	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "test_access_token",
			"refresh_token": "test_refresh_token",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export
	exporter := &VaultExporter{
		VaultPath: vaultDir,
		DataPath:  tempDir,
	}

	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir

	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Now test import with dry-run
	if err := os.MkdirAll(importVaultDir, 0700); err != nil {
		t.Fatal(err)
	}

	importer := &VaultImporter{
		BundlePath: exportResult.OutputPath,
	}

	importOpts := DefaultImportOptions()
	importOpts.DryRun = true
	importOpts.VaultPath = importVaultDir

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import dry-run failed: %v", err)
	}

	// Verify dry-run results
	if result.Manifest == nil {
		t.Error("expected manifest in result")
	}

	if result.VerificationResult == nil {
		t.Error("expected verification result")
	}

	if !result.VerificationResult.Valid {
		t.Errorf("expected valid checksums: %s", result.VerificationResult.Summary())
	}

	// Verify profile would be added (not actually added in dry-run)
	if result.NewProfiles != 1 {
		t.Errorf("expected 1 new profile, got %d", result.NewProfiles)
	}

	// Verify vault wasn't actually modified
	importedProfile := filepath.Join(importVaultDir, "claude", "test@example.com")
	if _, err := os.Stat(importedProfile); !os.IsNotExist(err) {
		t.Error("dry-run should not create files")
	}
}

func TestVaultImporter_Import_ActualImport(t *testing.T) {
	// Create a test bundle with export first
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create vault structure
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create auth file with expiry
	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "test_access_token",
			"refresh_token": "test_refresh_token",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export
	exporter := &VaultExporter{
		VaultPath: vaultDir,
		DataPath:  tempDir,
	}

	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir

	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Now test actual import
	if err := os.MkdirAll(importVaultDir, 0700); err != nil {
		t.Fatal(err)
	}

	importer := &VaultImporter{
		BundlePath: exportResult.OutputPath,
	}

	importOpts := DefaultImportOptions()
	importOpts.VaultPath = importVaultDir

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify results
	if result.NewProfiles != 1 {
		t.Errorf("expected 1 new profile, got %d", result.NewProfiles)
	}

	// Verify file was actually created
	importedProfile := filepath.Join(importVaultDir, "claude", "test@example.com", ".claude.json")
	if _, err := os.Stat(importedProfile); os.IsNotExist(err) {
		t.Error("import should have created profile file")
	}

	// Verify content
	importedData, err := os.ReadFile(importedProfile)
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}

	var imported map[string]interface{}
	if err := json.Unmarshal(importedData, &imported); err != nil {
		t.Fatalf("parse imported file: %v", err)
	}

	oauthToken, ok := imported["oauthToken"].(map[string]interface{})
	if !ok {
		t.Fatal("expected oauthToken in imported data")
	}

	if oauthToken["access_token"] != "test_access_token" {
		t.Error("imported token doesn't match")
	}
}

func TestVaultImporter_Import_MergeMode(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create source vault with profile
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "bundle_token",
			"refresh_token": "bundle_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Create destination vault with existing profile
	existingProfileDir := filepath.Join(importVaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(existingProfileDir, 0700); err != nil {
		t.Fatal(err)
	}

	existingData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "local_token",
			"refresh_token": "local_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(12 * time.Hour).Format(time.RFC3339), // Older token
		},
	}
	existingJSON, _ := json.Marshal(existingData)
	if err := os.WriteFile(filepath.Join(existingProfileDir, ".claude.json"), existingJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Import in merge mode (should skip existing)
	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.Mode = ImportModeMerge
	importOpts.VaultPath = importVaultDir

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify profile was skipped
	if result.SkippedProfiles != 1 {
		t.Errorf("expected 1 skipped profile, got %d", result.SkippedProfiles)
	}

	// Verify local token was preserved
	importedData, _ := os.ReadFile(filepath.Join(existingProfileDir, ".claude.json"))
	var imported map[string]interface{}
	json.Unmarshal(importedData, &imported)

	oauthToken := imported["oauthToken"].(map[string]interface{})
	if oauthToken["access_token"] != "local_token" {
		t.Error("merge mode should have preserved local token")
	}
}

func TestVaultImporter_Import_ReplaceMode(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create source vault with profile
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "bundle_token",
			"refresh_token": "bundle_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Create destination vault with existing profile
	existingProfileDir := filepath.Join(importVaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(existingProfileDir, 0700); err != nil {
		t.Fatal(err)
	}

	existingData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "local_token",
			"refresh_token": "local_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(48 * time.Hour).Format(time.RFC3339), // Fresher token
		},
	}
	existingJSON, _ := json.Marshal(existingData)
	if err := os.WriteFile(filepath.Join(existingProfileDir, ".claude.json"), existingJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Import in replace mode (should overwrite even fresher local)
	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.Mode = ImportModeReplace
	importOpts.VaultPath = importVaultDir

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify profile was updated
	if result.UpdatedProfiles != 1 {
		t.Errorf("expected 1 updated profile, got %d", result.UpdatedProfiles)
	}

	// Verify bundle token replaced local
	importedData, _ := os.ReadFile(filepath.Join(existingProfileDir, ".claude.json"))
	var imported map[string]interface{}
	json.Unmarshal(importedData, &imported)

	oauthToken := imported["oauthToken"].(map[string]interface{})
	if oauthToken["access_token"] != "bundle_token" {
		t.Error("replace mode should have overwritten with bundle token")
	}
}

func TestVaultImporter_Import_SmartMode_FresherBundle(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create source vault with FRESHER profile
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "bundle_token",
			"refresh_token": "bundle_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(48 * time.Hour).Format(time.RFC3339), // FRESHER
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Create destination vault with OLDER profile
	existingProfileDir := filepath.Join(importVaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(existingProfileDir, 0700); err != nil {
		t.Fatal(err)
	}

	existingData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "local_token",
			"refresh_token": "local_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(12 * time.Hour).Format(time.RFC3339), // OLDER
		},
	}
	existingJSON, _ := json.Marshal(existingData)
	if err := os.WriteFile(filepath.Join(existingProfileDir, ".claude.json"), existingJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Import in smart mode (should update because bundle is fresher)
	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.Mode = ImportModeSmart
	importOpts.VaultPath = importVaultDir

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify profile was updated
	if result.UpdatedProfiles != 1 {
		t.Errorf("expected 1 updated profile, got %d", result.UpdatedProfiles)
	}

	// Verify fresher bundle token replaced local
	importedData, _ := os.ReadFile(filepath.Join(existingProfileDir, ".claude.json"))
	var imported map[string]interface{}
	json.Unmarshal(importedData, &imported)

	oauthToken := imported["oauthToken"].(map[string]interface{})
	if oauthToken["access_token"] != "bundle_token" {
		t.Error("smart mode should have imported fresher bundle token")
	}
}

func TestVaultImporter_Import_SmartMode_FresherLocal(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create source vault with OLDER profile
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "bundle_token",
			"refresh_token": "bundle_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(12 * time.Hour).Format(time.RFC3339), // OLDER
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Create destination vault with FRESHER profile
	existingProfileDir := filepath.Join(importVaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(existingProfileDir, 0700); err != nil {
		t.Fatal(err)
	}

	existingData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token":  "local_token",
			"refresh_token": "local_refresh",
			"token_type":    "Bearer",
			"expiry":        time.Now().Add(48 * time.Hour).Format(time.RFC3339), // FRESHER
		},
	}
	existingJSON, _ := json.Marshal(existingData)
	if err := os.WriteFile(filepath.Join(existingProfileDir, ".claude.json"), existingJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Import in smart mode (should skip because local is fresher)
	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.Mode = ImportModeSmart
	importOpts.VaultPath = importVaultDir

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify profile was skipped
	if result.SkippedProfiles != 1 {
		t.Errorf("expected 1 skipped profile, got %d", result.SkippedProfiles)
	}

	// Verify local token was preserved
	importedData, _ := os.ReadFile(filepath.Join(existingProfileDir, ".claude.json"))
	var imported map[string]interface{}
	json.Unmarshal(importedData, &imported)

	oauthToken := imported["oauthToken"].(map[string]interface{})
	if oauthToken["access_token"] != "local_token" {
		t.Error("smart mode should have preserved fresher local token")
	}
}

func TestVaultImporter_Import_ProviderFilter(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create source vault with multiple providers
	for _, provider := range []string{"claude", "codex", "gemini"} {
		profileDir := filepath.Join(vaultDir, provider, "test@example.com")
		if err := os.MkdirAll(profileDir, 0700); err != nil {
			t.Fatal(err)
		}

		var authData map[string]interface{}
		var fileName string

		switch provider {
		case "claude":
			authData = map[string]interface{}{
				"oauthToken": map[string]interface{}{
					"access_token": "claude_token",
					"expiry":       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			}
			fileName = ".claude.json"
		case "codex":
			authData = map[string]interface{}{
				"access_token": "codex_token",
				"expires_at":   time.Now().Add(24 * time.Hour).Unix(),
			}
			fileName = "auth.json"
		case "gemini":
			authData = map[string]interface{}{
				"oauth_credentials": map[string]interface{}{
					"access_token": "gemini_token",
					"expiry":       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			}
			fileName = "settings.json"
		}

		authJSON, _ := json.Marshal(authData)
		if err := os.WriteFile(filepath.Join(profileDir, fileName), authJSON, 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Export all
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Import only claude
	if err := os.MkdirAll(importVaultDir, 0700); err != nil {
		t.Fatal(err)
	}

	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.VaultPath = importVaultDir
	importOpts.ProviderFilter = []string{"claude"}

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify only claude was imported
	if result.NewProfiles != 1 {
		t.Errorf("expected 1 new profile (claude only), got %d", result.NewProfiles)
	}

	// Verify claude exists
	if _, err := os.Stat(filepath.Join(importVaultDir, "claude", "test@example.com")); os.IsNotExist(err) {
		t.Error("claude profile should have been imported")
	}

	// Verify codex does NOT exist
	if _, err := os.Stat(filepath.Join(importVaultDir, "codex", "test@example.com")); !os.IsNotExist(err) {
		t.Error("codex profile should NOT have been imported")
	}

	// Verify gemini does NOT exist
	if _, err := os.Stat(filepath.Join(importVaultDir, "gemini", "test@example.com")); !os.IsNotExist(err) {
		t.Error("gemini profile should NOT have been imported")
	}
}

func TestVaultImporter_Import_WithEncryption(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create vault with profile
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token": "secret_token",
			"expiry":       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export with encryption
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportOpts.Encrypt = true
	exportOpts.Password = "testpassword123"

	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("encrypted export failed: %v", err)
	}

	// Import with correct password
	if err := os.MkdirAll(importVaultDir, 0700); err != nil {
		t.Fatal(err)
	}

	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.VaultPath = importVaultDir
	importOpts.Password = "testpassword123"

	result, err := importer.Import(importOpts)
	if err != nil {
		t.Fatalf("import with password failed: %v", err)
	}

	if result.NewProfiles != 1 {
		t.Errorf("expected 1 new profile, got %d", result.NewProfiles)
	}

	// Verify content
	importedData, err := os.ReadFile(filepath.Join(importVaultDir, "claude", "test@example.com", ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}

	var imported map[string]interface{}
	json.Unmarshal(importedData, &imported)
	oauthToken := imported["oauthToken"].(map[string]interface{})
	if oauthToken["access_token"] != "secret_token" {
		t.Error("decrypted token doesn't match")
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		slice    []string
		s        string
		expected bool
	}{
		{[]string{"claude", "codex"}, "claude", true},
		{[]string{"claude", "codex"}, "Claude", true},
		{[]string{"claude", "codex"}, "CODEX", true},
		{[]string{"claude", "codex"}, "gemini", false},
		{[]string{}, "claude", false},
	}

	for _, tt := range tests {
		result := containsIgnoreCase(tt.slice, tt.s)
		if result != tt.expected {
			t.Errorf("containsIgnoreCase(%v, %q) = %v, want %v", tt.slice, tt.s, result, tt.expected)
		}
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		s        string
		patterns []string
		expected bool
	}{
		{"alice@example.com", []string{"alice"}, true},
		{"alice@example.com", []string{"bob"}, false},
		{"work@company.com", []string{"work", "personal"}, true},
		{"random@test.com", []string{}, false},
		{"Alice@Example.com", []string{"alice"}, true}, // case insensitive
	}

	for _, tt := range tests {
		result := matchesAnyPattern(tt.s, tt.patterns)
		if result != tt.expected {
			t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v", tt.s, tt.patterns, result, tt.expected)
		}
	}
}

// Tests for mergeJSONFile
func TestMergeJSONFile(t *testing.T) {
	t.Run("merge new keys into existing", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src.json")
		dstPath := filepath.Join(tmpDir, "dst.json")

		// Create source with new key
		srcData := `{"newkey": "newvalue", "shared": "source_value"}`
		if err := os.WriteFile(srcPath, []byte(srcData), 0600); err != nil {
			t.Fatal(err)
		}

		// Create destination with existing key
		dstData := `{"existing": "existingvalue", "shared": "dest_value"}`
		if err := os.WriteFile(dstPath, []byte(dstData), 0600); err != nil {
			t.Fatal(err)
		}

		if err := mergeJSONFile(srcPath, dstPath); err != nil {
			t.Fatalf("mergeJSONFile error = %v", err)
		}

		// Read result
		result, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatal(err)
		}

		var merged map[string]interface{}
		if err := json.Unmarshal(result, &merged); err != nil {
			t.Fatal(err)
		}

		// Should have all keys
		if merged["existing"] != "existingvalue" {
			t.Errorf("existing = %v, want existingvalue", merged["existing"])
		}
		if merged["newkey"] != "newvalue" {
			t.Errorf("newkey = %v, want newvalue", merged["newkey"])
		}
		// Source should overwrite destination
		if merged["shared"] != "source_value" {
			t.Errorf("shared = %v, want source_value", merged["shared"])
		}
	})

	t.Run("destination does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src.json")
		dstPath := filepath.Join(tmpDir, "subdir", "dst.json")

		srcData := `{"key": "value"}`
		if err := os.WriteFile(srcPath, []byte(srcData), 0600); err != nil {
			t.Fatal(err)
		}

		if err := mergeJSONFile(srcPath, dstPath); err != nil {
			t.Fatalf("mergeJSONFile error = %v", err)
		}

		// Should have created the file
		result, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("File should be created: %v", err)
		}

		var merged map[string]interface{}
		json.Unmarshal(result, &merged)
		if merged["key"] != "value" {
			t.Errorf("key = %v, want value", merged["key"])
		}
	})

	t.Run("corrupt destination is replaced", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src.json")
		dstPath := filepath.Join(tmpDir, "dst.json")

		srcData := `{"key": "value"}`
		if err := os.WriteFile(srcPath, []byte(srcData), 0600); err != nil {
			t.Fatal(err)
		}

		// Corrupt destination
		if err := os.WriteFile(dstPath, []byte("not valid json"), 0600); err != nil {
			t.Fatal(err)
		}

		if err := mergeJSONFile(srcPath, dstPath); err != nil {
			t.Fatalf("mergeJSONFile error = %v", err)
		}

		// Should have source values
		result, _ := os.ReadFile(dstPath)
		var merged map[string]interface{}
		json.Unmarshal(result, &merged)
		if merged["key"] != "value" {
			t.Errorf("key = %v, want value", merged["key"])
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "nonexistent.json")
		dstPath := filepath.Join(tmpDir, "dst.json")

		err := mergeJSONFile(srcPath, dstPath)
		if err == nil {
			t.Error("Expected error for nonexistent source")
		}
	})

	t.Run("source is not valid json", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src.json")
		dstPath := filepath.Join(tmpDir, "dst.json")

		if err := os.WriteFile(srcPath, []byte("not valid json"), 0600); err != nil {
			t.Fatal(err)
		}

		err := mergeJSONFile(srcPath, dstPath)
		if err == nil {
			t.Error("Expected error for invalid source JSON")
		}
	})
}

// Tests for copyDirectory
func TestCopyDirectory(t *testing.T) {
	t.Run("copies files recursively", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "dest")

		// Create source structure
		subDir := filepath.Join(srcDir, "subdir")
		if err := os.MkdirAll(subDir, 0700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("content2"), 0600); err != nil {
			t.Fatal(err)
		}

		if err := copyDirectory(srcDir, dstDir); err != nil {
			t.Fatalf("copyDirectory error = %v", err)
		}

		// Verify files
		content1, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
		if err != nil || string(content1) != "content1" {
			t.Errorf("file1.txt content = %q, want content1", content1)
		}

		content2, err := os.ReadFile(filepath.Join(dstDir, "subdir", "file2.txt"))
		if err != nil || string(content2) != "content2" {
			t.Errorf("subdir/file2.txt content = %q, want content2", content2)
		}
	})

	t.Run("creates destination if not exists", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "nested", "deep", "dest")

		if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}

		if err := copyDirectory(srcDir, dstDir); err != nil {
			t.Fatalf("copyDirectory error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(dstDir, "file.txt")); os.IsNotExist(err) {
			t.Error("file.txt should exist in destination")
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		srcDir := "/nonexistent/source"
		dstDir := t.TempDir()

		err := copyDirectory(srcDir, dstDir)
		if err == nil {
			t.Error("Expected error for nonexistent source")
		}
	})

	t.Run("empty source directory", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "dest")

		if err := copyDirectory(srcDir, dstDir); err != nil {
			t.Fatalf("copyDirectory error = %v", err)
		}

		// Destination should exist but be empty
		entries, _ := os.ReadDir(dstDir)
		if len(entries) != 0 {
			t.Errorf("Destination should be empty, got %d entries", len(entries))
		}
	})
}

// Tests for import with wrong password
func TestVaultImporter_Import_WrongPassword(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, "vault")
	outputDir := filepath.Join(tempDir, "output")
	importVaultDir := filepath.Join(tempDir, "import_vault")

	// Create vault with profile
	profileDir := filepath.Join(vaultDir, "claude", "test@example.com")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatal(err)
	}

	authData := map[string]interface{}{
		"oauthToken": map[string]interface{}{
			"access_token": "secret_token",
			"expiry":       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	authJSON, _ := json.Marshal(authData)
	if err := os.WriteFile(filepath.Join(profileDir, ".claude.json"), authJSON, 0600); err != nil {
		t.Fatal(err)
	}

	// Export with encryption
	exporter := &VaultExporter{VaultPath: vaultDir, DataPath: tempDir}
	exportOpts := DefaultExportOptions()
	exportOpts.OutputDir = outputDir
	exportOpts.Encrypt = true
	exportOpts.Password = "correct_password"

	exportResult, err := exporter.Export(exportOpts)
	if err != nil {
		t.Fatalf("encrypted export failed: %v", err)
	}

	// Try import with wrong password
	if err := os.MkdirAll(importVaultDir, 0700); err != nil {
		t.Fatal(err)
	}

	importer := &VaultImporter{BundlePath: exportResult.OutputPath}
	importOpts := DefaultImportOptions()
	importOpts.VaultPath = importVaultDir
	importOpts.Password = "wrong_password"

	_, err = importer.Import(importOpts)
	if err == nil {
		t.Error("Import should fail with wrong password")
	}
}
