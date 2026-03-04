package authfile

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkVaultBackup benchmarks the backup operation.
func BenchmarkVaultBackup(b *testing.B) {
	tmpDir := b.TempDir()
	vaultDir := filepath.Join(tmpDir, "vault")
	authDir := filepath.Join(tmpDir, "auth")

	// Create auth file
	if err := os.MkdirAll(authDir, 0700); err != nil {
		b.Fatal(err)
	}
	authFile := filepath.Join(authDir, "auth.json")
	content := []byte(`{"token": "secret123", "expires_at": "2025-12-31T23:59:59Z"}`)
	if err := os.WriteFile(authFile, content, 0600); err != nil {
		b.Fatal(err)
	}

	v := NewVault(vaultDir)
	fileSet := AuthFileSet{
		Tool: "testtool",
		Files: []AuthFileSpec{
			{Tool: "testtool", Path: authFile, Required: true},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profileName := "profile" + string(rune('0'+i%10))
		if err := v.Backup(fileSet, profileName); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVaultRestore benchmarks the restore operation.
func BenchmarkVaultRestore(b *testing.B) {
	tmpDir := b.TempDir()
	vaultDir := filepath.Join(tmpDir, "vault")
	authDir := filepath.Join(tmpDir, "auth")

	// Create auth file and backup first
	if err := os.MkdirAll(authDir, 0700); err != nil {
		b.Fatal(err)
	}
	authFile := filepath.Join(authDir, "auth.json")
	content := []byte(`{"token": "secret123", "expires_at": "2025-12-31T23:59:59Z"}`)
	if err := os.WriteFile(authFile, content, 0600); err != nil {
		b.Fatal(err)
	}

	v := NewVault(vaultDir)
	fileSet := AuthFileSet{
		Tool: "testtool",
		Files: []AuthFileSpec{
			{Tool: "testtool", Path: authFile, Required: true},
		},
	}

	// Create backup
	if err := v.Backup(fileSet, "benchprofile"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.Restore(fileSet, "benchprofile"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVaultList benchmarks listing profiles.
func BenchmarkVaultList(b *testing.B) {
	tmpDir := b.TempDir()
	vaultDir := filepath.Join(tmpDir, "vault")
	authDir := filepath.Join(tmpDir, "auth")

	// Create auth file
	if err := os.MkdirAll(authDir, 0700); err != nil {
		b.Fatal(err)
	}
	authFile := filepath.Join(authDir, "auth.json")
	content := []byte(`{"token": "secret123"}`)
	if err := os.WriteFile(authFile, content, 0600); err != nil {
		b.Fatal(err)
	}

	v := NewVault(vaultDir)
	fileSet := AuthFileSet{
		Tool: "testtool",
		Files: []AuthFileSpec{
			{Tool: "testtool", Path: authFile, Required: true},
		},
	}

	// Create multiple profiles
	for i := 0; i < 20; i++ {
		profileName := "profile" + string(rune('A'+i))
		if err := v.Backup(fileSet, profileName); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiles, err := v.List("testtool")
		if err != nil {
			b.Fatal(err)
		}
		if len(profiles) != 20 {
			b.Fatalf("expected 20 profiles, got %d", len(profiles))
		}
	}
}

// BenchmarkVaultActiveProfile benchmarks detecting the active profile via hashing.
func BenchmarkVaultActiveProfile(b *testing.B) {
	tmpDir := b.TempDir()
	vaultDir := filepath.Join(tmpDir, "vault")
	authDir := filepath.Join(tmpDir, "auth")

	// Create auth file
	if err := os.MkdirAll(authDir, 0700); err != nil {
		b.Fatal(err)
	}
	authFile := filepath.Join(authDir, "auth.json")
	// Larger content for more realistic benchmark
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte(i % 256)
	}
	if err := os.WriteFile(authFile, content, 0600); err != nil {
		b.Fatal(err)
	}

	v := NewVault(vaultDir)
	fileSet := AuthFileSet{
		Tool: "testtool",
		Files: []AuthFileSpec{
			{Tool: "testtool", Path: authFile, Required: true},
		},
	}

	// Create multiple backups
	for i := 0; i < 10; i++ {
		profileName := "profile" + string(rune('A'+i))
		if err := v.Backup(fileSet, profileName); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := v.ActiveProfile(fileSet)
		if err != nil {
			b.Fatal(err)
		}
	}
}
