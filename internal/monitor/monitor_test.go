package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
)

type fakeFetcher struct {
	usages map[string]*usage.UsageInfo
}

func (f *fakeFetcher) FetchAllProfiles(ctx context.Context, provider string, profiles map[string]string) []usage.ProfileUsage {
	results := make([]usage.ProfileUsage, 0, len(profiles))
	for name := range profiles {
		key := provider + "/" + name
		info := f.usages[key]
		if info == nil {
			info = &usage.UsageInfo{
				Provider:    provider,
				ProfileName: name,
				Error:       "missing usage",
				FetchedAt:   time.Now(),
			}
		}
		results = append(results, usage.ProfileUsage{
			Provider:    provider,
			ProfileName: name,
			Usage:       info,
		})
	}
	return results
}

func TestMonitorRefreshBuildsState(t *testing.T) {
	tmpDir := t.TempDir()
	vault := authfile.NewVault(tmpDir)

	writeProfileFile(t, vault, "claude", "alice", ".credentials.json", `{"claudeAiOauth":{"accessToken":"tok-claude"}}`)
	writeProfileFile(t, vault, "codex", "bob", "auth.json", `{"tokens":{"access_token":"tok-codex"}}`)

	fetcher := &fakeFetcher{
		usages: map[string]*usage.UsageInfo{
			"claude/alice": {
				Provider:    "claude",
				ProfileName: "alice",
				PrimaryWindow: &usage.UsageWindow{
					UsedPercent: 80,
				},
			},
			"codex/bob": {
				Provider:    "codex",
				ProfileName: "bob",
				PrimaryWindow: &usage.UsageWindow{
					UsedPercent: 20,
				},
			},
		},
	}

	mon := NewMonitor(
		WithVault(vault),
		WithFetcher(fetcher),
		WithProviders([]string{"claude", "codex"}),
		WithHealthStore(nil),
	)

	if err := mon.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	state := mon.GetState()
	if state == nil {
		t.Fatal("GetState() returned nil")
	}
	if len(state.Profiles) != 2 {
		t.Fatalf("Profiles count = %d, want 2", len(state.Profiles))
	}

	claude := state.Profiles["claude/alice"]
	if claude == nil {
		t.Fatal("missing claude/alice profile")
	}
	if claude.Alert == nil || claude.Alert.Type != AlertWarning {
		t.Fatalf("claude alert = %v, want warning", claude.Alert)
	}

	codex := state.Profiles["codex/bob"]
	if codex == nil {
		t.Fatal("missing codex/bob profile")
	}
	if codex.Alert != nil {
		t.Fatalf("codex alert = %v, want nil", codex.Alert)
	}
}

func TestMonitorRefreshUnsupportedProvider(t *testing.T) {
	tmpDir := t.TempDir()
	vault := authfile.NewVault(tmpDir)
	writeProfileDir(t, vault, "gemini", "carol")

	mon := NewMonitor(
		WithVault(vault),
		WithFetcher(&fakeFetcher{}),
		WithProviders([]string{"gemini"}),
		WithHealthStore(nil),
	)

	if err := mon.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	state := mon.GetState()
	profile := state.Profiles["gemini/carol"]
	if profile == nil || profile.Usage == nil || profile.Usage.Error == "" {
		t.Fatalf("expected usage error for gemini profile, got %+v", profile)
	}
}

func TestMonitorRefreshClaudeFallbackAuth(t *testing.T) {
	tmpDir := t.TempDir()
	vault := authfile.NewVault(tmpDir)

	writeProfileFile(t, vault, "claude", "legacy", ".claude.json", `{"oauthToken":"tok-legacy"}`)

	fetcher := &fakeFetcher{
		usages: map[string]*usage.UsageInfo{
			"claude/legacy": {
				Provider:    "claude",
				ProfileName: "legacy",
				PrimaryWindow: &usage.UsageWindow{
					UsedPercent: 10,
				},
			},
		},
	}

	mon := NewMonitor(
		WithVault(vault),
		WithFetcher(fetcher),
		WithProviders([]string{"claude"}),
		WithHealthStore(nil),
	)

	if err := mon.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	state := mon.GetState()
	profile := state.Profiles["claude/legacy"]
	if profile == nil || profile.Usage == nil || profile.Usage.Error != "" {
		t.Fatalf("expected legacy claude profile usage, got %+v", profile)
	}
}

func writeProfileFile(t *testing.T, vault *authfile.Vault, provider, profile, name, contents string) {
	t.Helper()
	dir := vault.ProfilePath(provider, profile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeProfileDir(t *testing.T, vault *authfile.Vault, provider, profile string) {
	t.Helper()
	dir := vault.ProfilePath(provider, profile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}
