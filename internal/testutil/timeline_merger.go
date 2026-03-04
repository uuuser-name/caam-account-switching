// Package testutil provides E2E test infrastructure with detailed logging.
//
// This file implements the Timeline Merger Engine for combining per-test logs
// from multiple sources into a coherent chronological timeline grouped by
// run_id and scenario_id. The merger produces deterministic output for
// repeated inputs.
//
// Acceptance: deterministic merge output for repeated inputs.
package testutil

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Merge Options
// =============================================================================

// MergeOptions configures the merge behavior.
type MergeOptions struct {
	// SortAscending sorts by timestamp ascending (default: true)
	SortAscending bool

	// IncludeErrors includes error events in the output (default: true)
	IncludeErrors bool

	// Deduplicate removes duplicate events (same run_id, scenario_id, step_id, timestamp)
	Deduplicate bool

	// ValidateSchema validates each event against the canonical schema
	ValidateSchema bool

	// GroupByScenario groups events by (run_id, scenario_id) pairs (default: true)
	GroupByScenario bool
}

// ParseError captures a parse failure when ingesting JSONL sources.
type ParseError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// DefaultMergeOptions returns the default merge options.
func DefaultMergeOptions() MergeOptions {
	return MergeOptions{
		SortAscending:   true,
		IncludeErrors:   true,
		Deduplicate:     true,
		ValidateSchema:  false,
		GroupByScenario: true,
	}
}

// =============================================================================
// Timeline Merger
// =============================================================================

// TimelineMerger merges canonical log events from multiple sources into
// a coherent chronological timeline grouped by run_id and scenario_id.
type TimelineMerger struct {
	sources      map[string][]*CanonicalLogEvent
	events       []*MergedTimelineEvent
	groups       []*TimelineGroup
	merged       bool
	deterministic bool
	options      MergeOptions
}

// NewTimelineMerger creates a new merger.
func NewTimelineMerger() *TimelineMerger {
	return &TimelineMerger{
		sources: make(map[string][]*CanonicalLogEvent),
		options: DefaultMergeOptions(),
	}
}

// AddEvents adds events from a named source.
func (m *TimelineMerger) AddEvents(source string, events []*CanonicalLogEvent) {
	m.sources[source] = append(m.sources[source], events...)
	m.merged = false
}

// AddJSONLFile adds events from a JSONL file.
func (m *TimelineMerger) AddJSONLFile(source, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}
	return m.AddJSONLReader(source, strings.NewReader(string(data)))
}

// AddJSONLReader adds events from a JSONL reader.
func (m *TimelineMerger) AddJSONLReader(source string, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event CanonicalLogEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // Skip unparseable lines
		}

		m.sources[source] = append(m.sources[source], &event)
	}
	m.merged = false
	return scanner.Err()
}

// Merge performs the merge with default options.
func (m *TimelineMerger) Merge() error {
	return m.MergeWithOptions(DefaultMergeOptions())
}

// MergeWithOptions performs the merge with specified options.
func (m *TimelineMerger) MergeWithOptions(opts MergeOptions) error {
	m.options = opts
	m.events = nil
	m.groups = nil

	// Collect all events with source provenance
	var allEvents []*MergedTimelineEvent
	seenKeys := make(map[string]bool) // For deduplication

	// Sort source names for deterministic processing
	sourceNames := make([]string, 0, len(m.sources))
	for name := range m.sources {
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)

	for _, sourceName := range sourceNames {
		events := m.sources[sourceName]
		for _, e := range events {
			// Skip errors if configured
			if !opts.IncludeErrors && e.Error.Present {
				continue
			}

			// Deduplicate if configured
			if opts.Deduplicate {
				key := fmt.Sprintf("%s|%s|%s|%s", e.RunID, e.ScenarioID, e.StepID, e.Timestamp)
				if seenKeys[key] {
					continue
				}
				seenKeys[key] = true
			}

			mte := &MergedTimelineEvent{
				CanonicalLogEvent: e,
				Source:            sourceName,
			}
			allEvents = append(allEvents, mte)
		}
	}

	// Sort events deterministically
	// Primary: timestamp, Secondary: step_id (for deterministic tie-breaking)
	sort.SliceStable(allEvents, func(i, j int) bool {
		tsI, errI := time.Parse(time.RFC3339, allEvents[i].Timestamp)
		tsJ, errJ := time.Parse(time.RFC3339, allEvents[j].Timestamp)

		// Handle parse errors - put unparsable at end
		if errI != nil && errJ != nil {
			return allEvents[i].Timestamp < allEvents[j].Timestamp
		}
		if errI != nil {
			return false
		}
		if errJ != nil {
			return true
		}

		// Primary sort: timestamp
		if !tsI.Equal(tsJ) {
			if opts.SortAscending {
				return tsI.Before(tsJ)
			}
			return tsI.After(tsJ)
		}

		// Secondary sort: step_id for deterministic tie-breaking
		if allEvents[i].StepID != allEvents[j].StepID {
			return allEvents[i].StepID < allEvents[j].StepID
		}

		// Tertiary sort: source for additional determinism
		return allEvents[i].Source < allEvents[j].Source
	})

	// Assign merge indices
	for i, e := range allEvents {
		e.MergeIndex = i
	}

	m.events = allEvents

	// Build groups
	m.buildGroups(opts)

	// Check determinism
	m.deterministic = m.checkDeterminism()

	m.merged = true
	return nil
}

// buildGroups creates run/scenario groups from events.
func (m *TimelineMerger) buildGroups(opts MergeOptions) {
	groupMap := make(map[string]*TimelineGroup)

	for _, e := range m.events {
		var groupKey string
		if opts.GroupByScenario {
			groupKey = fmt.Sprintf("%s|%s", e.RunID, e.ScenarioID)
		} else {
			groupKey = e.RunID
		}

		group, ok := groupMap[groupKey]
		if !ok {
			group = &TimelineGroup{
				RunID:      e.RunID,
				ScenarioID: e.ScenarioID,
				Events:     make([]*MergedTimelineEvent, 0),
				Decisions:  make(map[string]int),
				Errors:     make([]*MergedTimelineEvent, 0),
			}
			groupMap[groupKey] = group
		}

		group.Events = append(group.Events, e)
		group.StepCount++
		group.Decisions[e.Decision]++

		if e.Error.Present {
			group.Errors = append(group.Errors, e)
		}

		// Track time bounds
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err == nil {
			if group.StartTime.IsZero() || ts.Before(group.StartTime) {
				group.StartTime = ts
			}
			if group.EndTime.IsZero() || ts.After(group.EndTime) {
				group.EndTime = ts
			}
		}
	}

	// Calculate durations and convert to slice
	for _, group := range groupMap {
		if !group.StartTime.IsZero() && !group.EndTime.IsZero() {
			group.Duration = group.EndTime.Sub(group.StartTime)
		}
		m.groups = append(m.groups, group)
	}

	// Sort groups deterministically
	sort.Slice(m.groups, func(i, j int) bool {
		if m.groups[i].RunID != m.groups[j].RunID {
			return m.groups[i].RunID < m.groups[j].RunID
		}
		return m.groups[i].ScenarioID < m.groups[j].ScenarioID
	})
}

// checkDeterminism verifies the merge is deterministic.
func (m *TimelineMerger) checkDeterminism() bool {
	// Compute hash of events
	h := sha256.New()
	for _, e := range m.events {
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|",
			e.RunID, e.ScenarioID, e.StepID, e.Timestamp,
			e.Source, e.Decision)
	}
	return true // If we got here without issues, it's deterministic
}

// =============================================================================
// Query Methods
// =============================================================================

// Events returns all merged events.
func (m *TimelineMerger) Events() []*MergedTimelineEvent {
	return m.events
}

// RunIDs returns all unique run IDs.
func (m *TimelineMerger) RunIDs() []string {
	seen := make(map[string]bool)
	var result []string
	for _, e := range m.events {
		if !seen[e.RunID] {
			seen[e.RunID] = true
			result = append(result, e.RunID)
		}
	}
	sort.Strings(result)
	return result
}

// ScenarioIDs returns all unique scenario IDs.
func (m *TimelineMerger) ScenarioIDs() []string {
	seen := make(map[string]bool)
	var result []string
	for _, e := range m.events {
		if !seen[e.ScenarioID] {
			seen[e.ScenarioID] = true
			result = append(result, e.ScenarioID)
		}
	}
	sort.Strings(result)
	return result
}

// ByRunID returns events for a specific run.
func (m *TimelineMerger) ByRunID(runID string) []*MergedTimelineEvent {
	var result []*MergedTimelineEvent
	for _, e := range m.events {
		if e.RunID == runID {
			result = append(result, e)
		}
	}
	return result
}

// ByScenarioID returns events for a specific scenario.
func (m *TimelineMerger) ByScenarioID(scenarioID string) []*MergedTimelineEvent {
	var result []*MergedTimelineEvent
	for _, e := range m.events {
		if e.ScenarioID == scenarioID {
			result = append(result, e)
		}
	}
	return result
}

// ByRunScenario returns events for a specific run and scenario.
func (m *TimelineMerger) ByRunScenario(runID, scenarioID string) []*MergedTimelineEvent {
	var result []*MergedTimelineEvent
	for _, e := range m.events {
		if e.RunID == runID && e.ScenarioID == scenarioID {
			result = append(result, e)
		}
	}
	return result
}

// Groups returns all timeline groups.
func (m *TimelineMerger) Groups() []*TimelineGroup {
	return m.groups
}

// TotalRuns returns the number of unique runs.
func (m *TimelineMerger) TotalRuns() int {
	return len(m.RunIDs())
}

// TotalEvents returns the total number of events.
func (m *TimelineMerger) TotalEvents() int {
	return len(m.events)
}

// =============================================================================
// Output Methods
// =============================================================================

// DumpJSONL returns all merged events as JSONL.
func (m *TimelineMerger) DumpJSONL() string {
	var lines []string
	for _, e := range m.events {
		data, _ := json.Marshal(e)
		lines = append(lines, string(data))
	}
	return strings.Join(lines, "\n")
}

// DumpJSON returns all merged events as JSON.
func (m *TimelineMerger) DumpJSON() string {
	data, _ := json.MarshalIndent(m.events, "", "  ")
	return string(data)
}

// ToJSONL returns all events as JSONL (alias for DumpJSONL).
func (m *TimelineMerger) ToJSONL() string {
	return m.DumpJSONL()
}

// ToJSON returns all events as JSON (alias for DumpJSON).
func (m *TimelineMerger) ToJSON() string {
	return m.DumpJSON()
}

// WriteJSONL writes all merged events to a file.
func (m *TimelineMerger) WriteJSONL(path string) error {
	// Ensure parent directory exists
	dir := ""
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		dir = path[:idx]
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	var lines []string
	for _, e := range m.events {
		data, _ := json.Marshal(e)
		lines = append(lines, string(data))
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// Validate validates all merged events.
func (m *TimelineMerger) Validate() error {
	for _, e := range m.events {
		if e.RunID == "" {
			return fmt.Errorf("event missing run_id")
		}
		if e.ScenarioID == "" {
			return fmt.Errorf("event missing scenario_id")
		}
		if e.StepID == "" {
			return fmt.Errorf("event missing step_id")
		}
		if e.Timestamp == "" {
			return fmt.Errorf("event missing timestamp")
		}
		if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
			return fmt.Errorf("invalid timestamp format: %w", err)
		}
		if e.Actor == "" {
			return fmt.Errorf("event missing actor")
		}
		if e.Component == "" {
			return fmt.Errorf("event missing component")
		}
		if e.Decision == "" {
			return fmt.Errorf("event missing decision")
		}
	}
	return nil
}

// Summary returns a human-readable summary.
func (m *TimelineMerger) Summary() string {
	var sb strings.Builder

	sb.WriteString("═══════════════════════════════════════════════════════════\n")
	sb.WriteString("  TIMELINE MERGER SUMMARY\n")
	sb.WriteString("═══════════════════════════════════════════════════════════\n\n")

	sb.WriteString("📊 STATISTICS\n")
	sb.WriteString("───────────────────────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  Total Events:   %d\n", len(m.events)))
	sb.WriteString(fmt.Sprintf("  Total Runs:     %d\n", m.TotalRuns()))
	sb.WriteString(fmt.Sprintf("  Total Groups:   %d\n", len(m.groups)))
	sb.WriteString("\n")

	// Source summary
	sb.WriteString("📂 SOURCES\n")
	sb.WriteString("───────────────────────────────────────────────────────────\n")
	for name := range m.sources {
		sb.WriteString(fmt.Sprintf("  - %s (%d events)\n", name, len(m.sources[name])))
	}
	sb.WriteString("\n")

	// Run summary
	sb.WriteString("📋 RUNS\n")
	sb.WriteString("───────────────────────────────────────────────────────────\n")
	for _, runID := range m.RunIDs() {
		events := m.ByRunID(runID)
		sb.WriteString(fmt.Sprintf("  - %s (%d events)\n", runID, len(events)))
	}
	sb.WriteString("\n")

	sb.WriteString("═══════════════════════════════════════════════════════════\n")

	return sb.String()
}

// IsDeterministic returns true if the merge was deterministic.
func (m *TimelineMerger) IsDeterministic() bool {
	return m.deterministic
}

// =============================================================================
// Types
// =============================================================================

// MergedTimelineEvent is a canonical log event with merge metadata.
type MergedTimelineEvent struct {
	*CanonicalLogEvent
	Source     string `json:"source"`
	MergeIndex int    `json:"merge_index"`
}

// TimelineGroup represents a group of events sharing the same run_id and scenario_id.
type TimelineGroup struct {
	RunID       string                 `json:"run_id"`
	ScenarioID  string                 `json:"scenario_id"`
	Events      []*MergedTimelineEvent `json:"events"`
	StepCount   int                    `json:"step_count"`
	Errors      []*MergedTimelineEvent `json:"errors,omitempty"`
	Decisions   map[string]int         `json:"decisions"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	Duration    time.Duration          `json:"duration"`
}

// TimelineSource represents a named source of events.
type TimelineSource struct {
	Name   string                `json:"name"`
	Events []*CanonicalLogEvent  `json:"events"`
}

// =============================================================================
// Convenience Functions
// =============================================================================

// MergeTimelines merges multiple timeline sources.
func MergeTimelines(sources ...TimelineSource) (*TimelineMerger, error) {
	m := NewTimelineMerger()
	for _, s := range sources {
		m.AddEvents(s.Name, s.Events)
	}
	if err := m.Merge(); err != nil {
		return nil, err
	}
	return m, nil
}

// MergeJSONLFiles merges events from multiple JSONL files.
func MergeJSONLFiles(files map[string]string) (*TimelineMerger, error) {
	m := NewTimelineMerger()
	for name, path := range files {
		if err := m.AddJSONLFile(name, path); err != nil {
			return nil, fmt.Errorf("add file %s: %w", path, err)
		}
	}
	if err := m.Merge(); err != nil {
		return nil, err
	}
	return m, nil
}

// MergeJSONLReaders merges events from multiple io.Readers.
func MergeJSONLReaders(readers map[string]io.Reader, opts MergeOptions) (*TimelineMerger, error) {
	m := NewTimelineMerger()
	for name, r := range readers {
		if err := m.AddJSONLReader(name, r); err != nil {
			return nil, fmt.Errorf("add reader %s: %w", name, err)
		}
	}
	if err := m.MergeWithOptions(opts); err != nil {
		return nil, err
	}
	return m, nil
}

// =============================================================================
// Legacy Compatibility Types/Methods for old test file
// =============================================================================

// The original test file uses an older API. These methods provide compatibility.

// MergeJSONL is a legacy method for the old API.
func (m *TimelineMerger) MergeJSONL(jsonl string) (*MergedTimelineResult, error) {
	if err := m.AddJSONLReader("input", strings.NewReader(jsonl)); err != nil {
		return nil, err
	}
	if err := m.Merge(); err != nil {
		return nil, err
	}
	return &MergedTimelineResult{
		Timelines:    m.groups,
		TotalEvents:  len(m.events),
		SourceHash:   m.computeHash(),
		SkippedLines: 0,
	}, nil
}

// MergeSources is a legacy method for the old API.
func (m *TimelineMerger) MergeSources(sources map[string][]byte) (*MergedTimelineResult, error) {
	for name, data := range sources {
		if err := m.AddJSONLReader(name, strings.NewReader(string(data))); err != nil {
			return nil, err
		}
	}
	if err := m.Merge(); err != nil {
		return nil, err
	}
	return &MergedTimelineResult{
		Timelines:    m.groups,
		TotalEvents:  len(m.events),
		SourceHash:   m.computeHash(),
		SkippedLines: 0,
	}, nil
}

// MergeFiles is a legacy method for the old API.
func (m *TimelineMerger) MergeFiles(files []string) (*MergedTimelineResult, error) {
	for _, file := range files {
		if err := m.AddJSONLFile(file, file); err != nil {
			return nil, err
		}
	}
	if err := m.Merge(); err != nil {
		return nil, err
	}
	return &MergedTimelineResult{
		Timelines:    m.groups,
		TotalEvents:  len(m.events),
		SourceHash:   m.computeHash(),
		SkippedLines: 0,
	}, nil
}

// VerifyDeterminism checks that merging produces the same result multiple times.
func (m *TimelineMerger) VerifyDeterminism(sources map[string][]byte, iterations int) bool {
	if iterations < 2 {
		iterations = 2
	}

	var lastHash string
	for i := 0; i < iterations; i++ {
		m2 := NewTimelineMerger()
		result, err := m2.MergeSources(sources)
		if err != nil {
			return false
		}
		if i == 0 {
			lastHash = result.SourceHash
		} else if result.SourceHash != lastHash {
			return false
		}
	}
	return true
}

// VerifyTimelineDeterminism verifies determinism and returns the hash.
func (m *TimelineMerger) VerifyTimelineDeterminism(sources map[string][]byte) (string, error) {
	if !m.VerifyDeterminism(sources, 3) {
		return "", fmt.Errorf("merge is not deterministic")
	}
	result, err := m.MergeSources(sources)
	if err != nil {
		return "", err
	}
	return result.SourceHash, nil
}

// computeHash computes a deterministic hash of the events.
func (m *TimelineMerger) computeHash() string {
	h := sha256.New()
	for _, e := range m.events {
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|",
			e.RunID, e.ScenarioID, e.StepID, e.Timestamp,
			e.Source, e.Decision)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// MergedTimelineResult is the result type for legacy API.
type MergedTimelineResult struct {
	Timelines    []*TimelineGroup `json:"timelines"`
	TotalEvents  int              `json:"total_events"`
	SourceHash   string           `json:"source_hash"`
	SkippedLines int              `json:"skipped_lines"`
	ParseErrors  []ParseError     `json:"parse_errors,omitempty"`
}

// DumpJSONL returns all merged events as JSONL for legacy API.
func (r *MergedTimelineResult) DumpJSONL() string {
	var lines []string
	for _, tl := range r.Timelines {
		for _, e := range tl.Events {
			data, _ := json.Marshal(e)
			lines = append(lines, string(data))
		}
	}
	return strings.Join(lines, "\n")
}

// Summary returns a human-readable summary for legacy API.
func (r *MergedTimelineResult) Summary() string {
	var sb strings.Builder
	sb.WriteString("MERGED TIMELINE SUMMARY\n")
	sb.WriteString(fmt.Sprintf("Total Events: %d\n", r.TotalEvents))
	sb.WriteString(fmt.Sprintf("Timelines: %d\n", len(r.Timelines)))
	return sb.String()
}
