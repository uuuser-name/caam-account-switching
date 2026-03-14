package runtimefixture

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/profile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
)

// RuntimeProfileVaultFixture provides a small reusable runtime fixture for
// internal/exec-style tests that need temp HOME, vault, profile store, and DB.
type RuntimeProfileVaultFixture struct {
	Harness       *testutil.ExtendedHarness
	RootDir       string
	VaultDir      string
	ProfilesDir   string
	DBPath        string
	CodexHome     string
	XDGConfigHome string
}

// NewRuntimeProfileVaultFixture creates a temp runtime fixture rooted in the
// harness temp directory and applies common environment variables.
func NewRuntimeProfileVaultFixture(h *testutil.ExtendedHarness) *RuntimeProfileVaultFixture {
	fixture := &RuntimeProfileVaultFixture{
		Harness:       h,
		RootDir:       h.TempDir,
		VaultDir:      filepath.Join(h.TempDir, "vault"),
		ProfilesDir:   filepath.Join(h.TempDir, "profiles"),
		DBPath:        filepath.Join(h.TempDir, "caam.db"),
		CodexHome:     filepath.Join(h.TempDir, ".codex"),
		XDGConfigHome: filepath.Join(h.TempDir, ".config"),
	}

	h.SetEnv("HOME", fixture.RootDir)
	h.SetEnv("XDG_CONFIG_HOME", fixture.XDGConfigHome)
	h.SetEnv("CODEX_HOME", fixture.CodexHome)

	return fixture
}

// CreateVaultProfile writes provider auth material into the temp vault.
func (f *RuntimeProfileVaultFixture) CreateVaultProfile(tb testing.TB, provider, name, authContents string) string {
	tb.Helper()

	dir := filepath.Join(f.VaultDir, provider, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatalf("mkdir vault profile dir: %v", err)
	}

	authPath := filepath.Join(dir, providerAuthFileName(provider))
	if err := os.WriteFile(authPath, []byte(authContents), 0o600); err != nil {
		tb.Fatalf("write vault auth fixture: %v", err)
	}
	return authPath
}

// SetActiveAuth writes active provider auth into the expected live location.
func (f *RuntimeProfileVaultFixture) SetActiveAuth(tb testing.TB, provider, authContents string) string {
	tb.Helper()

	activePath := f.activeAuthPath(provider)
	if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
		tb.Fatalf("mkdir active auth dir: %v", err)
	}
	if err := os.WriteFile(activePath, []byte(authContents), 0o600); err != nil {
		tb.Fatalf("write active auth fixture: %v", err)
	}
	return activePath
}

// NewVault returns a vault rooted in the temp fixture.
func (f *RuntimeProfileVaultFixture) NewVault() *authfile.Vault {
	return authfile.NewVault(f.VaultDir)
}

// OpenDB opens the temp CAAM DB for the fixture.
func (f *RuntimeProfileVaultFixture) OpenDB(tb testing.TB) *caamdb.DB {
	tb.Helper()

	db, err := caamdb.OpenAt(f.DBPath)
	if err != nil {
		tb.Fatalf("open temp caam db: %v", err)
	}
	return db
}

// CreateProfile creates a real profile in the temp profile store.
func (f *RuntimeProfileVaultFixture) CreateProfile(tb testing.TB, provider, name, authMode string) *profile.Profile {
	tb.Helper()

	store := profile.NewStore(f.ProfilesDir)
	prof, err := store.Create(provider, name, authMode)
	if err != nil {
		tb.Fatalf("create temp profile: %v", err)
	}
	return prof
}

func (f *RuntimeProfileVaultFixture) activeAuthPath(provider string) string {
	switch provider {
	case "claude":
		return filepath.Join(f.RootDir, ".claude.json")
	case "codex":
		return filepath.Join(f.CodexHome, "auth.json")
	default:
		return filepath.Join(f.RootDir, fmt.Sprintf(".%s", provider), "auth.json")
	}
}

func providerAuthFileName(provider string) string {
	switch provider {
	case "claude":
		return ".claude.json"
	default:
		return "auth.json"
	}
}
