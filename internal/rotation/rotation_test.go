package rotation

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestNewSelector(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)
	if s == nil {
		t.Fatal("expected non-nil selector")
	}
	if s.algorithm != AlgorithmSmart {
		t.Errorf("expected algorithm %q, got %q", AlgorithmSmart, s.algorithm)
	}
}

func TestSelectRandom(t *testing.T) {
	s := NewSelector(AlgorithmRandom, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42))) // Fixed seed for determinism

	profiles := []string{"alpha", "beta", "gamma"}
	result, err := s.Select("claude", profiles, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Algorithm != AlgorithmRandom {
		t.Errorf("expected algorithm %q, got %q", AlgorithmRandom, result.Algorithm)
	}

	// Selected should be one of the profiles
	found := false
	for _, p := range profiles {
		if result.Selected == p {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("selected profile %q not in input list", result.Selected)
	}

	// All profiles should appear in alternatives
	if len(result.Alternatives) != len(profiles) {
		t.Errorf("expected %d alternatives, got %d", len(profiles), len(result.Alternatives))
	}

	t.Run("excludes current profile when alternatives exist", func(t *testing.T) {
		result, err := s.Select("claude", profiles, "alpha")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Selected == "alpha" {
			t.Fatalf("selected current profile %q; expected a different profile", result.Selected)
		}
	})
}

func TestSelectRoundRobin(t *testing.T) {
	s := NewSelector(AlgorithmRoundRobin, nil, nil)

	t.Run("selects next profile in sequence", func(t *testing.T) {
		profiles := []string{"alpha", "beta", "gamma"}
		result, err := s.Select("claude", profiles, "alpha")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Selected != "beta" {
			t.Errorf("expected 'beta' after 'alpha', got %q", result.Selected)
		}
	})

	t.Run("wraps around to first profile", func(t *testing.T) {
		profiles := []string{"alpha", "beta", "gamma"}
		result, err := s.Select("claude", profiles, "gamma")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Selected != "alpha" {
			t.Errorf("expected 'alpha' after 'gamma', got %q", result.Selected)
		}
	})

	t.Run("handles no current profile", func(t *testing.T) {
		profiles := []string{"alpha", "beta", "gamma"}
		result, err := s.Select("claude", profiles, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should select first in sorted order
		if result.Selected != "alpha" {
			t.Errorf("expected 'alpha' with no current, got %q", result.Selected)
		}
	})
}

func TestSelectSmart(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42))) // Fixed seed for determinism

	t.Run("selects from available profiles", func(t *testing.T) {
		profiles := []string{"alpha", "beta", "gamma"}
		result, err := s.Select("claude", profiles, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Algorithm != AlgorithmSmart {
			t.Errorf("expected algorithm %q, got %q", AlgorithmSmart, result.Algorithm)
		}

		// Selected should be one of the profiles
		found := false
		for _, p := range profiles {
			if result.Selected == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("selected profile %q not in input list", result.Selected)
		}
	})

	t.Run("provides reasons for selection", func(t *testing.T) {
		profiles := []string{"work", "personal"}
		result, err := s.Select("claude", profiles, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have at least one alternative with reasons
		if len(result.Alternatives) == 0 {
			t.Fatal("expected alternatives")
		}

		hasReasons := false
		for _, alt := range result.Alternatives {
			if len(alt.Reasons) > 0 {
				hasReasons = true
				break
			}
		}
		if !hasReasons {
			t.Error("expected at least one alternative with reasons")
		}
	})

	t.Run("excludes current profile when alternatives exist", func(t *testing.T) {
		profiles := []string{"alpha", "beta", "gamma"}
		result, err := s.Select("claude", profiles, "alpha")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Selected == "alpha" {
			t.Fatalf("selected current profile %q; expected a different profile", result.Selected)
		}
	})
}

func TestSelectFiltersSystemProfiles(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)

	t.Run("excludes system profiles", func(t *testing.T) {
		profiles := []string{"_original", "_backup_20241217", "work", "personal"}
		result, err := s.Select("claude", profiles, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should not select a system profile
		if strings.HasPrefix(result.Selected, "_") {
			t.Errorf("selected system profile %q", result.Selected)
		}

		// Alternatives should only have user profiles
		for _, alt := range result.Alternatives {
			if strings.HasPrefix(alt.Name, "_") {
				t.Errorf("alternative contains system profile %q", alt.Name)
			}
		}
	})

	t.Run("errors when only system profiles exist", func(t *testing.T) {
		profiles := []string{"_original", "_backup_20241217"}
		_, err := s.Select("claude", profiles, "")
		if err == nil {
			t.Fatal("expected error when only system profiles exist")
		}
		if !strings.Contains(err.Error(), "no user profiles") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestSelectSingleProfile(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)

	profiles := []string{"only-one"}
	result, err := s.Select("claude", profiles, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Selected != "only-one" {
		t.Errorf("expected 'only-one', got %q", result.Selected)
	}

	// Should have a reason indicating it's the only profile
	found := false
	for _, alt := range result.Alternatives {
		if alt.Name == "only-one" {
			for _, r := range alt.Reasons {
				if strings.Contains(r.Text, "Only available") {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("expected 'only available profile' reason")
	}
}

func TestSelectNoProfiles(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)

	_, err := s.Select("claude", nil, "")
	if err == nil {
		t.Fatal("expected error with no profiles")
	}
	if !strings.Contains(err.Error(), "no profiles") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{2 * time.Hour, "2h"},
		{25 * time.Hour, "1d 1h"},
		{48 * time.Hour, "2d"},
		{50 * time.Hour, "2d 2h"},
	}

	for _, tc := range tests {
		result := formatDuration(tc.duration)
		if result != tc.expected {
			t.Errorf("formatDuration(%v) = %q, expected %q", tc.duration, result, tc.expected)
		}
	}
}

func TestFormatResult(t *testing.T) {
	t.Run("nil result", func(t *testing.T) {
		output := FormatResult(nil)
		if !strings.Contains(output, "No selection") {
			t.Errorf("unexpected output for nil: %q", output)
		}
	})

	t.Run("basic result", func(t *testing.T) {
		result := &Result{
			Selected:  "work",
			Algorithm: AlgorithmSmart,
			Alternatives: []ProfileScore{
				{
					Name:  "work",
					Score: 150,
					Reasons: []Reason{
						{Text: "Healthy token", Positive: true},
						{Text: "Not used recently", Positive: true},
					},
				},
				{
					Name:  "personal",
					Score: 100,
					Reasons: []Reason{
						{Text: "Used recently", Positive: false},
					},
				},
			},
		}

		output := FormatResult(result)
		if !strings.Contains(output, "Recommended: work") {
			t.Errorf("output missing recommended profile: %q", output)
		}
		if !strings.Contains(output, "Healthy token") {
			t.Errorf("output missing reason: %q", output)
		}
		if !strings.Contains(output, "Alternatives:") {
			t.Errorf("output missing alternatives section: %q", output)
		}
		if !strings.Contains(output, "personal") {
			t.Errorf("output missing alternative profile: %q", output)
		}
	})

	t.Run("with cooldown profiles", func(t *testing.T) {
		result := &Result{
			Selected:  "work",
			Algorithm: AlgorithmSmart,
			Alternatives: []ProfileScore{
				{
					Name:  "work",
					Score: 150,
					Reasons: []Reason{
						{Text: "Healthy", Positive: true},
					},
				},
				{
					Name:  "blocked",
					Score: -10000,
					Reasons: []Reason{
						{Text: "In cooldown (2h remaining)", Positive: false},
					},
				},
			},
		}

		output := FormatResult(result)
		if !strings.Contains(output, "In cooldown:") {
			t.Errorf("output missing cooldown section: %q", output)
		}
		if !strings.Contains(output, "blocked") {
			t.Errorf("output missing cooldown profile: %q", output)
		}
	})
}

func TestAlgorithmConstants(t *testing.T) {
	// Ensure algorithm constants have expected values
	if AlgorithmSmart != "smart" {
		t.Errorf("AlgorithmSmart = %q, expected 'smart'", AlgorithmSmart)
	}
	if AlgorithmRoundRobin != "round_robin" {
		t.Errorf("AlgorithmRoundRobin = %q, expected 'round_robin'", AlgorithmRoundRobin)
	}
	if AlgorithmRandom != "random" {
		t.Errorf("AlgorithmRandom = %q, expected 'random'", AlgorithmRandom)
	}
}

func TestSetAvoidRecent(t *testing.T) {
	s := NewSelector(AlgorithmSmart, nil, nil)

	// Default
	if s.avoidRecent != 30*time.Minute {
		t.Errorf("default avoidRecent = %v, expected 30m", s.avoidRecent)
	}

	// After setting
	s.SetAvoidRecent(2 * time.Hour)
	if s.avoidRecent != 2*time.Hour {
		t.Errorf("avoidRecent = %v, expected 2h", s.avoidRecent)
	}
}
