package testutil

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExtendedHarness_CloseEmitsArtifactBundle(t *testing.T) {
	h := NewExtendedHarness(t)
	h.SetEnv("CAAM_E2E_ARTIFACT_ROOT", t.TempDir())

	h.StartStep("bundle", "emit durable artifact bundle")
	h.LogInfo("bundle log entry", "key", "value")
	h.RecordMetric("bundle_metric", 25*time.Millisecond)
	h.EndStep("bundle")

	paths := h.artifactBundlePaths()
	h.Close()

	requiredPaths := []string{
		paths.CanonicalPath,
		paths.TranscriptPath,
		paths.SummaryPath,
		paths.ReportPath,
		paths.ReplayHintsPath,
	}
	for _, path := range requiredPaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
	}

	canonical, err := os.ReadFile(paths.CanonicalPath)
	if err != nil {
		t.Fatalf("ReadFile(canonical): %v", err)
	}
	if !strings.Contains(string(canonical), "bundle-start") {
		t.Fatalf("canonical bundle missing step start event: %s", canonical)
	}

	transcript, err := os.ReadFile(paths.TranscriptPath)
	if err != nil {
		t.Fatalf("ReadFile(transcript): %v", err)
	}
	if !strings.Contains(string(transcript), "bundle log entry") {
		t.Fatalf("raw transcript missing expected log entry: %s", transcript)
	}

	summary, err := os.ReadFile(paths.SummaryPath)
	if err != nil {
		t.Fatalf("ReadFile(summary): %v", err)
	}
	if !strings.Contains(string(summary), t.Name()) {
		t.Fatalf("summary missing test name: %s", summary)
	}

	reportBytes, err := os.ReadFile(paths.ReportPath)
	if err != nil {
		t.Fatalf("ReadFile(report): %v", err)
	}
	var report ExportReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatalf("Unmarshal(report): %v", err)
	}
	if report.TestName != t.Name() {
		t.Fatalf("report test name = %q, want %q", report.TestName, t.Name())
	}
	if report.StepCount != 1 {
		t.Fatalf("report step count = %d, want 1", report.StepCount)
	}

	replayBytes, err := os.ReadFile(paths.ReplayHintsPath)
	if err != nil {
		t.Fatalf("ReadFile(replay): %v", err)
	}
	var replay ReplayHints
	if err := json.Unmarshal(replayBytes, &replay); err != nil {
		t.Fatalf("Unmarshal(replay): %v", err)
	}
	if replay.RunID != h.runID {
		t.Fatalf("replay run_id = %q, want %q", replay.RunID, h.runID)
	}
	if replay.ScenarioID != h.scenarioID {
		t.Fatalf("replay scenario_id = %q, want %q", replay.ScenarioID, h.scenarioID)
	}
	if len(replay.Commands) == 0 || !strings.Contains(replay.Commands[0], "go test . -run '^") {
		t.Fatalf("replay commands missing go test hint: %+v", replay.Commands)
	}
	if replay.ArtifactPaths.CanonicalPath != paths.CanonicalPath {
		t.Fatalf("replay canonical path = %q, want %q", replay.ArtifactPaths.CanonicalPath, paths.CanonicalPath)
	}
}
