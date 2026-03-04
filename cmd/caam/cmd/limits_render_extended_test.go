package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
	"github.com/stretchr/testify/require"
)

func makeUsageProfile(provider, profile string, primaryPct, secondaryPct int, errMsg string) usage.ProfileUsage {
	now := time.Now()
	info := &usage.UsageInfo{
		Provider:  provider,
		FetchedAt: now,
		Error:     errMsg,
	}
	if primaryPct >= 0 {
		info.PrimaryWindow = &usage.UsageWindow{
			Utilization: float64(primaryPct) / 100,
			UsedPercent: primaryPct,
			ResetsAt:    now.Add(45 * time.Minute),
		}
	}
	if secondaryPct >= 0 {
		info.SecondaryWindow = &usage.UsageWindow{
			Utilization: float64(secondaryPct) / 100,
			UsedPercent: secondaryPct,
			ResetsAt:    now.Add(2 * time.Hour),
		}
	}
	return usage.ProfileUsage{
		Provider:    provider,
		ProfileName: profile,
		Usage:       info,
	}
}

func TestRenderLimitsFormatsExtended(t *testing.T) {
	results := []usage.ProfileUsage{
		makeUsageProfile("codex", "healthy", 20, 30, ""),
		makeUsageProfile("codex", "hot", 92, 80, ""),
	}

	var out bytes.Buffer
	require.NoError(t, renderLimits(&out, "table", results))
	require.Contains(t, out.String(), "Rate Limit Usage")
	require.Contains(t, out.String(), "codex/healthy")

	out.Reset()
	require.NoError(t, renderLimits(&out, "json", results))
	var got []usage.ProfileUsage
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Len(t, got, 2)

	out.Reset()
	require.NoError(t, renderLimits(&out, "table", nil))
	require.Contains(t, out.String(), "No profiles found")

	err := renderLimits(&out, "xml", results)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format")
}

func TestRenderBestProfileRecommendationsAndForecastExtended(t *testing.T) {
	results := []usage.ProfileUsage{
		makeUsageProfile("codex", "near-limit", 95, 82, ""),
		makeUsageProfile("codex", "good", 25, 35, ""),
		makeUsageProfile("claude", "medium", 55, 45, ""),
	}

	var out bytes.Buffer
	require.NoError(t, renderBestProfile(&out, "table", results, 0.8))
	require.Contains(t, out.String(), "Best profile:")

	out.Reset()
	require.NoError(t, renderBestProfile(&out, "json", results, 0.8))
	require.Contains(t, out.String(), "\"provider\"")

	recs := generateLimitsRecommendations(results, 0.8)
	require.NotEmpty(t, recs)
	require.True(t, recs[0].Action == "Switch from" || recs[0].Action == "Switch to")

	out.Reset()
	require.NoError(t, renderRecommendations(&out, "table", results, 0.8))
	require.Contains(t, out.String(), "Smart Rotation Recommendations")

	out.Reset()
	require.NoError(t, renderRecommendations(&out, "json", results, 0.8))
	require.Contains(t, out.String(), "\"action\"")

	forecasts := generateForecasts(results)
	require.NotEmpty(t, forecasts)
	require.Contains(t, forecasts[0].Profile, "/")

	out.Reset()
	require.NoError(t, renderForecast(&out, "table", results))
	require.Contains(t, out.String(), "Usage Forecasts")

	out.Reset()
	require.NoError(t, renderForecast(&out, "json", results))
	require.Contains(t, out.String(), "\"profile\"")
}

func TestGetProfileTokenCodexAndUnsupported(t *testing.T) {
	vaultDir := t.TempDir()
	profileDir := filepath.Join(vaultDir, "codex", "work")
	require.NoError(t, os.MkdirAll(profileDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth.json"), []byte(`{"tokens":{"access_token":"tok-123","account_id":"acct-1"}}`), 0o600))

	token, err := getProfileToken(vaultDir, "codex", "work")
	require.NoError(t, err)
	require.Equal(t, "tok-123", token)

	_, err = getProfileToken(vaultDir, "gemini", "work")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported provider")
}
