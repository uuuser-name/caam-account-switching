package rotation

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
)

func TestSetUsageData_AppliesToSmartSelection(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(7)))
	s.SetAvoidRecent(0)

	usage := map[string]*UsageInfo{
		"near-limit": {
			ProfileName:      "near-limit",
			PrimaryPercent:   95,
			SecondaryPercent: 90,
			AvailScore:       0,
		},
		"healthy": {
			ProfileName:      "healthy",
			PrimaryPercent:   20,
			SecondaryPercent: 10,
			AvailScore:       100,
		},
	}
	s.SetUsageData(usage)

	if s.usageData["healthy"] != usage["healthy"] {
		t.Fatal("SetUsageData should store usage data for selection")
	}

	result, err := s.Select("codex", []string{"near-limit", "healthy"}, "")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selected != "healthy" {
		t.Fatalf("Selected = %q, want %q", result.Selected, "healthy")
	}

	var sawNearLimitReason, sawHealthyReason bool
	for _, alt := range result.Alternatives {
		for _, reason := range alt.Reasons {
			if alt.Name == "near-limit" && strings.Contains(reason.Text, "Secondary limit 90% used") {
				sawNearLimitReason = true
			}
			if alt.Name == "healthy" && strings.Contains(reason.Text, "Primary limit 20% used (plenty available)") {
				sawHealthyReason = true
			}
		}
	}
	if !sawNearLimitReason {
		t.Fatal("expected near-limit alternative to explain secondary limit pressure")
	}
	if !sawHealthyReason {
		t.Fatal("expected healthy alternative to explain available primary capacity")
	}
}

func TestSelectSmart_UsageFetchErrorAddsPenaltyReason(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(11)))
	s.SetAvoidRecent(0)
	s.SetUsageData(map[string]*UsageInfo{
		"erroring": {
			ProfileName: "erroring",
			Error:       "timeout",
		},
		"stable": {
			ProfileName:      "stable",
			PrimaryPercent:   35,
			SecondaryPercent: 10,
			AvailScore:       90,
		},
	})

	result, err := s.Select("codex", []string{"erroring", "stable"}, "")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selected != "stable" {
		t.Fatalf("Selected = %q, want %q", result.Selected, "stable")
	}

	for _, alt := range result.Alternatives {
		if alt.Name != "erroring" {
			continue
		}
		for _, reason := range alt.Reasons {
			if strings.Contains(reason.Text, "Usage data unavailable") {
				return
			}
		}
		t.Fatal("expected erroring alternative to include usage-data-unavailable reason")
	}

	t.Fatal("expected alternatives to include erroring profile")
}

func TestCooldownRemaining_UsesActiveCooldown(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := caamdb.OpenAt(filepath.Join(tmpDir, "caam.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	_, err = db.SetCooldown("codex", "blocked", now.Add(-15*time.Minute), 45*time.Minute, "rate limit")
	if err != nil {
		t.Fatalf("SetCooldown() error = %v", err)
	}

	s := NewSelector(AlgorithmSmart, nil, db)

	remaining := s.cooldownRemaining("codex", "blocked", now)
	if remaining != 30*time.Minute {
		t.Fatalf("cooldownRemaining() = %s, want %s", remaining, 30*time.Minute)
	}
	if got := s.cooldownRemaining("codex", "other", now); got != 0 {
		t.Fatalf("cooldownRemaining(other) = %s, want 0", got)
	}
	if got := s.cooldownRemaining("codex", "blocked", now.Add(2*time.Hour)); got != 0 {
		t.Fatalf("cooldownRemaining(expired) = %s, want 0", got)
	}
}

func TestSelectSmart_IncludesCooldownAlternativeReason(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := caamdb.OpenAt(filepath.Join(tmpDir, "caam.db"))
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	_, err = db.SetCooldown("codex", "blocked", now.Add(-10*time.Minute), time.Hour, "rate limit")
	if err != nil {
		t.Fatalf("SetCooldown() error = %v", err)
	}

	s := NewSelector(AlgorithmSmart, nil, db)
	s.SetRNG(rand.New(rand.NewSource(13)))

	result, err := s.Select("codex", []string{"blocked", "open"}, "")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selected != "open" {
		t.Fatalf("Selected = %q, want %q", result.Selected, "open")
	}

	for _, alt := range result.Alternatives {
		if alt.Name != "blocked" {
			continue
		}
		for _, reason := range alt.Reasons {
			if strings.Contains(reason.Text, "In cooldown (") {
				return
			}
		}
		t.Fatal("expected blocked alternative to include cooldown reason")
	}

	t.Fatal("expected alternatives to include blocked profile")
}
