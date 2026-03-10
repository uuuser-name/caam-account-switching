// Package exec handles running AI CLI tools with profile isolation.
package exec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/ratelimit"
)

// Runner executes AI CLI tools with profile isolation.
type Runner struct {
	registry *provider.Registry
}

// NewRunner creates a new runner with the given provider registry.
func NewRunner(registry *provider.Registry) *Runner {
	return &Runner{registry: registry}
}

// RunOptions configures the exec behavior.
type RunOptions struct {
	// Profile is the profile to use.
	Profile *profile.Profile

	// Provider is the provider to use.
	Provider provider.Provider

	// Args are the arguments to pass to the CLI.
	Args []string

	// WorkDir is the working directory.
	WorkDir string

	// NoLock disables profile locking.
	NoLock bool

	// Env are additional environment variables.
	Env map[string]string

	// Binary overrides the provider default binary.
	// When set, this value is used as the executable for the session.
	Binary string

	// OnRateLimit is called when a rate limit is detected in command output.
	// It is invoked asynchronously and at most once per run.
	OnRateLimit func(ctx context.Context) error

	// RateLimitDelay debounces the rate limit callback to avoid rapid triggers.
	// If zero, the callback fires immediately.
	RateLimitDelay time.Duration

	// UseGlobalEnv disables directory isolation (HOME, etc.), forcing the tool
	// to use the global user environment. This is required for vault-based
	// auth file swapping (caam run).
	UseGlobalEnv bool

	// PostStartCommand injects a command into a PTY-managed session shortly
	// after startup. This is used for resumed interactive sessions that need
	// one extra prompt to continue working after re-entry.
	PostStartCommand string

	// PostStartDelay waits before injecting PostStartCommand.
	// If zero, the caller's default delay is used.
	PostStartDelay time.Duration
}

// ExitCodeError wraps a process exit code.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

// Run executes the AI CLI tool with profile isolation.
func (r *Runner) Run(ctx context.Context, opts RunOptions) error {
	// Lock profile if not disabled
	if !opts.NoLock {
		// Use LockWithCleanup to handle stale locks from dead processes
		if err := opts.Profile.LockWithCleanup(); err != nil {
			return fmt.Errorf("lock profile: %w", err)
		}
		defer opts.Profile.Unlock()
	}

	// Get provider environment
	var providerEnv map[string]string
	var err error
	if !opts.UseGlobalEnv {
		providerEnv, err = opts.Provider.Env(ctx, opts.Profile)
		if err != nil {
			return fmt.Errorf("get provider env: %w", err)
		}
	}

	// Build command
	bin := strings.TrimSpace(opts.Binary)
	if bin == "" {
		bin = opts.Provider.DefaultBin()
	}
	if bin == "" {
		return fmt.Errorf("no binary configured for provider %s", opts.Provider.ID())
	}
	cmd := exec.CommandContext(ctx, bin, opts.Args...)

	// Set up environment with deduplication (last one wins in our map logic)
	envMap := make(map[string]string)

	// 1. Start with inherited environment
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// 2. Apply provider environment (overrides inherited)
	for k, v := range providerEnv {
		envMap[k] = v
	}

	// 3. Apply custom environment options (overrides provider)
	for k, v := range opts.Env {
		envMap[k] = v
	}

	// Reassemble into slice
	cmd.Env = make([]string, 0, len(envMap))
	for k, v := range envMap {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Set working directory
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	var capture *codexSessionCapture
	var stdoutObserver, stderrObserver *lineObserverWriter
	var rateObserver func(line string)
	if opts.Provider.ID() == "codex" {
		capture = &codexSessionCapture{}
	}

	if opts.OnRateLimit != nil {
		detector, err := ratelimit.NewDetector(ratelimit.ProviderFromString(opts.Provider.ID()), nil)
		if err != nil {
			return fmt.Errorf("create rate limit detector: %w", err)
		}

		var once sync.Once
		trigger := func() {
			once.Do(func() {
				invoke := func() {
					if err := opts.OnRateLimit(ctx); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: rate limit callback failed: %v\n", err)
					}
				}
				if opts.RateLimitDelay <= 0 {
					go invoke()
					return
				}
				go func() {
					timer := time.NewTimer(opts.RateLimitDelay)
					defer timer.Stop()
					select {
					case <-ctx.Done():
						return
					case <-timer.C:
						invoke()
					}
				}()
			})
		}

		rateObserver = func(line string) {
			if detector.Check(line) {
				trigger()
			}
		}
	}

	var observers []func(string)
	if capture != nil {
		observers = append(observers, capture.ObserveLine)
	}
	if rateObserver != nil {
		observers = append(observers, rateObserver)
	}
	if len(observers) > 0 {
		onLine := func(line string) {
			for _, obs := range observers {
				obs(line)
			}
		}
		stdoutObserver = newLineObserverWriter(os.Stdout, onLine)
		stderrObserver = newLineObserverWriter(os.Stderr, onLine)
	}

	// Connect stdio
	cmd.Stdin = os.Stdin
	if stdoutObserver != nil {
		cmd.Stdout = stdoutObserver
	} else {
		cmd.Stdout = os.Stdout
	}
	if stderrObserver != nil {
		cmd.Stderr = stderrObserver
	} else {
		cmd.Stderr = os.Stderr
	}

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigChan:
				if cmd.Process != nil {
					cmd.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()
	defer close(done)
	defer signal.Stop(sigChan)

	// Run command
	runErr := cmd.Run()
	if stdoutObserver != nil {
		stdoutObserver.Flush()
	}
	if stderrObserver != nil {
		stderrObserver.Flush()
	}

	now := time.Now()
	opts.Profile.LastUsedAt = now
	if capture != nil {
		if sessionID := capture.ID(); sessionID != "" {
			opts.Profile.LastSessionID = sessionID
			opts.Profile.LastSessionTS = now.UTC()
		}
	}
	if err := opts.Profile.Save(); err != nil {
		// Don't hide the original process exit code, but do surface metadata issues
		// when the command otherwise succeeded.
		if runErr == nil {
			return fmt.Errorf("save profile metadata: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Warning: failed to save profile metadata: %v\n", err)
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			// Propagate the actual exit code.
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("run command: %w", runErr)
	}

	return nil
}

// RunInteractive runs an interactive session with the AI CLI.
func (r *Runner) RunInteractive(ctx context.Context, opts RunOptions) error {
	return r.Run(ctx, opts)
}

// LoginFlow runs the login flow for a profile.
func (r *Runner) LoginFlow(ctx context.Context, prov provider.Provider, prof *profile.Profile) error {
	return prov.Login(ctx, prof)
}

// Status checks the authentication status of a profile.
func (r *Runner) Status(ctx context.Context, prov provider.Provider, prof *profile.Profile) (*provider.ProfileStatus, error) {
	return prov.Status(ctx, prof)
}
