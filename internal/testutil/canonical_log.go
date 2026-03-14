// Package testutil provides E2E test infrastructure with detailed logging.
//
// This file defines the canonical log event types and logger for E2E tests.
// All log events conform to docs/testing/e2e_log_schema.json.
package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// =============================================================================
// Canonical Log Event Types (matching e2e_log_schema.json v1.1.0)
// =============================================================================

// CanonicalLogEvent represents a single E2E log event conforming to the canonical schema.
// Schema: docs/testing/e2e_log_schema.json
type CanonicalLogEvent struct {
	// Required fields
	RunID         string                 `json:"run_id"`
	ScenarioID    string                 `json:"scenario_id"`
	StepID        string                 `json:"step_id"`
	Timestamp     string                 `json:"timestamp"`
	Actor         string                 `json:"actor"`
	Component     string                 `json:"component"`
	InputRedacted map[string]interface{} `json:"input_redacted"`
	Output        map[string]interface{} `json:"output"`
	Decision      string                 `json:"decision"`
	DurationMs    int64                  `json:"duration_ms"`
	Error         ErrorEnvelope          `json:"error"`
}

// ErrorEnvelope represents the error structure in canonical log events.
type ErrorEnvelope struct {
	Present bool                   `json:"present"`
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details"`
}

// Decision constants for canonical logs.
const (
	DecisionPass     = "pass"
	DecisionContinue = "continue"
	DecisionRetry    = "retry"
	DecisionAbort    = "abort"
)

// Component constants for canonical logs.
const (
	ComponentAuthPool  = "authpool"
	ComponentVault     = "vault"
	ComponentDaemon    = "daemon"
	ComponentCLI       = "cli"
	ComponentSession   = "session"
	ComponentProfile   = "profile"
	ComponentConfig    = "config"
	ComponentBackup    = "backup"
	ComponentRestore   = "restore"
	ComponentRotate    = "rotate"
	ComponentSwitch    = "switch"
	ComponentSync      = "sync"
	ComponentBundle    = "bundle"
	ComponentRateLimit = "ratelimit"
	ComponentCooldown  = "cooldown"
	ComponentWatcher   = "watcher"
	ComponentTest      = "test"
)

// Actor constants for canonical logs.
const (
	ActorCI      = "ci"
	ActorUser    = "user"
	ActorDaemon  = "daemon"
	ActorWatcher = "watcher"
	ActorSystem  = "system"
)

// =============================================================================
// Canonical Logger
// =============================================================================

// CanonicalLogger produces schema-valid JSONL log output for E2E tests.
type CanonicalLogger struct {
	mu          sync.Mutex
	runID       string
	scenarioID  string
	actor       string
	outputPath  string
	file        *os.File
	events      []CanonicalLogEvent
	startTime   time.Time
	stepCounter int
}

// CanonicalLoggerConfig holds configuration for the canonical logger.
type CanonicalLoggerConfig struct {
	// RunID is the stable identifier for this test run.
	// If empty, a UUID will be generated.
	RunID string

	// ScenarioID is the stable scenario key from the workflow matrix.
	ScenarioID string

	// Actor is the human or automation actor producing events.
	// Defaults to "ci".
	Actor string

	// OutputPath is the path to write JSONL output.
	// If empty, no file is written (only in-memory).
	OutputPath string
}

// NewCanonicalLogger creates a new canonical logger.
func NewCanonicalLogger(config CanonicalLoggerConfig) *CanonicalLogger {
	runID := config.RunID
	if runID == "" {
		runID = fmt.Sprintf("run-%s-%s", time.Now().Format("20060102"), uuid.New().String()[:8])
	}

	actor := config.Actor
	if actor == "" {
		actor = ActorCI
	}

	l := &CanonicalLogger{
		runID:      runID,
		scenarioID: config.ScenarioID,
		actor:      actor,
		events:     make([]CanonicalLogEvent, 0),
		startTime:  time.Now(),
	}
	if strings.TrimSpace(config.OutputPath) != "" {
		if err := l.SetOutputPath(config.OutputPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize canonical log output: %v\n", err)
		}
	}

	return l
}

// RunID returns the current run ID.
func (l *CanonicalLogger) RunID() string {
	return l.runID
}

// ScenarioID returns the current scenario ID.
func (l *CanonicalLogger) ScenarioID() string {
	return l.scenarioID
}

// SetScenarioID updates the scenario ID for subsequent events.
func (l *CanonicalLogger) SetScenarioID(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scenarioID = id
}

// SetOutputPath sets the output file path for JSONL output.
func (l *CanonicalLogger) SetOutputPath(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.closeFileLocked(); err != nil {
		return fmt.Errorf("close existing output file: %w", err)
	}

	l.outputPath = path
	if strings.TrimSpace(path) == "" {
		return nil
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	// Open file for appending
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	l.file = f

	return nil
}

func (l *CanonicalLogger) closeFileLocked() error {
	if l.file == nil {
		return nil
	}
	if err := l.file.Close(); err != nil {
		return err
	}
	l.file = nil
	return nil
}

// LogEvent writes a canonical log event.
func (l *CanonicalLogger) LogEvent(event CanonicalLogEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Ensure timestamp is set
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	// Ensure run_id and scenario_id are set
	if event.RunID == "" {
		event.RunID = l.runID
	}
	if event.ScenarioID == "" {
		event.ScenarioID = l.scenarioID
	}
	if event.Actor == "" {
		event.Actor = l.actor
	}

	// Store event
	l.events = append(l.events, event)

	// Write to file if configured
	if l.file != nil {
		jsonBytes, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		if _, err := l.file.Write(append(jsonBytes, '\n')); err != nil {
			return fmt.Errorf("write event: %w", err)
		}
	}

	return nil
}

// LogStep logs a step execution event.
func (l *CanonicalLogger) LogStep(stepID, component, decision string, durationMs int64, input, output map[string]interface{}, err error) error {
	event := CanonicalLogEvent{
		StepID:        stepID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Actor:         l.actor,
		Component:     component,
		InputRedacted: redactInput(input),
		Output:        output,
		Decision:      decision,
		DurationMs:    durationMs,
	}

	if err != nil {
		event.Error = ErrorEnvelope{
			Present: true,
			Code:    errorCodeFromError(err),
			Message: err.Error(),
			Details: map[string]interface{}{},
		}
	} else {
		event.Error = ErrorEnvelope{
			Present: false,
			Code:    "",
			Message: "",
			Details: map[string]interface{}{},
		}
	}

	return l.LogEvent(event)
}

// NextStepID generates a unique step ID based on the step counter.
func (l *CanonicalLogger) NextStepID(prefix string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stepCounter++
	if prefix == "" {
		prefix = "step"
	}
	return fmt.Sprintf("%s-%d", prefix, l.stepCounter)
}

// Events returns all logged events.
func (l *CanonicalLogger) Events() []CanonicalLogEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]CanonicalLogEvent, len(l.events))
	copy(result, l.events)
	return result
}

// Close closes the logger and flushes any pending output.
func (l *CanonicalLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeFileLocked()
}

// DumpJSONL returns all events as JSONL string.
func (l *CanonicalLogger) DumpJSONL() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	var lines []string
	for _, event := range l.events {
		jsonBytes, _ := json.Marshal(event)
		lines = append(lines, string(jsonBytes))
	}
	return strings.Join(lines, "\n")
}

// ValidateAgainstSchema validates all events against the canonical schema.
func (l *CanonicalLogger) ValidateAgainstSchema() error {
	return validateCanonicalEventSet(l.Events())
}

// =============================================================================
// Helper Functions
// =============================================================================

// redactInput removes sensitive data from input maps.
func redactInput(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}

	// Keys to redact (case-insensitive)
	redactKeys := []string{
		"token", "access_token", "refresh_token", "session_token",
		"password", "secret", "api_key", "apikey", "key",
		"credential", "auth", "authorization",
	}

	result := make(map[string]interface{})
	for k, v := range input {
		lowerKey := strings.ToLower(k)
		redact := false
		for _, rk := range redactKeys {
			if strings.Contains(lowerKey, rk) {
				redact = true
				break
			}
		}

		if redact {
			result[k] = "[REDACTED]"
		} else if nested, ok := v.(map[string]interface{}); ok {
			result[k] = redactInput(nested)
		} else {
			result[k] = v
		}
	}
	return result
}

// errorCodeFromError extracts an error code from an error.
func errorCodeFromError(err error) string {
	if err == nil {
		return ""
	}

	// Check for common error patterns
	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	switch {
	case strings.Contains(errLower, "timeout"):
		return "ERR_TIMEOUT"
	case strings.Contains(errLower, "not found"):
		return "ERR_NOT_FOUND"
	case strings.Contains(errLower, "permission") || strings.Contains(errLower, "access denied"):
		return "ERR_PERMISSION"
	case strings.Contains(errLower, "already exists"):
		return "ERR_EXISTS"
	case strings.Contains(errLower, "invalid"):
		return "ERR_INVALID"
	case strings.Contains(errLower, "rate limit"):
		return "ERR_RATE_LIMIT"
	case strings.Contains(errLower, "connection"):
		return "ERR_CONNECTION"
	default:
		return "ERR_UNKNOWN"
	}
}

// validateCanonicalEvent validates a single event against the schema.
func validateCanonicalEvent(event CanonicalLogEvent) error {
	// Check required fields
	if event.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if event.ScenarioID == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if event.StepID == "" {
		return fmt.Errorf("step_id is required")
	}
	if event.Timestamp == "" {
		return fmt.Errorf("timestamp is required")
	}
	if event.Actor == "" {
		return fmt.Errorf("actor is required")
	}
	if event.Component == "" {
		return fmt.Errorf("component is required")
	}
	if event.Decision == "" {
		return fmt.Errorf("decision is required")
	}

	// Validate timestamp format
	if _, err := time.Parse(time.RFC3339, event.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}

	// Validate duration
	if event.DurationMs < 0 {
		return fmt.Errorf("duration_ms must be >= 0")
	}

	// Validate decision
	validDecisions := map[string]bool{
		DecisionPass:     true,
		DecisionContinue: true,
		DecisionRetry:    true,
		DecisionAbort:    true,
	}
	if !validDecisions[event.Decision] {
		return fmt.Errorf("invalid decision: %s", event.Decision)
	}

	// Validate error envelope consistency
	if !event.Error.Present {
		if event.Error.Code != "" || event.Error.Message != "" {
			return fmt.Errorf("error.code and error.message must be empty when error.present=false")
		}
	}

	// Check canonical payloads don't contain raw tokens or deny-pattern matches
	inputJSON, _ := json.Marshal(event.InputRedacted)
	outputJSON, _ := json.Marshal(event.Output)
	errorJSON, _ := json.Marshal(event.Error.Details)
	combined := string(inputJSON) + "\n" + string(outputJSON) + "\n" + string(errorJSON)
	if containsRawToken(combined) {
		return fmt.Errorf("canonical payload contains potential raw token")
	}
	if matchesCanonicalDenyPattern(combined) {
		return fmt.Errorf("canonical payload contains deny-pattern match")
	}

	return nil
}

var canonicalRedactionDenyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)api[_-]?key`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9]+`),
	regexp.MustCompile(`(?i)password`),
	regexp.MustCompile(`(?i)secret`),
	regexp.MustCompile(`(?i)authorization`),
}

var canonicalRawTokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:access|refresh|session)?[_-]?token\b["']?\s*[:=]\s*["']?[A-Za-z0-9._-]{16,}`),
	regexp.MustCompile(`\bpk-[A-Za-z0-9]{12,}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{12,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9._-]{8,}`),
}

func matchesCanonicalDenyPattern(blob string) bool {
	for _, re := range canonicalRedactionDenyPatterns {
		if re.MatchString(blob) {
			return true
		}
	}
	return false
}

func validateCanonicalEventSet(events []CanonicalLogEvent) error {
	if len(events) == 0 {
		return fmt.Errorf("no canonical log events")
	}
	for i, event := range events {
		if err := validateCanonicalEvent(event); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
	}
	if err := validateCanonicalRunIntegrity(events); err != nil {
		return err
	}
	return nil
}

func validateCanonicalRunIntegrity(events []CanonicalLogEvent) error {
	if len(events) == 0 {
		return fmt.Errorf("no canonical log events")
	}

	runID := events[0].RunID
	scenarioID := events[0].ScenarioID
	timestamps := make([]time.Time, len(events))

	type stepStamp struct {
		base string
		ts   time.Time
	}
	var starts []stepStamp
	var ends []stepStamp

	for i, event := range events {
		if event.RunID != runID {
			return fmt.Errorf("event %d: run_id drift detected (%q != %q)", i+1, event.RunID, runID)
		}
		if event.ScenarioID != scenarioID {
			return fmt.Errorf("event %d: scenario_id drift detected (%q != %q)", i+1, event.ScenarioID, scenarioID)
		}

		ts, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			return fmt.Errorf("event %d: invalid timestamp format: %w", i+1, err)
		}
		timestamps[i] = ts
		if i > 0 && ts.Before(timestamps[i-1]) {
			return fmt.Errorf("event %d: timestamp monotonicity violated", i+1)
		}

		switch {
		case strings.HasSuffix(event.StepID, "-start"):
			starts = append(starts, stepStamp{
				base: strings.TrimSuffix(event.StepID, "-start"),
				ts:   ts,
			})
		case strings.HasSuffix(event.StepID, "-end"):
			ends = append(ends, stepStamp{
				base: strings.TrimSuffix(event.StepID, "-end"),
				ts:   ts,
			})
		}
	}

	for _, start := range starts {
		matched := false
		for _, end := range ends {
			if end.base == start.base && !end.ts.Before(start.ts) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("step %q missing matching end event", start.base)
		}
	}

	return nil
}

// containsRawToken checks if a string contains raw token patterns.
func containsRawToken(s string) bool {
	if strings.Contains(s, "[REDACTED]") {
		return false
	}
	for _, re := range canonicalRawTokenPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// =============================================================================
// Scenario ID Helpers
// =============================================================================

// ScenarioIDFromTestName converts a test name to a stable scenario ID.
// Example: TestE2E_CompleteBackupActivateSwitchWorkflow -> complete-backup-activate-switch
func ScenarioIDFromTestName(testName string) string {
	// Remove Test prefix
	name := strings.TrimPrefix(testName, "Test")
	name = strings.TrimPrefix(name, "E2E_")
	name = strings.TrimPrefix(name, "_")

	// Convert CamelCase to kebab-case
	var result strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			result.WriteByte('-')
		}
		result.WriteRune(unicode.ToLower(r))
	}

	// Clean up multiple dashes
	s := result.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	return strings.Trim(s, "-")
}

// GenerateRunID generates a unique run ID with timestamp prefix.
func GenerateRunID() string {
	return fmt.Sprintf("run-%s-%s", time.Now().Format("20060102"), uuid.New().String()[:8])
}
