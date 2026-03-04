package prediction

import (
	"testing"
	"time"
)

func TestGenerateAlerts_ApproachingAndImminent(t *testing.T) {
	approaching := &Prediction{
		Provider:        "claude",
		Profile:         "work",
		CurrentPercent:  50,
		TimeToDepletion: 25 * time.Minute,
		Warning:         WarningApproaching,
		Confidence:      0.8,
	}
	imminent := &Prediction{
		Provider:        "codex",
		Profile:         "personal",
		CurrentPercent:  90,
		TimeToDepletion: 5 * time.Minute,
		Warning:         WarningNone,
		Confidence:      0.9,
	}

	alerts := GenerateAlerts([]*Prediction{approaching, imminent}, DefaultAlertOptions())

	if findAlert(alerts, AlertApproachingLimit, "work") == nil {
		t.Fatalf("expected approaching alert for work profile")
	}
	if findAlert(alerts, AlertImminentLimit, "personal") == nil {
		t.Fatalf("expected imminent alert for personal profile")
	}
}

func TestGenerateAlerts_SwitchRecommended(t *testing.T) {
	pred := &Prediction{
		Provider:        "claude",
		Profile:         "work",
		TimeToDepletion: 10 * time.Minute,
		Warning:         WarningApproaching,
		Confidence:      0.8,
	}

	alerts := GenerateAlerts([]*Prediction{pred}, DefaultAlertOptions())
	if findAlert(alerts, AlertSwitchRecommended, "work") == nil {
		t.Fatalf("expected switch recommended alert")
	}
}

func TestGenerateAlerts_AllProfilesLow(t *testing.T) {
	opts := DefaultAlertOptions()
	p1 := &Prediction{Profile: "one", CurrentPercent: opts.WarningPercent + 1, Warning: WarningNone}
	p2 := &Prediction{Profile: "two", CurrentPercent: opts.WarningPercent + 5, Warning: WarningApproaching}

	alerts := GenerateAlerts([]*Prediction{p1, p2}, opts)
	if findAlert(alerts, AlertAllProfilesLow, "") == nil {
		t.Fatalf("expected all profiles low alert")
	}
}

func TestToNotifyAlert(t *testing.T) {
	alert := &Alert{
		Type:      AlertImminentLimit,
		Profile:   "work",
		Message:   "Will hit limit soon",
		Urgency:   UrgencyHigh,
		TimeUntil: 5 * time.Minute,
	}

	notifyAlert := ToNotifyAlert(alert)
	if notifyAlert == nil {
		t.Fatal("expected notify alert")
	}
	if notifyAlert.Profile != "work" {
		t.Fatalf("notify profile = %q, want work", notifyAlert.Profile)
	}
	if notifyAlert.Title != "Rate limit imminent" {
		t.Fatalf("notify title = %q, want rate limit imminent", notifyAlert.Title)
	}
}

func findAlert(alerts []*Alert, alertType AlertType, profile string) *Alert {
	for _, alert := range alerts {
		if alert == nil {
			continue
		}
		if alert.Type != alertType {
			continue
		}
		if profile != "" && alert.Profile != profile {
			continue
		}
		return alert
	}
	return nil
}
