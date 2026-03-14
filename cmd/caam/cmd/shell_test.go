package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateBashInit(t *testing.T) {
	caamPath := "/usr/local/bin/caam"
	tools := []string{"claude", "codex", "gemini", "openclaw"}

	output := generateBashInit(caamPath, tools, false)

	if !strings.Contains(output, "_caam_shim_dir") {
		t.Error("Missing CAAM shim directory setup")
	}
	if !strings.Contains(output, "unalias claude codex gemini openclaw") {
		t.Error("Missing stale-alias cleanup")
	}
	if !strings.Contains(output, "unset -f claude codex gemini openclaw") {
		t.Error("Missing stale-function cleanup")
	}
	if !strings.Contains(output, "for _caam_tool in claude codex gemini openclaw") {
		t.Error("Missing shim install loop")
	}
	if !strings.Contains(output, "ln -s \"$_caam_path\" \"$_caam_shim_dir/$_caam_tool\"") {
		t.Error("Missing shim symlink install command")
	}
	if !strings.Contains(output, "export PATH=\"$_caam_shim_dir:$PATH\"") {
		t.Error("Missing PATH prepend for shims")
	}
	if strings.Contains(output, "codex()") {
		t.Error("Should not emit shell wrapper functions when shims are enabled")
	}

	// Check for completion
	if !strings.Contains(output, "_caam_completions") {
		t.Error("Missing completion function")
	}
	if !strings.Contains(output, "complete -F _caam_completions caam") {
		t.Error("Missing completion registration")
	}
}

func TestGenerateBashInit_NoWrap(t *testing.T) {
	caamPath := "/usr/local/bin/caam"
	tools := []string{"claude", "codex", "gemini", "openclaw"}

	output := generateBashInit(caamPath, tools, true)

	// Should NOT have shim installation
	if strings.Contains(output, "_caam_shim_dir") {
		t.Error("Should not have shim setup when noWrap=true")
	}

	// Should still have completions
	if !strings.Contains(output, "_caam_completions") {
		t.Error("Missing completion function even with noWrap=true")
	}
}

func TestGenerateBashInit_CustomTools(t *testing.T) {
	caamPath := "/usr/local/bin/caam"
	tools := []string{"claude"} // Only claude

	output := generateBashInit(caamPath, tools, false)

	if !strings.Contains(output, "for _caam_tool in claude") {
		t.Error("Missing claude shim loop")
	}
	if strings.Contains(output, "unalias claude codex") {
		t.Error("Should not clean up codex alias when only claude is requested")
	}
	if strings.Contains(output, "for _caam_tool in claude codex") {
		t.Error("Should not have codex shim when only claude is requested")
	}
	if strings.Contains(output, "unset -f claude codex") {
		t.Error("Should not clean up codex when only claude is requested")
	}
}

func TestGenerateFishInit(t *testing.T) {
	caamPath := "/usr/local/bin/caam"
	tools := []string{"claude", "codex", "gemini", "openclaw"}

	output := generateFishInit(caamPath, tools, false)

	if !strings.Contains(output, "set -l _caam_shim_dir") {
		t.Error("Missing fish shim directory setup")
	}
	if !strings.Contains(output, "for _caam_tool in claude codex gemini openclaw") {
		t.Error("Missing fish shim install loop")
	}
	if !strings.Contains(output, "functions -q $_caam_tool; and functions -e $_caam_tool") {
		t.Error("Missing fish stale-function cleanup")
	}
	if !strings.Contains(output, "ln -s \"$_caam_path\" \"$_caam_target\"") {
		t.Error("Missing fish shim symlink install command")
	}
	if !strings.Contains(output, "set -gx PATH \"$_caam_shim_dir\" $PATH") {
		t.Error("Missing fish PATH prepend for shims")
	}
	if strings.Contains(output, "function codex") {
		t.Error("Should not emit fish wrapper functions when shims are enabled")
	}

	// Check for fish completion syntax
	if !strings.Contains(output, "complete -c caam") {
		t.Error("Missing fish completion")
	}
}

func TestGenerateFishInit_NoWrap(t *testing.T) {
	caamPath := "/usr/local/bin/caam"
	tools := []string{"claude"}

	output := generateFishInit(caamPath, tools, true)

	// Should NOT have shim installation
	if strings.Contains(output, "_caam_shim_dir") {
		t.Error("Should not have shim setup when noWrap=true")
	}

	// Should still have completions
	if !strings.Contains(output, "complete -c caam") {
		t.Error("Missing fish completion even with noWrap=true")
	}
}

func TestResolveShellInitTarget(t *testing.T) {
	t.Setenv("SHELL", "/bin/fish")

	if got := resolveShellInitTarget(false, false, true, false); got != "zsh" {
		t.Fatalf("resolveShellInitTarget(... zsh ...) = %q, want zsh", got)
	}
	if got := resolveShellInitTarget(false, true, false, false); got != "bash" {
		t.Fatalf("resolveShellInitTarget(... bash ...) = %q, want bash", got)
	}
	if got := resolveShellInitTarget(false, false, false, true); got != "sh" {
		t.Fatalf("resolveShellInitTarget(... posix ...) = %q, want sh", got)
	}
	if got := resolveShellInitTarget(false, false, false, false); got != "fish" {
		t.Fatalf("resolveShellInitTarget(... default ...) = %q, want fish", got)
	}
}

func TestGenerateZshInit_AvoidsBashCompletionSyntax(t *testing.T) {
	output := generateZshInit("/usr/local/bin/caam", []string{"codex"}, false)
	if strings.Contains(output, "_init_completion") || strings.Contains(output, "complete -F") {
		t.Fatal("zsh init should not emit bash completion helpers")
	}
	if !strings.Contains(output, "shell completion zsh") {
		t.Fatal("zsh init should load zsh completion")
	}
}

func TestGeneratePOSIXInit_AvoidsBashSpecificSyntax(t *testing.T) {
	output := generatePOSIXInit("/usr/local/bin/caam", []string{"codex"}, false)
	if strings.Contains(output, "_init_completion") || strings.Contains(output, "complete -F") || strings.Contains(output, "[[") {
		t.Fatal("POSIX init should not emit bash-specific completion syntax")
	}
}

func TestGenerateZshInit_SourcesCleanly(t *testing.T) {
	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available")
	}

	tmp := t.TempDir()
	initFile := filepath.Join(tmp, "caam.zsh")
	if err := os.WriteFile(initFile, []byte(generateZshInit("/bin/echo", []string{"codex"}, false)), 0o600); err != nil {
		t.Fatalf("write init file: %v", err)
	}

	cmd := exec.Command(zshPath, initFile)
	cmd.Env = append(os.Environ(), "HOME="+tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zsh init failed: %v\n%s", err, out)
	}
}

func TestGeneratePOSIXInit_SourcesCleanly(t *testing.T) {
	tmp := t.TempDir()
	initFile := filepath.Join(tmp, "caam.sh")
	if err := os.WriteFile(initFile, []byte(generatePOSIXInit("/bin/echo", []string{"codex"}, false)), 0o600); err != nil {
		t.Fatalf("write init file: %v", err)
	}

	cmd := exec.Command("sh", initFile)
	cmd.Env = append(os.Environ(), "HOME="+tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("posix init failed: %v\n%s", err, out)
	}
}

func TestDetectShell(t *testing.T) {
	// This test just ensures detectShell returns a valid shell name
	shell := detectShell()

	validShells := map[string]bool{
		"bash": true,
		"zsh":  true,
		"fish": true,
		"sh":   true,
	}

	if !validShells[shell] {
		t.Errorf("detectShell() returned unexpected value: %s", shell)
	}
}

func TestShellInitCommand(t *testing.T) {
	// Test that the command exists and can be executed
	cmd := shellInitCmd

	if cmd.Use != "init" {
		t.Errorf("shellInitCmd.Use = %s, want 'init'", cmd.Use)
	}

	// Check flags exist
	if cmd.Flags().Lookup("fish") == nil {
		t.Error("Missing --fish flag")
	}
	if cmd.Flags().Lookup("bash") == nil {
		t.Error("Missing --bash flag")
	}
	if cmd.Flags().Lookup("zsh") == nil {
		t.Error("Missing --zsh flag")
	}
	if cmd.Flags().Lookup("no-wrap") == nil {
		t.Error("Missing --no-wrap flag")
	}
	if cmd.Flags().Lookup("tools") == nil {
		t.Error("Missing --tools flag")
	}
}

func TestShellCompletionCommand(t *testing.T) {
	cmd := shellCompletionCmd

	if cmd.Use != "completion [bash|zsh|fish|powershell]" {
		t.Errorf("shellCompletionCmd.Use = %s", cmd.Use)
	}

	// Check valid args
	validArgs := cmd.ValidArgs
	expected := []string{"bash", "zsh", "fish", "powershell"}

	if len(validArgs) != len(expected) {
		t.Errorf("ValidArgs length = %d, want %d", len(validArgs), len(expected))
	}

	for _, arg := range expected {
		found := false
		for _, v := range validArgs {
			if v == arg {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing valid arg: %s", arg)
		}
	}
}

func TestShellInitOutput(t *testing.T) {
	// Capture stdout by running the command
	var buf bytes.Buffer
	cmd := shellInitCmd
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Reset flags
	require.NoError(t, cmd.Flags().Set("tools", "claude"))
	require.NoError(t, cmd.Flags().Set("no-wrap", "false"))

	// We can't easily test runShellInit directly since it writes to stdout,
	// but we can test the generators
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path no quoting needed",
			input:    "/usr/local/bin/caam",
			expected: "/usr/local/bin/caam",
		},
		{
			name:     "path with spaces",
			input:    "/path with spaces/caam",
			expected: "'/path with spaces/caam'",
		},
		{
			name:     "path with single quote",
			input:    "/path'quote/caam",
			expected: "'/path'\"'\"'quote/caam'",
		},
		{
			name:     "path with dollar sign",
			input:    "/path$var/caam",
			expected: "'/path$var/caam'",
		},
		{
			name:     "path with backtick",
			input:    "/path`cmd`/caam",
			expected: "'/path`cmd`/caam'",
		},
		{
			name:     "path with semicolon (injection attempt)",
			input:    "/path; rm -rf /;/caam",
			expected: "'/path; rm -rf /;/caam'",
		},
		{
			name:     "path with pipe (injection attempt)",
			input:    "/path | cat /etc/passwd/caam",
			expected: "'/path | cat /etc/passwd/caam'",
		},
		{
			name:     "path with ampersand",
			input:    "/path && evil/caam",
			expected: "'/path && evil/caam'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFishQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path no quoting needed",
			input:    "/usr/local/bin/caam",
			expected: "/usr/local/bin/caam",
		},
		{
			name:     "path with spaces",
			input:    "/path with spaces/caam",
			expected: "'/path with spaces/caam'",
		},
		{
			name:     "path with single quote",
			input:    "/path'quote/caam",
			expected: "'/path\\'quote/caam'",
		},
		{
			name:     "path with dollar sign",
			input:    "/path$var/caam",
			expected: "'/path$var/caam'",
		},
		{
			name:     "path with semicolon",
			input:    "/path; rm -rf /;/caam",
			expected: "'/path; rm -rf /;/caam'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fishQuote(tt.input)
			if got != tt.expected {
				t.Errorf("fishQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGenerateBashInit_PathWithSpaces(t *testing.T) {
	// This tests that paths with special characters are properly quoted
	caamPath := "/path with spaces/caam"
	tools := []string{"claude"}

	output := generateBashInit(caamPath, tools, false)

	// Should use quoted path
	if !strings.Contains(output, "'/path with spaces/caam'") {
		t.Error("Path with spaces should be single-quoted in output")
	}
}

func TestGenerateFishInit_PathWithSpaces(t *testing.T) {
	// This tests that paths with special characters are properly quoted
	caamPath := "/path with spaces/caam"
	tools := []string{"claude"}

	output := generateFishInit(caamPath, tools, false)

	// Should use quoted path
	if !strings.Contains(output, "'/path with spaces/caam'") {
		t.Error("Path with spaces should be single-quoted in output")
	}
}
