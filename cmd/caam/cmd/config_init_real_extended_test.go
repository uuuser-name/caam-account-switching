package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider"
	codexprovider "github.com/Dicklesworthstone/coding_agent_account_manager/internal/provider/codex"
	"github.com/stretchr/testify/require"
)

func TestConfigNestedSectionsAndTUIBranches(t *testing.T) {
	cfg := config.DefaultSPMConfig()

	require.NoError(t, setConfigValue(cfg, "alerts.enabled", "false"))
	require.NoError(t, setConfigValue(cfg, "alerts.warning_threshold", "7"))
	require.NoError(t, setConfigValue(cfg, "alerts.critical_threshold", "11"))
	require.NoError(t, setConfigValue(cfg, "alerts.notifications.terminal", "off"))
	require.NoError(t, setConfigValue(cfg, "alerts.notifications.desktop", "yes"))
	require.NoError(t, setConfigValue(cfg, "alerts.notifications.webhook", "https://example.invalid/hook"))
	require.NoError(t, setConfigValue(cfg, "handoff.auto_trigger", "true"))
	require.NoError(t, setConfigValue(cfg, "handoff.debounce_delay", "45s"))
	require.NoError(t, setConfigValue(cfg, "handoff.max_retries", "9"))
	require.NoError(t, setConfigValue(cfg, "handoff.fallback_to_manual", "no"))
	require.NoError(t, setConfigValue(cfg, "daemon.check_interval", "10m"))
	require.NoError(t, setConfigValue(cfg, "daemon.refresh_threshold", "35m"))
	require.NoError(t, setConfigValue(cfg, "daemon.verbose", "on"))
	require.NoError(t, setConfigValue(cfg, "daemon.auth_pool.enabled", "true"))
	require.NoError(t, setConfigValue(cfg, "daemon.auth_pool.max_concurrent_refresh", "4"))
	require.NoError(t, setConfigValue(cfg, "daemon.auth_pool.refresh_retry_delay", "2m"))
	require.NoError(t, setConfigValue(cfg, "daemon.auth_pool.max_refresh_retries", "6"))
	require.NoError(t, setConfigValue(cfg, "tui.theme", "dark"))
	require.NoError(t, setConfigValue(cfg, "tui.high_contrast", "true"))
	require.NoError(t, setConfigValue(cfg, "tui.reduced_motion", "true"))
	require.NoError(t, setConfigValue(cfg, "tui.toasts", "false"))
	require.NoError(t, setConfigValue(cfg, "tui.mouse", "false"))
	require.NoError(t, setConfigValue(cfg, "tui.show_key_hints", "true"))
	require.NoError(t, setConfigValue(cfg, "tui.density", "compact"))
	require.NoError(t, setConfigValue(cfg, "tui.no_tui", "false"))

	for key, want := range map[string]string{
		"alerts.enabled":                          "false",
		"alerts.warning_threshold":                "7",
		"alerts.critical_threshold":               "11",
		"alerts.notifications.terminal":           "false",
		"alerts.notifications.desktop":            "true",
		"alerts.notifications.webhook":            "https://example.invalid/hook",
		"handoff.auto_trigger":                    "true",
		"handoff.debounce_delay":                  "45s",
		"handoff.max_retries":                     "9",
		"handoff.fallback_to_manual":              "false",
		"daemon.check_interval":                   "10m0s",
		"daemon.refresh_threshold":                "35m0s",
		"daemon.verbose":                          "true",
		"daemon.auth_pool.enabled":                "true",
		"daemon.auth_pool.max_concurrent_refresh": "4",
		"daemon.auth_pool.refresh_retry_delay":    "2m0s",
		"daemon.auth_pool.max_refresh_retries":    "6",
		"tui.theme":                               "dark",
		"tui.high_contrast":                       "true",
		"tui.reduced_motion":                      "true",
		"tui.toasts":                              "false",
		"tui.mouse":                               "false",
		"tui.show_key_hints":                      "true",
		"tui.density":                             "compact",
		"tui.no_tui":                              "false",
	} {
		got, err := getConfigValue(cfg, key)
		require.NoError(t, err, key)
		require.Equal(t, want, got, key)
	}

	_, err := getConfigValue(cfg, "alerts.notifications.mystery")
	require.ErrorContains(t, err, "unknown notifications field")
	_, err = getConfigValue(cfg, "daemon.auth_pool.mystery")
	require.ErrorContains(t, err, "unknown auth_pool field")
	require.ErrorContains(t, setConfigValue(cfg, "alerts.notifications.mystery", "x"), "unknown notifications field")
	require.ErrorContains(t, setConfigValue(cfg, "daemon.auth_pool.mystery", "x"), "unknown auth_pool field")
	require.ErrorContains(t, setConfigValue(cfg, "tui.theme", "neon"), "invalid theme")
}

func TestSetupShellIntegrationDetectProviderAuthAndImportRealCodex(t *testing.T) {
	layout := newStartupLayout(t)

	originalRegistry := registry
	originalProfileStore := profileStore
	originalVault := vault
	originalConfigPath := config.ConfigPath()
	t.Cleanup(func() {
		registry = originalRegistry
		profileStore = originalProfileStore
		vault = originalVault
		config.SetConfigPath(originalConfigPath)
	})

	config.SetConfigPath(filepath.Join(layout.xdgConfig, "caam", "config.json"))
	registry = provider.NewRegistry()
	registry.Register(codexprovider.New())
	profileStore = profile.NewStore(profile.DefaultStorePath())
	vault = authfile.NewVault(authfile.DefaultVaultPath())

	authPath := filepath.Join(layout.codexHome, "auth.json")
	require.NoError(t, os.WriteFile(authPath, []byte(`{"access_token":"codex-token","expires_at":`+
		strconv.FormatInt(time.Now().Add(4*time.Hour).Unix(), 10)+`}`), 0o600))

	t.Setenv("SHELL", "/bin/zsh")
	out, err := captureStdout(t, func() error {
		withTestStdin(t, "y\n", func() {
			setupShellIntegration()
		})
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "STEP 2: Shell Integration")
	require.Contains(t, out, "[OK] Added to")

	rcData, err := os.ReadFile(filepath.Join(layout.home, ".zshrc"))
	require.NoError(t, err)
	require.Contains(t, string(rcData), `eval "$(caam shell init)"`)

	detections := detectProviderAuth()
	require.NotEmpty(t, detections)

	var codexDetection ProviderAuthDetection
	for _, detection := range detections {
		if detection.ProviderID == "codex" {
			codexDetection = detection
			break
		}
	}
	require.NoError(t, codexDetection.Error)
	require.NotNil(t, codexDetection.Detection)
	require.True(t, codexDetection.Detection.Found)
	require.NotNil(t, codexDetection.Detection.Primary)
	require.Equal(t, authPath, codexDetection.Detection.Primary.Path)

	out, err = captureStdout(t, func() error {
		printProviderDetectionResults(detections)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, out, "Checking for existing auth credentials")
	require.Contains(t, out, "Codex")

	imported := importDetectedAuth(detections, true)
	require.Equal(t, 1, imported)
	require.True(t, profileStore.Exists("codex", "default"))

	prof, err := profileStore.Load("codex", "default")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(prof.CodexHomePath(), "auth.json"))
}

func TestResolveProfileNameUsesAliasesAndFuzzyMatching(t *testing.T) {
	layout := newStartupLayout(t)

	originalConfigPath := config.ConfigPath()
	t.Cleanup(func() { config.SetConfigPath(originalConfigPath) })
	config.SetConfigPath(filepath.Join(layout.xdgConfig, "caam", "config.json"))

	cfg := config.DefaultConfig()
	cfg.AddAlias("codex", "workhorse", "work")
	require.NoError(t, cfg.Save())

	profiles := []string{"workhorse", "worker-two", "personal"}
	require.Equal(t, "workhorse", resolveProfileName("codex", "work", profiles, true))
	require.Equal(t, "worker-two", resolveProfileName("codex", "worker", profiles, true))
	require.Equal(t, "missing", resolveProfileName("codex", "missing", profiles, true))
}
