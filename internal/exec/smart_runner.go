package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authpool"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/handoff"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/notify"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	codexprovider "github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider/codex"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/pty"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/ratelimit"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
	"golang.org/x/term"
)

// ExecCommand allows mocking exec.CommandContext in tests
var ExecCommand = exec.CommandContext

// SmartRunner orchestrates the auto-handoff flow for seamless profile switching.
// When a rate limit is detected in the CLI output, SmartRunner:
// 1. Selects the best backup profile using the rotation algorithm
// 2. Swaps auth files atomically
// 3. Injects the login command via PTY
// 4. Waits for login completion
// 5. Notifies the user and continues execution
//
// On any failure, it rolls back to the original profile and shows manual instructions.
type SmartRunner struct {
	*Runner

	detector      *ratelimit.Detector
	rotation      *rotation.Selector
	vault         *authfile.Vault
	db            *caamdb.DB
	authPool      *authpool.AuthPool
	ptyController pty.Controller
	loginHandler  handoff.LoginHandler
	handoffConfig *config.HandoffConfig
	notifier      notify.Notifier

	// Cooldown duration to apply when rate limit is detected
	cooldownDuration time.Duration

	// State (protected by mu)
	mu                sync.Mutex
	providerID        string
	currentProfile    string
	previousProfile   string // For rollback
	handoffCount      int
	maxHandoffs       int
	lastSessionID     string
	lastResumeCommand string
	authSwapped       bool // True after auth files are successfully restored to a backup profile
	exhaustedProfiles map[string]struct{}
	state             HandoffState
	// Suppress duplicate rate-limit redispatch while stale buffered output from
	// the just-interrupted session is still draining.
	rateLimitDispatchPausedUntil time.Time
	// Seamless resume state for codex auto-handoff.
	seamlessResumePending bool
	seamlessResumeHint    string
	seamlessResumeArmedAt time.Time
	seamlessResumeOutput  bool

	// WaitGroup to track background goroutines (handleRateLimit)
	wg sync.WaitGroup

	// Login detection channel
	loginDone chan loginResult
}

// loginResult captures the outcome of a login attempt.
type loginResult struct {
	success bool
	message string
}

const seamlessResumeDepthEnv = "CAAM_SEAMLESS_RESUME_DEPTH"
const maxSeamlessResumeDepth = 1
const seamlessResumeMaxDelay = 20 * time.Second
const defaultPostStartDelay = 1200 * time.Millisecond
const defaultSeamlessResumePrompt = "Continue exactly where you left off before the rate-limit interruption."

// SmartRunnerOptions configures the SmartRunner.
type SmartRunnerOptions struct {
	HandoffConfig    *config.HandoffConfig
	Notifier         notify.Notifier
	Vault            *authfile.Vault
	DB               *caamdb.DB
	AuthPool         *authpool.AuthPool
	Rotation         *rotation.Selector
	CooldownDuration time.Duration
}

// NewSmartRunner creates a new SmartRunner.
func NewSmartRunner(runner *Runner, opts SmartRunnerOptions) *SmartRunner {
	// Use default notifier if none provided
	notifier := opts.Notifier
	if notifier == nil {
		notifier = notify.NewTerminalNotifier(os.Stderr, true)
	}

	return &SmartRunner{
		Runner:            runner,
		vault:             opts.Vault,
		db:                opts.DB,
		authPool:          opts.AuthPool,
		rotation:          opts.Rotation,
		handoffConfig:     opts.HandoffConfig,
		notifier:          notifier,
		cooldownDuration:  opts.CooldownDuration,
		maxHandoffs:       configuredMaxHandoffs(opts.HandoffConfig),
		state:             Running,
		exhaustedProfiles: make(map[string]struct{}),
		loginDone:         make(chan loginResult, 1),
	}
}

// Run executes the command with smart handoff capabilities.
func (r *SmartRunner) Run(ctx context.Context, opts RunOptions) (err error) {
	return r.run(ctx, opts, false)
}

func (r *SmartRunner) run(ctx context.Context, opts RunOptions, preserveHandoffState bool) (err error) {
	// Initialize rate limit detector
	detector, err := ratelimit.NewDetector(
		ratelimit.ProviderFromString(opts.Provider.ID()),
		nil, // Use default patterns
	)
	if err != nil {
		return fmt.Errorf("create detector: %w", err)
	}
	r.detector = detector

	// Get login handler
	r.loginHandler = handoff.GetHandler(opts.Provider.ID())
	if r.loginHandler == nil {
		// Fallback to basic runner if no login handler (can't do handoff)
		return r.Runner.Run(ctx, opts)
	}

	r.mu.Lock()
	r.providerID = opts.Provider.ID()
	r.currentProfile = opts.Profile.Name
	r.previousProfile = ""
	if !preserveHandoffState {
		r.handoffCount = 0
		r.exhaustedProfiles = make(map[string]struct{})
	}
	r.authSwapped = false
	r.state = Running
	r.rateLimitDispatchPausedUntil = time.Time{}
	r.seamlessResumePending = false
	r.seamlessResumeHint = ""
	r.seamlessResumeArmedAt = time.Time{}
	r.seamlessResumeOutput = false
	r.maxHandoffs = configuredMaxHandoffs(r.handoffConfig)
	r.lastSessionID = strings.TrimSpace(opts.Profile.LastSessionID)
	if r.providerID == "codex" && r.lastSessionID != "" {
		r.lastResumeCommand = fmt.Sprintf("codex resume %s", r.lastSessionID)
	} else {
		r.lastResumeCommand = ""
	}
	r.mu.Unlock()

	if r.vault != nil {
		if profiles, listErr := r.vault.List(opts.Provider.ID()); listErr == nil {
			r.maybeExpandMaxHandoffs(profiles)
		}
	}

	// Log activation event
	if r.db != nil {
		_ = r.db.Log(caamdb.Event{
			Type:        caamdb.EventActivate,
			Provider:    opts.Provider.ID(),
			ProfileName: r.currentProfile,
			Timestamp:   time.Now(),
		})
	}

	// Track session
	startTime := time.Now()
	defer func() {
		if r.db != nil {
			duration := time.Since(startTime)
			// Determine final exit code from error
			finalCode := 0
			if err != nil {
				var exitErr *ExitCodeError
				// Check if it's an ExitCodeError (wrapper type in this package)
				if errors.As(err, &exitErr) {
					finalCode = exitErr.Code
				} else {
					finalCode = 1 // Generic error
				}
			}

			session := caamdb.WrapSession{
				Provider:        opts.Provider.ID(),
				ProfileName:     r.currentProfile, // Use the final profile
				StartedAt:       startTime,
				EndedAt:         time.Now(),
				DurationSeconds: int(duration.Seconds()),
				ExitCode:        finalCode,
				RateLimitHit:    r.handoffCount > 0,
			}
			if r.handoffCount > 0 {
				session.Notes = fmt.Sprintf("handoffs: %d", r.handoffCount)
			}
			_ = r.db.RecordWrapSession(session)
		}
	}()

	// Lock profile
	if !opts.NoLock {
		if err := opts.Profile.LockWithCleanup(); err != nil {
			return fmt.Errorf("lock profile: %w", err)
		}
		defer opts.Profile.Unlock()
	}

	// Get env
	var providerEnv map[string]string
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
	cmd := ExecCommand(ctx, bin, opts.Args...)

	// Apply env (same as Runner.Run)
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		parts := splitEnv(e)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for k, v := range providerEnv {
		envMap[k] = v
	}
	for k, v := range opts.Env {
		envMap[k] = v
	}
	cmd.Env = make([]string, 0, len(envMap))
	for k, v := range envMap {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	// Create PTY controller
	ctrl, err := pty.NewController(cmd, currentPTYOptions())
	if err != nil {
		return fmt.Errorf("create pty controller: %w", err)
	}
	defer ctrl.Close()

	// Start the PTY (this executes the command)
	if err := ctrl.Start(); err != nil {
		return fmt.Errorf("start pty: %w", err)
	}
	r.setPTYController(ctrl)
	defer r.setPTYController(nil)

	if !preserveHandoffState {
		restoreInput := r.startInteractiveInputRelay(ctx)
		defer func() {
			if restoreInput != nil {
				restoreInput()
			}
		}()
	}

	var capture *codexSessionCapture
	if opts.Provider.ID() == "codex" || opts.Provider.ID() == "claude" {
		capture = &codexSessionCapture{provider: opts.Provider.ID()}
	}

	// Start output monitoring in background
	monitorCtx, cancelMonitor := context.WithCancel(ctx)
	defer cancelMonitor()
	monitorDone := make(chan struct{})

	var observer func(string)
	if capture != nil {
		observer = func(line string) {
			capture.ObserveLine(line)
			r.updateResumeHint(capture.Command(), capture.ID())
		}
	}
	go r.monitorOutput(monitorCtx, ctrl, monitorDone, observer)
	r.maybeSchedulePostStartCommand(ctx, ctrl, opts.PostStartCommand, opts.PostStartDelay)

	// Wait for command completion using the controller's Wait method
	exitCode, waitErr := ctrl.Wait()

	// Cancel monitor context, wait for monitor to stop, then wait for any handoff goroutines.
	cancelMonitor()
	<-monitorDone
	r.wg.Wait()

	// Update profile metadata
	now := time.Now()
	opts.Profile.LastUsedAt = now
	if capture != nil {
		if sessionID := capture.ID(); sessionID != "" && opts.Provider.ID() == "codex" {
			opts.Profile.LastSessionID = sessionID
			opts.Profile.LastSessionTS = now.UTC()
		}
	}
	if saveErr := opts.Profile.Save(); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save profile metadata: %v\n", saveErr)
	}

	if resumed, resumeErr := r.maybeRunSeamlessResume(ctx, opts, exitCode, waitErr); resumed {
		return resumeErr
	}

	if waitErr != nil {
		return fmt.Errorf("command failed: %w", waitErr)
	}
	if exitCode != 0 {
		return &ExitCodeError{Code: exitCode}
	}
	if r.getState() == HandoffFailed {
		return fmt.Errorf("auto-handoff failed; see stderr for recovery steps")
	}

	return nil
}

// handleRateLimit handles the rate limit detection and handoff flow.
func (r *SmartRunner) handleRateLimit(ctx context.Context) {
	reason := strings.TrimSpace(r.detector.Reason())
	authRefreshFailure := r.loginHandler.Provider() == "codex" && isCodexReauthRequiredReason(reason)
	cooldownDuration := r.cooldownDuration
	if cooldownDuration == 0 {
		cooldownDuration = 60 * time.Minute
	}
	cooldownNote := "auto-detected via SmartRunner"
	if authRefreshFailure {
		// Longer cooldown for invalid refresh tokens to avoid repeatedly selecting
		// a profile that requires manual re-authentication.
		if cooldownDuration < 24*time.Hour {
			cooldownDuration = 24 * time.Hour
		}
		cooldownNote = "auth refresh failed; re-login required"
	}
	cooldownApplied := false
	applyCurrentCooldown := func() {
		if cooldownApplied {
			return
		}
		if r.authPool != nil {
			r.authPool.SetCooldown(r.loginHandler.Provider(), r.currentProfile, cooldownDuration)
		}
		if r.db != nil {
			r.db.SetCooldown(r.loginHandler.Provider(), r.currentProfile, time.Now(), cooldownDuration, cooldownNote)
		}
		cooldownApplied = true
	}

	r.mu.Lock()
	if r.state != Running {
		r.mu.Unlock()
		return // Already handling or failed
	}
	// Respect configured max handoff attempts per session.
	if r.maxHandoffs >= 0 && r.handoffCount >= r.maxHandoffs {
		maxRetries := r.maxHandoffs
		r.mu.Unlock()
		r.failWithManual("maximum profile switches reached for this session (%d)", maxRetries)
		return
	}
	r.state = RateLimited
	r.authSwapped = false
	if profile := strings.TrimSpace(r.currentProfile); profile != "" {
		r.exhaustedProfiles[profile] = struct{}{}
	}
	r.mu.Unlock()

	// Notify detection
	r.notifyHandoff(r.currentProfile, "", "Rate limit detected, selecting backup profile...")
	if authRefreshFailure {
		// Immediately quarantine auth-invalid profiles, even if no handoff is possible.
		applyCurrentCooldown()
	}

	// Get file set
	fileSet, ok := authfile.GetAuthFileSet(r.loginHandler.Provider())
	if !ok {
		r.failWithManual("unknown provider file set")
		return
	}

	// 1. Save current state for rollback
	r.previousProfile = r.currentProfile
	if err := r.vault.Backup(fileSet, r.currentProfile); err != nil {
		r.failWithManual("failed to backup current profile: %v", err)
		return
	}

	defer func() {
		// If auth was already swapped, keep the new profile active instead of
		// rolling back to the exhausted one. This prevents getting stuck on A
		// after a partial handoff failure.
		if r.getState() == HandoffFailed && !r.hasAuthSwapped() {
			r.rollback(fileSet)
		}
	}()

	// 2. Select best backup profile
	r.setState(SelectingBackup)

	// Get all profiles
	profiles, err := r.vault.List(r.loginHandler.Provider())
	if err != nil {
		r.failWithManual("failed to list profiles: %v", err)
		return
	}
	basePool := profiles
	if provider := r.loginHandler.Provider(); provider == "codex" || provider == "claude" {
		healthyProfiles, allUnavailable := r.healthySelectionPool(ctx, provider, profiles, 0.8)
		if allUnavailable {
			if authRefreshFailure {
				r.failWithManual(
					"current profile %s needs re-login (refresh token invalid), and all other %s profiles are unavailable or out of credits",
					r.currentProfile,
					provider,
				)
				return
			}
			if provider == "codex" {
				r.failWithManual("all %s profiles are unavailable or out of credits", provider)
			} else {
				r.failWithManual("all %s profiles are near limit or unavailable", provider)
			}
			return
		}
		if len(healthyProfiles) > 0 {
			basePool = healthyProfiles
		}
	}
	preferredPool := preferUserProfiles(basePool)
	selectionPool, aliasOnly := r.filterSelectionPool(r.loginHandler.Provider(), r.currentProfile, preferredPool)

	// If user-named profiles are exhausted/unusable, fall back to system profiles
	// (e.g. _backup_*) so we still have a deterministic recovery path.
	if len(selectionPool) == 0 && len(preferredPool) < len(basePool) {
		fallbackPool, fallbackAliasOnly := r.filterSelectionPool(r.loginHandler.Provider(), r.currentProfile, basePool)
		if len(fallbackPool) > 0 {
			selectionPool = fallbackPool
			aliasOnly = false
		} else if fallbackAliasOnly {
			aliasOnly = true
		}
	}

	if aliasOnly {
		r.failWithManual(
			"all %s alternatives resolve to the same credentials as %s; fix profile identity drift first",
			r.loginHandler.Provider(),
			r.currentProfile,
		)
		return
	}
	if len(selectionPool) == 0 {
		if r.loginHandler.Provider() == "codex" {
			r.failWithManual("all %s profiles are exhausted, in cooldown, locked, or out of credits", r.loginHandler.Provider())
		} else {
			r.failWithManual("all %s profiles are exhausted, in cooldown, locked, or near limit", r.loginHandler.Provider())
		}
		return
	}

	// Select best
	var nextProfile string
	if allSystemProfiles(selectionPool) {
		selectionPool = r.excludeActiveCooldownProfiles(r.loginHandler.Provider(), selectionPool)
		if len(selectionPool) == 0 {
			r.failWithManual("all %s profiles are exhausted, in cooldown, locked, or unavailable", r.loginHandler.Provider())
			return
		}
		nextProfile = selectDeterministicSystemFallback(selectionPool)
	} else {
		selection, err := r.rotation.Select(r.loginHandler.Provider(), selectionPool, r.currentProfile)
		if err != nil {
			r.failWithManual("no backup available: %v", err)
			return
		}
		nextProfile = selection.Selected
	}

	if nextProfile == r.currentProfile {
		r.failWithManual("no other profiles available")
		return
	}

	r.notifyHandoff(r.currentProfile, nextProfile)

	// 3. Mark current profile as in cooldown (if authPool is available)
	applyCurrentCooldown()

	// 4. Swap auth files
	r.setState(SwappingAuth)
	if r.loginHandler != nil && r.loginHandler.Provider() == "codex" {
		if err := codexprovider.EnsureFileCredentialStore(codexprovider.ResolveHome()); err != nil {
			r.failWithManual("codex credential store prep failed: %v", err)
			return
		}
	}
	if err := r.vault.Restore(fileSet, nextProfile); err != nil {
		r.failWithManual("auth swap failed: %v", err)
		return
	}
	r.mu.Lock()
	r.currentProfile = nextProfile
	r.authSwapped = true
	r.mu.Unlock()

	// 5. For Codex usage-limit handoffs we can continue without forcing interactive
	// re-login. Trigger login only when auth refresh is explicitly invalid.
	if shouldSkipInteractiveLogin(r.loginHandler.Provider(), reason) {
		r.completeHandoff(nextProfile, true)
		return
	}

	// 6. Inject login command
	r.drainLoginDone()
	r.setState(LoggingIn)
	if err := r.loginHandler.TriggerLogin(r.currentPTYController()); err != nil {
		r.failWithManual("login trigger failed: %v", err)
		return
	}

	// 7. Wait for login completion (monitorOutput detects success/failure and signals via loginDone)
	loginTimeout := 30 * time.Second
	if r.handoffConfig != nil && r.handoffConfig.DebounceDelay.Duration() > 0 {
		loginTimeout = r.handoffConfig.DebounceDelay.Duration() * 10 // 10x debounce as timeout
		if loginTimeout < 30*time.Second {
			loginTimeout = 30 * time.Second
		}
	}

	select {
	case result := <-r.loginDone:
		if !result.success {
			r.failWithManual("login failed: %s", result.message)
			return
		}
	case <-time.After(loginTimeout):
		r.failWithManual("login timed out after %v", loginTimeout)
		return
	case <-ctx.Done():
		r.failWithManual("context cancelled during login")
		return
	}

	// 8. Success!
	r.completeHandoff(nextProfile, false)
}

func isCodexReauthRequiredReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	needles := []string{
		"access token could not be refreshed",
		"refresh token was already used",
		"refresh_token_reused",
		"invalid_grant",
		"token refresh",
		"token expired or invalid",
	}
	for _, needle := range needles {
		if strings.Contains(reason, needle) {
			return true
		}
	}
	return false
}

func shouldSkipInteractiveLogin(provider, reason string) bool {
	if provider != "codex" {
		return false
	}
	return !isCodexReauthRequiredReason(reason)
}

func (r *SmartRunner) completeHandoff(nextProfile string, loginSkipped bool) {
	r.setState(LoginComplete)
	r.mu.Lock()
	r.currentProfile = nextProfile
	r.handoffCount++
	r.mu.Unlock()

	resumeHint := r.resumeHint(nextProfile)

	successMsg := fmt.Sprintf("Switched to %s. Continue working.", nextProfile)
	if loginSkipped {
		successMsg = fmt.Sprintf("Switched to %s via stored credentials (no re-login needed). Continue working.", nextProfile)
	}
	if resumeHint != "" {
		successMsg = successMsg + " Resume hint: " + resumeHint
	}

	r.notifier.Notify(&notify.Alert{
		Level:   notify.Info,
		Title:   "Profile switched",
		Message: successMsg,
	})
	if resumeHint != "" {
		fmt.Fprintf(os.Stderr, "[caam] Resume hint: %s\n", resumeHint)
	}
	if loginSkipped {
		// When we skip interactive login, any remaining PTY output before the
		// interrupted process fully exits still belongs to the exhausted session.
		// Keep redispatch paused for the full seamless-resume arming window so
		// stale buffered rate-limit lines cannot trigger a second handoff on the
		// already-switched profile.
		r.pauseRateLimitDispatch(seamlessResumeMaxDelay)
		r.maybeMarkSeamlessResumePending(resumeHint)
	}

	// Reset detector state so we don't immediately trigger again
	r.detector.Reset()
	r.setState(Running)
}

func (r *SmartRunner) rollback(fileSet authfile.AuthFileSet) {
	fmt.Fprintf(os.Stderr, "Rolling back to %s...\n", r.previousProfile)
	if r.loginHandler != nil && r.loginHandler.Provider() == "codex" {
		if err := codexprovider.EnsureFileCredentialStore(codexprovider.ResolveHome()); err != nil {
			fmt.Fprintf(os.Stderr, "Rollback prep failed: %v\n", err)
			return
		}
	}
	if err := r.vault.Restore(fileSet, r.previousProfile); err != nil {
		fmt.Fprintf(os.Stderr, "Rollback failed: %v\n", err)
	}
	r.currentProfile = r.previousProfile
	r.detector.Reset()
	r.setState(Running)
}

func (r *SmartRunner) failWithManual(format string, args ...interface{}) {
	r.setState(HandoffFailed)
	msg := fmt.Sprintf(format, args...)
	resumeHint := r.resumeHint("")
	currentProfile := ""
	swapped := false
	r.mu.Lock()
	currentProfile = strings.TrimSpace(r.currentProfile)
	swapped = r.authSwapped
	r.mu.Unlock()

	action := "Run 'caam ls' to see available profiles, then 'caam activate <tool> <profile>'"
	lowerMsg := strings.ToLower(msg)
	if strings.Contains(lowerMsg, "re-login") || strings.Contains(lowerMsg, "refresh token invalid") {
		if providerID := strings.TrimSpace(r.providerID); providerID != "" && currentProfile != "" {
			action = fmt.Sprintf("Run 'caam login %s %s' to re-authenticate this profile, then retry.", providerID, currentProfile)
		}
	}
	if swapped && currentProfile != "" {
		action = fmt.Sprintf("Profile already switched to '%s'. Restart the session (or resume) with this profile.", currentProfile)
	}
	if resumeHint != "" {
		action = strings.TrimSuffix(action, ".") + ". Resume with: " + resumeHint
	}

	r.notifier.Notify(&notify.Alert{
		Level:   notify.Warning,
		Title:   "Auto-handoff failed",
		Message: msg,
		Action:  action,
	})

	fmt.Fprintf(os.Stderr, "\n[caam] Auto-handoff failed: %s\n", msg)
	if swapped && currentProfile != "" {
		fmt.Fprintf(os.Stderr, "[caam] Active profile preserved: %s\n", currentProfile)
	}
	if resumeHint != "" {
		fmt.Fprintf(os.Stderr, "[caam] Resume hint: %s\n", resumeHint)
	}

	// End the current interactive session when auto-handoff fails so the user
	// can immediately resume with the suggested command instead of staying stuck.
	r.terminateCurrentSession()
}

func (r *SmartRunner) notifyHandoff(from, to string, msg ...string) {
	message := fmt.Sprintf("Rate limit on %s, switching to %s...", from, to)
	if len(msg) > 0 {
		message = msg[0]
	}
	r.notifier.Notify(&notify.Alert{
		Level:   notify.Info,
		Title:   "Switching profiles",
		Message: message,
	})
}

func (r *SmartRunner) setState(s HandoffState) {
	r.mu.Lock()
	r.state = s
	r.mu.Unlock()
}

func (r *SmartRunner) getState() HandoffState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *SmartRunner) drainLoginDone() {
	for {
		select {
		case <-r.loginDone:
			continue
		default:
			return
		}
	}
}

func (r *SmartRunner) pauseRateLimitDispatch(duration time.Duration) {
	if duration <= 0 {
		return
	}
	r.mu.Lock()
	r.rateLimitDispatchPausedUntil = time.Now().Add(duration)
	r.mu.Unlock()
}

func (r *SmartRunner) rateLimitDispatchPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.rateLimitDispatchPausedUntil.IsZero() && time.Now().Before(r.rateLimitDispatchPausedUntil)
}

func (r *SmartRunner) monitorOutput(ctx context.Context, ctrl pty.Controller, done chan<- struct{}, observer func(string)) {
	defer close(done)
	// Create an observing writer to handle split packets and buffering
	// Use a local flag to prevent repeated dispatching within this loop context
	dispatched := false
	pollTicker := time.NewTicker(10 * time.Millisecond)
	defer pollTicker.Stop()

	var lineObserver *lineObserverWriter
	if observer != nil {
		lineObserver = newLineObserverWriter(io.Discard, observer)
		defer lineObserver.Flush()
	}

	writer := ratelimit.NewObservingWriter(r.detector, func(line string) {
		// This callback is triggered when a complete line is processed
		if !dispatched && r.detector.Detected() {
			if r.rateLimitDispatchPaused() {
				r.detector.Reset()
				return
			}
			dispatched = true
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.handleRateLimit(ctx)
			}()
		}
	})
	defer writer.Flush()

	for {
		// Poll for output (ReadOutput is non-blocking with timeout)
		output, err := ctrl.ReadOutput()
		if err != nil {
			// If the wrapped process ended while we were waiting for login completion,
			// unblock handleRateLimit immediately instead of timing out after 30s.
			if r.getState() == LoggingIn {
				msg := "session ended before login completed"
				if !errors.Is(err, io.EOF) {
					msg = fmt.Sprintf("session ended before login completed: %v", err)
				}
				select {
				case r.loginDone <- loginResult{success: false, message: msg}:
				default:
				}
			}
			break
		}

		if output != "" {
			os.Stdout.Write([]byte(output))
			if lineObserver != nil {
				_, _ = lineObserver.Write([]byte(output))
			}

			r.mu.Lock()
			state := r.state
			r.mu.Unlock()

			if state == Running {
				r.noteSeamlessResumeOutput(output)
				// If detector was reset (e.g. after successful handoff), allow new dispatch
				if !r.detector.Detected() {
					dispatched = false
				}

				writer.Write([]byte(output))
			} else if state == LoggingIn {
				// Check for login completion and signal handleRateLimit
				if r.loginHandler.IsLoginComplete(output) {
					select {
					case r.loginDone <- loginResult{success: true}:
					default:
						// Channel already has a value
					}
				} else if failed, msg := r.loginHandler.IsLoginFailed(output); failed {
					select {
					case r.loginDone <- loginResult{success: false, message: msg}:
					default:
						// Channel already has a value
					}
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			// Yield
		}
	}
}

func splitEnv(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func currentPTYOptions() *pty.Options {
	opts := pty.DefaultOptions()
	if cols, rows, ok := currentTerminalSize(); ok {
		opts.Cols = uint16(cols)
		opts.Rows = uint16(rows)
	}
	opts.DisableEcho = true
	return opts
}

func currentTerminalSize() (cols, rows int, ok bool) {
	for _, file := range []*os.File{os.Stdout, os.Stdin} {
		if file == nil {
			continue
		}
		fd := int(file.Fd())
		if !term.IsTerminal(fd) {
			continue
		}
		width, height, err := term.GetSize(fd)
		if err != nil || width <= 0 || height <= 0 {
			continue
		}
		return width, height, true
	}
	return 0, 0, false
}

func (r *SmartRunner) startInteractiveInputRelay(ctx context.Context) func() {
	stdin := os.Stdin
	if stdin == nil {
		return nil
	}

	relayInput := stdin
	closeRelayInput := func() {}
	if duplicated, closeDup, err := duplicateInputSource(stdin); err != nil {
		fmt.Fprintf(os.Stderr, "[caam] Warning: failed to duplicate terminal input source: %v\n", err)
	} else if duplicated != nil {
		relayInput = duplicated
		closeRelayInput = closeDup
	}

	restoreTerminal := func() {}
	fd := int(stdin.Fd())
	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[caam] Warning: failed to enable raw terminal input: %v\n", err)
		} else {
			restoreTerminal = func() {
				_ = term.Restore(fd, oldState)
			}
		}
	}

	stopRelay := make(chan struct{})
	done := make(chan struct{})
	var cleanupOnce sync.Once

	go func() {
		defer close(done)
		defer cleanupOnce.Do(closeRelayInput)
		buf := make([]byte, 4096)
		for {
			n, err := relayInput.Read(buf)
			if n > 0 {
				select {
				case <-stopRelay:
					return
				default:
				}
				ctrl := r.currentPTYController()
				if ctrl != nil {
					data := append([]byte(nil), buf[:n]...)
					if injectErr := ctrl.InjectRaw(data); injectErr != nil && !errors.Is(injectErr, pty.ErrClosed) {
						fmt.Fprintf(os.Stderr, "[caam] Warning: failed to forward terminal input: %v\n", injectErr)
					}
				}
			}
			if err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-stopRelay:
				return
			default:
			}
		}
	}()

	return func() {
		cleanupOnce.Do(func() {
			close(stopRelay)
			closeRelayInput()
			restoreTerminal()
		})
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (r *SmartRunner) setPTYController(ctrl pty.Controller) {
	r.mu.Lock()
	r.ptyController = ctrl
	r.mu.Unlock()
}

func (r *SmartRunner) currentPTYController() pty.Controller {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ptyController
}

func (r *SmartRunner) updateResumeHint(command, sessionID string) {
	command = strings.TrimSpace(command)
	sessionID = strings.TrimSpace(sessionID)
	if command == "" && sessionID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if command != "" {
		r.lastResumeCommand = command
	}
	if sessionID != "" {
		r.lastSessionID = sessionID
	}
}

func (r *SmartRunner) resumeHint(nextProfile string) string {
	r.mu.Lock()
	providerID := strings.TrimSpace(r.providerID)
	sessionID := strings.TrimSpace(r.lastSessionID)
	command := strings.TrimSpace(r.lastResumeCommand)
	currentProfile := strings.TrimSpace(r.currentProfile)
	r.mu.Unlock()

	// Prefer the exact command captured from CLI output.
	if command != "" {
		return command
	}

	// Fallback to caam-native resume command when codex session ID is available.
	if providerID == "codex" && sessionID != "" {
		profileName := strings.TrimSpace(nextProfile)
		if profileName == "" {
			profileName = currentProfile
		}
		if profileName != "" {
			return fmt.Sprintf("caam resume codex %s --session %s", profileName, sessionID)
		}
	}

	return ""
}

func (r *SmartRunner) terminateCurrentSession() {
	ctrl := r.currentPTYController()
	if ctrl == nil {
		return
	}
	// Try signal first; fall back to Ctrl+C byte injection for shells/CLIs
	// that only react to terminal control characters.
	if err := ctrl.Signal(pty.SIGINT); err != nil {
		_ = ctrl.InjectRaw([]byte{3})
	}
}

func (r *SmartRunner) hasAuthSwapped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.authSwapped
}

func (r *SmartRunner) maybeMarkSeamlessResumePending(hint string) {
	hint = strings.TrimSpace(hint)
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(r.providerID) != "codex" {
		return
	}
	if hint == "" && strings.TrimSpace(r.lastSessionID) == "" {
		return
	}
	r.seamlessResumePending = true
	r.seamlessResumeHint = hint
	r.seamlessResumeArmedAt = time.Now()
	r.seamlessResumeOutput = false
}

func (r *SmartRunner) maybeSchedulePostStartCommand(ctx context.Context, ctrl pty.Controller, command string, delay time.Duration) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	if delay <= 0 {
		delay = defaultPostStartDelay
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if err := ctrl.InjectCommand(command); err != nil {
			if errors.Is(err, pty.ErrClosed) {
				return
			}
			fmt.Fprintf(os.Stderr, "[caam] Warning: failed to inject post-start command: %v\n", err)
			return
		}

		fmt.Fprintf(os.Stderr, "[caam] Injected continuation prompt after seamless resume.\n")
	}()
}

func isInterruptExitCode(code int) bool {
	return code == 130 || code == 143
}

func (r *SmartRunner) noteSeamlessResumeOutput(output string) {
	if strings.TrimSpace(output) == "" {
		return
	}
	r.mu.Lock()
	if r.seamlessResumePending {
		if !r.rateLimitDispatchPausedUntil.IsZero() && time.Now().Before(r.rateLimitDispatchPausedUntil) {
			r.mu.Unlock()
			return
		}
		r.seamlessResumeOutput = true
	}
	r.mu.Unlock()
}

func seamlessResumeDepth(opts RunOptions) int {
	if opts.Env != nil {
		if raw := strings.TrimSpace(opts.Env[seamlessResumeDepthEnv]); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
				return n
			}
		}
	}
	if raw := strings.TrimSpace(os.Getenv(seamlessResumeDepthEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

func resumeArgsFromHint(provider, hint, fallbackSession string) ([]string, bool) {
	hint = strings.TrimSpace(hint)
	if hint != "" {
		match := strings.TrimSpace(providerResumeCommandRe.FindString(hint))
		if match != "" {
			fields, err := splitCommandLine(match)
			if err == nil && len(fields) >= 2 && strings.EqualFold(fields[0], provider) && strings.EqualFold(fields[1], "resume") {
				return append([]string(nil), fields[1:]...), true
			}
		}
	}
	if strings.EqualFold(provider, "codex") && strings.TrimSpace(fallbackSession) != "" {
		return []string{"resume", strings.TrimSpace(fallbackSession)}, true
	}
	return nil, false
}

func resumeHasPrompt(args []string, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return len(args) > 2
	}
	for i := 1; i < len(args); i++ {
		if strings.TrimSpace(args[i]) == sessionID {
			return i < len(args)-1
		}
	}
	return len(args) > 2
}

func cloneProfileForSwitch(current *profile.Profile, targetName string) *profile.Profile {
	if current == nil {
		return nil
	}
	copied := *current
	copied.Name = targetName
	if current.BasePath != "" {
		parent := filepath.Dir(current.BasePath)
		copied.BasePath = filepath.Join(parent, targetName)
	}
	return &copied
}

func (r *SmartRunner) maybeRunSeamlessResume(ctx context.Context, opts RunOptions, exitCode int, waitErr error) (bool, error) {
	if waitErr != nil {
		return false, nil
	}
	if isInterruptExitCode(exitCode) || ctx.Err() != nil {
		return false, nil
	}
	if r.getState() == HandoffFailed {
		return false, nil
	}
	if seamlessResumeDepth(opts) >= maxSeamlessResumeDepth {
		return false, nil
	}

	r.mu.Lock()
	pending := r.seamlessResumePending
	hint := strings.TrimSpace(r.seamlessResumeHint)
	providerID := strings.TrimSpace(r.providerID)
	currentProfile := strings.TrimSpace(r.currentProfile)
	lastSessionID := strings.TrimSpace(r.lastSessionID)
	armedAt := r.seamlessResumeArmedAt
	outputSeen := r.seamlessResumeOutput
	r.seamlessResumePending = false
	r.seamlessResumeHint = ""
	r.seamlessResumeArmedAt = time.Time{}
	r.seamlessResumeOutput = false
	r.mu.Unlock()

	if !pending || providerID != "codex" {
		return false, nil
	}
	if armedAt.IsZero() || time.Since(armedAt) > seamlessResumeMaxDelay {
		return false, nil
	}
	if outputSeen {
		return false, nil
	}

	resumeArgs, ok := resumeArgsFromHint(providerID, hint, lastSessionID)
	if !ok {
		return false, nil
	}

	resumeProfile := cloneProfileForSwitch(opts.Profile, currentProfile)
	if resumeProfile == nil || strings.TrimSpace(resumeProfile.Name) == "" {
		return false, nil
	}

	resumeOpts := opts
	resumeOpts.Profile = resumeProfile
	resumeOpts.Args = append([]string(nil), resumeArgs...)
	if !resumeHasPrompt(resumeArgs, extractResumeSessionID(append([]string{providerID}, resumeArgs...))) {
		resumeOpts.PostStartCommand = defaultSeamlessResumePrompt
		resumeOpts.PostStartDelay = defaultPostStartDelay
	}
	resumeOpts.NoLock = true
	if resumeOpts.Env == nil {
		resumeOpts.Env = map[string]string{}
	}
	resumeOpts.Env[seamlessResumeDepthEnv] = strconv.Itoa(seamlessResumeDepth(opts) + 1)

	fmt.Fprintf(os.Stderr, "[caam] Session hit a hard rate-limit exit; auto-resuming on profile %s...\n", resumeProfile.Name)

	return true, r.run(ctx, resumeOpts, true)
}

func configuredMaxHandoffs(cfg *config.HandoffConfig) int {
	if cfg == nil {
		return -1
	}
	return cfg.MaxRetries
}

func autoMaxHandoffs(profiles []string) int {
	count := 0
	for _, profile := range profiles {
		if authfile.IsSystemProfile(profile) {
			continue
		}
		count++
	}
	if count <= 1 {
		return 0
	}
	return count - 1
}

func (r *SmartRunner) maybeExpandMaxHandoffs(profiles []string) {
	suggested := autoMaxHandoffs(profiles)
	if suggested <= 0 {
		return
	}
	r.mu.Lock()
	if r.maxHandoffs >= 0 && r.maxHandoffs < suggested {
		r.maxHandoffs = suggested
	}
	r.mu.Unlock()
}

func (r *SmartRunner) excludeExhaustedProfiles(profiles []string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if _, exhausted := r.exhaustedProfiles[profile]; exhausted {
			continue
		}
		filtered = append(filtered, profile)
	}
	return filtered
}

func preferUserProfiles(profiles []string) []string {
	userProfiles := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if authfile.IsSystemProfile(profile) {
			continue
		}
		userProfiles = append(userProfiles, profile)
	}
	if len(userProfiles) > 0 {
		return userProfiles
	}
	return profiles
}

func allSystemProfiles(profiles []string) bool {
	if len(profiles) == 0 {
		return false
	}
	for _, profile := range profiles {
		if !authfile.IsSystemProfile(profile) {
			return false
		}
	}
	return true
}

func selectDeterministicSystemFallback(profiles []string) string {
	if len(profiles) == 0 {
		return ""
	}
	sorted := append([]string(nil), profiles...)
	sort.Strings(sorted)
	return sorted[0]
}

func (r *SmartRunner) excludeSharedCredentialAliases(provider, currentProfile string, profiles []string) []string {
	if provider != "codex" && provider != "claude" {
		return profiles
	}
	if strings.TrimSpace(currentProfile) == "" || len(profiles) == 0 {
		return profiles
	}

	credentials, err := r.loadProviderCredentials(provider)
	if err != nil || len(credentials) == 0 {
		return profiles
	}

	currentToken := strings.TrimSpace(credentials[currentProfile])
	if currentToken == "" {
		return profiles
	}

	filtered := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile == currentProfile {
			continue
		}
		token := strings.TrimSpace(credentials[profile])
		if token != "" && token == currentToken {
			continue
		}
		filtered = append(filtered, profile)
	}
	return filtered
}

func (r *SmartRunner) filterSelectionPool(provider, currentProfile string, profiles []string) ([]string, bool) {
	pool := r.excludeExhaustedProfiles(profiles)
	beforeAliasFilter := len(pool)
	pool = r.excludeSharedCredentialAliases(provider, currentProfile, pool)
	aliasOnly := beforeAliasFilter > 0 && len(pool) == 0
	return pool, aliasOnly
}

func (r *SmartRunner) excludeActiveCooldownProfiles(provider string, profiles []string) []string {
	if len(profiles) == 0 {
		return profiles
	}

	inCooldown := make(map[string]struct{}, len(profiles))
	if r.authPool != nil {
		for _, profile := range r.authPool.GetProfilesInCooldown(provider) {
			if profile == nil {
				continue
			}
			inCooldown[strings.TrimSpace(profile.ProfileName)] = struct{}{}
		}
	}

	now := time.Now()
	filtered := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if _, blocked := inCooldown[profile]; blocked {
			continue
		}
		if r.db != nil {
			if ev, err := r.db.ActiveCooldown(provider, profile, now); err == nil && ev != nil {
				continue
			}
		}
		filtered = append(filtered, profile)
	}
	return filtered
}

func (r *SmartRunner) vaultPath() string {
	if r.vault != nil {
		if base := strings.TrimSpace(r.vault.BasePath()); base != "" {
			return base
		}
	}
	return authfile.DefaultVaultPath()
}

func (r *SmartRunner) loadProviderCredentials(provider string) (map[string]string, error) {
	return usage.LoadProfileCredentials(r.vaultPath(), provider)
}

// healthySelectionPool returns profiles that are candidates for failover when usage data is available.
// allUnavailable=true means we successfully checked usage and every checked profile was unavailable.
func (r *SmartRunner) healthySelectionPool(ctx context.Context, provider string, profiles []string, threshold float64) ([]string, bool) {
	if len(profiles) == 0 {
		return nil, false
	}

	credentials, err := r.loadProviderCredentials(provider)
	if err != nil || len(credentials) == 0 {
		return nil, false
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	fetcher := usage.NewMultiProfileFetcher()
	results := fetcher.FetchAllProfiles(fetchCtx, provider, credentials)

	profileSet := make(map[string]struct{}, len(profiles))
	for _, p := range profiles {
		profileSet[p] = struct{}{}
	}

	usageData := make(map[string]*rotation.UsageInfo)
	healthy := make([]string, 0, len(results))
	checked := 0

	for _, result := range results {
		if _, ok := profileSet[result.ProfileName]; !ok {
			continue
		}
		if result.Usage == nil {
			continue
		}

		info := &rotation.UsageInfo{
			ProfileName: result.ProfileName,
			AvailScore:  result.Usage.AvailabilityScore(),
			Error:       result.Usage.Error,
		}
		if result.Usage.PrimaryWindow != nil {
			info.PrimaryPercent = result.Usage.PrimaryWindow.UsedPercent
		}
		if result.Usage.SecondaryWindow != nil {
			info.SecondaryPercent = result.Usage.SecondaryWindow.UsedPercent
		}
		usageData[result.ProfileName] = info

		if result.Usage.Error != "" {
			continue
		}
		checked++
		if isProviderSwitchCandidateForHandoff(provider, result.Usage, threshold) {
			healthy = append(healthy, result.ProfileName)
		}
	}

	if r.rotation != nil && len(usageData) > 0 {
		r.rotation.SetUsageData(usageData)
	}

	if checked > 0 && len(healthy) == 0 {
		return nil, true
	}
	if len(healthy) == 0 {
		return nil, false
	}

	// Keep only currently available profiles (defensive in case of stale usage data)
	filtered := make([]string, 0, len(healthy))
	for _, p := range healthy {
		if slices.Contains(profiles, p) {
			filtered = append(filtered, p)
		}
	}
	return filtered, false
}

func isProviderSwitchCandidateForHandoff(provider string, info *usage.UsageInfo, threshold float64) bool {
	if info == nil || info.Error != "" {
		return false
	}
	// For Codex, credits are the hard gate. High utilization is a ranking signal,
	// but we should still attempt failover to other credit-available profiles.
	if provider == "codex" {
		if info.HasExhaustedWindow() {
			return false
		}
		return !info.IsCreditExhausted()
	}
	return !info.IsNearLimit(threshold)
}
