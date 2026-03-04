package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatcher_TranslateEvent_IgnoresProviderRootFiles(t *testing.T) {
	tmpDir := t.TempDir()
	providerDir := filepath.Join(tmpDir, "codex")
	if err := os.MkdirAll(providerDir, 0700); err != nil {
		t.Fatalf("mkdir provider: %v", err)
	}

	dsStorePath := filepath.Join(providerDir, ".DS_Store")
	if err := os.WriteFile(dsStorePath, []byte("x"), 0600); err != nil {
		t.Fatalf("write .DS_Store: %v", err)
	}

	w := &Watcher{profilesDir: tmpDir}
	if got := w.translateEvent(fsnotify.Event{Name: dsStorePath, Op: fsnotify.Create}); got != nil {
		t.Fatalf("translateEvent() = %+v, want nil", *got)
	}
}

func TestWatcher_TranslateEvent_ParsesProviderAndProfile(t *testing.T) {
	tmpDir := t.TempDir()
	profileDir := filepath.Join(tmpDir, "codex", "work")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}

	profileJSON := filepath.Join(profileDir, "profile.json")
	if err := os.WriteFile(profileJSON, []byte("{}"), 0600); err != nil {
		t.Fatalf("write profile.json: %v", err)
	}

	w := &Watcher{profilesDir: tmpDir, debouncer: nil}
	got := w.translateEvent(fsnotify.Event{Name: profileJSON, Op: fsnotify.Write})
	if got == nil {
		t.Fatalf("translateEvent() = nil, want event")
	}
	if got.Provider != "codex" {
		t.Fatalf("Provider = %q, want %q", got.Provider, "codex")
	}
	if got.Profile != "work" {
		t.Fatalf("Profile = %q, want %q", got.Profile, "work")
	}
	if got.Type != EventProfileModified {
		t.Fatalf("Type = %v, want %v", got.Type, EventProfileModified)
	}
}

func TestWatcher_EndToEnd_ProfileLifecycle(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := NewWithDebounceDelay(tmpDir, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWithDebounceDelay() error = %v", err)
	}
	defer w.Close()

	profileDir := filepath.Join(tmpDir, "codex", "work")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}

	// We should receive an "added" event even when provider and profile directories
	// are created in rapid succession (common on first-run).
	waitForEvent(t, w.Events(), func(e Event) bool {
		return e.Type == EventProfileAdded && e.Provider == "codex" && e.Profile == "work"
	})

	profileJSON := filepath.Join(profileDir, "profile.json")
	if err := os.WriteFile(profileJSON, []byte("{}"), 0600); err != nil {
		t.Fatalf("write profile.json: %v", err)
	}

	waitForEvent(t, w.Events(), func(e Event) bool {
		return e.Type == EventProfileModified && e.Provider == "codex" && e.Profile == "work"
	})

	if err := os.RemoveAll(profileDir); err != nil {
		t.Fatalf("remove profile: %v", err)
	}

	waitForEvent(t, w.Events(), func(e Event) bool {
		return e.Type == EventProfileDeleted && e.Provider == "codex" && e.Profile == "work"
	})
}

func waitForEvent(t *testing.T, ch <-chan Event, match func(Event) bool) Event {
	t.Helper()

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("events channel closed while waiting")
			}
			if match(e) {
				return e
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for event")
		}
	}
}

// Test EventType String() method - 0% coverage
func TestEventType_String(t *testing.T) {
	tests := []struct {
		et   EventType
		want string
	}{
		{EventProfileAdded, "profile_added"},
		{EventProfileModified, "profile_modified"},
		{EventProfileDeleted, "profile_deleted"},
		{EventType(999), "unknown(999)"},
	}

	for _, tc := range tests {
		got := tc.et.String()
		if got != tc.want {
			t.Errorf("EventType(%d).String() = %q, want %q", tc.et, got, tc.want)
		}
	}
}

// Test New() wrapper function - 0% coverage
func TestNew(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Close()

	if w == nil {
		t.Fatal("New() returned nil watcher")
	}

	// Verify it uses the default debounce delay by checking watcher is functional
	if w.Events() == nil {
		t.Error("Events() channel is nil")
	}
}

// Test New() with empty profilesDir
func TestNew_EmptyDir(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Fatal("New() with empty dir should return error")
	}
}

// Test Errors() channel accessor - 0% coverage
func TestWatcher_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := NewWithDebounceDelay(tmpDir, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWithDebounceDelay() error = %v", err)
	}
	defer w.Close()

	errChan := w.Errors()
	if errChan == nil {
		t.Fatal("Errors() returned nil")
	}
}

// Test Close() on nil watcher
func TestWatcher_Close_Nil(t *testing.T) {
	var w *Watcher
	err := w.Close()
	if err != nil {
		t.Errorf("Close() on nil watcher returned error: %v", err)
	}
}

// Test Close() called twice - idempotent
func TestWatcher_Close_Twice(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := NewWithDebounceDelay(tmpDir, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWithDebounceDelay() error = %v", err)
	}

	// First close
	err = w.Close()
	if err != nil {
		t.Errorf("First Close() error = %v", err)
	}

	// Second close should not panic
	err = w.Close()
	// Errors are acceptable on second close, just ensure no panic
}

// Test translateEvent with nil watcher
func TestWatcher_TranslateEvent_NilWatcher(t *testing.T) {
	var w *Watcher
	got := w.translateEvent(fsnotify.Event{Name: "/test/path", Op: fsnotify.Write})
	if got != nil {
		t.Errorf("translateEvent() on nil watcher = %+v, want nil", got)
	}
}

// Test translateEvent with empty event name
func TestWatcher_TranslateEvent_EmptyName(t *testing.T) {
	w := &Watcher{profilesDir: "/tmp/test"}
	got := w.translateEvent(fsnotify.Event{Name: "", Op: fsnotify.Write})
	if got != nil {
		t.Errorf("translateEvent() with empty name = %+v, want nil", got)
	}
}

// Test translateEvent for paths outside profilesDir
func TestWatcher_TranslateEvent_OutsidePath(t *testing.T) {
	tmpDir := t.TempDir()
	w := &Watcher{profilesDir: filepath.Join(tmpDir, "profiles")}

	// Path outside the profiles dir
	outsidePath := filepath.Join(tmpDir, "other", "file.txt")
	got := w.translateEvent(fsnotify.Event{Name: outsidePath, Op: fsnotify.Write})
	if got != nil {
		t.Errorf("translateEvent() outside path = %+v, want nil", got)
	}
}

// Test translateEvent for profile root (only provider, no profile)
func TestWatcher_TranslateEvent_ProviderOnly(t *testing.T) {
	tmpDir := t.TempDir()
	providerDir := filepath.Join(tmpDir, "codex")
	if err := os.MkdirAll(providerDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := &Watcher{profilesDir: tmpDir}
	got := w.translateEvent(fsnotify.Event{Name: providerDir, Op: fsnotify.Write})
	if got != nil {
		t.Errorf("translateEvent() for provider-only path = %+v, want nil", got)
	}
}

// Test translateEvent with profile directory delete
func TestWatcher_TranslateEvent_ProfileDelete(t *testing.T) {
	tmpDir := t.TempDir()
	profileDir := filepath.Join(tmpDir, "codex", "work")

	w := &Watcher{profilesDir: tmpDir}
	got := w.translateEvent(fsnotify.Event{Name: profileDir, Op: fsnotify.Remove})
	if got == nil {
		t.Fatal("translateEvent() for profile delete = nil, want event")
	}
	if got.Type != EventProfileDeleted {
		t.Errorf("Type = %v, want %v", got.Type, EventProfileDeleted)
	}
}

// Test translateEvent with profile directory rename
func TestWatcher_TranslateEvent_ProfileRename(t *testing.T) {
	tmpDir := t.TempDir()
	profileDir := filepath.Join(tmpDir, "codex", "work")

	w := &Watcher{profilesDir: tmpDir}
	got := w.translateEvent(fsnotify.Event{Name: profileDir, Op: fsnotify.Rename})
	if got == nil {
		t.Fatal("translateEvent() for profile rename = nil, want event")
	}
	if got.Type != EventProfileDeleted {
		t.Errorf("Type = %v, want %v", got.Type, EventProfileDeleted)
	}
}

// Test translateEvent ignores .git paths
func TestWatcher_TranslateEvent_IgnoresGit(t *testing.T) {
	tmpDir := t.TempDir()
	gitPath := filepath.Join(tmpDir, "codex", "work", ".git", "config")

	w := &Watcher{profilesDir: tmpDir}
	got := w.translateEvent(fsnotify.Event{Name: gitPath, Op: fsnotify.Write})
	if got != nil {
		t.Errorf("translateEvent() for .git path = %+v, want nil", got)
	}
}

// Test translateEvent ignores Thumbs.db
func TestWatcher_TranslateEvent_IgnoresThumbsDb(t *testing.T) {
	tmpDir := t.TempDir()
	thumbsPath := filepath.Join(tmpDir, "codex", "work", "Thumbs.db")

	w := &Watcher{profilesDir: tmpDir}
	got := w.translateEvent(fsnotify.Event{Name: thumbsPath, Op: fsnotify.Create})
	if got != nil {
		t.Errorf("translateEvent() for Thumbs.db = %+v, want nil", got)
	}
}

// Test translateEvent ignores desktop.ini
func TestWatcher_TranslateEvent_IgnoresDesktopIni(t *testing.T) {
	tmpDir := t.TempDir()
	iniPath := filepath.Join(tmpDir, "codex", "work", "desktop.ini")

	w := &Watcher{profilesDir: tmpDir}
	got := w.translateEvent(fsnotify.Event{Name: iniPath, Op: fsnotify.Create})
	if got != nil {
		t.Errorf("translateEvent() for desktop.ini = %+v, want nil", got)
	}
}

// Test eventTypeForPath with various operations
func TestEventTypeForPath(t *testing.T) {
	tests := []struct {
		name     string
		fullPath string
		relParts []string
		op       fsnotify.Op
		wantNil  bool
		wantType EventType
	}{
		{
			name:     "single part (provider only)",
			fullPath: "/profiles/codex",
			relParts: []string{"codex"},
			op:       fsnotify.Write,
			wantNil:  true,
		},
		{
			name:     "deep path modification",
			fullPath: "/profiles/codex/work/auth/token.json",
			relParts: []string{"codex", "work", "auth", "token.json"},
			op:       fsnotify.Write,
			wantNil:  false,
			wantType: EventProfileModified,
		},
		{
			name:     "deep path create",
			fullPath: "/profiles/codex/work/auth/token.json",
			relParts: []string{"codex", "work", "auth", "token.json"},
			op:       fsnotify.Create,
			wantNil:  false,
			wantType: EventProfileModified,
		},
		{
			name:     "deep path chmod",
			fullPath: "/profiles/codex/work/config.json",
			relParts: []string{"codex", "work", "config.json"},
			op:       fsnotify.Chmod,
			wantNil:  false,
			wantType: EventProfileModified,
		},
		{
			name:     "deep path remove",
			fullPath: "/profiles/codex/work/config.json",
			relParts: []string{"codex", "work", "config.json"},
			op:       fsnotify.Remove,
			wantNil:  false,
			wantType: EventProfileModified,
		},
		{
			name:     "deep path rename",
			fullPath: "/profiles/codex/work/config.json",
			relParts: []string{"codex", "work", "config.json"},
			op:       fsnotify.Rename,
			wantNil:  false,
			wantType: EventProfileModified,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := eventTypeForPath(tc.fullPath, tc.relParts, tc.op)
			if tc.wantNil {
				if got != nil {
					t.Errorf("eventTypeForPath() = %v, want nil", *got)
				}
			} else {
				if got == nil {
					t.Fatal("eventTypeForPath() = nil, want non-nil")
				}
				if *got != tc.wantType {
					t.Errorf("eventTypeForPath() = %v, want %v", *got, tc.wantType)
				}
			}
		})
	}
}

// Test eventTypeForPath with profile directory at 2-level depth
func TestEventTypeForPath_ProfileDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	profileDir := filepath.Join(tmpDir, "codex", "work")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create directory - should be profile added
	got := eventTypeForPath(profileDir, []string{"codex", "work"}, fsnotify.Create)
	if got == nil {
		t.Fatal("eventTypeForPath() for dir create = nil")
	}
	if *got != EventProfileAdded {
		t.Errorf("eventTypeForPath() = %v, want %v", *got, EventProfileAdded)
	}

	// Chmod on directory - should be modified
	got = eventTypeForPath(profileDir, []string{"codex", "work"}, fsnotify.Chmod)
	if got == nil {
		t.Fatal("eventTypeForPath() for dir chmod = nil")
	}
	if *got != EventProfileModified {
		t.Errorf("eventTypeForPath() = %v, want %v", *got, EventProfileModified)
	}

	// Write to directory - should be modified
	got = eventTypeForPath(profileDir, []string{"codex", "work"}, fsnotify.Write)
	if got == nil {
		t.Fatal("eventTypeForPath() for dir write = nil")
	}
	if *got != EventProfileModified {
		t.Errorf("eventTypeForPath() = %v, want %v", *got, EventProfileModified)
	}
}

// Test eventTypeForPath ignores provider-level file creates
func TestEventTypeForPath_ProviderLevelFile(t *testing.T) {
	tmpDir := t.TempDir()
	providerDir := filepath.Join(tmpDir, "codex")
	if err := os.MkdirAll(providerDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a file at provider level (not a directory)
	testFile := filepath.Join(providerDir, "somefile.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// File create at provider level should be ignored
	got := eventTypeForPath(testFile, []string{"codex", "somefile.txt"}, fsnotify.Create)
	if got != nil {
		t.Errorf("eventTypeForPath() for provider-level file = %v, want nil", *got)
	}
}

// Test isDirNoSymlink
func TestIsDirNoSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Regular directory
	dir := filepath.Join(tmpDir, "testdir")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !isDirNoSymlink(dir) {
		t.Error("isDirNoSymlink() for directory = false, want true")
	}

	// Regular file
	file := filepath.Join(tmpDir, "testfile")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if isDirNoSymlink(file) {
		t.Error("isDirNoSymlink() for file = true, want false")
	}

	// Non-existent path
	if isDirNoSymlink(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("isDirNoSymlink() for non-existent = true, want false")
	}

	// Symlink to directory (should return false)
	symlinkTarget := filepath.Join(tmpDir, "symlink_target")
	if err := os.MkdirAll(symlinkTarget, 0700); err != nil {
		t.Fatalf("mkdir symlink target: %v", err)
	}
	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(symlinkTarget, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if isDirNoSymlink(symlinkPath) {
		t.Error("isDirNoSymlink() for symlink to dir = true, want false")
	}
}

// Test shouldIgnoreParts
func TestShouldIgnoreParts(t *testing.T) {
	tests := []struct {
		parts []string
		want  bool
	}{
		{[]string{}, true},
		{[]string{"codex", ".DS_Store"}, true},
		{[]string{"codex", "work", ".DS_Store"}, true},
		{[]string{"codex", "Thumbs.db"}, true},
		{[]string{"codex", "desktop.ini"}, true},
		{[]string{".git", "config"}, true},
		{[]string{"codex", ".git", "objects"}, true},
		{[]string{"codex", "work", "profile.json"}, false},
		{[]string{"codex", "work"}, false},
	}

	for _, tc := range tests {
		got := shouldIgnoreParts(tc.parts)
		if got != tc.want {
			t.Errorf("shouldIgnoreParts(%v) = %v, want %v", tc.parts, got, tc.want)
		}
	}
}

// Test maybeEmitProviderBootstrapEvents with nil watcher
func TestWatcher_MaybeEmitProviderBootstrapEvents_Nil(t *testing.T) {
	var w *Watcher
	// Should not panic
	w.maybeEmitProviderBootstrapEvents("/some/path")
}

// Test maybeEmitProviderBootstrapEvents with empty dir
func TestWatcher_MaybeEmitProviderBootstrapEvents_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	w := &Watcher{
		profilesDir: tmpDir,
		events:      make(chan Event, 10),
	}
	// Should not panic with empty string
	w.maybeEmitProviderBootstrapEvents("")
}

// Test maybeEmitProviderBootstrapEvents with path outside profiles
func TestWatcher_MaybeEmitProviderBootstrapEvents_OutsidePath(t *testing.T) {
	tmpDir := t.TempDir()
	w := &Watcher{
		profilesDir: filepath.Join(tmpDir, "profiles"),
		events:      make(chan Event, 10),
	}
	// Path is outside, should not emit
	w.maybeEmitProviderBootstrapEvents(filepath.Join(tmpDir, "other"))

	select {
	case e := <-w.events:
		t.Errorf("unexpected event: %+v", e)
	default:
		// Expected: no events
	}
}

// Test maybeEmitProviderBootstrapEvents for provider directory with profiles
func TestWatcher_MaybeEmitProviderBootstrapEvents_WithProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	providerDir := filepath.Join(tmpDir, "codex")
	profileDir := filepath.Join(providerDir, "work")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := &Watcher{
		profilesDir: tmpDir,
		events:      make(chan Event, 10),
	}

	w.maybeEmitProviderBootstrapEvents(providerDir)

	select {
	case e := <-w.events:
		if e.Type != EventProfileAdded || e.Provider != "codex" || e.Profile != "work" {
			t.Errorf("unexpected event: %+v", e)
		}
	default:
		t.Error("expected profile added event")
	}
}

// Test maybeEmitProviderBootstrapEvents ignores symlinks
func TestWatcher_MaybeEmitProviderBootstrapEvents_IgnoresSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	providerDir := filepath.Join(tmpDir, "codex")
	if err := os.MkdirAll(providerDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a regular profile
	realProfile := filepath.Join(providerDir, "real")
	if err := os.MkdirAll(realProfile, 0700); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}

	// Create a symlink profile (should be ignored)
	symlinkProfile := filepath.Join(providerDir, "linked")
	if err := os.Symlink(realProfile, symlinkProfile); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	w := &Watcher{
		profilesDir: tmpDir,
		events:      make(chan Event, 10),
	}

	w.maybeEmitProviderBootstrapEvents(providerDir)

	// Should only get one event for "real", not "linked"
	count := 0
	for {
		select {
		case e := <-w.events:
			count++
			if e.Profile == "linked" {
				t.Error("should not emit event for symlink profile")
			}
		default:
			goto done
		}
	}
done:
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

// Test maybeEmitProviderBootstrapEvents for nested path (should not emit)
func TestWatcher_MaybeEmitProviderBootstrapEvents_NestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	profileDir := filepath.Join(tmpDir, "codex", "work")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := &Watcher{
		profilesDir: tmpDir,
		events:      make(chan Event, 10),
	}

	// Profile directory (2-level deep) should not trigger bootstrap
	w.maybeEmitProviderBootstrapEvents(profileDir)

	select {
	case e := <-w.events:
		t.Errorf("unexpected event for nested path: %+v", e)
	default:
		// Expected: no events
	}
}

// Test addDir with already-watched directory
func TestWatcher_AddDir_AlreadyWatched(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := NewWithDebounceDelay(tmpDir, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWithDebounceDelay() error = %v", err)
	}
	defer w.Close()

	// tmpDir should already be watched
	err = w.addDir(tmpDir)
	if err != nil {
		t.Errorf("addDir() for already-watched dir error = %v", err)
	}
}

// Test addRecursive skips symlinks
func TestWatcher_AddRecursive_SkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real directory
	realDir := filepath.Join(tmpDir, "real")
	if err := os.MkdirAll(realDir, 0700); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}

	// Create a symlink to it
	symlinkDir := filepath.Join(tmpDir, "linked")
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	w, err := NewWithDebounceDelay(tmpDir, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWithDebounceDelay() error = %v", err)
	}
	defer w.Close()

	// Verify symlink wasn't added to watched dirs
	w.mu.Lock()
	_, symlinkWatched := w.watched[symlinkDir]
	w.mu.Unlock()

	if symlinkWatched {
		t.Error("symlink should not be in watched map")
	}
}

// Test addRecursive skips .git directories
func TestWatcher_AddRecursive_SkipsGit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a .git directory with contents
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	w, err := NewWithDebounceDelay(tmpDir, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWithDebounceDelay() error = %v", err)
	}
	defer w.Close()

	// Verify .git wasn't added to watched dirs
	w.mu.Lock()
	_, gitWatched := w.watched[gitDir]
	w.mu.Unlock()

	if gitWatched {
		t.Error(".git should not be in watched map")
	}
}
