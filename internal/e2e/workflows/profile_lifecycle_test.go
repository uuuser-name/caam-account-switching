package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileLifecycle(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Initialize environment")
	rootDir := h.TempDir
	binDir := filepath.Join(rootDir, "bin")
	capturePath := filepath.Join(rootDir, "exec-capture.txt")
	require.NoError(t, os.MkdirAll(binDir, 0755))

	// Set XDG vars to isolate the test
	env := withEnvOverrides(os.Environ(), map[string]string{
		"GO_WANT_CLI_HELPER": "1",
		"XDG_DATA_HOME":      rootDir,
		"XDG_CONFIG_HOME":    rootDir,
		"HOME":               rootDir,
		"PATH":               binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CAAM_EXEC_CAPTURE":  capturePath,
	})

	exe, err := os.Executable()
	require.NoError(t, err)

	runCLI := func(args ...string) (string, error) {
		cmdArgs := []string{"-test.run=^TestCLIHelper$", "--"}
		cmdArgs = append(cmdArgs, args...)

		cmd := exec.Command(exe, cmdArgs...)
		cmd.Env = env

		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	h.EndStep("Setup")

	// 2. Add Profile
	h.StartStep("Add", "Create isolated profile")
	out, err := runCLI("profile", "add", "claude", "test-work", "--description", "Work Profile")
	if err != nil {
		t.Fatalf("profile add failed: %v\nOutput: %s", err, out)
	}
	assert.Contains(t, out, "Created profile claude/test-work")

	// Verify directory structure
	profileDir := filepath.Join(rootDir, "caam", "profiles", "claude", "test-work")
	require.DirExists(t, profileDir)
	require.DirExists(t, filepath.Join(profileDir, "home"))
	require.FileExists(t, filepath.Join(profileDir, "profile.json"))

	h.EndStep("Add")

	// 3. Status
	h.StartStep("Status", "Check profile status")
	out, err = runCLI("profile", "status", "claude", "test-work")
	require.NoError(t, err)
	assert.Contains(t, out, "Profile: claude/test-work")
	assert.Contains(t, out, "Description: Work Profile")

	h.EndStep("Status")

	// 4. Update Description (Describe)
	h.StartStep("Describe", "Update description")
	out, err = runCLI("profile", "describe", "claude", "test-work", "New Description")
	require.NoError(t, err)
	assert.Contains(t, out, "Set description")

	out, err = runCLI("profile", "describe", "claude", "test-work")
	require.NoError(t, err)
	assert.Contains(t, out, "New Description")

	h.EndStep("Describe")

	// 5. List
	h.StartStep("List", "List profiles")
	out, err = runCLI("profile", "ls", "claude")
	require.NoError(t, err)
	assert.Contains(t, out, "test-work")
	assert.Contains(t, out, "New Description")

	h.EndStep("List")

	// 6. Exec
	if runtime.GOOS != "windows" {
		h.StartStep("Exec", "Run CLI with isolated profile")

		mockBinary := filepath.Join(binDir, "claude")
		mockScript := "#!/bin/sh\nprintf '%s\\n' \"$HOME\" > \"$CAAM_EXEC_CAPTURE\"\nprintf '%s\\n' \"$*\" >> \"$CAAM_EXEC_CAPTURE\"\n"
		require.NoError(t, os.WriteFile(mockBinary, []byte(mockScript), 0755))

			_, err = runCLI("exec", "claude", "test-work", "--", "--print-home")
		require.NoError(t, err)

		captureData, err := os.ReadFile(capturePath)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSpace(string(captureData)), "\n")
		require.Len(t, lines, 2)
		assert.Equal(t, filepath.Join(profileDir, "home"), lines[0], "exec should run with the isolated profile HOME")
		assert.Equal(t, "--print-home", lines[1], "exec should forward tool arguments to the provider binary")

		h.EndStep("Exec")
	}

	// 7. Delete
	h.StartStep("Delete", "Delete profile")
	out, err = runCLI("profile", "delete", "claude", "test-work", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "Deleted claude/test-work")

	// Verify directory gone
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Errorf("Profile directory still exists: %s", profileDir)
	}

	h.EndStep("Delete")
}
