package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/config"
	caamdb "github.com/Dicklesworthstone/coding_agent_account_manager/internal/db"
	"github.com/spf13/cobra"
)

func setActivateRotationHomes(t *testing.T, tmpDir string) {
	t.Helper()

	codexHome := filepath.Join(tmpDir, "codex_home")
	caamHome := filepath.Join(tmpDir, "caam_home")
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CAAM_HOME", caamHome)

	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatalf("MkdirAll(CODEX_HOME) error = %v", err)
	}
	if err := os.MkdirAll(caamHome, 0o700); err != nil {
		t.Fatalf("MkdirAll(CAAM_HOME) error = %v", err)
	}
}

func TestActivate_AutoSelect_ChoosesNonCooldownProfile(t *testing.T) {
	tmpDir := t.TempDir()

	setActivateRotationHomes(t, tmpDir)

	// Create current auth state.
	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"access_token":"current"}`), 0600); err != nil {
		t.Fatalf("WriteFile(current auth) error = %v", err)
	}

	// Use a temp vault with two profiles.
	oldVault := vault
	vault = authfile.NewVault(filepath.Join(tmpDir, "vault"))
	t.Cleanup(func() { vault = oldVault })

	if err := os.MkdirAll(vault.ProfilePath("codex", "a"), 0700); err != nil {
		t.Fatalf("MkdirAll(profile a) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", "a"), "auth.json"), []byte(`{"access_token":"a"}`), 0600); err != nil {
		t.Fatalf("WriteFile(profile a) error = %v", err)
	}
	if err := os.MkdirAll(vault.ProfilePath("codex", "b"), 0700); err != nil {
		t.Fatalf("MkdirAll(profile b) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", "b"), "auth.json"), []byte(`{"access_token":"b"}`), 0600); err != nil {
		t.Fatalf("WriteFile(profile b) error = %v", err)
	}

	// Put profile a in cooldown.
	db, err := caamdb.Open()
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.SetCooldown("codex", "a", time.Now().UTC(), 60*time.Minute, ""); err != nil {
		t.Fatalf("SetCooldown() error = %v", err)
	}

	c := &cobra.Command{}
	c.Flags().Bool("backup-current", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("auto", true, "")
	_ = c.Flags().Set("auto", "true")

	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate(--auto) error = %v", err)
	}

	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(active auth) error = %v", err)
	}
	if string(got) != `{"access_token":"b"}` {
		t.Fatalf("auth mismatch: got %q want %q", string(got), `{"access_token":"b"}`)
	}
}

func TestActivate_NoProfileNoDefault_UsesRotationWhenEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	setActivateRotationHomes(t, tmpDir)

	// Enable rotation in SPM config.
	spmCfg := []byte("version: 1\nstealth:\n  rotation:\n    enabled: true\n    algorithm: smart\n")
	if err := os.WriteFile(config.SPMConfigPath(), spmCfg, 0600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	// Ensure caam global config has no default profiles for this test.
	oldCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = oldCfg })

	// Use a temp vault with one profile.
	oldVault := vault
	vault = authfile.NewVault(filepath.Join(tmpDir, "vault"))
	t.Cleanup(func() { vault = oldVault })

	if err := os.MkdirAll(vault.ProfilePath("codex", "only"), 0700); err != nil {
		t.Fatalf("MkdirAll(profile) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", "only"), "auth.json"), []byte(`{"access_token":"only"}`), 0600); err != nil {
		t.Fatalf("WriteFile(profile auth) error = %v", err)
	}

	c := &cobra.Command{}
	c.Flags().Bool("backup-current", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("auto", false, "")

	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate() error = %v", err)
	}

	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(active auth) error = %v", err)
	}
	if string(got) != `{"access_token":"only"}` {
		t.Fatalf("auth mismatch: got %q want %q", string(got), `{"access_token":"only"}`)
	}
}

func TestActivate_DefaultInCooldown_AutoSelectsAlternative(t *testing.T) {
	tmpDir := t.TempDir()

	setActivateRotationHomes(t, tmpDir)

	// Enable rotation in SPM config.
	spmCfg := []byte("version: 1\nstealth:\n  rotation:\n    enabled: true\n    algorithm: smart\n")
	if err := os.WriteFile(config.SPMConfigPath(), spmCfg, 0600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	// Set caam global config default to profile a.
	oldCfg := cfg
	cfg = config.DefaultConfig()
	cfg.SetDefault("codex", "a")
	t.Cleanup(func() { cfg = oldCfg })

	// Use a temp vault with two profiles.
	oldVault := vault
	vault = authfile.NewVault(filepath.Join(tmpDir, "vault"))
	t.Cleanup(func() { vault = oldVault })

	if err := os.MkdirAll(vault.ProfilePath("codex", "a"), 0700); err != nil {
		t.Fatalf("MkdirAll(profile a) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", "a"), "auth.json"), []byte(`{"access_token":"a"}`), 0600); err != nil {
		t.Fatalf("WriteFile(profile a) error = %v", err)
	}
	if err := os.MkdirAll(vault.ProfilePath("codex", "b"), 0700); err != nil {
		t.Fatalf("MkdirAll(profile b) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", "b"), "auth.json"), []byte(`{"access_token":"b"}`), 0600); err != nil {
		t.Fatalf("WriteFile(profile b) error = %v", err)
	}

	// Put default profile a in cooldown.
	db, err := caamdb.Open()
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.SetCooldown("codex", "a", time.Now().UTC(), 60*time.Minute, ""); err != nil {
		t.Fatalf("SetCooldown() error = %v", err)
	}

	c := &cobra.Command{}
	c.Flags().Bool("backup-current", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("auto", false, "")

	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate() error = %v", err)
	}

	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(active auth) error = %v", err)
	}
	if string(got) != `{"access_token":"b"}` {
		t.Fatalf("auth mismatch: got %q want %q", string(got), `{"access_token":"b"}`)
	}
}

// TestActivate_RotationAlgorithm_RoundRobin covers the round_robin algorithm
// selection behavior through the CLI entrypoint.
func TestActivate_RotationAlgorithm_RoundRobin(t *testing.T) {
	tmpDir := t.TempDir()

	setActivateRotationHomes(t, tmpDir)

	// Configure round_robin algorithm.
	spmCfg := []byte("version: 1\nstealth:\n  rotation:\n    enabled: true\n    algorithm: round_robin\n")
	if err := os.WriteFile(config.SPMConfigPath(), spmCfg, 0600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	// Use a temp vault with three profiles.
	oldVault := vault
	vault = authfile.NewVault(filepath.Join(tmpDir, "vault"))
	t.Cleanup(func() { vault = oldVault })

	profiles := []string{"alpha", "beta", "gamma"}
	for _, p := range profiles {
		if err := os.MkdirAll(vault.ProfilePath("codex", p), 0700); err != nil {
			t.Fatalf("MkdirAll(profile %s) error = %v", p, err)
		}
		auth := []byte(`{"access_token":"` + p + `"}`)
		if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", p), "auth.json"), auth, 0600); err != nil {
			t.Fatalf("WriteFile(profile %s auth) error = %v", p, err)
		}
	}

	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")

	// First activation with --auto: should select first in sorted order (alpha).
	c := &cobra.Command{}
	c.Flags().Bool("backup-current", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("auto", true, "")
	_ = c.Flags().Set("auto", "true")

	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate(--auto) error = %v", err)
	}

	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(auth) error = %v", err)
	}
	if string(got) != `{"access_token":"alpha"}` {
		t.Fatalf("first activation: got %q, want alpha", string(got))
	}

	// Second activation: round_robin should select next in sequence (beta).
	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate(--auto) second call error = %v", err)
	}

	got, err = os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(auth) error = %v", err)
	}
	if string(got) != `{"access_token":"beta"}` {
		t.Fatalf("second activation: got %q, want beta (round_robin should advance)", string(got))
	}

	// Third activation: should select gamma.
	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate(--auto) third call error = %v", err)
	}

	got, err = os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(auth) error = %v", err)
	}
	if string(got) != `{"access_token":"gamma"}` {
		t.Fatalf("third activation: got %q, want gamma", string(got))
	}

	// Fourth activation: should wrap around to alpha.
	if err := runActivate(c, []string{"codex"}); err != nil {
		t.Fatalf("runActivate(--auto) fourth call error = %v", err)
	}

	got, err = os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("ReadFile(auth) error = %v", err)
	}
	if string(got) != `{"access_token":"alpha"}` {
		t.Fatalf("fourth activation: got %q, want alpha (round_robin should wrap)", string(got))
	}
}

// TestActivate_RotationAlgorithm_Random covers the random algorithm
// selection behavior through the CLI entrypoint.
func TestActivate_RotationAlgorithm_Random(t *testing.T) {
	tmpDir := t.TempDir()

	setActivateRotationHomes(t, tmpDir)

	// Configure random algorithm.
	spmCfg := []byte("version: 1\nstealth:\n  rotation:\n    enabled: true\n    algorithm: random\n")
	if err := os.WriteFile(config.SPMConfigPath(), spmCfg, 0600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	// Use a temp vault with three profiles.
	oldVault := vault
	vault = authfile.NewVault(filepath.Join(tmpDir, "vault"))
	t.Cleanup(func() { vault = oldVault })

	profiles := []string{"alpha", "beta", "gamma"}
	for _, p := range profiles {
		if err := os.MkdirAll(vault.ProfilePath("codex", p), 0700); err != nil {
			t.Fatalf("MkdirAll(profile %s) error = %v", p, err)
		}
		auth := []byte(`{"access_token":"` + p + `"}`)
		if err := os.WriteFile(filepath.Join(vault.ProfilePath("codex", p), "auth.json"), auth, 0600); err != nil {
			t.Fatalf("WriteFile(profile %s auth) error = %v", p, err)
		}
	}

	authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")

	c := &cobra.Command{}
	c.Flags().Bool("backup-current", false, "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("auto", true, "")
	_ = c.Flags().Set("auto", "true")

	// Run multiple activations and verify random selects from available profiles.
	selections := make(map[string]int)
	for i := 0; i < 10; i++ {
		if err := runActivate(c, []string{"codex"}); err != nil {
			t.Fatalf("runActivate(--auto) iteration %d error = %v", i, err)
		}

		got, err := os.ReadFile(authPath)
		if err != nil {
			t.Fatalf("ReadFile(auth) error = %v", err)
		}

		selected := string(got)
		if selected != `{"access_token":"alpha"}` && selected != `{"access_token":"beta"}` && selected != `{"access_token":"gamma"}` {
			t.Fatalf("iteration %d: unexpected profile %q", i, selected)
		}
		selections[selected]++
	}

	// With 10 random selections from 3 profiles, we expect some variety.
	// This is a probabilistic test - could theoretically fail but very unlikely.
	if len(selections) < 2 {
		t.Logf("Warning: only %d unique profile(s) selected in 10 random picks", len(selections))
	}
}
