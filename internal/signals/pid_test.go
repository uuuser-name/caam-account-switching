package signals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFileRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "caam.pid")

	if err := WritePIDFile(pidPath, 12345); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Fatalf("pid=%d, want 12345", pid)
	}

	if err := RemovePIDFile(pidPath); err != nil {
		t.Fatalf("RemovePIDFile: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed, stat err=%v", err)
	}
}

func TestDefaultPIDFilePathUsesCAAMHome(t *testing.T) {
	orig := os.Getenv("CAAM_HOME")
	defer os.Setenv("CAAM_HOME", orig)

	tmpDir := t.TempDir()
	os.Setenv("CAAM_HOME", tmpDir)

	got := DefaultPIDFilePath()
	want := filepath.Join(tmpDir, "caam.pid")
	if got != want {
		t.Fatalf("DefaultPIDFilePath=%q, want %q", got, want)
	}
}

func TestDefaultPIDFilePathWithoutCAAMHome(t *testing.T) {
	orig := os.Getenv("CAAM_HOME")
	defer os.Setenv("CAAM_HOME", orig)

	os.Unsetenv("CAAM_HOME")

	got := DefaultPIDFilePath()
	// Should end with caam.pid
	if !filepath.IsAbs(got) && got != filepath.Join(".caam", "caam.pid") {
		if filepath.Base(got) != "caam.pid" {
			t.Fatalf("DefaultPIDFilePath=%q should end with caam.pid", got)
		}
	}
}

func TestWritePIDFileCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "subdir", "nested", "caam.pid")

	if err := WritePIDFile(pidPath, 99999); err != nil {
		t.Fatalf("WritePIDFile with nested dir: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Fatal("PID file was not created")
	}

	// Verify content
	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 99999 {
		t.Fatalf("pid=%d, want 99999", pid)
	}
}

func TestReadPIDFileNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	_, err := ReadPIDFile(pidPath)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestRemovePIDFileNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	// Should not error when file doesn't exist
	err := RemovePIDFile(pidPath)
	if err != nil {
		t.Fatalf("RemovePIDFile on nonexistent file should not error: %v", err)
	}
}

func TestReadPIDFileInvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "bad.pid")

	// Write invalid content
	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadPIDFile(pidPath)
	if err == nil {
		t.Fatal("expected error for invalid PID content")
	}
}

func TestWritePIDFileInvalidPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Test with zero PID
	if err := WritePIDFile(pidPath, 0); err == nil {
		t.Fatal("expected error for zero PID")
	}

	// Test with negative PID
	if err := WritePIDFile(pidPath, -1); err == nil {
		t.Fatal("expected error for negative PID")
	}
}

func TestWritePIDFileEmptyPath(t *testing.T) {
	// Set CAAM_HOME to temp directory to avoid writing to real home
	orig := os.Getenv("CAAM_HOME")
	defer os.Setenv("CAAM_HOME", orig)

	tmpDir := t.TempDir()
	os.Setenv("CAAM_HOME", tmpDir)

	// Empty path should use default
	if err := WritePIDFile("", 54321); err != nil {
		t.Fatalf("WritePIDFile with empty path: %v", err)
	}

	// Verify file exists at default path
	defaultPath := DefaultPIDFilePath()
	pid, err := ReadPIDFile(defaultPath)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 54321 {
		t.Fatalf("pid=%d, want 54321", pid)
	}
}

func TestReadPIDFileEmptyPath(t *testing.T) {
	// Set CAAM_HOME to temp directory
	orig := os.Getenv("CAAM_HOME")
	defer os.Setenv("CAAM_HOME", orig)

	tmpDir := t.TempDir()
	os.Setenv("CAAM_HOME", tmpDir)

	// First write a PID file
	if err := WritePIDFile("", 11111); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	// Read with empty path should use default
	pid, err := ReadPIDFile("")
	if err != nil {
		t.Fatalf("ReadPIDFile with empty path: %v", err)
	}
	if pid != 11111 {
		t.Fatalf("pid=%d, want 11111", pid)
	}
}

func TestRemovePIDFileEmptyPath(t *testing.T) {
	// Set CAAM_HOME to temp directory
	orig := os.Getenv("CAAM_HOME")
	defer os.Setenv("CAAM_HOME", orig)

	tmpDir := t.TempDir()
	os.Setenv("CAAM_HOME", tmpDir)

	// First write a PID file
	if err := WritePIDFile("", 22222); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	// Remove with empty path should use default
	if err := RemovePIDFile(""); err != nil {
		t.Fatalf("RemovePIDFile with empty path: %v", err)
	}

	// File should be gone
	defaultPath := DefaultPIDFilePath()
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatal("PID file should be removed")
	}
}

func TestReadPIDFileZeroPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zero.pid")

	// Write zero PID
	if err := os.WriteFile(pidPath, []byte("0\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadPIDFile(pidPath)
	if err == nil {
		t.Fatal("expected error for zero PID in file")
	}
}

func TestReadPIDFileNegativePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "negative.pid")

	// Write negative PID
	if err := os.WriteFile(pidPath, []byte("-123\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadPIDFile(pidPath)
	if err == nil {
		t.Fatal("expected error for negative PID in file")
	}
}

func TestWritePIDFileOverwritesStale(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "stale.pid")

	// Write a stale PID (a PID that's very likely not running)
	// Use a very high PID that's unlikely to exist
	staleContent := []byte("999999999\n")
	if err := os.WriteFile(pidPath, staleContent, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Now write our PID, which should succeed (overwriting stale)
	if err := WritePIDFile(pidPath, 12345); err != nil {
		t.Fatalf("WritePIDFile should overwrite stale PID: %v", err)
	}

	// Verify our PID is written
	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Fatalf("pid=%d, want 12345", pid)
	}
}

func TestWritePIDFileSamePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "same.pid")

	// Write a PID
	if err := WritePIDFile(pidPath, 77777); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	// Write the same PID again - should succeed
	if err := WritePIDFile(pidPath, 77777); err != nil {
		t.Fatalf("WritePIDFile with same PID should succeed: %v", err)
	}

	// Verify PID
	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 77777 {
		t.Fatalf("pid=%d, want 77777", pid)
	}
}
