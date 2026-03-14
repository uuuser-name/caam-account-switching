package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func TestBrowserLaunchersResolvePathsAndErrorBranches(t *testing.T) {
	tempPath := t.TempDir()
	chromePath := writeExecutable(t, tempPath, "google-chrome")
	writeExecutable(t, tempPath, "firefox")
	t.Setenv("PATH", tempPath)

	if got := findChrome(); got == "" {
		t.Fatal("findChrome() returned empty path")
	}
	if got := findFirefox(); runtime.GOOS == "linux" && got == "" {
		t.Fatal("findFirefox() returned empty path on linux")
	}
	if got := findInPath("google-chrome"); got != chromePath {
		t.Fatalf("findInPath(google-chrome) = %q, want %q", got, chromePath)
	}
	if got := findInPath("definitely-missing-browser"); got != "" {
		t.Fatalf("findInPath(missing) = %q, want empty", got)
	}

	if err := (&ChromeLauncher{config: Config{Command: "/definitely/missing/chrome"}}).Open("https://example.com"); err == nil {
		t.Fatal("expected chrome launcher to fail when command is missing")
	}
	if err := (&FirefoxLauncher{config: Config{Command: "/definitely/missing/firefox"}}).Open("https://example.com"); err == nil {
		t.Fatal("expected firefox launcher to fail when command is missing")
	}
	if err := (&GenericLauncher{config: Config{Command: "/definitely/missing/browser"}}).Open("https://example.com"); err == nil {
		t.Fatal("expected generic launcher to fail when command is missing")
	}
}

func TestDefaultLauncherOpenReturnsErrorWhenCommandUnavailable(t *testing.T) {
	t.Setenv("PATH", "")

	err := (&DefaultLauncher{}).Open("https://example.com")
	if err == nil {
		t.Fatal("expected default launcher to fail when no opener is available")
	}
}
