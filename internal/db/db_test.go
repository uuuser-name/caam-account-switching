package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAt_CreatesDBAndRunsMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "caam.db")

	d, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file stat error = %v", err)
	}

	// Migration-created tables should exist.
	for _, table := range []string{"schema_version", "activity_log", "profile_stats", "limit_events"} {
		var name string
		if err := d.Conn().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}

	// Migrations should be idempotent.
	if err := RunMigrations(d.Conn()); err != nil {
		t.Fatalf("RunMigrations() second run error = %v", err)
	}

	var version int
	if err := d.Conn().QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema_version error = %v", err)
	}
	if version != 3 {
		t.Fatalf("schema_version max = %d, want 3", version)
	}
}

func TestOpenAt_EnablesWALMode(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "caam.db")

	d, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	var mode string
	if err := d.Conn().QueryRow(`PRAGMA journal_mode;`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode error = %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestOpenAt_CorruptDB_RenamedAndRecreated(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "caam.db")

	// Create an invalid "database" file.
	if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
		t.Fatalf("write corrupt db: %v", err)
	}

	d, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// Original should be replaced by a valid sqlite database file.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file missing after recreate: %v", err)
	}

	backups, err := filepath.Glob(path + ".corrupt.*")
	if err != nil {
		t.Fatalf("glob corrupt backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("corrupt backup count = %d, want 1", len(backups))
	}
}

func TestCheckpoint_TruncatesWAL(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "caam.db")

	d, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt() error = %v", err)
	}
	defer d.Close()

	if _, err := d.Conn().Exec(`PRAGMA wal_autocheckpoint=0;`); err != nil {
		t.Fatalf("set wal_autocheckpoint=0: %v", err)
	}

	if _, err := d.Conn().Exec(`CREATE TABLE IF NOT EXISTS t (id INTEGER);`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := d.Conn().Exec(`INSERT INTO t (id) VALUES (1);`); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	walPath := path + "-wal"
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("wal file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("wal file size = 0, want > 0")
	}

	if err := Checkpoint(path); err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}

	if info, err := os.Stat(walPath); err == nil {
		if info.Size() != 0 {
			t.Fatalf("wal file size after checkpoint = %d, want 0", info.Size())
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat wal after checkpoint: %v", err)
	}
}

func TestRenameSQLiteSidecars(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "caam.db")
	backup := path + ".corrupt.20260101T000000Z"

	if err := os.WriteFile(path+"-wal", []byte("wal"), 0600); err != nil {
		t.Fatalf("write wal: %v", err)
	}
	if err := os.WriteFile(path+"-shm", []byte("shm"), 0600); err != nil {
		t.Fatalf("write shm: %v", err)
	}

	if err := renameSQLiteSidecars(path, backup); err != nil {
		t.Fatalf("renameSQLiteSidecars() error = %v", err)
	}

	if data, err := os.ReadFile(backup + "-wal"); err != nil {
		t.Fatalf("read wal backup: %v", err)
	} else if string(data) != "wal" {
		t.Fatalf("wal backup content = %q, want %q", string(data), "wal")
	}

	if data, err := os.ReadFile(backup + "-shm"); err != nil {
		t.Fatalf("read shm backup: %v", err)
	} else if string(data) != "shm" {
		t.Fatalf("shm backup content = %q, want %q", string(data), "shm")
	}
}
