package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/health"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type robotTestEnv struct {
	layout startupLayout
	db     *caamdb.DB
}

func setupRobotTestEnv(t *testing.T) robotTestEnv {
	t.Helper()

	layout := newStartupLayout(t)

	oldVault := vault
	oldHealthStore := healthStore
	oldGlobalDB := globalDB
	t.Cleanup(func() {
		vault = oldVault
		healthStore = oldHealthStore
		if globalDB != nil && globalDB != oldGlobalDB {
			globalDB.Close()
		}
		globalDB = oldGlobalDB
	})

	vault = authfile.NewVault(authfile.DefaultVaultPath())
	healthStore = health.NewStorage(filepath.Join(layout.caamHome, "health.json"))
	globalDB = nil

	db, err := caamdb.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		if globalDB == db {
			globalDB = nil
		}
	})

	return robotTestEnv{
		layout: layout,
		db:     db,
	}
}

func newRobotCommand() (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{}
	cmd.Flags().String("provider", "", "")
	cmd.Flags().Bool("compact", false, "")
	cmd.Flags().Bool("include-coordinators", false, "")
	cmd.Flags().String("strategy", "smart", "")
	cmd.Flags().Bool("include-cooldown", false, "")
	cmd.Flags().Int("days", 7, "")
	cmd.Flags().Int("limit", 50, "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	return cmd, &buf
}

func writeRobotCodexProfileAuth(t *testing.T, profileName, email, plan string, expiresAt time.Time, opts ...func(map[string]any)) []byte {
	t.Helper()

	payload := map[string]any{
		"email":     email,
		"plan_type": plan,
	}
	jwt := buildRobotJWT(t, payload)

	content := map[string]any{
		"id_token":     jwt,
		"access_token": "access-" + profileName,
		"expires_at":   expiresAt.Unix(),
	}
	for _, opt := range opts {
		opt(content)
	}

	data, err := json.Marshal(content)
	require.NoError(t, err)

	profileDir := vault.ProfilePath("codex", profileName)
	require.NoError(t, os.MkdirAll(profileDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "auth.json"), data, 0o600))
	return data
}

func setActiveCodexAuth(t *testing.T, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(os.Getenv("CODEX_HOME"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json"), data, 0o600))
}

func buildRobotJWT(t *testing.T, payload map[string]any) string {
	t.Helper()

	headerJSON, err := json.Marshal(map[string]any{
		"alg": "none",
		"typ": "JWT",
	})
	require.NoError(t, err)
	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	return base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON) +
		".sig"
}

func decodeRobotOutput[T any](t *testing.T, buf *bytes.Buffer) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out), buf.String())
	return out
}

func TestRobotStatusAndProviderInfoUseRealState(t *testing.T) {
	env := setupRobotTestEnv(t)

	stableAuth := writeRobotCodexProfileAuth(
		t,
		"stable",
		"stable@example.com",
		"pro",
		time.Now().Add(72*time.Hour),
		func(content map[string]any) {
			content["refresh_token"] = "refresh-stable"
		},
	)
	writeRobotCodexProfileAuth(
		t,
		"cooling",
		"cooling@example.com",
		"enterprise",
		time.Now().Add(30*time.Minute),
	)
	setActiveCodexAuth(t, stableAuth)

	require.NoError(t, healthStore.UpdateProfile("codex", "cooling", &health.ProfileHealth{
		ErrorCount1h: 1,
	}))
	_, err := env.db.SetCooldown("codex", "cooling", time.Now().UTC(), 45*time.Minute, "rate limit hit")
	require.NoError(t, err)

	providerInfo := buildProviderInfo("codex", false)
	require.True(t, providerInfo.LoggedIn)
	require.Equal(t, "stable", providerInfo.ActiveProfile)
	require.Len(t, providerInfo.Profiles, 2)
	require.NotEmpty(t, providerInfo.AuthPaths)

	profilesByName := map[string]RobotProfileInfo{}
	for _, profile := range providerInfo.Profiles {
		profilesByName[profile.Name] = profile
	}

	require.Equal(t, "stable@example.com", profilesByName["stable"].Email)
	require.Equal(t, "pro", profilesByName["stable"].PlanType)
	require.True(t, profilesByName["stable"].Active)
	require.Equal(t, "healthy", profilesByName["stable"].Health.Status)
	require.Contains(t, profilesByName["cooling"].Recommendation, "wait for cooldown")

	cooling := buildProfileInfo("codex", "cooling", "stable", env.db, false)
	require.Equal(t, "warning", cooling.Health.Status)
	require.Equal(t, "token expiring soon", cooling.Health.Reason)
	require.NotNil(t, cooling.Cooldown)
	require.True(t, cooling.Cooldown.Active)
	require.Contains(t, cooling.Recommendation, "wait for cooldown")

	cmd, buf := newRobotCommand()
	require.NoError(t, runRobotStatus(cmd, []string{"codex"}))

	out := decodeRobotOutput[struct {
		Success     bool            `json:"success"`
		Command     string          `json:"command"`
		Data        RobotStatusData `json:"data"`
		Suggestions []string        `json:"suggestions"`
		Error       *RobotError     `json:"error"`
		Timing      *RobotTiming    `json:"timing"`
	}](t, buf)

	require.True(t, out.Success)
	require.Equal(t, "status", out.Command)
	require.Nil(t, out.Error)
	require.NotNil(t, out.Timing)
	require.Equal(t, 2, out.Data.Summary.TotalProfiles)
	require.Equal(t, 1, out.Data.Summary.ActiveProfiles)
	require.Equal(t, 1, out.Data.Summary.HealthyProfiles)
	require.Equal(t, 1, out.Data.Summary.CooldownProfiles)
	require.Equal(t, 1, out.Data.Summary.ExpiringSoon)
	require.Contains(t, strings.Join(out.Suggestions, "\n"), "expiring within 24h")

	cmd, buf = newRobotCommand()
	err = runRobotStatus(cmd, []string{"mystery"})
	require.Error(t, err)

	errOut := decodeRobotOutput[struct {
		Success bool        `json:"success"`
		Command string      `json:"command"`
		Error   *RobotError `json:"error"`
	}](t, buf)
	require.False(t, errOut.Success)
	require.Equal(t, "status", errOut.Command)
	require.Equal(t, "INVALID_PROVIDER", errOut.Error.Code)
}

func TestRobotNextHealthPathsHistoryAndConfigUseRealState(t *testing.T) {
	env := setupRobotTestEnv(t)

	stableAuth := writeRobotCodexProfileAuth(
		t,
		"stable",
		"stable@example.com",
		"pro",
		time.Now().Add(96*time.Hour),
		func(content map[string]any) {
			content["refresh_token"] = "refresh-stable"
		},
	)
	setActiveCodexAuth(t, stableAuth)

	writeRobotCodexProfileAuth(t, "backup", "backup@example.com", "team", time.Now().Add(20*time.Minute))
	writeRobotCodexProfileAuth(t, "blocked", "blocked@example.com", "pro", time.Now().Add(90*time.Minute))

	require.NoError(t, healthStore.UpdateProfile("codex", "backup", &health.ProfileHealth{ErrorCount1h: 1}))
	require.NoError(t, healthStore.UpdateProfile("codex", "blocked", &health.ProfileHealth{ErrorCount1h: 1}))
	_, err := env.db.SetCooldown("codex", "blocked", time.Now().UTC(), 2*time.Hour, "cooldown")
	require.NoError(t, err)
	require.NoError(t, env.db.LogEvent(caamdb.Event{
		Type:        caamdb.EventActivate,
		Provider:    "codex",
		ProfileName: "stable",
		Duration:    90 * time.Second,
	}))

	cmd, buf := newRobotCommand()
	require.NoError(t, runRobotNext(cmd, []string{"codex"}))

	nextOut := decodeRobotOutput[struct {
		Success bool          `json:"success"`
		Command string        `json:"command"`
		Data    RobotNextData `json:"data"`
	}](t, buf)
	require.True(t, nextOut.Success)
	require.Equal(t, "next", nextOut.Command)
	require.Equal(t, "stable", nextOut.Data.Profile)
	require.Contains(t, nextOut.Data.Command, "caam activate codex stable")
	require.NotNil(t, nextOut.Data.AlternateChoice)
	require.Equal(t, "backup", nextOut.Data.AlternateChoice.Profile)

	cmd, _ = newRobotCommand()
	require.NoError(t, cmd.Flags().Set("include-cooldown", "true"))
	require.NoError(t, runRobotNext(cmd, []string{"codex"}))

	cmd, buf = newRobotCommand()
	err = runRobotNext(cmd, []string{"unknown"})
	require.Error(t, err)

	nextErr := decodeRobotOutput[struct {
		Success bool        `json:"success"`
		Error   *RobotError `json:"error"`
	}](t, buf)
	require.False(t, nextErr.Success)
	require.Equal(t, "INVALID_PROVIDER", nextErr.Error.Code)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotHealth(cmd, nil))
	healthOut := decodeRobotOutput[struct {
		Success bool `json:"success"`
		Data    struct {
			Overall string `json:"overall"`
			Checks  []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"data"`
	}](t, buf)
	require.True(t, healthOut.Success)
	require.Equal(t, "healthy", healthOut.Data.Overall)
	require.GreaterOrEqual(t, len(healthOut.Data.Checks), 3)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotPaths(cmd, nil))
	pathsOut := decodeRobotOutput[struct {
		Success bool           `json:"success"`
		Data    RobotPathsData `json:"data"`
	}](t, buf)
	require.True(t, pathsOut.Success)
	require.Equal(t, authfile.DefaultVaultPath(), pathsOut.Data.VaultPath)
	require.NotEmpty(t, pathsOut.Data.Providers)

	cmd, buf = newRobotCommand()
	require.NoError(t, cmd.Flags().Set("provider", "codex"))
	require.NoError(t, cmd.Flags().Set("days", "30"))
	require.NoError(t, cmd.Flags().Set("limit", "10"))
	require.NoError(t, runRobotHistory(cmd, nil))
	historyOut := decodeRobotOutput[struct {
		Success bool             `json:"success"`
		Data    RobotHistoryData `json:"data"`
	}](t, buf)
	require.True(t, historyOut.Success)
	require.Equal(t, 1, historyOut.Data.Count)
	require.Len(t, historyOut.Data.Events, 1)
	require.Equal(t, "codex", historyOut.Data.Events[0].Provider)
	require.Equal(t, "stable", historyOut.Data.Events[0].Profile)
	require.Equal(t, "activate", historyOut.Data.Events[0].Event)
	require.Equal(t, "1m", historyOut.Data.Events[0].Duration)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotConfig(cmd, nil))
	configOut := decodeRobotOutput[struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
		Timing  *RobotTiming   `json:"timing"`
	}](t, buf)
	require.True(t, configOut.Success)
	require.NotNil(t, configOut.Data["config"])
	require.NotNil(t, configOut.Data["spm_config"])
	require.NotNil(t, configOut.Timing)

	cmd, _ = newRobotCommand()
	require.NoError(t, runRobotConfig(cmd, []string{"set", "rotation_algorithm", "random"}))
	spmCfg, err := config.LoadSPMConfig()
	require.NoError(t, err)
	require.Equal(t, "random", spmCfg.Stealth.Rotation.Algorithm)

	cmd, buf = newRobotCommand()
	err = runRobotConfig(cmd, []string{"set", "unknown_key", "value"})
	require.Error(t, err)
	configErr := decodeRobotOutput[struct {
		Success bool        `json:"success"`
		Error   *RobotError `json:"error"`
	}](t, buf)
	require.False(t, configErr.Success)
	require.Equal(t, "UNKNOWN_KEY", configErr.Error.Code)
}

func TestRobotHealthReasonAndRecommendationBranches(t *testing.T) {
	expired := &health.ProfileHealth{
		TokenExpiresAt: time.Now().Add(-time.Minute),
	}
	require.Equal(t, "token expired", getHealthReason(expired, health.StatusCritical))
	require.Equal(t, "refresh token required", generateRecommendation(RobotProfileInfo{
		Health: RobotHealthInfo{Status: "critical", Reason: "token expired"},
	}))

	errorsOnly := &health.ProfileHealth{ErrorCount1h: 4}
	require.Contains(t, getHealthReason(errorsOnly, health.StatusCritical), "high error rate")
	require.Equal(t, "investigate errors before use", generateRecommendation(RobotProfileInfo{
		Health: RobotHealthInfo{Status: "critical", Reason: "high error rate"},
	}))

	warning := &health.ProfileHealth{
		TokenExpiresAt: time.Now().Add(30 * time.Minute),
	}
	require.Equal(t, "token expiring soon", getHealthReason(warning, health.StatusWarning))
	require.Equal(t, "consider refreshing token soon", generateRecommendation(RobotProfileInfo{
		Health: RobotHealthInfo{Status: "warning", Reason: "token expiring soon"},
	}))
}

func TestRobotActLimitsPrecheckValidateAndQuickStartUseRealState(t *testing.T) {
	env := setupRobotTestEnv(t)

	stableAuth := writeRobotCodexProfileAuth(
		t,
		"stable",
		"stable@example.com",
		"pro",
		time.Now().Add(48*time.Hour),
		func(content map[string]any) {
			content["refresh_token"] = "refresh-stable"
		},
	)
	setActiveCodexAuth(t, stableAuth)
	writeRobotCodexProfileAuth(t, "backup", "backup@example.com", "team", time.Now().Add(2*time.Hour))
	writeRobotCodexProfileAuth(t, "blocked", "blocked@example.com", "pro", time.Now().Add(20*time.Minute))

	require.NoError(t, healthStore.UpdateProfile("codex", "blocked", &health.ProfileHealth{ErrorCount1h: 1}))
	_, err := env.db.SetCooldown("codex", "blocked", time.Now().UTC(), 90*time.Minute, "robot cooldown")
	require.NoError(t, err)

	cmd, buf := newRobotCommand()
	require.NoError(t, runRobotAct(cmd, []string{"backup", "codex", "zsnapshot"}))
	var actOut struct {
		Success bool           `json:"success"`
		Command string         `json:"command"`
		Data    RobotActResult `json:"data"`
	}
	actOut = decodeRobotOutput[struct {
		Success bool           `json:"success"`
		Command string         `json:"command"`
		Data    RobotActResult `json:"data"`
	}](t, buf)
	require.True(t, actOut.Success)
	require.Equal(t, "act", actOut.Command)
	require.Equal(t, "zsnapshot", actOut.Data.Profile)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotAct(cmd, []string{"cooldown", "codex", "stable", "15m"}))
	actOut = decodeRobotOutput[struct {
		Success bool           `json:"success"`
		Command string         `json:"command"`
		Data    RobotActResult `json:"data"`
	}](t, buf)
	require.True(t, actOut.Success)
	require.Contains(t, actOut.Data.Message, "cooldown set until")

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotAct(cmd, []string{"uncooldown", "codex", "stable"}))
	actOut = decodeRobotOutput[struct {
		Success bool           `json:"success"`
		Command string         `json:"command"`
		Data    RobotActResult `json:"data"`
	}](t, buf)
	require.True(t, actOut.Success)
	require.Contains(t, actOut.Data.Message, "cleared cooldown")

	cmd, buf = newRobotCommand()
	err = runRobotAct(cmd, []string{"unknown", "codex"})
	require.Error(t, err)
	actErr := decodeRobotOutput[struct {
		Success bool        `json:"success"`
		Error   *RobotError `json:"error"`
	}](t, buf)
	require.False(t, actErr.Success)
	require.Equal(t, "INVALID_ACTION", actErr.Error.Code)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotLimits(cmd, []string{"codex"}))
	limitsOut := decodeRobotOutput[struct {
		Success bool `json:"success"`
		Data    struct {
			Provider string `json:"provider"`
			Profiles []struct {
				Name           string `json:"name"`
				AvailScore     int    `json:"availability_score"`
				Recommendation string `json:"recommendation"`
			} `json:"profiles"`
		} `json:"data"`
	}](t, buf)
	require.True(t, limitsOut.Success)
	require.Equal(t, "codex", limitsOut.Data.Provider)
	require.Len(t, limitsOut.Data.Profiles, 4)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotPrecheck(cmd, []string{"codex"}))
	precheckOut := decodeRobotOutput[struct {
		Success bool `json:"success"`
		Data    struct {
			Provider    string `json:"provider"`
			Recommended *struct {
				Name string `json:"name"`
			} `json:"recommended"`
			InCooldown []struct {
				Name string `json:"name"`
			} `json:"in_cooldown"`
			Summary struct {
				Ready      int `json:"ready_profiles"`
				InCooldown int `json:"in_cooldown"`
			} `json:"summary"`
			Commands struct {
				Activate string `json:"activate"`
			} `json:"commands"`
		} `json:"data"`
	}](t, buf)
	require.True(t, precheckOut.Success)
	require.Equal(t, "codex", precheckOut.Data.Provider)
	require.NotNil(t, precheckOut.Data.Recommended)
	require.Len(t, precheckOut.Data.InCooldown, 1)
	require.Equal(t, "blocked", precheckOut.Data.InCooldown[0].Name)
	require.Equal(t, 3, precheckOut.Data.Summary.Ready)
	require.Equal(t, 1, precheckOut.Data.Summary.InCooldown)
	require.Contains(t, precheckOut.Data.Commands.Activate, precheckOut.Data.Recommended.Name)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotValidate(cmd, []string{"codex"}))
	validateOut := decodeRobotOutput[struct {
		Success bool `json:"success"`
		Data    struct {
			Method  string `json:"method"`
			Summary struct {
				Total   int `json:"total"`
				Valid   int `json:"valid"`
				Invalid int `json:"invalid"`
			} `json:"summary"`
			Profiles []struct {
				Profile string `json:"profile"`
				Valid   bool   `json:"valid"`
			} `json:"profiles"`
		} `json:"data"`
	}](t, buf)
	require.True(t, validateOut.Success)
	require.Equal(t, "passive", validateOut.Data.Method)
	require.Equal(t, 4, validateOut.Data.Summary.Total)
	require.Equal(t, 4, validateOut.Data.Summary.Valid)
	require.Equal(t, 0, validateOut.Data.Summary.Invalid)

	cmd, buf = newRobotCommand()
	require.NoError(t, runRobotQuickStart(cmd, nil))
	require.Contains(t, buf.String(), "# caam robot - Agent Quick Start")
	require.Contains(t, buf.String(), "caam robot status")
}
