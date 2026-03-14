package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	ptyctrl "github.com/Dicklesworthstone/coding_agent_account_manager/internal/pty"
)

// TestBinaryExecFixture builds real subprocess launchers that execute the
// current Go test binary with a narrow -test.run target and deterministic env.
type TestBinaryExecFixture struct {
	testRunPattern string
	baseEnv        map[string]string
}

// NewTestBinaryExecFixture returns a fixture targeting TestMockCLI_Handoff.
func NewTestBinaryExecFixture() *TestBinaryExecFixture {
	return &TestBinaryExecFixture{
		testRunPattern: "^TestMockCLI_Handoff$",
		baseEnv: map[string]string{
			"GO_WANT_MOCK_CLI": "1",
		},
	}
}

// WithTestRunPattern overrides the -test.run pattern.
func (f *TestBinaryExecFixture) WithTestRunPattern(pattern string) *TestBinaryExecFixture {
	f.testRunPattern = pattern
	return f
}

// WithBaseEnv sets a persistent env var for launched subprocesses.
func (f *TestBinaryExecFixture) WithBaseEnv(key, value string) *TestBinaryExecFixture {
	if f.baseEnv == nil {
		f.baseEnv = make(map[string]string)
	}
	f.baseEnv[key] = value
	return f
}

// ExecCommand returns an exec.CommandContext-compatible launcher for tests.
func (f *TestBinaryExecFixture) ExecCommand(extraEnv map[string]string) func(context.Context, string, ...string) *exec.Cmd {
	mergedEnv := mergeStringMaps(f.baseEnv, extraEnv)

	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=" + f.testRunPattern, "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), formatFixtureEnv(mergedEnv)...)
		return cmd
	}
}

func mergeStringMaps(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func formatFixtureEnv(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	formatted := make([]string, 0, len(keys))
	for _, key := range keys {
		formatted = append(formatted, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return formatted
}

// RealtimePTYSession wraps a real PTY controller and captures a transcript.
type RealtimePTYSession struct {
	controller ptyctrl.Controller
	transcript strings.Builder
}

// StartRealtimePTYSession starts a command inside a PTY controller.
func StartRealtimePTYSession(cmd *exec.Cmd, opts *ptyctrl.Options) (*RealtimePTYSession, error) {
	controller, err := ptyctrl.NewController(cmd, opts)
	if err != nil {
		return nil, err
	}
	if err := controller.Start(); err != nil {
		_ = controller.Close()
		return nil, err
	}
	return &RealtimePTYSession{controller: controller}, nil
}

// StartRealtimePTYSessionFromArgs starts a PTY-backed session from argv.
func StartRealtimePTYSessionFromArgs(name string, args []string, opts *ptyctrl.Options) (*RealtimePTYSession, error) {
	cmd := exec.Command(name, args...)
	return StartRealtimePTYSession(cmd, opts)
}

// InjectCommand writes a command plus newline into the PTY.
func (s *RealtimePTYSession) InjectCommand(cmd string) error {
	return s.controller.InjectCommand(cmd)
}

// InjectRaw writes raw bytes into the PTY.
func (s *RealtimePTYSession) InjectRaw(data []byte) error {
	return s.controller.InjectRaw(data)
}

// ReadOutput reads currently available PTY output and appends it to transcript.
func (s *RealtimePTYSession) ReadOutput() (string, error) {
	output, err := s.controller.ReadOutput()
	if output != "" {
		s.transcript.WriteString(output)
	}
	return output, err
}

// ReadLine reads a line from the PTY and appends it to transcript.
func (s *RealtimePTYSession) ReadLine(ctx context.Context) (string, error) {
	line, err := s.controller.ReadLine(ctx)
	if line != "" {
		s.transcript.WriteString(line)
	}
	return line, err
}

// WaitForPattern waits for a PTY output pattern and appends matched output.
func (s *RealtimePTYSession) WaitForPattern(ctx context.Context, pattern *regexp.Regexp, timeout time.Duration) (string, error) {
	output, err := s.controller.WaitForPattern(ctx, pattern, timeout)
	if output != "" {
		s.transcript.WriteString(output)
	}
	return output, err
}

// Wait waits for the PTY command to exit.
func (s *RealtimePTYSession) Wait() (int, error) {
	return s.controller.Wait()
}

// Close closes the PTY controller.
func (s *RealtimePTYSession) Close() error {
	return s.controller.Close()
}

// Transcript returns all output captured through this wrapper.
func (s *RealtimePTYSession) Transcript() string {
	return s.transcript.String()
}

// Fd returns the PTY master file descriptor.
func (s *RealtimePTYSession) Fd() int {
	return s.controller.Fd()
}
