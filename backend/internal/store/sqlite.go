package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver; works with CGO_ENABLED=0 / distroless
)

// DB is the SQLite-backed durable store. The in-memory Memory store stays the
// live presence engine (occupancy, TTL reaping, reservation check-in); this
// persists the known-device registry so the devices view survives restarts and
// can be queried with SQL.
type DB struct {
	sql *sql.DB
}

const deviceSchema = `
CREATE TABLE IF NOT EXISTS devices (
	device_id    TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	workspace_id TEXT NOT NULL DEFAULT '',  -- '' = not in any room
	last_seen    INTEGER NOT NULL           -- unix seconds, server clock
);`

// OpenDB opens (creating if absent) the SQLite database at path, creating any
// missing parent directory, and applies the schema. WAL + a busy timeout keep
// the polling dashboard reads from colliding with heartbeat writes.
func OpenDB(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir %q: %w", dir, err)
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1) // serialise access; ample for the prototype's load
	if _, err := sqlDB.Exec(deviceSchema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{sql: sqlDB}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// UpsertDevice records a device's latest room and marks it seen at seenAt
// (server clock, matching the in-memory TTL philosophy). A blank display name
// never overwrites a known one.
func (d *DB) UpsertDevice(deviceID, displayName, workspaceID string, seenAt time.Time) error {
	_, err := d.sql.Exec(`
		INSERT INTO devices (device_id, display_name, workspace_id, last_seen)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE devices.display_name END,
			workspace_id = excluded.workspace_id,
			last_seen    = excluded.last_seen`,
		deviceID, displayName, workspaceID, seenAt.Unix())
	return err
}

// DeviceView is one row of the device registry for the web table.
type DeviceView struct {
	DeviceID    string `json:"device_id"`
	DisplayName string `json:"display_name"`
	WorkspaceID string `json:"workspace_id"` // "" = not in any room
	LastSeenSec int64  `json:"last_seen_sec"`
}

// Devices returns the known-device registry, most-recently-seen first.
func (d *DB) Devices(now time.Time) ([]DeviceView, error) {
	rows, err := d.sql.Query(`SELECT device_id, display_name, workspace_id, last_seen FROM devices ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceView{}
	for rows.Next() {
		var v DeviceView
		var lastSeen int64
		if err := rows.Scan(&v.DeviceID, &v.DisplayName, &v.WorkspaceID, &lastSeen); err != nil {
			return nil, err
		}
		if v.LastSeenSec = now.Unix() - lastSeen; v.LastSeenSec < 0 {
			v.LastSeenSec = 0
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
