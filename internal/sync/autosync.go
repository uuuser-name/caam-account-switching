package sync

import (
	"context"
	"log"
	"sync"
	"time"
)

// Default configuration for auto-sync.
const (
	// DefaultThrottleInterval is the minimum time between syncs for the same profile.
	DefaultThrottleInterval = 30 * time.Second

	// DefaultSyncTimeout is the maximum time for a single sync operation.
	DefaultSyncTimeout = 5 * time.Minute
)

// SyncThrottler prevents sync storms by rate-limiting sync operations.
type SyncThrottler struct {
	// lastSync tracks when each profile was last synced.
	lastSync map[string]time.Time

	// minInterval is the minimum time between syncs for the same profile.
	minInterval time.Duration

	mu sync.RWMutex
}

// NewThrottler creates a new SyncThrottler with the given minimum interval.
func NewThrottler(minInterval time.Duration) *SyncThrottler {
	if minInterval <= 0 {
		minInterval = DefaultThrottleInterval
	}
	return &SyncThrottler{
		lastSync:    make(map[string]time.Time),
		minInterval: minInterval,
	}
}

// ShouldSync returns true if enough time has passed since the last sync
// for this profile.
func (t *SyncThrottler) ShouldSync(provider, profile string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := profileKey(provider, profile)
	if last, ok := t.lastSync[key]; ok {
		if time.Since(last) < t.minInterval {
			return false // Too soon, skip
		}
	}
	return true
}

// RecordSync records that a sync was just performed for this profile.
func (t *SyncThrottler) RecordSync(provider, profile string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := profileKey(provider, profile)
	t.lastSync[key] = time.Now()
}

// Reset clears all throttle records.
func (t *SyncThrottler) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastSync = make(map[string]time.Time)
}

// LastSyncTime returns when the profile was last synced, or zero if never.
func (t *SyncThrottler) LastSyncTime(provider, profile string) time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := profileKey(provider, profile)
	return t.lastSync[key]
}

// profileKey creates a unique key for a provider/profile combination.
func profileKey(provider, profile string) string {
	return provider + "/" + profile
}

// Global throttler instance.
var globalThrottler = NewThrottler(DefaultThrottleInterval)

// AutoSyncConfig configures auto-sync behavior.
type AutoSyncConfig struct {
	// ThrottleInterval is the minimum time between syncs for the same profile.
	ThrottleInterval time.Duration

	// SyncTimeout is the maximum time for a sync operation.
	SyncTimeout time.Duration

	// VaultPath is the local vault directory.
	VaultPath string

	// RemoteVaultPath is the remote vault directory.
	RemoteVaultPath string

	// Verbose enables verbose logging.
	Verbose bool
}

// DefaultAutoSyncConfig returns the default auto-sync configuration.
func DefaultAutoSyncConfig() AutoSyncConfig {
	defaultSyncerConfig := DefaultSyncerConfig()
	return AutoSyncConfig{
		ThrottleInterval: DefaultThrottleInterval,
		SyncTimeout:      DefaultSyncTimeout,
		VaultPath:        defaultSyncerConfig.VaultPath,
		RemoteVaultPath:  defaultSyncerConfig.RemoteVaultPath,
		Verbose:          false,
	}
}

// TriggerSyncIfEnabled checks if auto-sync is enabled and triggers sync
// for the specified profile. Returns immediately - sync runs in background.
// This is the main entry point for auto-sync after backup/refresh.
func TriggerSyncIfEnabled(provider, profile string) {
	TriggerSyncIfEnabledWithConfig(provider, profile, DefaultAutoSyncConfig())
}

// TriggerSyncIfEnabledWithConfig is like TriggerSyncIfEnabled but with custom config.
func TriggerSyncIfEnabledWithConfig(provider, profile string, config AutoSyncConfig) {
	state, err := LoadSyncState()
	if err != nil {
		// Sync not configured, silently ignore
		return
	}

	// Apply throttle interval from config (if set)
	SetThrottleInterval(config.ThrottleInterval)

	// Check if sync is enabled (both enabled and auto_sync must be true)
	if state.Pool == nil || !state.Pool.Enabled || !state.Pool.AutoSync {
		// Sync disabled, do nothing
		return
	}

	if len(state.Pool.Machines) == 0 {
		// No machines to sync with
		return
	}

	// Check throttle to prevent sync storms
	if !globalThrottler.ShouldSync(provider, profile) {
		// Recently synced, skip
		return
	}

	// Run sync in background goroutine
	go runBackgroundSync(provider, profile, state, config)
}

// runBackgroundSync performs the actual sync operation in the background.
func runBackgroundSync(provider, profile string, state *SyncState, config AutoSyncConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), config.SyncTimeout)
	defer cancel()

	syncerConfig := SyncerConfig{
		VaultPath:       config.VaultPath,
		RemoteVaultPath: config.RemoteVaultPath,
		ConnectOptions:  DefaultConnectOptions(),
	}

	syncer, err := NewSyncer(syncerConfig)
	if err != nil {
		logSyncError("create syncer", err, config.Verbose)
		return
	}
	defer syncer.Close()

	// Override syncer's state with our loaded state
	syncer.state = state

	results, err := syncer.SyncProfile(ctx, provider, profile)
	if err != nil {
		logSyncError("sync profile", err, config.Verbose)
		// Record throttle even on failure to prevent sync storms
		globalThrottler.RecordSync(provider, profile)
		// Queue for retry
		queueFailedSync(state, provider, profile, err.Error())
		return
	}

	// Log results and queue failures
	logSyncResults(results, config.Verbose)

	// Update throttle timestamp
	globalThrottler.RecordSync(provider, profile)

	// Queue any failed machines for retry
	failedMachines := getFailedMachines(results)
	if len(failedMachines) > 0 {
		for _, machineID := range failedMachines {
			errMsg := getErrorForMachine(results, machineID)
			state.AddToQueue(provider, profile, machineID, errMsg)
		}
		state.Save()
	}
}

// logSyncResults logs the results of sync operations.
func logSyncResults(results []*SyncResult, verbose bool) {
	if !verbose {
		return
	}

	stats := AggregateResults(results)
	log.Printf("Sync complete: %d pushed, %d pulled, %d skipped, %d failed",
		stats.Pushed, stats.Pulled, stats.Skipped, stats.Failed)
}

// logSyncError logs a sync error.
func logSyncError(operation string, err error, verbose bool) {
	if !verbose {
		return
	}
	log.Printf("Sync error (%s): %v", operation, err)
}

// getFailedMachines extracts machine IDs that had failures.
func getFailedMachines(results []*SyncResult) []string {
	var failed []string
	seen := make(map[string]bool)

	for _, r := range results {
		if !r.Success && r.Operation != nil && r.Operation.Machine != nil {
			machineID := r.Operation.Machine.ID
			if !seen[machineID] {
				seen[machineID] = true
				failed = append(failed, machineID)
			}
		}
	}

	return failed
}

// getErrorForMachine gets the error message for a specific machine from results.
func getErrorForMachine(results []*SyncResult, machineID string) string {
	for _, r := range results {
		if !r.Success && r.Operation != nil && r.Operation.Machine != nil {
			if r.Operation.Machine.ID == machineID && r.Error != nil {
				return r.Error.Error()
			}
		}
	}
	return "unknown error"
}

// queueFailedSync adds a failed sync to the queue for retry.
func queueFailedSync(state *SyncState, provider, profile, errorMsg string) {
	if state.Pool == nil {
		return
	}

	for _, m := range state.Pool.Machines {
		state.AddToQueue(provider, profile, m.ID, errorMsg)
	}

	if err := state.Save(); err != nil {
		// Log but don't fail
		logSyncError("save state", err, true)
	}
}

// ProcessQueueIfNeeded processes pending sync retries in the background.
// Call this during idle periods to retry failed syncs.
func ProcessQueueIfNeeded() {
	ProcessQueueIfNeededWithConfig(DefaultAutoSyncConfig())
}

// ProcessQueueIfNeededWithConfig processes pending sync retries with custom config.
func ProcessQueueIfNeededWithConfig(config AutoSyncConfig) {
	state, err := LoadSyncState()
	if err != nil || state.Pool == nil || !state.Pool.Enabled {
		return
	}

	// Clean up old entries (> 24 hours)
	state.ClearOldQueueEntries(24 * time.Hour)

	if state.Queue == nil || len(state.Queue.Entries) == 0 {
		return
	}

	// Process queue in background
	go processQueue(state, config)
}

// processQueue processes all pending queue entries.
func processQueue(state *SyncState, config AutoSyncConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), config.SyncTimeout)
	defer cancel()

	syncerConfig := SyncerConfig{
		VaultPath:       config.VaultPath,
		RemoteVaultPath: config.RemoteVaultPath,
		ConnectOptions:  DefaultConnectOptions(),
	}

	syncer, err := NewSyncer(syncerConfig)
	if err != nil {
		logSyncError("create syncer for queue", err, config.Verbose)
		return
	}
	defer syncer.Close()

	// Override syncer's state
	syncer.state = state

	// Track entries to remove after iteration (modifying slice during range is unsafe)
	type entryKey struct {
		provider, profile, machine string
	}
	var toRemove []entryKey

	// Take a snapshot of entries to process (in case underlying slice changes)
	entries := make([]QueueEntry, len(state.Queue.Entries))
	copy(entries, state.Queue.Entries)

	// Process each queue entry individually with its specific machine
	for _, entry := range entries {
		// Find the specific machine that failed
		machine := state.Pool.GetMachine(entry.Machine)
		if machine == nil {
			// Machine was removed from pool, mark for removal
			toRemove = append(toRemove, entryKey{entry.Provider, entry.Profile, entry.Machine})
			continue
		}

		// Sync only with the specific machine that failed
		result, err := syncer.SyncProfileWithMachine(ctx, entry.Provider, entry.Profile, machine)
		if err != nil {
			logSyncError("process queue entry", err, config.Verbose)
			continue
		}

		if result.Success {
			toRemove = append(toRemove, entryKey{entry.Provider, entry.Profile, entry.Machine})
		}
	}

	// Remove completed/orphaned entries after iteration
	for _, key := range toRemove {
		state.RemoveFromQueue(key.provider, key.profile, key.machine)
	}

	// Save updated queue
	if err := state.Save(); err != nil {
		logSyncError("save state", err, config.Verbose)
	}
}

// SetThrottleInterval updates the global throttle interval.
func SetThrottleInterval(interval time.Duration) {
	globalThrottler.mu.Lock()
	defer globalThrottler.mu.Unlock()

	if interval > 0 {
		globalThrottler.minInterval = interval
	}
}

// GetThrottleInterval returns the current global throttle interval.
func GetThrottleInterval() time.Duration {
	globalThrottler.mu.RLock()
	defer globalThrottler.mu.RUnlock()

	return globalThrottler.minInterval
}

// ResetThrottler clears all throttle records.
func ResetThrottler() {
	globalThrottler.Reset()
}

// IsSyncEnabled checks if auto-sync is currently enabled.
func IsSyncEnabled() bool {
	state, err := LoadSyncState()
	if err != nil {
		return false
	}
	return state.Pool != nil && state.Pool.Enabled && state.Pool.AutoSync
}

// HasMachinesConfigured checks if there are machines in the sync pool.
func HasMachinesConfigured() bool {
	state, err := LoadSyncState()
	if err != nil {
		return false
	}
	return state.Pool != nil && len(state.Pool.Machines) > 0
}

// GetSyncStatus returns a summary of the current sync status.
type SyncStatus struct {
	Enabled      bool
	AutoSync     bool
	MachineCount int
	QueueCount   int
	LastFullSync time.Time
}

// GetSyncStatus returns the current sync status.
func GetSyncStatus() (*SyncStatus, error) {
	state, err := LoadSyncState()
	if err != nil {
		return nil, err
	}

	status := &SyncStatus{}

	if state.Pool != nil {
		status.Enabled = state.Pool.Enabled
		status.AutoSync = state.Pool.AutoSync
		status.MachineCount = len(state.Pool.Machines)
		status.LastFullSync = state.Pool.LastFullSync
	}

	if state.Queue != nil {
		status.QueueCount = len(state.Queue.Entries)
	}

	return status, nil
}
