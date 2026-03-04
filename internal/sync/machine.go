// Package sync provides multi-machine vault synchronization capabilities.
//
// This package implements the infrastructure for syncing authentication tokens
// across multiple machines using SSH transport. It provides:
//   - Machine identity and discovery (SSH config, CSV file)
//   - Sync pool management
//   - State persistence
//   - Local machine identity generation
package sync

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Machine status constants.
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusSyncing = "syncing"
	StatusError   = "error"
	StatusUnknown = "unknown"
)

// Machine source constants.
const (
	SourceSSHConfig = "ssh_config"
	SourceCSV       = "csv"
	SourceManual    = "manual"
)

// DefaultSSHPort is the default SSH port.
const DefaultSSHPort = 22

// Machine represents a remote machine in the sync pool.
type Machine struct {
	// ID is a unique identifier for this machine (UUID).
	ID string `json:"id"`

	// Name is a friendly name for this machine.
	Name string `json:"name"`

	// Address is the IP address or hostname.
	Address string `json:"address"`

	// Port is the SSH port (default: 22).
	Port int `json:"port"`

	// SSHUser is the SSH username (default: current user).
	SSHUser string `json:"ssh_user,omitempty"`

	// SSHKeyPath is the path to the SSH private key.
	SSHKeyPath string `json:"ssh_key_path,omitempty"`

	// RemotePath is the path to caam data on the remote machine.
	// If empty, defaults to the same path as local data.
	RemotePath string `json:"remote_path,omitempty"`

	// Status is the current connection status.
	Status string `json:"status"`

	// LastSync is the timestamp of the last successful sync.
	LastSync time.Time `json:"last_sync,omitempty"`

	// LastError is the last error message if Status is "error".
	LastError string `json:"last_error,omitempty"`

	// LastErrorAt is when the last error occurred.
	LastErrorAt time.Time `json:"last_error_at,omitempty"`

	// AddedAt is when this machine was added to the pool.
	AddedAt time.Time `json:"added_at"`

	// Source indicates where this machine definition came from.
	Source string `json:"source"`
}

// NewMachine creates a new Machine with a generated UUID.
func NewMachine(name, address string) *Machine {
	return &Machine{
		ID:      uuid.New().String(),
		Name:    name,
		Address: address,
		Port:    DefaultSSHPort,
		Status:  StatusUnknown,
		AddedAt: time.Now(),
		Source:  SourceManual,
	}
}

// HostPort returns the address:port string for SSH connection.
func (m *Machine) HostPort() string {
	port := m.Port
	if port == 0 {
		port = DefaultSSHPort
	}
	host := strings.TrimSpace(m.Address)
	// Accept both bare IPv6 and already-bracketed IPv6 input.
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && len(host) > 2 {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// SetError updates the machine status to error with the given message.
func (m *Machine) SetError(msg string) {
	m.Status = StatusError
	m.LastError = msg
	m.LastErrorAt = time.Now()
}

// SetOnline updates the machine status to online.
func (m *Machine) SetOnline() {
	m.Status = StatusOnline
	m.LastError = ""
}

// SetOffline updates the machine status to offline.
func (m *Machine) SetOffline() {
	m.Status = StatusOffline
}

// RecordSync records a successful sync.
func (m *Machine) RecordSync() {
	m.Status = StatusOnline
	m.LastSync = time.Now()
	m.LastError = ""
}

// Validate checks if the machine has required fields.
func (m *Machine) Validate() error {
	if m.Name == "" {
		return &ValidationError{Field: "name", Message: "machine name is required"}
	}
	if m.Address == "" {
		return &ValidationError{Field: "address", Message: "machine address is required"}
	}
	return nil
}

// ValidationError represents a validation error for a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ParseAddress parses an address string that may contain user, host, and port.
// Supported formats:
//   - "hostname"
//   - "hostname:port"
//   - "user@hostname"
//   - "user@hostname:port"
//
// Returns the host, port (0 if not specified), and user (empty if not specified).
func ParseAddress(addr string) (host string, port int, user string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, ""
	}

	// Check for user@ prefix
	if atIdx := strings.Index(addr, "@"); atIdx != -1 {
		user = addr[:atIdx]
		addr = addr[atIdx+1:]
	}

	// Check for :port suffix
	// Handle IPv6 addresses in brackets: [::1]:22
	if strings.HasPrefix(addr, "[") {
		// IPv6 format
		if bracketEnd := strings.Index(addr, "]"); bracketEnd != -1 {
			host = addr[1:bracketEnd]
			remaining := addr[bracketEnd+1:]
			if strings.HasPrefix(remaining, ":") {
				if p, err := strconv.Atoi(remaining[1:]); err == nil {
					port = p
				}
			}
		} else {
			// Malformed, just use as-is
			host = addr
		}
	} else {
		// IPv4 or hostname
		// Count colons to distinguish "host:port" from IPv6 without brackets
		colonCount := strings.Count(addr, ":")
		if colonCount == 1 {
			// Single colon = host:port
			parts := strings.SplitN(addr, ":", 2)
			host = parts[0]
			if p, err := strconv.Atoi(parts[1]); err == nil {
				port = p
			}
		} else {
			// No colon or multiple colons (bare IPv6) = just host
			host = addr
		}
	}

	return host, port, user
}

// NormalizeAddress constructs a normalized address string from components.
// The returned format is "user@host:port" with optional parts omitted.
func NormalizeAddress(host string, port int, user string) string {
	var sb strings.Builder

	if user != "" {
		sb.WriteString(user)
		sb.WriteString("@")
	}

	sb.WriteString(host)

	if port != 0 && port != DefaultSSHPort {
		sb.WriteString(":")
		sb.WriteString(strconv.Itoa(port))
	}

	return sb.String()
}

// MachinesEqual checks if two machines represent the same remote host.
// Machines are considered equal if they have the same address and port.
func MachinesEqual(a, b *Machine) bool {
	if a == nil || b == nil {
		return a == b
	}

	portA := a.Port
	if portA == 0 {
		portA = DefaultSSHPort
	}
	portB := b.Port
	if portB == 0 {
		portB = DefaultSSHPort
	}

	return strings.EqualFold(a.Address, b.Address) && portA == portB
}
