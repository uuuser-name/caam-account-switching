package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalLoggerSetOutputPathSupportsRelativePathAndDisable(t *testing.T) {
	tmpDir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	logger := NewCanonicalLogger(CanonicalLoggerConfig{ScenarioID: "scenario"})
	t.Cleanup(func() {
		_ = logger.Close()
	})

	if err := logger.SetOutputPath("events.jsonl"); err != nil {
		t.Fatalf("SetOutputPath(relative): %v", err)
	}
	if err := logger.LogStep("first-step", ComponentTest, DecisionPass, 1, nil, map[string]interface{}{"ok": true}, nil); err != nil {
		t.Fatalf("LogStep(first): %v", err)
	}

	if err := logger.SetOutputPath(""); err != nil {
		t.Fatalf("SetOutputPath(disable): %v", err)
	}
	if err := logger.LogStep("second-step", ComponentTest, DecisionPass, 1, nil, map[string]interface{}{"ok": true}, nil); err != nil {
		t.Fatalf("LogStep(second): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(events.jsonl): %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "first-step") {
		t.Fatalf("expected first-step in canonical log file, got %q", content)
	}
	if strings.Contains(content, "second-step") {
		t.Fatalf("expected disabled output path to stop file writes, got %q", content)
	}
}

func TestNewCanonicalLoggerHonorsConfiguredOutputPath(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")

	logger := NewCanonicalLogger(CanonicalLoggerConfig{
		ScenarioID: "scenario",
		OutputPath: logPath,
	})
	t.Cleanup(func() {
		_ = logger.Close()
	})

	if err := logger.LogStep("configured-output", ComponentTest, DecisionPass, 1, nil, map[string]interface{}{"ok": true}, nil); err != nil {
		t.Fatalf("LogStep: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(events.jsonl): %v", err)
	}
	if !strings.Contains(string(data), "configured-output") {
		t.Fatalf("expected configured output path to receive events, got %q", string(data))
	}
}
