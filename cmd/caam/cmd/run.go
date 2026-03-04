package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authpool"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/exec"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/notify"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/rotation"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
	"github.com/spf13/cobra"
)

// getWd allows mocking os.Getwd in tests
var getWd = os.Getwd

func resolveInvokedProviderBinary(tool string) string {
	tool = normalizeToolName(tool)
	if !isInvokedAsProviderTool(tool) {
		return ""
	}

	invocationPath := os.Args[0]
	if abs, err := filepath.Abs(invocationPath); err == nil {
		invocationPath = abs
	}
	invocationPath = filepath.Clean(invocationPath)

	invocationResolved := invocationPath
	if resolved, err := filepath.EvalSymlinks(invocationPath); err == nil {
		invocationResolved = filepath.Clean(resolved)
	}

	invocationName := strings.ToLower(strings.TrimSuffix(filepath.Base(invocationPath), filepath.Ext(invocationPath)))
	candidateNames := providerBinaryCandidates(tool, invocationName)
	if runtime.GOOS == "windows" {
		withExt := make([]string, 0, len(candidateNames))
		for _, name := range candidateNames {
			withExt = append(withExt, name+".exe")
		}
		candidateNames = append(candidateNames, withExt...)
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		for _, candidateName := range candidateNames {
			candidate := filepath.Join(dir, candidateName)
			candidateAbs := candidate
			if abs, err := filepath.Abs(candidate); err == nil {
				candidateAbs = abs
			}
			candidateAbs = filepath.Clean(candidateAbs)
			if samePath(candidateAbs, invocationPath) || samePath(candidateAbs, invocationResolved) {
				continue
			}
			info, err := os.Stat(candidateAbs)
			if err != nil || info.IsDir() {
				continue
			}
			if info.Mode()&0o111 == 0 {
				continue
			}
			return candidateAbs
		}
	}

	return ""
}

func providerBinaryCandidates(tool, invocationName string) []string {
	tool = normalizeToolName(tool)
	invocationName = strings.ToLower(strings.TrimSpace(invocationName))
	seen := map[string]struct{}{}
	candidates := make([]string, 0, 6)
	add := func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, name)
	}

	add(tool)
	if invocationName != "" && normalizeToolName(invocationName) == tool {
		add(invocationName)
	}
	for alias, canonical := range toolAliases {
		if canonical == tool {
			add(alias)
		}
	}

	return candidates
}

func isInvokedAsProviderTool(tool string) bool {
	tool = normalizeToolName(tool)
	if tool == "" {
		return false
	}
	invoked := normalizeToolName(strings.ToLower(strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0]))))
	if invoked == "" {
		return false
	}
	return tool == invoked
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		if strings.EqualFold(a, b) {
			return true
		}
	} else if a == b {
		return true
	}
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = filepath.Clean(ra)
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = filepath.Clean(rb)
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// runCmd wraps AI CLI execution with automatic rate limit handling.
var runCmd = &cobra.Command{
	Use:   "run <tool> [-- args...]",
	Short: "Run AI CLI with automatic account switching",
	Long: `Wraps AI CLI execution with transparent rate limit detection and automatic
profile switching. This is the "zero friction" mode - just use caam run instead
of calling the CLI directly.

When a rate limit is detected:
1. The current profile is put into cooldown
2. The next best profile is automatically selected
3. The command is re-executed seamlessly

Use --precheck for proactive switching:
  When enabled, caam checks real-time usage levels BEFORE running and
  automatically switches to a healthier profile if current usage is near
  the limit. This prevents rate limit errors before they happen.

Examples:
  caam run claude -- "explain this code"
  caam run codex -- --model gpt-5 "write tests"
  caam run gemini -- "summarize this file"

  # Proactive switching (checks usage before running)
  caam run claude --precheck -- "explain this code"

  # Interactive mode (no auto-retry on rate limit)
  caam run claude

For shell integration, add an alias:
  alias claude='caam run claude --precheck --'

Then you can just use:
  claude "explain this code"

And rate limits will be handled automatically!`,
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: false,
	RunE:               runWrap,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().Int("max-retries", 1, "maximum retry attempts on rate limit (0 = no retries)")
	runCmd.Flags().Duration("cooldown", 60*time.Minute, "cooldown duration after rate limit")
	runCmd.Flags().Bool("quiet", false, "suppress profile switch notifications")
	runCmd.Flags().String("algorithm", "smart", "rotation algorithm (smart, round_robin, random)")
	runCmd.Flags().Bool("precheck", false, "check usage levels before running and switch if near limit")
	runCmd.Flags().Float64("precheck-threshold", 0.8, "usage threshold for precheck switching (0-1)")
}

func runWrap(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tool name required")
	}

	tool := normalizeToolName(args[0])

	// Validate tool
	if _, ok := tools[tool]; !ok {
		return fmt.Errorf("unknown tool: %s (supported: codex, claude, gemini; aliases: openclaw/open-claw/open_claw -> codex)", tool)
	}

	providerBinary := resolveInvokedProviderBinary(tool)
	if isInvokedAsProviderTool(tool) && providerBinary == "" {
		return fmt.Errorf("invoked caam as %s, but no alternate %s binary found in PATH", tool, tool)
	}

	// Parse CLI args (everything after the tool name)
	var cliArgs []string
	if len(args) > 1 {
		cliArgs = args[1:]
	}

	// Get flags
	quiet, _ := cmd.Flags().GetBool("quiet")
	algorithmStr, _ := cmd.Flags().GetString("algorithm")
	cooldownDur, _ := cmd.Flags().GetDuration("cooldown")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")

	// Parse algorithm
	var algorithm rotation.Algorithm
	switch strings.ToLower(algorithmStr) {
	case "smart":
		algorithm = rotation.AlgorithmSmart
	case "round_robin", "roundrobin":
		algorithm = rotation.AlgorithmRoundRobin
	case "random":
		algorithm = rotation.AlgorithmRandom
	default:
		return fmt.Errorf("unknown algorithm: %s (supported: smart, round_robin, random)", algorithmStr)
	}

	// Initialize vault
	if vault == nil {
		vault = authfile.NewVault(authfile.DefaultVaultPath())
	}

	// Initialize database
	db, err := getDB()
	if err != nil {
		// Non-fatal: cooldowns won't be recorded but execution can continue
		fmt.Fprintf(os.Stderr, "Warning: database unavailable, cooldowns will not be recorded\n")
		db = nil
	}

	// Initialize health storage
	healthStore := health.NewStorage("")

	// Load global config
	spmCfg, err := config.LoadSPMConfig()
	if err != nil {
		// Non-fatal: use defaults
		spmCfg = config.DefaultSPMConfig()
	}
	// CLI flag override: make --max-retries actually control handoff attempts.
	if maxRetries >= 0 {
		spmCfg.Handoff.MaxRetries = maxRetries
	}

	// Get working directory
	cwd, err := getWd()
	if err != nil {
		cwd, _ = os.Getwd()
	}

	// Precheck: switch profile if near limit before running
	precheckSelectedProfile := ""
	precheck, _ := cmd.Flags().GetBool("precheck")
	precheckThreshold, _ := cmd.Flags().GetFloat64("precheck-threshold")
	if precheck && (tool == "claude" || tool == "codex") {
		if switched, selected := runPrecheck(tool, precheckThreshold, quiet, db, algorithm); switched {
			precheckSelectedProfile = selected
		}
		if precheckSelectedProfile != "" && !quiet {
			fmt.Fprintf(os.Stderr, "caam: switched profile before running (usage was near limit)\n")
		}
	}

	// Initialize AuthPool (if enabled in config)
	var pool *authpool.AuthPool
	if spmCfg.Daemon.AuthPool.Enabled {
		pool = authpool.NewAuthPool(authpool.WithVault(vault))
		// Best effort load
		_ = pool.Load(authpool.PersistOptions{})
	}

	// Initialize Rotation Selector
	selector := rotation.NewSelector(algorithm, healthStore, db)

	// Initialize Runner
	if runner == nil {
		// Should be initialized in Root PersistentPreRunE, but defensive check
		runner = exec.NewRunner(registry)
	}

	// Initialize Notifier
	var notifier notify.Notifier
	if quiet {
		notifier = notify.NewTerminalNotifier(io.Discard, false)
	} else {
		notifier = notify.NewTerminalNotifier(os.Stderr, true)
	}

	// Create SmartRunner
	opts := exec.SmartRunnerOptions{
		HandoffConfig:    &spmCfg.Handoff,
		Notifier:         notifier,
		Vault:            vault,
		DB:               db,
		AuthPool:         pool,
		Rotation:         selector,
		CooldownDuration: cooldownDur,
	}
	smartRunner := exec.NewSmartRunner(runner, opts)

	// Get provider
	prov, ok := registry.Get(tool)
	if !ok {
		return fmt.Errorf("provider %s not found in registry", tool)
	}

	// Get active profile
	fileSet := tools[tool]()
	activeProfileName, _ := vault.ActiveProfile(fileSet)
	if precheckSelectedProfile != "" {
		activeProfileName = precheckSelectedProfile
	}
	if activeProfileName == "" {
		// If no active profile, try to select one
		profiles, err := vault.List(tool)
		if err != nil || len(profiles) == 0 {
			return fmt.Errorf("no profiles found for %s", tool)
		}
		eligible := filterEligibleRotationProfiles(tool, profiles, "")
		if len(eligible) == 0 {
			return fmt.Errorf("no usable profiles found for %s; run 'caam ls %s --all' to inspect and fix profile identity/auth mismatches", tool, tool)
		}
		res, err := selector.Select(tool, eligible, "")
		if err != nil {
			return fmt.Errorf("select profile: %w", err)
		}
		if res == nil || res.Selected == "" {
			return fmt.Errorf("no profile selected for %s", tool)
		}
		activeProfileName = res.Selected
		// Restore it
		if err := vault.Restore(fileSet, activeProfileName); err != nil {
			return fmt.Errorf("activate profile: %w", err)
		}
	}

	// Load profile object
	prof, err := profileStore.Load(tool, activeProfileName)
	if err != nil {
		// If profile object doesn't exist (only in vault), create a transient one.
		// We need a proper BasePath for locking to work correctly - otherwise
		// the lock file ends up in the current directory which causes issues
		// when multiple runs use the same profile.
		var basePath string
		if profileStore != nil {
			basePath = profileStore.ProfilePath(tool, activeProfileName)
		} else {
			// Fallback: use default store path
			basePath = filepath.Join(profile.DefaultStorePath(), tool, activeProfileName)
		}
		prof = &profile.Profile{
			Name:     activeProfileName,
			Provider: tool,
			AuthMode: "oauth", // Assumption
			BasePath: basePath,
		}
	}

	// Set CLI overrides
	// Cooldown duration is now passed directly to SmartRunner via opts.CooldownDuration

	// Run
	runOptions := exec.RunOptions{
		Profile:      prof,
		Provider:     prov,
		Args:         cliArgs,
		WorkDir:      cwd,
		Env:          nil,  // Inherit
		UseGlobalEnv: true, // Force global environment for vault-based switching
		Binary:       providerBinary,
	}
	if !runOptions.NoLock && !profileReadyForLock(runOptions.Profile) {
		_, nextProf, switchErr := switchToUnlockedProfile(tool, runOptions.Profile.Name, fileSet, selector, quiet)
		if switchErr != nil {
			return fmt.Errorf("lock profile: profile %s is already locked (and no unlocked alternatives: %v)", runOptions.Profile.Name, switchErr)
		}
		runOptions.Profile = nextProf
		activeProfileName = nextProf.Name
	}

	// Handle signals - use cmd.Context() for proper context propagation
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
			// Context cancelled, goroutine can exit cleanly
		}
	}()

	err = smartRunner.Run(ctx, runOptions)
	if isProfileLockedErr(err) {
		_, nextProf, switchErr := switchToUnlockedProfile(tool, activeProfileName, fileSet, selector, quiet)
		if switchErr != nil {
			err = fmt.Errorf("%w (and no unlocked alternatives: %v)", err, switchErr)
		} else {
			runOptions.Profile = nextProf
			err = smartRunner.Run(ctx, runOptions)
		}
	}

	// Stop signal handling and allow the signal goroutine to exit
	signal.Stop(sigChan)
	cancel() // Ensure goroutine exits via ctx.Done() path

	// Handle exit code
	var exitErr *exec.ExitCodeError
	if errors.As(err, &exitErr) {
		// Clean up before exiting - os.Exit() bypasses defers
		if db != nil {
			db.Close()
		}
		os.Exit(exitErr.Code)
	}

	return err
}

// runPrecheck checks current usage levels and switches profile if near limit.
// Returns whether a switch was performed and the selected profile name.
func runPrecheck(tool string, threshold float64, quiet bool, db *caamdb.DB, algorithm rotation.Algorithm) (bool, string) {
	tool = normalizeToolName(tool)

	// Get current profile's access token
	vaultDir := authfile.DefaultVaultPath()

	// Get the currently active profile
	fileSet := tools[tool]()
	currentProfile, _ := vault.ActiveProfile(fileSet)
	if currentProfile == "" {
		return false, "" // No active profile
	}
	currentInCooldown := false
	if db != nil {
		if ev, err := db.ActiveCooldown(tool, currentProfile, time.Now()); err == nil && ev != nil {
			currentInCooldown = true
		}
	}

	// Load credentials for current profile
	credentials, err := usage.LoadProfileCredentials(vaultDir, tool)
	if err != nil || len(credentials) == 0 {
		return false, ""
	}

	currentUsage := &usage.UsageInfo{Provider: tool, ProfileName: currentProfile}
	currentHasAuthFailure := false

	// If current profile is not already in cooldown, inspect current usage/auth state.
	if !currentInCooldown {
		token, ok := credentials[currentProfile]
		if !ok {
			return false, ""
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		fetcher := usage.NewMultiProfileFetcher()
		results := fetcher.FetchAllProfiles(ctx, tool, map[string]string{currentProfile: token})

		if len(results) == 0 || results[0].Usage == nil {
			return false, ""
		}

		currentUsage = results[0].Usage
		currentHasAuthFailure = currentUsage.Error != "" && isProviderAuthFailure(tool, currentUsage.Error)
		currentHardBlocked := currentHasAuthFailure || isProviderHardBlocked(tool, currentUsage)
		if !currentHardBlocked && !currentUsage.IsNearLimit(threshold) {
			return false, "" // All good, no switch needed
		}
	}

	// Need to switch - find all profiles
	allProfiles, err := vault.List(tool)
	if err != nil || len(allProfiles) <= 1 {
		return false, "" // Can't switch
	}
	allProfiles = filterEligibleRotationProfiles(tool, allProfiles, currentProfile)
	if len(allProfiles) <= 1 {
		return false, "" // Can't switch
	}
	eligibleSet := make(map[string]struct{}, len(allProfiles))
	for _, profileName := range allProfiles {
		eligibleSet[profileName] = struct{}{}
	}

	// Find best alternative using usage-aware selection
	allCredentials, err := usage.LoadProfileCredentials(vaultDir, tool)
	if err != nil || len(allCredentials) == 0 {
		return false, ""
	}

	// Fetch usage for all profiles
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fetcher := usage.NewMultiProfileFetcher()
	allResults := fetcher.FetchAllProfiles(ctx, tool, allCredentials)

	// Convert to rotation.UsageInfo format
	usageData := make(map[string]*rotation.UsageInfo)
	healthyProfiles := make([]string, 0, len(allResults))
	checkedProfiles := 0
	for _, r := range allResults {
		if _, ok := eligibleSet[r.ProfileName]; !ok {
			continue
		}
		if r.Usage == nil {
			continue
		}
		if r.Usage.Error == "" {
			checkedProfiles++
			if isProviderSwitchCandidate(tool, r.Usage, threshold) {
				healthyProfiles = append(healthyProfiles, r.ProfileName)
			}
		}
		info := &rotation.UsageInfo{
			ProfileName: r.ProfileName,
			AvailScore:  r.Usage.AvailabilityScore(),
			Error:       r.Usage.Error,
		}
		if r.Usage.PrimaryWindow != nil {
			info.PrimaryPercent = r.Usage.PrimaryWindow.UsedPercent
		}
		if r.Usage.SecondaryWindow != nil {
			info.SecondaryPercent = r.Usage.SecondaryWindow.UsedPercent
		}
		usageData[r.ProfileName] = info
	}

	// Use rotation selector with usage data
	selector := rotation.NewSelector(algorithm, nil, db)
	selector.SetUsageData(usageData)

	selectionPool := allProfiles
	if checkedProfiles > 0 && len(healthyProfiles) == 0 {
		if !quiet {
			if tool == "codex" {
				fmt.Fprintf(os.Stderr, "caam: all %s profiles are unavailable or out of credits; cannot precheck switch\n", tool)
			} else {
				fmt.Fprintf(os.Stderr, "caam: all %s profiles are near limit or unavailable; cannot precheck switch\n", tool)
			}
		}
		return false, ""
	}
	if len(healthyProfiles) > 0 {
		selectionPool = healthyProfiles
	}
	shouldExcludeCurrent := currentInCooldown || currentHasAuthFailure || isProviderHardBlocked(tool, currentUsage)
	if !shouldExcludeCurrent && tool != "codex" && currentUsage.IsNearLimit(threshold) {
		shouldExcludeCurrent = true
	}
	if shouldExcludeCurrent {
		filtered := make([]string, 0, len(selectionPool))
		for _, p := range selectionPool {
			if p != currentProfile {
				filtered = append(filtered, p)
			}
		}
		selectionPool = filtered
		if len(selectionPool) == 0 {
			return false, ""
		}
	}
	readyPool := make([]string, 0, len(selectionPool))
	for _, p := range selectionPool {
		prof, loadErr := loadOrCreateRunProfile(tool, p)
		if loadErr != nil {
			continue
		}
		if profileReadyForLock(prof) {
			readyPool = append(readyPool, p)
		}
	}
	if len(readyPool) > 0 {
		selectionPool = readyPool
	}

	result, err := selector.Select(tool, selectionPool, currentProfile)
	if err != nil || result.Selected == currentProfile {
		return false, "" // Couldn't find better alternative
	}
	if tool == "codex" {
		currentToken := strings.TrimSpace(allCredentials[currentProfile])
		selectedToken := strings.TrimSpace(allCredentials[result.Selected])
		if currentToken != "" && selectedToken != "" && currentToken == selectedToken {
			if !quiet {
				fmt.Fprintf(
					os.Stderr,
					"caam: skipping codex precheck switch %s -> %s (profiles share same credentials)\n",
					currentProfile,
					result.Selected,
				)
			}
			return false, ""
		}
	}

	// Switch to the better profile
	if err := vault.Restore(fileSet, result.Selected); err != nil {
		return false, ""
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "caam: precheck switched %s/%s -> %s/%s\n",
			tool, currentProfile, tool, result.Selected)
	}

	return true, result.Selected
}

func isProviderAuthFailure(tool, errMsg string) bool {
	tool = normalizeToolName(tool)
	errMsg = strings.ToLower(strings.TrimSpace(errMsg))
	if errMsg == "" {
		return false
	}
	switch tool {
	case "codex":
		needles := []string{
			"refresh_token_reused",
			"refresh token was already used",
			"access token could not be refreshed",
			"invalid_grant",
		}
		for _, needle := range needles {
			if strings.Contains(errMsg, needle) {
				return true
			}
		}
	}
	return false
}

func isProviderHardBlocked(tool string, info *usage.UsageInfo) bool {
	tool = normalizeToolName(tool)
	if info == nil {
		return false
	}
	switch tool {
	case "codex":
		return info.IsCreditExhausted()
	case "claude":
		if info.HasExhaustedWindow() {
			return true
		}
		return isRateLimitSignal(info.Error)
	default:
		return false
	}
}

func isProviderSwitchCandidate(tool string, info *usage.UsageInfo, threshold float64) bool {
	tool = normalizeToolName(tool)
	if info == nil || info.Error != "" {
		return false
	}
	if isProviderHardBlocked(tool, info) {
		return false
	}
	// For Codex, credits are the hard gate; near-limit windows are used for ranking,
	// not hard exclusion, because profiles can still be usable above warning thresholds.
	if tool == "codex" {
		if info.HasExhaustedWindow() {
			return false
		}
		return true
	}
	return !info.IsNearLimit(threshold)
}

func isRateLimitSignal(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	needles := []string{
		"rate limit",
		"429",
		"too many requests",
		"quota",
		"usage limit",
		"exhausted",
		"try again in",
		"resource_exhausted",
	}
	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func isProfileLockedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "already locked")
}

func loadOrCreateRunProfile(tool, profileName string) (*profile.Profile, error) {
	prof, err := profileStore.Load(tool, profileName)
	if err == nil {
		return prof, nil
	}

	var basePath string
	if profileStore != nil {
		basePath = profileStore.ProfilePath(tool, profileName)
	} else {
		basePath = filepath.Join(profile.DefaultStorePath(), tool, profileName)
	}

	return &profile.Profile{
		Name:     profileName,
		Provider: tool,
		AuthMode: "oauth",
		BasePath: basePath,
	}, nil
}

func profileReadyForLock(prof *profile.Profile) bool {
	if prof == nil {
		return false
	}
	if err := prof.LockWithCleanup(); err != nil {
		return false
	}
	_ = prof.Unlock()
	return true
}

func switchToUnlockedProfile(tool, currentProfile string, fileSet authfile.AuthFileSet, selector *rotation.Selector, quiet bool) (string, *profile.Profile, error) {
	profiles, err := vault.List(tool)
	if err != nil {
		return "", nil, fmt.Errorf("list profiles: %w", err)
	}
	profiles = filterEligibleRotationProfiles(tool, profiles, currentProfile)

	candidates := make([]string, 0, len(profiles))
	for _, name := range profiles {
		if name == currentProfile {
			continue
		}
		prof, loadErr := loadOrCreateRunProfile(tool, name)
		if loadErr != nil {
			continue
		}
		if profileReadyForLock(prof) {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return "", nil, fmt.Errorf("no unlocked candidate profiles for %s", tool)
	}
	if tool == "codex" || tool == "claude" {
		credentials, credErr := usage.LoadProfileCredentials(authfile.DefaultVaultPath(), tool)
		if credErr == nil && len(credentials) > 0 {
			subset := make(map[string]string)
			for _, name := range candidates {
				if token := strings.TrimSpace(credentials[name]); token != "" {
					subset[name] = token
				}
			}
			if len(subset) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				fetcher := usage.NewMultiProfileFetcher()
				results := fetcher.FetchAllProfiles(ctx, tool, subset)

				usageData := make(map[string]*rotation.UsageInfo)
				healthy := make([]string, 0, len(results))
				checked := 0
				for _, r := range results {
					if r.Usage == nil {
						continue
					}
					if r.Usage.Error == "" {
						checked++
						if isProviderSwitchCandidate(tool, r.Usage, 0.8) {
							healthy = append(healthy, r.ProfileName)
						}
					}
					info := &rotation.UsageInfo{
						ProfileName: r.ProfileName,
						AvailScore:  r.Usage.AvailabilityScore(),
						Error:       r.Usage.Error,
					}
					if r.Usage.PrimaryWindow != nil {
						info.PrimaryPercent = r.Usage.PrimaryWindow.UsedPercent
					}
					if r.Usage.SecondaryWindow != nil {
						info.SecondaryPercent = r.Usage.SecondaryWindow.UsedPercent
					}
					usageData[r.ProfileName] = info
				}
				if len(usageData) > 0 {
					selector.SetUsageData(usageData)
				}
				if checked > 0 && len(healthy) == 0 {
					if tool == "codex" {
						return "", nil, fmt.Errorf("no unlocked %s candidates with available windows/credits", tool)
					}
					return "", nil, fmt.Errorf("no unlocked %s candidates below near-limit threshold", tool)
				}
				if len(healthy) > 0 {
					candidates = healthy
				}
			}
		}
	}

	res, err := selector.Select(tool, candidates, currentProfile)
	if err != nil {
		return "", nil, fmt.Errorf("select unlocked profile: %w", err)
	}
	if res == nil || res.Selected == "" {
		return "", nil, fmt.Errorf("selector returned no unlocked profile")
	}
	if err := vault.Restore(fileSet, res.Selected); err != nil {
		return "", nil, fmt.Errorf("activate unlocked profile %s: %w", res.Selected, err)
	}

	nextProf, err := loadOrCreateRunProfile(tool, res.Selected)
	if err != nil {
		return "", nil, fmt.Errorf("load unlocked profile %s: %w", res.Selected, err)
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "caam: switched from locked %s/%s -> %s/%s\n", tool, currentProfile, tool, res.Selected)
	}
	return res.Selected, nextProf, nil
}
