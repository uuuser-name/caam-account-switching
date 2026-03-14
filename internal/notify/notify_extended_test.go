package notify

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

type recordingNotifier struct {
	available bool
	err       error
	calls     int
}

func (n *recordingNotifier) Notify(*Alert) error {
	n.calls++
	return n.err
}

func (n *recordingNotifier) Name() string { return "recording" }

func (n *recordingNotifier) Available() bool { return n.available }

func TestAlertLevelStringExtended(t *testing.T) {
	tests := []struct {
		level AlertLevel
		want  string
	}{
		{level: Info, want: "INFO"},
		{level: Warning, want: "WARNING"},
		{level: Critical, want: "CRITICAL"},
		{level: AlertLevel(99), want: "UNKNOWN"},
	}

	for _, tc := range tests {
		if got := tc.level.String(); got != tc.want {
			t.Fatalf("level %v string = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestNotifierMetadataAndAvailability(t *testing.T) {
	terminal := NewTerminalNotifier(nil, true)
	if terminal == nil {
		t.Fatal("expected terminal notifier")
	}
	if terminal.Name() != "terminal" {
		t.Fatalf("terminal name = %q", terminal.Name())
	}
	if !terminal.Available() {
		t.Fatal("terminal notifier should always be available")
	}
	if terminal.writer == nil {
		t.Fatal("expected nil writer to default to stderr")
	}

	desktop := NewDesktopNotifier()
	if desktop == nil {
		t.Fatal("expected desktop notifier")
	}
	if desktop.Name() != "desktop" {
		t.Fatalf("desktop name = %q", desktop.Name())
	}

	expectedDesktopAvailable := false
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("osascript")
		expectedDesktopAvailable = err == nil
	case "linux":
		_, err := exec.LookPath("notify-send")
		expectedDesktopAvailable = err == nil
	}
	if desktop.Available() != expectedDesktopAvailable {
		t.Fatalf("desktop availability = %v, want %v", desktop.Available(), expectedDesktopAvailable)
	}

	webhook := NewWebhookNotifier("")
	if webhook.Name() != "webhook" {
		t.Fatalf("webhook name = %q", webhook.Name())
	}
	if webhook.Available() {
		t.Fatal("empty webhook URL should be unavailable")
	}

	primary := &recordingNotifier{available: false}
	secondary := &recordingNotifier{available: true}
	multi := NewMultiNotifier(primary, secondary)
	if multi.Name() != "multi" {
		t.Fatalf("multi name = %q", multi.Name())
	}
	if !multi.Available() {
		t.Fatal("multi notifier should be available when any child is available")
	}
}

func TestDesktopNotifierUnavailableError(t *testing.T) {
	t.Setenv("PATH", "")

	err := NewDesktopNotifier().Notify(&Alert{
		Level:   Info,
		Title:   "Test",
		Message: "Desktop path",
	})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("desktop notify error = %v, want unavailable error", err)
	}
}

func TestWebhookNotifierEdgeCases(t *testing.T) {
	if err := NewWebhookNotifier("").Notify(&Alert{}); err != nil {
		t.Fatalf("empty-url webhook should no-op, got %v", err)
	}

	invalid := NewWebhookNotifier("://bad")
	err := invalid.Notify(&Alert{Timestamp: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "create request") {
		t.Fatalf("invalid-url webhook error = %v, want request creation failure", err)
	}
}

func TestMultiNotifierSkipsUnavailableAndAggregatesErrors(t *testing.T) {
	unavailable := &recordingNotifier{available: false}
	ok := &recordingNotifier{available: true}
	failing := &recordingNotifier{available: true, err: errors.New("boom")}

	err := NewMultiNotifier(unavailable, ok, failing).Notify(&Alert{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("multi notifier error = %v, want joined notifier error", err)
	}
	if unavailable.calls != 0 {
		t.Fatalf("unavailable notifier calls = %d, want 0", unavailable.calls)
	}
	if ok.calls != 1 {
		t.Fatalf("ok notifier calls = %d, want 1", ok.calls)
	}
	if failing.calls != 1 {
		t.Fatalf("failing notifier calls = %d, want 1", failing.calls)
	}
}
