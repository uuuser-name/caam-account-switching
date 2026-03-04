package timelinemerge

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Test Event Creation Helpers
// =============================================================================

func testEvent(runID, scenarioID, stepID, timestamp string) *Event {
	t, _ := time.Parse(time.RFC3339, timestamp)
	return &Event{
		RunID:            runID,
		ScenarioID:       scenarioID,
		StepID:           stepID,
		Timestamp:        timestamp,
		ParsedTimestamp:  t,
		Component:        "test",
		Actor:            "ci",
		Decision:         "pass",
		DurationMs:       100,
		InputRedacted:    map[string]interface{}{},
		Output:           map[string]interface{}{},
		Source:           "test-source",
	}
}

func testEventWithSource(runID, scenarioID, stepID, timestamp, source string) *Event {
	e := testEvent(runID, scenarioID, stepID, timestamp)
	e.Source = source
	return e
}

// =============================================================================
// Deterministic Merge Tests
// =============================================================================

func TestDeterministicMerge_SameInputsSameOutput(t *testing.T) {
	// Create identical input sets multiple times
	inputs := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
		testEvent("run-1", "scenario-b", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-2", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
	}

	// Run merge multiple times and compare JSONL output (which is deterministic)
	var results []string
	for i := 0; i < 5; i++ {
		merger := NewDeterministicMerger()
		for _, event := range inputs {
			merger.Add(event)
		}
		result := merger.Merge()

		// Use JSONL output for comparison (deterministic serialization)
		var jsonlLines []string
		keys := make([]TimelineKey, 0, len(result.Timelines))
		for k := range result.Timelines {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].RunID != keys[j].RunID {
				return keys[i].RunID < keys[j].RunID
			}
			return keys[i].ScenarioID < keys[j].ScenarioID
		})
		for _, key := range keys {
			timeline := result.Timelines[key]
			for _, event := range timeline.Events {
				data, err := json.Marshal(event)
				if err != nil {
					t.Fatalf("Failed to marshal event: %v", err)
				}
				jsonlLines = append(jsonlLines, string(data))
			}
		}
		results = append(results, strings.Join(jsonlLines, "\n"))
	}

	// All results should be identical
	for i := 1; i < len(results); i++ {
		if results[0] != results[i] {
			t.Errorf("Results differ between runs:\nRun 0: %s\nRun %d: %s", results[0], i, results[i])
		}
	}
}

func TestDeterministicMerge_OrderIndependence(t *testing.T) {
	// Same events added in different orders should produce same result
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
		testEvent("run-1", "scenario-b", "step-1", "2024-01-01T10:00:30Z"),
	}

	// Order 1: original
	merger1 := NewDeterministicMerger()
	for _, e := range events {
		merger1.Add(e)
	}
	result1 := merger1.Merge()

	// Order 2: reversed
	merger2 := NewDeterministicMerger()
	for i := len(events) - 1; i >= 0; i-- {
		merger2.Add(events[i])
	}
	result2 := merger2.Merge()

	// Order 3: shuffled
	merger3 := NewDeterministicMerger()
	merger3.Add(events[1])
	merger3.Add(events[0])
	merger3.Add(events[2])
	result3 := merger3.Merge()

	// All should produce identical timeline ordering
	if len(result1.Timelines) != len(result2.Timelines) || len(result1.Timelines) != len(result3.Timelines) {
		t.Errorf("Different number of timelines: %d, %d, %d", len(result1.Timelines), len(result2.Timelines), len(result3.Timelines))
	}

	// Check event order in each timeline
	for key := range result1.Timelines {
		t1 := result1.Timelines[key]
		t2 := result2.Timelines[key]
		t3 := result3.Timelines[key]

		for i := range t1.Events {
			if t1.Events[i].StepID != t2.Events[i].StepID || t1.Events[i].StepID != t3.Events[i].StepID {
				t.Errorf("Event order differs at index %d: %s vs %s vs %s", i, t1.Events[i].StepID, t2.Events[i].StepID, t3.Events[i].StepID)
			}
		}
	}
}

func TestDeterministicMerge_SortOrder(t *testing.T) {
	// Events with same timestamp should be sorted by step_id
	merger := NewDeterministicMerger()
	merger.Add(testEvent("run-1", "scenario-a", "step-3", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-1", "scenario-a", "step-0", "2024-01-01T10:01:00Z"))

	result := merger.Merge()

	key := TimelineKey{RunID: "run-1", ScenarioID: "scenario-a"}
	timeline := result.Timelines[key]
	if timeline == nil {
		t.Fatal("Expected timeline not found")
	}

	// Events should be sorted: step-0 (later timestamp) should be last
	// Others with same timestamp should be sorted by step_id: step-1, step-2, step-3
	expected := []string{"step-1", "step-2", "step-3", "step-0"}
	if len(timeline.Events) != len(expected) {
		t.Fatalf("Expected %d events, got %d", len(expected), len(timeline.Events))
	}

	for i, e := range timeline.Events {
		if e.StepID != expected[i] {
			t.Errorf("Event %d: expected step_id %s, got %s", i, expected[i], e.StepID)
		}
	}
}

// =============================================================================
// Deduplication Tests
// =============================================================================

func TestDeduplication_ExactDuplicates(t *testing.T) {
	merger := NewDeterministicMerger()
	event := testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z")

	// Add same event multiple times
	merger.Add(event)
	merger.Add(event)
	merger.Add(event)

	result := merger.Merge()

	if result.Stats.TotalEvents != 1 {
		t.Errorf("Expected 1 event after dedup, got %d", result.Stats.TotalEvents)
	}
	if result.Stats.DuplicateEvents != 2 {
		t.Errorf("Expected 2 duplicates counted, got %d", result.Stats.DuplicateEvents)
	}
}

func TestDeduplication_SameKeyDifferentSource(t *testing.T) {
	merger := NewDeterministicMerger()

	// Same event from different sources
	merger.Add(testEventWithSource("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "source-a"))
	merger.Add(testEventWithSource("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "source-b"))

	result := merger.Merge()

	// Should be deduplicated (same key)
	if result.Stats.TotalEvents != 1 {
		t.Errorf("Expected 1 event after dedup, got %d", result.Stats.TotalEvents)
	}
}

func TestDeduplication_DifferentKeys(t *testing.T) {
	merger := NewDeterministicMerger()

	// Different events (different step_id)
	merger.Add(testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:00:00Z"))

	result := merger.Merge()

	// Both should be kept
	if result.Stats.TotalEvents != 2 {
		t.Errorf("Expected 2 events, got %d", result.Stats.TotalEvents)
	}
}

// =============================================================================
// Timeline Grouping Tests
// =============================================================================

func TestTimelineGrouping_ByRunID(t *testing.T) {
	merger := NewDeterministicMerger()

	merger.Add(testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-1", "scenario-b", "step-1", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-2", "scenario-a", "step-1", "2024-01-01T10:00:00Z"))

	result := merger.Merge()

	// Should have 3 timelines (different run+scenario combinations)
	if len(result.Timelines) != 3 {
		t.Errorf("Expected 3 timelines, got %d", len(result.Timelines))
	}

	// Verify each timeline exists
	keys := []TimelineKey{
		{RunID: "run-1", ScenarioID: "scenario-a"},
		{RunID: "run-1", ScenarioID: "scenario-b"},
		{RunID: "run-2", ScenarioID: "scenario-a"},
	}

	for _, key := range keys {
		if result.Timelines[key] == nil {
			t.Errorf("Missing timeline for key: %+v", key)
		}
	}
}

func TestTimelineGrouping_MultipleEventsPerTimeline(t *testing.T) {
	merger := NewDeterministicMerger()

	// Add multiple events to same timeline
	merger.Add(testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"))
	merger.Add(testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"))
	merger.Add(testEvent("run-1", "scenario-a", "step-3", "2024-01-01T10:02:00Z"))

	result := merger.Merge()

	key := TimelineKey{RunID: "run-1", ScenarioID: "scenario-a"}
	timeline := result.Timelines[key]

	if timeline == nil {
		t.Fatal("Expected timeline not found")
	}

	if len(timeline.Events) != 3 {
		t.Errorf("Expected 3 events in timeline, got %d", len(timeline.Events))
	}

	// Check time bounds
	if !timeline.StartTime.Equal(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Unexpected start time: %v", timeline.StartTime)
	}
	if !timeline.EndTime.Equal(time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC)) {
		t.Errorf("Unexpected end time: %v", timeline.EndTime)
	}
}

// =============================================================================
// JSONL Parsing Tests
// =============================================================================

func TestParseEventFromJSON(t *testing.T) {
	jsonStr := `{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z","actor":"ci","component":"test","decision":"pass","duration_ms":100}`

	event, err := ParseEventFromJSON([]byte(jsonStr))
	if err != nil {
		t.Fatalf("Failed to parse event: %v", err)
	}

	if event.RunID != "run-1" {
		t.Errorf("Expected run_id 'run-1', got '%s'", event.RunID)
	}
	if event.ScenarioID != "scenario-a" {
		t.Errorf("Expected scenario_id 'scenario-a', got '%s'", event.ScenarioID)
	}
	if event.StepID != "step-1" {
		t.Errorf("Expected step_id 'step-1', got '%s'", event.StepID)
	}
	if event.Timestamp != "2024-01-01T10:00:00Z" {
		t.Errorf("Expected timestamp '2024-01-01T10:00:00Z', got '%s'", event.Timestamp)
	}
}

func TestParseEvent_MissingRequiredField(t *testing.T) {
	testCases := []struct {
		name string
		json string
	}{
		{"missing run_id", `{"scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z"}`},
		{"missing scenario_id", `{"run_id":"run-1","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z"}`},
		{"missing step_id", `{"run_id":"run-1","scenario_id":"scenario-a","timestamp":"2024-01-01T10:00:00Z"}`},
		{"missing timestamp", `{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-1"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEventFromJSON([]byte(tc.json))
			if err == nil {
				t.Error("Expected error for missing required field")
			}
		})
	}
}

func TestParseJSONLReader(t *testing.T) {
	jsonl := `{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z"}
{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:01:00Z"}
{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-3","timestamp":"2024-01-01T10:02:00Z"}`

	events, err := ParseJSONLReader(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("Failed to parse JSONL: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}
}

func TestWriteJSONL(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
	}

	var buf bytes.Buffer
	err := WriteJSONL(&buf, events)
	if err != nil {
		t.Fatalf("Failed to write JSONL: %v", err)
	}

	// Parse back and verify
	parsed, err := ParseJSONLReader(&buf)
	if err != nil {
		t.Fatalf("Failed to parse written JSONL: %v", err)
	}

	if len(parsed) != len(events) {
		t.Errorf("Round-trip failed: expected %d events, got %d", len(events), len(parsed))
	}
}

// =============================================================================
// Source Tests
// =============================================================================

func TestSource_EventsSource(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
	}

	source := EventsSource("test-source", events)

	if source.Name != "test-source" {
		t.Errorf("Expected source name 'test-source', got '%s'", source.Name)
	}

	if len(source.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(source.Events))
	}

	// Check that source was set on events
	for _, e := range source.Events {
		if e.Source != "test-source" {
			t.Errorf("Event source not set: expected 'test-source', got '%s'", e.Source)
		}
	}
}

func TestSource_ReaderSource(t *testing.T) {
	jsonl := `{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z"}
{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-2","timestamp":"2024-01-01T10:01:00Z"}`

	source, err := ReaderSource("test-reader", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	if source.Name != "test-reader" {
		t.Errorf("Expected source name 'test-reader', got '%s'", source.Name)
	}

	if len(source.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(source.Events))
	}
}

// =============================================================================
// Utility Function Tests
// =============================================================================

func TestFilterByRunID(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-b", "step-1", "2024-01-01T10:01:00Z"),
		testEvent("run-2", "scenario-a", "step-1", "2024-01-01T10:02:00Z"),
	}

	filtered := FilterByRunID(events, "run-1")

	if len(filtered) != 2 {
		t.Errorf("Expected 2 events for run-1, got %d", len(filtered))
	}
}

func TestFilterByScenarioID(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-b", "step-1", "2024-01-01T10:01:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:02:00Z"),
	}

	filtered := FilterByScenarioID(events, "scenario-a")

	if len(filtered) != 2 {
		t.Errorf("Expected 2 events for scenario-a, got %d", len(filtered))
	}
}

func TestSortEvents(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-3", "2024-01-01T10:02:00Z"),
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
		testEvent("run-1", "scenario-b", "step-0", "2024-01-01T10:00:00Z"),
	}

	SortEvents(events)

	// First event should be earliest timestamp
	if events[0].Timestamp != "2024-01-01T10:00:00Z" {
		t.Errorf("First event should have timestamp 2024-01-01T10:00:00Z, got %s", events[0].Timestamp)
	}

	// Within same timestamp, should be sorted by scenario_id
	if events[0].ScenarioID != "scenario-a" && events[1].ScenarioID != "scenario-b" {
		t.Errorf("Events with same timestamp not sorted by scenario_id")
	}
}

func TestDeduplicateEvents(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"), // duplicate
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
	}

	deduped, duplicates := DeduplicateEvents(events)

	if len(deduped) != 2 {
		t.Errorf("Expected 2 unique events, got %d", len(deduped))
	}

	if duplicates != 1 {
		t.Errorf("Expected 1 duplicate, got %d", duplicates)
	}
}

func TestStats(t *testing.T) {
	events := []*Event{
		testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
		testEvent("run-1", "scenario-b", "step-1", "2024-01-01T10:02:00Z"),
		testEvent("run-2", "scenario-a", "step-1", "2024-01-01T10:03:00Z"),
	}

	stats := Stats(events)

	if stats.TotalEvents != 4 {
		t.Errorf("Expected 4 total events, got %d", stats.TotalEvents)
	}

	if stats.ByRunID["run-1"] != 3 {
		t.Errorf("Expected 3 events in run-1, got %d", stats.ByRunID["run-1"])
	}

	if stats.ByRunID["run-2"] != 1 {
		t.Errorf("Expected 1 event in run-2, got %d", stats.ByRunID["run-2"])
	}

	if stats.ByScenario["scenario-a"] != 3 {
		t.Errorf("Expected 3 events in scenario-a, got %d", stats.ByScenario["scenario-a"])
	}
}

// =============================================================================
// Engine Tests
// =============================================================================

func TestEngine_MergeFiles(t *testing.T) {
	// This test would require actual files, so we'll test the merge sources path instead
	engine := NewEngine()

	sources := []*Source{
		EventsSource("source-a", []*Event{
			testEventWithSource("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z", "source-a"),
		}),
		EventsSource("source-b", []*Event{
			testEventWithSource("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z", "source-b"),
			testEventWithSource("run-1", "scenario-b", "step-1", "2024-01-01T10:00:00Z", "source-b"),
		}),
	}

	result := engine.MergeSources(sources)

	if result.Stats.TotalSources != 2 {
		t.Errorf("Expected 2 sources, got %d", result.Stats.TotalSources)
	}

	if len(result.Timelines) != 2 {
		t.Errorf("Expected 2 timelines, got %d", len(result.Timelines))
	}
}

// =============================================================================
// Timeline Output Tests
// =============================================================================

func TestTimeline_ToJSONL(t *testing.T) {
	timeline := &Timeline{
		RunID:      "run-1",
		ScenarioID: "scenario-a",
		Events: []*Event{
			testEvent("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
			testEvent("run-1", "scenario-a", "step-2", "2024-01-01T10:01:00Z"),
		},
	}

	jsonl := timeline.ToJSONL()

	// Should have two lines
	lines := strings.Count(jsonl, "\n")
	if lines != 1 { // 2 events = 1 newline between them
		t.Errorf("Expected 1 newline in JSONL, got %d", lines)
	}

	// Should be parseable
	parsed, err := ParseJSONLReader(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("Failed to parse JSONL: %v", err)
	}

	if len(parsed) != 2 {
		t.Errorf("Expected 2 parsed events, got %d", len(parsed))
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestMerge_EmptyInput(t *testing.T) {
	merger := NewDeterministicMerger()
	result := merger.Merge()

	if result.Stats.TotalEvents != 0 {
		t.Errorf("Expected 0 events, got %d", result.Stats.TotalEvents)
	}

	if len(result.Timelines) != 0 {
		t.Errorf("Expected 0 timelines, got %d", len(result.Timelines))
	}
}

func TestMerge_NilEvent(t *testing.T) {
	merger := NewDeterministicMerger()
	merger.Add(nil) // Should not panic

	result := merger.Merge()

	if result.Stats.TotalEvents != 0 {
		t.Errorf("Expected 0 events after adding nil, got %d", result.Stats.TotalEvents)
	}
}

func TestParseEvent_InvalidTimestamp(t *testing.T) {
	jsonStr := `{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-1","timestamp":"invalid"}`

	_, err := ParseEventFromJSON([]byte(jsonStr))
	if err == nil {
		t.Error("Expected error for invalid timestamp")
	}
}

func TestTimeline_WriteJSONL_Empty(t *testing.T) {
	timeline := &Timeline{
		RunID:      "run-1",
		ScenarioID: "scenario-a",
		Events:     []*Event{},
	}

	var buf bytes.Buffer
	err := timeline.WriteJSONL(&buf)
	if err != nil {
		t.Errorf("WriteJSONL on empty timeline should not error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("Expected empty output, got: %s", buf.String())
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkDeterministicMerge_1000Events(b *testing.B) {
	events := make([]*Event, 1000)
	for i := 0; i < 1000; i++ {
		ts := time.Date(2024, 1, 1, 10, i/60, i%60, 0, time.UTC).Format(time.RFC3339)
		events[i] = testEvent("run-1", "scenario-a", "step-"+string(rune(i)), ts)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		merger := NewDeterministicMerger()
		for _, e := range events {
			merger.Add(e)
		}
		merger.Merge()
	}
}

func BenchmarkParseJSONL_100Lines(b *testing.B) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, `{"run_id":"run-1","scenario_id":"scenario-a","step_id":"step-1","timestamp":"2024-01-01T10:00:00Z"}`)
	}
	jsonl := strings.Join(lines, "\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseJSONLReader(strings.NewReader(jsonl))
	}
}
