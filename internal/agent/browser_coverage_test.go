package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func cleanupBrowserFixture(t *testing.T, browser *Browser, userDataDir string) {
	t.Helper()

	t.Cleanup(func() {
		browser.Close()
		if userDataDir == "" {
			return
		}

		deadline := time.Now().Add(3 * time.Second)
		for {
			if err := os.RemoveAll(userDataDir); err == nil || os.IsNotExist(err) {
				return
			} else if time.Now().After(deadline) {
				t.Fatalf("cleanup browser user data dir %q: %v", userDataDir, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

func TestBrowserCoverageCompleteOAuthWithLocalFixture(t *testing.T) {
	if findChrome() == "" {
		t.Skip("chrome not available on this host")
	}

	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><main><div class="challenge">ABCD-EFGH</div></main></body></html>`))
	}))
	defer fixture.Close()

	userDataDir := t.TempDir()
	browser := NewBrowser(BrowserConfig{
		UserDataDir: userDataDir,
		Headless:    true,
	})
	cleanupBrowserFixture(t, browser, userDataDir)

	code, account, err := browser.CompleteOAuth(context.Background(), fixture.URL, "")
	if err != nil {
		t.Fatalf("CompleteOAuth failed against local fixture: %v", err)
	}
	if code != "ABCD-EFGH" {
		t.Fatalf("code=%q, want ABCD-EFGH", code)
	}
	if account != "" {
		t.Fatalf("account=%q, want empty", account)
	}
}

func TestBrowserCoverageCompleteOAuthCancelledContext(t *testing.T) {
	if findChrome() == "" {
		t.Skip("chrome not available on this host")
	}

	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>ABCD-EFGH</body></html>`))
	}))
	defer fixture.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	userDataDir := t.TempDir()
	browser := NewBrowser(BrowserConfig{
		UserDataDir: userDataDir,
		Headless:    true,
	})
	cleanupBrowserFixture(t, browser, userDataDir)

	_, _, err := browser.CompleteOAuth(ctx, fixture.URL, "")
	if err == nil {
		t.Fatal("expected canceled context to fail")
	}
	if !strings.Contains(err.Error(), "navigate:") {
		t.Fatalf("expected navigate error, got %v", err)
	}
}

func TestBrowserCoverageCompleteOAuthPreferredAccountFixture(t *testing.T) {
	if findChrome() == "" {
		t.Skip("chrome not available on this host")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/accounts.google.com/select", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div data-email="preferred@example.com" data-identifier="preferred@example.com" style="display:block;cursor:pointer" onclick="window.location='/done?step=selected'">preferred@example.com</div></body></html>`))
	})
	mux.HandleFunc("/done", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div>ACCT-1234</div></body></html>`))
	})

	fixture := httptest.NewServer(mux)
	defer fixture.Close()

	userDataDir := t.TempDir()
	browser := NewBrowser(BrowserConfig{
		UserDataDir: userDataDir,
		Headless:    true,
	})
	cleanupBrowserFixture(t, browser, userDataDir)

	code, account, err := browser.CompleteOAuth(context.Background(), fixture.URL+"/accounts.google.com/select", "preferred@example.com")
	if err != nil {
		t.Fatalf("CompleteOAuth failed on preferred-account fixture: %v", err)
	}
	if code != "ACCT-1234" {
		t.Fatalf("code=%q, want ACCT-1234", code)
	}
	if account != "preferred@example.com" {
		t.Fatalf("account=%q, want preferred@example.com", account)
	}
}

func TestBrowserCoverageCompleteOAuthConsentFixture(t *testing.T) {
	if findChrome() == "" {
		t.Skip("chrome not available on this host")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/consent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div>consent step</div><button type="submit" onclick="window.location='/done?code=CONS-1234'">Allow</button></body></html>`))
	})
	mux.HandleFunc("/done", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div>CONS-1234</div></body></html>`))
	})

	fixture := httptest.NewServer(mux)
	defer fixture.Close()

	userDataDir := t.TempDir()
	browser := NewBrowser(BrowserConfig{
		UserDataDir: userDataDir,
		Headless:    true,
	})
	cleanupBrowserFixture(t, browser, userDataDir)

	code, account, err := browser.CompleteOAuth(context.Background(), fixture.URL+"/consent", "")
	if err != nil {
		t.Fatalf("CompleteOAuth failed on consent fixture: %v", err)
	}
	if code != "CONS-1234" {
		t.Fatalf("code=%q, want CONS-1234", code)
	}
	if account != "" {
		t.Fatalf("account=%q, want empty", account)
	}
}

func TestBrowserCoverageHelpers(t *testing.T) {
	browser := NewBrowser(BrowserConfig{})
	if browser.logger == nil {
		t.Fatal("expected default logger to be initialized")
	}

	canceled := false
	browser.cancelFunc = func() {
		canceled = true
	}
	browser.Close()
	if !canceled {
		t.Fatal("expected Close to invoke cancelFunc")
	}

	if code := extractChallengeCode(`<div>WXYZ-9876</div>`); code != "WXYZ-9876" {
		t.Fatalf("extractChallengeCode dash pattern=%q", code)
	}
	if code := extractChallengeCode(`<span>LONGCODE1234</span>`); code != "LONGCODE1234" {
		t.Fatalf("extractChallengeCode long pattern=%q", code)
	}
	if code := extractChallengeCode(`<html><body>nothing useful here</body></html>`); code != "" {
		t.Fatalf("extractChallengeCode no-match=%q", code)
	}

	if got := truncateURL("short", 10); got != "short" {
		t.Fatalf("truncateURL short=%q", got)
	}
	if got := truncateURL("https://example.com/really/long/path", 12); got != "https://e..." {
		t.Fatalf("truncateURL long=%q", got)
	}

	if chromePath := findChrome(); chromePath != "" {
		if _, err := os.Stat(chromePath); err != nil {
			t.Fatalf("findChrome returned missing path %q: %v", chromePath, err)
		}
	}
}
