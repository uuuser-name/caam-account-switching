package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	daemond "github.com/Dicklesworthstone/coding_agent_account_manager/internal/daemon"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDaemonCommandsReportMissingStateCleanly(t *testing.T) {
	layout := newStartupLayout(t)

	daemond.SetPIDFilePath(filepath.Join(layout.caamHome, "daemon.pid"))
	t.Cleanup(func() {
		daemond.SetPIDFilePath("")
		require.NoError(t, daemonLogsCmd.Flags().Set("lines", "50"))
		require.NoError(t, daemonLogsCmd.Flags().Set("follow", "false"))
	})

	statusOut, err := captureCommandStdout(t, func() error {
		return runDaemonStatus(daemonStatusCmd, nil)
	})
	require.NoError(t, err)
	require.Contains(t, statusOut, "Daemon is not running")

	stopOut, err := captureCommandStdout(t, func() error {
		return runDaemonStop(daemonStopCmd, nil)
	})
	require.NoError(t, err)
	require.Contains(t, stopOut, "Daemon is not running")

	require.NoError(t, daemonLogsCmd.Flags().Set("lines", "5"))
	require.NoError(t, daemonLogsCmd.Flags().Set("follow", "false"))
	logsOut, err := captureCommandStdout(t, func() error {
		return runDaemonLogs(daemonLogsCmd, nil)
	})
	require.NoError(t, err)
	require.Contains(t, logsOut, "No daemon logs found")
	require.Contains(t, logsOut, daemond.LogFilePath())
}

func TestCollectSessionsReportIncludesActiveAndStaleProfiles(t *testing.T) {
	newStartupLayout(t)

	originalProfileStore := profileStore
	t.Cleanup(func() { profileStore = originalProfileStore })

	profileStore = profile.NewStore(profile.DefaultStorePath())

	active, err := profileStore.Create("codex", "active", "oauth")
	require.NoError(t, err)
	require.NoError(t, active.Lock())
	t.Cleanup(func() { _ = active.Unlock() })

	stale, err := profileStore.Create("codex", "stale", "oauth")
	require.NoError(t, err)
	staleLock := map[string]any{
		"pid":       99999999,
		"locked_at": time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
	staleData, err := json.Marshal(staleLock)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(stale.BasePath, 0o700))
	require.NoError(t, os.WriteFile(stale.LockPath(), staleData, 0o600))
	t.Cleanup(func() { _ = os.Remove(stale.LockPath()) })

	report, err := collectSessions("")
	require.NoError(t, err)
	require.Len(t, report.Sessions, 2)
	require.Equal(t, 1, report.TotalActive)
	require.Equal(t, 1, report.TotalStale)

	filtered, err := collectSessions("gemini")
	require.NoError(t, err)
	require.Empty(t, filtered.Sessions)

	reportOut, err := captureCommandStdout(t, func() error {
		printSessionsReport(report)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, reportOut, "active")
	require.Contains(t, reportOut, "stale (process not running)")
	require.Contains(t, reportOut, "Summary: 1 active, 1 stale")

	emptyOut, err := captureCommandStdout(t, func() error {
		printSessionsReport(&SessionsReport{})
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, emptyOut, "No active sessions found.")
}

func TestFormatDurationHumanizesRanges(t *testing.T) {
	require.Equal(t, "just now", formatDuration(30*time.Second))
	require.Equal(t, "1 minute ago", formatDuration(time.Minute))
	require.Equal(t, "5 minutes ago", formatDuration(5*time.Minute))
	require.Equal(t, "1 hour ago", formatDuration(time.Hour))
	require.Equal(t, "6 hours ago", formatDuration(6*time.Hour))
	require.Equal(t, "1 day ago", formatDuration(24*time.Hour))
	require.Equal(t, "3 days ago", formatDuration(72*time.Hour))
}

func TestRunMonitorRejectsInvalidFormat(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Duration("interval", 30*time.Second, "")
	cmd.Flags().StringSlice("provider", nil, "")
	cmd.Flags().String("format", "broken", "")
	cmd.Flags().Float64("threshold", 80, "")
	cmd.Flags().Bool("once", false, "")
	cmd.Flags().Bool("no-emoji", false, "")
	cmd.Flags().Int("width", 75, "")

	err := runMonitor(cmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid format")
}
