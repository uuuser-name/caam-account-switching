package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type coveragePaneClient struct {
	mu      sync.Mutex
	panes   []Pane
	outputs map[int]string
	sent    map[int][]string
	listErr error
	getErr  error
	sendErr error
}

func newCoveragePaneClient(panes []Pane) *coveragePaneClient {
	return &coveragePaneClient{
		panes:   panes,
		outputs: map[int]string{},
		sent:    map[int][]string{},
	}
}

func (f *coveragePaneClient) ListPanes(context.Context) ([]Pane, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Pane, len(f.panes))
	copy(out, f.panes)
	return out, nil
}

func (f *coveragePaneClient) GetText(_ context.Context, paneID int, _ int) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outputs[paneID], nil
}

func (f *coveragePaneClient) SendText(_ context.Context, paneID int, text string, _ bool) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[paneID] = append(f.sent[paneID], text)
	return nil
}

func (f *coveragePaneClient) IsAvailable(context.Context) bool { return true }
func (f *coveragePaneClient) Backend() string                  { return "fake" }

func (f *coveragePaneClient) setPaneOutput(paneID int, out string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outputs[paneID] = out
}

func (f *coveragePaneClient) sentForPane(paneID int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent[paneID]))
	copy(out, f.sent[paneID])
	return out
}

func newCoverageCoordinator(client PaneClient) *Coordinator {
	cfg := DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	cfg.AuthTimeout = 250 * time.Millisecond
	cfg.StateTimeout = 500 * time.Millisecond
	cfg.ResumePrompt = "resume\n"
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	c := New(cfg)
	c.paneClient = client
	return c
}

func TestCoordinatorStateMachineHappyPath(t *testing.T) {
	client := newCoveragePaneClient([]Pane{{PaneID: 11}})
	coord := newCoverageCoordinator(client)

	var gotReq *AuthRequest
	var gotCompletedPane int
	var gotCompletedAccount string
	coord.OnAuthRequest = func(req *AuthRequest) { gotReq = req }
	coord.OnAuthComplete = func(paneID int, account string) {
		gotCompletedPane = paneID
		gotCompletedAccount = account
	}

	client.setPaneOutput(11, "You've hit your limit. this resets 2pm")
	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := coord.trackers[11].GetState(); state != StateRateLimited {
		t.Fatalf("expected RATE_LIMITED, got %s", state)
	}
	assertSentContains(t, client.sentForPane(11), "/login\n")

	client.setPaneOutput(11, "Select login method:")
	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := coord.trackers[11].GetState(); state != StateAwaitingMethodSelect {
		t.Fatalf("expected AWAITING_METHOD_SELECT, got %s", state)
	}
	assertSentContains(t, client.sentForPane(11), "1\n")

	oauthURL := "https://claude.ai/oauth/authorize?code=true&x=1"
	client.setPaneOutput(11, "Open this URL: "+oauthURL+"\nPaste code here if prompted >")
	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := coord.trackers[11].GetState(); state != StateAwaitingURL {
		t.Fatalf("expected AWAITING_URL, got %s", state)
	}

	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	tracker := coord.trackers[11]
	if state := tracker.GetState(); state != StateAuthPending {
		t.Fatalf("expected AUTH_PENDING, got %s", state)
	}
	if gotReq == nil || gotReq.PaneID != 11 || gotReq.URL != oauthURL {
		t.Fatalf("expected auth request callback with pane/url, got %+v", gotReq)
	}

	if err := coord.ReceiveAuthResponse(AuthResponse{
		RequestID: tracker.GetRequestID(),
		Code:      "CODE-123",
		Account:   "acct@example.com",
	}); err != nil {
		t.Fatalf("ReceiveAuthResponse failed: %v", err)
	}

	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := tracker.GetState(); state != StateCodeReceived {
		t.Fatalf("expected CODE_RECEIVED, got %s", state)
	}

	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := tracker.GetState(); state != StateAwaitingConfirm {
		t.Fatalf("expected AWAITING_CONFIRM, got %s", state)
	}
	assertSentContains(t, client.sentForPane(11), "CODE-123\n")

	client.setPaneOutput(11, "Logged in as acct@example.com")
	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := tracker.GetState(); state != StateResuming {
		t.Fatalf("expected RESUMING, got %s", state)
	}

	coord.processPaneState(context.Background(), Pane{PaneID: 11})
	if state := tracker.GetState(); state != StateIdle {
		t.Fatalf("expected IDLE after resume, got %s", state)
	}
	assertSentContains(t, client.sentForPane(11), "resume\n")

	if gotCompletedPane != 11 || gotCompletedAccount != "acct@example.com" {
		t.Fatalf("unexpected completion callback pane=%d account=%q", gotCompletedPane, gotCompletedAccount)
	}
}

func TestCoordinatorStateMachineErrorsAndTimeouts(t *testing.T) {
	client := newCoveragePaneClient([]Pane{{PaneID: 42}})
	coord := newCoverageCoordinator(client)
	coord.trackers[42] = NewPaneTracker(42)

	t.Run("rate limited timeout resets", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.SetState(StateRateLimited)
		tr.mu.Lock()
		tr.StateEntered = time.Now().Add(-coord.config.StateTimeout * 2)
		tr.mu.Unlock()
		client.setPaneOutput(42, "still rate limited")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateIdle {
			t.Fatalf("expected reset to IDLE, got %s", tr.GetState())
		}
	})

	t.Run("awaiting method select timeout resets", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.SetState(StateAwaitingMethodSelect)
		tr.mu.Lock()
		tr.StateEntered = time.Now().Add(-coord.config.StateTimeout * 2)
		tr.mu.Unlock()
		client.setPaneOutput(42, "stuck")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateIdle {
			t.Fatalf("expected reset to IDLE, got %s", tr.GetState())
		}
	})

	t.Run("awaiting url timeout resets", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.SetState(StateAwaitingURL)
		tr.mu.Lock()
		tr.StateEntered = time.Now().Add(-coord.config.StateTimeout * 2)
		tr.mu.Unlock()
		client.setPaneOutput(42, "waiting")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateIdle {
			t.Fatalf("expected reset to IDLE, got %s", tr.GetState())
		}
	})

	t.Run("auth pending timeout fails", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.SetState(StateAuthPending)
		tr.SetRequestID("req-timeout")
		coord.requests["req-timeout"] = &AuthRequest{ID: "req-timeout", PaneID: 42, Status: "pending"}

		var gotErr error
		coord.OnAuthFailed = func(_ int, err error) { gotErr = err }
		tr.mu.Lock()
		tr.StateEntered = time.Now().Add(-coord.config.AuthTimeout * 2)
		tr.mu.Unlock()
		client.setPaneOutput(42, "waiting for code")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateFailed {
			t.Fatalf("expected FAILED, got %s", tr.GetState())
		}
		if gotErr == nil || !strings.Contains(gotErr.Error(), "auth timeout") {
			t.Fatalf("expected auth timeout callback error, got %v", gotErr)
		}
	})

	t.Run("code received without code fails", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.Reset()
		tr.SetState(StateCodeReceived)
		client.setPaneOutput(42, "anything")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateFailed {
			t.Fatalf("expected FAILED, got %s", tr.GetState())
		}
	})

	t.Run("code injection send error fails", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.Reset()
		tr.SetState(StateCodeReceived)
		tr.SetAuthResponse("ABC", "acct")
		client.sendErr = errors.New("send failed")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		client.sendErr = nil
		if tr.GetState() != StateFailed {
			t.Fatalf("expected FAILED, got %s", tr.GetState())
		}
		if !strings.Contains(tr.GetErrorMessage(), "send failed") {
			t.Fatalf("expected send error message, got %q", tr.GetErrorMessage())
		}
	})

	t.Run("awaiting confirm failed signal", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.Reset()
		tr.SetState(StateAwaitingConfirm)
		var gotErr error
		coord.OnAuthFailed = func(_ int, err error) { gotErr = err }
		client.setPaneOutput(42, "Login failed: invalid code")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateFailed {
			t.Fatalf("expected FAILED, got %s", tr.GetState())
		}
		if gotErr == nil || !strings.Contains(gotErr.Error(), "login failed") {
			t.Fatalf("expected login failed callback, got %v", gotErr)
		}
	})

	t.Run("awaiting confirm timeout fails", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.Reset()
		tr.SetState(StateAwaitingConfirm)
		tr.mu.Lock()
		tr.StateEntered = time.Now().Add(-coord.config.StateTimeout * 2)
		tr.mu.Unlock()
		client.setPaneOutput(42, "waiting for confirmation")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateFailed {
			t.Fatalf("expected FAILED, got %s", tr.GetState())
		}
		if !strings.Contains(tr.GetErrorMessage(), "confirmation timeout") {
			t.Fatalf("expected confirmation timeout message, got %q", tr.GetErrorMessage())
		}
	})

	t.Run("failed timeout resets to idle", func(t *testing.T) {
		tr := coord.trackers[42]
		tr.SetState(StateFailed)
		tr.mu.Lock()
		tr.StateEntered = time.Now().Add(-coord.config.StateTimeout * 2)
		tr.mu.Unlock()
		client.setPaneOutput(42, "stale")
		coord.processPaneState(context.Background(), Pane{PaneID: 42})
		if tr.GetState() != StateIdle {
			t.Fatalf("expected IDLE, got %s", tr.GetState())
		}
	})
}

func TestCoordinatorPollingStartStopAndHelpers(t *testing.T) {
	client := newCoveragePaneClient([]Pane{{PaneID: 1}, {PaneID: 2}})
	client.setPaneOutput(1, "normal output")
	client.setPaneOutput(2, "normal output")
	coord := newCoverageCoordinator(client)

	coord.trackers[99] = NewPaneTracker(99)
	coord.pollPanes(context.Background())
	if _, ok := coord.trackers[99]; ok {
		t.Fatalf("expected stale tracker to be removed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := coord.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := coord.Start(ctx); err == nil {
		t.Fatalf("expected second Start to fail")
	}

	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := coord.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := coord.Stop(); err != nil {
		t.Fatalf("second Stop should be no-op, got: %v", err)
	}

	if got := coord.Backend(); got != "fake" {
		t.Fatalf("unexpected backend %q", got)
	}

	status := coord.GetStatus()
	if len(status) == 0 {
		t.Fatalf("expected non-empty status map")
	}
	if len(coord.GetTrackers()) == 0 {
		t.Fatalf("expected trackers")
	}
}

func TestCoordinatorReceiveAuthResponseEdgeCases(t *testing.T) {
	client := newCoveragePaneClient([]Pane{{PaneID: 7}})
	coord := newCoverageCoordinator(client)

	if err := coord.ReceiveAuthResponse(AuthResponse{RequestID: "missing"}); err == nil {
		t.Fatalf("expected unknown request error")
	}

	coord.requests["r1"] = &AuthRequest{ID: "r1", PaneID: 7, Status: "pending"}
	if err := coord.ReceiveAuthResponse(AuthResponse{RequestID: "r1"}); err == nil || !strings.Contains(err.Error(), "no tracker") {
		t.Fatalf("expected no tracker error, got %v", err)
	}

	tracker := NewPaneTracker(7)
	tracker.SetRequestID("r2")
	coord.trackers[7] = tracker
	coord.requests["r2"] = &AuthRequest{ID: "r2", PaneID: 7, Status: "pending"}

	var callbackErr error
	coord.OnAuthFailed = func(_ int, err error) { callbackErr = err }
	if err := coord.ReceiveAuthResponse(AuthResponse{RequestID: "r2", Error: "invalid code"}); err != nil {
		t.Fatalf("expected handled error response, got %v", err)
	}
	if tracker.GetState() != StateFailed {
		t.Fatalf("expected failed state, got %s", tracker.GetState())
	}
	if callbackErr == nil || !strings.Contains(callbackErr.Error(), "invalid code") {
		t.Fatalf("expected callback error, got %v", callbackErr)
	}
	if len(coord.GetPendingRequests()) != 0 {
		t.Fatalf("expected request removed from pending")
	}
}

func TestAPIServerHandlers(t *testing.T) {
	client := newCoveragePaneClient([]Pane{{PaneID: 15, Title: "pane"}})
	client.setPaneOutput(15, "ok")
	coord := newCoverageCoordinator(client)
	coord.trackers[15] = NewPaneTracker(15)
	coord.trackers[15].SetState(StateAuthPending)
	coord.trackers[15].SetRequestID("r15")
	coord.requests["r15"] = &AuthRequest{
		ID:        "r15",
		PaneID:    15,
		URL:       "https://claude.ai/oauth/authorize?code=true",
		CreatedAt: time.Now(),
		Status:    "pending",
	}

	var logs bytes.Buffer
	api := NewAPIServer(coord, 0, slog.New(slog.NewTextHandler(&logs, nil)))

	t.Run("status endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		rr := httptest.NewRecorder()
		api.handleStatus(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		var resp StatusResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode status response: %v", err)
		}
		if resp.PaneCount != 1 || resp.PendingAuths != 1 {
			t.Fatalf("unexpected status payload %+v", resp)
		}
	})

	t.Run("pending endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/pending", nil)
		rr := httptest.NewRecorder()
		api.handleGetPending(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		var pending []*AuthRequest
		if err := json.Unmarshal(rr.Body.Bytes(), &pending); err != nil {
			t.Fatalf("decode pending response: %v", err)
		}
		if len(pending) != 1 || pending[0].ID != "r15" {
			t.Fatalf("unexpected pending response: %+v", pending)
		}
	})

	t.Run("complete bad body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/complete", strings.NewReader("{bad"))
		rr := httptest.NewRecorder()
		api.handleComplete(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("complete missing request_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/complete", strings.NewReader(`{"code":"x"}`))
		rr := httptest.NewRecorder()
		api.handleComplete(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("complete unknown request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/complete", strings.NewReader(`{"request_id":"missing","code":"x"}`))
		rr := httptest.NewRecorder()
		api.handleComplete(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("complete success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/complete", strings.NewReader(`{"request_id":"r15","code":"ABC","account":"acct"}`))
		rr := httptest.NewRecorder()
		api.handleComplete(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("list panes success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/panes", nil)
		rr := httptest.NewRecorder()
		api.handleListPanes(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("list panes error", func(t *testing.T) {
		client.listErr = errors.New("list failed")
		req := httptest.NewRequest(http.MethodGet, "/panes", nil)
		rr := httptest.NewRecorder()
		api.handleListPanes(rr, req)
		client.listErr = nil
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rr.Code)
		}
	})

	t.Run("withLogging middleware", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		api.withLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rr.Code)
		}
	})

	t.Run("start returns listen error", func(t *testing.T) {
		api.server.Addr = ":-1"
		if err := api.Start(); err == nil {
			t.Fatalf("expected start error with invalid port")
		}
	})

	t.Run("shutdown on fresh server", func(t *testing.T) {
		if err := api.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown should succeed on idle server, got %v", err)
		}
	})
}

func TestPaneTrackerAccessors(t *testing.T) {
	tracker := NewPaneTracker(5)
	tracker.SetOAuthURL("https://x")
	tracker.SetRequestID("r")
	tracker.SetReceivedCode("c")
	tracker.SetUsedAccount("a")
	tracker.SetErrorMessage("e")

	if tracker.GetOAuthURL() != "https://x" ||
		tracker.GetRequestID() != "r" ||
		tracker.GetReceivedCode() != "c" ||
		tracker.GetUsedAccount() != "a" ||
		tracker.GetErrorMessage() != "e" {
		t.Fatalf("accessors did not round-trip values")
	}
}

func TestSelectPaneClientAndBackends(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_ = selectPaneClient(BackendWezTerm, logger)
	_ = selectPaneClient(BackendTmux, logger)
	_ = selectPaneClient("unknown", logger)
}

func TestTmuxClientScripted(t *testing.T) {
	tmuxPath := writeExecutable(t, "tmux", `#!/bin/sh
cmd="$1"
shift
case "$cmd" in
  list-panes)
    echo "%1	s1	2	0	title	/tmp	1	100	40"
    ;;
  capture-pane)
    echo "captured output"
    ;;
  send-keys)
    exit 0
    ;;
  list-sessions)
    exit 0
    ;;
  *)
    echo "unknown command" >&2
    exit 1
    ;;
esac
`)

	client := &TmuxClient{binaryPath: tmuxPath}
	panes, err := client.ListPanes(context.Background())
	if err != nil {
		t.Fatalf("ListPanes failed: %v", err)
	}
	if len(panes) != 1 || panes[0].PaneID != 1 || panes[0].Domain != "s1:2.0" {
		t.Fatalf("unexpected panes: %+v", panes)
	}

	if _, err := client.parsePaneLine("%bad"); err == nil {
		t.Fatalf("expected parse error for malformed pane line")
	}
	if _, err := client.parsePaneLine("x\tbroken\tline\twith\tfew\tparts"); err == nil {
		t.Fatalf("expected parse error for short pane line")
	}

	txt, err := client.GetText(context.Background(), 1, -5)
	if err != nil || !strings.Contains(txt, "captured output") {
		t.Fatalf("GetText failed: txt=%q err=%v", txt, err)
	}

	if err := client.SendText(context.Background(), 1, "/login\n", true); err != nil {
		t.Fatalf("SendText noPaste failed: %v", err)
	}
	if err := client.SendText(context.Background(), 1, "1\n", false); err != nil {
		t.Fatalf("SendText paste failed: %v", err)
	}

	if !client.IsAvailable(context.Background()) {
		t.Fatalf("expected IsAvailable true")
	}
	if client.Backend() != "tmux" {
		t.Fatalf("expected tmux backend")
	}
}

func TestTmuxClientErrors(t *testing.T) {
	noServer := writeExecutable(t, "tmux-noserver", `#!/bin/sh
if [ "$1" = "list-panes" ]; then
  echo "no server running on /tmp/tmux-1000/default" >&2
  exit 1
fi
exit 1
`)
	client := &TmuxClient{binaryPath: noServer}
	if _, err := client.ListPanes(context.Background()); err == nil || !strings.Contains(err.Error(), "server not running") {
		t.Fatalf("expected server not running error, got %v", err)
	}

	sendErr := writeExecutable(t, "tmux-senderr", `#!/bin/sh
if [ "$1" = "send-keys" ]; then
  echo "send failed" >&2
  exit 1
fi
if [ "$1" = "capture-pane" ]; then
  echo "capture failed" >&2
  exit 1
fi
exit 0
`)
	client = &TmuxClient{binaryPath: sendErr}
	if _, err := client.GetText(context.Background(), 1, 0); err == nil {
		t.Fatalf("expected capture-pane error")
	}
	if err := client.SendText(context.Background(), 1, "x", false); err == nil {
		t.Fatalf("expected send-keys error")
	}
}

func TestWezTermClientScripted(t *testing.T) {
	weztermPath := writeExecutable(t, "wezterm", `#!/bin/sh
if [ "$1" != "cli" ]; then
  exit 1
fi
case "$2" in
  list)
    echo '[{"pane_id":9,"window_id":1,"tab_id":2,"domain":"ssh","title":"main","cwd":"/tmp","is_active":true,"cols":120,"size":40}]'
    ;;
  get-text)
    echo "pane text"
    ;;
  send-text)
    exit 0
    ;;
  activate-pane)
    exit 0
    ;;
  *)
    echo "unknown wezterm cli command" >&2
    exit 1
    ;;
esac
`)

	client := &WezTermClient{binaryPath: weztermPath}
	panes, err := client.ListPanes(context.Background())
	if err != nil {
		t.Fatalf("ListPanes failed: %v", err)
	}
	if len(panes) != 1 || panes[0].PaneID != 9 {
		t.Fatalf("unexpected panes: %+v", panes)
	}

	txt, err := client.GetText(context.Background(), 9, -3)
	if err != nil || !strings.Contains(txt, "pane text") {
		t.Fatalf("GetText failed: txt=%q err=%v", txt, err)
	}

	if err := client.SendText(context.Background(), 9, "x", true); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
	if err := client.SendKeys(context.Background(), 9, "Enter", "Tab", "Esc", "Space", "Backspace", "x"); err != nil {
		t.Fatalf("SendKeys failed: %v", err)
	}
	if err := client.ActivatePane(context.Background(), 9); err != nil {
		t.Fatalf("ActivatePane failed: %v", err)
	}
	if !client.IsAvailable(context.Background()) {
		t.Fatalf("expected IsAvailable true")
	}
	if client.Backend() != "wezterm" {
		t.Fatalf("expected wezterm backend")
	}

	if mapKey("Enter") != "\n" || mapKey("tab") != "\t" || mapKey("esc") != "\x1b" || mapKey("space") != " " || mapKey("backspace") != "\x7f" {
		t.Fatalf("mapKey common mappings failed")
	}
}

func TestWezTermClientErrors(t *testing.T) {
	wezErr := writeExecutable(t, "wezterm-err", `#!/bin/sh
if [ "$1" != "cli" ]; then
  exit 1
fi
case "$2" in
  list)
    echo "not-json"
    exit 0
    ;;
  get-text|send-text|activate-pane)
    echo "failed" >&2
    exit 1
    ;;
  *)
    exit 1
    ;;
esac
`)

	client := &WezTermClient{binaryPath: wezErr}
	if _, err := client.ListPanes(context.Background()); err == nil {
		t.Fatalf("expected parse error")
	}
	if _, err := client.GetText(context.Background(), 1, 0); err == nil {
		t.Fatalf("expected get-text error")
	}
	if err := client.SendText(context.Background(), 1, "x", false); err == nil {
		t.Fatalf("expected send-text error")
	}
	if err := client.ActivatePane(context.Background(), 1); err == nil {
		t.Fatalf("expected activate-pane error")
	}
	if !client.IsAvailable(context.Background()) {
		t.Fatalf("expected IsAvailable true when list command exits 0")
	}
}

func writeExecutable(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func assertSentContains(t *testing.T, sent []string, want string) {
	t.Helper()
	for _, v := range sent {
		if v == want {
			return
		}
	}
	t.Fatalf("expected sent text %q in %#v", want, sent)
}
