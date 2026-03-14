package timeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Test Helpers
// =============================================================================

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func makeEntry(runID, scenarioID, stepID, timestamp, decision string, durationMs int64) *TimelineEntry {
	t, _ := time.Parse(time.RFC3339, timestamp)
	return &TimelineEntry{
		RunID:           runID,
		ScenarioID:      scenarioID,
		StepID:          stepID,
		Timestamp:       timestamp,
		ParsedTimestamp: t,
		Decision:        decision,
		DurationMs:      durationMs,
		InputRedacted:   map[string]interface{}{},
		Output:          map[string]interface{}{},
		Error:           ErrorInfo{Present: false},
	}
}

func makeEntryWithError(runID, scenarioID, stepID, timestamp string, errCode, errMsg string) *TimelineEntry {
	e := makeEntry(runID, scenarioID, stepID, timestamp, "abort", 100)
	e.Error = ErrorInfo{
		Present: true,
		Code:    errCode,
		Message: errMsg,
		Details: map[string]interface{}{},
	}
	return e
}

// =============================================================================
// Basic Merge Tests
// =============================================================================

func TestMerger_Empty(t *testing.T) {
	m := NewMerger()
	timelines := m.Merge()

	if len(timelines) != 0 {
		t.Errorf("expected 0 timelines, got %d", len(timelines))
	}

	if m.EntryCount() != 0 {
		t.Errorf("expected 0 entries, got %d", m.EntryCount())
	}
}

func TestMerger_SingleEntry(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
	})

	timelines := m.Merge()
	if len(timelines) != 1 {
		t.Fatalf("expected 1 timeline, got %d", len(timelines))
	}

	tl := timelines[0]
	if tl.RunID != "run-001" {
		t.Errorf("expected run_id run-001, got %s", tl.RunID)
	}
	if tl.ScenarioID != "scenario-a" {
		t.Errorf("expected scenario_id scenario-a, got %s", tl.ScenarioID)
	}
	if len(tl.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(tl.Entries))
	}
	if tl.Metadata.TotalEntries != 1 {
		t.Errorf("expected TotalEntries=1, got %d", tl.Metadata.TotalEntries)
	}
}

func TestMerger_MultipleEntriesSameRun(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
		makeEntry("run-001", "scenario-a", "step-3", "2024-01-01T10:00:10Z", "pass", 150),
	})

	timelines := m.Merge()
	if len(timelines) != 1 {
		t.Fatalf("expected 1 timeline, got %d", len(timelines))
	}

	tl := timelines[0]
	if len(tl.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(tl.Entries))
	}
	if tl.Metadata.StepCount != 3 {
		t.Errorf("expected StepCount=3, got %d", tl.Metadata.StepCount)
	}
}

func TestMerger_MultipleRuns(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-002", "scenario-b", "step-1", "2024-01-01T11:00:00Z", "pass", 150),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
	})

	timelines := m.Merge()
	if len(timelines) != 2 {
		t.Fatalf("expected 2 timelines, got %d", len(timelines))
	}

	// Timelines should be sorted by run_id|scenario_id
	if timelines[0].RunID != "run-001" {
		t.Errorf("expected first timeline run_id=run-001, got %s", timelines[0].RunID)
	}
	if timelines[1].RunID != "run-002" {
		t.Errorf("expected second timeline run_id=run-002, got %s", timelines[1].RunID)
	}
}

// =============================================================================
// Sorting Tests
// =============================================================================

func TestMerger_ChronologicalSort(t *testing.T) {
	m := NewMerger()
	// Add entries out of order
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-3", "2024-01-01T10:00:20Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:10Z", "pass", 100),
	})

	timelines := m.Merge()
	tl := timelines[0]

	// Should be sorted by timestamp
	expectedOrder := []string{"step-1", "step-2", "step-3"}
	for i, expected := range expectedOrder {
		if tl.Entries[i].StepID != expected {
			t.Errorf("entry %d: expected step_id=%s, got %s", i, expected, tl.Entries[i].StepID)
		}
	}
}

func TestMerger_SameTimestampSortByStepID(t *testing.T) {
	m := NewMerger()
	// Entries with same timestamp should be sorted by step_id
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-c", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-a", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-b", "2024-01-01T10:00:00Z", "pass", 100),
	})

	timelines := m.Merge()
	tl := timelines[0]

	expectedOrder := []string{"step-a", "step-b", "step-c"}
	for i, expected := range expectedOrder {
		if tl.Entries[i].StepID != expected {
			t.Errorf("entry %d: expected step_id=%s, got %s", i, expected, tl.Entries[i].StepID)
		}
	}
}

// =============================================================================
// Determinism Tests
// =============================================================================

func TestMerger_Deterministic(t *testing.T) {
	// Run the same merge multiple times with shuffled input
	entries := []*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
		makeEntry("run-001", "scenario-a", "step-3", "2024-01-01T10:00:10Z", "pass", 150),
		makeEntry("run-002", "scenario-b", "step-1", "2024-01-01T11:00:00Z", "pass", 300),
		makeEntry("run-002", "scenario-b", "step-2", "2024-01-01T11:00:05Z", "pass", 250),
	}

	// Compute first result
	m1 := NewMerger()
	m1.AddEntries(entries)
	result1 := m1.Merge()
	hash1 := result1[0].Metadata.Hash

	// Compute second result (same input)
	m2 := NewMerger()
	m2.AddEntries(entries)
	result2 := m2.Merge()
	hash2 := result2[0].Metadata.Hash

	// Hashes should be identical
	if hash1 != hash2 {
		t.Errorf("deterministic hash mismatch: %s != %s", hash1, hash2)
	}

	// Output should be byte-identical
	jsonl1 := result1[0].ToJSONL()
	jsonl2 := result2[0].ToJSONL()
	if jsonl1 != jsonl2 {
		t.Errorf("deterministic output mismatch:\n%s\n---\n%s", jsonl1, jsonl2)
	}
}

func TestMerger_DeterministicWithDifferentInputOrder(t *testing.T) {
	// Add entries in different orders, verify same output
	entriesA := []*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
		makeEntry("run-001", "scenario-a", "step-3", "2024-01-01T10:00:10Z", "pass", 150),
	}

	entriesB := []*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-3", "2024-01-01T10:00:10Z", "pass", 150),
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
	}

	m1 := NewMerger()
	m1.AddEntries(entriesA)
	result1 := m1.Merge()

	m2 := NewMerger()
	m2.AddEntries(entriesB)
	result2 := m2.Merge()

	if result1[0].Metadata.Hash != result2[0].Metadata.Hash {
		t.Errorf("hash mismatch with different input order: %s != %s",
			result1[0].Metadata.Hash, result2[0].Metadata.Hash)
	}

	if result1[0].ToJSONL() != result2[0].ToJSONL() {
		t.Errorf("output mismatch with different input order")
	}
}

// =============================================================================
// JSONL Parsing Tests
// =============================================================================

func TestMerger_AddReader(t *testing.T) {
	jsonl := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false,"code":"","message":"","details":{}}}
{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:00:05Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":200,"error":{"present":false,"code":"","message":"","details":{}}}
`

	m := NewMerger()
	count, err := m.AddReader("test.jsonl", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("AddReader failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}

	timelines := m.Merge()
	if len(timelines) != 1 {
		t.Fatalf("expected 1 timeline, got %d", len(timelines))
	}
	if len(timelines[0].Entries) != 2 {
		t.Errorf("expected 2 entries in timeline, got %d", len(timelines[0].Entries))
	}
}

func TestMerger_AddReaderWithErrors(t *testing.T) {
	// Valid entry, invalid entry, valid entry
	jsonl := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}
this is not valid json
{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:00:05Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":200,"error":{"present":false}}
`

	m := NewMerger()
	count, err := m.AddReader("test.jsonl", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("AddReader failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}

	parseErrors := m.ParseErrors()
	if len(parseErrors) != 1 {
		t.Errorf("expected 1 parse error, got %d", len(parseErrors))
	}
}

func TestMerger_AddFile(t *testing.T) {
	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.jsonl")

	content := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}
`
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	m := NewMerger()
	count, err := m.AddFile(tmpFile)
	if err != nil {
		t.Fatalf("AddFile failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}

	if len(m.SourceFiles()) != 1 {
		t.Errorf("expected 1 source file, got %d", len(m.SourceFiles()))
	}
}

// =============================================================================
// JSONL Export Tests
// =============================================================================

func TestMergedTimeline_ToJSONL(t *testing.T) {
	tl := &MergedTimeline{
		RunID:      "run-001",
		ScenarioID: "scenario-a",
		Entries: []*TimelineEntry{
			makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
			makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
		},
	}

	jsonl := tl.ToJSONL()
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")

	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var entry TimelineEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestMergedTimeline_WriteJSONLTo(t *testing.T) {
	tl := &MergedTimeline{
		RunID:      "run-001",
		ScenarioID: "scenario-a",
		Entries: []*TimelineEntry{
			makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		},
	}

	var buf bytes.Buffer
	if err := tl.WriteJSONLTo(&buf); err != nil {
		t.Fatalf("WriteJSONLTo failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
}

// =============================================================================
// Metadata Tests
// =============================================================================

func TestMergeMetadata_Calculations(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
		makeEntryWithError("run-001", "scenario-a", "step-3", "2024-01-01T10:00:10Z", "ERR_TIMEOUT", "timeout"),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:07Z", "pass", 50), // duplicate step_id
	})

	timelines := m.Merge()
	tl := timelines[0]

	if tl.Metadata.TotalEntries != 4 {
		t.Errorf("expected TotalEntries=4, got %d", tl.Metadata.TotalEntries)
	}

	if tl.Metadata.StepCount != 3 {
		t.Errorf("expected StepCount=3 (unique steps), got %d", tl.Metadata.StepCount)
	}

	if tl.Metadata.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1, got %d", tl.Metadata.ErrorCount)
	}

	if tl.Metadata.StartTime != "2024-01-01T10:00:00Z" {
		t.Errorf("unexpected StartTime: %s", tl.Metadata.StartTime)
	}

	if tl.Metadata.EndTime != "2024-01-01T10:00:10Z" {
		t.Errorf("unexpected EndTime: %s", tl.Metadata.EndTime)
	}

	// Duration should be 10 seconds = 10000ms
	if tl.Metadata.DurationMs != 10000 {
		t.Errorf("expected DurationMs=10000, got %d", tl.Metadata.DurationMs)
	}
}

// =============================================================================
// Convenience Function Tests
// =============================================================================

func TestMergeFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two test files
	file1 := filepath.Join(tmpDir, "test1.jsonl")
	file2 := filepath.Join(tmpDir, "test2.jsonl")

	content1 := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}
`
	content2 := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:00:05Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":200,"error":{"present":false}}
`

	if err := os.WriteFile(file1, []byte(content1), 0644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(content2), 0644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	timelines, err := MergeFiles(file1, file2)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	if len(timelines) != 1 {
		t.Fatalf("expected 1 timeline, got %d", len(timelines))
	}

	if len(timelines[0].Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(timelines[0].Entries))
	}

	if len(timelines[0].Metadata.SourceFiles) != 2 {
		t.Errorf("expected 2 source files in metadata, got %d", len(timelines[0].Metadata.SourceFiles))
	}
}

func TestMergeReaders(t *testing.T) {
	r1 := strings.NewReader(`{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}
`)
	r2 := strings.NewReader(`{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:00:05Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":200,"error":{"present":false}}
`)

	timelines, err := MergeReaders([]string{"file1.jsonl", "file2.jsonl"}, []io.Reader{r1, r2})
	if err != nil {
		t.Fatalf("MergeReaders failed: %v", err)
	}

	if len(timelines) != 1 {
		t.Fatalf("expected 1 timeline, got %d", len(timelines))
	}

	if len(timelines[0].Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(timelines[0].Entries))
	}
}

// =============================================================================
// Reset Tests
// =============================================================================

func TestMerger_Reset(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
	})

	if m.EntryCount() != 1 {
		t.Errorf("expected 1 entry before reset, got %d", m.EntryCount())
	}

	m.Reset()

	if m.EntryCount() != 0 {
		t.Errorf("expected 0 entries after reset, got %d", m.EntryCount())
	}

	if len(m.SourceFiles()) != 0 {
		t.Errorf("expected 0 source files after reset, got %d", len(m.SourceFiles()))
	}
}

// =============================================================================
// MergeSingle Tests
// =============================================================================

func TestMerger_MergeSingle(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
	})

	tl := m.MergeSingle()
	if tl == nil {
		t.Fatal("expected non-nil timeline")
	}

	if tl.RunID != "run-001" {
		t.Errorf("expected run_id=run-001, got %s", tl.RunID)
	}

	if len(tl.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(tl.Entries))
	}
}

func TestMerger_MergeSingleEmpty(t *testing.T) {
	m := NewMerger()
	tl := m.MergeSingle()

	if tl != nil {
		t.Errorf("expected nil timeline for empty merger, got %+v", tl)
	}
}

// =============================================================================
// Source File Tracking Tests
// =============================================================================

func TestMerger_SourceFileTracking(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "test1.jsonl")
	file2 := filepath.Join(tmpDir, "test2.jsonl")

	content := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}
`
	if err := os.WriteFile(file1, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", file2, err)
	}

	m := NewMerger()
	if _, err := m.AddFile(file1); err != nil {
		t.Fatalf("AddFile(%s) error = %v", file1, err)
	}
	if _, err := m.AddFile(file2); err != nil {
		t.Fatalf("AddFile(%s) error = %v", file2, err)
	}

	timelines := m.Merge()
	tl := timelines[0]

	// Check source file is tracked in entries
	for _, entry := range tl.Entries {
		if entry.SourceFile == "" {
			t.Error("expected source file to be tracked in entry")
		}
		if entry.LineNumber == 0 {
			t.Error("expected line number to be tracked in entry")
		}
	}

	// Check source files in metadata
	if len(tl.Metadata.SourceFiles) != 2 {
		t.Errorf("expected 2 source files in metadata, got %d", len(tl.Metadata.SourceFiles))
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestMerger_EmptyLinesInJSONL(t *testing.T) {
	jsonl := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}

{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:00:05Z","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":200,"error":{"present":false}}

`

	m := NewMerger()
	count, err := m.AddReader("test.jsonl", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("AddReader failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 entries (skipping empty lines), got %d", count)
	}
}

func TestMerger_NilEntries(t *testing.T) {
	m := NewMerger()
	m.AddEntries([]*TimelineEntry{
		makeEntry("run-001", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "pass", 100),
		nil, // should be ignored
		makeEntry("run-001", "scenario-a", "step-2", "2024-01-01T10:00:05Z", "pass", 200),
	})

	timelines := m.Merge()
	if len(timelines[0].Entries) != 2 {
		t.Errorf("expected 2 entries (nil ignored), got %d", len(timelines[0].Entries))
	}
}

func TestMerger_InvalidTimestamp(t *testing.T) {
	// Entry with invalid timestamp - should still parse, just won't have ParsedTimestamp
	jsonl := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"invalid","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false}}
`

	m := NewMerger()
	count, err := m.AddReader("test.jsonl", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("AddReader failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}

	if m.entries[0].Timestamp != "invalid" {
		t.Errorf("expected timestamp to be preserved as 'invalid', got %s", m.entries[0].Timestamp)
	}
}

func TestMerger_MissingFields(t *testing.T) {
	// Entry with missing optional fields
	jsonl := `{"run_id":"run-001","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","decision":"pass","duration_ms":100}
`

	m := NewMerger()
	count, err := m.AddReader("test.jsonl", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("AddReader failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}

	entry := m.entries[0]
	if entry.RunID != "run-001" {
		t.Errorf("expected run_id=run-001, got %s", entry.RunID)
	}
	// Missing fields should be zero values
	if entry.InputRedacted != nil {
		t.Error("expected InputRedacted to be nil when missing")
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkMerger_Merge(b *testing.B) {
	// Create a large number of entries
	entries := make([]*TimelineEntry, 1000)
	for i := 0; i < 1000; i++ {
		ts := time.Date(2024, 1, 1, 10, 0, i, 0, time.UTC)
		entries[i] = &TimelineEntry{
			RunID:           "run-001",
			ScenarioID:      "scenario-a",
			StepID:          fmt.Sprintf("step-%d", i),
			Timestamp:       ts.Format(time.RFC3339),
			ParsedTimestamp: ts,
			Decision:        "pass",
			DurationMs:      int64(i * 10),
			InputRedacted:   map[string]interface{}{},
			Output:          map[string]interface{}{},
			Error:           ErrorInfo{Present: false},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewMerger()
		m.AddEntries(entries)
		m.Merge()
	}
}

func BenchmarkMerger_AddReader(b *testing.B) {
	// Create JSONL content
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		ts := time.Date(2024, 1, 1, 10, 0, i, 0, time.UTC)
		entry := map[string]interface{}{
			"run_id":         "run-001",
			"scenario_id":    "scenario-a",
			"step_id":        fmt.Sprintf("step-%d", i),
			"timestamp":      ts.Format(time.RFC3339),
			"actor":          "ci",
			"component":      "test",
			"input_redacted": map[string]interface{}{},
			"output":         map[string]interface{}{},
			"decision":       "pass",
			"duration_ms":    i * 10,
			"error":          map[string]interface{}{"present": false},
		}
		jsonBytes, _ := json.Marshal(entry)
		sb.Write(jsonBytes)
		sb.WriteString("\n")
	}
	jsonl := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewMerger()
		if _, err := m.AddReader("test.jsonl", strings.NewReader(jsonl)); err != nil {
			b.Fatalf("AddReader() error = %v", err)
		}
	}
}
