package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// shellCmd is the parent command for shell integration.
var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Shell integration commands",
	Long: `Commands for integrating caam with your shell.

Use 'caam shell-init' to set up automatic profile management.`,
}

// shellInitCmd outputs shell initialization code.
var shellInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Output shell initialization code",
	Long: `Outputs shell initialization code that installs CAAM-owned shim commands.

Add this to your shell's rc file:

  # For bash (~/.bashrc):
  eval "$(caam shell init)"

  # For zsh (~/.zshrc):
  eval "$(caam shell init)"

  # For fish (~/.config/fish/config.fish):
  caam shell init --fish | source

This installs shim commands for claude, codex, gemini, and openclaw that:
- Automatically use the best available profile
- Handle rate limits transparently
- Record usage for analytics
- Route openclaw through codex-compatible account switching

It also removes stale shell functions and aliases with those names so codex, claude,
and the other tool commands resolve to the CAAM-managed shims on PATH.

After setup, just use the tools normally:
  claude "explain this code"
  codex "write tests"
  gemini "summarize this file"
  openclaw "continue implementation"

Rate limits are handled automatically!`,
	RunE: runShellInit,
}

func init() {
	shellInitCmd.Flags().Bool("fish", false, "output fish shell syntax")
	shellInitCmd.Flags().Bool("bash", false, "output bash syntax (default)")
	shellInitCmd.Flags().Bool("zsh", false, "output zsh syntax")
	shellInitCmd.Flags().Bool("posix", false, "output POSIX shell syntax")
	shellInitCmd.Flags().Bool("no-wrap", false, "disable tool wrapping (only completions)")
	shellInitCmd.Flags().String("tools", "claude,codex,gemini,openclaw", "comma-separated list of tools to wrap")

	shellCmd.AddCommand(shellInitCmd)
	rootCmd.AddCommand(shellCmd)
}

func runShellInit(cmd *cobra.Command, args []string) error {
	fish, _ := cmd.Flags().GetBool("fish")
	bash, _ := cmd.Flags().GetBool("bash")
	zsh, _ := cmd.Flags().GetBool("zsh")
	posix, _ := cmd.Flags().GetBool("posix")
	noWrap, _ := cmd.Flags().GetBool("no-wrap")
	toolsStr, _ := cmd.Flags().GetString("tools")

	// Parse tools list
	var tools []string
	for _, t := range strings.Split(toolsStr, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tools = append(tools, t)
		}
	}

	shell := resolveShellInitTarget(fish, bash, zsh, posix)

	// Find caam binary path
	caamPath, err := findCaamPath()
	if err != nil {
		caamPath = "caam" // Fallback to PATH lookup
	}

	// Generate output
	var output string
	switch shell {
	case "fish":
		output = generateFishInit(caamPath, tools, noWrap)
	case "zsh":
		output = generateZshInit(caamPath, tools, noWrap)
	case "sh":
		output = generatePOSIXInit(caamPath, tools, noWrap)
	default:
		output = generateBashInit(caamPath, tools, noWrap)
	}

	fmt.Fprint(cmd.OutOrStdout(), output)
	return nil
}

func resolveShellInitTarget(fish, bash, zsh, posix bool) string {
	switch {
	case fish:
		return "fish"
	case zsh:
		return "zsh"
	case posix:
		return "sh"
	case bash:
		return "bash"
	default:
		return detectShell()
	}
}

func detectShell() string {
	// Check SHELL environment variable
	shell := os.Getenv("SHELL")
	if shell != "" {
		base := filepath.Base(shell)
		switch base {
		case "fish":
			return "fish"
		case "zsh":
			return "zsh"
		case "bash":
			return "bash"
		}
	}

	// Check parent process on Unix
	if runtime.GOOS != "windows" {
		ppid := os.Getppid()
		procPath := fmt.Sprintf("/proc/%d/comm", ppid)
		if data, err := os.ReadFile(procPath); err == nil {
			name := strings.TrimSpace(string(data))
			switch name {
			case "fish":
				return "fish"
			case "zsh":
				return "zsh"
			case "bash":
				return "bash"
			}
		}
	}

	return "bash" // Default
}

func findCaamPath() (string, error) {
	// Try to find the current executable
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

// shellQuote returns a properly shell-quoted string for bash/zsh.
// This prevents command injection when paths contain spaces or special characters.
func shellQuote(s string) string {
	// If the string contains no special characters, return as-is
	needsQuote := false
	for _, c := range s {
		if c == ' ' || c == '\'' || c == '"' || c == '\\' || c == '$' ||
			c == '`' || c == '!' || c == '*' || c == '?' || c == '[' ||
			c == ']' || c == '(' || c == ')' || c == '{' || c == '}' ||
			c == '|' || c == '&' || c == ';' || c == '<' || c == '>' ||
			c == '\n' || c == '\t' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	// Use single quotes and escape any embedded single quotes
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func generateBashInit(caamPath string, tools []string, noWrap bool) string {
	var sb strings.Builder

	sb.WriteString("# caam shell integration\n")
	sb.WriteString("# Add to your ~/.bashrc or ~/.zshrc:\n")
	sb.WriteString("#   eval \"$(caam shell init)\"\n\n")

	writePOSIXShimInit(&sb, caamPath, tools, noWrap)

	// Completion for caam command
	sb.WriteString(`# caam command completion
_caam_completions() {
  local cur prev words cword
  _init_completion || return

  case "$prev" in
    caam)
      COMPREPLY=($(compgen -W "activate backup clear cooldown delete exec export import init login ls paths profile project refresh reload resume run shell status sync tui uninstall usage" -- "$cur"))
      return
      ;;
    activate|backup|clear|delete|ls|paths|status)
      COMPREPLY=($(compgen -W "claude codex gemini" -- "$cur"))
      return
      ;;
  esac

  # Profile completion for activate
  if [[ "${words[1]}" == "activate" && ${#words[@]} -ge 3 ]]; then
    local tool="${words[2]}"
    local profiles
    profiles=$(caam ls "$tool" 2>/dev/null | tr '\n' ' ')
    COMPREPLY=($(compgen -W "$profiles" -- "$cur"))
    return
  fi
}

complete -F _caam_completions caam
`)

	return sb.String()
}

func generateZshInit(caamPath string, tools []string, noWrap bool) string {
	var sb strings.Builder

	sb.WriteString("# caam shell integration for zsh\n")
	sb.WriteString("# Add to your ~/.zshrc:\n")
	sb.WriteString("#   eval \"$(caam shell init --zsh)\"\n\n")

	writePOSIXShimInit(&sb, caamPath, tools, noWrap)

	sb.WriteString(`# zsh completion
autoload -Uz compinit 2>/dev/null || true
if ! command -v compdef >/dev/null 2>&1; then
  compinit -i >/dev/null 2>&1 || true
fi
if command -v compdef >/dev/null 2>&1; then
  source <(caam shell completion zsh 2>/dev/null) 2>/dev/null || true
fi
`)

	return sb.String()
}

func generatePOSIXInit(caamPath string, tools []string, noWrap bool) string {
	var sb strings.Builder

	sb.WriteString("# caam shell integration for POSIX shells\n")
	sb.WriteString("# Add to your ~/.profile:\n")
	sb.WriteString("#   eval \"$(caam shell init --posix)\"\n\n")

	writePOSIXShimInit(&sb, caamPath, tools, noWrap)
	sb.WriteString("# POSIX shells do not have a portable completion protocol.\n")

	return sb.String()
}

func writePOSIXShimInit(sb *strings.Builder, caamPath string, tools []string, noWrap bool) {
	quotedPath := shellQuote(caamPath)

	if noWrap {
		return
	}

	sb.WriteString("# Install CAAM provider shims and prefer them on PATH\n")
	sb.WriteString(fmt.Sprintf("_caam_path=%s\n", quotedPath))
	sb.WriteString("_caam_shim_dir=\"${CAAM_HOME:-$HOME/.caam}/shims\"\n")
	sb.WriteString("mkdir -p \"$_caam_shim_dir\"\n")
	if len(tools) > 0 {
		sb.WriteString("unalias")
		for _, tool := range tools {
			sb.WriteString(" " + tool)
		}
		sb.WriteString(" 2>/dev/null || true\n")
		sb.WriteString("unset -f")
		for _, tool := range tools {
			sb.WriteString(" " + tool)
		}
		sb.WriteString(" 2>/dev/null || true\n")
		sb.WriteString("for _caam_tool in")
		for _, tool := range tools {
			sb.WriteString(" " + tool)
		}
		sb.WriteString("; do\n")
		sb.WriteString("  if [ \"$(readlink \"$_caam_shim_dir/$_caam_tool\" 2>/dev/null)\" != \"$_caam_path\" ]; then\n")
		sb.WriteString("    rm -f \"$_caam_shim_dir/$_caam_tool\"\n")
		sb.WriteString("    ln -s \"$_caam_path\" \"$_caam_shim_dir/$_caam_tool\"\n")
		sb.WriteString("  fi\n")
		sb.WriteString("done\n")
	}
	sb.WriteString("case \":$PATH:\" in\n")
	sb.WriteString("  *\":$_caam_shim_dir:\"*) ;;\n")
	sb.WriteString("  *) export PATH=\"$_caam_shim_dir:$PATH\" ;;\n")
	sb.WriteString("esac\n")
	sb.WriteString("hash -r 2>/dev/null || true\n")
	sb.WriteString("unset _caam_path _caam_shim_dir _caam_tool\n\n")
}

// fishQuote returns a properly quoted string for fish shell.
func fishQuote(s string) string {
	// If the string contains no special characters, return as-is
	needsQuote := false
	for _, c := range s {
		if c == ' ' || c == '\'' || c == '"' || c == '\\' || c == '$' ||
			c == '(' || c == ')' || c == '{' || c == '}' || c == '[' ||
			c == ']' || c == '*' || c == '?' || c == '~' || c == '#' ||
			c == '|' || c == '&' || c == ';' || c == '<' || c == '>' ||
			c == '\n' || c == '\t' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	// Fish uses single quotes and escapes single quotes with \'
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

func generateFishInit(caamPath string, tools []string, noWrap bool) string {
	var sb strings.Builder

	sb.WriteString("# caam shell integration for fish\n")
	sb.WriteString("# Add to your ~/.config/fish/config.fish:\n")
	sb.WriteString("#   caam shell init --fish | source\n\n")

	// Quote the caam path for safe shell interpolation
	quotedPath := fishQuote(caamPath)

	// Install PATH shims instead of shell functions so direct `codex` invocations
	// are still routed through CAAM even outside the current shell process.
	if !noWrap {
		sb.WriteString("# Install CAAM provider shims and prefer them on PATH\n")
		sb.WriteString(fmt.Sprintf("set -l _caam_path %s\n", quotedPath))
		sb.WriteString("set -l _caam_shim_dir \"$HOME/.caam/shims\"\n")
		sb.WriteString("if set -q CAAM_HOME\n")
		sb.WriteString("  set _caam_shim_dir \"$CAAM_HOME/shims\"\n")
		sb.WriteString("end\n")
		sb.WriteString("mkdir -p \"$_caam_shim_dir\"\n")
		if len(tools) > 0 {
			sb.WriteString("for _caam_tool in")
			for _, tool := range tools {
				sb.WriteString(" " + tool)
			}
			sb.WriteString("\n")
			sb.WriteString("  functions -q $_caam_tool; and functions -e $_caam_tool\n")
			sb.WriteString("  set -l _caam_target \"$_caam_shim_dir/$_caam_tool\"\n")
			sb.WriteString("  set -l _caam_current \"\"\n")
			sb.WriteString("  if test -L \"$_caam_target\"\n")
			sb.WriteString("    set _caam_current (readlink \"$_caam_target\" 2>/dev/null)\n")
			sb.WriteString("  end\n")
			sb.WriteString("  if test \"$_caam_current\" != \"$_caam_path\"\n")
			sb.WriteString("    rm -f \"$_caam_target\"\n")
			sb.WriteString("    ln -s \"$_caam_path\" \"$_caam_target\"\n")
			sb.WriteString("  end\n")
			sb.WriteString("end\n")
		}
		sb.WriteString("if not contains -- \"$_caam_shim_dir\" $PATH\n")
		sb.WriteString("  set -gx PATH \"$_caam_shim_dir\" $PATH\n")
		sb.WriteString("end\n")
		sb.WriteString("set -e _caam_path _caam_shim_dir _caam_tool _caam_target _caam_current\n\n")
	}

	// Completion for caam command
	sb.WriteString(`# caam command completion
complete -c caam -f
complete -c caam -n "__fish_use_subcommand" -a "activate" -d "Activate a profile"
complete -c caam -n "__fish_use_subcommand" -a "backup" -d "Backup current auth"
complete -c caam -n "__fish_use_subcommand" -a "clear" -d "Clear auth files"
complete -c caam -n "__fish_use_subcommand" -a "cooldown" -d "Manage cooldowns"
complete -c caam -n "__fish_use_subcommand" -a "delete" -d "Delete a profile"
complete -c caam -n "__fish_use_subcommand" -a "ls" -d "List profiles"
complete -c caam -n "__fish_use_subcommand" -a "paths" -d "Show auth file paths"
complete -c caam -n "__fish_use_subcommand" -a "run" -d "Run with auto-switching"
complete -c caam -n "__fish_use_subcommand" -a "shell" -d "Shell integration"
complete -c caam -n "__fish_use_subcommand" -a "status" -d "Show current status"
complete -c caam -n "__fish_use_subcommand" -a "tui" -d "Open terminal UI"

# Tool completion
complete -c caam -n "__fish_seen_subcommand_from activate backup clear delete ls paths status" -a "claude codex gemini"

# Profile completion for activate
complete -c caam -n "__fish_seen_subcommand_from activate; and __fish_seen_subcommand_from claude codex gemini" -a "(caam ls (commandline -opc | tail -n1) 2>/dev/null)"
`)

	return sb.String()
}

// shellCompletionCmd generates completion scripts.
var shellCompletionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `Generate shell completion script.

To load completions:

Bash:
  source <(caam shell completion bash)

  # To load completions for each session, execute once:
  # Linux:
  caam shell completion bash > /etc/bash_completion.d/caam
  # macOS:
  caam shell completion bash > /usr/local/etc/bash_completion.d/caam

Zsh:
  # If shell completion is not already enabled, enable it:
  echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session:
  caam shell completion zsh > "${fpath[1]}/_caam"

Fish:
  caam shell completion fish > ~/.config/fish/completions/caam.fish
`,
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unknown shell: %s", args[0])
		}
	},
}

func init() {
	shellCmd.AddCommand(shellCompletionCmd)
}
