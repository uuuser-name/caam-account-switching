package runtimefixture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
)

func TestRuntimeProfileVaultFixtureLifecycle(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	fixture := NewRuntimeProfileVaultFixture(h)
	if fixture.RootDir != h.TempDir {
		t.Fatalf("RootDir = %q, want %q", fixture.RootDir, h.TempDir)
	}
	if got := os.Getenv("HOME"); got != fixture.RootDir {
		t.Fatalf("HOME = %q, want %q", got, fixture.RootDir)
	}
	if got := os.Getenv("XDG_CONFIG_HOME"); got != fixture.XDGConfigHome {
		t.Fatalf("XDG_CONFIG_HOME = %q, want %q", got, fixture.XDGConfigHome)
	}
	if got := os.Getenv("CODEX_HOME"); got != fixture.CodexHome {
		t.Fatalf("CODEX_HOME = %q, want %q", got, fixture.CodexHome)
	}

	codexVaultAuth := fixture.CreateVaultProfile(t, "codex", "primary", `{"access_token":"codex"}`)
	if got := filepath.Base(codexVaultAuth); got != "auth.json" {
		t.Fatalf("codex vault auth file = %q, want auth.json", got)
	}
	if _, err := os.Stat(codexVaultAuth); err != nil {
		t.Fatalf("Stat(codex vault auth) error = %v", err)
	}

	claudeVaultAuth := fixture.CreateVaultProfile(t, "claude", "writer", `{"access_token":"claude"}`)
	if got := filepath.Base(claudeVaultAuth); got != ".claude.json" {
		t.Fatalf("claude vault auth file = %q, want .claude.json", got)
	}

	codexActive := fixture.SetActiveAuth(t, "codex", `{"active":"codex"}`)
	if got := codexActive; got != filepath.Join(fixture.CodexHome, "auth.json") {
		t.Fatalf("codex active auth path = %q", got)
	}
	claudeActive := fixture.SetActiveAuth(t, "claude", `{"active":"claude"}`)
	if got := claudeActive; got != filepath.Join(fixture.RootDir, ".claude.json") {
		t.Fatalf("claude active auth path = %q", got)
	}

	vault := fixture.NewVault()
	profiles, err := vault.List("codex")
	if err != nil {
		t.Fatalf("vault.List(codex) error = %v", err)
	}
	if len(profiles) != 1 || profiles[0] != "primary" {
		t.Fatalf("vault codex profiles = %#v, want [primary]", profiles)
	}

	db := fixture.OpenDB(t)
	defer db.Close()
	if _, err := os.Stat(fixture.DBPath); err != nil {
		t.Fatalf("Stat(DBPath) error = %v", err)
	}

	prof := fixture.CreateProfile(t, "codex", "runner", "oauth")
	if prof.Provider != "codex" || prof.Name != "runner" || prof.AuthMode != "oauth" {
		t.Fatalf("unexpected profile basics: %+v", prof)
	}
	if got := prof.BasePath; got != filepath.Join(fixture.ProfilesDir, "codex", "runner") {
		t.Fatalf("profile BasePath = %q, want rooted profile store path", got)
	}
	if _, err := os.Stat(prof.BasePath); err != nil {
		t.Fatalf("Stat(profile BasePath) error = %v", err)
	}
}

func TestRuntimeFixtureProviderHelpers(t *testing.T) {
	tests := []struct {
		provider       string
		wantAuthFile   string
		wantActivePath string
	}{
		{
			provider:       "claude",
			wantAuthFile:   ".claude.json",
			wantActivePath: ".claude.json",
		},
		{
			provider:       "codex",
			wantAuthFile:   "auth.json",
			wantActivePath: filepath.Join(".codex", "auth.json"),
		},
		{
			provider:       "gemini",
			wantAuthFile:   "auth.json",
			wantActivePath: filepath.Join(".gemini", "auth.json"),
		},
	}

	h := testutil.NewExtendedHarness(t)
	defer h.Close()
	fixture := NewRuntimeProfileVaultFixture(h)

	for _, tc := range tests {
		if got := providerAuthFileName(tc.provider); got != tc.wantAuthFile {
			t.Fatalf("providerAuthFileName(%q) = %q, want %q", tc.provider, got, tc.wantAuthFile)
		}
		if got := fixture.activeAuthPath(tc.provider); got != filepath.Join(fixture.RootDir, tc.wantActivePath) && !(tc.provider == "codex" && got == filepath.Join(fixture.CodexHome, "auth.json")) {
			t.Fatalf("activeAuthPath(%q) = %q", tc.provider, got)
		}
	}
}
