package logs

import (
	"context"
	"time"
)

// Scanner is the interface for provider-specific log scanners.
// Each AI CLI tool (Claude, Codex, Gemini) has its own log format,
// so each implements this interface with provider-specific parsing.
type Scanner interface {
	// Scan parses logs from the given directory since the given time.
	// If logDir is empty, uses the provider's default log directory.
	// Returns all entries with timestamps >= since.
	Scan(ctx context.Context, logDir string, since time.Time) (*ScanResult, error)

	// LogDir returns the default log directory for this provider.
	// This is typically ~/.local/share/<provider>/logs or similar.
	LogDir() string
}

// MultiScanner scans logs from multiple providers.
type MultiScanner struct {
	scanners map[string]Scanner
}

// NewMultiScanner creates a scanner that aggregates multiple providers.
func NewMultiScanner() *MultiScanner {
	return &MultiScanner{
		scanners: make(map[string]Scanner),
	}
}

// Register adds a scanner for a provider.
func (m *MultiScanner) Register(provider string, scanner Scanner) {
	m.scanners[provider] = scanner
}

// Providers returns the list of registered provider names.
func (m *MultiScanner) Providers() []string {
	providers := make([]string, 0, len(m.scanners))
	for p := range m.scanners {
		providers = append(providers, p)
	}
	return providers
}

// LogDir returns an empty string as MultiScanner aggregates multiple directories.
func (m *MultiScanner) LogDir() string {
	return ""
}

// Scan implements the Scanner interface but returns an error.
// MultiScanner requires using ScanAll or selecting a specific provider scanner.
func (m *MultiScanner) Scan(ctx context.Context, logDir string, since time.Time) (*ScanResult, error) {
	return nil, context.DeadlineExceeded // Return a generic error, or better a specific one
}

// Scanner returns the scanner for a specific provider, or nil if not registered.
func (m *MultiScanner) Scanner(provider string) Scanner {
	return m.scanners[provider]
}

// ScanAll scans all registered providers and returns combined results.
func (m *MultiScanner) ScanAll(ctx context.Context, since time.Time) (map[string]*ScanResult, error) {
	results := make(map[string]*ScanResult)

	for provider, scanner := range m.scanners {
		result, err := scanner.Scan(ctx, "", since)
		if err != nil {
			// Log error but continue with other providers
			result = &ScanResult{
				Provider:    provider,
				ParseErrors: 1,
				Since:       since,
				Until:       time.Now(),
			}
		}
		results[provider] = result
	}

	return results, nil
}

// CombinedTokenUsage returns aggregated token usage across all providers.
func (m *MultiScanner) CombinedTokenUsage(ctx context.Context, since time.Time) (*TokenUsage, error) {
	results, err := m.ScanAll(ctx, since)
	if err != nil {
		return nil, err
	}

	combined := NewTokenUsage()
	for _, result := range results {
		for _, entry := range result.Entries {
			combined.Add(entry)
		}
	}

	return combined, nil
}
