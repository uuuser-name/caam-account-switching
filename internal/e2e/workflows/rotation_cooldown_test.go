// Package workflows provides E2E workflow tests for caam operations.
package workflows

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
)

// =============================================================================
// E2E Test: Profile Rotation and Cooldown Workflow
// =============================================================================

// TestE2E_CooldownEnforcesDuringAutoRotation tests that profiles in cooldown
// are skipped when using --auto rotation.
func TestE2E_CooldownEnforcesDuringAutoRotation(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// ==========================================================================
	// PHASE 1: Setup - Create profiles and initialize DB
	// ==========================================================================
	h.StartStep("setup", "Creating profiles and initializing DB")

	homeDir := h.SubDir("home")
	vaultDir := h.SubDir("vault")
	dbPath := filepath.Join(h.TempDir, "caam.db")

	// Create Codex home directory
	codexHome := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		t.Fatalf("Failed to create codex home: %v", err)
	}

	codexAuthPath := filepath.Join(codexHome, "auth.json")
	fileSet := authfile.AuthFileSet{
		Tool: "codex",
		Files: []authfile.AuthFileSpec{
			{Tool: "codex", Path: codexAuthPath, Required: true},
		},
	}

	vault := authfile.NewVault(vaultDir)

	// Create 3 test profiles
	profiles := []string{"profile1", "profile2", "profile3"}
	for _, name := range profiles {
		content := map[string]interface{}{
			"access_token":  name + "-token",
			"refresh_token": name + "-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		}
		jsonBytes, _ := json.MarshalIndent(content, "", "  ")
		if err := os.WriteFile(codexAuthPath, jsonBytes, 0600); err != nil {
			t.Fatalf("Failed to write auth for %s: %v", name, err)
		}
		if err := vault.Backup(fileSet, name); err != nil {
			t.Fatalf("Failed to backup %s: %v", name, err)
		}
	}
	h.LogInfo("Created test profiles", "count", len(profiles))

	// Initialize DB for cooldown tracking
	db, err := caamdb.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	h.EndStep("setup")

	// ==========================================================================
	// PHASE 2: Test Cooldown Scenario
	// ==========================================================================
	h.StartStep("cooldown_scenario", "Testing cooldown enforcement")

	// Create selector with smart algorithm
	selector := rotation.NewSelector(rotation.AlgorithmSmart, nil, db)
	// Use fixed RNG for reproducibility
	selector.SetRNG(rand.New(rand.NewSource(42)))
	selector.SetAvoidRecent(0) // Disable recency penalty for this test

	// Initial selection - should pick one of the profiles
	h.StartStep("initial_selection", "Testing initial --auto selection")
	result1, err := selector.Select("codex", profiles, "")
	if err != nil {
		t.Fatalf("Initial selection failed: %v", err)
	}
	h.LogInfo("Initial selection", "selected", result1.Selected, "algorithm", string(result1.Algorithm))
	h.EndStep("initial_selection")

	// Set cooldown on the selected profile (60 minutes)
	h.StartStep("set_cooldown", "Setting cooldown on selected profile")
	selectedProfile := result1.Selected
	cooldownDuration := 60 * time.Minute
	_, err = db.SetCooldown("codex", selectedProfile, time.Now(), cooldownDuration, "rate limit hit")
	if err != nil {
		t.Fatalf("Failed to set cooldown: %v", err)
	}
	h.LogInfo("Set cooldown", "profile", selectedProfile, "duration", cooldownDuration.String())
	h.EndStep("set_cooldown")

	// Second selection - should NOT select the profile in cooldown
	h.StartStep("post_cooldown_selection", "Testing selection after cooldown")
	result2, err := selector.Select("codex", profiles, selectedProfile)
	if err != nil {
		t.Fatalf("Post-cooldown selection failed: %v", err)
	}

	// Verify the profile in cooldown was NOT selected
	if result2.Selected == selectedProfile {
		t.Errorf("Expected different profile, but selected the same one in cooldown: %s", result2.Selected)
	}
	h.LogInfo("Post-cooldown selection", "selected", result2.Selected, "skipped_in_cooldown", selectedProfile)

	// Verify the cooldown profile appears in alternatives with negative score
	foundInCooldown := false
	for _, alt := range result2.Alternatives {
		if alt.Name == selectedProfile && alt.Score < -9000 {
			foundInCooldown = true
			h.LogInfo("Verified cooldown in alternatives", "profile", alt.Name, "score", alt.Score)
			break
		}
	}
	if !foundInCooldown {
		t.Errorf("Expected %s to appear in alternatives with cooldown indication", selectedProfile)
	}
	h.EndStep("post_cooldown_selection")

	h.EndStep("cooldown_scenario")

	// ==========================================================================
	// PHASE 3: All Profiles in Cooldown
	// ==========================================================================
	h.StartStep("all_cooldown", "Testing when all profiles are in cooldown")

	// Put all profiles in cooldown
	for _, name := range profiles {
		if name != selectedProfile { // First one already in cooldown
			_, err = db.SetCooldown("codex", name, time.Now(), cooldownDuration, "rate limit hit")
			if err != nil {
				t.Fatalf("Failed to set cooldown for %s: %v", name, err)
			}
		}
	}
	h.LogInfo("All profiles now in cooldown")

	// Selection should now fail
	_, err = selector.Select("codex", profiles, "")
	if err == nil {
		t.Errorf("Expected error when all profiles are in cooldown")
	} else {
		h.LogInfo("Correctly failed when all in cooldown", "error", err.Error())
	}

	h.EndStep("all_cooldown")

	// ==========================================================================
	// PHASE 4: Clear Cooldown and Verify Selection Works
	// ==========================================================================
	h.StartStep("clear_cooldown", "Testing cooldown clearing")

	// Clear cooldown for profile2
	cleared, err := db.ClearCooldown("codex", "profile2")
	if err != nil {
		t.Fatalf("Failed to clear cooldown: %v", err)
	}
	h.LogInfo("Cleared cooldown", "profile", "profile2", "rows_affected", cleared)

	// Selection should now succeed and select profile2 (only available)
	result3, err := selector.Select("codex", profiles, "")
	if err != nil {
		t.Fatalf("Selection after clearing cooldown failed: %v", err)
	}
	if result3.Selected != "profile2" {
		t.Errorf("Expected profile2 (only non-cooldown), got %s", result3.Selected)
	}
	h.LogInfo("Post-clear selection", "selected", result3.Selected)

	h.EndStep("clear_cooldown")

	t.Log("\n" + h.Summary())
}

// TestE2E_RotationAlgorithms tests all three rotation algorithms.
func TestE2E_RotationAlgorithms(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// ==========================================================================
	// PHASE 1: Setup
	// ==========================================================================
	h.StartStep("setup", "Creating profiles for algorithm tests")

	dbPath := filepath.Join(h.TempDir, "caam.db")
	db, err := caamdb.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	profiles := []string{"alpha", "beta", "gamma", "delta"}
	h.LogInfo("Test profiles created", "count", len(profiles))
	h.EndStep("setup")

	// ==========================================================================
	// PHASE 2: Test Smart Algorithm
	// ==========================================================================
	h.StartStep("smart_algorithm", "Testing smart rotation algorithm")

	smartSelector := rotation.NewSelector(rotation.AlgorithmSmart, nil, db)
	smartSelector.SetRNG(rand.New(rand.NewSource(42)))
	smartSelector.SetAvoidRecent(0)

	result, err := smartSelector.Select("codex", profiles, "")
	if err != nil {
		t.Fatalf("Smart selection failed: %v", err)
	}

	if result.Algorithm != rotation.AlgorithmSmart {
		t.Errorf("Expected smart algorithm, got %s", result.Algorithm)
	}

	// Verify reasons are provided for the selected profile
	var selectedScore rotation.ProfileScore
	for _, alt := range result.Alternatives {
		if alt.Name == result.Selected {
			selectedScore = alt
			break
		}
	}

	if len(selectedScore.Reasons) == 0 {
		t.Errorf("Smart algorithm should provide reasons for selection")
	}
	h.LogInfo("Smart algorithm result",
		"selected", result.Selected,
		"score", selectedScore.Score,
		"reason_count", len(selectedScore.Reasons))

	h.EndStep("smart_algorithm")

	// ==========================================================================
	// PHASE 3: Test Round Robin Algorithm
	// ==========================================================================
	h.StartStep("round_robin_algorithm", "Testing round-robin rotation algorithm")

	rrSelector := rotation.NewSelector(rotation.AlgorithmRoundRobin, nil, db)

	// Track selections to verify sequential behavior
	currentProfile := ""
	selections := make([]string, 0, 8)

	for i := 0; i < 8; i++ {
		result, err := rrSelector.Select("codex", profiles, currentProfile)
		if err != nil {
			t.Fatalf("Round-robin selection %d failed: %v", i, err)
		}
		selections = append(selections, result.Selected)
		currentProfile = result.Selected
	}

	// Verify round-robin pattern: after 4 selections, should see each profile
	// The pattern may not be exact due to sorting, but should cycle
	profileCounts := make(map[string]int)
	for _, s := range selections {
		profileCounts[s]++
	}

	// Each profile should be selected at least once in 8 iterations
	for _, name := range profiles {
		if profileCounts[name] == 0 {
			t.Errorf("Round-robin failed to select %s in 8 iterations", name)
		}
	}
	h.LogInfo("Round-robin results",
		"selections", selections,
		"alpha_count", profileCounts["alpha"],
		"beta_count", profileCounts["beta"],
		"gamma_count", profileCounts["gamma"],
		"delta_count", profileCounts["delta"])

	h.EndStep("round_robin_algorithm")

	// ==========================================================================
	// PHASE 4: Test Random Algorithm
	// ==========================================================================
	h.StartStep("random_algorithm", "Testing random rotation algorithm")

	randomSelector := rotation.NewSelector(rotation.AlgorithmRandom, nil, db)

	// Run 20 selections and verify distribution
	randomSelections := make([]string, 0, 20)
	randomCounts := make(map[string]int)

	for i := 0; i < 20; i++ {
		result, err := randomSelector.Select("codex", profiles, "")
		if err != nil {
			t.Fatalf("Random selection %d failed: %v", i, err)
		}
		randomSelections = append(randomSelections, result.Selected)
		randomCounts[result.Selected]++
	}

	// With 20 selections and 4 profiles, each should be selected at least once
	// (probability of not selecting any one profile: (3/4)^20 ≈ 0.003)
	// This is a statistical test - we check that distribution is reasonable
	selectedCount := 0
	for _, name := range profiles {
		if randomCounts[name] > 0 {
			selectedCount++
		}
	}
	// At least 3 of 4 profiles should be selected (very high probability)
	if selectedCount < 3 {
		t.Errorf("Random algorithm only selected %d of 4 profiles in 20 iterations", selectedCount)
	}

	h.LogInfo("Random algorithm results",
		"sample_selections", randomSelections[:5],
		"alpha_count", randomCounts["alpha"],
		"beta_count", randomCounts["beta"],
		"gamma_count", randomCounts["gamma"],
		"delta_count", randomCounts["delta"])

	h.EndStep("random_algorithm")

	t.Log("\n" + h.Summary())
}

// TestE2E_CooldownBypass tests that cooldown can be bypassed when needed.
// This simulates the --force flag behavior in the CLI.
func TestE2E_CooldownBypass(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// ==========================================================================
	// PHASE 1: Setup
	// ==========================================================================
	h.StartStep("setup", "Creating profile and setting cooldown")

	homeDir := h.SubDir("home")
	vaultDir := h.SubDir("vault")
	dbPath := filepath.Join(h.TempDir, "caam.db")

	// Create Codex home directory
	codexHome := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		t.Fatalf("Failed to create codex home: %v", err)
	}

	codexAuthPath := filepath.Join(codexHome, "auth.json")
	fileSet := authfile.AuthFileSet{
		Tool: "codex",
		Files: []authfile.AuthFileSpec{
			{Tool: "codex", Path: codexAuthPath, Required: true},
		},
	}

	vault := authfile.NewVault(vaultDir)

	// Create profiles - one will be in cooldown, one won't be
	profileInCooldown := "forced"
	profileAvailable := "available"

	for _, profileName := range []string{profileInCooldown, profileAvailable} {
		content := map[string]interface{}{
			"access_token":  profileName + "-token",
			"refresh_token": profileName + "-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		}
		jsonBytes, _ := json.MarshalIndent(content, "", "  ")
		if err := os.WriteFile(codexAuthPath, jsonBytes, 0600); err != nil {
			t.Fatalf("Failed to write auth: %v", err)
		}
		if err := vault.Backup(fileSet, profileName); err != nil {
			t.Fatalf("Failed to backup profile: %v", err)
		}
	}

	// Initialize DB and set cooldown on one profile
	db, err := caamdb.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.SetCooldown("codex", profileInCooldown, time.Now(), 60*time.Minute, "rate limit")
	if err != nil {
		t.Fatalf("Failed to set cooldown: %v", err)
	}
	h.LogInfo("Set cooldown on profile", "profile", profileInCooldown)

	h.EndStep("setup")

	// ==========================================================================
	// PHASE 2: Verify Cooldown Prevents Selection of That Profile
	// ==========================================================================
	h.StartStep("verify_blocked", "Verifying cooldown profile is not selected")

	selector := rotation.NewSelector(rotation.AlgorithmSmart, nil, db)
	selector.SetRNG(rand.New(rand.NewSource(42)))
	selector.SetAvoidRecent(0)

	// Selection should succeed but select the available profile, not the one in cooldown
	result, err := selector.Select("codex", []string{profileInCooldown, profileAvailable}, "")
	if err != nil {
		t.Fatalf("Selection failed unexpectedly: %v", err)
	}
	if result.Selected == profileInCooldown {
		t.Errorf("Expected to NOT select %s (in cooldown), but it was selected", profileInCooldown)
	}
	if result.Selected != profileAvailable {
		t.Errorf("Expected to select %s, got %s", profileAvailable, result.Selected)
	}
	h.LogInfo("Selection correctly avoided cooldown profile", "selected", result.Selected, "avoided", profileInCooldown)

	h.EndStep("verify_blocked")

	// ==========================================================================
	// PHASE 3: Force Activate Despite Cooldown
	// ==========================================================================
	h.StartStep("force_activate", "Testing force activation bypasses cooldown")

	// In force mode, we bypass the selector and directly restore
	// This simulates what `caam activate codex profile --force` does
	h.TimeStep("restore_profile", "Restoring profile directly (force mode)", func() {
		if err := vault.Restore(fileSet, profileInCooldown); err != nil {
			t.Fatalf("Force restore failed: %v", err)
		}
	})

	// Verify the profile is now active
	active, err := vault.ActiveProfile(fileSet)
	if err != nil {
		t.Fatalf("ActiveProfile check failed: %v", err)
	}
	if active != profileInCooldown {
		t.Errorf("Expected %s to be active after force restore, got %s", profileInCooldown, active)
	}
	h.LogInfo("Force activation successful", "active_profile", active)

	// Verify cooldown still exists in DB (force doesn't clear it)
	event, err := db.ActiveCooldown("codex", profileInCooldown, time.Now())
	if err != nil {
		t.Fatalf("Failed to check cooldown: %v", err)
	}
	if event == nil {
		t.Errorf("Expected cooldown to still exist after force activation")
	} else {
		h.LogInfo("Cooldown preserved after force", "until", event.CooldownUntil.Format(time.RFC3339))
	}

	h.EndStep("force_activate")

	// ==========================================================================
	// PHASE 4: Record Activation in DB for Tracking
	// ==========================================================================
	h.StartStep("record_activation", "Recording activation in database")

	// Record the activation using LogEvent
	activationEvent := caamdb.Event{
		Timestamp:   time.Now(),
		Type:        "activate",
		Provider:    "codex",
		ProfileName: profileInCooldown,
	}
	err = db.LogEvent(activationEvent)
	if err != nil {
		t.Fatalf("Failed to record activation: %v", err)
	}

	// Verify last activation is recorded
	lastActivation, err := db.LastActivation("codex", profileInCooldown)
	if err != nil {
		t.Fatalf("Failed to get last activation: %v", err)
	}
	if lastActivation.IsZero() {
		t.Errorf("Expected last activation to be recorded")
	}
	h.LogInfo("Activation recorded", "last_activation", lastActivation.Format(time.RFC3339))

	h.EndStep("record_activation")

	t.Log("\n" + h.Summary())
}

// TestE2E_CooldownListAndClearAll tests listing and clearing all cooldowns.
func TestE2E_CooldownListAndClearAll(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// ==========================================================================
	// PHASE 1: Setup Multiple Cooldowns
	// ==========================================================================
	h.StartStep("setup", "Setting up multiple cooldowns")

	dbPath := filepath.Join(h.TempDir, "caam.db")
	db, err := caamdb.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create cooldowns for multiple profiles across providers
	cooldowns := []struct {
		provider string
		profile  string
		duration time.Duration
	}{
		{"codex", "work", 30 * time.Minute},
		{"codex", "personal", 60 * time.Minute},
		{"claude", "main", 45 * time.Minute},
	}

	for _, c := range cooldowns {
		_, err := db.SetCooldown(c.provider, c.profile, time.Now(), c.duration, "test")
		if err != nil {
			t.Fatalf("Failed to set cooldown for %s/%s: %v", c.provider, c.profile, err)
		}
	}
	h.LogInfo("Created cooldowns", "count", len(cooldowns))

	h.EndStep("setup")

	// ==========================================================================
	// PHASE 2: List Active Cooldowns
	// ==========================================================================
	h.StartStep("list_cooldowns", "Listing active cooldowns")

	active, err := db.ListActiveCooldowns(time.Now())
	if err != nil {
		t.Fatalf("ListActiveCooldowns failed: %v", err)
	}

	if len(active) != len(cooldowns) {
		t.Errorf("Expected %d active cooldowns, got %d", len(cooldowns), len(active))
	}

	for _, ev := range active {
		h.LogInfo("Active cooldown",
			"provider", ev.Provider,
			"profile", ev.ProfileName,
			"until", ev.CooldownUntil.Format(time.RFC3339))
	}

	h.EndStep("list_cooldowns")

	// ==========================================================================
	// PHASE 3: Clear All Cooldowns
	// ==========================================================================
	h.StartStep("clear_all", "Clearing all cooldowns")

	cleared, err := db.ClearAllCooldowns()
	if err != nil {
		t.Fatalf("ClearAllCooldowns failed: %v", err)
	}
	h.LogInfo("Cleared cooldowns", "count", cleared)

	// Verify no active cooldowns remain
	active, err = db.ListActiveCooldowns(time.Now())
	if err != nil {
		t.Fatalf("ListActiveCooldowns after clear failed: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("Expected 0 active cooldowns after clear, got %d", len(active))
	}

	h.EndStep("clear_all")

	t.Log("\n" + h.Summary())
}

// TestE2E_RotationWithRecencyPenalty tests that the smart algorithm
// considers recency when selecting profiles.
func TestE2E_RotationWithRecencyPenalty(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// ==========================================================================
	// PHASE 1: Setup
	// ==========================================================================
	h.StartStep("setup", "Setting up profiles with activation history")

	dbPath := filepath.Join(h.TempDir, "caam.db")
	db, err := caamdb.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	profiles := []string{"recent", "old", "never"}

	// Record recent activation for "recent" using LogEvent
	recentEvent := caamdb.Event{
		Timestamp:   time.Now().Add(-5 * time.Minute),
		Type:        "activate",
		Provider:    "codex",
		ProfileName: "recent",
	}
	err = db.LogEvent(recentEvent)
	if err != nil {
		t.Fatalf("Failed to record activation: %v", err)
	}

	// Record old activation for "old" using LogEvent
	oldEvent := caamdb.Event{
		Timestamp:   time.Now().Add(-2 * time.Hour),
		Type:        "activate",
		Provider:    "codex",
		ProfileName: "old",
	}
	err = db.LogEvent(oldEvent)
	if err != nil {
		t.Fatalf("Failed to record activation: %v", err)
	}

	// "never" has no activation history

	h.LogInfo("Setup complete",
		"recent_activated", "5 min ago",
		"old_activated", "2 hours ago",
		"never_activated", "never")

	h.EndStep("setup")

	// ==========================================================================
	// PHASE 2: Verify Recency Affects Selection
	// ==========================================================================
	h.StartStep("recency_selection", "Testing recency affects smart selection")

	selector := rotation.NewSelector(rotation.AlgorithmSmart, nil, db)
	selector.SetRNG(rand.New(rand.NewSource(42)))
	// Default avoid recent is 30 minutes

	result, err := selector.Select("codex", profiles, "")
	if err != nil {
		t.Fatalf("Selection failed: %v", err)
	}

	// "recent" should have a penalty, so "old" or "never" should be preferred
	if result.Selected == "recent" {
		// Check if the score reflects the penalty
		for _, alt := range result.Alternatives {
			if alt.Name == "recent" {
				hasRecencyPenalty := false
				for _, r := range alt.Reasons {
					if !r.Positive && (contains(r.Text, "recently") || contains(r.Text, "Used")) {
						hasRecencyPenalty = true
						break
					}
				}
				if !hasRecencyPenalty {
					t.Errorf("Expected recency penalty for recent")
				}
			}
		}
	}

	h.LogInfo("Selection result",
		"selected", result.Selected,
		"algorithm", string(result.Algorithm))

	// Log all scores for visibility
	for _, alt := range result.Alternatives {
		h.LogInfo("Profile score",
			"profile", alt.Name,
			"score", alt.Score,
			"reason_count", len(alt.Reasons))
	}

	h.EndStep("recency_selection")

	t.Log("\n" + h.Summary())
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
