package workflows

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
)

func newDeterministicSelector(db *caamdb.DB) *rotation.Selector {
	selector := rotation.NewSelector(rotation.AlgorithmSmart, nil, db)
	selector.SetRNG(rand.New(rand.NewSource(42)))
	selector.SetAvoidRecent(0)
	return selector
}

func findAlternative(result *rotation.Result, profile string) *rotation.ProfileScore {
	if result == nil {
		return nil
	}
	for i := range result.Alternatives {
		if result.Alternatives[i].Name == profile {
			return &result.Alternatives[i]
		}
	}
	return nil
}

func TestE2E_FiveHourCreditExhaustionSwitch(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("setup", "Preparing profiles and cooldown database")
	db, err := caamdb.OpenAt(filepath.Join(h.TempDir, "caam.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	profiles := []string{"codex-primary", "codex-secondary", "codex-third", "codex-fourth"}
	selector := newDeterministicSelector(db)
	h.EndStep("setup")

	h.StartStep("mark_five_hour_exhaustion", "Marking primary profile exhausted for five-hour window")
	exhausted := "codex-primary"
	hitAt := time.Now()
	event, err := db.SetCooldown("codex", exhausted, hitAt, 5*time.Hour, "credits exhausted: five-hour window")
	if err != nil {
		t.Fatalf("set cooldown: %v", err)
	}
	h.LogInfo("Recorded cooldown event", "profile", exhausted, "cooldown_until", event.CooldownUntil.UTC().Format(time.RFC3339))
	h.EndStep("mark_five_hour_exhaustion")

	h.StartStep("auto_switch", "Selecting alternate profile after five-hour exhaustion")
	result, err := selector.Select("codex", profiles, exhausted)
	if err != nil {
		t.Fatalf("select alternate profile: %v", err)
	}
	if result.Selected == exhausted {
		t.Fatalf("expected alternate profile, got exhausted profile %q", exhausted)
	}
	exhaustedScore := findAlternative(result, exhausted)
	if exhaustedScore == nil {
		t.Fatalf("expected exhausted profile to appear in alternatives")
	}
	if exhaustedScore.Score > -9000 {
		t.Fatalf("expected exhausted profile cooldown penalty, got score %.2f", exhaustedScore.Score)
	}
	if len(exhaustedScore.Reasons) == 0 || !strings.Contains(strings.ToLower(exhaustedScore.Reasons[0].Text), "cooldown") {
		t.Fatalf("expected cooldown reason for exhausted profile, got %+v", exhaustedScore.Reasons)
	}
	h.LogInfo("Five-hour failover success", "from", exhausted, "to", result.Selected)
	h.EndStep("auto_switch")

	if err := h.ValidateCanonicalLogs(); err != nil {
		t.Fatalf("canonical log validation failed: %v", err)
	}
	t.Log("\n" + h.Summary())
}

func TestE2E_WeeklyCreditExhaustionSwitch(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("setup", "Preparing profiles and cooldown database")
	db, err := caamdb.OpenAt(filepath.Join(h.TempDir, "caam.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	profiles := []string{"codex-a", "codex-b", "codex-c", "codex-d"}
	selector := newDeterministicSelector(db)
	h.EndStep("setup")

	h.StartStep("mark_weekly_exhaustion", "Marking profile exhausted for weekly credits")
	weeklyExhausted := "codex-b"
	hitAt := time.Now()
	event, err := db.SetCooldown("codex", weeklyExhausted, hitAt, 7*24*time.Hour, "credits exhausted: weekly budget")
	if err != nil {
		t.Fatalf("set cooldown: %v", err)
	}
	remaining := time.Until(event.CooldownUntil)
	if remaining < 6*24*time.Hour {
		t.Fatalf("expected weekly cooldown horizon, got remaining %s", remaining)
	}
	h.LogInfo("Recorded weekly exhaustion", "profile", weeklyExhausted, "remaining_hours", int(remaining.Hours()))
	h.EndStep("mark_weekly_exhaustion")

	h.StartStep("auto_switch", "Selecting alternate profile after weekly exhaustion")
	result, err := selector.Select("codex", profiles, weeklyExhausted)
	if err != nil {
		t.Fatalf("select alternate profile: %v", err)
	}
	if result.Selected == weeklyExhausted {
		t.Fatalf("expected alternate profile, got weekly-exhausted profile")
	}
	weeklyScore := findAlternative(result, weeklyExhausted)
	if weeklyScore == nil {
		t.Fatalf("expected weekly-exhausted profile to appear in alternatives")
	}
	if weeklyScore.Score > -9000 {
		t.Fatalf("expected strong cooldown penalty, got %.2f", weeklyScore.Score)
	}
	h.LogInfo("Weekly failover success", "from", weeklyExhausted, "to", result.Selected)
	h.EndStep("auto_switch")

	if err := h.ValidateCanonicalLogs(); err != nil {
		t.Fatalf("canonical log validation failed: %v", err)
	}
	t.Log("\n" + h.Summary())
}

func TestE2E_ActiveSessionHandoffContinuityUnderExhaustion(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("setup", "Preparing active session handoff scenario")
	db, err := caamdb.OpenAt(filepath.Join(h.TempDir, "caam.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	profiles := []string{"codex-main", "codex-backup-1", "codex-backup-2", "codex-backup-3"}
	selector := newDeterministicSelector(db)
	sessionID := "sess-001"
	h.EndStep("setup")

	h.StartStep("mark_primary_exhausted", "Setting exhaustion on active session profile")
	activeProfile := "codex-main"
	if _, err := db.SetCooldown("codex", activeProfile, time.Now(), 5*time.Hour, "credits exhausted during active session"); err != nil {
		t.Fatalf("set cooldown: %v", err)
	}
	h.LogInfo("Active profile exhausted", "session_id", sessionID, "profile", activeProfile)
	h.EndStep("mark_primary_exhausted")

	h.StartStep("handoff_sequence", "Performing frictionless selection handoff sequence")
	current := activeProfile
	trail := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		result, err := selector.Select("codex", profiles, current)
		if err != nil {
			t.Fatalf("handoff iteration %d failed: %v", i+1, err)
		}
		if result.Selected == activeProfile {
			t.Fatalf("iteration %d selected exhausted active profile %q", i+1, activeProfile)
		}
		trail = append(trail, result.Selected)
		h.LogInfo("Handoff step", "session_id", sessionID, "iteration", i+1, "from", current, "to", result.Selected)
		current = result.Selected
	}
	if len(trail) != 3 {
		t.Fatalf("expected 3 handoff steps, got %d", len(trail))
	}
	h.EndStep("handoff_sequence")

	if err := h.ValidateCanonicalLogs(); err != nil {
		t.Fatalf("canonical log validation failed: %v", err)
	}
	t.Log("\n" + h.Summary())
}

func TestE2E_CooldownClearNonexistentProfile(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	h.StartStep("setup", "Opening database for nonexistent-clear scenario")
	db, err := caamdb.OpenAt(filepath.Join(h.TempDir, "caam.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	h.EndStep("setup")

	h.StartStep("clear_nonexistent", "Clearing cooldown for profile without entries")
	cleared, err := db.ClearCooldown("codex", "does-not-exist")
	if err != nil {
		t.Fatalf("clear cooldown returned unexpected error: %v", err)
	}
	if cleared != 0 {
		t.Fatalf("expected zero rows cleared for nonexistent profile, got %d", cleared)
	}
	h.LogInfo("Clear nonexistent cooldown handled", "rows_cleared", cleared)
	h.EndStep("clear_nonexistent")

	if err := h.ValidateCanonicalLogs(); err != nil {
		t.Fatalf("canonical log validation failed: %v", err)
	}
	t.Log("\n" + h.Summary())
}
