package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/spf13/cobra"
)

func seedVaultProfile(t *testing.T, v *authfile.Vault, tool, profile, auth string) {
	t.Helper()
	profileDir := v.ProfilePath(tool, profile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "meta.json"), []byte(`{"tool":"`+tool+`","profile":"`+profile+`"}`), 0o600); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
}

func newExportTestCommand(out io.Writer, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().String("output", "", "")
	return cmd
}

func newImportTestCommand(in io.Reader, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetIn(in)
	cmd.SetOut(out)
	cmd.Flags().String("as", "", "")
	cmd.Flags().Bool("force", false, "")
	return cmd
}

func TestRunExportImportRoundTripViaStdIO(t *testing.T) {
	origVault := vault
	t.Cleanup(func() { vault = origVault })

	tmpDir := t.TempDir()
	srcVault := authfile.NewVault(filepath.Join(tmpDir, "src-vault"))
	seedVaultProfile(t, srcVault, "codex", "work", `{"access_token":"token-1"}`)
	vault = srcVault

	var archive bytes.Buffer
	var exportErr bytes.Buffer
	exportCmd := newExportTestCommand(&archive, &exportErr)
	if err := runExport(exportCmd, []string{"codex/work"}); err != nil {
		t.Fatalf("runExport failed: %v", err)
	}
	if !strings.Contains(exportErr.String(), "Exported 1 profile(s)") {
		t.Fatalf("expected export summary, got: %q", exportErr.String())
	}

	dstVault := authfile.NewVault(filepath.Join(tmpDir, "dst-vault"))
	vault = dstVault

	importCmd := newImportTestCommand(bytes.NewReader(archive.Bytes()), &bytes.Buffer{})
	if err := runImport(importCmd, []string{"-"}); err != nil {
		t.Fatalf("runImport failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dstVault.ProfilePath("codex", "work"), "auth.json"))
	if err != nil {
		t.Fatalf("read imported auth: %v", err)
	}
	if string(got) != `{"access_token":"token-1"}` {
		t.Fatalf("imported auth mismatch: %q", string(got))
	}
}

func TestRunExportUsageErrorWithoutArgsOrAll(t *testing.T) {
	origVault := vault
	t.Cleanup(func() { vault = origVault })

	vault = authfile.NewVault(filepath.Join(t.TempDir(), "vault"))
	cmd := newExportTestCommand(&bytes.Buffer{}, &bytes.Buffer{})

	err := runExport(cmd, nil)
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(err.Error(), "usage: caam export") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunImportRejectsInvalidAsFlag(t *testing.T) {
	origVault := vault
	t.Cleanup(func() { vault = origVault })

	vault = authfile.NewVault(filepath.Join(t.TempDir(), "vault"))

	cmd := newImportTestCommand(bytes.NewReader([]byte("unused")), &bytes.Buffer{})
	if err := cmd.Flags().Set("as", "invalid-format"); err != nil {
		t.Fatalf("set --as: %v", err)
	}

	err := runImport(cmd, []string{"-"})
	if err == nil {
		t.Fatal("expected invalid --as error")
	}
	if !strings.Contains(err.Error(), "invalid --as") {
		t.Fatalf("unexpected error: %v", err)
	}
}

