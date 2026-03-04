package coordinator

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDetectState(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected PaneState
	}{
		{
			name:     "idle output",
			output:   "Normal terminal output\nNothing special here",
			expected: StateIdle,
		},
		{
			name:     "rate limit detected",
			output:   "You've hit your limit on Claude usage today. This resets 2pm (America/New_York)",
			expected: StateRateLimited,
		},
		{
			name:     "method selection prompt",
			output:   "Select login method:\n1. Claude account with subscription\n2. API key",
			expected: StateAwaitingMethodSelect,
		},
		{
			name:     "OAuth URL shown",
			output:   "Open this URL in your browser: https://claude.ai/oauth/authorize?code_challenge=abc123",
			expected: StateAwaitingURL,
		},
		{
			name:     "paste code prompt",
			output:   "Paste code here if prompted > ",
			expected: StateAwaitingURL,
		},
		{
			name:     "login success",
			output:   "Logged in as user@example.com\nReady to continue",
			expected: StateResuming,
		},
		{
			name:     "login success - welcome back",
			output:   "Welcome back! Session resumed.",
			expected: StateResuming,
		},
		{
			name:     "login failed",
			output:   "Login failed: invalid code",
			expected: StateFailed,
		},
		{
			name:     "login failed - expired",
			output:   "Authentication error: code expired",
			expected: StateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, _ := DetectState(tt.output)
			if state != tt.expected {
				t.Errorf("DetectState() = %v, want %v", state, tt.expected)
			}
		})
	}
}

func TestDetectStateMetadata(t *testing.T) {
	// Test that reset time is extracted from rate limit message
	output := "You've hit your limit. This resets 2pm (America/New_York)"
	state, metadata := DetectState(output)

	if state != StateRateLimited {
		t.Errorf("expected StateRateLimited, got %v", state)
	}

	if resetTime, ok := metadata["reset_time"]; !ok || resetTime != "2pm" {
		t.Errorf("expected reset_time=2pm, got %v", metadata)
	}
}

func TestExtractOAuthURL(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "URL in output",
			output:   "Please visit: https://claude.ai/oauth/authorize?code_challenge=xyz123&client_id=claude-code",
			expected: "https://claude.ai/oauth/authorize?code_challenge=xyz123&client_id=claude-code",
		},
		{
			name:     "no URL",
			output:   "Just some regular text",
			expected: "",
		},
		{
			name:     "URL with extra text after",
			output:   "Open https://claude.ai/oauth/authorize?foo=bar in browser",
			expected: "https://claude.ai/oauth/authorize?foo=bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := ExtractOAuthURL(tt.output)
			if url != tt.expected {
				t.Errorf("ExtractOAuthURL() = %q, want %q", url, tt.expected)
			}
		})
	}
}

func TestPaneTracker(t *testing.T) {
	tracker := NewPaneTracker(123)

	// Initial state should be idle
	if tracker.GetState() != StateIdle {
		t.Errorf("initial state = %v, want StateIdle", tracker.GetState())
	}

	// Verify pane ID
	if tracker.PaneID != 123 {
		t.Errorf("pane ID = %d, want 123", tracker.PaneID)
	}

	// Set state to rate limited
	tracker.SetState(StateRateLimited)
	if tracker.GetState() != StateRateLimited {
		t.Errorf("state after SetState = %v, want StateRateLimited", tracker.GetState())
	}

	// TimeSinceStateChange should be small
	if tracker.TimeSinceStateChange() > time.Second {
		t.Errorf("time since state change too large")
	}

	// Reset should return to idle
	tracker.Reset()
	if tracker.GetState() != StateIdle {
		t.Errorf("state after Reset = %v, want StateIdle", tracker.GetState())
	}

	// Reset should clear fields
	tracker.OAuthURL = "https://example.com"
	tracker.RequestID = "req-123"
	tracker.ReceivedCode = "code-456"
	tracker.UsedAccount = "user@example.com"
	tracker.ErrorMessage = "some error"
	tracker.Reset()

	if tracker.OAuthURL != "" {
		t.Errorf("OAuthURL not cleared")
	}
	if tracker.RequestID != "" {
		t.Errorf("RequestID not cleared")
	}
	if tracker.ReceivedCode != "" {
		t.Errorf("ReceivedCode not cleared")
	}
	if tracker.UsedAccount != "" {
		t.Errorf("UsedAccount not cleared")
	}
	if tracker.ErrorMessage != "" {
		t.Errorf("ErrorMessage not cleared")
	}
}

func TestPaneStateString(t *testing.T) {
	tests := []struct {
		state    PaneState
		expected string
	}{
		{StateIdle, "IDLE"},
		{StateRateLimited, "RATE_LIMITED"},
		{StateAwaitingMethodSelect, "AWAITING_METHOD_SELECT"},
		{StateAwaitingURL, "AWAITING_URL"},
		{StateAuthPending, "AUTH_PENDING"},
		{StateCodeReceived, "CODE_RECEIVED"},
		{StateAwaitingConfirm, "AWAITING_CONFIRM"},
		{StateResuming, "RESUMING"},
		{StateFailed, "FAILED"},
		{PaneState(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

type fakePaneClient struct {
	panes  []Pane
	output string
	sent   []string
	mu     sync.Mutex
}

func (f *fakePaneClient) ListPanes(ctx context.Context) ([]Pane, error) {
	return f.panes, nil
}

func (f *fakePaneClient) GetText(ctx context.Context, paneID int, startLine int) (string, error) {
	return f.output, nil
}

func (f *fakePaneClient) SendText(ctx context.Context, paneID int, text string, noPaste bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	return nil
}

func (f *fakePaneClient) IsAvailable(ctx context.Context) bool {
	return true
}

func (f *fakePaneClient) Backend() string {
	return "fake"
}

func (f *fakePaneClient) sentText() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent))
	copy(out, f.sent)
	return out
}

func TestCoordinator_AuthPendingProcessesWithoutOutputChange(t *testing.T) {
	client := &fakePaneClient{
		panes:  []Pane{{PaneID: 1}},
		output: "Paste code here if prompted >",
	}

	cfg := DefaultConfig()
	coord := New(cfg)
	coord.paneClient = client

	tracker := NewPaneTracker(1)
	tracker.LastOutput = client.output
	tracker.SetState(StateAuthPending)
	tracker.SetRequestID("req-1")

	coord.trackers[1] = tracker
	coord.requests["req-1"] = &AuthRequest{
		ID:        "req-1",
		PaneID:    1,
		URL:       "https://claude.ai/oauth/authorize?code_challenge=abc",
		CreatedAt: time.Now(),
		Status:    "pending",
	}

	if err := coord.ReceiveAuthResponse(AuthResponse{
		RequestID: "req-1",
		Code:      "CODE123",
		Account:   "user@example.com",
	}); err != nil {
		t.Fatalf("ReceiveAuthResponse error: %v", err)
	}

	coord.processPaneState(context.Background(), client.panes[0])
	if tracker.GetState() != StateCodeReceived {
		t.Fatalf("state after auth response = %v, want %v", tracker.GetState(), StateCodeReceived)
	}

	coord.processPaneState(context.Background(), client.panes[0])
	if tracker.GetState() != StateAwaitingConfirm {
		t.Fatalf("state after code injection = %v, want %v", tracker.GetState(), StateAwaitingConfirm)
	}

	sent := client.sentText()
	found := false
	for _, s := range sent {
		if s == "CODE123\n" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected code to be injected, sent=%v", sent)
	}
}
