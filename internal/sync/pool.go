package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// poolFileName is the name of the pool configuration file.
const poolFileName = "pool.json"

// SyncPool manages a collection of machines for sync operations.
type SyncPool struct {
	// LocalMachineID is the ID of the local machine.
	LocalMachineID string `json:"local_machine_id"`

	// Machines is a map of machine ID to Machine.
	Machines map[string]*Machine `json:"machines"`

	// Enabled indicates if sync is enabled for this pool.
	Enabled bool `json:"enabled"`

	// AutoSync enables automatic sync after backup/refresh operations.
	// Defaults to false - must be explicitly enabled by user.
	AutoSync bool `json:"auto_sync"`

	// LastFullSync is the timestamp of the last full sync operation.
	LastFullSync time.Time `json:"last_full_sync,omitempty"`

	// basePath is the directory where pool.json is stored.
	// If empty, uses the global SyncDataDir().
	basePath string

	mu sync.RWMutex
}

// NewSyncPool creates a new empty SyncPool.
// Both Enabled and AutoSync default to false for safety.
func NewSyncPool() *SyncPool {
	return &SyncPool{
		Machines: make(map[string]*Machine),
		Enabled:  false,
		AutoSync: false,
	}
}

// poolPath returns the path to the pool configuration file.
func poolPath() string {
	return filepath.Join(SyncDataDir(), poolFileName)
}

// SetBasePath sets the base directory for pool storage.
// This allows tests to use a custom directory instead of the global system path.
func (p *SyncPool) SetBasePath(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.basePath = path
}

// getPoolPath returns the path to use for this pool's file.
// Uses basePath if set, otherwise falls back to the global poolPath().
func (p *SyncPool) getPoolPath() string {
	if p.basePath != "" {
		return filepath.Join(p.basePath, poolFileName)
	}
	return poolPath()
}

// AddMachine adds a machine to the pool.
// Returns an error if a machine with the same ID already exists.
func (p *SyncPool) AddMachine(m *Machine) error {
	if err := m.Validate(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Machines == nil {
		p.Machines = make(map[string]*Machine)
	}

	if _, exists := p.Machines[m.ID]; exists {
		return fmt.Errorf("machine with ID %s already exists", m.ID)
	}

	// Check for duplicate name
	for _, existing := range p.Machines {
		if strings.EqualFold(existing.Name, m.Name) {
			return fmt.Errorf("machine with name %q already exists", m.Name)
		}
	}

	p.Machines[m.ID] = m
	return nil
}

// RemoveMachine removes a machine from the pool by ID.
// Returns an error if the machine is not found.
func (p *SyncPool) RemoveMachine(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.Machines[id]; !exists {
		return fmt.Errorf("machine with ID %s not found", id)
	}

	delete(p.Machines, id)
	return nil
}

// GetMachine returns a machine by ID, or nil if not found.
func (p *SyncPool) GetMachine(id string) *Machine {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.Machines[id]
}

// GetMachineByName returns a machine by name (case-insensitive), or nil if not found.
func (p *SyncPool) GetMachineByName(name string) *Machine {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, m := range p.Machines {
		if strings.EqualFold(m.Name, name) {
			return m
		}
	}
	return nil
}

// ListMachines returns all machines in the pool, sorted by name.
func (p *SyncPool) ListMachines() []*Machine {
	p.mu.RLock()
	defer p.mu.RUnlock()

	machines := make([]*Machine, 0, len(p.Machines))
	for _, m := range p.Machines {
		machines = append(machines, m)
	}

	// Sort by name for consistent ordering
	sort.Slice(machines, func(i, j int) bool {
		return strings.ToLower(machines[i].Name) < strings.ToLower(machines[j].Name)
	})

	return machines
}

// MachineCount returns the number of machines in the pool.
func (p *SyncPool) MachineCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.Machines)
}

// IsEmpty returns true if the pool has no machines.
func (p *SyncPool) IsEmpty() bool {
	return p.MachineCount() == 0
}

// Enable enables sync for this pool.
func (p *SyncPool) Enable() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Enabled = true
}

// Disable disables sync for this pool.
func (p *SyncPool) Disable() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Enabled = false
}

// EnableAutoSync enables automatic sync after backup/refresh.
func (p *SyncPool) EnableAutoSync() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.AutoSync = true
}

// DisableAutoSync disables automatic sync.
func (p *SyncPool) DisableAutoSync() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.AutoSync = false
}

// RecordFullSync records a successful full sync.
func (p *SyncPool) RecordFullSync() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.LastFullSync = time.Now()
}

// Save writes the pool configuration to disk atomically.
func (p *SyncPool) Save() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	path := p.getPoolPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pool: %w", err)
	}

	// Atomic write: write to temp file then rename
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

// Load reads the pool configuration from disk.
// Returns a new empty pool if the file doesn't exist.
func (p *SyncPool) Load() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	path := p.getPoolPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No pool file yet - initialize with defaults
			p.Machines = make(map[string]*Machine)
			p.Enabled = false
			p.AutoSync = false
			return nil
		}
		return fmt.Errorf("read pool: %w", err)
	}

	// Unmarshal into a temporary struct to preserve the lock
	var loaded SyncPool
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parse pool: %w", err)
	}

	// Copy fields
	p.LocalMachineID = loaded.LocalMachineID
	p.Machines = loaded.Machines
	p.Enabled = loaded.Enabled
	p.AutoSync = loaded.AutoSync
	p.LastFullSync = loaded.LastFullSync

	// Ensure map is initialized
	if p.Machines == nil {
		p.Machines = make(map[string]*Machine)
	}

	return nil
}

// LoadSyncPool loads a pool from disk or returns a new empty pool.
func LoadSyncPool() (*SyncPool, error) {
	pool := NewSyncPool()
	if err := pool.Load(); err != nil {
		return nil, err
	}
	return pool, nil
}

// OnlineMachines returns machines with status online.
func (p *SyncPool) OnlineMachines() []*Machine {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var machines []*Machine
	for _, m := range p.Machines {
		if m.Status == StatusOnline {
			machines = append(machines, m)
		}
	}
	return machines
}

// OfflineMachines returns machines with status offline or error.
func (p *SyncPool) OfflineMachines() []*Machine {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var machines []*Machine
	for _, m := range p.Machines {
		if m.Status == StatusOffline || m.Status == StatusError {
			machines = append(machines, m)
		}
	}
	return machines
}

// UpdateMachine updates an existing machine in the pool.
// Returns an error if the machine is not found.
func (p *SyncPool) UpdateMachine(m *Machine) error {
	if err := m.Validate(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.Machines[m.ID]; !exists {
		return fmt.Errorf("machine with ID %s not found", m.ID)
	}

	p.Machines[m.ID] = m
	return nil
}
