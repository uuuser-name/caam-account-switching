// Package testutil provides E2E test infrastructure with detailed logging.
//
// This file tests the TimelineMerger engine for deterministic merging of
// multi-source test logs into coherent chronological timelines.
package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Test Data Helpers
// =============================================================================

func createTestEvent(runID, scenarioID, stepID, timestamp, actor, component, decision string, durationMs int64) *CanonicalLogEvent {
	return &CanonicalLogEvent{
		RunID:         runID,
		ScenarioID:    scenarioID,
		StepID:        stepID,
		Timestamp:     timestamp,
		Actor:         actor,
		Component:     component,
		InputRedacted: map[string]interface{}{},
		Output:        map[string]interface{}{},
		Decision:      decision,
		DurationMs:    durationMs,
		Error:         ErrorEnvelope{Present: false, Code: "", Message: "", Details: map[string]interface{}{}},
	}
}

func createTestEventWithError(runID, scenarioID, stepID, timestamp string, errMsg string) *CanonicalLogEvent {
	return &CanonicalLogEvent{
		RunID:         runID,
		ScenarioID:    scenarioID,
		StepID:        stepID,
		Timestamp:     timestamp,
		Actor:         "ci",
		Component:     ComponentTest,
		InputRedacted: map[string]interface{}{},
		Output:        map[string]interface{}{},
		Decision:      DecisionAbort,
		DurationMs:    100,
		Error:         ErrorEnvelope{Present: true, Code: "ERR_TEST", Message: errMsg, Details: map[string]interface{}{}},
	}
}

// =============================================================================
// Basic Merge Tests
// =============================================================================

func TestTimelineMerger_EmptyInput(t *testing.T) {
	merger := NewTimelineMerger()

	if err := merger.Merge(); err != nil {
		t.Errorf("Unexpected error on empty merge: %v", err)
	}

	if count := len(merger.Events()); count != 0 {
		t.Errorf("Expected 0 events, got %d", count)
	}

	if count := len(merger.RunIDs()); count != 0 {
		t.Errorf("Expected 0 runs, got %d", count)
	}
}

func TestTimelineMerger_SingleEvent(t *testing.T) {
	merger := NewTimelineMerger()
	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-test", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if count := len(merger.Events()); count != 1 {
		t.Errorf("Expected 1 event, got %d", count)
	}

	events := merger.ByRunID("run-001")
	if len(events) != 1 {
		t.Fatalf("Expected 1 event for run-001, got %d", len(events))
	}

	if events[0].RunID != "run-001" {
		t.Errorf("Expected RunID=run-001, got %s", events[0].RunID)
	}
}

func TestTimelineMerger_MultipleEventsSameRun(t *testing.T) {
	merger := NewTimelineMerger()
	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 150),
		createTestEvent("run-001", "scenario-1", "step-3", "2024-01-01T10:00:02Z", "ci", ComponentTest, DecisionPass, 200),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	events := merger.ByRunID("run-001")
	if len(events) != 3 {
		t.Errorf("Expected 3 events for run-001, got %d", len(events))
	}

	// Verify chronological order
	for i := 1; i < len(events); i++ {
		prev, _ := time.Parse(time.RFC3339, events[i-1].Timestamp)
		curr, _ := time.Parse(time.RFC3339, events[i].Timestamp)
		if curr.Before(prev) {
			t.Errorf("Events not in chronological order at index %d", i)
		}
	}
}

// =============================================================================
// Determinism Tests
// =============================================================================

func TestTimelineMerger_DeterministicOutput(t *testing.T) {
	opts := DefaultMergeOptions()
	opts.Deduplicate = false // Test raw sorting determinism

	// Create events with identical timestamps (requires secondary sort)
	baseTime := "2024-01-01T10:00:00Z"
	events := []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-z", baseTime, "ci", ComponentVault, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-a", baseTime, "ci", ComponentBackup, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-m", baseTime, "ci", ComponentDaemon, DecisionPass, 100),
	}

	// Run merge multiple times to verify determinism
	var firstOutput string
	for i := 0; i < 10; i++ {
		merger := NewTimelineMerger()
		merger.AddEvents("test-source", events)
		if err := merger.MergeWithOptions(opts); err != nil {
			t.Fatalf("MergeWithOptions() error = %v", err)
		}

		output := merger.DumpJSONL()
		if i == 0 {
			firstOutput = output
		} else if output != firstOutput {
			t.Errorf("Merge output is not deterministic on iteration %d", i)
		}
	}
}

func TestTimelineMerger_DeterministicWithShuffledInput(t *testing.T) {
	opts := DefaultMergeOptions()
	opts.Deduplicate = false

	// Create many events
	var events []*CanonicalLogEvent
	for i := 0; i < 50; i++ {
		ts := time.Date(2024, 1, 1, 10, 0, i, 0, time.UTC).Format(time.RFC3339)
		events = append(events, createTestEvent(
			"run-001",
			"scenario-1",
			fmt.Sprintf("step-%d", i),
			ts,
			"ci",
			ComponentTest,
			DecisionPass,
			int64(i*10),
		))
	}

	// Get reference output
	merger1 := NewTimelineMerger()
	merger1.AddEvents("test-source", events)
	if err := merger1.MergeWithOptions(opts); err != nil {
		t.Fatalf("MergeWithOptions() error = %v", err)
	}
	referenceOutput := merger1.DumpJSONL()

	// Shuffle and merge multiple times
	for iter := 0; iter < 10; iter++ {
		// Shuffle events
		shuffled := make([]*CanonicalLogEvent, len(events))
		copy(shuffled, events)
		for i := len(shuffled) - 1; i > 0; i-- {
			j := (i * 7) % (i + 1) // Deterministic shuffle
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}

		merger := NewTimelineMerger()
		merger.AddEvents("test-source", shuffled)
		if err := merger.MergeWithOptions(opts); err != nil {
			t.Fatalf("MergeWithOptions() error = %v", err)
		}
		output := merger.DumpJSONL()

		if output != referenceOutput {
			t.Errorf("Output differs after shuffling input on iteration %d", iter)
		}
	}
}

// =============================================================================
// Multi-Source Merge Tests
// =============================================================================

func TestTimelineMerger_MergeMultipleSources(t *testing.T) {
	// Source 1: Run 1 events
	merger := NewTimelineMerger()
	merger.AddEvents("source-1", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})

	// Source 2: Run 2 events
	merger.AddEvents("source-2", []*CanonicalLogEvent{
		createTestEvent("run-002", "scenario-2", "step-1", "2024-01-01T11:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-002", "scenario-2", "step-2", "2024-01-01T11:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})

	// Source 3: More run 1 events
	merger.AddEvents("source-3", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-3", "2024-01-01T10:00:02Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Verify run IDs
	runIDs := merger.RunIDs()
	if len(runIDs) != 2 {
		t.Errorf("Expected 2 run IDs, got %d: %v", len(runIDs), runIDs)
	}

	// Verify scenario IDs
	scenarioIDs := merger.ScenarioIDs()
	if len(scenarioIDs) != 2 {
		t.Errorf("Expected 2 scenario IDs, got %d: %v", len(scenarioIDs), scenarioIDs)
	}

	// Verify run-001 has 3 events
	events1 := merger.ByRunID("run-001")
	if len(events1) != 3 {
		t.Errorf("Expected 3 events in run-001, got %d", len(events1))
	}

	// Verify run-002 has 2 events
	events2 := merger.ByRunID("run-002")
	if len(events2) != 2 {
		t.Errorf("Expected 2 events in run-002, got %d", len(events2))
	}
}

func TestTimelineMerger_AddJSONLFile(t *testing.T) {
	// Create temp directory with JSONL files
	tmpDir := t.TempDir()

	// Create test file
	file1 := filepath.Join(tmpDir, "events.jsonl")
	events := []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	}
	writeJSONLFileHelper(t, file1, events)

	merger := NewTimelineMerger()
	if err := merger.AddJSONLFile("file-source", file1); err != nil {
		t.Fatalf("AddJSONLFile failed: %v", err)
	}

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if len(merger.Events()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(merger.Events()))
	}
}

func writeJSONLFileHelper(t *testing.T, path string, events []*CanonicalLogEvent) {
	var buf bytes.Buffer
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("Failed to marshal event: %v", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
}

// =============================================================================
// Deduplication Tests
// =============================================================================

func TestTimelineMerger_Deduplication(t *testing.T) {
	opts := DefaultMergeOptions()
	opts.Deduplicate = true

	merger := NewTimelineMerger()

	// Create duplicate events (same step_id, same timestamp)
	ts := "2024-01-01T10:00:00Z"
	merger.AddEvents("source-1", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", ts, "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-1", ts, "ci", ComponentTest, DecisionPass, 100), // Exact duplicate
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.MergeWithOptions(opts); err != nil {
		t.Fatalf("MergeWithOptions() error = %v", err)
	}

	// Should have 2 events (duplicates removed)
	if len(merger.Events()) != 2 {
		t.Errorf("Expected 2 events after deduplication, got %d", len(merger.Events()))
	}
}

func TestTimelineMerger_DeduplicationDisabled(t *testing.T) {
	opts := DefaultMergeOptions()
	opts.Deduplicate = false

	merger := NewTimelineMerger()

	ts := "2024-01-01T10:00:00Z"
	merger.AddEvents("source-1", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", ts, "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-1", ts, "ci", ComponentTest, DecisionPass, 100), // Duplicate
	})

	if err := merger.MergeWithOptions(opts); err != nil {
		t.Fatalf("MergeWithOptions() error = %v", err)
	}

	// Should have 2 events (dedup disabled)
	if len(merger.Events()) != 2 {
		t.Errorf("Expected 2 events with dedup disabled, got %d", len(merger.Events()))
	}
}

// =============================================================================
// Scenario-based Merge Tests
// =============================================================================

func TestTimelineMerger_ByScenarioID(t *testing.T) {
	merger := NewTimelineMerger()

	// Multiple runs with same scenario
	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "backup-activate", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-002", "backup-activate", "step-1", "2024-01-01T11:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-003", "backup-activate", "step-1", "2024-01-01T12:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "switch-profile", "step-1", "2024-01-01T10:30:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Query by scenario
	events := merger.ByScenarioID("backup-activate")
	if len(events) != 3 {
		t.Errorf("Expected 3 events for backup-activate scenario, got %d", len(events))
	}

	// Verify all events have the correct scenario_id
	for _, e := range events {
		if e.ScenarioID != "backup-activate" {
			t.Errorf("Unexpected scenario_id: %s", e.ScenarioID)
		}
	}
}

func TestTimelineMerger_ByRunScenario(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-2", "step-1", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-002", "scenario-1", "step-1", "2024-01-01T11:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Query by run + scenario
	events := merger.ByRunScenario("run-001", "scenario-1")
	if len(events) != 1 {
		t.Errorf("Expected 1 event for run-001/scenario-1, got %d", len(events))
	}

	if events[0].RunID != "run-001" {
		t.Errorf("Expected RunID=run-001, got %s", events[0].RunID)
	}
	if events[0].ScenarioID != "scenario-1" {
		t.Errorf("Expected ScenarioID=scenario-1, got %s", events[0].ScenarioID)
	}
}

// =============================================================================
// Groups Tests
// =============================================================================

func TestTimelineMerger_Groups(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionContinue, 100),
		createTestEvent("run-001", "scenario-2", "step-1", "2024-01-01T11:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEventWithError("run-001", "scenario-2", "step-2", "2024-01-01T11:00:01Z", "test error"),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	groups := merger.Groups()
	if len(groups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(groups))
	}

	// Find the scenario-2 group
	var scenario2Group *TimelineGroup
	for _, g := range groups {
		if g.ScenarioID == "scenario-2" {
			scenario2Group = g
			break
		}
	}

	if scenario2Group == nil {
		t.Fatal("Expected scenario-2 group")
	}

	// Check error collection
	if len(scenario2Group.Errors) != 1 {
		t.Errorf("Expected 1 error in scenario-2 group, got %d", len(scenario2Group.Errors))
	}

	// Check duration calculation
	if scenario2Group.Duration == 0 {
		t.Error("Expected non-zero duration")
	}
}

func TestTimelineMerger_GroupStatistics(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionContinue, 100),
		createTestEvent("run-001", "scenario-1", "step-3", "2024-01-01T10:00:02Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	groups := merger.Groups()
	if len(groups) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(groups))
	}

	group := groups[0]

	// Check step count
	if group.StepCount != 3 {
		t.Errorf("Expected StepCount=3, got %d", group.StepCount)
	}

	// Check decision counts
	if group.Decisions[DecisionPass] != 2 {
		t.Errorf("Expected 2 pass decisions, got %d", group.Decisions[DecisionPass])
	}
	if group.Decisions[DecisionContinue] != 1 {
		t.Errorf("Expected 1 continue decision, got %d", group.Decisions[DecisionContinue])
	}
}

// =============================================================================
// Output Tests
// =============================================================================

func TestTimelineMerger_DumpJSONL(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	output := merger.DumpJSONL()

	// Verify output is valid JSONL
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 JSONL lines, got %d", len(lines))
	}

	for i, line := range lines {
		var event MergedTimelineEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestTimelineMerger_DumpJSON(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	output := merger.DumpJSON()

	// Verify output is valid JSON
	var parsed []*MergedTimelineEvent
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}

	if len(parsed) != 1 {
		t.Errorf("Expected 1 parsed event, got %d", len(parsed))
	}
}

func TestTimelineMerger_WriteJSONL(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Write to temp file
	tmpFile := filepath.Join(t.TempDir(), "timeline.jsonl")
	if err := merger.WriteJSONL(tmpFile); err != nil {
		t.Fatalf("WriteJSONL failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Errorf("File does not exist: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	var event MergedTimelineEvent
	if err := json.Unmarshal(content, &event); err != nil {
		t.Errorf("File content is not valid JSON: %v", err)
	}
}

// =============================================================================
// Validation Tests
// =============================================================================

func TestTimelineMerger_Validate(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if err := merger.Validate(); err != nil {
		t.Errorf("Validation failed: %v", err)
	}
}

func TestTimelineMerger_ValidateInvalidEvent(t *testing.T) {
	// Create invalid event manually (missing required fields)
	invalidEvent := &CanonicalLogEvent{
		RunID: "run-001",
		// Missing scenario_id
	}

	merger := NewTimelineMerger()
	merger.AddEvents("test-source", []*CanonicalLogEvent{invalidEvent})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if err := merger.Validate(); err == nil {
		t.Error("Expected validation error for invalid event")
	}
}

// =============================================================================
// Source Provenance Tests
// =============================================================================

func TestTimelineMerger_SourceProvenance(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("source-a", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})
	merger.AddEvents("source-b", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	events := merger.Events()
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	// Check source tracking
	if events[0].Source != "source-a" {
		t.Errorf("Expected source 'source-a', got '%s'", events[0].Source)
	}
	if events[1].Source != "source-b" {
		t.Errorf("Expected source 'source-b', got '%s'", events[1].Source)
	}
}

func TestTimelineMerger_MergeIndex(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-3", "2024-01-01T10:00:02Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	events := merger.Events()
	for i, e := range events {
		if e.MergeIndex != i {
			t.Errorf("Event %d has wrong MergeIndex: %d", i, e.MergeIndex)
		}
	}
}

// =============================================================================
// Convenience Function Tests
// =============================================================================

func TestMergeTimelines(t *testing.T) {
	sources := []TimelineSource{
		{
			Name: "source-1",
			Events: []*CanonicalLogEvent{
				createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
			},
		},
		{
			Name: "source-2",
			Events: []*CanonicalLogEvent{
				createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
			},
		},
	}

	merger, err := MergeTimelines(sources...)
	if err != nil {
		t.Fatalf("MergeTimelines failed: %v", err)
	}

	if len(merger.Events()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(merger.Events()))
	}
}

func TestMergeJSONLFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tmpDir, "events1.jsonl")
	file2 := filepath.Join(tmpDir, "events2.jsonl")

	events1 := []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	}
	events2 := []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	}

	writeJSONLFileHelper(t, file1, events1)
	writeJSONLFileHelper(t, file2, events2)

	// Merge using convenience function
	files := map[string]string{
		"source-1": file1,
		"source-2": file2,
	}

	merger, err := MergeJSONLFiles(files)
	if err != nil {
		t.Fatalf("MergeJSONLFiles failed: %v", err)
	}

	if len(merger.Events()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(merger.Events()))
	}
}

// =============================================================================
// Summary Tests
// =============================================================================

func TestTimelineMerger_Summary(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("source-1", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})
	merger.AddEvents("source-2", []*CanonicalLogEvent{
		createTestEvent("run-002", "scenario-2", "step-1", "2024-01-01T11:00:00Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	summary := merger.Summary()

	// Verify summary contains key information
	if !strings.Contains(summary, "TIMELINE MERGER SUMMARY") {
		t.Error("Summary missing title")
	}
	if !strings.Contains(summary, "source-1") {
		t.Error("Summary missing source-1")
	}
	if !strings.Contains(summary, "source-2") {
		t.Error("Summary missing source-2")
	}
	if !strings.Contains(summary, "run-001") {
		t.Error("Summary missing run-001")
	}
}

// =============================================================================
// IsDeterministic Test
// =============================================================================

func TestTimelineMerger_IsDeterministic(t *testing.T) {
	merger := NewTimelineMerger()

	merger.AddEvents("test-source", []*CanonicalLogEvent{
		createTestEvent("run-001", "scenario-1", "step-1", "2024-01-01T10:00:00Z", "ci", ComponentTest, DecisionPass, 100),
		createTestEvent("run-001", "scenario-1", "step-2", "2024-01-01T10:00:01Z", "ci", ComponentTest, DecisionPass, 100),
	})

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if !merger.IsDeterministic() {
		t.Error("Expected deterministic merge")
	}
}

// =============================================================================
// AddJSONLReader Test
// =============================================================================

func TestTimelineMerger_AddJSONLReader(t *testing.T) {
	jsonl := `{"run_id":"run-001","scenario_id":"scenario-1","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false,"code":"","message":"","details":{}}}
{"run_id":"run-001","scenario_id":"scenario-1","step_id":"step-2","timestamp":"2024-01-01T10:00:01Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false,"code":"","message":"","details":{}}}
`
	merger := NewTimelineMerger()
	if err := merger.AddJSONLReader("reader-source", strings.NewReader(jsonl)); err != nil {
		t.Fatalf("AddJSONLReader failed: %v", err)
	}

	if err := merger.Merge(); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if len(merger.Events()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(merger.Events()))
	}
}
