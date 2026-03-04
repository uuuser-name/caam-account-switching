package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModel_ProjectAssociation_MarksProjectDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CAAM_HOME", tmpDir)

	projectDir := filepath.Join(tmpDir, "project-a")
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}

	m := New()
	m.cwd = projectDir

	// Seed a vault profile in model state.
	model, _ := m.Update(profilesLoadedMsg{profiles: map[string][]Profile{
		"claude": {{Name: "alice@example.com", Provider: "claude"}},
	}})
	m = model.(Model)

	if err := m.projectStore.SetAssociation(projectDir, "claude", "alice@example.com"); err != nil {
		t.Fatalf("SetAssociation() error = %v", err)
	}
	resolved, err := m.projectStore.Resolve(projectDir)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	model, _ = m.Update(projectContextLoadedMsg{cwd: projectDir, resolved: resolved})
	m = model.(Model)

	selected := m.profilesPanel.GetSelectedProfile()
	if selected == nil {
		t.Fatalf("expected selected profile")
	}
	if !selected.ProjectDefault {
		t.Fatalf("ProjectDefault = false, want true")
	}
}
