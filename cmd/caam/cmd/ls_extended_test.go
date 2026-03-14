package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/authfile"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLsCommand_Extended(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	// 1. Setup
	h.StartStep("Setup", "Create vault profiles")

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	defer func() {
		vault = originalVault
		tools = originalTools
	}()

	vaultDir := filepath.Join(h.TempDir, "vault")
	vault = authfile.NewVault(vaultDir)

	// Use simplified file sets so ls can detect readable auth files in profile folders.
	tools["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "codex",
			Files: []authfile.AuthFileSpec{
				{Path: filepath.Join("/tmp", "codex", "auth.json"), Required: true},
			},
		}
	}
	tools["claude"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{
			Tool: "claude",
			Files: []authfile.AuthFileSpec{
				{Path: filepath.Join("/tmp", "claude", ".credentials.json"), Required: true},
			},
		}
	}
	tools["gemini"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "gemini", Files: []authfile.AuthFileSpec{}}
	}

	writeProfileAuth := map[string]func(string, string) error{
		"claude": writeClaudeProfileAuth,
		"codex":  writeCodexProfileAuth,
	}

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "work"), 0755))
	require.NoError(t, writeProfileAuth["claude"](filepath.Join(vaultDir, "claude", "work", ".credentials.json"), "work@gmail.com"))

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "personal"), 0755))
	require.NoError(t, writeProfileAuth["claude"](filepath.Join(vaultDir, "claude", "personal", ".credentials.json"), "personal@gmail.com"))

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "_original"), 0755))
	require.NoError(t, writeProfileAuth["claude"](filepath.Join(vaultDir, "claude", "_original", ".credentials.json"), "work@gmail.com"))

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "project-x"), 0755))
	require.NoError(t, writeProfileAuth["codex"](filepath.Join(vaultDir, "codex", "project-x", "auth.json"), "project-x@gmail.com"))

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "leothehumanbeing.contact"), 0755))
	require.NoError(t, writeProfileAuth["codex"](filepath.Join(vaultDir, "codex", "leothehumanbeing.contact", "auth.json"), "leothehumanbeing@gmail.com"))

	h.EndStep("Setup")

	// 2. Execute ls --json
	h.StartStep("Execute", "Run ls --json")
	require.NoError(t, lsCmd.Flags().Set("json", "true"))
	require.NoError(t, lsCmd.Flags().Set("tag", ""))
	require.NoError(t, lsCmd.Flags().Set("all", "false"))

	buf := &bytes.Buffer{}
	lsCmd.SetOut(buf)
	err := runLs(lsCmd, []string{})
	require.NoError(t, err)

	var output lsOutput
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)
	if output.Count == 0 && len(output.Profiles) > 0 {
		t.Errorf("inconsistent output count: count=0 profiles=%d", len(output.Profiles))
	}
	h.EndStep("Execute")

	// 3. Verify
	h.StartStep("Verify", "Check JSON output")
	require.Equal(t, 4, output.Count)

	toolNames := make([]string, 0, len(output.Profiles))
	for _, p := range output.Profiles {
		toolNames = append(toolNames, p.Tool+"/"+p.Name)
	}
	sort.Strings(toolNames)

	assert.Equal(t, []string{"claude/personal", "claude/work", "codex/leothehumanbeing.contact", "codex/project-x"}, toolNames)

	for _, p := range output.Profiles {
		assert.False(t, p.System, "system profile must be hidden by default")
		assert.True(t, p.Usable)
		assert.Empty(t, p.Warnings)
	}

	h.EndStep("Verify")
}

// TestLsCommand_FilterByTool tests `ls claude`
func TestLsCommand_FilterByTool(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	defer func() {
		vault = originalVault
		tools = originalTools
	}()

	vaultDir := filepath.Join(h.TempDir, "vault")
	vault = authfile.NewVault(vaultDir)

	tools["claude"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "claude", Files: []authfile.AuthFileSpec{
			{Path: filepath.Join("/tmp", "claude", ".credentials.json"), Required: true},
		}}
	}
	tools["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "codex", Files: []authfile.AuthFileSpec{
			{Path: filepath.Join("/tmp", "codex", "auth.json"), Required: true},
		}}
	}
	tools["gemini"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "gemini", Files: []authfile.AuthFileSpec{}}
	}

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "work"), 0755))
	require.NoError(t, writeClaudeProfileAuth(filepath.Join(vaultDir, "claude", "work", ".credentials.json"), "work@gmail.com"))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "claude", "leothehumanbeing.contact"), 0755))
	require.NoError(t, writeClaudeProfileAuth(filepath.Join(vaultDir, "claude", "leothehumanbeing.contact", ".credentials.json"), "leothehumanbeing@gmail.com"))

	require.NoError(t, lsCmd.Flags().Set("json", "true"))
	require.NoError(t, lsCmd.Flags().Set("all", "false"))

	buf := &bytes.Buffer{}
	lsCmd.SetOut(buf)
	err := runLs(lsCmd, []string{"claude"})
	require.NoError(t, err)

	var output lsOutput
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)

	assert.Equal(t, 2, output.Count)
	assert.Equal(t, 2, len(output.Profiles))
	names := make(map[string]struct{}, len(output.Profiles))
	for _, p := range output.Profiles {
		names[p.Name] = struct{}{}
		assert.True(t, p.Usable)
		assert.False(t, p.System)
		assert.Empty(t, p.Warnings)
	}
	assert.Contains(t, names, "work")
	assert.Contains(t, names, "leothehumanbeing.contact")
}

func TestLsCommand_AllShowsHiddenProfiles(t *testing.T) {
	h := testutil.NewExtendedHarness(t)
	defer h.Close()

	originalVault := vault
	originalTools := make(map[string]func() authfile.AuthFileSet)
	for k, v := range tools {
		originalTools[k] = v
	}
	defer func() {
		vault = originalVault
		tools = originalTools
	}()

	vaultDir := filepath.Join(h.TempDir, "vault")
	vault = authfile.NewVault(vaultDir)

	tools["codex"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "codex", Files: []authfile.AuthFileSpec{
			{Path: filepath.Join("/tmp", "codex", "auth.json"), Required: true},
		}}
	}
	tools["claude"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "claude", Files: []authfile.AuthFileSpec{
			{Path: filepath.Join("/tmp", "claude", ".credentials.json"), Required: true},
		}}
	}
	tools["gemini"] = func() authfile.AuthFileSet {
		return authfile.AuthFileSet{Tool: "gemini", Files: []authfile.AuthFileSpec{}}
	}

	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "work"), 0755))
	require.NoError(t, writeCodexProfileAuth(filepath.Join(vaultDir, "codex", "work", "auth.json"), "work@gmail.com"))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "_auto_backup_1"), 0755))
	require.NoError(t, writeCodexProfileAuth(filepath.Join(vaultDir, "codex", "_auto_backup_1", "auth.json"), "work@gmail.com"))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "codex", "badalias"), 0755))
	require.NoError(t, writeCodexProfileAuth(filepath.Join(vaultDir, "codex", "badalias", "auth.json"), "work@gmail.com"))

	buf := &bytes.Buffer{}
	require.NoError(t, lsCmd.Flags().Set("json", "true"))
	require.NoError(t, lsCmd.Flags().Set("all", "false"))
	lsCmd.SetOut(buf)
	err := runLs(lsCmd, []string{"codex"})
	require.NoError(t, err)

	var output lsOutput
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)

	assert.Equal(t, 1, output.Count)
	assert.Equal(t, "work", output.Profiles[0].Name)
	assert.True(t, output.Profiles[0].Usable)

	buf.Reset()
	require.NoError(t, lsCmd.Flags().Set("all", "true"))
	lsCmd.SetOut(buf)
	err = runLs(lsCmd, []string{"codex"})
	require.NoError(t, err)

	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)

	assert.Equal(t, 3, output.Count)
	names := make(map[string]lsProfile)
	for _, p := range output.Profiles {
		names[p.Name] = p
	}
	assert.True(t, names["_auto_backup_1"].System)
	assert.False(t, names["_auto_backup_1"].Usable)
	assert.Len(t, names["_auto_backup_1"].Warnings, 1)
	assert.Equal(t, "identity mismatch: work@gmail.com", names["_auto_backup_1"].Warnings[0])
	assert.False(t, names["badalias"].Usable)
	assert.Len(t, names["badalias"].Warnings, 1)
	assert.Equal(t, "identity mismatch: work@gmail.com", names["badalias"].Warnings[0])
}

func writeCodexProfileAuth(path, email string) error {
	token := makeTestJWT(map[string]interface{}{"email": email})
	payload := map[string]interface{}{
		"id_token": token,
	}
	return writeJSON(path, payload)
}

func writeClaudeProfileAuth(path, email string) error {
	payload := map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"email":            email,
			"subscriptionType": "pro",
		},
	}
	return writeJSON(path, payload)
}

func writeJSON(path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func makeTestJWT(claims map[string]interface{}) string {
	header := map[string]interface{}{"alg": "none", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(claims)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return headerB64 + "." + payloadB64 + ".sig"
}
