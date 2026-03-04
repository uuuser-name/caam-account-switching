package testutil

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewExtendedHarness(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	if h.TestHarness == nil {
		t.Error("TestHarness should be embedded")
	}
	if h.logBuffer == nil {
		t.Error("logBuffer should be initialized")
	}
	if h.metrics == nil {
		t.Error("metrics should be initialized")
	}
	if h.stepLogs == nil {
		t.Error("stepLogs should be initialized")
	}
	if h.startTime.IsZero() {
		t.Error("startTime should be set")
	}
}

func TestExtendedHarness_StartEndStep(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Start a step
	h.StartStep("setup", "Setting up test environment")

	if h.CurrentStep() != "setup" {
		t.Errorf("CurrentStep() = %q, want %q", h.CurrentStep(), "setup")
	}
	if h.stepCount != 1 {
		t.Errorf("stepCount = %d, want 1", h.stepCount)
	}

	// Small delay to ensure measurable duration
	time.Sleep(10 * time.Millisecond)

	// End the step
	h.EndStep("setup")

	if h.CurrentStep() != "" {
		t.Errorf("CurrentStep() = %q, want empty string after EndStep", h.CurrentStep())
	}

	// Check that step was recorded
	if len(h.stepLogs) != 1 {
		t.Fatalf("len(stepLogs) = %d, want 1", len(h.stepLogs))
	}

	step := h.stepLogs[0]
	if step.Name != "setup" {
		t.Errorf("step.Name = %q, want %q", step.Name, "setup")
	}
	if step.Description != "Setting up test environment" {
		t.Errorf("step.Description = %q, want %q", step.Description, "Setting up test environment")
	}
	if step.Duration < 10*time.Millisecond {
		t.Errorf("step.Duration = %v, want >= 10ms", step.Duration)
	}
	if !step.completed {
		t.Error("step should be marked as completed")
	}

	// Check metric was recorded
	metric := h.GetMetric("step.setup")
	if metric < 10*time.Millisecond {
		t.Errorf("metric 'step.setup' = %v, want >= 10ms", metric)
	}
}

func TestExtendedHarness_NestedSteps(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Start parent step
	h.StartStep("outer", "Outer step")
	if h.CurrentStep() != "outer" {
		t.Errorf("CurrentStep() = %q, want %q", h.CurrentStep(), "outer")
	}

	// Start nested step
	h.StartStep("inner", "Inner step")
	if h.CurrentStep() != "inner" {
		t.Errorf("CurrentStep() = %q, want %q", h.CurrentStep(), "inner")
	}
	if h.stepCount != 2 {
		t.Errorf("stepCount = %d, want 2", h.stepCount)
	}

	// End inner step
	h.EndStep("inner")
	if h.CurrentStep() != "outer" {
		t.Errorf("CurrentStep() = %q, want %q after ending inner", h.CurrentStep(), "outer")
	}

	// End outer step
	h.EndStep("outer")
	if h.CurrentStep() != "" {
		t.Errorf("CurrentStep() = %q, want empty string", h.CurrentStep())
	}

	// Verify nested structure
	if len(h.stepLogs) != 1 {
		t.Fatalf("len(stepLogs) = %d, want 1 (outer only at top level)", len(h.stepLogs))
	}

	outer := h.stepLogs[0]
	if len(outer.Nested) != 1 {
		t.Fatalf("len(outer.Nested) = %d, want 1", len(outer.Nested))
	}

	inner := outer.Nested[0]
	if inner.Name != "inner" {
		t.Errorf("inner.Name = %q, want %q", inner.Name, "inner")
	}
}

func TestExtendedHarness_Logging(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.LogInfo("info message", "key", "value")
	h.LogDebug("debug message")
	h.LogWarn("warning message", map[string]interface{}{"code": 123})
	h.LogError(errors.New("test error"), "during test operation")

	logs := h.DumpLogs()

	if !strings.Contains(logs, "info message") {
		t.Error("DumpLogs should contain 'info message'")
	}
	if !strings.Contains(logs, "debug message") {
		t.Error("DumpLogs should contain 'debug message'")
	}
	if !strings.Contains(logs, "warning message") {
		t.Error("DumpLogs should contain 'warning message'")
	}
	if !strings.Contains(logs, "test error") {
		t.Error("DumpLogs should contain 'test error'")
	}

	if h.errorCount != 1 {
		t.Errorf("errorCount = %d, want 1", h.errorCount)
	}
}

func TestExtendedHarness_Metrics(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Record a metric
	h.RecordMetric("api_call", 150*time.Millisecond)
	h.RecordMetric("db_query", 25*time.Millisecond)

	// Verify metrics
	if got := h.GetMetric("api_call"); got != 150*time.Millisecond {
		t.Errorf("GetMetric('api_call') = %v, want 150ms", got)
	}
	if got := h.GetMetric("db_query"); got != 25*time.Millisecond {
		t.Errorf("GetMetric('db_query') = %v, want 25ms", got)
	}
	if got := h.GetMetric("nonexistent"); got != 0 {
		t.Errorf("GetMetric('nonexistent') = %v, want 0", got)
	}

	// Get all metrics
	metrics := h.Metrics()
	if len(metrics) != 2 {
		t.Errorf("len(Metrics()) = %d, want 2", len(metrics))
	}
}

func TestExtendedHarness_TimeIt(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	duration := h.TimeIt("sleep_test", func() {
		time.Sleep(20 * time.Millisecond)
	})

	if duration < 20*time.Millisecond {
		t.Errorf("TimeIt duration = %v, want >= 20ms", duration)
	}

	metric := h.GetMetric("sleep_test")
	if metric < 20*time.Millisecond {
		t.Errorf("Recorded metric = %v, want >= 20ms", metric)
	}
}

func TestExtendedHarness_TimeStep(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	duration := h.TimeStep("timed_operation", "Testing timed step", func() {
		time.Sleep(15 * time.Millisecond)
	})

	if duration < 15*time.Millisecond {
		t.Errorf("TimeStep duration = %v, want >= 15ms", duration)
	}

	// Should have created step metrics
	if stepMetric := h.GetMetric("step.timed_operation"); stepMetric == 0 {
		t.Error("Expected step metric to be recorded")
	}

	// Step should be recorded and completed
	if len(h.stepLogs) != 1 {
		t.Fatalf("len(stepLogs) = %d, want 1", len(h.stepLogs))
	}
	if !h.stepLogs[0].completed {
		t.Error("Step should be completed after TimeStep")
	}
}

func TestExtendedHarness_Summary(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("step1", "First step")
	h.LogInfo("doing work")
	h.EndStep("step1")

	h.StartStep("step2", "Second step")
	h.RecordMetric("custom_metric", 100*time.Millisecond)
	h.EndStep("step2")

	summary := h.Summary()

	// Check that summary contains expected sections
	if !strings.Contains(summary, "TEST SUMMARY") {
		t.Error("Summary should contain 'TEST SUMMARY'")
	}
	if !strings.Contains(summary, "OVERALL STATISTICS") {
		t.Error("Summary should contain 'OVERALL STATISTICS'")
	}
	if !strings.Contains(summary, "STEP TIMELINE") {
		t.Error("Summary should contain 'STEP TIMELINE'")
	}
	if !strings.Contains(summary, "METRICS") {
		t.Error("Summary should contain 'METRICS'")
	}
	if !strings.Contains(summary, "step1") {
		t.Error("Summary should contain 'step1'")
	}
	if !strings.Contains(summary, "step2") {
		t.Error("Summary should contain 'step2'")
	}
	if !strings.Contains(summary, "custom_metric") {
		t.Error("Summary should contain 'custom_metric'")
	}
}

func TestExtendedHarness_SummaryJSON(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("json_step", "Testing JSON summary")
	h.RecordMetric("test_metric", 50*time.Millisecond)
	h.EndStep("json_step")

	jsonSummary := h.SummaryJSON()

	if !strings.Contains(jsonSummary, "test_name") {
		t.Error("SummaryJSON should contain 'test_name'")
	}
	if !strings.Contains(jsonSummary, "step_count") {
		t.Error("SummaryJSON should contain 'step_count'")
	}
	if !strings.Contains(jsonSummary, "json_step") {
		t.Error("SummaryJSON should contain 'json_step'")
	}
	if !strings.Contains(jsonSummary, "test_metric") {
		t.Error("SummaryJSON should contain 'test_metric'")
	}
}

func TestExtendedHarness_DumpLogsJSON(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.LogInfo("first entry")
	h.LogInfo("second entry")

	jsonLogs := h.DumpLogsJSON()

	// Should be a valid JSON array
	if !strings.HasPrefix(jsonLogs, "[") {
		t.Errorf("DumpLogsJSON should start with '[', got: %q", jsonLogs[:min(50, len(jsonLogs))])
	}
	if !strings.Contains(jsonLogs, "first entry") {
		t.Error("DumpLogsJSON should contain 'first entry'")
	}
	if !strings.Contains(jsonLogs, "second entry") {
		t.Error("DumpLogsJSON should contain 'second entry'")
	}
}

func TestExtendedHarness_CloseCompletesOpenSteps(t *testing.T) {
	h := NewExtendedHarness(t)

	h.StartStep("unclosed_step", "This step won't be explicitly closed")

	// Close should complete any open steps
	h.Close()

	// Verify step was completed
	if len(h.stepLogs) != 1 {
		t.Fatalf("len(stepLogs) = %d, want 1", len(h.stepLogs))
	}
	if !h.stepLogs[0].completed {
		t.Error("Open step should be completed by Close()")
	}
}

func TestExtendedHarness_EndStepMismatch(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("correct_name", "")

	// Ending with wrong name should still work (logs warning)
	h.EndStep("wrong_name")

	// Step should still be completed
	if len(h.stepStack) != 0 {
		t.Error("Step stack should be empty after EndStep")
	}
}

func TestExtendedHarness_EndStepNoActive(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Ending step when none active should be a no-op
	h.EndStep("nonexistent")

	// Should not panic, step count should be 0
	if h.stepCount != 0 {
		t.Errorf("stepCount = %d, want 0", h.stepCount)
	}
}

func TestExtendedHarness_BackwardsCompatibility(t *testing.T) {
	// Verify that ExtendedHarness can be used like TestHarness
	h := NewExtendedHarness(t)
	defer h.Close()

	// Use TestHarness methods
	h.SetEnv("TEST_VAR", "test_value")

	subDir := h.SubDir("test_subdir")
	if subDir == "" {
		t.Error("SubDir should return non-empty path")
	}

	filePath := h.WriteFile("test.txt", "test content")
	if !h.FileExists(filePath) {
		t.Error("FileExists should return true for created file")
	}

	if !h.FileContains(filePath, "test content") {
		t.Error("FileContains should return true for matching content")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// =============================================================================
// Tests for JSON Export and CI Artifact Support
// =============================================================================

func TestExtendedHarness_ExportJSON(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Add some test data
	h.StartStep("export_test", "Testing JSON export")
	h.LogInfo("test log entry")
	h.RecordMetric("test_metric", 100*time.Millisecond)
	h.EndStep("export_test")

	// Export to temp file
	tmpFile := h.WriteFile("export.json", "")
	err := h.ExportJSON(tmpFile)
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	// Verify file was created and contains expected data
	if !h.FileExists(tmpFile) {
		t.Error("Export file should exist")
	}
	if !h.FileContains(tmpFile, "test_name") {
		t.Error("Export should contain 'test_name'")
	}
	if !h.FileContains(tmpFile, "export_test") {
		t.Error("Export should contain step name 'export_test'")
	}
	if !h.FileContains(tmpFile, "test_metric") {
		t.Error("Export should contain metric 'test_metric'")
	}
	if !h.FileContains(tmpFile, "log_entries") {
		t.Error("Export should contain 'log_entries'")
	}
}

func TestExtendedHarness_GetReport(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("report_test", "Testing report generation")
	h.LogInfo("test log")
	h.RecordMetric("custom", 50*time.Millisecond)
	h.EndStep("report_test")

	report := h.GetReport()

	if report == nil {
		t.Fatal("GetReport should not return nil")
	}
	if report.TestName != t.Name() {
		t.Errorf("TestName = %q, want %q", report.TestName, t.Name())
	}
	if report.StepCount != 1 {
		t.Errorf("StepCount = %d, want 1", report.StepCount)
	}
	if len(report.Steps) != 1 {
		t.Errorf("len(Steps) = %d, want 1", len(report.Steps))
	}
	if len(report.Metrics) == 0 {
		t.Error("Metrics should not be empty")
	}
	if _, ok := report.Metrics["custom"]; !ok {
		t.Error("Metrics should contain 'custom'")
	}
	if report.DurationMs < 0 {
		t.Error("DurationMs should be non-negative")
	}
}

func TestExtendedHarness_ReportMetricCategories(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("category_test", "")
	h.EndStep("category_test")
	h.RecordMetric("custom_metric", 100*time.Millisecond)

	report := h.GetReport()

	// Check step metrics have "step" category
	stepMetric, ok := report.Metrics["step.category_test"]
	if !ok {
		t.Fatal("Missing step.category_test metric")
	}
	if stepMetric.Category != "step" {
		t.Errorf("step metric category = %q, want 'step'", stepMetric.Category)
	}

	// Check custom metrics have "custom" category
	customMetric, ok := report.Metrics["custom_metric"]
	if !ok {
		t.Fatal("Missing custom_metric")
	}
	if customMetric.Category != "custom" {
		t.Errorf("custom metric category = %q, want 'custom'", customMetric.Category)
	}
}

func TestExtendedHarness_RelevantEnvVars(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Set some relevant env vars
	h.SetEnv("CAAM_DEBUG", "true")
	h.SetEnv("CI", "true")

	report := h.GetReport()

	// Since test didn't fail, FailureContext should be nil
	if report.FailureContext != nil {
		// If for some reason it's not nil, check the env vars
		if report.FailureContext.EnvVars == nil {
			t.Error("EnvVars should not be nil in failure context")
		}
	}
}

// =============================================================================
// Tests for Performance Baseline Comparison
// =============================================================================

func TestExtendedHarness_SaveAndLoadBaseline(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Record some metrics
	h.RecordMetric("api_latency", 100*time.Millisecond)
	h.RecordMetric("db_query", 50*time.Millisecond)

	// Save baseline
	baselinePath := h.WriteFile("baseline.json", "")
	err := h.SaveBaseline(baselinePath)
	if err != nil {
		t.Fatalf("SaveBaseline failed: %v", err)
	}

	// Load baseline
	baseline, err := LoadBaseline(baselinePath)
	if err != nil {
		t.Fatalf("LoadBaseline failed: %v", err)
	}

	if baseline.TestName != t.Name() {
		t.Errorf("TestName = %q, want %q", baseline.TestName, t.Name())
	}
	if len(baseline.Metrics) != 2 {
		t.Errorf("len(Metrics) = %d, want 2", len(baseline.Metrics))
	}
	if baseline.Metrics["api_latency"].ValueMs != 100 {
		t.Errorf("api_latency = %d, want 100", baseline.Metrics["api_latency"].ValueMs)
	}
}

func TestExtendedHarness_CompareToBaseline_NoRegression(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Create baseline with same values
	baseline := &BaselineMetrics{
		TestName:    t.Name(),
		RecordedAt:  time.Now().Add(-24 * time.Hour),
		TotalTimeMs: 100,
		Metrics: map[string]MetricValue{
			"api_latency": {Value: "100ms", ValueMs: 100},
		},
	}

	// Record similar metrics (within threshold)
	h.RecordMetric("api_latency", 105*time.Millisecond) // 5% increase, within 20% threshold

	comparison := h.CompareToBaseline(baseline, 0.2) // 20% threshold

	if comparison == nil {
		t.Fatal("CompareToBaseline should not return nil")
	}
	if comparison.IsRegression {
		t.Error("Should not be flagged as regression within threshold")
	}
	if len(comparison.Regressions) > 0 {
		t.Errorf("Should have no regressions, got %d", len(comparison.Regressions))
	}
}

func TestExtendedHarness_CompareToBaseline_WithRegression(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Create baseline
	baseline := &BaselineMetrics{
		TestName:    t.Name(),
		RecordedAt:  time.Now().Add(-24 * time.Hour),
		TotalTimeMs: 100,
		Metrics: map[string]MetricValue{
			"api_latency": {Value: "100ms", ValueMs: 100},
		},
	}

	// Record significantly worse metrics (50% increase)
	h.RecordMetric("api_latency", 150*time.Millisecond)

	comparison := h.CompareToBaseline(baseline, 0.2) // 20% threshold

	if comparison == nil {
		t.Fatal("CompareToBaseline should not return nil")
	}
	if !comparison.IsRegression {
		t.Error("Should be flagged as regression")
	}
	if len(comparison.Regressions) != 1 {
		t.Errorf("Should have 1 regression, got %d", len(comparison.Regressions))
	}
	if comparison.Regressions[0].Name != "api_latency" {
		t.Errorf("Regression name = %q, want 'api_latency'", comparison.Regressions[0].Name)
	}
}

func TestExtendedHarness_CompareToBaseline_WithImprovement(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Create baseline
	baseline := &BaselineMetrics{
		TestName:    t.Name(),
		RecordedAt:  time.Now().Add(-24 * time.Hour),
		TotalTimeMs: 100,
		Metrics: map[string]MetricValue{
			"api_latency": {Value: "100ms", ValueMs: 100},
		},
	}

	// Record significantly better metrics (50% improvement)
	h.RecordMetric("api_latency", 50*time.Millisecond)

	comparison := h.CompareToBaseline(baseline, 0.2) // 20% threshold

	if comparison == nil {
		t.Fatal("CompareToBaseline should not return nil")
	}
	if comparison.IsRegression {
		t.Error("Should not be flagged as regression for improvement")
	}
	if len(comparison.Improvements) != 1 {
		t.Errorf("Should have 1 improvement, got %d", len(comparison.Improvements))
	}
}

func TestExtendedHarness_CompareToBaseline_NilBaseline(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	comparison := h.CompareToBaseline(nil, 0.2)
	if comparison != nil {
		t.Error("CompareToBaseline should return nil for nil baseline")
	}
}

func TestExtendedHarness_CompareToBaseline_EmptyBaseline(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	baseline := &BaselineMetrics{
		TestName: t.Name(),
		Metrics:  map[string]MetricValue{},
	}

	comparison := h.CompareToBaseline(baseline, 0.2)
	if comparison != nil {
		t.Error("CompareToBaseline should return nil for empty baseline metrics")
	}
}

func TestLoadBaseline_NonExistent(t *testing.T) {
	_, err := LoadBaseline("/nonexistent/path/baseline.json")
	if err == nil {
		t.Error("LoadBaseline should fail for non-existent file")
	}
}

func TestExtendedHarness_LastLogLines(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Log more than 10 entries
	for i := 0; i < 15; i++ {
		h.LogInfo("log entry", "index", i)
	}

	// Use buildReportUnsafe to get last log lines
	h.mu.Lock()
	lines := h.lastLogLinesUnsafe(10)
	h.mu.Unlock()

	if len(lines) != 10 {
		t.Errorf("lastLogLinesUnsafe(10) returned %d lines, want 10", len(lines))
	}
}

func TestExtendedHarness_LastLogLines_LessThanN(t *testing.T) {
	h := NewExtendedHarness(t)
	defer h.Close()

	// Log only 5 entries
	for i := 0; i < 5; i++ {
		h.LogInfo("log entry", "index", i)
	}

	h.mu.Lock()
	lines := h.lastLogLinesUnsafe(10)
	h.mu.Unlock()

	if len(lines) != 5 {
		t.Errorf("lastLogLinesUnsafe(10) with 5 entries returned %d lines, want 5", len(lines))
	}
}
