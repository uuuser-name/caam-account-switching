package exec

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authpool"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/handoff"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/notify"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/pty"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/ratelimit"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
	"golang.org/x/term"
)

// =============================================================================
// HandoffState Tests
// =============================================================================

func TestHandoffState_String(t *testing.T) {
	tests := []struct {
		state    HandoffState
		expected string
	}{
		{Running, "RUNNING"},
		{RateLimited, "RATE_LIMITED"},
		{SelectingBackup, "SELECTING_BACKUP"},
		{SwappingAuth, "SWAPPING_AUTH"},
		{LoggingIn, "LOGGING_IN"},
		{LoginComplete, "LOGIN_COMPLETE"},
		{HandoffFailed, "HANDOFF_FAILED"},
		{ManualMode, "MANUAL_MODE"},
		{HandoffState(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("HandoffState.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// SmartRunner Tests
// =============================================================================

func TestNewSmartRunner(t *testing.T) {
	t.Run("creates runner with defaults", func(t *testing.T) {
		registry := provider.NewRegistry()
		runner := NewRunner(registry)

		sr := NewSmartRunner(runner, SmartRunnerOptions{})

		if sr == nil {
			t.Fatal("NewSmartRunner returned nil")
		}
		if sr.Runner != runner {
			t.Error("Runner not set correctly")
		}
		if sr.state != Running {
			t.Errorf("initial state = %v, want %v", sr.state, Running)
		}
		if sr.notifier == nil {
			t.Error("notifier should have default value")
		}
	})

	t.Run("default notifier is safe to use", func(t *testing.T) {
		registry := provider.NewRegistry()
		runner := NewRunner(registry)
		sr := NewSmartRunner(runner, SmartRunnerOptions{})

		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("default notifier should not panic, got: %v", rec)
			}
		}()
		sr.notifyHandoff("profile-a", "profile-b")
	})

	t.Run("creates runner with custom options", func(t *testing.T) {
		registry := provider.NewRegistry()
		runner := NewRunner(registry)
		vault := authfile.NewVault(t.TempDir())
		pool := authpool.NewAuthPool()
		notifier := &notify.TerminalNotifier{}
		handoffCfg := &config.HandoffConfig{
			AutoTrigger:      true,
			MaxRetries:       3,
			FallbackToManual: true,
		}

		sr := NewSmartRunner(runner, SmartRunnerOptions{
			Vault:            vault,
			AuthPool:         pool,
			Notifier:         notifier,
			HandoffConfig:    handoffCfg,
			CooldownDuration: 30 * time.Minute,
		})

		if sr.vault != vault {
			t.Error("vault not set correctly")
		}
		if sr.authPool != pool {
			t.Error("authPool not set correctly")
		}
		if sr.notifier != notifier {
			t.Error("notifier not set correctly")
		}
		if sr.handoffConfig != handoffCfg {
			t.Error("handoffConfig not set correctly")
		}
		if sr.cooldownDuration != 30*time.Minute {
			t.Errorf("cooldownDuration = %v, want 30m", sr.cooldownDuration)
		}
	})
}

func TestSmartRunner_setState(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	states := []HandoffState{
		Running,
		RateLimited,
		SelectingBackup,
		SwappingAuth,
		LoggingIn,
		LoginComplete,
		HandoffFailed,
		ManualMode,
	}

	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			sr.setState(state)

			sr.mu.Lock()
			got := sr.state
			sr.mu.Unlock()

			if got != state {
				t.Errorf("setState() = %v, want %v", got, state)
			}
		})
	}
}

func TestSmartRunner_InitialState(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	if sr.handoffCount != 0 {
		t.Errorf("initial handoffCount = %d, want 0", sr.handoffCount)
	}
	if sr.currentProfile != "" {
		t.Errorf("initial currentProfile = %q, want empty", sr.currentProfile)
	}
	if sr.previousProfile != "" {
		t.Errorf("initial previousProfile = %q, want empty", sr.previousProfile)
	}
}

func TestSmartRunner_DrainLoginDone(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	sr.loginDone <- loginResult{success: true}
	sr.drainLoginDone()

	select {
	case <-sr.loginDone:
		t.Fatal("expected loginDone to be empty after drain")
	default:
	}

	// Ensure drain is safe on empty channel
	sr.drainLoginDone()
}

func TestSmartRunner_MonitorOutputCapturesResumeHintOutsideRunningState(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	detector, err := ratelimit.NewDetector(ratelimit.ProviderFromString("codex"), nil)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}
	sr.detector = detector
	sr.providerID = "codex"
	sr.state = RateLimited

	capture := &codexSessionCapture{provider: "codex"}
	ctrl := &scriptedController{
		outputs: []string{"To continue this session, run codex resume session-123\n"},
	}

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sr.monitorOutput(ctx, ctrl, done, func(line string) {
		capture.ObserveLine(line)
		sr.updateResumeHint(capture.Command(), capture.ID())
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitorOutput did not finish")
	}

	if got := sr.resumeHint(""); got != "codex resume session-123" {
		t.Fatalf("resumeHint() = %q, want %q", got, "codex resume session-123")
	}
}

func TestSplitEnv(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "standard", input: "HOME=/tmp/test", want: []string{"HOME", "/tmp/test"}},
		{name: "value contains equals", input: "TOKEN=a=b=c", want: []string{"TOKEN", "a=b=c"}},
		{name: "missing equals", input: "JUSTKEY", want: []string{"JUSTKEY"}},
		{name: "empty key", input: "=value", want: []string{"", "value"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitEnv(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("splitEnv(%q) len=%d, want %d (%v)", tc.input, len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("splitEnv(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSmartRunner_PauseRateLimitDispatch(t *testing.T) {
	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{})

	sr.pauseRateLimitDispatch(0)
	if sr.rateLimitDispatchPaused() {
		t.Fatal("expected zero-duration pause to be ignored")
	}

	sr.pauseRateLimitDispatch(-time.Second)
	if sr.rateLimitDispatchPaused() {
		t.Fatal("expected negative-duration pause to be ignored")
	}

	sr.pauseRateLimitDispatch(50 * time.Millisecond)
	if !sr.rateLimitDispatchPaused() {
		t.Fatal("expected positive-duration pause to be active")
	}
}

// =============================================================================
// Mock Notifier for Testing
// =============================================================================

type mockNotifier struct {
	alerts []*notify.Alert
	calls  int
	err    error
}

func (m *mockNotifier) Notify(alert *notify.Alert) error {
	m.calls++
	m.alerts = append(m.alerts, alert)
	return m.err
}

func (m *mockNotifier) Name() string {
	return "mock"
}

func (m *mockNotifier) Available() bool {
	return true
}

type scriptedController struct {
	outputs          []string
	index            int
	injectedCommands []string
	injectedRaw      [][]byte
	injectCommandCh  chan struct{}
	injectRawNotify  chan struct{}
	injectCommandErr error
	injectRawErr     error
	signalErr        error
}

func (c *scriptedController) Start() error { return nil }

func (c *scriptedController) InjectCommand(command string) error {
	c.injectedCommands = append(c.injectedCommands, command)
	if c.injectCommandCh != nil {
		select {
		case c.injectCommandCh <- struct{}{}:
		default:
		}
	}
	return c.injectCommandErr
}

func (c *scriptedController) InjectRaw(data []byte) error {
	c.injectedRaw = append(c.injectedRaw, append([]byte(nil), data...))
	if c.injectRawNotify != nil {
		select {
		case c.injectRawNotify <- struct{}{}:
		default:
		}
	}
	return c.injectRawErr
}

func (c *scriptedController) ReadOutput() (string, error) {
	if c.index >= len(c.outputs) {
		return "", io.EOF
	}
	out := c.outputs[c.index]
	c.index++
	return out, nil
}

func (c *scriptedController) ReadLine(context.Context) (string, error) {
	return "", io.EOF
}

func (c *scriptedController) WaitForPattern(context.Context, *regexp.Regexp, time.Duration) (string, error) {
	return "", io.EOF
}

func (c *scriptedController) Wait() (int, error) { return 0, nil }

func (c *scriptedController) Signal(pty.Signal) error { return c.signalErr }

func (c *scriptedController) Close() error { return nil }

func (c *scriptedController) Fd() int { return -1 }

func installSmartRunnerTerminalHooks(t *testing.T) {
	t.Helper()

	originalCandidates := smartRunnerTerminalCandidates
	originalInputSource := smartRunnerInputSource
	originalDuplicateInput := smartRunnerDuplicateInput
	originalIsTerminal := smartRunnerIsTerminal
	originalGetSize := smartRunnerGetSize
	originalMakeRaw := smartRunnerMakeRaw
	originalRestore := smartRunnerRestoreTerm

	t.Cleanup(func() {
		smartRunnerTerminalCandidates = originalCandidates
		smartRunnerInputSource = originalInputSource
		smartRunnerDuplicateInput = originalDuplicateInput
		smartRunnerIsTerminal = originalIsTerminal
		smartRunnerGetSize = originalGetSize
		smartRunnerMakeRaw = originalMakeRaw
		smartRunnerRestoreTerm = originalRestore
	})
}

func TestCurrentTerminalSizeFallsBackAcrossCandidates(t *testing.T) {
	installSmartRunnerTerminalHooks(t)

	firstRead, firstWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("first pipe: %v", err)
	}
	defer firstRead.Close()
	defer firstWrite.Close()

	secondRead, secondWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("second pipe: %v", err)
	}
	defer secondRead.Close()
	defer secondWrite.Close()

	smartRunnerTerminalCandidates = func() []*os.File { return []*os.File{firstRead, secondRead} }
	smartRunnerIsTerminal = func(int) bool { return true }
	smartRunnerGetSize = func(fd int) (int, int, error) {
		switch fd {
		case int(firstRead.Fd()):
			return 0, 0, io.ErrUnexpectedEOF
		case int(secondRead.Fd()):
			return 132, 43, nil
		default:
			return 0, 0, io.EOF
		}
	}

	cols, rows, ok := currentTerminalSize()
	if !ok || cols != 132 || rows != 43 {
		t.Fatalf("currentTerminalSize() = (%d,%d,%v), want (132,43,true)", cols, rows, ok)
	}

	opts := currentPTYOptions()
	if opts.Cols != 132 || opts.Rows != 43 {
		t.Fatalf("currentPTYOptions() size = (%d,%d), want (132,43)", opts.Cols, opts.Rows)
	}
	if !opts.DisableEcho {
		t.Fatal("expected PTY echo to be disabled")
	}
}

func TestCurrentPTYOptionsUsesDefaultsWithoutTerminal(t *testing.T) {
	installSmartRunnerTerminalHooks(t)

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer readPipe.Close()
	defer writePipe.Close()

	smartRunnerTerminalCandidates = func() []*os.File { return []*os.File{readPipe} }
	smartRunnerIsTerminal = func(int) bool { return false }
	smartRunnerGetSize = func(int) (int, int, error) { return 200, 55, nil }

	cols, rows, ok := currentTerminalSize()
	if ok || cols != 0 || rows != 0 {
		t.Fatalf("currentTerminalSize() = (%d,%d,%v), want (0,0,false)", cols, rows, ok)
	}

	opts := currentPTYOptions()
	if opts.Cols != 80 || opts.Rows != 24 {
		t.Fatalf("default PTY size = (%d,%d), want (80,24)", opts.Cols, opts.Rows)
	}
	if !opts.DisableEcho {
		t.Fatal("expected PTY echo to be disabled")
	}
}

func TestSmartRunner_StartInteractiveInputRelayFallsBackAfterDupFailure(t *testing.T) {
	installSmartRunnerTerminalHooks(t)

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer readPipe.Close()

	smartRunnerInputSource = func() *os.File { return readPipe }
	smartRunnerDuplicateInput = func(*os.File) (*os.File, func(), error) {
		return nil, nil, io.ErrUnexpectedEOF
	}
	smartRunnerIsTerminal = func(int) bool { return false }

	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{})
	ctrl := &scriptedController{injectRawNotify: make(chan struct{}, 1)}
	sr.setPTYController(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleanup := sr.startInteractiveInputRelay(ctx)
	if cleanup == nil {
		t.Fatal("expected relay cleanup function")
	}

	if _, err := writePipe.Write([]byte("hello relay\n")); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := writePipe.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	select {
	case <-ctrl.injectRawNotify:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay input to reach PTY controller")
	}
	cleanup()

	if len(ctrl.injectedRaw) != 1 {
		t.Fatalf("expected 1 forwarded chunk, got %d", len(ctrl.injectedRaw))
	}
	if got := string(ctrl.injectedRaw[0]); got != "hello relay\n" {
		t.Fatalf("forwarded chunk = %q, want %q", got, "hello relay\n")
	}
}

func TestSmartRunner_StartInteractiveInputRelayRestoresRawTerminal(t *testing.T) {
	installSmartRunnerTerminalHooks(t)

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer writePipe.Close()

	smartRunnerInputSource = func() *os.File { return readPipe }
	smartRunnerDuplicateInput = func(*os.File) (*os.File, func(), error) {
		return readPipe, func() { _ = readPipe.Close() }, nil
	}
	expectedFD := int(readPipe.Fd())
	smartRunnerIsTerminal = func(fd int) bool { return fd == expectedFD }

	makeRawCalls := 0
	restoreCalls := 0
	smartRunnerMakeRaw = func(fd int) (*term.State, error) {
		makeRawCalls++
		return nil, nil
	}
	smartRunnerRestoreTerm = func(fd int, state *term.State) error {
		restoreCalls++
		if fd != expectedFD {
			t.Fatalf("restore fd = %d, want %d", fd, expectedFD)
		}
		return nil
	}

	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleanup := sr.startInteractiveInputRelay(ctx)
	if cleanup == nil {
		t.Fatal("expected relay cleanup function")
	}

	if err := writePipe.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	cleanup()

	if makeRawCalls != 1 {
		t.Fatalf("MakeRaw calls = %d, want 1", makeRawCalls)
	}
	if restoreCalls != 1 {
		t.Fatalf("Restore calls = %d, want 1", restoreCalls)
	}
}

func TestSmartRunner_MaybeSchedulePostStartCommand(t *testing.T) {
	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{})

	t.Run("injects command after delay", func(t *testing.T) {
		ctrl := &scriptedController{injectCommandCh: make(chan struct{}, 1)}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sr.maybeSchedulePostStartCommand(ctx, ctrl, "continue", 5*time.Millisecond)

		select {
		case <-ctrl.injectCommandCh:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for post-start command injection")
		}
		sr.wg.Wait()

		if len(ctrl.injectedCommands) != 1 || ctrl.injectedCommands[0] != "continue" {
			t.Fatalf("injected commands = %v", ctrl.injectedCommands)
		}
	})

	t.Run("skips injection after cancellation", func(t *testing.T) {
		ctrl := &scriptedController{injectCommandCh: make(chan struct{}, 1)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		sr.maybeSchedulePostStartCommand(ctx, ctrl, "continue", 20*time.Millisecond)
		select {
		case <-ctrl.injectCommandCh:
			t.Fatal("expected canceled context to prevent command injection")
		case <-time.After(50 * time.Millisecond):
		}
		sr.wg.Wait()
	})
}

func TestSmartRunner_NoteSeamlessResumeOutput(t *testing.T) {
	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{})

	sr.noteSeamlessResumeOutput("")
	if sr.seamlessResumeOutput {
		t.Fatal("expected blank output to be ignored")
	}

	sr.seamlessResumePending = true
	sr.rateLimitDispatchPausedUntil = time.Now().Add(time.Minute)
	sr.noteSeamlessResumeOutput("still draining stale output")
	if sr.seamlessResumeOutput {
		t.Fatal("expected paused redispatch window to suppress seamless output marker")
	}

	sr.rateLimitDispatchPausedUntil = time.Time{}
	sr.noteSeamlessResumeOutput("fresh output after switch")
	if !sr.seamlessResumeOutput {
		t.Fatal("expected non-empty output to mark seamless resume output once pending")
	}
}

func TestDuplicateInputSource(t *testing.T) {
	duplicated, closeDup, err := duplicateInputSource(nil)
	if err != nil {
		t.Fatalf("duplicateInputSource(nil) error = %v", err)
	}
	if duplicated != nil {
		t.Fatal("expected nil duplicated input for nil stdin")
	}
	closeDup()

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer readPipe.Close()

	duplicated, closeDup, err = duplicateInputSource(readPipe)
	if err != nil {
		t.Fatalf("duplicateInputSource(readPipe) error = %v", err)
	}
	defer closeDup()

	if duplicated == nil {
		t.Fatal("expected duplicated input file")
	}
	if duplicated.Fd() == readPipe.Fd() {
		t.Fatal("expected duplicated input to use a distinct fd")
	}

	if _, err := writePipe.Write([]byte("dup-check")); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := writePipe.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	data, err := io.ReadAll(duplicated)
	if err != nil {
		t.Fatalf("read duplicated input: %v", err)
	}
	if string(data) != "dup-check" {
		t.Fatalf("duplicated input = %q, want %q", string(data), "dup-check")
	}
}

func writeSmartRunnerFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newCodexRateLimitedSmartRunner(t *testing.T, vaultDir string, notifier *mockNotifier) *SmartRunner {
	t.Helper()

	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	detector, err := ratelimit.NewDetector(ratelimit.ProviderCodex, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	if !detector.Check("refresh_token_reused") {
		t.Fatal("expected detector to accept codex auth refresh failure fixture")
	}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Vault:    authfile.NewVault(vaultDir),
		AuthPool: authpool.NewAuthPool(),
		Notifier: notifier,
		Rotation: rotation.NewSelector(rotation.AlgorithmSmart, nil, nil),
	})
	sr.detector = detector
	sr.loginHandler = handoff.GetHandler("codex")
	sr.providerID = "codex"
	sr.currentProfile = "active"
	return sr
}

func TestSmartRunner_NotifierIntegration(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	notifier := &mockNotifier{}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Notifier: notifier,
	})

	// Test notifyHandoff
	sr.notifyHandoff("profile1", "profile2")

	if len(notifier.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(notifier.alerts))
	}
	if notifier.alerts[0].Level != notify.Info {
		t.Errorf("expected Info level, got %v", notifier.alerts[0].Level)
	}
	if notifier.alerts[0].Title != "Switching profiles" {
		t.Errorf("unexpected title: %s", notifier.alerts[0].Title)
	}
}

func TestSmartRunner_FailWithManual(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	notifier := &mockNotifier{}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Notifier: notifier,
	})
	sr.currentProfile = "test-profile"

	sr.failWithManual("test error: %s", "details")

	if sr.state != HandoffFailed {
		t.Errorf("state = %v, want %v", sr.state, HandoffFailed)
	}
	if len(notifier.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(notifier.alerts))
	}
	if notifier.alerts[0].Level != notify.Warning {
		t.Errorf("expected Warning level, got %v", notifier.alerts[0].Level)
	}
}

func TestIsCodexReauthRequiredReason(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "refresh token reused", reason: "refresh_token_reused", want: true},
		{name: "already used", reason: "Your refresh token was already used", want: true},
		{name: "access token could not be refreshed", reason: "access token could not be refreshed", want: true},
		{name: "invalid grant", reason: "invalid_grant", want: true},
		{name: "token expired invalid", reason: "token expired or invalid", want: true},
		{name: "normal 429", reason: "429 too many requests", want: false},
		{name: "empty", reason: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isCodexReauthRequiredReason(tc.reason)
			if got != tc.want {
				t.Fatalf("isCodexReauthRequiredReason(%q) = %v, want %v", tc.reason, got, tc.want)
			}
		})
	}
}

func TestShouldSkipInteractiveLogin(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		reason   string
		want     bool
	}{
		{name: "codex generic rate limit", provider: "codex", reason: "rate limit exceeded", want: true},
		{name: "codex credits exhausted", provider: "codex", reason: "out of credits", want: true},
		{name: "codex auth refresh failure", provider: "codex", reason: "refresh_token_reused", want: false},
		{name: "claude never skips", provider: "claude", reason: "rate limit", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSkipInteractiveLogin(tc.provider, tc.reason)
			if got != tc.want {
				t.Fatalf("shouldSkipInteractiveLogin(%q,%q) = %v, want %v", tc.provider, tc.reason, got, tc.want)
			}
		})
	}
}

func TestPreferUserProfiles(t *testing.T) {
	t.Run("prefers non-system profiles when available", func(t *testing.T) {
		got := preferUserProfiles([]string{"_backup_1", "work", "_original"})
		if len(got) != 1 || got[0] != "work" {
			t.Fatalf("preferUserProfiles returned %v", got)
		}
	})

	t.Run("falls back to system profiles when no user profiles exist", func(t *testing.T) {
		in := []string{"_backup_1", "_backup_2"}
		got := preferUserProfiles(in)
		if len(got) != 2 || got[0] != "_backup_1" || got[1] != "_backup_2" {
			t.Fatalf("preferUserProfiles returned %v", got)
		}
	})
}

func TestSystemProfileFallbackHelpers(t *testing.T) {
	if allSystemProfiles([]string{}) {
		t.Fatal("expected empty slice to not be treated as all-system")
	}
	if !allSystemProfiles([]string{"_backup_a", "_backup_b"}) {
		t.Fatal("expected allSystemProfiles=true for system-only slice")
	}
	if allSystemProfiles([]string{"_backup_a", "work"}) {
		t.Fatal("expected allSystemProfiles=false for mixed slice")
	}

	selected := selectDeterministicSystemFallback([]string{"_backup_z", "_backup_a", "_backup_m"})
	if selected != "_backup_a" {
		t.Fatalf("deterministic system fallback selected %q, want _backup_a", selected)
	}
	if selectDeterministicSystemFallback(nil) != "" {
		t.Fatal("expected empty fallback when no system profiles are available")
	}
}

func TestSmartRunner_ExcludeSharedCredentialAliases(t *testing.T) {
	vaultDir := t.TempDir()
	writeAuth := func(profile, token string) {
		dir := filepath.Join(vaultDir, "codex", profile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		content := []byte(`{"tokens":{"access_token":"` + token + `"}}`)
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), content, 0o600); err != nil {
			t.Fatalf("write auth %s: %v", profile, err)
		}
	}

	writeAuth("active", "token-a")
	writeAuth("alias", "token-a")
	writeAuth("backup", "token-b")

	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{
		Vault: authfile.NewVault(vaultDir),
	})

	filtered := sr.excludeSharedCredentialAliases("codex", "active", []string{"alias", "backup", "missing"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 profiles after filtering, got %v", filtered)
	}
	if filtered[0] != "backup" || filtered[1] != "missing" {
		t.Fatalf("unexpected filtered profiles: %v", filtered)
	}
}

func TestSmartRunner_FilterSelectionPoolDetectsAliasOnlySet(t *testing.T) {
	vaultDir := t.TempDir()
	writeAuth := func(profile, token string) {
		dir := filepath.Join(vaultDir, "codex", profile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		content := []byte(`{"tokens":{"access_token":"` + token + `"}}`)
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), content, 0o600); err != nil {
			t.Fatalf("write auth %s: %v", profile, err)
		}
	}

	writeAuth("active", "token-a")
	writeAuth("alias-a", "token-a")
	writeAuth("alias-b", "token-a")

	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{
		Vault: authfile.NewVault(vaultDir),
	})

	filtered, aliasOnly := sr.filterSelectionPool("codex", "active", []string{"alias-a", "alias-b"})
	if len(filtered) != 0 {
		t.Fatalf("expected alias-only pool to be fully filtered, got %v", filtered)
	}
	if !aliasOnly {
		t.Fatal("expected aliasOnly=true when candidates all share the current credential")
	}
}

func TestSmartRunner_ExcludeActiveCooldownProfiles(t *testing.T) {
	registry := provider.NewRegistry()
	pool := authpool.NewAuthPool()
	pool.AddProfile("codex", "_backup_ready")
	pool.AddProfile("codex", "_backup_cooldown")
	pool.SetCooldown("codex", "_backup_cooldown", time.Hour)

	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{
		AuthPool: pool,
	})

	filtered := sr.excludeActiveCooldownProfiles("codex", []string{"_backup_ready", "_backup_cooldown"})
	if len(filtered) != 1 || filtered[0] != "_backup_ready" {
		t.Fatalf("unexpected cooldown filtered set: %v", filtered)
	}
}

func TestSmartRunner_ExcludeActiveCooldownProfiles_UsesDatabaseCooldowns(t *testing.T) {
	db, err := caamdb.OpenAt(filepath.Join(t.TempDir(), "caam.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.SetCooldown("codex", "db_blocked", time.Now(), time.Hour, "rate limit"); err != nil {
		t.Fatalf("set cooldown: %v", err)
	}

	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{DB: db})
	filtered := sr.excludeActiveCooldownProfiles("codex", []string{"db_blocked", "ready"})
	if len(filtered) != 1 || filtered[0] != "ready" {
		t.Fatalf("unexpected database cooldown filtered set: %v", filtered)
	}
}

func TestCloneProfileForSwitch(t *testing.T) {
	if cloneProfileForSwitch(nil, "backup") != nil {
		t.Fatal("expected nil profile clone to stay nil")
	}

	current := &profile.Profile{Name: "active", BasePath: filepath.Join(t.TempDir(), "active")}
	cloned := cloneProfileForSwitch(current, "backup")
	if cloned == nil {
		t.Fatal("expected cloned profile")
	}
	if cloned.Name != "backup" {
		t.Fatalf("cloned name = %q, want %q", cloned.Name, "backup")
	}
	if cloned.BasePath != filepath.Join(filepath.Dir(current.BasePath), "backup") {
		t.Fatalf("cloned base path = %q", cloned.BasePath)
	}

	noBasePath := &profile.Profile{Name: "active"}
	cloned = cloneProfileForSwitch(noBasePath, "backup")
	if cloned.BasePath != "" {
		t.Fatalf("expected empty base path to remain empty, got %q", cloned.BasePath)
	}
}

func TestSmartRunner_VaultPath(t *testing.T) {
	sr := NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{})
	if got := sr.vaultPath(); got != authfile.DefaultVaultPath() {
		t.Fatalf("vaultPath() = %q, want %q", got, authfile.DefaultVaultPath())
	}

	customVault := authfile.NewVault(t.TempDir())
	sr = NewSmartRunner(NewRunner(provider.NewRegistry()), SmartRunnerOptions{Vault: customVault})
	if got := sr.vaultPath(); got != customVault.BasePath() {
		t.Fatalf("vaultPath() = %q, want %q", got, customVault.BasePath())
	}
}

func TestIsProviderSwitchCandidateForHandoff(t *testing.T) {
	t.Run("codex keeps credit-available profile as candidate", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: true, Unlimited: false},
			SecondaryWindow: &usage.UsageWindow{
				Utilization: 0.99,
				UsedPercent: 99,
			},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile with credits to remain candidate")
		}
	})

	t.Run("codex at 100% secondary usage is excluded", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: true, Unlimited: false},
			SecondaryWindow: &usage.UsageWindow{
				UsedPercent: 100,
			},
		}
		if isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile at 100% secondary usage to be excluded")
		}
	})

	t.Run("codex without explicit depletion remains candidate", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile without explicit depletion to remain candidate")
		}
	})

	t.Run("codex with zero balance but no windows remains candidate", func(t *testing.T) {
		zero := 0.0
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false, Balance: &zero},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile with zero balance and no windows to remain candidate")
		}
	})

	t.Run("codex with zero balance but active window remains candidate", func(t *testing.T) {
		zero := 0.0
		info := &usage.UsageInfo{
			Provider: "codex",
			Credits:  &usage.CreditInfo{HasCredits: false, Unlimited: false, Balance: &zero},
			PrimaryWindow: &usage.UsageWindow{
				UsedPercent: 28,
			},
		}
		if !isProviderSwitchCandidateForHandoff("codex", info, 0.8) {
			t.Fatal("expected codex profile with active window to remain candidate")
		}
	})

	t.Run("claude near-limit is excluded", func(t *testing.T) {
		info := &usage.UsageInfo{
			Provider: "claude",
			PrimaryWindow: &usage.UsageWindow{
				Utilization: 0.95,
				UsedPercent: 95,
			},
		}
		if isProviderSwitchCandidateForHandoff("claude", info, 0.8) {
			t.Fatal("expected near-limit claude profile to be excluded")
		}
	})
}

func TestSmartRunner_WithRotation(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	selector := rotation.NewSelector(rotation.AlgorithmSmart, nil, nil)

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Rotation: selector,
	})

	if sr.rotation != selector {
		t.Error("rotation selector not set correctly")
	}
}

func TestSmartRunner_ResumeHint(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	sr := NewSmartRunner(runner, SmartRunnerOptions{})

	sr.providerID = "codex"
	sr.currentProfile = "work"
	sr.lastSessionID = "019b2e3d-b524-7c22-91da-47de9068d09a"

	if got := sr.resumeHint("backup"); got != "caam resume codex backup --session 019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected fallback resume hint: %q", got)
	}

	sr.updateResumeHint("codex resume 019b2e3d-b524-7c22-91da-47de9068d09a", "")
	if got := sr.resumeHint("backup"); got != "codex resume 019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected captured resume hint: %q", got)
	}
}

func TestResumeArgsFromHint(t *testing.T) {
	t.Run("parses codex resume hint", func(t *testing.T) {
		args, ok := resumeArgsFromHint("codex", "codex resume 019b2e3d-b524-7c22-91da-47de9068d09a", "")
		if !ok {
			t.Fatal("expected hint to parse")
		}
		if len(args) != 2 || args[0] != "resume" || args[1] != "019b2e3d-b524-7c22-91da-47de9068d09a" {
			t.Fatalf("unexpected resume args: %v", args)
		}
	})

	t.Run("falls back to session ID", func(t *testing.T) {
		args, ok := resumeArgsFromHint("codex", "", "fallback-session")
		if !ok {
			t.Fatal("expected fallback session to parse")
		}
		if len(args) != 2 || args[0] != "resume" || args[1] != "fallback-session" {
			t.Fatalf("unexpected fallback args: %v", args)
		}
	})

	t.Run("preserves quoted prompt and options from hint", func(t *testing.T) {
		hint := `To continue this session, run codex resume --model gpt-5 019b2e3d-b524-7c22-91da-47de9068d09a "continue from here"`
		args, ok := resumeArgsFromHint("codex", hint, "")
		if !ok {
			t.Fatal("expected quoted hint to parse")
		}
		if len(args) != 5 {
			t.Fatalf("unexpected arg count: %v", args)
		}
		if args[0] != "resume" || args[1] != "--model" || args[2] != "gpt-5" || args[3] != "019b2e3d-b524-7c22-91da-47de9068d09a" || args[4] != "continue from here" {
			t.Fatalf("unexpected parsed args: %v", args)
		}
	})

	t.Run("rejects non-codex hint without fallback", func(t *testing.T) {
		if args, ok := resumeArgsFromHint("claude", "caam resume codex backup --session id", ""); ok || len(args) > 0 {
			t.Fatalf("expected no args, got %v (ok=%v)", args, ok)
		}
	})
}

func TestResumeHasPrompt(t *testing.T) {
	if resumeHasPrompt([]string{"resume", "session-1"}, "session-1") {
		t.Fatal("expected bare resume command to require a continuation prompt")
	}
	if !resumeHasPrompt([]string{"resume", "session-1", "continue previous task"}, "session-1") {
		t.Fatal("expected resume command with trailing payload to be treated as already prompted")
	}
	if !resumeHasPrompt([]string{"resume", "--model", "gpt-5", "session-1", "continue previous task"}, "session-1") {
		t.Fatal("expected prompt detection to survive leading options")
	}
}

func TestResumeHasPrompt_FallbackCases(t *testing.T) {
	t.Run("empty session id falls back to trailing args", func(t *testing.T) {
		if resumeHasPrompt([]string{"resume", "session-1"}, "") {
			t.Fatal("expected bare resume command without session context to be treated as promptless")
		}
		if !resumeHasPrompt([]string{"resume", "session-1", "continue previous task"}, "") {
			t.Fatal("expected trailing payload to count as a continuation prompt")
		}
	})

	t.Run("missing session id still treats trailing payload as a prompt", func(t *testing.T) {
		if !resumeHasPrompt([]string{"resume", "--model", "gpt-5", "continue previous task"}, "session-1") {
			t.Fatal("expected fallback branch to treat trailing payload as a prompt when the session id is absent")
		}
	})
}

func TestSmartRunner_HandleRateLimit_MaxHandoffs(t *testing.T) {
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	notifier := &mockNotifier{}
	detector, err := ratelimit.NewDetector(ratelimit.ProviderClaude, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	if !detector.Check("rate limit exceeded") {
		t.Fatal("expected detector to accept rate limit fixture")
	}

	sr := NewSmartRunner(runner, SmartRunnerOptions{Notifier: notifier})
	sr.detector = detector
	sr.loginHandler = handoff.GetHandler("claude")
	sr.currentProfile = "active"
	sr.handoffCount = 1
	sr.maxHandoffs = 1

	sr.handleRateLimit(context.Background())

	if sr.getState() != HandoffFailed {
		t.Fatalf("state = %v, want %v", sr.getState(), HandoffFailed)
	}
	if len(notifier.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(notifier.alerts))
	}
	if notifier.alerts[0].Title != "Auto-handoff failed" {
		t.Fatalf("unexpected alert title: %q", notifier.alerts[0].Title)
	}
	if notifier.alerts[0].Message != "maximum profile switches reached for this session (1)" {
		t.Fatalf("unexpected alert message: %q", notifier.alerts[0].Message)
	}
}

func TestSmartRunner_HandleRateLimit_RefreshTokenFailureAppliesExtendedCooldown(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	vaultDir := t.TempDir()
	notifier := &mockNotifier{}
	sr := newCodexRateLimitedSmartRunner(t, vaultDir, notifier)
	sr.authPool.AddProfile("codex", "active")

	writeSmartRunnerFixtureFile(t, filepath.Join(codexHome, "auth.json"), "active-token")

	sr.handleRateLimit(context.Background())

	profileState := sr.authPool.GetProfile("codex", "active")
	if profileState == nil {
		t.Fatal("expected active profile to remain tracked in auth pool")
	}
	if profileState.Status != authpool.PoolStatusCooldown {
		t.Fatalf("status = %v, want cooldown", profileState.Status)
	}
	if remaining := time.Until(profileState.CooldownUntil); remaining < 23*time.Hour {
		t.Fatalf("cooldown remaining = %v, want at least 23h", remaining)
	}
	if sr.getState() != Running {
		t.Fatalf("state = %v, want %v after rollback", sr.getState(), Running)
	}
	if sr.currentProfile != "active" {
		t.Fatalf("currentProfile = %q, want %q after rollback", sr.currentProfile, "active")
	}
	if len(notifier.alerts) == 0 || notifier.alerts[len(notifier.alerts)-1].Title != "Auto-handoff failed" {
		t.Fatal("expected auto-handoff failure alert to be emitted")
	}
}

func TestSmartRunner_HandleRateLimit_CodexSystemFallbackSkipsInteractiveLogin(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	vaultDir := t.TempDir()
	notifier := &mockNotifier{}
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	detector, err := ratelimit.NewDetector(ratelimit.ProviderCodex, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	if !detector.Check("rate limit exceeded") {
		t.Fatal("expected detector to accept codex rate limit fixture")
	}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Vault:    authfile.NewVault(vaultDir),
		Notifier: notifier,
	})
	sr.detector = detector
	sr.loginHandler = handoff.GetHandler("codex")
	sr.providerID = "codex"
	sr.currentProfile = "active"
	sr.lastSessionID = "session-1"

	writeSmartRunnerFixtureFile(t, filepath.Join(codexHome, "auth.json"), "active-token")
	writeSmartRunnerFixtureFile(t, filepath.Join(vaultDir, "codex", "_backup_ready", "auth.json"), "backup-token")

	sr.handleRateLimit(context.Background())

	if sr.getState() != Running {
		t.Fatalf("state = %v, want %v", sr.getState(), Running)
	}
	if sr.currentProfile != "_backup_ready" {
		t.Fatalf("currentProfile = %q, want %q", sr.currentProfile, "_backup_ready")
	}
	if sr.previousProfile != "active" {
		t.Fatalf("previousProfile = %q, want %q", sr.previousProfile, "active")
	}
	if sr.handoffCount != 1 {
		t.Fatalf("handoffCount = %d, want 1", sr.handoffCount)
	}
	if !sr.hasAuthSwapped() {
		t.Fatal("expected authSwapped to remain true after stored-credential handoff")
	}
	if !sr.seamlessResumePending {
		t.Fatal("expected seamless resume to be armed when a codex session id is available")
	}
	if got := sr.resumeHint("_backup_ready"); got != "caam resume codex _backup_ready --session session-1" {
		t.Fatalf("unexpected resume hint: %q", got)
	}
	liveAuth, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		t.Fatalf("read live auth: %v", err)
	}
	if string(liveAuth) != "backup-token" {
		t.Fatalf("live auth content = %q, want backup token", string(liveAuth))
	}
	if sr.detector.Detected() || sr.detector.Reason() != "" {
		t.Fatalf("expected detector to be reset, got detected=%v reason=%q", sr.detector.Detected(), sr.detector.Reason())
	}
}

func TestSmartRunner_HandleRateLimit_NoUsableFallbackRollsBackAndAlerts(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	vaultDir := t.TempDir()
	notifier := &mockNotifier{}
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	detector, err := ratelimit.NewDetector(ratelimit.ProviderCodex, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	if !detector.Check("rate limit exceeded") {
		t.Fatal("expected detector to accept codex rate limit fixture")
	}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Vault:    authfile.NewVault(vaultDir),
		Notifier: notifier,
	})
	sr.detector = detector
	sr.loginHandler = handoff.GetHandler("codex")
	sr.providerID = "codex"
	sr.currentProfile = "active"

	writeSmartRunnerFixtureFile(t, filepath.Join(codexHome, "auth.json"), "active-token")

	sr.handleRateLimit(context.Background())

	if sr.getState() != Running {
		t.Fatalf("state = %v, want %v after rollback", sr.getState(), Running)
	}
	if sr.currentProfile != "active" {
		t.Fatalf("currentProfile = %q, want active after rollback", sr.currentProfile)
	}
	if len(notifier.alerts) < 2 {
		t.Fatalf("expected at least 2 alerts, got %d", len(notifier.alerts))
	}
	lastAlert := notifier.alerts[len(notifier.alerts)-1]
	if lastAlert.Title != "Auto-handoff failed" {
		t.Fatalf("unexpected final alert title: %q", lastAlert.Title)
	}
	if lastAlert.Message != "all codex profiles are exhausted, in cooldown, locked, or out of credits" {
		t.Fatalf("unexpected failure alert message: %q", lastAlert.Message)
	}
	liveAuth, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		t.Fatalf("read live auth: %v", err)
	}
	if string(liveAuth) != "active-token" {
		t.Fatalf("live auth content = %q, want active token after rollback", string(liveAuth))
	}
}

func TestSmartRunner_HandleRateLimit_NotifierFailureStillCompletesStoredCredentialHandoff(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	vaultDir := t.TempDir()
	notifier := &mockNotifier{err: context.Canceled}
	registry := provider.NewRegistry()
	runner := NewRunner(registry)
	detector, err := ratelimit.NewDetector(ratelimit.ProviderCodex, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	if !detector.Check("rate limit exceeded") {
		t.Fatal("expected detector to accept codex rate limit fixture")
	}

	sr := NewSmartRunner(runner, SmartRunnerOptions{
		Vault:    authfile.NewVault(vaultDir),
		Notifier: notifier,
	})
	sr.detector = detector
	sr.loginHandler = handoff.GetHandler("codex")
	sr.providerID = "codex"
	sr.currentProfile = "active"
	sr.lastSessionID = "session-1"

	writeSmartRunnerFixtureFile(t, filepath.Join(codexHome, "auth.json"), "active-token")
	writeSmartRunnerFixtureFile(t, filepath.Join(vaultDir, "codex", "_backup_ready", "auth.json"), "backup-token")

	sr.handleRateLimit(context.Background())

	if sr.getState() != Running {
		t.Fatalf("state = %v, want %v", sr.getState(), Running)
	}
	if sr.currentProfile != "_backup_ready" {
		t.Fatalf("currentProfile = %q, want _backup_ready", sr.currentProfile)
	}
	if sr.handoffCount != 1 {
		t.Fatalf("handoffCount = %d, want 1", sr.handoffCount)
	}
	if !sr.seamlessResumePending {
		t.Fatal("expected seamless resume to remain armed despite notifier failure")
	}
	if notifier.calls < 2 {
		t.Fatalf("expected notifier to be attempted at least twice, got %d", notifier.calls)
	}
}

func TestMaybeMarkSeamlessResumePendingAllowsOriginalCommandRerunWithoutResumeHint(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.seamlessResumeCanRerun = true

	sr.maybeMarkSeamlessResumePending("")

	if !sr.seamlessResumePending {
		t.Fatal("expected seamless resume to arm when the original command can be replayed")
	}
}

func TestSeamlessResumeDepth(t *testing.T) {
	t.Run("uses run option env first", func(t *testing.T) {
		depth := seamlessResumeDepth(RunOptions{
			Env: map[string]string{seamlessResumeDepthEnv: "1"},
		})
		if depth != 1 {
			t.Fatalf("depth = %d, want 1", depth)
		}
	})

	t.Run("falls back to process env", func(t *testing.T) {
		t.Setenv(seamlessResumeDepthEnv, "2")
		depth := seamlessResumeDepth(RunOptions{})
		if depth != 2 {
			t.Fatalf("depth = %d, want 2", depth)
		}
	})
}

func TestIsInterruptExitCode(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{code: 0, want: false},
		{code: 1, want: false},
		{code: 130, want: true},
		{code: 143, want: true},
	}
	for _, tc := range tests {
		if got := isInterruptExitCode(tc.code); got != tc.want {
			t.Fatalf("isInterruptExitCode(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestMaybeRunSeamlessResumeSkipsWhenWindowExpired(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now().Add(-(seamlessResumeMaxDelay + time.Second))

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 1, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected seamless resume to be skipped when arming window expired")
	}
}

func TestMaybeRunSeamlessResumeSkipsInterruptExit(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now()

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 130, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected seamless resume to be skipped for user-interrupt exit")
	}
}

func TestMaybeRunSeamlessResumeAllowsExitZeroAfterHandoff(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now()
	sr.seamlessResumeOutput = true

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 0, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected exit-zero run with post-handoff output to skip seamless resume")
	}
}

func TestMaybeRunSeamlessResumeSkipsWhenOutputContinuedAfterSwitch(t *testing.T) {
	registry := provider.NewRegistry()
	sr := NewSmartRunner(NewRunner(registry), SmartRunnerOptions{})
	sr.providerID = "codex"
	sr.currentProfile = "backup"
	sr.lastSessionID = "session-1"
	sr.seamlessResumePending = true
	sr.seamlessResumeHint = "codex resume session-1"
	sr.seamlessResumeArmedAt = time.Now()
	sr.seamlessResumeOutput = true

	resumed, err := sr.maybeRunSeamlessResume(context.Background(), RunOptions{
		Profile: &profile.Profile{
			Name:     "active",
			Provider: "codex",
			BasePath: filepath.Join(t.TempDir(), "active"),
		},
	}, 1, nil)

	if err != nil {
		t.Fatalf("maybeRunSeamlessResume returned unexpected error: %v", err)
	}
	if resumed {
		t.Fatal("expected seamless resume to be skipped after post-handoff output was observed")
	}
}
