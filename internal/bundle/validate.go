package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/version"
)

// ValidationError represents a bundle validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// ValidateManifest checks that a manifest is well-formed.
// It returns an error if any required fields are missing or invalid.
func ValidateManifest(m *ManifestV1) error {
	if m == nil {
		return &ValidationError{Message: "manifest is nil"}
	}

	// Check schema version
	if m.SchemaVersion < 1 {
		return &ValidationError{
			Field:   "schema_version",
			Message: "must be >= 1",
		}
	}

	// Check caam version
	if m.CAAMVersion == "" {
		return &ValidationError{
			Field:   "caam_version",
			Message: "is required",
		}
	}

	// Check export timestamp
	if m.ExportTimestamp.IsZero() {
		return &ValidationError{
			Field:   "export_timestamp",
			Message: "is required",
		}
	}

	// Validate source info
	if err := validateSourceInfo(&m.Source); err != nil {
		return err
	}

	// Validate checksums
	if err := validateChecksumInfo(&m.Checksums); err != nil {
		return err
	}

	return nil
}

// validateSourceInfo validates the source information.
func validateSourceInfo(s *SourceInfo) error {
	if s.Hostname == "" {
		return &ValidationError{
			Field:   "source.hostname",
			Message: "is required",
		}
	}

	validPlatforms := map[string]bool{
		"darwin":  true,
		"linux":   true,
		"windows": true,
		"":        true, // Allow empty for legacy/unknown
	}
	if !validPlatforms[s.Platform] {
		return &ValidationError{
			Field:   "source.platform",
			Message: fmt.Sprintf("invalid platform %q", s.Platform),
		}
	}

	validArchs := map[string]bool{
		"amd64": true,
		"arm64": true,
		"386":   true,
		"arm":   true,
		"":      true, // Allow empty for legacy/unknown
	}
	if !validArchs[s.Arch] {
		return &ValidationError{
			Field:   "source.arch",
			Message: fmt.Sprintf("invalid architecture %q", s.Arch),
		}
	}

	return nil
}

// validateChecksumInfo validates the checksum information.
func validateChecksumInfo(c *ChecksumInfo) error {
	validAlgorithms := map[string]bool{
		"sha256": true,
		"sha512": true,
		"":       true, // Allow empty (no checksums)
	}
	if !validAlgorithms[c.Algorithm] {
		return &ValidationError{
			Field:   "checksums.algorithm",
			Message: fmt.Sprintf("unsupported algorithm %q", c.Algorithm),
		}
	}

	// Validate checksum format (hex string of correct length)
	if c.Algorithm != "" && len(c.Files) > 0 {
		expectedLen := 64 // sha256 produces 64 hex chars
		if c.Algorithm == "sha512" {
			expectedLen = 128
		}

		for path, hash := range c.Files {
			if len(hash) != expectedLen {
				return &ValidationError{
					Field:   fmt.Sprintf("checksums.files[%q]", path),
					Message: fmt.Sprintf("invalid hash length: expected %d, got %d", expectedLen, len(hash)),
				}
			}
			// Validate hex characters
			for _, r := range hash {
				if !isHexChar(r) {
					return &ValidationError{
						Field:   fmt.Sprintf("checksums.files[%q]", path),
						Message: "contains non-hex characters",
					}
				}
			}
		}
	}

	return nil
}

// isHexChar returns true if r is a valid hexadecimal character.
func isHexChar(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// ValidateManifestPath checks that a path from the manifest is safe and stays
// within the bundle directory. Prevents path traversal attacks.
func ValidateManifestPath(basePath, relPath string) (string, error) {
	if relPath == "" {
		return "", &ValidationError{Message: "path is empty"}
	}

	// Reject absolute paths
	if filepath.IsAbs(relPath) {
		return "", &ValidationError{
			Field:   relPath,
			Message: "absolute paths are not allowed in manifest",
		}
	}

	// Reject paths that attempt traversal
	if strings.Contains(relPath, "..") {
		return "", &ValidationError{
			Field:   relPath,
			Message: "path traversal not allowed in manifest",
		}
	}

	// Join and clean the path
	fullPath := filepath.Join(basePath, relPath)

	// Verify the resolved path is within basePath
	cleanBase := filepath.Clean(basePath)
	cleanFull := filepath.Clean(fullPath)

	// Ensure the full path starts with the base path
	if !strings.HasPrefix(cleanFull, cleanBase+string(filepath.Separator)) &&
		cleanFull != cleanBase {
		return "", &ValidationError{
			Field:   relPath,
			Message: "path escapes bundle directory",
		}
	}

	return fullPath, nil
}

// IsCompatibleVersion checks if a manifest was created by a compatible caam version.
// Returns nil if compatible, or an error describing the incompatibility.
func IsCompatibleVersion(m *ManifestV1) error {
	if m == nil {
		return &ValidationError{Message: "manifest is nil"}
	}

	// Check schema version compatibility
	if m.SchemaVersion > CurrentSchemaVersion {
		return &ValidationError{
			Field: "schema_version",
			Message: fmt.Sprintf(
				"bundle uses schema v%d but this caam only supports up to v%d; please upgrade caam",
				m.SchemaVersion, CurrentSchemaVersion,
			),
		}
	}

	// Schema version 1 is always compatible with current implementation
	if m.SchemaVersion < 1 {
		return &ValidationError{
			Field:   "schema_version",
			Message: fmt.Sprintf("unknown schema version %d", m.SchemaVersion),
		}
	}

	// Parse and compare caam versions for warnings (not errors)
	// We allow importing from any caam version, but may warn about mismatches
	_ = version.Short() // Reference for future version comparison

	return nil
}

// ValidateEncryptionMetadata validates encryption metadata.
func ValidateEncryptionMetadata(e *EncryptionMetadata) error {
	if e == nil {
		return &ValidationError{Message: "encryption metadata is nil"}
	}

	if e.Version < 1 {
		return &ValidationError{
			Field:   "version",
			Message: "must be >= 1",
		}
	}

	if e.Algorithm != "aes-256-gcm" {
		return &ValidationError{
			Field:   "algorithm",
			Message: fmt.Sprintf("unsupported algorithm %q; only aes-256-gcm is supported", e.Algorithm),
		}
	}

	if e.KDF != "argon2id" {
		return &ValidationError{
			Field:   "kdf",
			Message: fmt.Sprintf("unsupported KDF %q; only argon2id is supported", e.KDF),
		}
	}

	if e.Salt == "" {
		return &ValidationError{
			Field:   "salt",
			Message: "is required",
		}
	}

	if e.Nonce == "" {
		return &ValidationError{
			Field:   "nonce",
			Message: "is required",
		}
	}

	if e.Argon2Params != nil {
		if err := validateArgon2Params(e.Argon2Params); err != nil {
			return err
		}
	}

	return nil
}

// validateArgon2Params validates Argon2 parameters.
func validateArgon2Params(p *Argon2Params) error {
	if p.Time < 1 {
		return &ValidationError{
			Field:   "argon2_params.time",
			Message: "must be >= 1",
		}
	}

	if p.Memory < 1024 { // Minimum 1 MiB
		return &ValidationError{
			Field:   "argon2_params.memory",
			Message: "must be >= 1024 (1 MiB)",
		}
	}

	if p.Threads < 1 {
		return &ValidationError{
			Field:   "argon2_params.threads",
			Message: "must be >= 1",
		}
	}

	if p.KeyLen < 16 || p.KeyLen > 64 {
		return &ValidationError{
			Field:   "argon2_params.key_len",
			Message: "must be between 16 and 64 bytes",
		}
	}

	return nil
}

// LoadManifest reads and validates a manifest from a bundle directory.
func LoadManifest(bundleDir string) (*ManifestV1, error) {
	manifestPath := filepath.Join(bundleDir, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m ManifestV1
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if err := ValidateManifest(&m); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	return &m, nil
}

// SaveManifest writes a manifest to a bundle directory.
func SaveManifest(bundleDir string, m *ManifestV1) error {
	if err := ValidateManifest(m); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(bundleDir, ManifestFileName)

	// Ensure directory exists
	if err := os.MkdirAll(bundleDir, 0700); err != nil {
		return fmt.Errorf("create bundle directory: %w", err)
	}

	// Atomic write
	tmpPath := manifestPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp manifest file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp manifest file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp manifest file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp manifest file: %w", err)
	}

	if err := os.Rename(tmpPath, manifestPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp manifest file: %w", err)
	}

	return nil
}

// NormalizePath converts a path to use forward slashes for portability.
// This ensures bundles created on Windows work on Unix and vice versa.
func NormalizePath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), "\\", "/")
}

// DenormalizePath converts a normalized path to the OS-specific format.
func DenormalizePath(path string) string {
	return filepath.FromSlash(path)
}
