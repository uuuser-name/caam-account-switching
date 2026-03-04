package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// Logger Tests
// =============================================================================

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
		{LogLevel(99), "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			if got := tc.level.String(); got != tc.expected {
				t.Errorf("LogLevel(%d).String() = %q, want %q", tc.level, got, tc.expected)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	logger := NewLogger(t)

	if logger.testName != t.Name() {
		t.Errorf("logger.testName = %q, want %q", logger.testName, t.Name())
	}
	if logger.minLevel != INFO {
		t.Errorf("logger.minLevel = %d, want %d (INFO)", logger.minLevel, INFO)
	}
	if logger.startTime.IsZero() {
		t.Error("logger.startTime should not be zero")
	}
}

func TestLogger_SetLevel(t *testing.T) {
	logger := NewLogger(t)
	logger.SetLevel(DEBUG)

	if logger.minLevel != DEBUG {
		t.Errorf("logger.minLevel = %d, want %d (DEBUG)", logger.minLevel, DEBUG)
	}
}

func TestLogger_SetJSONMode(t *testing.T) {
	logger := NewLogger(t)
	logger.SetJSONMode(true)

	if !logger.jsonMode {
		t.Error("logger.jsonMode should be true")
	}
}

func TestLogger_SetStep(t *testing.T) {
	logger := NewLogger(t)
	logger.SetStep("setup")

	if logger.step != "setup" {
		t.Errorf("logger.step = %q, want %q", logger.step, "setup")
	}
}

func TestLogger_LogMethods(t *testing.T) {
	// These methods should not panic
	logger := NewLogger(t)
	logger.SetLevel(DEBUG)

	// Test without data
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// Test with data
	data := map[string]interface{}{"key": "value"}
	logger.Debug("debug with data", data)
	logger.Info("info with data", data)
	logger.Warn("warn with data", data)
	logger.Error("error with data", data)
}

// =============================================================================
// TestHarness Tests
// =============================================================================

func TestNewHarness(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	if h.T != t {
		t.Error("harness.T should be the test")
	}
	if h.Log == nil {
		t.Error("harness.Log should not be nil")
	}
	if h.TempDir == "" {
		t.Error("harness.TempDir should not be empty")
	}
	if _, err := os.Stat(h.TempDir); os.IsNotExist(err) {
		t.Errorf("harness.TempDir does not exist: %s", h.TempDir)
	}
}

func TestHarness_SetEnv(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	// Save original value
	original := os.Getenv("TESTUTIL_TEST_VAR")
	defer os.Setenv("TESTUTIL_TEST_VAR", original)

	// Set new value
	h.SetEnv("TESTUTIL_TEST_VAR", "test_value")

	if got := os.Getenv("TESTUTIL_TEST_VAR"); got != "test_value" {
		t.Errorf("env var = %q, want %q", got, "test_value")
	}
}

func TestHarness_UnsetEnv(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	// Set a value first
	os.Setenv("TESTUTIL_UNSET_VAR", "to_be_removed")

	h.UnsetEnv("TESTUTIL_UNSET_VAR")

	if _, exists := os.LookupEnv("TESTUTIL_UNSET_VAR"); exists {
		t.Error("env var should have been unset")
	}
}

func TestHarness_Close_RestoresEnv(t *testing.T) {
	// Set up initial state
	os.Setenv("TESTUTIL_RESTORE_VAR", "original")
	os.Unsetenv("TESTUTIL_NEW_VAR")

	h := NewHarness(t)

	// Modify environment
	h.SetEnv("TESTUTIL_RESTORE_VAR", "modified")
	h.SetEnv("TESTUTIL_NEW_VAR", "new_value")

	// Close should restore
	h.Close()

	// Check restoration
	if got := os.Getenv("TESTUTIL_RESTORE_VAR"); got != "original" {
		t.Errorf("TESTUTIL_RESTORE_VAR = %q, want %q", got, "original")
	}
	if _, exists := os.LookupEnv("TESTUTIL_NEW_VAR"); exists {
		t.Error("TESTUTIL_NEW_VAR should have been unset")
	}

	// Clean up
	os.Unsetenv("TESTUTIL_RESTORE_VAR")
}

func TestHarness_AddCleanup(t *testing.T) {
	h := NewHarness(t)

	var order []int
	h.AddCleanup(func() { order = append(order, 1) })
	h.AddCleanup(func() { order = append(order, 2) })
	h.AddCleanup(func() { order = append(order, 3) })

	h.Close()

	// Cleanups should run in reverse order
	expected := []int{3, 2, 1}
	if len(order) != len(expected) {
		t.Fatalf("got %d cleanups, want %d", len(order), len(expected))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %d, want %d", i, order[i], v)
		}
	}
}

func TestHarness_SubDir(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	dir := h.SubDir("test/nested/dir")

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("SubDir did not create directory: %s", dir)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("SubDir should return absolute path, got: %s", dir)
	}
}

func TestHarness_WriteFile(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	content := "test content"
	path := h.WriteFile("test/file.txt", content)

	// Check file exists and has correct content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", data, content)
	}
}

func TestHarness_WriteJSON(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	data := map[string]string{"key": "value"}
	path := h.WriteJSON("test/data.json", data)

	// Read and parse
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("JSON key = %q, want %q", result["key"], "value")
	}
}

// =============================================================================
// Fixture Tests
// =============================================================================

func TestHarness_CreateClaudeAuthFixture(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	path := h.CreateClaudeAuthFixture("work")

	// Check file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("fixture file not created: %s", path)
	}

	// Parse and validate
	content, _ := os.ReadFile(path)
	var fixture ClaudeAuthFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}

	if fixture.SessionToken == "" {
		t.Error("fixture.SessionToken should not be empty")
	}
	if fixture.RefreshToken == "" {
		t.Error("fixture.RefreshToken should not be empty")
	}
	if fixture.ExpiresAt == "" {
		t.Error("fixture.ExpiresAt should not be empty")
	}
}

func TestHarness_CreateCodexAuthFixture(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	path := h.CreateCodexAuthFixture("personal")

	// Parse and validate
	content, _ := os.ReadFile(path)
	var fixture CodexAuthFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}

	if fixture.AccessToken == "" {
		t.Error("fixture.AccessToken should not be empty")
	}
	if fixture.TokenType != "Bearer" {
		t.Errorf("fixture.TokenType = %q, want %q", fixture.TokenType, "Bearer")
	}
}

func TestHarness_CreateGeminiAuthFixture(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	path := h.CreateGeminiAuthFixture("test")

	// Parse and validate
	content, _ := os.ReadFile(path)
	var fixture GeminiAuthFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}

	if fixture.GoogleOAuth.AccessToken == "" {
		t.Error("fixture.GoogleOAuth.AccessToken should not be empty")
	}
	if fixture.DefaultModel != "gemini-pro" {
		t.Errorf("fixture.DefaultModel = %q, want %q", fixture.DefaultModel, "gemini-pro")
	}
}

func TestHarness_CreateProfileFixture(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	basePath := h.CreateProfileFixture("claude", "work")

	// Check directory structure
	expectedDirs := []string{"home", "xdg_config", "codex_home"}
	for _, dir := range expectedDirs {
		dirPath := filepath.Join(basePath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Errorf("expected directory not created: %s", dirPath)
		}
	}

	// Check profile.json
	metaPath := filepath.Join(basePath, "profile.json")
	content, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read profile.json: %v", err)
	}

	var profile ProfileFixture
	if err := json.Unmarshal(content, &profile); err != nil {
		t.Fatalf("failed to parse profile.json: %v", err)
	}

	if profile.Name != "work" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "work")
	}
	if profile.Provider != "claude" {
		t.Errorf("profile.Provider = %q, want %q", profile.Provider, "claude")
	}
}

// =============================================================================
// Assertion Tests
// =============================================================================

func TestHarness_FileExists(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	// Create a file
	path := h.WriteFile("exists.txt", "content")

	// Use a separate test instance to capture errors
	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	// Should return true for existing file
	if !mockH.FileExists(path) {
		t.Error("FileExists should return true for existing file")
	}

	// Should return false for non-existent file
	if mockH.FileExists(filepath.Join(h.TempDir, "nonexistent.txt")) {
		t.Error("FileExists should return false for non-existent file")
	}

	// Should return false for directory
	dir := h.SubDir("testdir")
	if mockH.FileExists(dir) {
		t.Error("FileExists should return false for directory")
	}
}

func TestHarness_DirExists(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	// Create a directory
	dir := h.SubDir("mydir")

	// Should return true for existing directory
	if !mockH.DirExists(dir) {
		t.Error("DirExists should return true for existing directory")
	}

	// Should return false for file
	file := h.WriteFile("file.txt", "content")
	if mockH.DirExists(file) {
		t.Error("DirExists should return false for file")
	}
}

func TestHarness_FileNotExists(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	// Should return true for non-existent file
	if !mockH.FileNotExists(filepath.Join(h.TempDir, "nonexistent.txt")) {
		t.Error("FileNotExists should return true for non-existent file")
	}

	// Should return false for existing file
	path := h.WriteFile("exists.txt", "content")
	if mockH.FileNotExists(path) {
		t.Error("FileNotExists should return false for existing file")
	}
}

func TestHarness_FileContains(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	path := h.WriteFile("content.txt", "hello world")

	// Should find substring
	if !mockH.FileContains(path, "world") {
		t.Error("FileContains should find existing substring")
	}

	// Should not find missing substring
	if mockH.FileContains(path, "missing") {
		t.Error("FileContains should not find missing substring")
	}
}

func TestHarness_FileEquals(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	content := "exact content"
	path := h.WriteFile("exact.txt", content)

	// Should match exact content
	if !mockH.FileEquals(path, content) {
		t.Error("FileEquals should match exact content")
	}

	// Should not match different content
	if mockH.FileEquals(path, "different content") {
		t.Error("FileEquals should not match different content")
	}
}

func TestHarness_JSONEquals(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	data := map[string]interface{}{"key": "value", "num": float64(42)}
	path := h.WriteJSON("data.json", data)

	// Should match equal structure
	if !mockH.JSONEquals(path, data) {
		t.Error("JSONEquals should match equal structure")
	}

	// Should not match different structure
	different := map[string]interface{}{"key": "other"}
	if mockH.JSONEquals(path, different) {
		t.Error("JSONEquals should not match different structure")
	}
}

func TestHarness_JSONContains(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	data := map[string]interface{}{"name": "test", "count": float64(10)}
	path := h.WriteJSON("partial.json", data)

	// Should find existing key-value
	if !mockH.JSONContains(path, "name", "test") {
		t.Error("JSONContains should find existing key-value")
	}

	// Should not find wrong value
	if mockH.JSONContains(path, "name", "wrong") {
		t.Error("JSONContains should not match wrong value")
	}

	// Should not find missing key
	if mockH.JSONContains(path, "missing", "value") {
		t.Error("JSONContains should not find missing key")
	}
}

func TestHarness_DirStructureMatches(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	// Create structure
	h.SubDir("structure/subdir")
	h.WriteFile("structure/file.txt", "content")

	expected := map[string]string{
		"subdir":   "dir",
		"file.txt": "file",
	}

	basePath := filepath.Join(h.TempDir, "structure")
	if !mockH.DirStructureMatches(basePath, expected) {
		t.Error("DirStructureMatches should match correct structure")
	}

	// Should fail with wrong expectation
	wrongExpected := map[string]string{
		"subdir":   "file", // Wrong type
		"file.txt": "file",
	}
	if mockH.DirStructureMatches(basePath, wrongExpected) {
		t.Error("DirStructureMatches should not match incorrect structure")
	}
}

func TestHarness_FilePermissions(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	mockT := &testing.T{}
	mockH := &TestHarness{T: mockT, TempDir: h.TempDir, Log: h.Log}

	path := h.WriteFile("perms.txt", "content")
	os.Chmod(path, 0644)

	// Should match correct permissions
	if !mockH.FilePermissions(path, 0644) {
		t.Error("FilePermissions should match correct permissions")
	}

	// Should not match wrong permissions
	if mockH.FilePermissions(path, 0755) {
		t.Error("FilePermissions should not match wrong permissions")
	}
}
