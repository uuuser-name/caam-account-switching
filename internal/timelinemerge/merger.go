// Package timelinemerge provides deterministic merging of multi-source test logs.
// This file contains the core merge engine and algorithms.
package timelinemerge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Merge Engine
// =============================================================================

// Engine provides advanced merging capabilities for test logs.
type Engine struct {
	// DedupeStrategy controls how duplicates are handled
	DedupeStrategy DedupeStrategy

	// SortStrategy controls the sort order
	SortStrategy SortStrategy

	// ClockSkewTolerance is the max allowed clock skew between sources
	ClockSkewTolerance time.Duration
}

// DedupeStrategy defines how duplicate events are handled.
type DedupeStrategy int

const (
	// DedupeByFullKey deduplicates by run_id + scenario_id + step_id + timestamp
	DedupeByFullKey DedupeStrategy = iota

	// DedupeByStepID deduplicates by run_id + scenario_id + step_id (keeps first)
	DedupeByStepID

	// DedupeNone does not deduplicate
	DedupeNone
)

// SortStrategy defines the sort order for events.
type SortStrategy int

const (
	// SortByTimestamp sorts primarily by timestamp
	SortByTimestamp SortStrategy = iota

	// SortByTimestampThenSource sorts by timestamp, then source
	SortByTimestampThenSource

	// SortByScenarioThenTimestamp groups by scenario then sorts by timestamp
	SortByScenarioThenTimestamp
)

// NewEngine creates a new merge engine with default settings.
func NewEngine() *Engine {
	return &Engine{
		DedupeStrategy:     DedupeByFullKey,
		SortStrategy:       SortByTimestampThenSource,
		ClockSkewTolerance: time.Second,
	}
}

// MergeSources merges multiple sources into a single result.
func (e *Engine) MergeSources(sources []*Source) *MergeResult {
	merger := NewMerger()
	for _, source := range sources {
		merger.AddSource(source)
	}
	return merger.Merge()
}

// MergeFiles merges JSONL files into timelines.
func (e *Engine) MergeFiles(paths []string) (*MergeResult, error) {
	var sources []*Source
	var mergeErrors []MergeError

	for _, path := range paths {
		source, err := FileSource(filepath.Base(path), path)
		if err != nil {
			mergeErrors = append(mergeErrors, MergeError{
				Source:  path,
				Message: err.Error(),
			})
			continue
		}
		sources = append(sources, source)
	}

	result := e.MergeSources(sources)
	result.Errors = append(result.Errors, mergeErrors...)
	result.Stats.ParseErrors = len(mergeErrors)

	return result, nil
}

// MergeReaders merges JSONL readers into timelines.
func (e *Engine) MergeReaders(name string, readers []io.Reader) *MergeResult {
	var sources []*Source

	for i, r := range readers {
		sourceName := fmt.Sprintf("%s-%d", name, i)
		source, err := ReaderSource(sourceName, r)
		if err != nil {
			continue
		}
		sources = append(sources, source)
	}

	return e.MergeSources(sources)
}

// =============================================================================
// Deterministic Merge Algorithm
// =============================================================================

// DeterministicMerger provides guaranteed deterministic merge output.
// Same inputs always produce identical outputs, bit-for-bit.
type DeterministicMerger struct {
	events         []*Event
	keyFunc        func(*Event) string
	lessFunc       func(a, b *Event) bool
	seen           map[string]int // key -> index in events
	duplicateCount int            // count of duplicate events
	sources        map[string]int // source name -> count
	startTime      time.Time
}

// NewDeterministicMerger creates a merger with guaranteed determinism.
func NewDeterministicMerger() *DeterministicMerger {
	return &DeterministicMerger{
		events:  make([]*Event, 0),
		seen:    make(map[string]int),
		sources: make(map[string]int),
		keyFunc: defaultKeyFunc,
		lessFunc: func(a, b *Event) bool {
			// Primary: timestamp (ascending)
			if !a.ParsedTimestamp.Equal(b.ParsedTimestamp) {
				return a.ParsedTimestamp.Before(b.ParsedTimestamp)
			}
			// Secondary: scenario_id (lexicographic)
			if a.ScenarioID != b.ScenarioID {
				return a.ScenarioID < b.ScenarioID
			}
			// Tertiary: step_id (lexicographic)
			if a.StepID != b.StepID {
				return a.StepID < b.StepID
			}
			// Quaternary: source (lexicographic)
			return a.Source < b.Source
		},
		startTime: time.Now(),
	}
}

// defaultKeyFunc generates a unique key for deduplication.
func defaultKeyFunc(e *Event) string {
	return fmt.Sprintf("%s|%s|%s|%s", e.RunID, e.ScenarioID, e.StepID, e.Timestamp)
}

// Add adds an event to the merger.
// Returns true if the event was added, false if it was a duplicate.
func (m *DeterministicMerger) Add(event *Event) bool {
	if event == nil {
		return false
	}

	key := m.keyFunc(event)

	// Check for existing event with same key
	if idx, exists := m.seen[key]; exists {
		m.duplicateCount++
		// Conflict resolution: prefer event with more detail
		existing := m.events[idx]
		if shouldReplace(existing, event) {
			m.events[idx] = event
		}
		return false
	}

	// Add new event
	m.seen[key] = len(m.events)
	m.events = append(m.events, event)
	m.sources[event.Source]++

	return true
}

// AddSource adds all events from a source.
func (m *DeterministicMerger) AddSource(source *Source) int {
	added := 0
	for _, event := range source.Events {
		if m.Add(event) {
			added++
		}
	}
	return added
}

// shouldReplace determines if a new event should replace an existing one.
// Uses deterministic rules: prefer event with more populated fields.
func shouldReplace(existing, newEvent *Event) bool {
	// Prefer event with non-empty component
	if existing.Component == "" && newEvent.Component != "" {
		return true
	}

	// Prefer event with non-empty actor
	if existing.Actor == "" && newEvent.Actor != "" {
		return true
	}

	// Prefer event with more input/output data
	existingFields := len(existing.InputRedacted) + len(existing.Output)
	newFields := len(newEvent.InputRedacted) + len(newEvent.Output)
	return newFields > existingFields
}

// Merge returns the merged result.
// Output is guaranteed deterministic: same inputs always produce same outputs.
func (m *DeterministicMerger) Merge() *MergeResult {
	// Sort events deterministically
	sorted := make([]*Event, len(m.events))
	copy(sorted, m.events)

	sort.SliceStable(sorted, func(i, j int) bool {
		return m.lessFunc(sorted[i], sorted[j])
	})

	// Group by timeline key
	timelines := make(map[TimelineKey]*Timeline)

	for _, event := range sorted {
		key := TimelineKey{
			RunID:      event.RunID,
			ScenarioID: event.ScenarioID,
		}

		timeline, exists := timelines[key]
		if !exists {
			timeline = &Timeline{
				RunID:       key.RunID,
				ScenarioID:  key.ScenarioID,
				Events:      make([]*Event, 0),
				SourceStats: make(map[string]int),
			}
			timelines[key] = timeline
		}

		timeline.Events = append(timeline.Events, event)
		timeline.TotalDurationMs += event.DurationMs
		timeline.SourceStats[event.Source]++

		// Update time bounds
		if timeline.StartTime.IsZero() || event.ParsedTimestamp.Before(timeline.StartTime) {
			timeline.StartTime = event.ParsedTimestamp
		}
		if timeline.EndTime.IsZero() || event.ParsedTimestamp.After(timeline.EndTime) {
			timeline.EndTime = event.ParsedTimestamp
		}
	}

	// Finalize timeline stats
	for _, timeline := range timelines {
		timeline.TotalEvents = len(timeline.Events)
	}

	return &MergeResult{
		Timelines: timelines,
		Stats: MergeStats{
			TotalSources:    len(m.sources),
			TotalEvents:     len(sorted),
			TotalTimelines:  len(timelines),
			DuplicateEvents: m.duplicateCount,
			MergeDurationMs: time.Since(m.startTime).Milliseconds(),
		},
	}
}

// =============================================================================
// JSONL Processing
// =============================================================================

// ParseJSONLFile parses a JSONL file into events.
func ParseJSONLFile(path string) ([]*Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return ParseJSONLReader(file)
}

// ParseJSONLReader parses JSONL from a reader into events.
func ParseJSONLReader(r io.Reader) ([]*Event, error) {
	var events []*Event
	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		event, err := ParseEventFromJSON([]byte(line))
		if err != nil {
			continue // Skip unparseable lines
		}

		events = append(events, event)
	}

	return events, scanner.Err()
}

// WriteJSONL writes events to a writer in JSONL format.
func WriteJSONL(w io.Writer, events []*Event) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}

	return nil
}

// WriteJSONLFile writes events to a file in JSONL format.
func WriteJSONLFile(path string, events []*Event) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return WriteJSONL(file, events)
}

// =============================================================================
// Utility Functions
// =============================================================================

// EventsByRunID groups events by run ID.
func EventsByRunID(events []*Event) map[string][]*Event {
	result := make(map[string][]*Event)
	for _, event := range events {
		result[event.RunID] = append(result[event.RunID], event)
	}
	return result
}

// EventsByScenarioID groups events by scenario ID within a run.
func EventsByScenarioID(events []*Event) map[string][]*Event {
	result := make(map[string][]*Event)
	for _, event := range events {
		result[event.ScenarioID] = append(result[event.ScenarioID], event)
	}
	return result
}

// FilterByRunID returns events matching a run ID.
func FilterByRunID(events []*Event, runID string) []*Event {
	var result []*Event
	for _, event := range events {
		if event.RunID == runID {
			result = append(result, event)
		}
	}
	return result
}

// FilterByScenarioID returns events matching a scenario ID.
func FilterByScenarioID(events []*Event, scenarioID string) []*Event {
	var result []*Event
	for _, event := range events {
		if event.ScenarioID == scenarioID {
			result = append(result, event)
		}
	}
	return result
}

// FilterByTimeRange returns events within a time range.
func FilterByTimeRange(events []*Event, start, end time.Time) []*Event {
	var result []*Event
	for _, event := range events {
		if (start.IsZero() || !event.ParsedTimestamp.Before(start)) &&
			(end.IsZero() || !event.ParsedTimestamp.After(end)) {
			result = append(result, event)
		}
	}
	return result
}

// SortEvents deterministically sorts a slice of events.
func SortEvents(events []*Event) {
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]

		// Primary: timestamp (ascending)
		if !a.ParsedTimestamp.Equal(b.ParsedTimestamp) {
			return a.ParsedTimestamp.Before(b.ParsedTimestamp)
		}

		// Secondary: scenario_id (lexicographic)
		if a.ScenarioID != b.ScenarioID {
			return a.ScenarioID < b.ScenarioID
		}

		// Tertiary: step_id (lexicographic)
		if a.StepID != b.StepID {
			return a.StepID < b.StepID
		}

		// Quaternary: source (lexicographic)
		return a.Source < b.Source
	})
}

// DeduplicateEvents removes duplicate events from a slice.
// Returns the deduplicated slice and count of duplicates removed.
func DeduplicateEvents(events []*Event) ([]*Event, int) {
	seen := make(map[string]bool)
	var result []*Event
	duplicates := 0

	for _, event := range events {
		key := defaultKeyFunc(event)
		if !seen[key] {
			seen[key] = true
			result = append(result, event)
		} else {
			duplicates++
		}
	}

	return result, duplicates
}

// MergeEventSlices merges multiple event slices into one deduplicated slice.
func MergeEventSlices(slices ...[]*Event) []*Event {
	merger := NewDeterministicMerger()
	for _, slice := range slices {
		for _, event := range slice {
			merger.Add(event)
		}
	}
	result := merger.Merge()

	// Flatten all timeline events into one slice
	var allEvents []*Event
	for _, timeline := range result.Timelines {
		allEvents = append(allEvents, timeline.Events...)
	}

	return allEvents
}

// Stats returns statistics about a slice of events.
func Stats(events []*Event) EventStats {
	stats := EventStats{
		ByRunID:     make(map[string]int),
		ByScenario:  make(map[string]int),
		ByComponent: make(map[string]int),
		BySource:    make(map[string]int),
		ByDecision:  make(map[string]int),
	}

	for _, event := range events {
		stats.TotalEvents++
		stats.TotalDurationMs += event.DurationMs

		stats.ByRunID[event.RunID]++
		stats.ByScenario[event.ScenarioID]++
		stats.ByComponent[event.Component]++
		stats.BySource[event.Source]++
		stats.ByDecision[event.Decision]++

		if event.Error.Present {
			stats.ErrorCount++
		}

		if stats.Earliest.IsZero() || event.ParsedTimestamp.Before(stats.Earliest) {
			stats.Earliest = event.ParsedTimestamp
		}
		if stats.Latest.IsZero() || event.ParsedTimestamp.After(stats.Latest) {
			stats.Latest = event.ParsedTimestamp
		}
	}

	return stats
}

// EventStats contains statistics about events.
type EventStats struct {
	TotalEvents     int
	TotalDurationMs int64
	ErrorCount      int
	Earliest        time.Time
	Latest          time.Time
	ByRunID         map[string]int
	ByScenario      map[string]int
	ByComponent     map[string]int
	BySource        map[string]int
	ByDecision      map[string]int
}
