package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func waitForTestCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition was not met before timeout")
}

func TestAgentCoverageEdgeBranches(t *testing.T) {
	t.Run("fallback clients and default strategy", func(t *testing.T) {
		a := New(DefaultConfig())
		a.accountUsage = map[string]*AccountUsage{}
		a.pendingClient = nil
		a.completeClient = nil
		a.config.Accounts = []string{"fallback@example.com"}
		a.config.AccountStrategy = StrategyRandom

		if got := a.pendingHTTPClient(); got.Timeout != defaultPendingRequestTimeout {
			t.Fatalf("pending timeout=%v", got.Timeout)
		}
		if got := a.authCompleteHTTPClient(); got.Timeout != defaultAuthCompleteTimeout {
			t.Fatalf("complete timeout=%v", got.Timeout)
		}
		if got := a.selectAccount(); got != "fallback@example.com" {
			t.Fatalf("selectAccount=%q", got)
		}
	})

	t.Run("pollLoop exits on cancellation and closed stop channel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		a := New(DefaultConfig())
		a.config.PollInterval = 100 * time.Millisecond
		a.doneCh = make(chan struct{})
		a.stopCh = make(chan struct{})

		go a.pollLoop(ctx)
		cancel()

		select {
		case <-a.doneCh:
		case <-time.After(time.Second):
			t.Fatal("pollLoop did not stop on context cancellation")
		}

		a = New(DefaultConfig())
		a.config.PollInterval = 100 * time.Millisecond
		a.doneCh = make(chan struct{})
		a.stopCh = make(chan struct{})

		go a.pollLoop(context.Background())
		close(a.stopCh)

		select {
		case <-a.doneCh:
		case <-time.After(time.Second):
			t.Fatal("pollLoop did not stop on closed stop channel")
		}
	})

	t.Run("processAuthRequest invokes callbacks for success and failure", func(t *testing.T) {
		oauthServer := newOAuthFixtureServer(t)
		defer oauthServer.Close()
		browserFixture := newOAuthFixtureBrowser()

		type completeBody struct {
			RequestID string `json:"request_id"`
			Code      string `json:"code"`
			Account   string `json:"account"`
			Error     string `json:"error"`
		}
		var (
			mu        sync.Mutex
			completes []completeBody
			started   []string
			successes []string
			failures  []string
		)

		coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/auth/complete" {
				http.NotFound(w, r)
				return
			}

			var body completeBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			completes = append(completes, body)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		defer coordinator.Close()

		cfg := DefaultConfig()
		cfg.CoordinatorURL = coordinator.URL
		cfg.Accounts = []string{"chosen@example.com"}
		a := New(cfg)
		a.browser = browserFixture
		a.usagePath = filepath.Join(t.TempDir(), "agent-edge-usage.json")
		a.OnAuthStart = func(url, account string) {
			mu.Lock()
			defer mu.Unlock()
			started = append(started, account+"|"+url)
		}
		a.OnAuthComplete = func(account, code string) {
			mu.Lock()
			defer mu.Unlock()
			successes = append(successes, account+"|"+code)
		}
		a.OnAuthFailed = func(account string, err error) {
			mu.Lock()
			defer mu.Unlock()
			failures = append(failures, account+"|"+err.Error())
		}

		a.processAuthRequest(context.Background(), "ok-1", oauthServer.URL+"/oauth/success?code=edge-code&account=edge@example.com")
		a.processAuthRequest(context.Background(), "bad-1", oauthServer.URL+"/oauth/failure")

		waitForTestCondition(t, time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(completes) == 2 && len(started) == 2 && len(successes) == 1 && len(failures) == 1
		})
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("pending decode, request build, and sendAuthComplete error paths", func(t *testing.T) {
		coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/auth/pending":
				_, _ = w.Write([]byte(`{`))
			case "/auth/complete":
				w.WriteHeader(http.StatusInternalServerError)
			default:
				http.NotFound(w, r)
			}
		}))
		defer coordinator.Close()

		a := New(DefaultConfig())
		a.config.CoordinatorURL = coordinator.URL
		a.checkPendingRequests(context.Background())
		a.sendAuthComplete(context.Background(), "req-1", "code", "acct@example.com", "")

		a.config.CoordinatorURL = "://bad-url"
		a.checkPendingRequests(context.Background())
		a.sendAuthComplete(context.Background(), "req-2", "code", "acct@example.com", "err")
	})

	t.Run("usage persistence helpers tolerate edge inputs", func(t *testing.T) {
		a := New(DefaultConfig())
		a.accountUsage = map[string]*AccountUsage{}
		tmpDir := t.TempDir()

		a.recordUsage("", "ignored")
		if len(a.accountUsage) != 0 {
			t.Fatalf("recordUsage with empty email mutated accountUsage: %+v", a.accountUsage)
		}

		a.usagePath = filepath.Join(tmpDir, "invalid.json")
		if err := os.WriteFile(a.usagePath, []byte("{"), 0o600); err != nil {
			t.Fatalf("write invalid usage file: %v", err)
		}
		a.loadUsage()

		a.accountUsage = map[string]*AccountUsage{
			"real@example.com": {
				Email:      "real@example.com",
				LastUsed:   time.Now(),
				UseCount:   2,
				LastResult: "success",
			},
			"nil@example.com": nil,
		}

		a.usagePath = filepath.Join(tmpDir, "usage-dir")
		if err := os.Mkdir(a.usagePath, 0o700); err != nil {
			t.Fatalf("mkdir rename target: %v", err)
		}
		a.saveUsage()

		blocker := filepath.Join(tmpDir, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatalf("write blocker file: %v", err)
		}
		a.usagePath = filepath.Join(blocker, "usage.json")
		a.saveUsage()
	})

	t.Run("writeJSON and handleAccounts cover unsupported payloads and nil usages", func(t *testing.T) {
		a := New(DefaultConfig())
		a.accountUsage = map[string]*AccountUsage{
			"good@example.com": {
				Email:      "good@example.com",
				LastUsed:   time.Now(),
				UseCount:   1,
				LastResult: "success",
			},
			"nil@example.com": nil,
		}

		rr := httptest.NewRecorder()
		a.handleAccounts(rr, httptest.NewRequest(http.MethodGet, "/accounts", nil))
		var accounts []AccountUsage
		if err := json.NewDecoder(rr.Body).Decode(&accounts); err != nil {
			t.Fatalf("decode accounts: %v", err)
		}
		if len(accounts) != 1 || accounts[0].Email != "good@example.com" {
			t.Fatalf("unexpected accounts payload: %+v", accounts)
		}

		rr = httptest.NewRecorder()
		a.writeJSON(rr, map[string]any{"bad": make(chan int)})
		if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type=%q", contentType)
		}
	})

	t.Run("Stop tolerates already-closed stop channel", func(t *testing.T) {
		a := New(DefaultConfig())
		a.running = true
		a.stopCh = make(chan struct{})
		a.doneCh = make(chan struct{})
		a.usagePath = filepath.Join(t.TempDir(), "stop-usage.json")
		close(a.stopCh)
		close(a.doneCh)

		if err := a.Stop(context.Background()); err != nil {
			t.Fatalf("Stop failed with closed stopCh: %v", err)
		}
	})
}

func TestMultiCoverageEdgeBranches(t *testing.T) {
	t.Run("default strategy and no-op usage paths", func(t *testing.T) {
		a := NewMulti(DefaultMultiConfig())
		a.accountUsage = map[string]*AccountUsage{}
		a.config.Accounts = []string{"fallback@example.com"}
		a.config.AccountStrategy = StrategyRandom
		if got := a.selectAccount(); got != "fallback@example.com" {
			t.Fatalf("selectAccount=%q", got)
		}

		a.recordUsage("", "ignored")
		if len(a.accountUsage) != 0 {
			t.Fatalf("recordUsage with empty email mutated accountUsage: %+v", a.accountUsage)
		}
	})

	t.Run("pollLoop exits on cancellation and closed stop channel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		a := NewMulti(DefaultMultiConfig())
		a.config.PollInterval = 100 * time.Millisecond
		a.doneCh = make(chan struct{})
		a.stopCh = make(chan struct{})

		go a.pollLoop(ctx)
		cancel()

		select {
		case <-a.doneCh:
		case <-time.After(time.Second):
			t.Fatal("pollLoop did not stop on context cancellation")
		}

		a = NewMulti(DefaultMultiConfig())
		a.config.PollInterval = 100 * time.Millisecond
		a.doneCh = make(chan struct{})
		a.stopCh = make(chan struct{})

		go a.pollLoop(context.Background())
		close(a.stopCh)

		select {
		case <-a.doneCh:
		case <-time.After(time.Second):
			t.Fatal("pollLoop did not stop on closed stop channel")
		}
	})

	t.Run("checkCoordinator handles bad URLs, bad JSON, dedupe, and cleanup", func(t *testing.T) {
		oauthServer := newOAuthFixtureServer(t)
		defer oauthServer.Close()
		browserFixture := newOAuthFixtureBrowser()

		var (
			mu         sync.Mutex
			completes  int
			completeID []string
		)

		dedupeCoordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/auth/pending":
				_ = json.NewEncoder(w).Encode([]map[string]any{
					{"id": "dup-1", "pane_id": 1, "url": oauthServer.URL + "/oauth/success?code=dup-code&account=multi@example.com"},
					{"id": "dup-1", "pane_id": 2, "url": oauthServer.URL + "/oauth/success?code=dup-code&account=multi@example.com"},
				})
			case "/auth/complete":
				var body struct {
					RequestID string `json:"request_id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				completes++
				completeID = append(completeID, body.RequestID)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer dedupeCoordinator.Close()

		badJSONCoordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/auth/pending" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{`))
		}))
		defer badJSONCoordinator.Close()

		cfg := DefaultMultiConfig()
		coord := &CoordinatorEndpoint{Name: "dedupe", URL: dedupeCoordinator.URL}
		cfg.Coordinators = []*CoordinatorEndpoint{coord}
		cfg.Accounts = []string{"multi@example.com"}
		a := NewMulti(cfg)
		a.browser = browserFixture
		a.usagePath = filepath.Join(t.TempDir(), "multi-edge-usage.json")

		a.checkCoordinator(context.Background(), &CoordinatorEndpoint{Name: "invalid", URL: "://bad-url"})
		a.checkCoordinator(context.Background(), &CoordinatorEndpoint{Name: "badjson", URL: badJSONCoordinator.URL})
		a.checkCoordinator(context.Background(), coord)

		waitForTestCondition(t, 2*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			a.procMu.Lock()
			defer a.procMu.Unlock()
			return completes == 1 && len(a.processing) == 0
		})

		mu.Lock()
		defer mu.Unlock()
		if len(completeID) != 1 || completeID[0] != "dup-1" {
			t.Fatalf("unexpected completion records: %+v", completeID)
		}
	})

	t.Run("processAuthRequest callbacks and sendAuthComplete edge cases", func(t *testing.T) {
		oauthServer := newOAuthFixtureServer(t)
		defer oauthServer.Close()
		browserFixture := newOAuthFixtureBrowser()

		var (
			mu        sync.Mutex
			started   []string
			successes []string
			failures  []string
		)

		coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/auth/complete" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer coordinator.Close()

		coord := &CoordinatorEndpoint{Name: "coord-1", URL: coordinator.URL}
		cfg := DefaultMultiConfig()
		cfg.Coordinators = []*CoordinatorEndpoint{coord}
		cfg.Accounts = []string{"picked@example.com"}
		a := NewMulti(cfg)
		a.browser = browserFixture
		a.usagePath = filepath.Join(t.TempDir(), "multi-callbacks-usage.json")
		a.OnAuthStart = func(coordinator, url, account string) {
			mu.Lock()
			defer mu.Unlock()
			started = append(started, coordinator+"|"+account+"|"+url)
		}
		a.OnAuthComplete = func(coordinator, account, code string) {
			mu.Lock()
			defer mu.Unlock()
			successes = append(successes, coordinator+"|"+account+"|"+code)
		}
		a.OnAuthFailed = func(coordinator, account string, err error) {
			mu.Lock()
			defer mu.Unlock()
			failures = append(failures, coordinator+"|"+account+"|"+err.Error())
		}

		a.processAuthRequest(context.Background(), coord, "ok-1", oauthServer.URL+"/oauth/success?code=multi-edge&account=multi@example.com")
		a.processAuthRequest(context.Background(), coord, "bad-1", oauthServer.URL+"/oauth/failure")
		a.sendAuthComplete(context.Background(), &CoordinatorEndpoint{Name: "invalid", URL: "://bad-url"}, "req-3", "code", "acct@example.com", "err")

		waitForTestCondition(t, time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(started) == 2 && len(successes) == 1 && len(failures) == 1
		})
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("loadUsage, writeJSON, handleStatus, handleAuth, and Stop edge cases", func(t *testing.T) {
		oauthServer := newOAuthFixtureServer(t)
		defer oauthServer.Close()
		browserFixture := newOAuthFixtureBrowser()

		cfg := DefaultMultiConfig()
		healthy := &CoordinatorEndpoint{Name: "healthy", URL: "http://healthy"}
		healthy.SetHealth(true, "")
		unhealthy := &CoordinatorEndpoint{Name: "unhealthy", URL: "http://unhealthy"}
		unhealthy.SetHealth(false, "boom")
		cfg.Coordinators = []*CoordinatorEndpoint{healthy, unhealthy}
		cfg.Accounts = []string{"multi@example.com"}
		a := NewMulti(cfg)
		a.browser = browserFixture
		a.usagePath = filepath.Join(t.TempDir(), "multi-invalid-usage.json")
		if err := os.WriteFile(a.usagePath, []byte("{"), 0o600); err != nil {
			t.Fatalf("write invalid usage file: %v", err)
		}
		a.loadUsage()

		rr := httptest.NewRecorder()
		a.handleStatus(rr, httptest.NewRequest(http.MethodGet, "/status", nil))
		var status map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&status); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if status["healthy_coordinators"].(float64) != 1 {
			t.Fatalf("healthy_coordinators=%v", status["healthy_coordinators"])
		}

		rr = httptest.NewRecorder()
		a.writeJSON(rr, map[string]any{"bad": make(chan int)})
		if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type=%q", contentType)
		}

		rr = httptest.NewRecorder()
		a.handleAuth(rr, httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{"url":""}`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected bad request for empty url, got %d", rr.Code)
		}

		rr = httptest.NewRecorder()
		a.handleAuth(rr, httptest.NewRequest(http.MethodPost, "/auth", bytes.NewBufferString(`{"url":"`+oauthServer.URL+`/oauth/failure","account":"manual@example.com"}`)))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected JSON error response, got %d", rr.Code)
		}
		waitForTestCondition(t, time.Second, func() bool {
			data, err := os.ReadFile(a.usagePath)
			if err != nil {
				return false
			}
			var usage []*AccountUsage
			return json.Unmarshal(data, &usage) == nil
		})

		a.running = true
		a.stopCh = make(chan struct{})
		a.doneCh = make(chan struct{})
		close(a.stopCh)
		close(a.doneCh)
		if err := a.Stop(context.Background()); err != nil {
			t.Fatalf("Stop failed with closed stopCh: %v", err)
		}
	})
}
