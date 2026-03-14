package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/spf13/cobra"
)

type watchCommandTestEnv struct {
	codexHome string
	vault     *authfile.Vault
}

func setupWatchCommandTestEnv(t *testing.T) watchCommandTestEnv {
	t.Helper()

	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex-home")
	vaultPath := filepath.Join(root, "vault")

	for _, dir := range []string{homeDir, codexHome, vaultPath} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GEMINI_HOME", filepath.Join(root, "gemini-home"))
	t.Setenv("CAAM_HOME", filepath.Join(root, "caam-home"))

	oldVault := vault
	testVault := authfile.NewVault(vaultPath)
	vault = testVault
	t.Cleanup(func() { vault = oldVault })

	oldOnce := watchOnce
	oldProviders := append([]string(nil), watchProviders...)
	oldVerbose := watchVerbose
	t.Cleanup(func() {
		watchOnce = oldOnce
		watchProviders = oldProviders
		watchVerbose = oldVerbose
	})

	return watchCommandTestEnv{
		codexHome: codexHome,
		vault:     testVault,
	}
}

func writeCodexAuthFile(t *testing.T, codexHome, email string) {
	t.Helper()

	payload := map[string]any{
		"email":     email,
		"plan_type": "pro",
	}
	token := buildWatchTestJWT(t, payload)
	authPath := filepath.Join(codexHome, "auth.json")
	data, err := json.Marshal(map[string]string{"id_token": token})
	if err != nil {
		t.Fatalf("json.Marshal(auth) error = %v", err)
	}
	if err := os.WriteFile(authPath, data, 0600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", authPath, err)
	}
}

func buildWatchTestJWT(t *testing.T, payload map[string]any) string {
	t.Helper()

	headerJSON, err := json.Marshal(map[string]string{
		"alg": "none",
		"typ": "JWT",
	})
	if err != nil {
		t.Fatalf("json.Marshal(header) error = %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON) + ".signature"
}

func TestRunWatch_OnceDiscoversCodexProfileFromNormalizedProvider(t *testing.T) {
	env := setupWatchCommandTestEnv(t)
	writeCodexAuthFile(t, env.codexHome, "watcher@example.com")

	watchOnce = true
	watchProviders = []string{" CODEX "}
	watchVerbose = false

	out, err := captureStdout(t, func() error {
		return runWatch(&cobra.Command{}, nil)
	})
	if err != nil {
		t.Fatalf("runWatch(--once) error = %v", err)
	}
	if !strings.Contains(out, "Scanning current auth files...") {
		t.Fatalf("watch once output missing scan banner: %q", out)
	}
	if !strings.Contains(out, "Discovered 1 account(s):") {
		t.Fatalf("watch once output missing discovery summary: %q", out)
	}
	if !strings.Contains(out, "codex/watcher@example.com") {
		t.Fatalf("watch once output missing discovered profile: %q", out)
	}

	profiles, err := env.vault.List("codex")
	if err != nil {
		t.Fatalf("vault.List(codex) error = %v", err)
	}
	if len(profiles) != 1 || profiles[0] != "watcher@example.com" {
		t.Fatalf("vault codex profiles = %#v, want watcher@example.com", profiles)
	}
}

func TestRunWatch_RejectsUnknownProvider(t *testing.T) {
	_ = setupWatchCommandTestEnv(t)

	watchOnce = true
	watchProviders = []string{"mystery"}
	watchVerbose = false

	err := runWatch(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("runWatch(unknown provider) error = %v, want unknown-provider error", err)
	}
}

func TestRunWatch_DaemonStopsOnContextCancel(t *testing.T) {
	env := setupWatchCommandTestEnv(t)
	_ = env

	watchOnce = false
	watchProviders = []string{"codex"}
	watchVerbose = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(250*time.Millisecond, cancel)

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)

	out, err := captureStdout(t, func() error {
		return runWatch(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runWatch(daemon) error = %v", err)
	}
	if !strings.Contains(out, "Starting auth file watcher...") {
		t.Fatalf("daemon output missing start banner: %q", out)
	}
	if !strings.Contains(out, "Watching providers: codex") {
		t.Fatalf("daemon output missing provider list: %q", out)
	}
	if !strings.Contains(out, "Watching for auth file changes...") {
		t.Fatalf("daemon output missing active watcher banner: %q", out)
	}
	if !strings.Contains(out, "Watcher stopped.") {
		t.Fatalf("daemon output missing stop banner: %q", out)
	}
}
