package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SyncState manages the complete sync state including identity, pool, queue, and history.
type SyncState struct {
	// Identity is the local machine's identity.
	Identity *LocalIdentity

	// Pool is the sync pool configuration.
	Pool *SyncPool

	// Queue holds pending sync operations for retry.
	Queue *SyncQueue

	// History records recent sync operations.
	History *SyncHistory

	basePath string
	mu       sync.RWMutex
}

// SyncQueue holds pending sync operations for machines that failed.
type SyncQueue struct {
	// Entries are pending sync operations.
	Entries []QueueEntry `json:"entries"`

	// MaxSize is the maximum number of entries to keep.
	MaxSize int `json:"max_size"`
}

// QueueEntry represents a pending sync operation.
type QueueEntry struct {
	// Provider is the auth provider (claude, codex, gemini).
	Provider string `json:"provider"`

	// Profile is the profile name.
	Profile string `json:"profile"`

	// Machine is the target machine ID.
	Machine string `json:"machine"`

	// AddedAt is when this entry was added.
	AddedAt time.Time `json:"added_at"`

	// Attempts is the number of retry attempts.
	Attempts int `json:"attempts"`

	// LastAttempt is when the last attempt was made.
	LastAttempt time.Time `json:"last_attempt,omitempty"`

	// LastError is the error from the last attempt.
	LastError string `json:"last_error,omitempty"`
}

// SyncHistory records recent sync operations.
type SyncHistory struct {
	// Entries are recent sync events.
	Entries []HistoryEntry `json:"entries"`

	// MaxSize is the maximum number of entries to keep.
	MaxSize int `json:"max_size"`
}

// HistoryEntry represents a single sync event.
type HistoryEntry struct {
	// Timestamp is when this event occurred.
	Timestamp time.Time `json:"timestamp"`

	// Trigger is what initiated the sync (backup, refresh, manual, retry).
	Trigger string `json:"trigger"`

	// Provider is the auth provider.
	Provider string `json:"provider"`

	// Profile is the profile name.
	Profile string `json:"profile"`

	// Machine is the target machine name.
	Machine string `json:"machine"`

	// Action is what happened (push, pull, skip).
	Action string `json:"action"`

	// Success indicates if the operation succeeded.
	Success bool `json:"success"`

	// Error is the error message if the operation failed.
	Error string `json:"error,omitempty"`

	// Duration is how long the operation took.
	Duration time.Duration `json:"duration"`
}

// Queue and History file names.
const (
	queueFileName   = "queue.json"
	historyFileName = "history.json"
)

// Default sizes.
const (
	DefaultQueueMaxSize   = 100
	DefaultHistoryMaxSize = 1000
)

// NewSyncState creates a new SyncState.
func NewSyncState(basePath string) *SyncState {
	if basePath == "" {
		basePath = SyncDataDir()
	}

	pool := NewSyncPool()
	pool.SetBasePath(basePath)

	return &SyncState{
		Identity: nil,
		Pool:     pool,
		Queue: &SyncQueue{
			Entries: make([]QueueEntry, 0),
			MaxSize: DefaultQueueMaxSize,
		},
		History: &SyncHistory{
			Entries: make([]HistoryEntry, 0),
			MaxSize: DefaultHistoryMaxSize,
		},
		basePath: basePath,
	}
}

// Load loads all sync state from disk.
func (s *SyncState) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load identity
	identity, err := GetOrCreateLocalIdentity()
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}
	s.Identity = identity

	// Load pool
	pool := NewSyncPool()
	pool.SetBasePath(s.basePath)
	if err := pool.Load(); err != nil {
		return fmt.Errorf("load pool: %w", err)
	}
	s.Pool = pool

	// Ensure pool has our machine ID
	if s.Pool.LocalMachineID == "" {
		s.Pool.LocalMachineID = s.Identity.ID
	}

	// Load queue
	if err := s.loadQueue(); err != nil {
		// Non-fatal - start with empty queue
		s.Queue = &SyncQueue{
			Entries: make([]QueueEntry, 0),
			MaxSize: DefaultQueueMaxSize,
		}
	}

	// Load history
	if err := s.loadHistory(); err != nil {
		// Non-fatal - start with empty history
		s.History = &SyncHistory{
			Entries: make([]HistoryEntry, 0),
			MaxSize: DefaultHistoryMaxSize,
		}
	}

	return nil
}

// Save saves all sync state to disk.
func (s *SyncState) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Save pool (LocalMachineID is set during Load, not here to avoid races)
	if s.Pool != nil {
		if err := s.Pool.Save(); err != nil {
			return fmt.Errorf("save pool: %w", err)
		}
	}

	// Save queue
	if err := s.saveQueue(); err != nil {
		return fmt.Errorf("save queue: %w", err)
	}

	// Save history
	if err := s.saveHistory(); err != nil {
		return fmt.Errorf("save history: %w", err)
	}

	return nil
}

// loadQueue loads the queue from disk.
func (s *SyncState) loadQueue() error {
	path := filepath.Join(s.basePath, queueFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var queue SyncQueue
	if err := json.Unmarshal(data, &queue); err != nil {
		return err
	}

	if queue.MaxSize == 0 {
		queue.MaxSize = DefaultQueueMaxSize
	}
	s.Queue = &queue
	return nil
}

// saveQueue saves the queue to disk.
func (s *SyncState) saveQueue() error {
	if s.Queue == nil {
		return nil
	}

	// Trim to max size
	if len(s.Queue.Entries) > s.Queue.MaxSize {
		s.Queue.Entries = s.Queue.Entries[len(s.Queue.Entries)-s.Queue.MaxSize:]
	}

	return s.saveJSON(queueFileName, s.Queue)
}

// loadHistory loads the history from disk.
func (s *SyncState) loadHistory() error {
	path := filepath.Join(s.basePath, historyFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var history SyncHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return err
	}

	if history.MaxSize == 0 {
		history.MaxSize = DefaultHistoryMaxSize
	}
	s.History = &history
	return nil
}

// saveHistory saves the history to disk.
func (s *SyncState) saveHistory() error {
	if s.History == nil {
		return nil
	}

	// Trim to max size
	if len(s.History.Entries) > s.History.MaxSize {
		s.History.Entries = s.History.Entries[len(s.History.Entries)-s.History.MaxSize:]
	}

	return s.saveJSON(historyFileName, s.History)
}

// saveJSON saves a value to a JSON file atomically.
func (s *SyncState) saveJSON(filename string, v interface{}) error {
	// Ensure directory exists
	if err := os.MkdirAll(s.basePath, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	path := filepath.Join(s.basePath, filename)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Atomic write
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// AddToQueue adds a sync operation to the queue.
func (s *SyncState) AddToQueue(provider, profile, machineID, errorMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Queue == nil {
		s.Queue = &SyncQueue{
			Entries: make([]QueueEntry, 0),
			MaxSize: DefaultQueueMaxSize,
		}
	}

	// Check if entry already exists
	for i, e := range s.Queue.Entries {
		if e.Provider == provider && e.Profile == profile && e.Machine == machineID {
			// Update existing entry
			s.Queue.Entries[i].Attempts++
			s.Queue.Entries[i].LastAttempt = time.Now()
			s.Queue.Entries[i].LastError = errorMsg
			return
		}
	}

	// Add new entry
	s.Queue.Entries = append(s.Queue.Entries, QueueEntry{
		Provider:    provider,
		Profile:     profile,
		Machine:     machineID,
		AddedAt:     time.Now(),
		Attempts:    1,
		LastAttempt: time.Now(),
		LastError:   errorMsg,
	})
}

// RemoveFromQueue removes an entry from the queue.
func (s *SyncState) RemoveFromQueue(provider, profile, machineID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Queue == nil {
		return
	}

	for i, e := range s.Queue.Entries {
		if e.Provider == provider && e.Profile == profile && e.Machine == machineID {
			s.Queue.Entries = append(s.Queue.Entries[:i], s.Queue.Entries[i+1:]...)
			return
		}
	}
}

// ClearOldQueueEntries removes entries older than maxAge.
func (s *SyncState) ClearOldQueueEntries(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Queue == nil {
		return
	}

	cutoff := time.Now().Add(-maxAge)
	var filtered []QueueEntry
	for _, e := range s.Queue.Entries {
		if e.AddedAt.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	s.Queue.Entries = filtered
}

// AddToHistory adds a sync event to the history.
func (s *SyncState) AddToHistory(entry HistoryEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.History == nil {
		s.History = &SyncHistory{
			Entries: make([]HistoryEntry, 0),
			MaxSize: DefaultHistoryMaxSize,
		}
	}

	s.History.Entries = append(s.History.Entries, entry)

	// Trim if over max size
	if len(s.History.Entries) > s.History.MaxSize {
		s.History.Entries = s.History.Entries[len(s.History.Entries)-s.History.MaxSize:]
	}
}

// RecentHistory returns the most recent history entries.
func (s *SyncState) RecentHistory(limit int) []HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.History == nil || len(s.History.Entries) == 0 {
		return nil
	}

	entries := s.History.Entries
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	// Return in reverse order (most recent first)
	result := make([]HistoryEntry, len(entries))
	for i, e := range entries {
		result[len(entries)-1-i] = e
	}
	return result
}

// LoadSyncState loads or creates the sync state.
func LoadSyncState() (*SyncState, error) {
	state := NewSyncState("")
	if err := state.Load(); err != nil {
		return nil, err
	}
	return state, nil
}
