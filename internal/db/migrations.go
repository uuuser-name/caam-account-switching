package db

import (
	"database/sql"
	"fmt"
)

type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

var migrations = []Migration{
	{
		Version: 1,
		Name:    "initial_schema",
		Up: `
CREATE TABLE IF NOT EXISTS activity_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type TEXT NOT NULL,
    provider TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    details TEXT,
    duration_seconds INTEGER
);

CREATE TABLE IF NOT EXISTS profile_stats (
    provider TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    total_activations INTEGER DEFAULT 0,
    total_errors INTEGER DEFAULT 0,
    total_active_seconds INTEGER DEFAULT 0,
    last_activated DATETIME,
    last_error DATETIME,
    PRIMARY KEY (provider, profile_name)
);

CREATE INDEX IF NOT EXISTS idx_activity_timestamp ON activity_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_activity_provider ON activity_log(provider, profile_name);
`,
	},
	{
		Version: 2,
		Name:    "limit_events",
		Up: `
CREATE TABLE IF NOT EXISTS limit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    hit_at DATETIME NOT NULL,
    cooldown_until DATETIME NOT NULL,
    notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_limit_events_provider_profile ON limit_events(provider, profile_name);
CREATE INDEX IF NOT EXISTS idx_limit_events_cooldown_until ON limit_events(cooldown_until);
`,
	},
	{
		Version: 3,
		Name:    "cost_tracking",
		Up: `
-- Table for tracking wrap sessions with cost estimates
CREATE TABLE IF NOT EXISTS wrap_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    duration_seconds INTEGER DEFAULT 0,
    exit_code INTEGER,
    rate_limit_hit INTEGER DEFAULT 0,
    estimated_cost_cents INTEGER DEFAULT 0,
    notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_wrap_sessions_provider_profile ON wrap_sessions(provider, profile_name);
CREATE INDEX IF NOT EXISTS idx_wrap_sessions_started_at ON wrap_sessions(started_at);

-- Table for user-configurable cost rates per provider
CREATE TABLE IF NOT EXISTS cost_rates (
    provider TEXT PRIMARY KEY,
    cents_per_minute INTEGER DEFAULT 0,
    cents_per_session INTEGER DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Insert default rates (can be overridden by user)
INSERT OR IGNORE INTO cost_rates (provider, cents_per_minute, cents_per_session, updated_at)
VALUES
    ('claude', 5, 0, CURRENT_TIMESTAMP),
    ('codex', 3, 0, CURRENT_TIMESTAMP),
    ('gemini', 2, 0, CURRENT_TIMESTAMP);
`,
	},
}

func RunMigrations(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := ensureSchemaVersionTable(tx); err != nil {
		return err
	}

	current, err := currentSchemaVersion(tx)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if m.Up == "" {
			return fmt.Errorf("migration %d (%s) has empty Up", m.Version, m.Name)
		}
		if _, err := tx.Exec(m.Up); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, m.Version); err != nil {
			return fmt.Errorf("record migration %d (%s): %w", m.Version, m.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func ensureSchemaVersionTable(exec sqlExecutor) error {
	if exec == nil {
		return fmt.Errorf("exec is nil")
	}

	_, err := exec.Exec(`
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	return nil
}

func currentSchemaVersion(query sqlQueryer) (int, error) {
	if query == nil {
		return 0, fmt.Errorf("query is nil")
	}

	var v int
	if err := query.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema_version: %w", err)
	}
	return v, nil
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type sqlQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}
