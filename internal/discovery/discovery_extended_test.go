package discovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/identity"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeJWTToken(t *testing.T, claims map[string]any) string {
	t.Helper()

	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	return "eyJhbGciOiJIUzI1NiJ9." + trimBase64Padding(base64.URLEncoding.EncodeToString(payload)) + ".signature"
}

func writeClaudeCredentials(t *testing.T, homeDir, email, plan string) string {
	t.Helper()

	credsPath := filepath.Join(homeDir, ".claude", ".credentials.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(credsPath), 0700))
	require.NoError(t, os.WriteFile(credsPath, []byte(`{"claudeAiOauth":{"email":"`+email+`","subscriptionType":"`+plan+`"}}`), 0600))
	return credsPath
}

func readStoredPlan(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var root map[string]map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	claudeOauth, ok := root["claudeAiOauth"]
	require.True(t, ok)

	plan, ok := claudeOauth["subscriptionType"].(string)
	require.True(t, ok)
	return plan
}

func TestHasAuthFilesAndExtractIdentityFallbacks(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex")
	geminiHome := filepath.Join(root, "gemini")

	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0700))
	require.NoError(t, os.MkdirAll(codexHome, 0700))
	require.NoError(t, os.MkdirAll(geminiHome, 0700))

	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GEMINI_HOME", geminiHome)

	claudeCreds := filepath.Join(homeDir, ".claude", ".credentials.json")
	codexAuth := filepath.Join(codexHome, "auth.json")
	geminiSettings := filepath.Join(geminiHome, "settings.json")

	require.NoError(t, os.WriteFile(claudeCreds, []byte(`{"claudeAiOauth":{"email":"claude@example.com","subscriptionType":"max"}}`), 0600))
	require.NoError(t, os.WriteFile(codexAuth, []byte(`{"tokens":{"id_token":"`+makeJWTToken(t, map[string]any{"email": "codex@example.com"})+`"}}`), 0600))
	require.NoError(t, os.WriteFile(geminiSettings, []byte(`{"account":{"email":"gemini@example.com"}}`), 0600))

	dummyPath := filepath.Join(root, "dummy-auth-state.json")
	require.NoError(t, os.WriteFile(dummyPath, []byte("{}"), 0600))

	vault := authfile.NewVault(filepath.Join(root, "vault"))
	watcher := &Watcher{vault: vault}

	tests := []struct {
		name      string
		tool      Tool
		provider  string
		wantEmail string
	}{
		{name: "claude", tool: ToolClaude, provider: "claude", wantEmail: "claude@example.com"},
		{name: "codex", tool: ToolCodex, provider: "codex", wantEmail: "codex@example.com"},
		{name: "gemini", tool: ToolGemini, provider: "gemini", wantEmail: "gemini@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, HasAuthFiles(tt.tool))

			ident, err := watcher.extractIdentity(tt.provider, dummyPath)
			require.NoError(t, err)
			require.NotNil(t, ident)
			assert.Equal(t, tt.wantEmail, ident.Email)
		})
	}

	assert.False(t, HasAuthFiles("unknown"))
}

func TestNewWatcherAppliesDefaults(t *testing.T) {
	watcher, err := NewWatcher(authfile.NewVault(t.TempDir()), WatcherConfig{})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	assert.Equal(t, 500*time.Millisecond, watcher.config.DebounceInterval)
	assert.Equal(t, []string{"claude", "codex", "gemini"}, watcher.config.Providers)
	assert.NotNil(t, watcher.logger)
}

func TestAutoProfileNameHandlesCollisions(t *testing.T) {
	vaultDir := t.TempDir()
	vault := authfile.NewVault(vaultDir)
	watcher := &Watcher{vault: vault}

	base := "auto-" + time.Now().Format("20060102-150405")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", base), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", base+"-2"), 0700))

	candidate := watcher.autoProfileName("claude")
	assert.True(t, strings.HasPrefix(candidate, base))
	assert.NotEqual(t, base, candidate)
	assert.NotEqual(t, base+"-2", candidate)

	globalCandidate := autoProfileName(vault, "claude")
	assert.True(t, strings.HasPrefix(globalCandidate, base))
	assert.NotEqual(t, base, globalCandidate)
	assert.NotEqual(t, base+"-2", globalCandidate)
}

func TestWatchOnceDiscoversCodexAndGemini(t *testing.T) {
	root := t.TempDir()
	vault := authfile.NewVault(filepath.Join(root, "vault"))
	homeDir := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex")
	geminiHome := filepath.Join(root, "gemini")

	require.NoError(t, os.MkdirAll(homeDir, 0700))
	require.NoError(t, os.MkdirAll(codexHome, 0700))
	require.NoError(t, os.MkdirAll(geminiHome, 0700))

	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GEMINI_HOME", geminiHome)

	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"id_token":"`+makeJWTToken(t, map[string]any{"email": "codex@example.com"})+`"}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(geminiHome, "settings.json"), []byte(`{"account":{"email":"gemini@example.com"},"project_id":"proj-1"}`), 0600))

	discovered, err := WatchOnce(vault, []string{"codex", "gemini"}, slog.Default())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"codex/codex@example.com",
		"gemini/gemini@example.com",
	}, discovered)
}

func TestWatcherProcessChangeSkipsUnknownAndMissingAuth(t *testing.T) {
	root := t.TempDir()
	vaultDir := filepath.Join(root, "vault")
	homeDir := filepath.Join(root, "home")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0700))

	t.Setenv("HOME", homeDir)

	vault := authfile.NewVault(vaultDir)
	watcher, err := NewWatcher(vault, WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	watcher.processChange(filepath.Join(root, "unrelated.json"))

	missingClaudeAuth := filepath.Join(homeDir, ".claude", ".credentials.json")
	watcher.processChange(missingClaudeAuth)

	profiles, err := vault.List("claude")
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestWatcherExtractIdentityDirectPathsAndUnknownProvider(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex")
	geminiHome := filepath.Join(root, "gemini")

	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GEMINI_HOME", geminiHome)

	claudePath := writeClaudeCredentials(t, homeDir, "claude-direct@example.com", "max")
	require.NoError(t, os.MkdirAll(codexHome, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"id_token":"`+makeJWTToken(t, map[string]any{"email": "codex-direct@example.com"})+`"}}`), 0600))
	require.NoError(t, os.MkdirAll(geminiHome, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(geminiHome, "settings.json"), []byte(`{"account":{"email":"gemini-direct@example.com"}}`), 0600))

	w := &Watcher{}

	ident, err := w.extractIdentity("claude", claudePath)
	require.NoError(t, err)
	assert.Equal(t, "claude-direct@example.com", ident.Email)

	ident, err = w.extractIdentity("codex", filepath.Join(codexHome, "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, "codex-direct@example.com", ident.Email)

	ident, err = w.extractIdentity("gemini", filepath.Join(geminiHome, "settings.json"))
	require.NoError(t, err)
	assert.Equal(t, "gemini-direct@example.com", ident.Email)

	_, err = w.extractIdentity("unknown", claudePath)
	require.Error(t, err)
}

func TestWatcherExtractIdentityMissingDefaultFiles(t *testing.T) {
	root := t.TempDir()
	dummyPath := filepath.Join(root, "dummy.json")
	require.NoError(t, os.WriteFile(dummyPath, []byte("{}"), 0600))

	tests := []struct {
		name     string
		provider string
		setupEnv func(t *testing.T)
	}{
		{
			name:     "claude",
			provider: "claude",
			setupEnv: func(t *testing.T) {
				t.Setenv("HOME", filepath.Join(root, "claude-home"))
			},
		},
		{
			name:     "codex",
			provider: "codex",
			setupEnv: func(t *testing.T) {
				t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			},
		},
		{
			name:     "gemini",
			provider: "gemini",
			setupEnv: func(t *testing.T) {
				t.Setenv("GEMINI_HOME", filepath.Join(root, "gemini-home"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)
			w := &Watcher{}
			_, err := w.extractIdentity(tt.provider, dummyPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not found")
		})
	}
}

func TestWatcherStartRejectsDoubleStart(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0700))
	t.Setenv("HOME", homeDir)

	watcher, err := NewWatcher(authfile.NewVault(vaultDir), WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, watcher.Start(ctx))
	require.Error(t, watcher.Start(ctx))
}

func TestWatcherStartToleratesClosedFsnotifyWatcher(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0700))
	t.Setenv("HOME", homeDir)

	watcher, err := NewWatcher(authfile.NewVault(vaultDir), WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
	})
	require.NoError(t, err)

	require.NoError(t, watcher.watcher.Close())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, watcher.Start(ctx))
	require.NoError(t, watcher.Stop())
}

func TestWatcherEventLoopDispatchesChangeAndErrorCallbacks(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0700))
	t.Setenv("HOME", homeDir)

	var changes []string
	var reported []error
	watcher, err := NewWatcher(authfile.NewVault(vaultDir), WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
		OnChange: func(provider, path string) {
			changes = append(changes, provider+":"+path)
		},
		OnError: func(err error) {
			reported = append(reported, err)
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.eventLoop(ctx)
	}()

	claudePath := filepath.Join(homeDir, ".claude", ".credentials.json")
	watcher.watcher.Events <- fsnotify.Event{Name: claudePath, Op: fsnotify.Create}

	expectedErr := errors.New("synthetic watcher error")
	watcher.watcher.Errors <- expectedErr

	require.Eventually(t, func() bool {
		return len(changes) == 1 && len(reported) == 1
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, "claude:"+claudePath, changes[0])
	assert.ErrorIs(t, reported[0], expectedErr)

	cancel()
	<-done
}

func TestWatcherProcessChangeSkipsDuplicateAliasProfile(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)

	credsPath := writeClaudeCredentials(t, homeDir, "real@example.com", "max")
	vault := authfile.NewVault(vaultDir)
	fileSet := authfile.ClaudeAuthFiles()
	require.NoError(t, vault.Backup(fileSet, "alias@example.com"))

	var discoveries []string
	watcher, err := NewWatcher(vault, WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
		OnDiscovery: func(provider, email string, _ *identity.Identity) {
			discoveries = append(discoveries, provider+"/"+email)
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	watcher.processChange(credsPath)

	profiles, err := vault.List("claude")
	require.NoError(t, err)
	assert.Contains(t, profiles, "alias@example.com")
	assert.NotContains(t, profiles, "real@example.com")
	assert.Empty(t, discoveries)
}

func TestWatcherProcessChangeUpdatesExistingProfile(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)

	credsPath := writeClaudeCredentials(t, homeDir, "existing@example.com", "max")
	vault := authfile.NewVault(vaultDir)
	fileSet := authfile.ClaudeAuthFiles()
	require.NoError(t, vault.Backup(fileSet, "existing@example.com"))

	require.NoError(t, os.WriteFile(credsPath, []byte(`{"claudeAiOauth":{"email":"existing@example.com","subscriptionType":"team"}}`), 0600))

	var discoveries []string
	watcher, err := NewWatcher(vault, WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
		OnDiscovery: func(provider, email string, _ *identity.Identity) {
			discoveries = append(discoveries, provider+"/"+email)
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	watcher.processChange(credsPath)

	assert.Equal(t, []string{"claude/existing@example.com"}, discoveries)
	assert.Equal(t, "team", readStoredPlan(t, vault.BackupPath("claude", "existing@example.com", ".credentials.json")))
}

func TestWatcherProcessChangeSkipsAlreadyActiveProfile(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)

	credsPath := writeClaudeCredentials(t, homeDir, "active@example.com", "max")
	vault := authfile.NewVault(vaultDir)
	fileSet := authfile.ClaudeAuthFiles()
	require.NoError(t, vault.Backup(fileSet, "active@example.com"))

	var discoveries []string
	watcher, err := NewWatcher(vault, WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
		OnDiscovery: func(provider, email string, _ *identity.Identity) {
			discoveries = append(discoveries, provider+"/"+email)
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	watcher.processChange(credsPath)
	assert.Empty(t, discoveries)
}

func TestWatcherProcessChangeSkipsAutoProfileWhenActiveExists(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)

	credsPath := writeClaudeCredentials(t, homeDir, "ignored@example.com", "max")
	require.NoError(t, os.WriteFile(credsPath, []byte("{invalid"), 0600))

	vault := authfile.NewVault(vaultDir)
	fileSet := authfile.ClaudeAuthFiles()
	require.NoError(t, vault.Backup(fileSet, "saved-profile"))

	var discoveries []string
	watcher, err := NewWatcher(vault, WatcherConfig{
		Providers: []string{"claude"},
		Logger:    slog.Default(),
		OnDiscovery: func(provider, email string, _ *identity.Identity) {
			discoveries = append(discoveries, provider+"/"+email)
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	watcher.processChange(credsPath)
	assert.Empty(t, discoveries)
}

func TestWatchOnceSkipsAliasDuplicateAndActiveMatch(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)

	credsPath := writeClaudeCredentials(t, homeDir, "watch@example.com", "max")
	vault := authfile.NewVault(vaultDir)
	fileSet := authfile.ClaudeAuthFiles()

	require.NoError(t, vault.Backup(fileSet, "alias@example.com"))
	discovered, err := WatchOnce(vault, []string{"claude"}, slog.Default())
	require.NoError(t, err)
	assert.Empty(t, discovered)

	require.NoError(t, os.RemoveAll(filepath.Join(vaultDir, "claude")))
	require.NoError(t, vault.Backup(fileSet, "watch@example.com"))
	discovered, err = WatchOnce(vault, []string{"claude"}, slog.Default())
	require.NoError(t, err)
	assert.Empty(t, discovered)

	require.NoError(t, os.WriteFile(credsPath, []byte(`{"claudeAiOauth":{"email":"watch@example.com","subscriptionType":"team"}}`), 0600))
	discovered, err = WatchOnce(vault, []string{"claude"}, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, []string{"claude/watch@example.com"}, discovered)
	assert.Equal(t, "team", readStoredPlan(t, vault.BackupPath("claude", "watch@example.com", ".credentials.json")))
}

func TestWatchOnceUsesDefaultProviders(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", filepath.Join(root, "empty-codex"))
	t.Setenv("GEMINI_HOME", filepath.Join(root, "empty-gemini"))

	writeClaudeCredentials(t, homeDir, "default@example.com", "max")

	discovered, err := WatchOnce(authfile.NewVault(vaultDir), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude/default@example.com"}, discovered)
}

func TestWatchOnceIgnoresUnknownAndMissingProviders(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "empty-codex"))
	t.Setenv("GEMINI_HOME", filepath.Join(root, "empty-gemini"))

	discovered, err := WatchOnce(authfile.NewVault(t.TempDir()), []string{"unknown", "codex"}, slog.Default())
	require.NoError(t, err)
	assert.Empty(t, discovered)
}

func TestWatchOnceSkipsAutoProfileWhenActiveExists(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	vaultDir := filepath.Join(root, "vault")
	t.Setenv("HOME", homeDir)

	credsPath := writeClaudeCredentials(t, homeDir, "ignored@example.com", "max")
	require.NoError(t, os.WriteFile(credsPath, []byte("{invalid"), 0600))

	vault := authfile.NewVault(vaultDir)
	fileSet := authfile.ClaudeAuthFiles()
	require.NoError(t, vault.Backup(fileSet, "saved-profile"))

	discovered, err := WatchOnce(vault, []string{"claude"}, slog.Default())
	require.NoError(t, err)
	assert.Empty(t, discovered)
}
