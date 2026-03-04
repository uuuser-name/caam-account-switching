package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// LocalIdentity represents this machine's unique identity in the sync network.
type LocalIdentity struct {
	// ID is a unique identifier (UUID) for this machine.
	ID string `json:"id"`

	// Hostname is the machine's hostname at identity creation time.
	Hostname string `json:"hostname"`

	// CreatedAt is when this identity was first created.
	CreatedAt time.Time `json:"created_at"`
}

// identityFileName is the name of the identity file.
const identityFileName = "identity.json"

// SyncDataDir returns the path to the sync data directory.
// Uses CAAM_HOME/data if set, otherwise XDG_DATA_HOME/caam/sync.
func SyncDataDir() string {
	if caamHome := os.Getenv("CAAM_HOME"); caamHome != "" {
		return filepath.Join(caamHome, "data", "sync")
	}
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "caam", "sync")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory - unusual but handles edge cases
		return filepath.Join(".local", "share", "caam", "sync")
	}
	return filepath.Join(homeDir, ".local", "share", "caam", "sync")
}

// identityPath returns the path to the identity file.
func identityPath() string {
	return filepath.Join(SyncDataDir(), identityFileName)
}

// GetOrCreateLocalIdentity returns the local machine's identity,
// creating one if it doesn't exist.
func GetOrCreateLocalIdentity() (*LocalIdentity, error) {
	path := identityPath()

	// Try to load existing identity
	identity, err := loadIdentity(path)
	if err == nil {
		return identity, nil
	}

	// If file doesn't exist, create new identity
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read identity: %w", err)
	}

	// Create new identity
	identity, err = createIdentity()
	if err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}

	// Save it
	if err := saveIdentity(path, identity); err != nil {
		return nil, fmt.Errorf("save identity: %w", err)
	}

	return identity, nil
}

// LoadLocalIdentity loads the local identity without creating one.
// Returns nil if no identity exists.
func LoadLocalIdentity() (*LocalIdentity, error) {
	identity, err := loadIdentity(identityPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	return identity, err
}

// createIdentity generates a new local identity.
func createIdentity() (*LocalIdentity, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return &LocalIdentity{
		ID:        uuid.New().String(),
		Hostname:  hostname,
		CreatedAt: time.Now(),
	}, nil
}

// loadIdentity reads an identity from the given path.
func loadIdentity(path string) (*LocalIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var identity LocalIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}

	return &identity, nil
}

// saveIdentity writes an identity to the given path atomically.
func saveIdentity(path string, identity *LocalIdentity) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal identity: %w", err)
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
