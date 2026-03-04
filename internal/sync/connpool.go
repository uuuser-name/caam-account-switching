package sync

import (
	"sync"
)

// ConnectionPool manages a pool of SSH connections.
type ConnectionPool struct {
	clients map[string]*SSHClient
	mu      sync.RWMutex
	opts    ConnectOptions
}

// NewConnectionPool creates a new connection pool with the given options.
func NewConnectionPool(opts ConnectOptions) *ConnectionPool {
	return &ConnectionPool{
		clients: make(map[string]*SSHClient),
		opts:    opts,
	}
}

// Get returns a connected SSH client for the given machine.
// If a connection already exists, it is reused.
func (p *ConnectionPool) Get(machine *Machine) (*SSHClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check for existing connection
	if client, exists := p.clients[machine.ID]; exists {
		if client.IsConnected() {
			return client, nil
		}
		// Connection exists but is dead - clean it up before creating new one
		client.Disconnect()
		delete(p.clients, machine.ID)
	}

	// Create new connection
	client := NewSSHClient(machine)
	if err := client.Connect(p.opts); err != nil {
		return nil, err
	}

	p.clients[machine.ID] = client
	return client, nil
}

// Release disconnects and removes a client from the pool.
func (p *ConnectionPool) Release(machineID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, exists := p.clients[machineID]; exists {
		client.Disconnect()
		delete(p.clients, machineID)
	}
}

// CloseAll disconnects all clients in the pool.
func (p *ConnectionPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, client := range p.clients {
		client.Disconnect()
		delete(p.clients, id)
	}
}

// Size returns the number of active connections in the pool.
func (p *ConnectionPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.clients)
}

// IsConnected checks if a machine has an active connection in the pool.
func (p *ConnectionPool) IsConnected(machineID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	client, exists := p.clients[machineID]
	return exists && client.IsConnected()
}

// Refresh disconnects and reconnects all clients.
func (p *ConnectionPool) Refresh() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for _, client := range p.clients {
		client.Disconnect()
		if err := client.Connect(p.opts); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// ConnectedMachines returns the IDs of all connected machines.
func (p *ConnectionPool) ConnectedMachines() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var ids []string
	for id, client := range p.clients {
		if client.IsConnected() {
			ids = append(ids, id)
		}
	}
	return ids
}
