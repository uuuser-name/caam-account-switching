package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type oauthFixtureBrowser struct {
	client     *http.Client
	closeCalls int
	mu         sync.Mutex
}

func newOAuthFixtureBrowser() *oauthFixtureBrowser {
	return &oauthFixtureBrowser{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (b *oauthFixtureBrowser) CompleteOAuth(ctx context.Context, oauthURL, preferredAccount string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fixture request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("fixture oauth status %d", resp.StatusCode)
	}

	finalURL := resp.Request.URL
	code := finalURL.Query().Get("code")
	account := finalURL.Query().Get("account")
	if account == "" {
		account = preferredAccount
	}
	if code == "" {
		return "", "", fmt.Errorf("fixture oauth response missing code")
	}

	return code, account, nil
}

func (b *oauthFixtureBrowser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeCalls++
}

func withFixtureBrowser(t *testing.T, b *oauthFixtureBrowser) {
	t.Helper()
	old := newOAuthBrowser
	newOAuthBrowser = func(BrowserConfig) oauthBrowser { return b }
	t.Cleanup(func() { newOAuthBrowser = old })
}

func newOAuthFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/success":
			code := r.URL.Query().Get("code")
			if code == "" {
				code = "fixture-code"
			}
			account := r.URL.Query().Get("account")
			if account == "" {
				account = "fixture@example.com"
			}
			redirectURL := fmt.Sprintf("/callback?code=%s&account=%s", code, account)
			http.Redirect(w, r, redirectURL, http.StatusFound)
		case "/oauth/failure":
			http.Error(w, "fixture oauth failure", http.StatusBadGateway)
		case "/callback":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAgentCoverageStartStopAndStatus(t *testing.T) {
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.CoordinatorURL = ""
	cfg.PollInterval = 10 * time.Millisecond
	a := New(cfg)
	a.usagePath = filepath.Join(t.TempDir(), "usage.json")

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatalf("expected second Start to fail while running")
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	a.handleStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status code=%d", rr.Code)
	}

	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop should be no-op: %v", err)
	}
	if browserFixture.closeCalls == 0 {
		t.Fatalf("expected browser Close to be called")
	}
}

func TestAgentCoveragePendingAndAuthCompleteFlow(t *testing.T) {
	oauthServer := newOAuthFixtureServer(t)
	defer oauthServer.Close()
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	type completeBody struct {
		RequestID string `json:"request_id"`
		Code      string `json:"code"`
		Account   string `json:"account"`
		Error     string `json:"error"`
	}
	var completes []completeBody

	coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/pending":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "req-1", "pane_id": 1, "url": oauthServer.URL + "/oauth/success?code=auth-code&account=acc1@example.com"},
			})
		case "/auth/complete":
			var body completeBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			completes = append(completes, body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer coordinator.Close()

	cfg := DefaultConfig()
	cfg.CoordinatorURL = coordinator.URL
	cfg.Accounts = []string{"acc1@example.com"}
	a := New(cfg)
	a.usagePath = filepath.Join(t.TempDir(), "agent_cov_usage_pending_flow.json")
	a.browser = browserFixture

	a.checkPendingRequests(context.Background())
	if len(completes) != 1 {
		t.Fatalf("expected 1 completion callback, got %d", len(completes))
	}
	if completes[0].RequestID != "req-1" || completes[0].Code != "auth-code" {
		t.Fatalf("unexpected completion payload: %+v", completes[0])
	}

	a.processAuthRequest(context.Background(), "req-2", oauthServer.URL+"/oauth/failure")
	if len(completes) != 2 {
		t.Fatalf("expected failure completion callback")
	}
	if completes[1].RequestID != "req-2" || completes[1].Error == "" {
		t.Fatalf("expected error payload for failed oauth: %+v", completes[1])
	}

	// processAuthRequest records usage asynchronously; wait for file writes before TempDir cleanup.
	time.Sleep(100 * time.Millisecond)
}

func TestAgentCoverageAuthAndAccountsHandlers(t *testing.T) {
	oauthServer := newOAuthFixtureServer(t)
	defer oauthServer.Close()
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	cfg := DefaultConfig()
	cfg.Accounts = []string{"manual@example.com"}
	a := New(cfg)
	a.usagePath = filepath.Join(t.TempDir(), "agent_cov_usage_handlers.json")
	a.browser = browserFixture

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{`))
	a.handleAuth(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for malformed json, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{"url":""}`))
	a.handleAuth(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for empty url, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{"url":"`+oauthServer.URL+`/oauth/success?code=manual-code&account=manual@example.com"}`))
	a.handleAuth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected auth success status, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{"url":"`+oauthServer.URL+`/oauth/failure"}`))
	a.handleAuth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected json error response status, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	a.handleAccounts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected accounts status 200, got %d", rr.Code)
	}

	// handleAuth records usage asynchronously; wait for file writes before TempDir cleanup.
	time.Sleep(100 * time.Millisecond)
}

func TestAgentCoverageUsagePersistence(t *testing.T) {
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	cfg := DefaultConfig()
	a := New(cfg)
	tmpDir := t.TempDir()
	a.usagePath = filepath.Join(tmpDir, "usage.json")

	initial := []*AccountUsage{{Email: "a@x", LastUsed: time.Now(), UseCount: 1, LastResult: "success"}}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("marshal initial usage: %v", err)
	}
	if err := os.WriteFile(a.usagePath, data, 0o600); err != nil {
		t.Fatalf("write initial usage file: %v", err)
	}

	a.accountUsage = map[string]*AccountUsage{}
	a.loadUsage()
	if _, ok := a.accountUsage["a@x"]; !ok {
		t.Fatalf("expected loaded usage for a@x")
	}

	a.recordUsage("b@x", "success")
	// Wait for async saveUsage to complete to avoid race condition
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(a.usagePath); err != nil {
		t.Fatalf("expected usage file to exist: %v", err)
	}
}

func TestAgentCoverageJsonReader(t *testing.T) {
	reader := jsonReader([]byte(`{"k":"v"}`))
	buf := make([]byte, 64)
	if _, err := reader.Read(buf); err != nil && err != io.EOF {
		t.Fatalf("unexpected reader error: %v", err)
	}
	if _, err := reader.Read(buf); err != io.EOF {
		t.Fatalf("expected EOF on second read, got %v", err)
	}
}

func TestAgentCoveragePollLoopAndWithLogging(t *testing.T) {
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	cfg := DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.CoordinatorURL = "http://127.0.0.1:1" // unreachable, but exercises poll path
	a := New(cfg)
	a.browser = browserFixture

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.stopCh = make(chan struct{})
	a.doneCh = make(chan struct{})
	go a.pollLoop(ctx)
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-a.doneCh:
	case <-time.After(1 * time.Second):
		t.Fatalf("pollLoop did not stop after context cancellation")
	}

	called := false
	h := a.withLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)
	if !called {
		t.Fatalf("expected wrapped handler to be called")
	}
}
