package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type multiCompletePayload struct {
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Account   string `json:"account"`
	Error     string `json:"error"`
}

func TestMultiCoverageStartStopAndPollLoop(t *testing.T) {
	oauthServer := newOAuthFixtureServer(t)
	defer oauthServer.Close()
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/pending":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case "/auth/complete":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer coordinator.Close()

	cfg := DefaultMultiConfig()
	cfg.Port = 0
	cfg.PollInterval = 10 * time.Millisecond
	cfg.Coordinators = []*CoordinatorEndpoint{{Name: "c1", URL: coordinator.URL}}
	a := NewMulti(cfg)
	a.usagePath = filepath.Join(t.TempDir(), "multi_cov_usage_poll.json")

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(35 * time.Millisecond)
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop should be no-op: %v", err)
	}
}

func TestMultiCoverageProcessAuthAndHandlers(t *testing.T) {
	oauthServer := newOAuthFixtureServer(t)
	defer oauthServer.Close()
	browserFixture := newOAuthFixtureBrowser()
	withFixtureBrowser(t, browserFixture)

	var (
		mu        sync.Mutex
		completes []multiCompletePayload
	)

	coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/complete":
			var body multiCompletePayload
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			completes = append(completes, body)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer coordinator.Close()

	coord := &CoordinatorEndpoint{Name: "c1", URL: coordinator.URL, DisplayName: "Coord 1"}
	cfg := DefaultMultiConfig()
	cfg.Coordinators = []*CoordinatorEndpoint{coord}
	cfg.Accounts = []string{"multi@example.com"}
	a := NewMulti(cfg)
	a.browser = browserFixture
	a.usagePath = filepath.Join(t.TempDir(), "multi_cov_usage_handlers.json")

	a.processAuthRequest(context.Background(), coord, "req-1", oauthServer.URL+"/oauth/success?code=multi-code&account=multi@example.com")
	mu.Lock()
	if len(completes) != 1 || completes[0].RequestID != "req-1" || completes[0].Code != "multi-code" {
		mu.Unlock()
		t.Fatalf("unexpected successful completion payload: %+v", completes)
	}
	mu.Unlock()

	a.processAuthRequest(context.Background(), coord, "req-2", oauthServer.URL+"/oauth/failure")
	mu.Lock()
	if len(completes) != 2 || completes[1].RequestID != "req-2" || completes[1].Error == "" {
		mu.Unlock()
		t.Fatalf("expected error payload for failed completion: %+v", completes)
	}
	mu.Unlock()

	// withLogging + status/accounts/auth handlers
	wrappedCalled := false
	wrapped := a.withLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrappedCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/noop", nil)
	wrapped.ServeHTTP(rr, req)
	if !wrappedCalled || rr.Code != http.StatusNoContent {
		t.Fatalf("withLogging wrapper did not pass through")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	a.handleAccounts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("handleAccounts status=%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{`))
	a.handleAuth(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for malformed auth JSON")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{"url":"`+oauthServer.URL+`/oauth/success?code=manual-multi&account=multi@example.com"}`))
	a.handleAuth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("handleAuth status=%d", rr.Code)
	}
}
