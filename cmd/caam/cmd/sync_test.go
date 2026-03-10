package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"github.com/spf13/cobra"
)

// Test helper functions that are exported

func TestGetStatusIcon(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{sync.StatusOnline, "🟢"},
		{sync.StatusOffline, "🔴"},
		{sync.StatusSyncing, "🔄"},
		{sync.StatusError, "⚠️"},
		{sync.StatusUnknown, "⚪"},
		{"other", "⚪"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := getStatusIcon(tt.status)
			if got != tt.expected {
				t.Errorf("getStatusIcon(%q) = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}

func TestFormatTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		ago      time.Duration
		expected string
	}{
		{"just now", 30 * time.Second, "just now"},
		{"1 minute", 1 * time.Minute, "1 min ago"},
		{"5 minutes", 5 * time.Minute, "5 mins ago"},
		{"1 hour", 1 * time.Hour, "1 hour ago"},
		{"3 hours", 3 * time.Hour, "3 hours ago"},
		{"1 day", 25 * time.Hour, "1 day ago"},
		{"3 days", 72 * time.Hour, "3 days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create time that was tt.ago in the past
			tm := time.Now().Add(-tt.ago)
			got := formatTimeAgo(tm)
			if got != tt.expected {
				t.Errorf("formatTimeAgo() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestSyncCommands tests that all sync subcommands are registered.
func TestSyncCommands(t *testing.T) {
	subcommands := []string{
		"init",
		"status",
		"add",
		"remove",
		"test",
		"enable",
		"disable",
		"log",
		"discover",
		"queue",
		"edit",
	}

	for _, name := range subcommands {
		t.Run(name, func(t *testing.T) {
			found := false
			for _, cmd := range syncCmd.Commands() {
				if cmd.Name() == name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("subcommand %q not found", name)
			}
		})
	}
}

// TestSyncCmdFlags tests that all expected flags are present.
func TestSyncCmdFlags(t *testing.T) {
	flags := []string{
		"machine",
		"provider",
		"profile",
		"dry-run",
		"force",
		"json",
	}

	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) {
			if syncCmd.Flags().Lookup(flag) == nil {
				t.Errorf("flag --%s not found", flag)
			}
		})
	}
}

// TestSyncInitCmdFlags tests sync init command flags.
func TestSyncInitCmdFlags(t *testing.T) {
	flags := []string{
		"discover",
		"csv",
	}

	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) {
			if syncInitCmd.Flags().Lookup(flag) == nil {
				t.Errorf("flag --%s not found", flag)
			}
		})
	}
}

// TestSyncAddCmdFlags tests sync add command flags.
func TestSyncAddCmdFlags(t *testing.T) {
	flags := []string{
		"key",
		"user",
		"remote-path",
		"test",
	}

	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) {
			if syncAddCmd.Flags().Lookup(flag) == nil {
				t.Errorf("flag --%s not found", flag)
			}
		})
	}
}

// TestSyncLogCmdFlags tests sync log command flags.
func TestSyncLogCmdFlags(t *testing.T) {
	flags := []string{
		"limit",
		"machine",
		"provider",
		"errors",
		"json",
	}

	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) {
			if syncLogCmd.Flags().Lookup(flag) == nil {
				t.Errorf("flag --%s not found", flag)
			}
		})
	}
}

// TestSyncQueueCmdFlags tests sync queue command flags.
func TestSyncQueueCmdFlags(t *testing.T) {
	flags := []string{
		"clear",
		"process",
		"json",
	}

	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) {
			if syncQueueCmd.Flags().Lookup(flag) == nil {
				t.Errorf("flag --%s not found", flag)
			}
		})
	}
}

// TestSyncStatusJSONOutput tests the JSON output helper.
func TestSyncStatusJSONOutput(t *testing.T) {
	state := sync.NewSyncState(t.TempDir())

	var buf bytes.Buffer
	err := runSyncStatusJSON(state, &buf)
	if err != nil {
		t.Fatalf("runSyncStatusJSON: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "auto_sync") {
		t.Errorf("JSON output missing auto_sync field")
	}
	if !strings.Contains(output, "machines") {
		t.Errorf("JSON output missing machines field")
	}
}

func TestRemoteVaultPath(t *testing.T) {
	defaultPath := sync.DefaultSyncerConfig().RemoteVaultPath

	if got := remoteVaultPath(nil); got != defaultPath {
		t.Fatalf("remoteVaultPath(nil) = %q, want %q", got, defaultPath)
	}

	if got := remoteVaultPath(&sync.Machine{}); got != defaultPath {
		t.Fatalf("remoteVaultPath(empty) = %q, want %q", got, defaultPath)
	}

	custom := &sync.Machine{RemotePath: "/data/caam"}
	if got := remoteVaultPath(custom); got != "/data/caam/vault" {
		t.Fatalf("remoteVaultPath(custom) = %q, want %q", got, "/data/caam/vault")
	}
}

func TestParseEditorCommandSupportsArguments(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	editorPath, editorArgs, err := parseEditorCommand(exe + " --wait")
	if err != nil {
		t.Fatalf("parseEditorCommand: %v", err)
	}
	if editorPath != exe {
		t.Fatalf("editorPath = %q, want %q", editorPath, exe)
	}
	if len(editorArgs) != 1 || editorArgs[0] != "--wait" {
		t.Fatalf("editorArgs = %#v, want [--wait]", editorArgs)
	}
}

func TestResolveEditorCommandPrefersEditorEnv(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	t.Setenv("EDITOR", exe+" --flag")
	t.Setenv("VISUAL", "")

	editorPath, editorArgs, err := resolveEditorCommand()
	if err != nil {
		t.Fatalf("resolveEditorCommand: %v", err)
	}
	if editorPath != exe {
		t.Fatalf("editorPath = %q, want %q", editorPath, exe)
	}
	if len(editorArgs) != 1 || editorArgs[0] != "--flag" {
		t.Fatalf("editorArgs = %#v, want [--flag]", editorArgs)
	}
}

func TestSanitizeTerminalTextStripsEscapeAndControlSequences(t *testing.T) {
	raw := "work-\x1b[31mlaptop\x1b[0m\x1b]52;c;ZGF0YQ==\a\n\t\u202E"
	if got := sanitizeTerminalText(raw); got != "work-laptop" {
		t.Fatalf("sanitizeTerminalText() = %q, want %q", got, "work-laptop")
	}
}

func TestRunSyncDryRunSanitizesMachineDisplay(t *testing.T) {
	setupSyncHome(t)
	loadAndSaveState(t, func(state *sync.SyncState) {
		m := sync.NewMachine("work-\x1b[31mlaptop\x1b[0m", "10.0.0.2\x1b]52;c;ZGF0YQ==\a\n")
		if err := state.Pool.AddMachine(m); err != nil {
			t.Fatalf("add machine: %v", err)
		}
	})

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.Flags().Bool("dry-run", true, "")
	cmd.Flags().String("machine", "", "")

	if err := runSync(cmd, nil); err != nil {
		t.Fatalf("runSync failed: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\a") {
		t.Fatalf("unexpected terminal escape sequence in output: %q", got)
	}
	if !strings.Contains(got, "work-laptop (10.0.0.2)") {
		t.Fatalf("expected sanitized machine output, got: %q", got)
	}
}
