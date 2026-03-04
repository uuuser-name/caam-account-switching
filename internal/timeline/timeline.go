// Package timeline provides log timeline merging for multi-source test logs.
//
// The timeline merger engine takes multiple JSONL log files (canonical format)
// and produces a coherent chronological timeline grouped by run_id and scenario_id.
// The merge is deterministic: the same inputs always produce the same output.
package timeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Timeline Entry Types
// =============================================================================

// TimelineEntry represents a single event in the merged timeline.
// It wraps CanonicalLogEvent with additional metadata for merging.
type TimelineEntry struct {
	// RunID is the stable identifier for this test run
	RunID string `json:"run_id"`

	// ScenarioID is the stable scenario key
	ScenarioID string `json:"scenario_id"`

	// StepID is the stable step key inside a scenario
	StepID string `json:"step_id"`

	// Timestamp is the RFC3339 UTC timestamp
	Timestamp string `json:"timestamp"`

	// ParsedTimestamp is the parsed time for sorting
	ParsedTimestamp time.Time `json:"-"`

	// Actor is who produced the event
	Actor string `json:"actor"`

	// Component is the logical component
	Component string `json:"component"`

	// InputRedacted contains sanitized input data
	InputRedacted map[string]interface{} `json:"input_redacted"`

	// Output contains output summary
	Output map[string]interface{} `json:"output"`

	// Decision is the action decision (pass, continue, retry, abort)
	Decision string `json:"decision"`

	// DurationMs is the execution latency
	DurationMs int64 `json:"duration_ms"`

	// Error contains error details if present
	Error ErrorInfo `json:"error"`

	// SourceFile is the original file this entry came from (for traceability)
	SourceFile string `json:"source_file,omitempty"`

	// LineNumber is the line number in the source file
	LineNumber int `json:"line_number,omitempty"`
}

// ErrorInfo represents error details in a timeline entry.
type ErrorInfo struct {
	Present bool                   `json:"present"`
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details"`
}

// MergedTimeline represents a complete merged timeline for a run.
type MergedTimeline struct {
	// RunID is the unique identifier for this run
	RunID string `json:"run_id"`

	// ScenarioID is the scenario this timeline belongs to
	ScenarioID string `json:"scenario_id"`

	// Entries is the chronologically sorted list of events
	Entries []*TimelineEntry `json:"entries"`

	// Metadata about the merge
	Metadata MergeMetadata `json:"metadata"`
}

// MergeMetadata contains information about how the timeline was created.
type MergeMetadata struct {
	// SourceFiles is the list of files that were merged
	SourceFiles []string `json:"source_files"`

	// TotalEntries is the count of all entries
	TotalEntries int `json:"total_entries"`

	// StartTime is the earliest timestamp in the timeline
	StartTime string `json:"start_time"`

	// EndTime is the latest timestamp in the timeline
	EndTime string `json:"end_time"`

	// DurationMs is the total wall-clock duration
	DurationMs int64 `json:"duration_ms"`

	// ErrorCount is the number of entries with errors
	ErrorCount int `json:"error_count"`

	// StepCount is the number of unique steps
	StepCount int `json:"step_count"`

	// MergedAt is when this timeline was created
	MergedAt string `json:"merged_at"`

	// Hash is a deterministic hash of the timeline content
	Hash string `json:"hash"`
}

// =============================================================================
// Timeline Merger
// =============================================================================

// Merger is the timeline merger engine.
type Merger struct {
	// entries stores all parsed entries before merging
	entries []*TimelineEntry

	// sourceFiles tracks which files were read
	sourceFiles []string

	// parseErrors stores any errors encountered during parsing
	parseErrors []ParseError
}

// ParseError represents an error encountered while parsing a log file.
type ParseError struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// NewMerger creates a new timeline merger.
func NewMerger() *Merger {
	return &Merger{
		entries:     make([]*TimelineEntry, 0),
		sourceFiles: make([]string, 0),
		parseErrors: make([]ParseError, 0),
	}
}

// AddFile adds entries from a JSONL file to the merger.
// Returns the number of entries added and any error.
func (m *Merger) AddFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open file %s: %w", path, err)
	}
	defer f.Close()

	m.sourceFiles = append(m.sourceFiles, path)
	return m.AddReader(path, f)
}

// AddReader adds entries from an io.Reader (JSONL format).
// The source parameter is used for traceability in error messages.
// Returns the number of entries added and any error.
func (m *Merger) AddReader(source string, r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	lineNum := 0
	added := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		entry, err := parseLine(line)
		if err != nil {
			m.parseErrors = append(m.parseErrors, ParseError{
				File:    source,
				Line:    lineNum,
				Message: err.Error(),
			})
			continue
		}

		entry.SourceFile = source
		entry.LineNumber = lineNum
		m.entries = append(m.entries, entry)
		added++
	}

	if err := scanner.Err(); err != nil {
		return added, fmt.Errorf("scan %s: %w", source, err)
	}

	return added, nil
}

// AddEntries adds pre-parsed timeline entries directly.
func (m *Merger) AddEntries(entries []*TimelineEntry) {
	for _, entry := range entries {
		if entry != nil {
			m.entries = append(m.entries, entry)
		}
	}
}

// parseLine parses a single JSONL line into a TimelineEntry.
func parseLine(line string) (*TimelineEntry, error) {
	// Parse as raw JSON first to handle the timestamp
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	entry := &TimelineEntry{}

	// Extract required fields
	if v, ok := raw["run_id"].(string); ok {
		entry.RunID = v
	}
	if v, ok := raw["scenario_id"].(string); ok {
		entry.ScenarioID = v
	}
	if v, ok := raw["step_id"].(string); ok {
		entry.StepID = v
	}
	if v, ok := raw["timestamp"].(string); ok {
		entry.Timestamp = v
		// Parse timestamp for sorting
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			entry.ParsedTimestamp = t
		}
	}
	if v, ok := raw["actor"].(string); ok {
		entry.Actor = v
	}
	if v, ok := raw["component"].(string); ok {
		entry.Component = v
	}
	if v, ok := raw["decision"].(string); ok {
		entry.Decision = v
	}
	if v, ok := raw["duration_ms"].(float64); ok {
		entry.DurationMs = int64(v)
	}

	// Extract maps
	if v, ok := raw["input_redacted"].(map[string]interface{}); ok {
		entry.InputRedacted = v
	}
	if v, ok := raw["output"].(map[string]interface{}); ok {
		entry.Output = v
	}

	// Extract error envelope
	if errMap, ok := raw["error"].(map[string]interface{}); ok {
		if v, ok := errMap["present"].(bool); ok {
			entry.Error.Present = v
		}
		if v, ok := errMap["code"].(string); ok {
			entry.Error.Code = v
		}
		if v, ok := errMap["message"].(string); ok {
			entry.Error.Message = v
		}
		if v, ok := errMap["details"].(map[string]interface{}); ok {
			entry.Error.Details = v
		}
	}

	return entry, nil
}

// Merge produces a merged timeline grouped by run_id and scenario_id.
// The result is deterministically sorted by:
// 1. run_id (lexicographic)
// 2. scenario_id (lexicographic)
// 3. timestamp (chronological)
// 4. step_id (lexicographic, for stable ordering of same-timestamp events)
func (m *Merger) Merge() []*MergedTimeline {
	// Group by run_id + scenario_id
	groups := make(map[string]*MergedTimeline)
	groupKeys := make([]string, 0) // For deterministic ordering

	for _, entry := range m.entries {
		key := fmt.Sprintf("%s|%s", entry.RunID, entry.ScenarioID)
		if _, exists := groups[key]; !exists {
			groups[key] = &MergedTimeline{
				RunID:      entry.RunID,
				ScenarioID: entry.ScenarioID,
				Entries:    make([]*TimelineEntry, 0),
			}
			groupKeys = append(groupKeys, key)
		}
		groups[key].Entries = append(groups[key].Entries, entry)
	}

	// Sort group keys for deterministic output
	sort.Strings(groupKeys)

	// Sort entries within each group and compute metadata
	results := make([]*MergedTimeline, 0, len(groups))
	for _, key := range groupKeys {
		timeline := groups[key]
		sortTimeline(timeline)
		computeMetadata(timeline, m.sourceFiles)
		results = append(results, timeline)
	}

	return results
}

// MergeSingle produces a single merged timeline from all entries.
// Use this when you know all entries belong to the same run/scenario.
func (m *Merger) MergeSingle() *MergedTimeline {
	if len(m.entries) == 0 {
		return nil
	}

	// Use first entry's run_id and scenario_id
	timeline := &MergedTimeline{
		RunID:      m.entries[0].RunID,
		ScenarioID: m.entries[0].ScenarioID,
		Entries:    m.entries,
	}

	sortTimeline(timeline)
	computeMetadata(timeline, m.sourceFiles)

	return timeline
}

// sortTimeline sorts entries within a timeline deterministically.
func sortTimeline(timeline *MergedTimeline) {
	sort.SliceStable(timeline.Entries, func(i, j int) bool {
		a := timeline.Entries[i]
		b := timeline.Entries[j]

		// Primary: timestamp
		if !a.ParsedTimestamp.Equal(b.ParsedTimestamp) {
			return a.ParsedTimestamp.Before(b.ParsedTimestamp)
		}

		// Secondary: step_id for stable ordering
		return a.StepID < b.StepID
	})
}

// computeMetadata populates the metadata for a merged timeline.
func computeMetadata(timeline *MergedTimeline, sourceFiles []string) {
	if len(timeline.Entries) == 0 {
		timeline.Metadata = MergeMetadata{
			SourceFiles: sourceFiles,
			MergedAt:    time.Now().UTC().Format(time.RFC3339),
		}
		return
	}

	// Find unique steps
	steps := make(map[string]bool)
	for _, e := range timeline.Entries {
		steps[e.StepID] = true
	}

	// Count errors
	errorCount := 0
	for _, e := range timeline.Entries {
		if e.Error.Present {
			errorCount++
		}
	}

	// Compute time range
	startTime := timeline.Entries[0].ParsedTimestamp
	endTime := timeline.Entries[len(timeline.Entries)-1].ParsedTimestamp

	timeline.Metadata = MergeMetadata{
		SourceFiles:  sourceFiles,
		TotalEntries: len(timeline.Entries),
		StartTime:    startTime.UTC().Format(time.RFC3339),
		EndTime:      endTime.UTC().Format(time.RFC3339),
		DurationMs:   endTime.Sub(startTime).Milliseconds(),
		ErrorCount:   errorCount,
		StepCount:    len(steps),
		MergedAt:     time.Now().UTC().Format(time.RFC3339),
		Hash:         computeHash(timeline),
	}
}

// computeHash creates a deterministic hash of the timeline content.
func computeHash(timeline *MergedTimeline) string {
	// Simple hash based on content for determinism verification
	h := uint32(0)
	for _, e := range timeline.Entries {
		h = hashAdd(h, e.RunID)
		h = hashAdd(h, e.ScenarioID)
		h = hashAdd(h, e.StepID)
		h = hashAdd(h, e.Timestamp)
		h = hashAdd(h, e.Decision)
		h = hashInt(h, int(e.DurationMs))
	}
	return fmt.Sprintf("%08x", h)
}

// hashAdd adds a string to a hash.
func hashAdd(h uint32, s string) uint32 {
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}

// hashInt adds an int to a hash.
func hashInt(h uint32, n int) uint32 {
	h = h*31 + uint32(n)
	return h
}

// ParseErrors returns any errors encountered during parsing.
func (m *Merger) ParseErrors() []ParseError {
	return m.parseErrors
}

// EntryCount returns the number of entries loaded.
func (m *Merger) EntryCount() int {
	return len(m.entries)
}

// SourceFiles returns the list of files that were read.
func (m *Merger) SourceFiles() []string {
	return m.sourceFiles
}

// Reset clears all entries and errors from the merger.
func (m *Merger) Reset() {
	m.entries = make([]*TimelineEntry, 0)
	m.sourceFiles = make([]string, 0)
	m.parseErrors = make([]ParseError, 0)
}

// =============================================================================
// JSONL Export
// =============================================================================

// WriteJSONL writes a merged timeline to a file in JSONL format.
func (t *MergedTimeline) WriteJSONL(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	return t.WriteJSONLTo(f)
}

// WriteJSONLTo writes a merged timeline to an io.Writer in JSONL format.
func (t *MergedTimeline) WriteJSONLTo(w io.Writer) error {
	for _, entry := range t.Entries {
		jsonBytes, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal entry: %w", err)
		}
		if _, err := w.Write(jsonBytes); err != nil {
			return fmt.Errorf("write entry: %w", err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}
	return nil
}

// ToJSONL returns the timeline as a JSONL string.
func (t *MergedTimeline) ToJSONL() string {
	var sb strings.Builder
	for _, entry := range t.Entries {
		jsonBytes, _ := json.Marshal(entry)
		sb.Write(jsonBytes)
		sb.WriteString("\n")
	}
	return sb.String()
}

// =============================================================================
// Utility Functions
// =============================================================================

// MergeFiles is a convenience function that merges multiple JSONL files.
// Returns a single timeline if all entries share the same run_id/scenario_id,
// or returns multiple timelines if there are different runs.
func MergeFiles(paths ...string) ([]*MergedTimeline, error) {
	m := NewMerger()
	for _, path := range paths {
		if _, err := m.AddFile(path); err != nil {
			return nil, fmt.Errorf("add file %s: %w", path, err)
		}
	}
	return m.Merge(), nil
}

// MergeReaders is a convenience function that merges multiple readers.
// The sources parameter provides names for traceability.
func MergeReaders(sources []string, readers []io.Reader) ([]*MergedTimeline, error) {
	m := NewMerger()
	for i, r := range readers {
		source := fmt.Sprintf("reader-%d", i)
		if i < len(sources) {
			source = sources[i]
		}
		if _, err := m.AddReader(source, r); err != nil {
			return nil, fmt.Errorf("add reader %s: %w", source, err)
		}
	}
	return m.Merge(), nil
}