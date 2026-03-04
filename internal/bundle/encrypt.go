package bundle

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

// EncryptionMarkerFile is the name of the file that indicates a bundle is encrypted.
const EncryptionMarkerFile = ".caam_encrypted"

// EncryptedPayloadFile is the name of the encrypted payload file.
const EncryptedPayloadFile = "payload.enc"

// NonceSize is the size of the GCM nonce in bytes.
const NonceSize = 12

// SaltSize is the size of the salt in bytes.
const SaltSize = 32

// EncryptBundle encrypts data using AES-256-GCM with Argon2id key derivation.
// Returns the encrypted data and metadata needed for decryption.
func EncryptBundle(plainData []byte, password string) ([]byte, *EncryptionMetadata, error) {
	if len(password) == 0 {
		return nil, nil, fmt.Errorf("password is required")
	}

	// Generate salt
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("generate salt: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Create encryption metadata
	params := DefaultArgon2Params()
	meta := &EncryptionMetadata{
		Version:      1,
		Algorithm:    "aes-256-gcm",
		KDF:          "argon2id",
		Salt:         base64.StdEncoding.EncodeToString(salt),
		Nonce:        base64.StdEncoding.EncodeToString(nonce),
		Argon2Params: params,
	}

	// Derive key using Argon2id
	key := argon2.IDKey(
		[]byte(password),
		salt,
		params.Time,
		params.Memory,
		params.Threads,
		params.KeyLen,
	)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create GCM: %w", err)
	}

	// Encrypt data
	ciphertext := gcm.Seal(nil, nonce, plainData, nil)

	return ciphertext, meta, nil
}

// DecryptBundle decrypts data using AES-256-GCM with the provided metadata.
func DecryptBundle(ciphertext []byte, meta *EncryptionMetadata, password string) ([]byte, error) {
	if meta == nil {
		return nil, fmt.Errorf("encryption metadata is required")
	}

	if err := ValidateEncryptionMetadata(meta); err != nil {
		return nil, fmt.Errorf("invalid encryption metadata: %w", err)
	}

	if len(password) == 0 {
		return nil, fmt.Errorf("password is required")
	}

	// Decode salt
	salt, err := base64.StdEncoding.DecodeString(meta.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	// Decode nonce
	nonce, err := base64.StdEncoding.DecodeString(meta.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}

	// Get Argon2 parameters
	params := meta.Argon2Params
	if params == nil {
		params = DefaultArgon2Params()
	}

	// Derive key using Argon2id
	key := argon2.IDKey(
		[]byte(password),
		salt,
		params.Time,
		params.Memory,
		params.Threads,
		params.KeyLen,
	)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	// Decrypt data
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w (wrong password?)", err)
	}

	return plaintext, nil
}

// IsEncrypted checks if a bundle at the given path is encrypted.
// It looks for the encryption marker file or the .enc filename marker.
func IsEncrypted(bundlePath string) (bool, error) {
	// Check if it's a directory (extracted bundle)
	info, err := os.Stat(bundlePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - check filename pattern
			return hasEncryptedFilename(bundlePath), nil
		}
		return false, err
	}

	if info.IsDir() {
		markerPath := filepath.Join(bundlePath, EncryptionMarkerFile)
		_, err := os.Stat(markerPath)
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// For files, check the filename for .enc marker
	return hasEncryptedFilename(bundlePath), nil
}

// hasEncryptedFilename checks if a path has the .enc.zip filename pattern.
func hasEncryptedFilename(bundlePath string) bool {
	base := filepath.Base(bundlePath)
	// Check for pattern: name.enc.zip
	encSuffix := EncryptedBundleMarker + BundleFileExtension // ".enc.zip"
	return len(base) > len(encSuffix) && base[len(base)-len(encSuffix):] == encSuffix
}

// LoadEncryptionMetadata loads encryption metadata from a bundle directory.
func LoadEncryptionMetadata(bundleDir string) (*EncryptionMetadata, error) {
	markerPath := filepath.Join(bundleDir, EncryptionMarkerFile)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Not encrypted
		}
		return nil, fmt.Errorf("read encryption metadata: %w", err)
	}

	var meta EncryptionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse encryption metadata: %w", err)
	}

	return &meta, nil
}

// SaveEncryptionMetadata saves encryption metadata to a bundle directory.
func SaveEncryptionMetadata(bundleDir string, meta *EncryptionMetadata) error {
	if err := ValidateEncryptionMetadata(meta); err != nil {
		return fmt.Errorf("invalid encryption metadata: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal encryption metadata: %w", err)
	}

	markerPath := filepath.Join(bundleDir, EncryptionMarkerFile)

	// Ensure directory exists
	if err := os.MkdirAll(bundleDir, 0700); err != nil {
		return fmt.Errorf("create bundle directory: %w", err)
	}

	// Atomic write
	tmpPath := markerPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp encryption file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp encryption file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp encryption file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp encryption file: %w", err)
	}

	if err := os.Rename(tmpPath, markerPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp encryption file: %w", err)
	}

	return nil
}

// GenerateRandomBytes generates cryptographically secure random bytes.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return b, nil
}

// SecureWipe attempts to overwrite sensitive data in memory.
// Note: This is best-effort; Go's GC may have copied the data elsewhere.
func SecureWipe(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
