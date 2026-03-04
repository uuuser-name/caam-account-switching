package signals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLogFilePath(t *testing.T) {
	t.Run("with CAAM_HOME", func(t *testing.T) {
		orig := os.Getenv("CAAM_HOME")
		defer os.Setenv("CAAM_HOME", orig)

		tmpDir := t.TempDir()
		os.Setenv("CAAM_HOME", tmpDir)

		got := DefaultLogFilePath()
		want := filepath.Join(tmpDir, "caam.log")
		if got != want {
			t.Fatalf("DefaultLogFilePath=%q, want %q", got, want)
		}
	})

	t.Run("without CAAM_HOME", func(t *testing.T) {
		orig := os.Getenv("CAAM_HOME")
		defer os.Setenv("CAAM_HOME", orig)

		os.Unsetenv("CAAM_HOME")

		got := DefaultLogFilePath()
		// Should be in home directory or fallback
		if !strings.HasSuffix(got, "caam.log") {
			t.Fatalf("DefaultLogFilePath=%q should end with caam.log", got)
		}
	})
}

func TestAppendLogLine(t *testing.T) {
	t.Run("basic append", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		// First line
		if err := AppendLogLine(logPath, "first message"); err != nil {
			t.Fatalf("AppendLogLine: %v", err)
		}

		// Second line
		if err := AppendLogLine(logPath, "second message"); err != nil {
			t.Fatalf("AppendLogLine (second): %v", err)
		}

		content, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		// Check content contains both messages
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(content))
		}

		if !strings.Contains(lines[0], "first message") {
			t.Errorf("first line should contain 'first message': %q", lines[0])
		}
		if !strings.Contains(lines[1], "second message") {
			t.Errorf("second line should contain 'second message': %q", lines[1])
		}
	})

	t.Run("creates parent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "subdir", "nested", "test.log")

		if err := AppendLogLine(logPath, "test message"); err != nil {
			t.Fatalf("AppendLogLine: %v", err)
		}

		// File should exist
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			t.Fatal("log file was not created")
		}
	})

	t.Run("empty path uses default", func(t *testing.T) {
		// Set CAAM_HOME to temp directory to avoid writing to real home
		orig := os.Getenv("CAAM_HOME")
		defer os.Setenv("CAAM_HOME", orig)

		tmpDir := t.TempDir()
		os.Setenv("CAAM_HOME", tmpDir)

		if err := AppendLogLine("", "test with empty path"); err != nil {
			t.Fatalf("AppendLogLine with empty path: %v", err)
		}

		// Check the file exists at default path
		defaultPath := DefaultLogFilePath()
		if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
			t.Fatal("log file was not created at default path")
		}
	})
}
