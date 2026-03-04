package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/coordinator"
)

func TestLoadCoordinatorConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "coordinator.json")

	data := []byte(`{
  "port": 9999,
  "poll_interval": "750ms",
  "auth_timeout": "45s",
  "state_timeout": "15s",
  "resume_prompt": "resume now",
  "output_lines": 55,
  "backend": "tmux"
}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, port, err := loadCoordinatorConfig(path)
	if err != nil {
		t.Fatalf("loadCoordinatorConfig error: %v", err)
	}
	if port != 9999 {
		t.Fatalf("port = %d, want 9999", port)
	}
	if cfg.PollInterval != 750*time.Millisecond {
		t.Fatalf("PollInterval = %v, want 750ms", cfg.PollInterval)
	}
	if cfg.AuthTimeout != 45*time.Second {
		t.Fatalf("AuthTimeout = %v, want 45s", cfg.AuthTimeout)
	}
	if cfg.StateTimeout != 15*time.Second {
		t.Fatalf("StateTimeout = %v, want 15s", cfg.StateTimeout)
	}
	if cfg.ResumePrompt != "resume now" {
		t.Fatalf("ResumePrompt = %q, want %q", cfg.ResumePrompt, "resume now")
	}
	if cfg.OutputLines != 55 {
		t.Fatalf("OutputLines = %d, want 55", cfg.OutputLines)
	}
	if cfg.Backend != coordinator.BackendTmux {
		t.Fatalf("Backend = %s, want %s", cfg.Backend, coordinator.BackendTmux)
	}
}
