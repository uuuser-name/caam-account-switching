// Package timelinemerge provides deterministic merging of multi-source test logs
// into coherent chronological timelines, keyed by run_id and scenario_id.
//
// The merger accepts canonical log events from multiple sources (test files,
// providers, scenarios) and produces a unified, chronologically sorted timeline
// with deterministic output for repeated inputs.
package timelinemerge

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Core Types
// =============================================================================

// Event represents a single canonical log event in the timeline.
// This is the internal representation used for merging.
type Event struct {
	// RunID is the stable identifier for the test run
	RunID string `json:"run_id"`

	// ScenarioID is the stable scenario key
	ScenarioID string `json:"scenario_id"`

	// StepID is the step identifier within the scenario
	StepID string `json:"step_id"`

	// Timestamp is the RFC3339 UTC timestamp
	Timestamp string `json:"timestamp"`

	// ParsedTimestamp is the parsed timestamp for sorting
	ParsedTimestamp time.Time `json:"-"`

	// Source identifies where this event came from (file, provider, etc.)
	Source string `json:"source,omitempty"`

	// Component is the logical component touched by this step
	Component string `json:"component"`

	// Actor is the human or automation actor
	Actor string `json:"actor"`

	// Decision is the action decision (pass, retry, abort, continue)
	Decision string `json:"decision"`

	// DurationMs is the execution latency in milliseconds
	DurationMs int64 `json:"duration_ms"`

	// InputRedacted contains the input summary with secrets removed
	InputRedacted map[string]interface{} `json:"input_redacted"`

	// Output contains the output summary
	Output map[string]interface{} `json:"output"`

	// Error contains error information if present
	Error ErrorInfo `json:"error"`

	// RawJSON preserves the original JSON for passthrough
	RawJSON json.RawMessage `json:"-"`
}

// ErrorInfo represents error details in an event.
type ErrorInfo struct {
	Present bool                   `json:"present"`
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details"`
}

// Timeline represents a merged timeline of events.
type Timeline struct {
	// RunID is the run this timeline belongs to
	RunID string `json:"run_id"`

	// ScenarioID is the scenario this timeline belongs to
	ScenarioID string `json:"scenario_id"`

	// Events is the chronologically sorted list of events
	Events []*Event `json:"events"`

	// SourceStats tracks event counts by source
	SourceStats map[string]int `json:"source_stats,omitempty"`

	// StartTime is the earliest event timestamp
	StartTime time.Time `json:"start_time"`

	// EndTime is the latest event timestamp
	EndTime time.Time `json:"end_time"`

	// TotalEvents is the count of events in the timeline
	TotalEvents int `json:"total_events"`

	// TotalDurationMs is the sum of all event durations
	TotalDurationMs int64 `json:"total_duration_ms"`
}

// TimelineKey identifies a unique timeline by run and scenario.
type TimelineKey struct {
	RunID      string
	ScenarioID string
}

// MergeResult contains the results of merging multiple sources.
type MergeResult struct {
	// Timelines is a map of timeline key to merged timeline
	Timelines map[TimelineKey]*Timeline

	// Stats contains merge statistics
	Stats MergeStats

	// Errors contains any non-fatal errors encountered during merge
	Errors []MergeError
}

// MergeStats contains statistics about the merge operation.
type MergeStats struct {
	// TotalSources is the number of input sources
	TotalSources int `json:"total_sources"`

	// TotalEvents is the total events across all timelines
	TotalEvents int `json:"total_events"`

	// TotalTimelines is the number of unique timelines
	TotalTimelines int `json:"total_timelines"`

	// DuplicateEvents is the count of duplicate events that were deduplicated
	DuplicateEvents int `json:"duplicate_events"`

	// ParseErrors is the count of events that failed to parse
	ParseErrors int `json:"parse_errors"`

	// MergeDurationMs is how long the merge took
	MergeDurationMs int64 `json:"merge_duration_ms"`
}

// MergeError represents a non-fatal error during merge.
type MergeError struct {
	Source  string `json:"source"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

// =============================================================================
// Source Types
// =============================================================================

// Source represents a source of events (file, reader, etc.)
type Source struct {
	// Name identifies this source
	Name string

	// Events are the events from this source
	Events []*Event
}

// FileSource creates a source from a JSONL file path.
func FileSource(name, path string) (*Source, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", path, err)
	}
	defer file.Close()

	return ReaderSource(name, file)
}

// ReaderSource creates a source from an io.Reader containing JSONL data.
func ReaderSource(name string, r io.Reader) (*Source, error) {
	source := &Source{
		Name:   name,
		Events: make([]*Event, 0),
	}

	decoder := json.NewDecoder(r)
	lineNum := 0

	for {
		lineNum++
		var rawEvent map[string]interface{}
		if err := decoder.Decode(&rawEvent); err != nil {
			if err == io.EOF {
				break
			}
			// Skip unparseable lines but continue
			continue
		}

		event, err := ParseEvent(rawEvent)
		if err != nil {
			// Skip unparseable events but continue
			continue
		}

		event.Source = name
		source.Events = append(source.Events, event)
	}

	return source, nil
}

// EventsSource creates a source from a slice of events.
func EventsSource(name string, events []*Event) *Source {
	for _, e := range events {
		if e.Source == "" {
			e.Source = name
		}
	}
	return &Source{
		Name:   name,
		Events: events,
	}
}

// =============================================================================
// Event Parsing
// =============================================================================

// ParseEvent parses a map into an Event.
func ParseEvent(m map[string]interface{}) (*Event, error) {
	event := &Event{
		InputRedacted: make(map[string]interface{}),
		Output:        make(map[string]interface{}),
	}

	// Required fields
	event.RunID = getString(m, "run_id")
	event.ScenarioID = getString(m, "scenario_id")
	event.StepID = getString(m, "step_id")
	event.Timestamp = getString(m, "timestamp")

	if event.RunID == "" || event.ScenarioID == "" || event.StepID == "" || event.Timestamp == "" {
		return nil, fmt.Errorf("missing required fields")
	}

	// Parse timestamp
	parsed, err := time.Parse(time.RFC3339, event.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %w", err)
	}
	event.ParsedTimestamp = parsed

	// Optional fields
	event.Component = getString(m, "component")
	event.Actor = getString(m, "actor")
	event.Decision = getString(m, "decision")
	event.DurationMs = getInt64(m, "duration_ms")

	// Maps
	if ir, ok := m["input_redacted"].(map[string]interface{}); ok {
		event.InputRedacted = ir
	}
	if out, ok := m["output"].(map[string]interface{}); ok {
		event.Output = out
	}

	// Error envelope
	if errMap, ok := m["error"].(map[string]interface{}); ok {
		event.Error = ErrorInfo{
			Present: getBool(errMap, "present"),
			Code:    getString(errMap, "code"),
			Message: getString(errMap, "message"),
			Details: getMap(errMap, "details"),
		}
	}

	return event, nil
}

// ParseEventFromJSON parses a JSON byte slice into an Event.
func ParseEventFromJSON(data []byte) (*Event, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	event, err := ParseEvent(m)
	if err != nil {
		return nil, err
	}
	event.RawJSON = data
	return event, nil
}

// =============================================================================
// Merger
// =============================================================================

// Merger merges multiple event sources into unified timelines.
type Merger struct {
	// events stores all events before merging
	events []*Event

	// seen tracks deduplication keys
	seen map[string]bool

	// stats tracks merge statistics
	stats MergeStats

	// errors tracks non-fatal errors
	errors []MergeError
}

// NewMerger creates a new Merger.
func NewMerger() *Merger {
	return &Merger{
		events: make([]*Event, 0),
		seen:   make(map[string]bool),
	}
}

// AddSource adds events from a source to the merger.
func (m *Merger) AddSource(source *Source) {
	m.stats.TotalSources++
	for _, event := range source.Events {
		m.AddEvent(event)
	}
}

// AddEvent adds a single event to the merger.
func (m *Merger) AddEvent(event *Event) {
	if event == nil {
		return
	}

	// Generate deduplication key
	key := dedupeKey(event)

	// Check for duplicate
	if m.seen[key] {
		m.stats.DuplicateEvents++
		return
	}

	m.seen[key] = true
	m.events = append(m.events, event)
	m.stats.TotalEvents++
}

// Merge performs the merge and returns the result.
// The output is deterministic: same inputs always produce same outputs.
func (m *Merger) Merge() *MergeResult {
	start := time.Now()

	result := &MergeResult{
		Timelines: make(map[TimelineKey]*Timeline),
		Errors:    m.errors,
	}

	// Group events by timeline key
	groups := make(map[TimelineKey][]*Event)
	for _, event := range m.events {
		key := TimelineKey{
			RunID:      event.RunID,
			ScenarioID: event.ScenarioID,
		}
		groups[key] = append(groups[key], event)
	}

	// Sort and build timelines
	for key, events := range groups {
		timeline := m.buildTimeline(key, events)
		result.Timelines[key] = timeline
	}

	// Calculate final stats
	result.Stats = m.stats
	result.Stats.TotalTimelines = len(result.Timelines)
	result.Stats.MergeDurationMs = time.Since(start).Milliseconds()

	return result
}

// buildTimeline creates a sorted, deduplicated timeline from events.
func (m *Merger) buildTimeline(key TimelineKey, events []*Event) *Timeline {
	timeline := &Timeline{
		RunID:       key.RunID,
		ScenarioID:  key.ScenarioID,
		Events:      events,
		SourceStats: make(map[string]int),
	}

	// Sort events deterministically:
	// 1. By timestamp (ascending)
	// 2. By step_id (for same timestamp)
	// 3. By source (for same timestamp and step_id)
	sort.SliceStable(timeline.Events, func(i, j int) bool {
		a, b := timeline.Events[i], timeline.Events[j]

		// Primary: timestamp
		if !a.ParsedTimestamp.Equal(b.ParsedTimestamp) {
			return a.ParsedTimestamp.Before(b.ParsedTimestamp)
		}

		// Secondary: step_id (lexicographic)
		if a.StepID != b.StepID {
			return a.StepID < b.StepID
		}

		// Tertiary: source (lexicographic)
		return a.Source < b.Source
	})

	// Calculate stats
	for _, event := range timeline.Events {
		timeline.TotalDurationMs += event.DurationMs
		timeline.SourceStats[event.Source]++

		if timeline.StartTime.IsZero() || event.ParsedTimestamp.Before(timeline.StartTime) {
			timeline.StartTime = event.ParsedTimestamp
		}
		if timeline.EndTime.IsZero() || event.ParsedTimestamp.After(timeline.EndTime) {
			timeline.EndTime = event.ParsedTimestamp
		}
	}

	timeline.TotalEvents = len(timeline.Events)

	return timeline
}

// =============================================================================
// Output
// =============================================================================

// WriteJSONL writes a timeline to a writer in JSONL format.
func (t *Timeline) WriteJSONL(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	for _, event := range t.Events {
		// Use raw JSON if available, otherwise encode
		if len(event.RawJSON) > 0 {
			if _, err := w.Write(event.RawJSON); err != nil {
				return err
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
		} else {
			if err := encoder.Encode(event); err != nil {
				return err
			}
		}
	}

	return nil
}

// WriteJSONLFile writes a timeline to a file in JSONL format.
func (t *Timeline) WriteJSONLFile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return t.WriteJSONL(file)
}

// ToJSONL returns the timeline as a JSONL string.
func (t *Timeline) ToJSONL() string {
	var sb strings.Builder
	for i, event := range t.Events {
		if i > 0 {
			sb.WriteByte('\n')
		}
		data, _ := json.Marshal(event)
		sb.Write(data)
	}
	return sb.String()
}

// =============================================================================
// Helper Functions
// =============================================================================

// dedupeKey generates a unique key for deduplication.
// Uses run_id + scenario_id + step_id + timestamp for uniqueness.
func dedupeKey(event *Event) string {
	return fmt.Sprintf("%s|%s|%s|%s", event.RunID, event.ScenarioID, event.StepID, event.Timestamp)
}

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getInt64 safely extracts an int64 from a map.
func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i
			}
		}
	}
	return 0
}

// getBool safely extracts a bool from a map.
func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getMap safely extracts a map from a map.
func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]interface{}); ok {
			return mm
		}
	}
	return nil
}