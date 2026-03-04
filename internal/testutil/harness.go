// Package testutil provides E2E test infrastructure with detailed logging.
//
// This package offers a TestHarness for managing test environments,
// structured logging, fixture creation, and assertion helpers.
// All components are designed to work with the standard testing package
// without external dependencies.
package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Logger - Structured logging for E2E tests
// =============================================================================

// LogLevel represents the severity of a log message.
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a single legacy log entry.
//
// Deprecated: canonical JSONL events emitted by CanonicalLogger are the
// authoritative e2e telemetry format.
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Test      string                 `json:"test"`
	Step      string                 `json:"step,omitempty"`
	Message   string                 `json:"message"`
	Duration  string                 `json:"duration,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// Logger provides structured logging for tests.
//
// Deprecated: prefer NewExtendedHarness + CanonicalLogger for new e2e tests.
// This logger remains for compatibility and now mirrors INFO+ events into the
// canonical schema when attached.
type Logger struct {
	t         *testing.T
	testName  string
	step      string
	stepStart time.Time
	minLevel  LogLevel
	startTime time.Time
	verbose   bool
	jsonMode  bool
	canonical *CanonicalLogger
}

// NewLogger creates a new logger for the given test.
func NewLogger(t *testing.T) *Logger {
	verbose := testing.Verbose()
	return &Logger{
		t:         t,
		testName:  t.Name(),
		stepStart: time.Now(),
		minLevel:  INFO,
		startTime: time.Now(),
		verbose:   verbose,
		jsonMode:  false,
	}
}

// AttachCanonical enables canonical JSONL event logging alongside standard test logs.
func (l *Logger) AttachCanonical(c *CanonicalLogger) {
	l.canonical = c
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level LogLevel) {
	l.minLevel = level
}

// SetJSONMode enables JSON output for CI integration.
func (l *Logger) SetJSONMode(enabled bool) {
	l.jsonMode = enabled
}

// SetStep sets the current test step for context.
func (l *Logger) SetStep(step string) {
	l.step = step
	l.stepStart = time.Now()
}

// log writes a log entry.
func (l *Logger) log(level LogLevel, msg string, data map[string]interface{}) {
	if level < l.minLevel {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Test:      l.testName,
		Step:      l.step,
		Message:   msg,
		Duration:  time.Since(l.startTime).String(),
		Data:      data,
	}

	if l.jsonMode {
		jsonBytes, _ := json.Marshal(entry)
		l.t.Log(string(jsonBytes))
	} else if l.verbose || level >= WARN {
		// Human-readable format
		prefix := fmt.Sprintf("[%s] %s", entry.Level, entry.Duration)
		if l.step != "" {
			prefix += fmt.Sprintf(" [%s]", l.step)
		}
		output := fmt.Sprintf("%s %s", prefix, msg)
		if len(data) > 0 {
			dataStr, _ := json.Marshal(data)
			output += fmt.Sprintf(" %s", dataStr)
		}
		l.t.Log(output)
	}

	if l.canonical != nil && level >= INFO {
		decision := DecisionContinue
		switch level {
		case WARN:
			decision = DecisionRetry
		case ERROR:
			decision = DecisionAbort
		}

		stepID := normalizeCanonicalStepID(l.step)
		if stepID == "" {
			stepID = normalizeCanonicalStepID(msg)
		}
		if stepID == "" {
			stepID = l.canonical.NextStepID("legacy-log")
		}

		output := map[string]interface{}{
			"message": msg,
			"level":   entry.Level,
			"test":    l.testName,
		}
		for k, v := range data {
			output[k] = v
		}

		_ = l.canonical.LogStep(
			stepID,
			ComponentTest,
			decision,
			time.Since(l.stepStart).Milliseconds(),
			map[string]interface{}{
				"step": l.step,
			},
			output,
			nil,
		)
	}
}

func normalizeCanonicalStepID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		" ", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		".", "_",
		"-", "_",
	)
	return replacer.Replace(s)
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, data ...map[string]interface{}) {
	var d map[string]interface{}
	if len(data) > 0 {
		d = data[0]
	}
	l.log(DEBUG, msg, d)
}

// Info logs an info message.
func (l *Logger) Info(msg string, data ...map[string]interface{}) {
	var d map[string]interface{}
	if len(data) > 0 {
		d = data[0]
	}
	l.log(INFO, msg, d)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, data ...map[string]interface{}) {
	var d map[string]interface{}
	if len(data) > 0 {
		d = data[0]
	}
	l.log(WARN, msg, d)
}

// Error logs an error message.
func (l *Logger) Error(msg string, data ...map[string]interface{}) {
	var d map[string]interface{}
	if len(data) > 0 {
		d = data[0]
	}
	l.log(ERROR, msg, d)
}

// =============================================================================
// TestHarness - Test environment management
// =============================================================================

// TestHarness manages the test environment for E2E tests.
type TestHarness struct {
	T       *testing.T
	Log     *Logger
	Canon   *CanonicalLogger
	TempDir string

	// Saved environment state for cleanup
	savedEnv map[string]string
	envSet   map[string]bool

	// Cleanup functions to run on Close
	cleanups []func()
}

// NewHarness creates a compatibility test harness.
//
// Deprecated: use NewExtendedHarness for new e2e integration tests. NewHarness
// dual-writes canonical JSONL events so existing suites remain schema-compliant
// during migration.
func NewHarness(t *testing.T) *TestHarness {
	tempDir := t.TempDir()
	logger := NewLogger(t)
	scenarioID := ScenarioIDFromTestName(t.Name())
	canonicalPath := filepath.Join(tempDir, "artifacts", "e2e", scenarioID+".jsonl")
	canon := NewCanonicalLogger(CanonicalLoggerConfig{
		ScenarioID: scenarioID,
		Actor:      ActorCI,
	})
	if err := canon.SetOutputPath(canonicalPath); err != nil {
		logger.Warn("Failed to enable canonical logger", map[string]interface{}{"error": err.Error()})
	} else {
		logger.AttachCanonical(canon)
		logger.Info("Canonical logger enabled", map[string]interface{}{"path": canonicalPath, "scenario_id": scenarioID})
	}

	h := &TestHarness{
		T:        t,
		Log:      logger,
		Canon:    canon,
		TempDir:  tempDir,
		savedEnv: make(map[string]string),
		envSet:   make(map[string]bool),
		cleanups: []func(){},
	}

	logger.Info("Test harness initialized", map[string]interface{}{
		"temp_dir": tempDir,
	})

	return h
}

// SetEnv sets an environment variable and saves the original for cleanup.
func (h *TestHarness) SetEnv(key, value string) {
	if _, saved := h.savedEnv[key]; !saved {
		h.savedEnv[key] = os.Getenv(key)
		_, exists := os.LookupEnv(key)
		h.envSet[key] = exists
	}
	os.Setenv(key, value)
	h.Log.Debug("Set environment variable", map[string]interface{}{
		"key":   key,
		"value": value,
	})
}

// UnsetEnv unsets an environment variable and saves the original for cleanup.
func (h *TestHarness) UnsetEnv(key string) {
	if _, saved := h.savedEnv[key]; !saved {
		h.savedEnv[key] = os.Getenv(key)
		_, h.envSet[key] = os.LookupEnv(key)
	}
	os.Unsetenv(key)
	h.Log.Debug("Unset environment variable", map[string]interface{}{
		"key": key,
	})
}

// AddCleanup registers a cleanup function to run when Close is called.
func (h *TestHarness) AddCleanup(fn func()) {
	h.cleanups = append(h.cleanups, fn)
}

// Close restores environment and runs cleanup functions.
func (h *TestHarness) Close() {
	h.Log.SetStep("cleanup")

	// Restore environment variables
	for key, value := range h.savedEnv {
		if h.envSet[key] {
			os.Setenv(key, value)
		} else {
			os.Unsetenv(key)
		}
	}

	// Run cleanup functions in reverse order
	for i := len(h.cleanups) - 1; i >= 0; i-- {
		h.cleanups[i]()
	}

	if h.Canon != nil {
		if err := h.Canon.Close(); err != nil {
			h.Log.Warn("Failed to close canonical logger", map[string]interface{}{"error": err.Error()})
		}
	}

	h.Log.Info("Test harness closed")
}

// SubDir creates a subdirectory in the temp directory.
func (h *TestHarness) SubDir(name string) string {
	dir := filepath.Join(h.TempDir, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		h.T.Fatalf("Failed to create subdir %s: %v", name, err)
	}
	return dir
}

// WriteFile writes content to a file in the temp directory.
func (h *TestHarness) WriteFile(relPath, content string) string {
	fullPath := filepath.Join(h.TempDir, relPath)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		h.T.Fatalf("Failed to create dir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0600); err != nil {
		h.T.Fatalf("Failed to write file %s: %v", relPath, err)
	}
	h.Log.Debug("Wrote file", map[string]interface{}{
		"path": relPath,
		"size": len(content),
	})
	return fullPath
}

// WriteJSON writes JSON content to a file.
func (h *TestHarness) WriteJSON(relPath string, data interface{}) string {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		h.T.Fatalf("Failed to marshal JSON for %s: %v", relPath, err)
	}
	return h.WriteFile(relPath, string(jsonBytes))
}

// =============================================================================
// Fixture Helpers - Create realistic test data
// =============================================================================

// ClaudeAuthFixture represents Claude auth file content.
type ClaudeAuthFixture struct {
	SessionToken string `json:"session_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// CreateClaudeAuthFixture creates a Claude .claude.json fixture.
func (h *TestHarness) CreateClaudeAuthFixture(name string) string {
	fixture := ClaudeAuthFixture{
		SessionToken: fmt.Sprintf("claude-session-%s-%d", name, time.Now().UnixNano()),
		RefreshToken: fmt.Sprintf("claude-refresh-%s-%d", name, time.Now().UnixNano()),
		ExpiresAt:    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}
	path := h.WriteJSON(fmt.Sprintf("claude/%s/.claude.json", name), fixture)
	h.Log.Info("Created Claude auth fixture", map[string]interface{}{
		"name": name,
		"path": path,
	})
	return path
}

// CodexAuthFixture represents Codex auth file content.
type CodexAuthFixture struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// CreateCodexAuthFixture creates a Codex auth.json fixture.
func (h *TestHarness) CreateCodexAuthFixture(name string) string {
	fixture := CodexAuthFixture{
		AccessToken:  fmt.Sprintf("codex-access-%s-%d", name, time.Now().UnixNano()),
		RefreshToken: fmt.Sprintf("codex-refresh-%s-%d", name, time.Now().UnixNano()),
		TokenType:    "Bearer",
		ExpiresIn:    3600,
	}
	path := h.WriteJSON(fmt.Sprintf("codex/%s/auth.json", name), fixture)
	h.Log.Info("Created Codex auth fixture", map[string]interface{}{
		"name": name,
		"path": path,
	})
	return path
}

// GeminiAuthFixture represents Gemini settings file content.
type GeminiAuthFixture struct {
	GoogleOAuth struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expires_at"`
	} `json:"google_oauth"`
	DefaultModel string `json:"default_model"`
}

// CreateGeminiAuthFixture creates a Gemini settings.json fixture.
func (h *TestHarness) CreateGeminiAuthFixture(name string) string {
	fixture := GeminiAuthFixture{
		DefaultModel: "gemini-pro",
	}
	fixture.GoogleOAuth.AccessToken = fmt.Sprintf("gemini-access-%s-%d", name, time.Now().UnixNano())
	fixture.GoogleOAuth.RefreshToken = fmt.Sprintf("gemini-refresh-%s-%d", name, time.Now().UnixNano())
	fixture.GoogleOAuth.ExpiresAt = time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	path := h.WriteJSON(fmt.Sprintf("gemini/%s/settings.json", name), fixture)
	h.Log.Info("Created Gemini auth fixture", map[string]interface{}{
		"name": name,
		"path": path,
	})
	return path
}

// ProfileFixture represents a caam profile.
type ProfileFixture struct {
	Name         string            `json:"name"`
	Provider     string            `json:"provider"`
	AuthMode     string            `json:"auth_mode"`
	BasePath     string            `json:"base_path"`
	AccountLabel string            `json:"account_label,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CreateProfileFixture creates a profile fixture with directories.
func (h *TestHarness) CreateProfileFixture(provider, name string) string {
	basePath := filepath.Join(h.TempDir, "profiles", provider, name)

	// Create directory structure
	for _, subdir := range []string{"home", "xdg_config", "codex_home"} {
		if err := os.MkdirAll(filepath.Join(basePath, subdir), 0700); err != nil {
			h.T.Fatalf("Failed to create profile dir: %v", err)
		}
	}

	// Create profile.json
	fixture := ProfileFixture{
		Name:         name,
		Provider:     provider,
		AuthMode:     "oauth",
		BasePath:     basePath,
		AccountLabel: fmt.Sprintf("%s@example.com", name),
		CreatedAt:    time.Now(),
		Metadata:     make(map[string]string),
	}

	metaPath := filepath.Join(basePath, "profile.json")
	jsonBytes, _ := json.MarshalIndent(fixture, "", "  ")
	if err := os.WriteFile(metaPath, jsonBytes, 0600); err != nil {
		h.T.Fatalf("Failed to write profile.json: %v", err)
	}

	h.Log.Info("Created profile fixture", map[string]interface{}{
		"provider": provider,
		"name":     name,
		"path":     basePath,
	})

	return basePath
}

// =============================================================================
// Assertion Helpers - Test assertions with detailed error messages
// =============================================================================

// FileExists asserts that a file exists.
func (h *TestHarness) FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			h.T.Errorf("FileExists: file does not exist: %s", path)
		} else {
			h.T.Errorf("FileExists: error checking file %s: %v", path, err)
		}
		return false
	}
	if info.IsDir() {
		h.T.Errorf("FileExists: path is a directory, not a file: %s", path)
		return false
	}
	return true
}

// DirExists asserts that a directory exists.
func (h *TestHarness) DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			h.T.Errorf("DirExists: directory does not exist: %s", path)
		} else {
			h.T.Errorf("DirExists: error checking directory %s: %v", path, err)
		}
		return false
	}
	if !info.IsDir() {
		h.T.Errorf("DirExists: path is a file, not a directory: %s", path)
		return false
	}
	return true
}

// FileNotExists asserts that a file does not exist.
func (h *TestHarness) FileNotExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		h.T.Errorf("FileNotExists: file exists but should not: %s", path)
		return false
	}
	if !os.IsNotExist(err) {
		h.T.Errorf("FileNotExists: unexpected error checking file %s: %v", path, err)
		return false
	}
	return true
}

// FileContains asserts that a file contains the given substring.
func (h *TestHarness) FileContains(path, substring string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		h.T.Errorf("FileContains: failed to read file %s: %v", path, err)
		return false
	}
	if !strings.Contains(string(content), substring) {
		h.T.Errorf("FileContains: file %s does not contain %q\nContent: %s", path, substring, content)
		return false
	}
	return true
}

// FileEquals asserts that a file has exactly the given content.
func (h *TestHarness) FileEquals(path, expected string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		h.T.Errorf("FileEquals: failed to read file %s: %v", path, err)
		return false
	}
	if string(content) != expected {
		h.T.Errorf("FileEquals: file %s content mismatch\nExpected: %q\nGot: %q", path, expected, content)
		return false
	}
	return true
}

// JSONEquals asserts that a JSON file equals the expected structure.
func (h *TestHarness) JSONEquals(path string, expected interface{}) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		h.T.Errorf("JSONEquals: failed to read file %s: %v", path, err)
		return false
	}

	// Parse actual JSON
	var actual interface{}
	if err := json.Unmarshal(content, &actual); err != nil {
		h.T.Errorf("JSONEquals: failed to parse JSON in %s: %v", path, err)
		return false
	}

	// Compare using reflection
	if !reflect.DeepEqual(actual, expected) {
		actualJSON, _ := json.MarshalIndent(actual, "", "  ")
		expectedJSON, _ := json.MarshalIndent(expected, "", "  ")
		h.T.Errorf("JSONEquals: JSON mismatch in %s\nExpected:\n%s\nGot:\n%s", path, expectedJSON, actualJSON)
		return false
	}
	return true
}

// JSONContains asserts that a JSON file contains specific key-value pairs.
func (h *TestHarness) JSONContains(path string, key string, expectedValue interface{}) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		h.T.Errorf("JSONContains: failed to read file %s: %v", path, err)
		return false
	}

	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		h.T.Errorf("JSONContains: failed to parse JSON in %s: %v", path, err)
		return false
	}

	actual, ok := data[key]
	if !ok {
		h.T.Errorf("JSONContains: key %q not found in %s", key, path)
		return false
	}

	if !reflect.DeepEqual(actual, expectedValue) {
		h.T.Errorf("JSONContains: value mismatch for key %q in %s\nExpected: %v\nGot: %v", key, path, expectedValue, actual)
		return false
	}
	return true
}

// DirStructureMatches asserts that a directory contains the expected structure.
// The expected map has relative paths as keys and "file" or "dir" as values.
func (h *TestHarness) DirStructureMatches(basePath string, expected map[string]string) bool {
	allMatch := true
	for relPath, expectedType := range expected {
		fullPath := filepath.Join(basePath, relPath)
		info, err := os.Stat(fullPath)

		if err != nil {
			h.T.Errorf("DirStructureMatches: path %s does not exist", relPath)
			allMatch = false
			continue
		}

		switch expectedType {
		case "file":
			if info.IsDir() {
				h.T.Errorf("DirStructureMatches: expected file but got directory: %s", relPath)
				allMatch = false
			}
		case "dir":
			if !info.IsDir() {
				h.T.Errorf("DirStructureMatches: expected directory but got file: %s", relPath)
				allMatch = false
			}
		default:
			h.T.Errorf("DirStructureMatches: unknown type %q for %s (use 'file' or 'dir')", expectedType, relPath)
			allMatch = false
		}
	}
	return allMatch
}

// FilePermissions asserts that a file has the expected permissions.
func (h *TestHarness) FilePermissions(path string, expected os.FileMode) bool {
	info, err := os.Stat(path)
	if err != nil {
		h.T.Errorf("FilePermissions: failed to stat %s: %v", path, err)
		return false
	}

	actual := info.Mode().Perm()
	if actual != expected {
		h.T.Errorf("FilePermissions: %s has permissions %o, expected %o", path, actual, expected)
		return false
	}
	return true
}
