package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_LoadMissingFile_ReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(filepath.Join(tmpDir, "projects.json"))

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got == nil {
		t.Fatalf("Load() returned nil store")
	}
	if got.Version != 1 {
		t.Fatalf("Version = %d, want 1", got.Version)
	}
	if got.Associations == nil || got.Defaults == nil {
		t.Fatalf("expected maps to be initialized")
	}
}

func TestStore_LoadCorruptFile_ReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "projects.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	store := NewStore(path)
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got == nil {
		t.Fatalf("Load() returned nil store")
	}
	if len(got.Associations) != 0 {
		t.Fatalf("Associations size = %d, want 0", len(got.Associations))
	}
}

func TestStore_SaveRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "projects.json")
	store := NewStore(path)

	data := &StoreData{
		Version: 1,
		Associations: map[string]map[string]string{
			"/tmp/project": {"claude": "work"},
		},
		Defaults: map[string]string{
			"codex": "main",
		},
	}

	if err := store.Save(data); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("Version = %d, want 1", got.Version)
	}
	if got.Defaults["codex"] != "main" {
		t.Fatalf("Defaults[codex] = %q, want %q", got.Defaults["codex"], "main")
	}
}

func TestStore_Resolve_InheritanceAndDefaults(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	clientA := filepath.Join(work, "client-a")
	subdir := filepath.Join(clientA, "subdir")

	store := NewStore(filepath.Join(base, "projects.json"))
	if err := store.SetAssociation(work, "claude", "work@company.com"); err != nil {
		t.Fatalf("SetAssociation(work) error = %v", err)
	}
	if err := store.SetAssociation(clientA, "codex", "client-main"); err != nil {
		t.Fatalf("SetAssociation(clientA) error = %v", err)
	}
	if err := store.SetDefault("gemini", "personal"); err != nil {
		t.Fatalf("SetDefault(gemini) error = %v", err)
	}

	resolved, err := store.Resolve(subdir)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := resolved.Profiles["codex"]; got != "client-main" {
		t.Fatalf("codex = %q, want %q", got, "client-main")
	}
	if got := resolved.Profiles["claude"]; got != "work@company.com" {
		t.Fatalf("claude = %q, want %q", got, "work@company.com")
	}
	if got := resolved.Profiles["gemini"]; got != "personal" {
		t.Fatalf("gemini = %q, want %q", got, "personal")
	}

	if got := resolved.Sources["codex"]; got != filepath.Clean(clientA) {
		t.Fatalf("codex source = %q, want %q", got, filepath.Clean(clientA))
	}
	if got := resolved.Sources["claude"]; got != filepath.Clean(work) {
		t.Fatalf("claude source = %q, want %q", got, filepath.Clean(work))
	}
	if got := resolved.Sources["gemini"]; got != "<default>" {
		t.Fatalf("gemini source = %q, want %q", got, "<default>")
	}
}

func TestStore_Resolve_GlobPatterns_AndExactOverride(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	clientA := filepath.Join(work, "client-a")

	store := NewStore(filepath.Join(base, "projects.json"))

	// Pattern matches /work/<anything>.
	if err := store.SetAssociation(filepath.Join(work, "*"), "claude", "pattern"); err != nil {
		t.Fatalf("SetAssociation(pattern) error = %v", err)
	}

	// Exact match should win for the same provider.
	if err := store.SetAssociation(clientA, "claude", "exact"); err != nil {
		t.Fatalf("SetAssociation(exact) error = %v", err)
	}

	resolved, err := store.Resolve(clientA)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := resolved.Profiles["claude"]; got != "exact" {
		t.Fatalf("claude = %q, want %q", got, "exact")
	}
	if got := resolved.Sources["claude"]; got != filepath.Clean(clientA) {
		t.Fatalf("claude source = %q, want %q", got, filepath.Clean(clientA))
	}
}

func TestStore_Resolve_GlobSpecificity(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	clientA := filepath.Join(work, "client-a")

	store := NewStore(filepath.Join(base, "projects.json"))

	// Two patterns that match clientA; the longer one should win.
	if err := store.SetAssociation(filepath.Join(work, "*"), "codex", "less-specific"); err != nil {
		t.Fatalf("SetAssociation(pattern1) error = %v", err)
	}
	if err := store.SetAssociation(filepath.Join(base, "*", "client-a"), "codex", "more-specific"); err != nil {
		t.Fatalf("SetAssociation(pattern2) error = %v", err)
	}

	resolved, err := store.Resolve(clientA)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := resolved.Profiles["codex"]; got != "more-specific" {
		t.Fatalf("codex = %q, want %q", got, "more-specific")
	}
	if got := resolved.Sources["codex"]; got != filepath.Clean(filepath.Join(base, "*", "client-a")) {
		t.Fatalf("codex source = %q, want %q", got, filepath.Clean(filepath.Join(base, "*", "client-a")))
	}
}

func TestStore_NormalizesProviderAndProfile(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")

	store := NewStore(filepath.Join(base, "projects.json"))

	if err := store.SetAssociation(work, "  CLAUDE  ", "  work@company.com  "); err != nil {
		t.Fatalf("SetAssociation() error = %v", err)
	}
	if err := store.SetDefault("  CODEX ", " main "); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}

	resolved, err := store.Resolve(work)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := resolved.Profiles["claude"]; got != "work@company.com" {
		t.Fatalf("claude = %q, want %q", got, "work@company.com")
	}
	if got := resolved.Profiles["codex"]; got != "main" {
		t.Fatalf("codex = %q, want %q", got, "main")
	}

	// Ensure on-disk representation is normalized too.
	key, err := normalizeKey(work)
	if err != nil {
		t.Fatalf("normalizeKey() error = %v", err)
	}
	data, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := data.Associations[key]["claude"]; got != "work@company.com" {
		t.Fatalf("stored associations[%s][claude] = %q, want %q", key, got, "work@company.com")
	}
	if got := data.Defaults["codex"]; got != "main" {
		t.Fatalf("stored defaults[codex] = %q, want %q", got, "main")
	}
}

func TestStore_NormalizesExistingStoredData(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "projects.json")
	work := filepath.Join(base, "work")

	key, err := normalizeKey(work)
	if err != nil {
		t.Fatalf("normalizeKey() error = %v", err)
	}

	// Seed a file with unnormalized keys/values (as if edited manually or from older versions).
	seed := &StoreData{
		Version: 1,
		Associations: map[string]map[string]string{
			key: {"  CLAUDE  ": "  work@company.com  "},
		},
		Defaults: map[string]string{
			"  CODEX  ": " main ",
		},
	}
	raw, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := NewStore(path)

	// Ensure a normal command works even if the stored provider key is unnormalized.
	if err := store.RemoveAssociation(work, "claude"); err != nil {
		t.Fatalf("RemoveAssociation() error = %v", err)
	}

	// Ensure setting a normalized default doesn't leave multiple variants behind.
	if err := store.SetDefault("codex", "secondary"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}

	data, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := data.Associations[key]; ok {
		t.Fatalf("expected associations[%s] to be removed", key)
	}
	if got := data.Defaults["codex"]; got != "secondary" {
		t.Fatalf("defaults[codex] = %q, want %q", got, "secondary")
	}
	if _, ok := data.Defaults["  CODEX  "]; ok {
		t.Fatalf("expected unnormalized default key to be removed")
	}
}

func TestGlobNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "projects.json")
	store := NewStore(storePath)

	// Intention: Allow any directory named "frontend" anywhere to use "work" profile
	// Pattern: */frontend
	err := store.SetAssociation("*/frontend", "claude", "work")
	if err != nil {
		t.Fatalf("SetAssociation failed: %v", err)
	}

	data, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// We expect one key
	if len(data.Associations) != 1 {
		t.Fatalf("expected 1 association, got %d", len(data.Associations))
	}

	// Extract the key
	var key string
	for k := range data.Associations {
		key = k
	}

	// Check if the key was absolutized
	if filepath.IsAbs(key) {
		t.Errorf("expected relative glob key '*/frontend', got absolute path '%s'", key)
	}
}

func TestDefaultPath(t *testing.T) {
	// Save original env var
	originalCaamHome := os.Getenv("CAAM_HOME")
	defer os.Setenv("CAAM_HOME", originalCaamHome)

	t.Run("uses CAAM_HOME when set", func(t *testing.T) {
		customPath := "/custom/caam/dir"
		os.Setenv("CAAM_HOME", customPath)

		got := DefaultPath()
		want := filepath.Join(customPath, "projects.json")
		if got != want {
			t.Errorf("DefaultPath() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to home dir when CAAM_HOME unset", func(t *testing.T) {
		os.Setenv("CAAM_HOME", "")

		got := DefaultPath()
		homeDir, err := os.UserHomeDir()
		if err != nil {
			// If UserHomeDir fails, should fall back to .caam
			want := filepath.Join(".caam", "projects.json")
			if got != want {
				t.Errorf("DefaultPath() = %q, want %q (when UserHomeDir fails)", got, want)
			}
			return
		}

		want := filepath.Join(homeDir, ".caam", "projects.json")
		if got != want {
			t.Errorf("DefaultPath() = %q, want %q", got, want)
		}
	})
}

func TestStore_Path(t *testing.T) {
	customPath := "/some/custom/path/projects.json"
	store := NewStore(customPath)

	got := store.Path()
	if got != customPath {
		t.Errorf("Path() = %q, want %q", got, customPath)
	}
}

func TestStore_DeleteProject(t *testing.T) {
	base := t.TempDir()
	projectPath := filepath.Join(base, "my-project")
	storePath := filepath.Join(base, "projects.json")

	store := NewStore(storePath)

	// Set up some associations for the project
	if err := store.SetAssociation(projectPath, "claude", "work@company.com"); err != nil {
		t.Fatalf("SetAssociation(claude) error = %v", err)
	}
	if err := store.SetAssociation(projectPath, "codex", "main"); err != nil {
		t.Fatalf("SetAssociation(codex) error = %v", err)
	}

	// Verify associations exist
	data, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	key, err := normalizeKey(projectPath)
	if err != nil {
		t.Fatalf("normalizeKey() error = %v", err)
	}

	if _, ok := data.Associations[key]; !ok {
		t.Fatalf("expected associations for %s to exist before delete", key)
	}

	// Delete the project
	if err := store.DeleteProject(projectPath); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}

	// Verify associations are removed
	data, err = store.Load()
	if err != nil {
		t.Fatalf("Load() after delete error = %v", err)
	}

	if _, ok := data.Associations[key]; ok {
		t.Fatalf("expected associations for %s to be removed after delete", key)
	}
}

func TestStore_DeleteProject_NonExistent(t *testing.T) {
	base := t.TempDir()
	storePath := filepath.Join(base, "projects.json")

	store := NewStore(storePath)

	// Delete a project that doesn't exist - should succeed (no-op)
	nonExistentPath := filepath.Join(base, "non-existent-project")
	if err := store.DeleteProject(nonExistentPath); err != nil {
		t.Fatalf("DeleteProject() for non-existent project error = %v", err)
	}
}

func TestStore_DeleteProject_InvalidPath(t *testing.T) {
	base := t.TempDir()
	storePath := filepath.Join(base, "projects.json")

	store := NewStore(storePath)

	// Try to delete with empty path - should fail
	err := store.DeleteProject("")
	if err == nil {
		t.Fatal("DeleteProject(\"\") expected error, got nil")
	}
}
