// Package cmd implements the CLI commands for caam.
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/spf13/cobra"
)

// =============================================================================
// aliasCmd Structure Tests
// =============================================================================

func TestAliasCommandStructure(t *testing.T) {
	if aliasCmd.Use != "alias [tool] [profile] [alias]" {
		t.Errorf("Expected Use 'alias [tool] [profile] [alias]', got %q", aliasCmd.Use)
	}

	if aliasCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if aliasCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestAliasCommandFlags(t *testing.T) {
	// Check --list flag
	listFlag := aliasCmd.Flags().Lookup("list")
	if listFlag == nil {
		t.Fatal("Expected --list flag")
	}
	if listFlag.DefValue != "false" {
		t.Errorf("Expected list default false, got %q", listFlag.DefValue)
	}

	// Check --remove flag with shorthand
	removeFlag := aliasCmd.Flags().Lookup("remove")
	if removeFlag == nil {
		t.Fatal("Expected --remove flag")
	}
	if removeFlag.Shorthand != "r" {
		t.Errorf("Expected shorthand 'r', got %q", removeFlag.Shorthand)
	}

	// Check --json flag
	jsonFlag := aliasCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Fatal("Expected --json flag")
	}
	if jsonFlag.DefValue != "false" {
		t.Errorf("Expected json default false, got %q", jsonFlag.DefValue)
	}
}

func readPipeOutput(t *testing.T, r *os.File) string {
	t.Helper()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("ReadFrom(pipe): %v", err)
	}
	return buf.String()
}

// =============================================================================
// favoriteCmd Structure Tests
// =============================================================================

func TestFavoriteCommandStructure(t *testing.T) {
	if !strings.HasPrefix(favoriteCmd.Use, "favorite") {
		t.Errorf("Expected Use to start with 'favorite', got %q", favoriteCmd.Use)
	}

	if favoriteCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if favoriteCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

func TestFavoriteCommandFlags(t *testing.T) {
	// Check --list flag
	listFlag := favoriteCmd.Flags().Lookup("list")
	if listFlag == nil {
		t.Error("Expected --list flag")
	}

	// Check --clear flag
	clearFlag := favoriteCmd.Flags().Lookup("clear")
	if clearFlag == nil {
		t.Error("Expected --clear flag")
	}

	// Check --json flag
	jsonFlag := favoriteCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Error("Expected --json flag")
	}
}

// =============================================================================
// listAliases Tests
// =============================================================================

func TestListAliasesEmpty(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test empty aliases (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listAliases(cfg, false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "No aliases configured") {
		t.Errorf("Expected 'No aliases configured', got %q", output)
	}
}

func TestListAliasesEmptyJSON(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test empty aliases (JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listAliases(cfg, true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if output != "{}\n" {
		t.Errorf("Expected '{}', got %q", output)
	}
}

func TestListAliasesWithAliases(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")
	cfg.AddAlias("claude", "work-account", "w")
	cfg.AddAlias("codex", "personal-profile", "personal")

	// Test with aliases (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listAliases(cfg, false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Configured aliases:") {
		t.Errorf("Expected 'Configured aliases:', got %q", output)
	}
	if !strings.Contains(output, "claude/work-account") {
		t.Errorf("Expected 'claude/work-account', got %q", output)
	}
	if !strings.Contains(output, "codex/personal-profile") {
		t.Errorf("Expected 'codex/personal-profile', got %q", output)
	}
}

func TestListAliasesWithAliasesJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Test with aliases (JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listAliases(cfg, true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON to verify structure
	var result map[string][]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON output: %v", err)
	}

	if len(result) == 0 {
		t.Error("Expected aliases in result")
	}
}

// =============================================================================
// removeAlias Tests
// =============================================================================

func TestRemoveAliasNotFound(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	err := removeAlias(cfg, "nonexistent", false)
	if err == nil {
		t.Error("Expected error for nonexistent alias")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got %v", err)
	}
}

func TestRemoveAliasSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test non-JSON output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := removeAlias(cfg, "work", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Removed alias: work") {
		t.Errorf("Expected 'Removed alias: work', got %q", output)
	}

	// Verify alias was removed
	if cfg.ResolveAliasForProvider("claude", "work") != "" {
		t.Error("Alias should be removed")
	}
}

func TestRemoveAliasSuccessJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test JSON output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := removeAlias(cfg, "work", true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}

	if result["removed"] != "work" {
		t.Errorf("Expected removed 'work', got %v", result["removed"])
	}
	if result["success"] != true {
		t.Errorf("Expected success true, got %v", result["success"])
	}
}

// =============================================================================
// showProfileAliases Tests
// =============================================================================

func TestShowProfileAliasesEmpty(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test empty aliases (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showProfileAliases(cfg, "claude", "work", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "No aliases for claude/work") {
		t.Errorf("Expected 'No aliases for claude/work', got %q", output)
	}
}

func TestShowProfileAliasesWithAliases(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")
	cfg.AddAlias("claude", "work-account", "w")

	// Test with aliases (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showProfileAliases(cfg, "claude", "work-account", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Aliases for claude/work-account:") {
		t.Errorf("Expected 'Aliases for claude/work-account:', got %q", output)
	}
	if !strings.Contains(output, "work") {
		t.Errorf("Expected 'work', got %q", output)
	}
}

func TestShowProfileAliasesJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Test JSON output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showProfileAliases(cfg, "claude", "work-account", true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}

	if result["tool"] != "claude" {
		t.Errorf("Expected tool 'claude', got %v", result["tool"])
	}
	if result["profile"] != "work-account" {
		t.Errorf("Expected profile 'work-account', got %v", result["profile"])
	}
}

// =============================================================================
// addAlias Tests
// =============================================================================

func TestAddAliasNew(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test adding new alias (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := addAlias(cfg, "claude", "work-account", "work", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Added alias: work -> claude/work-account") {
		t.Errorf("Expected 'Added alias: work -> claude/work-account', got %q", output)
	}

	// Verify alias was added
	if cfg.ResolveAliasForProvider("claude", "work") != "work-account" {
		t.Error("Alias should be added")
	}
}

func TestAddAliasNewJSON(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test adding new alias (JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := addAlias(cfg, "claude", "work-account", "work", true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}

	if result["status"] != "created" {
		t.Errorf("Expected status 'created', got %v", result["status"])
	}
}

func TestAddAliasAlreadyExists(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Add same alias again (should be idempotent)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := addAlias(cfg, "claude", "work-account", "work", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "already exists") {
		t.Errorf("Expected 'already exists', got %q", output)
	}
}

func TestAddAliasAlreadyExistsJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test JSON output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := addAlias(cfg, "claude", "work-account", "work", true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}

	if result["status"] != "already_exists" {
		t.Errorf("Expected status 'already_exists', got %v", result["status"])
	}
}

func TestAddAliasConflict(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Try to add same alias for different profile
	err := addAlias(cfg, "claude", "personal-account", "work", false)
	if err == nil {
		t.Error("Expected error for conflicting alias")
	}
	if !strings.Contains(err.Error(), "already used") {
		t.Errorf("Expected 'already used' error, got %v", err)
	}
}

// =============================================================================
// listFavorites Tests
// =============================================================================

func TestListFavoritesEmpty(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test empty favorites (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listFavorites(cfg, false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "No favorites configured") {
		t.Errorf("Expected 'No favorites configured', got %q", output)
	}
}

func TestListFavoritesEmptyJSON(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test empty favorites (JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listFavorites(cfg, true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if output != "{}\n" {
		t.Errorf("Expected '{}', got %q", output)
	}
}

func TestListFavoritesWithFavorites(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SetFavorites("claude", []string{"work", "personal"})
	cfg.SetFavorites("codex", []string{"main"})

	// Test with favorites (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listFavorites(cfg, false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Configured favorites:") {
		t.Errorf("Expected 'Configured favorites:', got %q", output)
	}
	if !strings.Contains(output, "claude:") {
		t.Errorf("Expected 'claude:', got %q", output)
	}
}

func TestListFavoritesWithFavoritesJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SetFavorites("claude", []string{"work", "personal"})

	// Test JSON output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listFavorites(cfg, true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON
	var result map[string][]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}

	if len(result["claude"]) != 2 {
		t.Errorf("Expected 2 favorites, got %d", len(result["claude"]))
	}
}

// =============================================================================
// showFavorites Tests
// =============================================================================

func TestShowFavoritesEmpty(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test empty favorites (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showFavorites(cfg, "claude", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "No favorites for claude") {
		t.Errorf("Expected 'No favorites for claude', got %q", output)
	}
}

func TestShowFavoritesWithFavorites(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SetFavorites("claude", []string{"work", "personal"})

	// Test with favorites (non-JSON)
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showFavorites(cfg, "claude", false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Favorites for claude:") {
		t.Errorf("Expected 'Favorites for claude:', got %q", output)
	}
	if !strings.Contains(output, "work") {
		t.Errorf("Expected 'work', got %q", output)
	}
}

func TestShowFavoritesJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SetFavorites("claude", []string{"work"})

	// Test JSON output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showFavorites(cfg, "claude", true)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Parse JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}

	if result["tool"] != "claude" {
		t.Errorf("Expected tool 'claude', got %v", result["tool"])
	}
}

// =============================================================================
// runAlias Tests (Integration-style tests)
// =============================================================================

func TestRunAliasListFlag(t *testing.T) {
	// Create a test command
	cmd := &cobra.Command{}
	cmd.Flags().Bool("list", false, "list all aliases")
	cmd.Flags().StringP("remove", "r", "", "remove an alias")
	cmd.Flags().Bool("json", false, "output in JSON format")
	cmd.SetArgs([]string{})

	// Set flags
	if err := cmd.ParseFlags([]string{"--list"}); err != nil {
		t.Fatalf("ParseFlags(--list): %v", err)
	}

	cfg := config.DefaultConfig()

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listAliases(cfg, false)
	w.Close()
	os.Stdout = origStdout

	_ = readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestRunAliasInsufficientArgs(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test with no args (should error)
	err := runAliasHelper(cfg, []string{}, false, "", false)
	if err == nil {
		t.Error("Expected error for insufficient args")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("Expected usage error, got %v", err)
	}
}

func TestRunAliasInvalidTool(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test with invalid tool
	err := runAliasHelper(cfg, []string{"invalid-tool", "profile", "alias"}, false, "", false)
	if err == nil {
		t.Error("Expected error for invalid tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("Expected 'unknown tool' error, got %v", err)
	}
}

// runAliasHelper is a test helper that mimics runAlias without requiring vault
func runAliasHelper(cfg *config.Config, args []string, listFlag bool, removeFlag string, jsonFlag bool) error {
	// List all aliases
	if listFlag {
		return listAliases(cfg, jsonFlag)
	}

	// Remove an alias
	if removeFlag != "" {
		return removeAlias(cfg, removeFlag, jsonFlag)
	}

	// Need at least tool and profile to add or show aliases
	if len(args) < 2 {
		return fmt.Errorf("usage: caam alias <tool> <profile> [alias]")
	}

	tool := args[0]
	profile := args[1]

	// Validate tool
	if _, ok := tools[tool]; !ok {
		return fmt.Errorf("unknown tool: %s (supported: codex, claude, gemini)", tool)
	}

	// If no alias provided, show current aliases
	if len(args) == 2 {
		return showProfileAliases(cfg, tool, profile, jsonFlag)
	}

	// Add alias
	alias := args[2]
	return addAlias(cfg, tool, profile, alias, jsonFlag)
}

// =============================================================================
// runFavorite Tests (Integration-style tests)
// =============================================================================

func TestRunFavoriteInsufficientArgs(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test with no args (should error)
	err := runFavoriteHelper(cfg, []string{}, false, false, false)
	if err == nil {
		t.Error("Expected error for insufficient args")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("Expected usage error, got %v", err)
	}
}

func TestRunFavoriteInvalidTool(t *testing.T) {
	cfg := config.DefaultConfig()

	// Create temp config dir
	tmpDir := t.TempDir()
	config.SetConfigPath(filepath.Join(tmpDir, "config.json"))

	// Test with invalid tool
	err := runFavoriteHelper(cfg, []string{"invalid-tool"}, false, false, false)
	if err == nil {
		t.Error("Expected error for invalid tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("Expected 'unknown tool' error, got %v", err)
	}
}

func TestRunFavoriteListFlag(t *testing.T) {
	cfg := config.DefaultConfig()

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listFavorites(cfg, false)
	w.Close()
	os.Stdout = origStdout

	_ = readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// runFavoriteHelper is a test helper that mimics runFavorite without requiring vault
func runFavoriteHelper(cfg *config.Config, args []string, listFlag, clearFlag, jsonFlag bool) error {
	// List all favorites
	if listFlag {
		return listFavorites(cfg, jsonFlag)
	}

	if len(args) < 1 {
		return fmt.Errorf("usage: caam favorite <tool> [profiles...]")
	}

	tool := args[0]
	if _, ok := tools[tool]; !ok {
		return fmt.Errorf("unknown tool: %s (supported: codex, claude, gemini)", tool)
	}

	// Clear favorites
	if clearFlag {
		cfg.SetFavorites(tool, nil)
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		return nil
	}

	// Show favorites if no profiles provided
	if len(args) == 1 {
		return showFavorites(cfg, tool, jsonFlag)
	}

	// Set favorites
	profiles := args[1:]
	cfg.SetFavorites(tool, profiles)
	return nil
}

// =============================================================================
// Config Method Tests for Aliases and Favorites
// =============================================================================

func TestConfigAddAlias(t *testing.T) {
	cfg := config.DefaultConfig()

	// Add first alias
	cfg.AddAlias("claude", "work-account", "work")
	aliases := cfg.GetAliases("claude", "work-account")
	if len(aliases) != 1 || aliases[0] != "work" {
		t.Errorf("Expected ['work'], got %v", aliases)
	}

	// Add second alias
	cfg.AddAlias("claude", "work-account", "w")
	aliases = cfg.GetAliases("claude", "work-account")
	if len(aliases) != 2 {
		t.Errorf("Expected 2 aliases, got %d", len(aliases))
	}

	// Add duplicate (should be idempotent)
	cfg.AddAlias("claude", "work-account", "work")
	aliases = cfg.GetAliases("claude", "work-account")
	if len(aliases) != 2 {
		t.Errorf("Expected 2 aliases after duplicate, got %d", len(aliases))
	}
}

func TestConfigRemoveAlias(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Remove existing alias
	removed := cfg.RemoveAlias("work")
	if !removed {
		t.Error("Expected RemoveAlias to return true")
	}

	aliases := cfg.GetAliases("claude", "work-account")
	if len(aliases) != 0 {
		t.Errorf("Expected 0 aliases after removal, got %d", len(aliases))
	}

	// Remove non-existent alias
	removed = cfg.RemoveAlias("nonexistent")
	if removed {
		t.Error("Expected RemoveAlias to return false for non-existent")
	}
}

func TestConfigResolveAlias(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Resolve existing alias
	provider, profile, found := cfg.ResolveAlias("work")
	if !found {
		t.Error("Expected ResolveAlias to find alias")
	}
	if provider != "claude" {
		t.Errorf("Expected provider 'claude', got %q", provider)
	}
	if profile != "work-account" {
		t.Errorf("Expected profile 'work-account', got %q", profile)
	}

	// Resolve non-existent alias
	_, _, found = cfg.ResolveAlias("nonexistent")
	if found {
		t.Error("Expected ResolveAlias to not find non-existent alias")
	}
}

func TestConfigResolveAliasForProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Resolve within correct provider
	profile := cfg.ResolveAliasForProvider("claude", "work")
	if profile != "work-account" {
		t.Errorf("Expected 'work-account', got %q", profile)
	}

	// Resolve within wrong provider
	profile = cfg.ResolveAliasForProvider("codex", "work")
	if profile != "" {
		t.Errorf("Expected empty string, got %q", profile)
	}
}

func TestConfigFavorites(t *testing.T) {
	cfg := config.DefaultConfig()

	// Set favorites
	cfg.SetFavorites("claude", []string{"work", "personal"})

	// Get favorites
	favorites := cfg.GetFavorites("claude")
	if len(favorites) != 2 {
		t.Errorf("Expected 2 favorites, got %d", len(favorites))
	}
	if favorites[0] != "work" {
		t.Errorf("Expected first favorite 'work', got %q", favorites[0])
	}

	// Check IsFavorite
	if !cfg.IsFavorite("claude", "work") {
		t.Error("Expected IsFavorite to return true")
	}
	if cfg.IsFavorite("claude", "nonexistent") {
		t.Error("Expected IsFavorite to return false for non-favorite")
	}

	// Clear favorites
	cfg.SetFavorites("claude", nil)
	favorites = cfg.GetFavorites("claude")
	if len(favorites) != 0 {
		t.Errorf("Expected 0 favorites after clear, got %d", len(favorites))
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestAliasMultipleProfilesSameAlias(t *testing.T) {
	cfg := config.DefaultConfig()

	// Add alias for one profile
	cfg.AddAlias("claude", "work-account", "work")

	// Try to add same alias for different profile
	cfg.AddAlias("claude", "personal-account", "work")

	// The alias should still resolve to the first profile
	// (config.AddAlias doesn't check conflicts, that's done in addAlias)
	_ = cfg.ResolveAliasForProvider("claude", "work")
	// Both profiles might have the alias in their list
	aliases1 := cfg.GetAliases("claude", "work-account")
	aliases2 := cfg.GetAliases("claude", "personal-account")

	// Both should have "work" alias
	if len(aliases1) == 0 || aliases1[0] != "work" {
		t.Errorf("Expected work-account to have 'work' alias, got %v", aliases1)
	}
	if len(aliases2) == 0 || aliases2[0] != "work" {
		t.Errorf("Expected personal-account to have 'work' alias, got %v", aliases2)
	}
}

func TestFavoritesOrderPreserved(t *testing.T) {
	cfg := config.DefaultConfig()

	// Set favorites in specific order
	expected := []string{"third", "first", "second"}
	cfg.SetFavorites("claude", expected)

	favorites := cfg.GetFavorites("claude")
	if len(favorites) != 3 {
		t.Errorf("Expected 3 favorites, got %d", len(favorites))
	}

	// Verify order is preserved
	for i, f := range favorites {
		if f != expected[i] {
			t.Errorf("Favorite at position %d: expected %q, got %q", i, expected[i], f)
		}
	}
}

func TestRemoveAliasCleansUpEmptyKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddAlias("claude", "work-account", "work")

	// Remove the only alias
	cfg.RemoveAlias("work")

	// The key should be removed from the map
	if len(cfg.Aliases) > 0 {
		t.Errorf("Expected empty Aliases map, got %d entries", len(cfg.Aliases))
	}
}

func TestShowFavoritesMultipleTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SetFavorites("claude", []string{"work"})
	cfg.SetFavorites("codex", []string{"personal"})

	// listFavorites should show all
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listFavorites(cfg, false)
	w.Close()
	os.Stdout = origStdout

	output := readPipeOutput(t, r)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should contain both tools
	if !strings.Contains(output, "claude:") {
		t.Error("Expected 'claude:' in output")
	}
	if !strings.Contains(output, "codex:") {
		t.Error("Expected 'codex:' in output")
	}
}
