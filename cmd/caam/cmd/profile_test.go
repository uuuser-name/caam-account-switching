// Package cmd implements the CLI commands for caam.
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/spf13/cobra"
)

// TestProfileCommandStructure tests the profile parent command.
func TestProfileCommandStructure(t *testing.T) {
	if profileCmd.Use != "profile" {
		t.Errorf("Expected Use 'profile', got %q", profileCmd.Use)
	}

	if profileCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	if profileCmd.Long == "" {
		t.Error("Expected non-empty Long description")
	}
}

// TestProfileSubcommands tests profile subcommands are registered.
func TestProfileSubcommands(t *testing.T) {
	subcommands := profileCmd.Commands()

	expectedCmds := map[string]bool{
		"add":    false,
		"ls":     false,
		"delete": false,
		"status": false,
		"unlock": false,
	}

	for _, cmd := range subcommands {
		// Get the first word of Use as the command name
		parts := strings.Fields(cmd.Use)
		if len(parts) > 0 {
			expectedCmds[parts[0]] = true
		}
	}

	for name, found := range expectedCmds {
		if !found {
			t.Errorf("Expected subcommand %q not found", name)
		}
	}
}

// TestProfileAddCommand tests profile add command structure.
func TestProfileAddCommand(t *testing.T) {
	// Check Use contains expected pattern
	if !strings.HasPrefix(profileAddCmd.Use, "add") {
		t.Errorf("Expected Use to start with 'add', got %q", profileAddCmd.Use)
	}

	// Check required flags exist
	flags := []string{"auth-mode", "browser", "browser-profile", "browser-name"}
	for _, name := range flags {
		flag := profileAddCmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("Expected flag --%s", name)
		}
	}

	// Check auth-mode default
	authModeFlag := profileAddCmd.Flags().Lookup("auth-mode")
	if authModeFlag.DefValue != "oauth" {
		t.Errorf("Expected auth-mode default 'oauth', got %q", authModeFlag.DefValue)
	}

	// Test arg validation
	err := profileAddCmd.Args(profileAddCmd, []string{"codex", "work"})
	if err != nil {
		t.Errorf("Expected no error for valid args: %v", err)
	}

	err = profileAddCmd.Args(profileAddCmd, []string{"codex"})
	if err == nil {
		t.Error("Expected error for single arg")
	}
}

// TestProfileLsCommand tests profile ls command structure.
func TestProfileLsCommand(t *testing.T) {
	if !strings.HasPrefix(profileLsCmd.Use, "ls") {
		t.Errorf("Expected Use to start with 'ls', got %q", profileLsCmd.Use)
	}

	// Check alias
	found := false
	for _, alias := range profileLsCmd.Aliases {
		if alias == "list" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'list' alias")
	}

	// Test arg validation - accepts 0 or 1 args
	err := profileLsCmd.Args(profileLsCmd, []string{})
	if err != nil {
		t.Errorf("Expected no error for 0 args: %v", err)
	}

	err = profileLsCmd.Args(profileLsCmd, []string{"codex"})
	if err != nil {
		t.Errorf("Expected no error for 1 arg: %v", err)
	}
}

// TestProfileDeleteCommand tests profile delete command structure.
func TestProfileDeleteCommand(t *testing.T) {
	if !strings.HasPrefix(profileDeleteCmd.Use, "delete") {
		t.Errorf("Expected Use to start with 'delete', got %q", profileDeleteCmd.Use)
	}

	// Check alias
	found := false
	for _, alias := range profileDeleteCmd.Aliases {
		if alias == "rm" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'rm' alias")
	}

	// Check force flag
	flag := profileDeleteCmd.Flags().Lookup("force")
	if flag == nil {
		t.Error("Expected --force flag")
	}

	// Test arg validation
	err := profileDeleteCmd.Args(profileDeleteCmd, []string{"codex", "work"})
	if err != nil {
		t.Errorf("Expected no error for valid args: %v", err)
	}
}

// TestProfileStatusCommand tests profile status command structure.
func TestProfileStatusCommand(t *testing.T) {
	if !strings.HasPrefix(profileStatusCmd.Use, "status") {
		t.Errorf("Expected Use to start with 'status', got %q", profileStatusCmd.Use)
	}

	// Test arg validation - requires exactly 2 args
	err := profileStatusCmd.Args(profileStatusCmd, []string{"codex", "work"})
	if err != nil {
		t.Errorf("Expected no error for valid args: %v", err)
	}

	err = profileStatusCmd.Args(profileStatusCmd, []string{"codex"})
	if err == nil {
		t.Error("Expected error for single arg")
	}
}

// TestProfileUnlockCommand tests profile unlock command structure.
func TestProfileUnlockCommand(t *testing.T) {
	if !strings.HasPrefix(profileUnlockCmd.Use, "unlock") {
		t.Errorf("Expected Use to start with 'unlock', got %q", profileUnlockCmd.Use)
	}

	// Check force flag with shorthand
	flag := profileUnlockCmd.Flags().Lookup("force")
	if flag == nil {
		t.Error("Expected --force flag")
	}
	if flag.Shorthand != "f" {
		t.Errorf("Expected shorthand 'f', got %q", flag.Shorthand)
	}

	// Test arg validation - requires exactly 2 args
	err := profileUnlockCmd.Args(profileUnlockCmd, []string{"codex", "work"})
	if err != nil {
		t.Errorf("Expected no error for valid args: %v", err)
	}
}

// TestProfileStore tests basic profile store operations.
func TestProfileStore(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	// Test create profile
	prof, err := store.Create("codex", "work", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	if prof.Provider != "codex" {
		t.Errorf("Expected provider 'codex', got %q", prof.Provider)
	}

	if prof.Name != "work" {
		t.Errorf("Expected name 'work', got %q", prof.Name)
	}

	if prof.AuthMode != "oauth" {
		t.Errorf("Expected auth mode 'oauth', got %q", prof.AuthMode)
	}

	// Test load profile
	loaded, err := store.Load("codex", "work")
	if err != nil {
		t.Fatalf("Load profile failed: %v", err)
	}

	if loaded.Name != "work" {
		t.Errorf("Loaded profile name mismatch: %q", loaded.Name)
	}
}

// TestProfileStoreList tests profile store list operations.
func TestProfileStoreList(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	// Create multiple profiles
	profiles := []struct {
		provider string
		name     string
	}{
		{"codex", "work"},
		{"codex", "personal"},
		{"claude", "main"},
	}

	for _, p := range profiles {
		if _, err := store.Create(p.provider, p.name, "oauth"); err != nil {
			t.Fatalf("Create profile %s/%s failed: %v", p.provider, p.name, err)
		}
	}

	// List codex profiles
	codexProfiles, err := store.List("codex")
	if err != nil {
		t.Fatalf("List codex profiles failed: %v", err)
	}

	if len(codexProfiles) != 2 {
		t.Errorf("Expected 2 codex profiles, got %d", len(codexProfiles))
	}

	// List claude profiles
	claudeProfiles, err := store.List("claude")
	if err != nil {
		t.Fatalf("List claude profiles failed: %v", err)
	}

	if len(claudeProfiles) != 1 {
		t.Errorf("Expected 1 claude profile, got %d", len(claudeProfiles))
	}

	// List all profiles
	allProfiles, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	totalCount := 0
	for _, profileList := range allProfiles {
		totalCount += len(profileList)
	}

	if totalCount != len(profiles) {
		t.Errorf("Expected %d total profiles, got %d", len(profiles), totalCount)
	}
}

// TestProfileStoreDelete tests profile store delete operations.
func TestProfileStoreDelete(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	// Create a profile
	if _, err := store.Create("codex", "to-delete", "oauth"); err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Verify it exists
	profiles, _ := store.List("codex")
	if len(profiles) != 1 {
		t.Fatalf("Expected 1 profile, got %d", len(profiles))
	}

	// Delete the profile
	if err := store.Delete("codex", "to-delete"); err != nil {
		t.Fatalf("Delete profile failed: %v", err)
	}

	// Verify it's gone
	profiles, _ = store.List("codex")
	if len(profiles) != 0 {
		t.Errorf("Expected 0 profiles after delete, got %d", len(profiles))
	}
}

// TestProfileLocking tests profile lock/unlock operations.
func TestProfileLocking(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	// Create a profile
	prof, err := store.Create("codex", "locktest", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Initially not locked
	if prof.IsLocked() {
		t.Error("Profile should not be locked initially")
	}

	// Lock the profile
	if err := prof.Lock(); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	t.Cleanup(func() { _ = prof.Unlock() })

	// Now locked
	if !prof.IsLocked() {
		t.Error("Profile should be locked after Lock()")
	}

	// Get lock info
	info, err := prof.GetLockInfo()
	if err != nil {
		t.Fatalf("GetLockInfo failed: %v", err)
	}

	if info.PID != os.Getpid() {
		t.Errorf("Expected PID %d, got %d", os.Getpid(), info.PID)
	}

	// Unlock
	if err := prof.Unlock(); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Now unlocked
	if prof.IsLocked() {
		t.Error("Profile should be unlocked after Unlock()")
	}
}

// TestProfileIsLockStale tests stale lock detection.
func TestProfileIsLockStale(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	// Create a profile
	prof, err := store.Create("codex", "staletest", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Lock with current process
	if err := prof.Lock(); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	t.Cleanup(func() { _ = prof.Unlock() })

	// Current process lock should not be stale
	stale, err := prof.IsLockStale()
	if err != nil {
		t.Fatalf("IsLockStale failed: %v", err)
	}

	if stale {
		t.Error("Current process lock should not be stale")
	}

	// Clean up
	prof.Unlock()
}

// TestProfileHomePath tests home path generation.
func TestProfileHomePath(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	prof, err := store.Create("codex", "pathtest", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	homePath := prof.HomePath()

	// Should be within profile base path
	if !strings.HasPrefix(homePath, prof.BasePath) {
		t.Errorf("HomePath %q should be within BasePath %q", homePath, prof.BasePath)
	}

	// Should contain "home" in the path
	if !strings.Contains(homePath, "home") {
		t.Errorf("HomePath %q should contain 'home'", homePath)
	}
}

// TestProfileBrowserConfig tests browser configuration.
func TestProfileBrowserConfig(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	prof, err := store.Create("codex", "browsertest", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Initially no browser config
	if prof.HasBrowserConfig() {
		t.Error("Should not have browser config initially")
	}

	// Set browser config
	prof.BrowserCommand = "chrome"
	prof.BrowserProfileDir = "/path/to/profile"
	prof.BrowserProfileName = "Work Profile"

	// Now has browser config
	if !prof.HasBrowserConfig() {
		t.Error("Should have browser config after setting")
	}

	// Test display name
	displayName := prof.BrowserDisplayName()
	if displayName == "" {
		t.Error("BrowserDisplayName should not be empty")
	}

	// Save and reload
	if err := prof.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load("codex", "browsertest")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.BrowserCommand != "chrome" {
		t.Errorf("Expected browser command 'chrome', got %q", loaded.BrowserCommand)
	}

	if loaded.BrowserProfileDir != "/path/to/profile" {
		t.Errorf("Expected browser profile dir, got %q", loaded.BrowserProfileDir)
	}
}

// TestProfileAuthModes tests different auth modes.
func TestProfileAuthModes(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	authModes := []string{"oauth", "api-key"}

	for _, mode := range authModes {
		t.Run(mode, func(t *testing.T) {
			prof, err := store.Create("codex", "authmode-"+mode, mode)
			if err != nil {
				t.Fatalf("Create profile failed: %v", err)
			}

			if prof.AuthMode != mode {
				t.Errorf("Expected auth mode %q, got %q", mode, prof.AuthMode)
			}
		})
	}
}

// TestLoginCommand tests the login command structure.
func TestLoginCommand(t *testing.T) {
	if loginCmd.Use != "login <tool> <profile>" {
		t.Errorf("Unexpected Use: %q", loginCmd.Use)
	}

	if loginCmd.Short == "" {
		t.Error("Expected non-empty Short description")
	}

	// Test arg validation
	err := loginCmd.Args(loginCmd, []string{"codex", "work"})
	if err != nil {
		t.Errorf("Expected no error for valid args: %v", err)
	}

	err = loginCmd.Args(loginCmd, []string{"codex"})
	if err == nil {
		t.Error("Expected error for single arg")
	}
}

// TestExecCommand tests the exec command structure.
func TestExecCommand(t *testing.T) {
	if !strings.HasPrefix(execCmd.Use, "exec") {
		t.Errorf("Expected Use to start with 'exec', got %q", execCmd.Use)
	}

	// Check no-lock flag
	flag := execCmd.Flags().Lookup("no-lock")
	if flag == nil {
		t.Error("Expected --no-lock flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("Expected default false, got %q", flag.DefValue)
	}

	// Test arg validation - requires at least 2 args
	err := execCmd.Args(execCmd, []string{"codex", "work"})
	if err != nil {
		t.Errorf("Expected no error for 2 args: %v", err)
	}

	err = execCmd.Args(execCmd, []string{"codex", "work", "--", "some", "command"})
	if err != nil {
		t.Errorf("Expected no error for args with --: %v", err)
	}
}

// TestProfileEnvironmentSetup tests profile environment configuration.
func TestProfileEnvironmentSetup(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	providers := []string{"codex", "claude", "gemini"}

	for _, prov := range providers {
		t.Run(prov, func(t *testing.T) {
			prof, err := store.Create(prov, "envtest", "oauth")
			if err != nil {
				t.Fatalf("Create profile failed: %v", err)
			}

			// Verify base path is created
			if prof.BasePath == "" {
				t.Error("BasePath should not be empty")
			}

			if _, err := os.Stat(prof.BasePath); err != nil {
				t.Errorf("BasePath should exist: %v", err)
			}

			// Home path should be set
			homePath := prof.HomePath()
			if homePath == "" {
				t.Error("HomePath should not be empty")
			}

			// Clean up for next iteration
			store.Delete(prov, "envtest")
		})
	}
}

// TestProfileStoreNonExistent tests loading non-existent profile.
func TestProfileStoreNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	_, err := store.Load("codex", "nonexistent")
	if err == nil {
		t.Error("Expected error loading non-existent profile")
	}
}

// TestProfileDirectoryStructure tests profile directory creation.
func TestProfileDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	prof, err := store.Create("codex", "dirtest", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Check base path contains expected structure
	expectedPath := filepath.Join(tmpDir, "codex", "dirtest")
	if prof.BasePath != expectedPath {
		t.Errorf("Expected BasePath %q, got %q", expectedPath, prof.BasePath)
	}

	// Verify directory exists
	if _, err := os.Stat(prof.BasePath); err != nil {
		t.Errorf("Profile directory should exist: %v", err)
	}

	// Check profile.json exists
	profileFile := filepath.Join(prof.BasePath, "profile.json")
	if _, err := os.Stat(profileFile); err != nil {
		t.Errorf("profile.json should exist: %v", err)
	}
}

// =============================================================================
// Profile Lifecycle Command Invocation Tests (bd-1r67.2.3.1)
// =============================================================================

// TestProfileLsCommand_Empty tests listing profiles when none exist.
func TestProfileLsCommand_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create a new command to execute
	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	// Execute profileLsCmd with no profiles
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := profileLsCmd.RunE(profileLsCmd, []string{})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "No isolated profiles") {
		t.Errorf("Expected 'No isolated profiles', got %q", output)
	}
}

// TestProfileLsCommand_WithProfiles tests listing existing profiles.
func TestProfileLsCommand_WithProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create test profiles
	_, _ = profileStore.Create("codex", "work", "oauth")
	_, _ = profileStore.Create("codex", "personal", "api-key")
	_, _ = profileStore.Create("claude", "main", "oauth")

	// Execute profileLsCmd
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := profileLsCmd.RunE(profileLsCmd, []string{})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "codex:") {
		t.Errorf("Expected 'codex:' section, got %q", output)
	}
	if !strings.Contains(output, "claude:") {
		t.Errorf("Expected 'claude:' section, got %q", output)
	}
	if !strings.Contains(output, "work") {
		t.Errorf("Expected 'work' profile, got %q", output)
	}
}

// TestProfileLsCommand_FilterByTool tests listing profiles for a specific tool.
func TestProfileLsCommand_FilterByTool(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create test profiles
	_, _ = profileStore.Create("codex", "work", "oauth")
	_, _ = profileStore.Create("claude", "main", "oauth")

	// Execute profileLsCmd with tool filter
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := profileLsCmd.RunE(profileLsCmd, []string{"codex"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if strings.Contains(output, "claude:") {
		t.Errorf("Should not contain claude section, got %q", output)
	}
	if !strings.Contains(output, "work") {
		t.Errorf("Expected 'work' profile, got %q", output)
	}
}

// TestProfileDescribeCommand_SetAndGet tests setting and getting profile descriptions.
func TestProfileDescribeCommand_SetAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create a profile
	_, err := profileStore.Create("codex", "work", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Set description
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = profileDescribeCmd.RunE(profileDescribeCmd, []string{"codex", "work", "Test description"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error setting description: %v", err)
	}
	if !strings.Contains(output, "Set description") {
		t.Errorf("Expected confirmation, got %q", output)
	}

	// Verify description was set
	prof, _ := profileStore.Load("codex", "work")
	if prof.Description != "Test description" {
		t.Errorf("Expected description 'Test description', got %q", prof.Description)
	}

	// Get description
	r, w, _ = os.Pipe()
	os.Stdout = w

	err = profileDescribeCmd.RunE(profileDescribeCmd, []string{"codex", "work"})
	w.Close()
	os.Stdout = origStdout

	buf.Reset()
	buf.ReadFrom(r)
	output = buf.String()

	if err != nil {
		t.Errorf("Unexpected error getting description: %v", err)
	}
	if !strings.Contains(output, "Test description") {
		t.Errorf("Expected description in output, got %q", output)
	}
}

// TestProfileDescribeCommand_Clear tests clearing a profile description.
func TestProfileDescribeCommand_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create a profile with description
	prof, _ := profileStore.Create("codex", "work", "oauth")
	prof.Description = "To be cleared"
	prof.Save()

	// Clear description using --clear flag
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set the clear flag
	if err := profileDescribeCmd.Flags().Set("clear", "true"); err != nil {
		t.Fatalf("set --clear: %v", err)
	}
	t.Cleanup(func() { _ = profileDescribeCmd.Flags().Set("clear", "false") })
	err := profileDescribeCmd.RunE(profileDescribeCmd, []string{"codex", "work"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error clearing description: %v", err)
	}
	if !strings.Contains(output, "Cleared description") {
		t.Errorf("Expected confirmation, got %q", output)
	}

	// Verify description was cleared
	prof, _ = profileStore.Load("codex", "work")
	if prof.Description != "" {
		t.Errorf("Expected empty description, got %q", prof.Description)
	}
}

// TestProfileCloneCommand tests cloning a profile.
func TestProfileCloneCommand(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create source profile with configuration
	source, err := profileStore.Create("codex", "source", "oauth")
	if err != nil {
		t.Fatalf("Create source profile failed: %v", err)
	}
	source.Description = "Original profile"
	source.BrowserCommand = "chrome"
	source.BrowserProfileName = "Test Profile"
	if err := source.Save(); err != nil {
		t.Fatalf("Save source profile failed: %v", err)
	}

	// Clone the profile
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = profileCloneCmd.RunE(profileCloneCmd, []string{"codex", "source", "cloned"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error cloning profile: %v", err)
	}
	if !strings.Contains(output, "Cloned") {
		t.Errorf("Expected 'Cloned' confirmation, got %q", output)
	}

	// Verify cloned profile exists with correct settings
	cloned, err := profileStore.Load("codex", "cloned")
	if err != nil {
		t.Fatalf("Load cloned profile failed: %v", err)
	}
	if cloned.AuthMode != "oauth" {
		t.Errorf("Expected auth mode 'oauth', got %q", cloned.AuthMode)
	}
	if cloned.BrowserCommand != "chrome" {
		t.Errorf("Expected browser command 'chrome', got %q", cloned.BrowserCommand)
	}
	if !strings.Contains(cloned.Description, "Cloned from") {
		t.Errorf("Expected clone description, got %q", cloned.Description)
	}

	// Verify source still exists
	_, err = profileStore.Load("codex", "source")
	if err != nil {
		t.Error("Source profile should still exist after clone")
	}
}

// TestProfileCloneCommand_Overwrite tests force-cloning over existing profile.
func TestProfileCloneCommand_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create source and target profiles
	_, _ = profileStore.Create("codex", "source", "oauth")
	_, _ = profileStore.Create("codex", "existing", "api-key")

	// Try to clone without force - should fail
	err := profileCloneCmd.RunE(profileCloneCmd, []string{"codex", "source", "existing"})
	if err == nil {
		t.Error("Expected error when cloning over existing profile without --force")
	}

	// Clone with force flag
	if err := profileCloneCmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set --force: %v", err)
	}
	t.Cleanup(func() { _ = profileCloneCmd.Flags().Set("force", "false") })
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = profileCloneCmd.RunE(profileCloneCmd, []string{"codex", "source", "existing"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error with --force: %v", err)
	}
	if !strings.Contains(output, "Cloned") {
		t.Errorf("Expected 'Cloned' confirmation, got %q", output)
	}

	// Verify overwritten profile has source's settings
	existing, _ := profileStore.Load("codex", "existing")
	if existing.AuthMode != "oauth" {
		t.Errorf("Expected auth mode 'oauth' from source, got %q", existing.AuthMode)
	}
}

// TestProfileDeleteCommand_Confirmation tests delete confirmation flow.
func TestProfileDeleteCommand_Confirmation(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create a profile
	_, _ = profileStore.Create("codex", "to-delete", "oauth")

	// Verify profile exists
	profiles, _ := profileStore.List("codex")
	if len(profiles) != 1 {
		t.Fatalf("Expected 1 profile, got %d", len(profiles))
	}

	// Delete with --force (skip confirmation)
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := profileDeleteCmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set --force: %v", err)
	}
	t.Cleanup(func() { _ = profileDeleteCmd.Flags().Set("force", "false") })
	err := profileDeleteCmd.RunE(profileDeleteCmd, []string{"codex", "to-delete"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "Deleted") {
		t.Errorf("Expected 'Deleted' confirmation, got %q", output)
	}

	// Verify profile is gone
	profiles, _ = profileStore.List("codex")
	if len(profiles) != 0 {
		t.Errorf("Expected 0 profiles after delete, got %d", len(profiles))
	}
}

// TestProfileUnlockCommand_NotLocked tests unlocking a non-locked profile.
func TestProfileUnlockCommand_NotLocked(t *testing.T) {
	tmpDir := t.TempDir()
	originalStore := profileStore
	profileStore = profile.NewStore(tmpDir)
	t.Cleanup(func() { profileStore = originalStore })

	// Create a non-locked profile
	_, _ = profileStore.Create("codex", "unlocked", "oauth")

	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := profileUnlockCmd.RunE(profileUnlockCmd, []string{"codex", "unlocked"})
	w.Close()
	os.Stdout = origStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(output, "not locked") {
		t.Errorf("Expected 'not locked' message, got %q", output)
	}
}

// TestProfileStore_UpdateAccountLabel tests setting account label on profile.
func TestProfileStore_UpdateAccountLabel(t *testing.T) {
	tmpDir := t.TempDir()
	store := profile.NewStore(tmpDir)

	prof, err := store.Create("codex", "labeled", "oauth")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Set account label
	prof.AccountLabel = "user@example.com"
	if err := prof.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reload and verify
	loaded, err := store.Load("codex", "labeled")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.AccountLabel != "user@example.com" {
		t.Errorf("Expected account label 'user@example.com', got %q", loaded.AccountLabel)
	}
}
