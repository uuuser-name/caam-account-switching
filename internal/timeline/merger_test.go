package timeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTimelineJSON(runID, scenarioID, stepID, ts string) string {
	return `{"run_id":"` + runID + `","scenario_id":"` + scenarioID + `","step_id":"` + stepID + `","timestamp":"` + ts + `","actor":"ci","component":"test","input_redacted":{},"output":{},"decision":"pass","duration_ms":100,"error":{"present":false,"code":"","message":"","details":{}}}`
}

func TestMerger_NewEmpty(t *testing.T) {
	m := NewMerger()
	if m == nil {
		t.Fatal("expected merger instance")
	}
	if got := m.EntryCount(); got != 0 {
		t.Fatalf("expected empty merger, got %d entries", got)
	}
	if len(m.ParseErrors()) != 0 {
		t.Fatalf("expected no parse errors on new merger")
	}
}

func TestMerger_AddReaderAndParseErrors(t *testing.T) {
	m := NewMerger()
	jsonl := strings.Join([]string{
		makeTimelineJSON("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z"),
		"{bad json",
		makeTimelineJSON("run-1", "scenario-a", "step-2", "2024-01-01T10:00:01Z"),
	}, "\n")

	added, err := m.AddReader("test.jsonl", strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("AddReader failed: %v", err)
	}
	if added != 2 {
		t.Fatalf("expected 2 parsed entries, got %d", added)
	}
	if m.EntryCount() != 2 {
		t.Fatalf("expected 2 total entries, got %d", m.EntryCount())
	}
	if len(m.ParseErrors()) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(m.ParseErrors()))
	}
	if m.ParseErrors()[0].Line != 2 {
		t.Fatalf("expected parse error on line 2, got %d", m.ParseErrors()[0].Line)
	}
}

func TestMerger_MergeGroupsAndSortsDeterministically(t *testing.T) {
	m := NewMerger()
	entries := []*TimelineEntry{
		{RunID: "run-1", ScenarioID: "scenario-a", StepID: "step-3", Timestamp: "2024-01-01T10:00:01Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:01Z")},
		{RunID: "run-1", ScenarioID: "scenario-a", StepID: "step-1", Timestamp: "2024-01-01T10:00:00Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:00Z")},
		{RunID: "run-1", ScenarioID: "scenario-a", StepID: "step-2", Timestamp: "2024-01-01T10:00:01Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:01Z")},
		{RunID: "run-1", ScenarioID: "scenario-b", StepID: "step-1", Timestamp: "2024-01-01T10:00:00Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:00Z")},
	}
	m.AddEntries(entries)

	merged := m.Merge()
	if len(merged) != 2 {
		t.Fatalf("expected 2 scenario timelines, got %d", len(merged))
	}

	if merged[0].ScenarioID != "scenario-a" || merged[1].ScenarioID != "scenario-b" {
		t.Fatalf("expected deterministic group ordering by scenario_id, got %s then %s", merged[0].ScenarioID, merged[1].ScenarioID)
	}

	a := merged[0]
	if len(a.Entries) != 3 {
		t.Fatalf("expected 3 entries for scenario-a, got %d", len(a.Entries))
	}
	if a.Entries[0].StepID != "step-1" || a.Entries[1].StepID != "step-2" || a.Entries[2].StepID != "step-3" {
		t.Fatalf("unexpected deterministic in-group order: %s, %s, %s", a.Entries[0].StepID, a.Entries[1].StepID, a.Entries[2].StepID)
	}
}

func TestMerger_DeterministicAcrossInputOrder(t *testing.T) {
	m1 := NewMerger()
	m2 := NewMerger()

	base := []*TimelineEntry{
		{RunID: "run-1", ScenarioID: "scenario-a", StepID: "step-3", Timestamp: "2024-01-01T10:00:02Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:02Z")},
		{RunID: "run-1", ScenarioID: "scenario-a", StepID: "step-1", Timestamp: "2024-01-01T10:00:00Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:00Z")},
		{RunID: "run-1", ScenarioID: "scenario-a", StepID: "step-2", Timestamp: "2024-01-01T10:00:01Z", ParsedTimestamp: mustParseTime("2024-01-01T10:00:01Z")},
	}

	reversed := []*TimelineEntry{base[2], base[0], base[1]}
	m1.AddEntries(base)
	m2.AddEntries(reversed)

	out1 := m1.Merge()
	out2 := m2.Merge()
	if len(out1) != 1 || len(out2) != 1 {
		t.Fatalf("expected one timeline in each result")
	}

	if out1[0].ToJSONL() != out2[0].ToJSONL() {
		t.Fatalf("determinism violation: same semantic events produced different JSONL output")
	}
	if out1[0].Metadata.Hash != out2[0].Metadata.Hash {
		t.Fatalf("determinism violation: hashes differ (%s vs %s)", out1[0].Metadata.Hash, out2[0].Metadata.Hash)
	}
}

func TestMergeFiles_Convenience(t *testing.T) {
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "a.jsonl")
	f2 := filepath.Join(tmp, "b.jsonl")

	if err := os.WriteFile(f1, []byte(makeTimelineJSON("run-1", "scenario-a", "step-1", "2024-01-01T10:00:00Z")+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", f1, err)
	}
	if err := os.WriteFile(f2, []byte(makeTimelineJSON("run-1", "scenario-b", "step-1", "2024-01-01T10:00:01Z")+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", f2, err)
	}

	merged, err := MergeFiles(f1, f2)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 timelines from 2 scenarios, got %d", len(merged))
	}
}
