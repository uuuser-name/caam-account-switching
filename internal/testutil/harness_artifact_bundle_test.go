package testutil

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestHarness_CloseEmitsArtifactBundle(t *testing.T) {
	h := NewHarness(t)
	h.SetEnv("CAAM_E2E_ARTIFACT_ROOT", t.TempDir())

	h.Log.SetStep("legacy_step")
	h.Log.Info("legacy log entry", map[string]interface{}{"kind": "legacy"})

	runID := h.Canon.RunID()
	paths := h.artifactBundlePaths(runID)
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
	if !strings.Contains(string(canonical), "legacy_step") {
		t.Fatalf("canonical bundle missing legacy step event: %s", canonical)
	}

	transcript, err := os.ReadFile(paths.TranscriptPath)
	if err != nil {
		t.Fatalf("ReadFile(transcript): %v", err)
	}
	if !strings.Contains(string(transcript), "legacy log entry") {
		t.Fatalf("raw transcript missing legacy log entry: %s", transcript)
	}

	reportBytes, err := os.ReadFile(paths.ReportPath)
	if err != nil {
		t.Fatalf("ReadFile(report): %v", err)
	}
	var report map[string]interface{}
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatalf("Unmarshal(report): %v", err)
	}
	if report["test_name"] != t.Name() {
		t.Fatalf("report test name = %v, want %q", report["test_name"], t.Name())
	}

	replayBytes, err := os.ReadFile(paths.ReplayHintsPath)
	if err != nil {
		t.Fatalf("ReadFile(replay): %v", err)
	}
	var replay ReplayHints
	if err := json.Unmarshal(replayBytes, &replay); err != nil {
		t.Fatalf("Unmarshal(replay): %v", err)
	}
	if replay.RunID != runID {
		t.Fatalf("replay run_id = %q, want %q", replay.RunID, runID)
	}
	if len(replay.Commands) == 0 || !strings.Contains(replay.Commands[0], "go test . -run '^") {
		t.Fatalf("replay commands missing go test hint: %+v", replay.Commands)
	}
}
