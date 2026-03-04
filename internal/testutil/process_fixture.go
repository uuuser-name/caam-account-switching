// Package testutil provides E2E test infrastructure with detailed logging.
//
// ProcessFixture provides deterministic helper processes for testing
// exec.Runner and command execution without relying on external commands.
package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// ProcessFixture - Deterministic helper processes for testing
// =============================================================================

// ProcessBehavior defines the behavior of a helper process.
type ProcessBehavior string

const (
	// BehaviorSuccess exits with code 0 immediately.
	BehaviorSuccess ProcessBehavior = "success"
	// BehaviorFailure exits with code 1 immediately.
	BehaviorFailure ProcessBehavior = "failure"
	// BehaviorSlowOutput writes output slowly then exits 0.
	BehaviorSlowOutput ProcessBehavior = "slow_output"
	// BehaviorSignal waits for signal then exits.
	BehaviorSignal ProcessBehavior = "signal"
	// BehaviorEchoArgs echoes all args to stdout.
	BehaviorEchoArgs ProcessBehavior = "echo_args"
	// BehaviorEchoEnv echoes named env vars to stdout.
	BehaviorEchoEnv ProcessBehavior = "echo_env"
	// BehaviorWriteFile writes content to a file.
	BehaviorWriteFile ProcessBehavior = "write_file"
	// BehaviorReadFile reads and outputs file content.
	BehaviorReadFile ProcessBehavior = "read_file"
	// BehaviorExitCode exits with specified code.
	BehaviorExitCode ProcessBehavior = "exit_code"
	// BehaviorTimeout sleeps then exits 0.
	BehaviorTimeout ProcessBehavior = "timeout"
	// BehaviorRateLimit outputs rate limit message.
	BehaviorRateLimit ProcessBehavior = "rate_limit"
	// BehaviorOAuth outputs OAuth URL.
	BehaviorOAuth ProcessBehavior = "oauth_url"
	// BehaviorSession outputs session resume hint.
	BehaviorSession ProcessBehavior = "session"
)

// ProcessFixtureConfig configures a helper process fixture.
type ProcessFixtureConfig struct {
	// Behavior is the process behavior.
	Behavior ProcessBehavior

	// ExitCode for BehaviorExitCode.
	ExitCode int

	// OutputLines for output behaviors.
	OutputLines []string

	// OutputDelay for slow output.
	OutputDelay time.Duration

	// EnvVars to echo for BehaviorEchoEnv.
	EnvVars []string

	// FilePath for file operations.
	FilePath string

	// FileContent for BehaviorWriteFile.
	FileContent string

	// SleepDuration for timeout behaviors.
	SleepDuration time.Duration

	// OAuthURL for BehaviorOAuth.
	OAuthURL string

	// SessionID for BehaviorSession.
	SessionID string
}

// ProcessFixture represents a compiled helper process.
type ProcessFixture struct {
	mu       sync.Mutex
	path     string
	config   ProcessFixtureConfig
	calls    []ProcessCall
	cleanups []func()
}

// ProcessCall records a single invocation of the fixture.
type ProcessCall struct {
	Args     []string
	Env      map[string]string
	WorkDir  string
	Start    time.Time
	End      time.Time
	ExitCode int
	Error    error
}

// ProcessFixtureBuilder builds helper process fixtures.
type ProcessFixtureBuilder struct {
	config   ProcessFixtureConfig
	cleanups []func()
}

// NewProcessFixtureBuilder creates a new fixture builder.
func NewProcessFixtureBuilder() *ProcessFixtureBuilder {
	return &ProcessFixtureBuilder{
		config: ProcessFixtureConfig{
			Behavior: BehaviorSuccess,
		},
		cleanups: make([]func(), 0),
	}
}

// WithBehavior sets the process behavior.
func (b *ProcessFixtureBuilder) WithBehavior(behavior ProcessBehavior) *ProcessFixtureBuilder {
	b.config.Behavior = behavior
	return b
}

// WithExitCode sets the exit code.
func (b *ProcessFixtureBuilder) WithExitCode(code int) *ProcessFixtureBuilder {
	b.config.ExitCode = code
	return b
}

// WithOutput sets output lines.
func (b *ProcessFixtureBuilder) WithOutput(lines ...string) *ProcessFixtureBuilder {
	b.config.OutputLines = lines
	return b
}

// WithOutputDelay sets delay between output lines.
func (b *ProcessFixtureBuilder) WithOutputDelay(d time.Duration) *ProcessFixtureBuilder {
	b.config.OutputDelay = d
	return b
}

// WithEnvVars sets env vars to echo.
func (b *ProcessFixtureBuilder) WithEnvVars(vars ...string) *ProcessFixtureBuilder {
	b.config.EnvVars = vars
	return b
}

// WithFilePath sets file path for file operations.
func (b *ProcessFixtureBuilder) WithFilePath(path string) *ProcessFixtureBuilder {
	b.config.FilePath = path
	return b
}

// WithFileContent sets content for write operations.
func (b *ProcessFixtureBuilder) WithFileContent(content string) *ProcessFixtureBuilder {
	b.config.FileContent = content
	return b
}

// WithSleepDuration sets sleep duration.
func (b *ProcessFixtureBuilder) WithSleepDuration(d time.Duration) *ProcessFixtureBuilder {
	b.config.SleepDuration = d
	return b
}

// WithOAuthURL sets OAuth URL.
func (b *ProcessFixtureBuilder) WithOAuthURL(url string) *ProcessFixtureBuilder {
	b.config.OAuthURL = url
	return b
}

// WithSessionID sets session ID.
func (b *ProcessFixtureBuilder) WithSessionID(id string) *ProcessFixtureBuilder {
	b.config.SessionID = id
	return b
}

// Build creates the fixture in the given temp directory.
func (b *ProcessFixtureBuilder) Build(tmpDir string) (*ProcessFixture, error) {
	fixture := &ProcessFixture{
		config:   b.config,
		calls:    make([]ProcessCall, 0),
		cleanups: make([]func(), 0),
	}

	// Generate unique name
	name := fmt.Sprintf("fixture_%s_%d", b.config.Behavior, time.Now().UnixNano())

	// Create the script/binary
	if runtime.GOOS == "windows" {
		fixture.path = filepath.Join(tmpDir, name+".bat")
		if err := fixture.writeWindowsScript(); err != nil {
			return nil, err
		}
	} else {
		fixture.path = filepath.Join(tmpDir, name)
		if err := fixture.writeUnixScript(); err != nil {
			return nil, err
		}
	}

	return fixture, nil
}

// BuildInTest creates a fixture using t.TempDir().
func (b *ProcessFixtureBuilder) BuildInTest(t *testing.T) *ProcessFixture {
	t.Helper()
	fixture, err := b.Build(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to build process fixture: %v", err)
	}
	return fixture
}

// writeUnixScript writes a shell script for Unix.
func (f *ProcessFixture) writeUnixScript() error {
	var script strings.Builder
	script.WriteString("#!/bin/bash\n\n")

	// Add call logging
	script.WriteString(`
CALL_LOG="${0}.calls"
echo "$(date -Iseconds)|$@|${PWD}" >> "$CALL_LOG"
`)

	switch f.config.Behavior {
	case BehaviorSuccess:
		script.WriteString("exit 0\n")

	case BehaviorFailure:
		script.WriteString("exit 1\n")

	case BehaviorExitCode:
		script.WriteString(fmt.Sprintf("exit %d\n", f.config.ExitCode))

	case BehaviorEchoArgs:
		script.WriteString("for arg in \"$@\"; do echo \"$arg\"; done\n")
		script.WriteString("exit 0\n")

	case BehaviorEchoEnv:
		for _, env := range f.config.EnvVars {
			script.WriteString(fmt.Sprintf("echo \"%s=${%s}\"\n", env, env))
		}
		script.WriteString("exit 0\n")

	case BehaviorSlowOutput:
		for _, line := range f.config.OutputLines {
			delay := f.config.OutputDelay
			if delay == 0 {
				delay = 100 * time.Millisecond
			}
			script.WriteString(fmt.Sprintf("sleep %f\n", delay.Seconds()))
			script.WriteString(fmt.Sprintf("echo %q\n", line))
		}
		script.WriteString("exit 0\n")

	case BehaviorTimeout:
		d := f.config.SleepDuration
		if d == 0 {
			d = 5 * time.Second
		}
		script.WriteString(fmt.Sprintf("sleep %f\n", d.Seconds()))
		script.WriteString("exit 0\n")

	case BehaviorWriteFile:
		path := f.config.FilePath
		if path == "" {
			path = "/tmp/fixture_output.txt"
		}
		script.WriteString(fmt.Sprintf("echo %q > %q\n", f.config.FileContent, path))
		script.WriteString("exit 0\n")

	case BehaviorReadFile:
		path := f.config.FilePath
		if path == "" {
			path = "/tmp/fixture_input.txt"
		}
		script.WriteString(fmt.Sprintf("cat %q\n", path))
		script.WriteString("exit 0\n")

	case BehaviorRateLimit:
		script.WriteString("echo 'Error: rate limit exceeded. Please try again later.' >&2\n")
		script.WriteString("echo 'Your usage has been limited. Visit https://example.com/limits for more information.' >&2\n")
		script.WriteString("exit 1\n")

	case BehaviorOAuth:
		url := f.config.OAuthURL
		if url == "" {
			url = "https://claude.ai/oauth/authorize?code=ABC123"
		}
		script.WriteString(fmt.Sprintf("echo 'Please visit: %s'\n", url))
		script.WriteString("exit 0\n")

	case BehaviorSession:
		sessionID := f.config.SessionID
		if sessionID == "" {
			sessionID = "12345678-1234-1234-1234-123456789abc"
		}
		script.WriteString(fmt.Sprintf("echo 'To continue this session, run codex resume %s'\n", sessionID))
		script.WriteString("exit 0\n")

	case BehaviorSignal:
		script.WriteString("# Wait for signal\n")
		script.WriteString("trap 'exit 0' SIGINT SIGTERM\n")
		script.WriteString("while true; do sleep 0.1; done\n")

	default:
		script.WriteString("exit 0\n")
	}

	return os.WriteFile(f.path, []byte(script.String()), 0755)
}

// writeWindowsScript writes a batch script for Windows.
func (f *ProcessFixture) writeWindowsScript() error {
	var script strings.Builder
	script.WriteString("@echo off\n\n")

	switch f.config.Behavior {
	case BehaviorSuccess:
		script.WriteString("exit /b 0\n")

	case BehaviorFailure:
		script.WriteString("exit /b 1\n")

	case BehaviorExitCode:
		script.WriteString(fmt.Sprintf("exit /b %d\n", f.config.ExitCode))

	case BehaviorEchoArgs:
		script.WriteString(":loop\n")
		script.WriteString("if \"%~1\"==\"\" goto end\n")
		script.WriteString("echo %~1\n")
		script.WriteString("shift\n")
		script.WriteString("goto loop\n")
		script.WriteString(":end\n")
		script.WriteString("exit /b 0\n")

	case BehaviorEchoEnv:
		for _, env := range f.config.EnvVars {
			script.WriteString(fmt.Sprintf("echo %s=%%%s%%\n", env, env))
		}
		script.WriteString("exit /b 0\n")

	case BehaviorSlowOutput:
		for _, line := range f.config.OutputLines {
			delay := f.config.OutputDelay
			if delay == 0 {
				delay = 100 * time.Millisecond
			}
			script.WriteString(fmt.Sprintf("ping -n 1 -w %d 127.0.0.1 > nul\n", delay.Milliseconds()))
			script.WriteString(fmt.Sprintf("echo %s\n", line))
		}
		script.WriteString("exit /b 0\n")

	case BehaviorTimeout:
		d := f.config.SleepDuration
		if d == 0 {
			d = 5 * time.Second
		}
		script.WriteString(fmt.Sprintf("ping -n 1 -w %d 127.0.0.1 > nul\n", d.Milliseconds()))
		script.WriteString("exit /b 0\n")

	case BehaviorRateLimit:
		script.WriteString("echo Error: rate limit exceeded. Please try again later. 1>&2\n")
		script.WriteString("exit /b 1\n")

	case BehaviorOAuth:
		url := f.config.OAuthURL
		if url == "" {
			url = "https://claude.ai/oauth/authorize?code=ABC123"
		}
		script.WriteString(fmt.Sprintf("echo Please visit: %s\n", url))
		script.WriteString("exit /b 0\n")

	case BehaviorSession:
		sessionID := f.config.SessionID
		if sessionID == "" {
			sessionID = "12345678-1234-1234-1234-123456789abc"
		}
		script.WriteString(fmt.Sprintf("echo To continue this session, run codex resume %s\n", sessionID))
		script.WriteString("exit /b 0\n")

	default:
		script.WriteString("exit /b 0\n")
	}

	return os.WriteFile(f.path, []byte(script.String()), 0755)
}

// Path returns the path to the fixture executable.
func (f *ProcessFixture) Path() string {
	return f.path
}

// Cmd creates an exec.Cmd for the fixture.
func (f *ProcessFixture) Cmd(args ...string) *exec.Cmd {
	cmd := exec.Command(f.path, args...)
	return cmd
}

// CmdContext creates an exec.Cmd with context.
func (f *ProcessFixture) CmdContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, f.path, args...)
	return cmd
}

// Run executes the fixture and records the call.
func (f *ProcessFixture) Run(ctx context.Context, args ...string) (int, error) {
	f.mu.Lock()
	call := ProcessCall{
		Args:  args,
		Env:   make(map[string]string),
		Start: time.Now(),
	}
	f.mu.Unlock()

	cmd := f.CmdContext(ctx, args...)
	err := cmd.Run()

	f.mu.Lock()
	call.End = time.Now()
	call.Error = err
	if exitErr, ok := err.(*exec.ExitError); ok {
		call.ExitCode = exitErr.ExitCode()
	}
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	return call.ExitCode, err
}

// Calls returns all recorded calls.
func (f *ProcessFixture) Calls() []ProcessCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]ProcessCall, len(f.calls))
	copy(result, f.calls)
	return result
}

// CallCount returns the number of recorded calls.
func (f *ProcessFixture) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// ClearCalls clears all recorded calls.
func (f *ProcessFixture) ClearCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = make([]ProcessCall, 0)
}

// Close cleans up the fixture.
func (f *ProcessFixture) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.path != "" {
		os.Remove(f.path)
		os.Remove(f.path + ".calls")
	}

	for _, cleanup := range f.cleanups {
		cleanup()
	}
}

// =============================================================================
// Pre-built Fixtures
// =============================================================================

// FixtureSuite provides a collection of common fixtures.
type FixtureSuite struct {
	Success   *ProcessFixture
	Failure   *ProcessFixture
	EchoArgs  *ProcessFixture
	EchoEnv   *ProcessFixture
	Slow      *ProcessFixture
	RateLimit *ProcessFixture
	OAuth     *ProcessFixture
	Session   *ProcessFixture
	Timeout   *ProcessFixture

	fixtures []*ProcessFixture
}

// NewFixtureSuite creates a suite of common fixtures.
func NewFixtureSuite(t *testing.T) *FixtureSuite {
	t.Helper()
	tmpDir := t.TempDir()

	suite := &FixtureSuite{
		fixtures: make([]*ProcessFixture, 0),
	}

	var err error

	suite.Success, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorSuccess).
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build success fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.Success)

	suite.Failure, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorFailure).
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build failure fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.Failure)

	suite.EchoArgs, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorEchoArgs).
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build echo args fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.EchoArgs)

	suite.EchoEnv, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorEchoEnv).
		WithEnvVars("HOME", "PATH", "USER", "TEST_VAR").
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build echo env fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.EchoEnv)

	suite.Slow, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorSlowOutput).
		WithOutput("line1", "line2", "line3").
		WithOutputDelay(50 * time.Millisecond).
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build slow fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.Slow)

	suite.RateLimit, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorRateLimit).
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build rate limit fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.RateLimit)

	suite.OAuth, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorOAuth).
		WithOAuthURL("https://claude.ai/oauth/authorize?code=test123").
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build oauth fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.OAuth)

	suite.Session, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorSession).
		WithSessionID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee").
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build session fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.Session)

	suite.Timeout, err = NewProcessFixtureBuilder().
		WithBehavior(BehaviorTimeout).
		WithSleepDuration(10 * time.Second).
		Build(tmpDir)
	if err != nil {
		t.Fatalf("Failed to build timeout fixture: %v", err)
	}
	suite.fixtures = append(suite.fixtures, suite.Timeout)

	return suite
}

// Close cleans up all fixtures.
func (s *FixtureSuite) Close() {
	for _, f := range s.fixtures {
		f.Close()
	}
}

// =============================================================================
// Process Assertions
// =============================================================================

// AssertExitCode asserts that a command exits with the expected code.
func AssertExitCode(t *testing.T, cmd *exec.Cmd, expected int) bool {
	t.Helper()
	err := cmd.Run()
	if err == nil {
		if expected != 0 {
			t.Errorf("Expected exit code %d, got 0", expected)
			return false
		}
		return true
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != expected {
			t.Errorf("Expected exit code %d, got %d", expected, exitErr.ExitCode())
			return false
		}
		return true
	}
	t.Errorf("Unexpected error: %v", err)
	return false
}

// AssertOutputContains asserts that command output contains a substring.
func AssertOutputContains(t *testing.T, cmd *exec.Cmd, substring string) bool {
	t.Helper()
	output, err := cmd.CombinedOutput()
	if err != nil && !isExitError(err) {
		t.Errorf("Command failed: %v", err)
		return false
	}
	if !strings.Contains(string(output), substring) {
		t.Errorf("Output does not contain %q\nGot: %s", substring, output)
		return false
	}
	return true
}

// AssertCommandTimesOut asserts that a command is killed by context timeout.
func AssertCommandTimesOut(t *testing.T, cmd *exec.Cmd, timeout time.Duration) bool {
	t.Helper()

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Command should have timed out but succeeded")
		return false
	}

	if elapsed > timeout*2 {
		t.Errorf("Command took %v, expected around %v", elapsed, timeout)
		return false
	}

	return true
}

// isExitError returns true if err is an ExitError.
func isExitError(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}