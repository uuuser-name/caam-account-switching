package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddCodexConfigRepairHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CODEX_REPAIR_HELPER") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 || args[0] != "codex" {
		os.Exit(2)
	}

	os.Exit(1)
}

func TestRunToolLoginRepairsMalformedCodexConfigBeforeLaunch(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatalf("MkdirAll codex home: %v", err)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	initial := strings.Join([]string{
		"[notice]",
		"hide_full_access_warning = true",
		"hide_rate_limit_model_nudge = true[notice.model_migrations]",
		`"gpt-5.2" = "gpt-5.2"`,
		"hide_rate_limit_model_nudge = true[assistant_principles]",
		`values = ["x"]`,
		"hide_rate_limit_model_nudge = true",
		"[features]multi_agent = true",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("Write config.toml: %v", err)
	}

	originalExecCommand := execCommand
	defer func() {
		execCommand = originalExecCommand
	}()

	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestAddCodexConfigRepairHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_CODEX_REPAIR_HELPER=1")
		return cmd
	}

	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)

	err := runToolLogin(context.Background(), "codex", false)
	if err == nil {
		t.Fatal("runToolLogin() unexpectedly succeeded")
	}

	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("Read repaired config.toml: %v", readErr)
	}
	text := string(data)
	if strings.Contains(text, "hide_rate_limit_model_nudge = true[notice.model_migrations]") {
		t.Fatalf("notice.model_migrations boundary still collapsed:\n%s", text)
	}
	if strings.Contains(text, "hide_rate_limit_model_nudge = true[assistant_principles]") {
		t.Fatalf("assistant_principles boundary still collapsed:\n%s", text)
	}
	if strings.Contains(text, "[features]multi_agent = true") {
		t.Fatalf("inline [features] multi_agent still present:\n%s", text)
	}
	if !strings.Contains(text, "hide_rate_limit_model_nudge = true\n[notice.model_migrations]") {
		t.Fatalf("expected repaired notice.model_migrations boundary:\n%s", text)
	}
	if !strings.Contains(text, "\n[assistant_principles]\nvalues = [\"x\"]\n") {
		t.Fatalf("expected standalone assistant_principles table:\n%s", text)
	}
	if !strings.Contains(text, "\n[features]\nmulti_agent = true\n") {
		t.Fatalf("expected canonical [features] table:\n%s", text)
	}
	if strings.Count(text, "hide_rate_limit_model_nudge = true") != 1 {
		t.Fatalf("expected one canonical rate-limit nudge setting:\n%s", text)
	}
}
