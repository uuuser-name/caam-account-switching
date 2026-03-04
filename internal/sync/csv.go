package sync

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CSVFileName is the name of the sync machines CSV file.
const CSVFileName = "sync_machines.csv"

// csvTemplate is the template content for a new CSV file.
const csvTemplate = `# CAAM Sync Pool Configuration
# 
# Add machines you want to sync auth tokens with.
# 
# Format: machine_name,address,ssh_key_path
#   - address: IP or hostname, optionally with :port and/or user@
#   - ssh_key_path: path to SSH private key (~ expands to home)
#
# Examples:
#   work-laptop,192.168.1.100,~/.ssh/id_ed25519
#   home-desktop,10.0.0.50:2222,~/.ssh/home_key
#   cloud-vm,jeff@34.123.45.67,~/.ssh/cloud_rsa
#
machine_name,address,ssh_key_path
`

// CSVPath returns the path to the sync machines CSV file.
// Uses ~/.caam/sync_machines.csv
func CSVPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".caam", CSVFileName)
	}
	return filepath.Join(homeDir, ".caam", CSVFileName)
}

// EnsureCSVFile creates the CSV file if it doesn't exist.
// Returns true if the file was created, false if it already existed.
func EnsureCSVFile() (bool, error) {
	path := CSVPath()

	// Check if file exists
	if _, err := os.Stat(path); err == nil {
		return false, nil // Already exists
	} else if !os.IsNotExist(err) {
		return false, err
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, fmt.Errorf("create directory: %w", err)
	}

	// Create file with template using atomic write pattern
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return false, fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.WriteString(csvTemplate); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return false, fmt.Errorf("write temp file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return false, fmt.Errorf("sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return false, fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return false, fmt.Errorf("rename temp file: %w", err)
	}

	return true, nil
}

// LoadFromCSV reads machines from the CSV file.
// Returns nil if the file doesn't exist.
func LoadFromCSV() ([]*Machine, error) {
	path := CSVPath()

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var machines []*Machine
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip header line
		if strings.HasPrefix(strings.ToLower(line), "machine_name") {
			continue
		}

		// Parse CSV fields
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue // Need at least name and address
		}

		name := strings.TrimSpace(fields[0])
		address := strings.TrimSpace(fields[1])
		sshKeyPath := ""
		if len(fields) >= 3 {
			sshKeyPath = strings.TrimSpace(fields[2])
		}

		if name == "" || address == "" {
			continue
		}

		// Parse address for user@host:port
		host, port, user := ParseAddress(address)
		if host == "" {
			host = address
		}

		m := NewMachine(name, host)
		m.Source = SourceCSV

		if port != 0 {
			m.Port = port
		}
		if user != "" {
			m.SSHUser = user
		}
		if sshKeyPath != "" {
			m.SSHKeyPath = expandPath(sshKeyPath)
		}

		machines = append(machines, m)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return machines, nil
}

// SaveToCSV writes machines to the CSV file, preserving comments.
func SaveToCSV(machines []*Machine) error {
	path := CSVPath()

	// Read existing file to preserve comments
	existingLines, err := readExistingLines(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing file: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Build output
	var lines []string

	// Preserve header comments
	for _, line := range existingLines {
		if strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}

	// Add header if no comments found
	if len(lines) == 0 {
		lines = append(lines, "# CAAM Sync Pool Configuration")
		lines = append(lines, "#")
		lines = append(lines, "# Format: machine_name,address,ssh_key_path")
		lines = append(lines, "#")
	}

	// Add header row
	lines = append(lines, "machine_name,address,ssh_key_path")

	// Add machines
	for _, m := range machines {
		address := NormalizeAddress(m.Address, m.Port, m.SSHUser)
		keyPath := m.SSHKeyPath
		if keyPath != "" {
			// Convert absolute path back to ~ if possible
			if homeDir, err := os.UserHomeDir(); err == nil {
				if strings.HasPrefix(keyPath, homeDir) {
					keyPath = "~" + keyPath[len(homeDir):]
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s,%s,%s", m.Name, address, keyPath))
	}

	// Write file atomically: open, write, fsync, close, rename
	tmpPath := path + ".tmp"
	content := strings.Join(lines, "\n") + "\n"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.WriteString(content); err != nil {
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

// readExistingLines reads all lines from a file.
func readExistingLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

// InitCSVWithDiscovery creates the CSV file and optionally populates it
// with machines discovered from SSH config.
func InitCSVWithDiscovery(includeSSHConfig bool) (created bool, machines []*Machine, err error) {
	created, err = EnsureCSVFile()
	if err != nil {
		return false, nil, err
	}

	if includeSSHConfig {
		discovered, err := DiscoverFromSSHConfig()
		if err != nil {
			// Non-fatal - continue without SSH config machines
			discovered = nil
		}

		if len(discovered) > 0 {
			if err := SaveToCSV(discovered); err != nil {
				return created, nil, fmt.Errorf("save discovered machines: %w", err)
			}
			return created, discovered, nil
		}
	}

	return created, nil, nil
}
