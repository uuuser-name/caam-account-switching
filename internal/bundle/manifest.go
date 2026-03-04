// Package bundle provides export/import functionality for vault bundles.
//
// A bundle is a portable zip file containing vault profiles, configuration,
// and metadata that can be transferred between machines. Bundles include:
//   - A manifest describing contents and checksums
//   - Vault profiles with auth tokens
//   - Optional configuration, project associations, and health data
//   - Optional AES-256-GCM encryption for security
package bundle

import (
	"encoding/base64"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/version"
)

// CurrentSchemaVersion is the current manifest schema version.
const CurrentSchemaVersion = 1

// BundleFileExtension is the standard file extension for bundles.
const BundleFileExtension = ".zip"

// EncryptedBundleMarker is the extension added before .zip for encrypted bundles.
const EncryptedBundleMarker = ".enc"

// ManifestFileName is the name of the manifest file within the bundle.
const ManifestFileName = "manifest.json"

// ManifestV1 is the bundle manifest schema version 1.
// It describes the contents, origin, and integrity information of a bundle.
type ManifestV1 struct {
	// SchemaVersion identifies the manifest format version.
	SchemaVersion int `json:"schema_version"`

	// CAAMVersion is the version of caam that created this bundle.
	CAAMVersion string `json:"caam_version"`

	// ExportTimestamp is when the bundle was created (RFC3339).
	ExportTimestamp time.Time `json:"export_timestamp"`

	// ExportTimestampHuman is a human-readable timestamp string.
	ExportTimestampHuman string `json:"export_timestamp_human"`

	// Source contains information about the exporting machine.
	Source SourceInfo `json:"source"`

	// Contents describes what is included in the bundle.
	Contents ContentsInfo `json:"contents"`

	// Checksums contains integrity hashes for bundle files.
	Checksums ChecksumInfo `json:"checksums"`
}

// SourceInfo contains information about the machine that created the bundle.
type SourceInfo struct {
	// Hostname is the machine's hostname.
	Hostname string `json:"hostname"`

	// Platform is the OS (darwin, linux, windows).
	Platform string `json:"platform"`

	// Arch is the CPU architecture (amd64, arm64).
	Arch string `json:"arch"`

	// Username is the user who created the bundle.
	Username string `json:"username"`

	// CAAMDataPath is the local path to caam data.
	CAAMDataPath string `json:"caam_data_path"`
}

// ContentsInfo describes what is included in the bundle.
type ContentsInfo struct {
	// Vault describes the vault profiles included.
	Vault VaultContents `json:"vault"`

	// Config describes included configuration.
	Config OptionalContent `json:"config"`

	// Projects describes included project associations.
	Projects OptionalContent `json:"projects"`

	// Health describes included health metadata.
	Health OptionalContent `json:"health"`

	// Database describes the included activity database.
	Database OptionalContent `json:"database"`

	// SyncConfig describes included sync pool configuration.
	SyncConfig OptionalContent `json:"sync_config"`
}

// VaultContents describes the vault profiles in the bundle.
type VaultContents struct {
	// Included indicates if the vault is in the bundle.
	Included bool `json:"included"`

	// Profiles maps provider names to lists of profile names.
	Profiles map[string][]string `json:"profiles"`

	// TotalProfiles is the total count of profiles.
	TotalProfiles int `json:"total_profiles"`
}

// OptionalContent describes an optional component of the bundle.
type OptionalContent struct {
	// Included indicates if this content is in the bundle.
	Included bool `json:"included"`

	// Path is the relative path within the bundle (if included).
	Path string `json:"path,omitempty"`

	// Reason explains why content was excluded (if not included).
	Reason string `json:"reason,omitempty"`

	// Note provides additional context about the content.
	Note string `json:"note,omitempty"`

	// Count is an optional count (e.g., association count for projects).
	Count int `json:"count,omitempty"`
}

// ChecksumInfo contains integrity verification data.
type ChecksumInfo struct {
	// Algorithm is the hash algorithm used (e.g., "sha256").
	Algorithm string `json:"algorithm"`

	// Files maps relative paths to their hex-encoded checksums.
	Files map[string]string `json:"files"`
}

// EncryptionMetadata describes how a bundle was encrypted.
type EncryptionMetadata struct {
	// Version is the encryption metadata format version.
	Version int `json:"version"`

	// Algorithm is the encryption algorithm (e.g., "aes-256-gcm").
	Algorithm string `json:"algorithm"`

	// KDF is the key derivation function (e.g., "argon2id").
	KDF string `json:"kdf"`

	// Salt is the base64-encoded salt used for key derivation.
	Salt string `json:"salt"`

	// Nonce is the base64-encoded nonce/IV for the cipher.
	Nonce string `json:"nonce"`

	// Argon2Params contains Argon2 parameters (if KDF is argon2id).
	Argon2Params *Argon2Params `json:"argon2_params,omitempty"`
}

// Argon2Params contains Argon2id parameters for key derivation.
type Argon2Params struct {
	// Time is the number of iterations.
	Time uint32 `json:"time"`

	// Memory is the memory size in KiB.
	Memory uint32 `json:"memory"`

	// Threads is the parallelism factor.
	Threads uint8 `json:"threads"`

	// KeyLen is the derived key length in bytes.
	KeyLen uint32 `json:"key_len"`
}

// DefaultArgon2Params returns recommended Argon2id parameters.
// These are balanced for security and performance on modern hardware.
func DefaultArgon2Params() *Argon2Params {
	return &Argon2Params{
		Time:    3,         // 3 iterations
		Memory:  64 * 1024, // 64 MiB
		Threads: 4,         // 4 threads
		KeyLen:  32,        // 256-bit key for AES-256
	}
}

// NewManifest creates a new ManifestV1 with default values.
func NewManifest() *ManifestV1 {
	now := time.Now()
	return &ManifestV1{
		SchemaVersion:        CurrentSchemaVersion,
		CAAMVersion:          version.Short(),
		ExportTimestamp:      now,
		ExportTimestampHuman: now.Format("January 2, 2006 at 3:04 PM"),
		Contents: ContentsInfo{
			Vault: VaultContents{
				Profiles: make(map[string][]string),
			},
		},
		Checksums: ChecksumInfo{
			Algorithm: "sha256",
			Files:     make(map[string]string),
		},
	}
}

// NewEncryptionMetadata creates new EncryptionMetadata with the given salt and nonce.
// Both salt and nonce are base64-encoded for JSON serialization.
func NewEncryptionMetadata(salt, nonce []byte) *EncryptionMetadata {
	return &EncryptionMetadata{
		Version:      1,
		Algorithm:    "aes-256-gcm",
		KDF:          "argon2id",
		Salt:         base64.StdEncoding.EncodeToString(salt),
		Nonce:        base64.StdEncoding.EncodeToString(nonce),
		Argon2Params: DefaultArgon2Params(),
	}
}

// NewEncryptionMetadataDefaults creates new EncryptionMetadata with empty salt/nonce
// for cases where the caller will set these values later.
func NewEncryptionMetadataDefaults() *EncryptionMetadata {
	return &EncryptionMetadata{
		Version:      1,
		Algorithm:    "aes-256-gcm",
		KDF:          "argon2id",
		Argon2Params: DefaultArgon2Params(),
	}
}

// AddProfile adds a profile to the manifest's vault contents.
func (m *ManifestV1) AddProfile(provider, profileName string) {
	if m.Contents.Vault.Profiles == nil {
		m.Contents.Vault.Profiles = make(map[string][]string)
	}
	m.Contents.Vault.Profiles[provider] = append(m.Contents.Vault.Profiles[provider], profileName)
	m.Contents.Vault.TotalProfiles++
	m.Contents.Vault.Included = true
}

// AddChecksum adds a file checksum to the manifest.
func (m *ManifestV1) AddChecksum(path, checksum string) {
	if m.Checksums.Files == nil {
		m.Checksums.Files = make(map[string]string)
	}
	m.Checksums.Files[path] = checksum
}

// SetConfig configures the config content entry.
func (m *ManifestV1) SetConfig(included bool, path string) {
	m.Contents.Config = OptionalContent{
		Included: included,
		Path:     path,
	}
	if !included {
		m.Contents.Config.Reason = "not included in export"
	}
}

// SetProjects configures the projects content entry.
func (m *ManifestV1) SetProjects(included bool, path string, count int) {
	m.Contents.Projects = OptionalContent{
		Included: included,
		Path:     path,
		Count:    count,
	}
	if !included {
		m.Contents.Projects.Reason = "not included in export"
	}
}

// SetHealth configures the health content entry.
func (m *ManifestV1) SetHealth(included bool, path string) {
	m.Contents.Health = OptionalContent{
		Included: included,
		Path:     path,
	}
	if !included {
		m.Contents.Health.Reason = "not included in export"
	}
}

// SetDatabase configures the database content entry.
func (m *ManifestV1) SetDatabase(included bool, path string) {
	m.Contents.Database = OptionalContent{
		Included: included,
		Path:     path,
	}
	if !included {
		m.Contents.Database.Reason = "user opted out"
	}
}

// SetSyncConfig configures the sync config content entry.
func (m *ManifestV1) SetSyncConfig(included bool, path string) {
	m.Contents.SyncConfig = OptionalContent{
		Included: included,
		Path:     path,
		Note:     "Sync pool and machine configuration",
	}
	if !included {
		m.Contents.SyncConfig.Reason = "not included in export"
	}
}
