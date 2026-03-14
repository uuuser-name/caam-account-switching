package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/logs"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCostCommandsAndTokenAnalysisUseRealState(t *testing.T) {
	newStartupLayout(t)

	db, err := caamdb.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.SetCostRate("claude", 6, 4))
	require.NoError(t, db.SetCostRate("codex", 3, 2))

	now := time.Now().UTC()
	require.NoError(t, db.RecordWrapSession(caamdb.WrapSession{
		Provider:        "claude",
		ProfileName:     "work",
		StartedAt:       now.Add(-2 * time.Hour),
		EndedAt:         now.Add(-95 * time.Minute),
		DurationSeconds: 30 * 60,
		ExitCode:        0,
		RateLimitHit:    true,
	}))
	require.NoError(t, db.RecordWrapSession(caamdb.WrapSession{
		Provider:        "codex",
		ProfileName:     "main",
		StartedAt:       now.Add(-90 * time.Minute),
		EndedAt:         now.Add(-30 * time.Minute),
		DurationSeconds: 60 * 60,
		ExitCode:        0,
	}))

	out, err := executeCommand("cost", "--json", "--since", "10000h")
	require.NoError(t, err, out)
	var summary costSummaryOutput
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &summary), out)
	require.Len(t, summary.Summaries, 2)
	require.NotZero(t, summary.TotalCents)

	out, err = executeCommand("cost", "sessions", "--json", "--limit", "10", "--since", "10000h")
	require.NoError(t, err, out)
	var sessions sessionsOutput
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &sessions), out)
	require.Len(t, sessions.Sessions, 2)
	require.Equal(t, "codex", sessions.Sessions[0].Provider)

	out, err = executeCommand("cost", "rates", "--json")
	require.NoError(t, err, out)
	var rates ratesOutput
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &rates), out)
	require.GreaterOrEqual(t, len(rates.Rates), 2)
	var sawClaude, sawCodex bool
	for _, rate := range rates.Rates {
		if rate.Provider == "claude" {
			sawClaude = true
		}
		if rate.Provider == "codex" {
			sawCodex = true
		}
	}
	require.True(t, sawClaude)
	require.True(t, sawCodex)

	out, err = executeCommand("cost", "rates", "--set", "claude", "--per-minute", "9", "--per-session", "2")
	require.NoError(t, err, out)
	require.Contains(t, out, "Updated claude: 9")

	analysis := analyzeTokenCosts("claude", []*logs.LogEntry{
		{
			Timestamp:         now.Add(-time.Hour),
			Model:             "claude-sonnet-4",
			InputTokens:       1200,
			OutputTokens:      600,
			CacheReadTokens:   300,
			CacheCreateTokens: 100,
		},
	}, now.Add(-24*time.Hour), now, 24*time.Hour)
	require.Equal(t, int64(2200), analysis.TotalTokens)
	require.NotEmpty(t, analysis.ByModel)
	require.Equal(t, "1d", formatTokenPeriod(24*time.Hour))
	require.Equal(t, "6h", formatTokenPeriod(6*time.Hour))

	var table bytes.Buffer
	require.NoError(t, renderTokenCostAnalysis(&table, "table", []TokenCostAnalysis{analysis}))
	require.Contains(t, table.String(), "TOKEN COST ANALYSIS")

	var jsonBuf bytes.Buffer
	require.NoError(t, renderTokenCostAnalysis(&jsonBuf, "json", []TokenCostAnalysis{analysis}))
	require.Contains(t, jsonBuf.String(), "\"provider\": \"claude\"")

	var csvBuf bytes.Buffer
	require.NoError(t, renderTokenCostAnalysis(&csvBuf, "csv", []TokenCostAnalysis{analysis}))
	require.Contains(t, csvBuf.String(), "provider,period,model")
	require.ErrorContains(t, renderTokenCostAnalysis(&csvBuf, "yaml", []TokenCostAnalysis{analysis}), "unsupported format")
}

func TestUsageAndNextCommandsUseRealState(t *testing.T) {
	newStartupLayout(t)

	db, err := caamdb.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.LogEvent(caamdb.Event{
		Type:        caamdb.EventActivate,
		Provider:    "codex",
		ProfileName: "alpha",
		Timestamp:   time.Now().Add(-3 * time.Hour),
	}))
	require.NoError(t, db.LogEvent(caamdb.Event{
		Type:        caamdb.EventDeactivate,
		Provider:    "codex",
		ProfileName: "alpha",
		Timestamp:   time.Now().Add(-2 * time.Hour),
		Duration:    90 * time.Minute,
	}))

	out, err := executeCommand("usage", "--profile", "codex/alpha", "--detailed", "--format", "csv", "--since", "1970-01-01")
	require.NoError(t, err, out)
	require.Contains(t, out, "timestamp,duration_hours")
	require.Contains(t, out, "1.500")

	out, err = executeCommand("usage", "--format", "table", "--since", "1970-01-01")
	require.NoError(t, err, out)
	require.Contains(t, out, "Profile Usage")
	require.Contains(t, out, "codex/alpha")

	originalVault := vault
	originalTools := tools
	t.Cleanup(func() {
		vault = originalVault
		tools = originalTools
	})

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	tools = map[string]func() authfile.AuthFileSet{
		"codex": authfile.CodexAuthFiles,
	}

	seedCodexProfile(t, "alpha", `{"access_token":"alpha"}`)
	seedCodexProfile(t, "beta", `{"access_token":"beta"}`)
	require.NoError(t, vault.Restore(authfile.CodexAuthFiles(), "alpha"))

	nextDryRunCmd := &cobra.Command{}
	nextDryRunCmd.Flags().Bool("dry-run", false, "")
	nextDryRunCmd.Flags().Bool("quiet", false, "")
	nextDryRunCmd.Flags().Bool("force", false, "")
	nextDryRunCmd.Flags().String("algorithm", "", "")
	nextDryRunCmd.Flags().Bool("usage-aware", false, "")
	require.NoError(t, nextDryRunCmd.Flags().Set("dry-run", "true"))

	out, err = captureStdout(t, func() error {
		return runNext(nextDryRunCmd, []string{"codex"})
	})
	require.NoError(t, err, out)
	require.Contains(t, out, "Next:")
	require.Contains(t, out, "codex/beta")

	_, err = executeCommand("next", "unknown")
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown tool")
}
