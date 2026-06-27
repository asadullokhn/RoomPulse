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

const schema = `
CREATE TABLE IF NOT EXISTS devices (
	device_id    TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	workspace_id TEXT NOT NULL DEFAULT '',  -- '' = not in any room
	last_seen    INTEGER NOT NULL           -- unix seconds, server clock
);
CREATE TABLE IF NOT EXISTS events (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	ts           INTEGER NOT NULL,          -- unix seconds, server clock
	workspace_id TEXT NOT NULL,
	actor        TEXT NOT NULL,             -- device id or user id
	name         TEXT NOT NULL DEFAULT '',  -- display name
	kind         TEXT NOT NULL              -- 'enter' | 'leave'
);
CREATE INDEX IF NOT EXISTS idx_events_ws_ts ON events(workspace_id, ts DESC);`

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
	if _, err := sqlDB.Exec(schema); err != nil {
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

// LogEvent records a presence transition (someone entered or left a room). The
// room modal's "recent activity" reads these back. Best-effort — a failed write
// must never break a heartbeat.
func (d *DB) LogEvent(workspaceID, actor, name, kind string, at time.Time) error {
	if workspaceID == "" || actor == "" {
		return nil
	}
	_, err := d.sql.Exec(
		`INSERT INTO events (ts, workspace_id, actor, name, kind) VALUES (?, ?, ?, ?, ?)`,
		at.Unix(), workspaceID, actor, name, kind)
	return err
}

// PruneEvents deletes activity rows older than `before`, bounding table growth.
// Cheap (indexed) and a no-op most ticks since rows are recent.
func (d *DB) PruneEvents(before time.Time) error {
	_, err := d.sql.Exec(`DELETE FROM events WHERE ts < ?`, before.Unix())
	return err
}

// EventView is one row of a room's activity history.
type EventView struct {
	Kind    string `json:"kind"` // "enter" | "leave"
	Name    string `json:"name"`
	Actor   string `json:"actor"`
	AgoSec  int64  `json:"ago_sec"`
}

// Events returns a room's most recent activity, newest first.
func (d *DB) Events(workspaceID string, limit int, now time.Time) ([]EventView, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := d.sql.Query(
		`SELECT kind, name, actor, ts FROM events WHERE workspace_id = ? ORDER BY ts DESC, id DESC LIMIT ?`,
		workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventView{}
	for rows.Next() {
		var e EventView
		var ts int64
		if err := rows.Scan(&e.Kind, &e.Name, &e.Actor, &ts); err != nil {
			return nil, err
		}
		if e.AgoSec = now.Unix() - ts; e.AgoSec < 0 {
			e.AgoSec = 0
		}
		out = append(out, e)
	}
	return out, rows.Err()
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
