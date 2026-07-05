package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quickroom/internal/domain"

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
CREATE INDEX IF NOT EXISTS idx_events_ws_ts ON events(workspace_id, ts DESC);
CREATE TABLE IF NOT EXISTS users (
	user_id    TEXT PRIMARY KEY,
	apple_sub  TEXT NOT NULL UNIQUE,
	email      TEXT NOT NULL DEFAULT '',
	name       TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,   -- SHA-256 hex of the opaque session token; raw token never stored
	user_id    TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS app_reservations (
	reservation_id    TEXT PRIMARY KEY,
	room_id           TEXT NOT NULL,
	zoom_workspace_id TEXT NOT NULL,
	booked_by_user_id TEXT NOT NULL,
	user_email        TEXT NOT NULL DEFAULT '',
	start_time        INTEGER NOT NULL,
	end_time          INTEGER NOT NULL,
	status            TEXT NOT NULL,
	check_in_status   TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS apns_tokens (
	token      TEXT PRIMARY KEY,  -- APNs device token (hex); PK so a device re-homes on account switch
	user_id    TEXT NOT NULL,
	updated_at INTEGER NOT NULL   -- unix seconds, server clock
);
CREATE TABLE IF NOT EXISTS admins (
	admin_id      TEXT PRIMARY KEY,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,   -- bcrypt
	created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS custom_rooms (
	workspace_id TEXT PRIMARY KEY,   -- "cr-" + 8 hex; admin-created, not Zoom-synced
	name         TEXT NOT NULL,
	capacity     INTEGER NOT NULL DEFAULT 0,
	has_tv       INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS room_overrides (
	workspace_id TEXT PRIMARY KEY,    -- Zoom room being overridden
	name         TEXT NOT NULL DEFAULT '',    -- '' = keep Zoom value
	capacity     INTEGER NOT NULL DEFAULT -1, -- -1 = keep
	has_tv       INTEGER NOT NULL DEFAULT -1  -- -1 = keep, else 0/1
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

// UpsertUser inserts or updates a user by user_id. apple_sub is unique and
// set once at creation; a re-upsert of the same user only refreshes
// email/name (Apple resends email on every sign-in, but name only once).
func (d *DB) UpsertUser(u domain.User) error {
	_, err := d.sql.Exec(`
		INSERT INTO users (user_id, apple_sub, email, name, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			email = excluded.email,
			name  = CASE WHEN excluded.name <> '' THEN excluded.name ELSE users.name END`,
		u.UserID, u.AppleSub, u.Email, u.Name, u.CreatedAt.Unix())
	return err
}

func scanUser(row interface{ Scan(...any) error }) (domain.User, error) {
	var u domain.User
	var createdAt int64
	if err := row.Scan(&u.UserID, &u.AppleSub, &u.Email, &u.Name, &createdAt); err != nil {
		return domain.User{}, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	return u, nil
}

// UserByAppleSub looks up a user by Apple's stable per-app identifier.
func (d *DB) UserByAppleSub(appleSub string) (domain.User, bool, error) {
	row := d.sql.QueryRow(`SELECT user_id, apple_sub, email, name, created_at FROM users WHERE apple_sub = ?`, appleSub)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

// UserByID looks up a user by their local user_id.
func (d *DB) UserByID(userID string) (domain.User, bool, error) {
	row := d.sql.QueryRow(`SELECT user_id, apple_sub, email, name, created_at FROM users WHERE user_id = ?`, userID)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

// Users returns every app account, most-recently-created first.
func (d *DB) Users() ([]domain.User, error) {
	rows, err := d.sql.Query(`SELECT user_id, apple_sub, email, name, created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser removes a user account. Callers are responsible for handling
// their bookings/sessions first (see Server.deleteUser).
func (d *DB) DeleteUser(userID string) error {
	_, err := d.sql.Exec(`DELETE FROM users WHERE user_id = ?`, userID)
	return err
}

// CreateSession stores a new session keyed by the SHA-256 hash of its opaque
// token — the raw token is never persisted, only ever returned once to the
// caller at sign-in time.
func (d *DB) CreateSession(tokenHash, userID string, createdAt, expiresAt time.Time) error {
	_, err := d.sql.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, createdAt.Unix(), expiresAt.Unix())
	return err
}

// SessionUserID resolves a session token hash to its owning user_id, if the
// session exists and hasn't expired as of now.
func (d *DB) SessionUserID(tokenHash string, now time.Time) (string, bool, error) {
	var userID string
	var expiresAt int64
	err := d.sql.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if now.Unix() >= expiresAt {
		return "", false, nil
	}
	return userID, true, nil
}

// DeleteSession revokes a session (logout). A no-op (not an error) if the
// token hash isn't found.
func (d *DB) DeleteSession(tokenHash string) error {
	_, err := d.sql.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteSessionsForUser revokes every session belonging to a user (e.g. on
// account deletion) — unlike DeleteSession, which revokes one session by
// token hash for a single-device logout.
func (d *DB) DeleteSessionsForUser(userID string) error {
	_, err := d.sql.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// SaveAppReservation upserts a QuickRoom-native (app-sourced) reservation.
// App bookings are the only record of themselves — there's no Zoom sync to
// recover them from — so every state change (create, check-in/out, cancel)
// must round-trip through here to survive a restart.
func (d *DB) SaveAppReservation(r domain.Reservation) error {
	_, err := d.sql.Exec(`
		INSERT INTO app_reservations
			(reservation_id, room_id, zoom_workspace_id, booked_by_user_id, user_email, start_time, end_time, status, check_in_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(reservation_id) DO UPDATE SET
			status          = excluded.status,
			check_in_status = excluded.check_in_status`,
		r.ReservationID, r.RoomID, r.ZoomWorkspaceID, r.BookedByUserID, r.UserEmail,
		r.StartTime.Unix(), r.EndTime.Unix(), string(r.Status), string(r.CheckInStatus))
	return err
}

// AppReservations returns every persisted app-sourced reservation, for
// reloading into the in-memory store at startup.
func (d *DB) AppReservations() ([]domain.Reservation, error) {
	rows, err := d.sql.Query(`
		SELECT reservation_id, room_id, zoom_workspace_id, booked_by_user_id, user_email, start_time, end_time, status, check_in_status
		FROM app_reservations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Reservation{}
	for rows.Next() {
		var r domain.Reservation
		var start, end int64
		var status, checkIn string
		if err := rows.Scan(&r.ReservationID, &r.RoomID, &r.ZoomWorkspaceID, &r.BookedByUserID, &r.UserEmail, &start, &end, &status, &checkIn); err != nil {
			return nil, err
		}
		r.StartTime = time.Unix(start, 0).UTC()
		r.EndTime = time.Unix(end, 0).UTC()
		r.Status = domain.ReservationStatus(status)
		r.CheckInStatus = domain.CheckInStatus(checkIn)
		r.Source = "app"
		r.UserID = r.BookedByUserID
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveAPNSToken registers (or re-homes) a device's APNs token to a user.
func (d *DB) SaveAPNSToken(token, userID string, at time.Time) error {
	_, err := d.sql.Exec(`INSERT INTO apns_tokens (token, user_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET user_id = excluded.user_id, updated_at = excluded.updated_at`,
		token, userID, at.Unix())
	return err
}

// APNSTokensForUser returns the user's registered device tokens.
func (d *DB) APNSTokensForUser(userID string) ([]string, error) {
	return d.tokenQuery(`SELECT token FROM apns_tokens WHERE user_id = ?`, userID)
}

// AllAPNSTokens returns every registered token (broadcast notifications).
func (d *DB) AllAPNSTokens() ([]string, error) {
	return d.tokenQuery(`SELECT token FROM apns_tokens`)
}

func (d *DB) tokenQuery(query string, args ...any) ([]string, error) {
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPNSToken removes a dead token (APNs 410 Unregistered).
func (d *DB) DeleteAPNSToken(token string) error {
	_, err := d.sql.Exec(`DELETE FROM apns_tokens WHERE token = ?`, token)
	return err
}

// UserByEmail finds a user by email. Notification recipients are the booker's
// email when known, else their user id — this covers the email arm.
func (d *DB) UserByEmail(email string) (domain.User, bool, error) {
	row := d.sql.QueryRow(`SELECT user_id, apple_sub, email, name, created_at FROM users WHERE email = ?`, email)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

// Admin is one admin-panel account (email + bcrypt password login).
type Admin struct {
	AdminID      string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// EnsureAdmin seeds the very first admin account. No-op when any admin
// already exists, so env-var creds only ever bootstrap an empty table.
func (d *DB) EnsureAdmin(email, passwordHash string, at time.Time) error {
	var n int
	if err := d.sql.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return fmt.Errorf("generate admin id: %w", err)
	}
	_, err := d.sql.Exec(`INSERT INTO admins (admin_id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		"adm_"+hex.EncodeToString(id), email, passwordHash, at.Unix())
	return err
}

// AdminByEmail looks an admin up for login.
func (d *DB) AdminByEmail(email string) (Admin, bool, error) {
	return d.adminQuery(`SELECT admin_id, email, password_hash, created_at FROM admins WHERE email = ?`, email)
}

// AdminByID confirms a JWT's admin principal still exists.
func (d *DB) AdminByID(id string) (Admin, bool, error) {
	return d.adminQuery(`SELECT admin_id, email, password_hash, created_at FROM admins WHERE admin_id = ?`, id)
}

func (d *DB) adminQuery(query string, arg any) (Admin, bool, error) {
	var a Admin
	var createdAt int64
	err := d.sql.QueryRow(query, arg).Scan(&a.AdminID, &a.Email, &a.PasswordHash, &createdAt)
	if err == sql.ErrNoRows {
		return Admin{}, false, nil
	}
	if err != nil {
		return Admin{}, false, err
	}
	a.CreatedAt = time.Unix(createdAt, 0)
	return a, true, nil
}

// SaveCustomRoom upserts an admin-created room.
func (d *DB) SaveCustomRoom(r domain.Room, at time.Time) error {
	hasTV := 0
	if r.HasTV {
		hasTV = 1
	}
	_, err := d.sql.Exec(`INSERT INTO custom_rooms (workspace_id, name, capacity, has_tv, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET name = excluded.name, capacity = excluded.capacity, has_tv = excluded.has_tv`,
		r.ZoomWorkspaceID, r.Name, r.Capacity, hasTV, at.Unix())
	return err
}

// CustomRooms returns every admin-created room as a domain.Room.
func (d *DB) CustomRooms() ([]domain.Room, error) {
	rows, err := d.sql.Query(`SELECT workspace_id, name, capacity, has_tv FROM custom_rooms ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Room
	for rows.Next() {
		var ws, name string
		var capacity, hasTV int
		if err := rows.Scan(&ws, &name, &capacity, &hasTV); err != nil {
			return nil, err
		}
		out = append(out, domain.Room{
			RoomID:          "room-" + ws,
			ZoomWorkspaceID: ws,
			Name:            name,
			Capacity:        capacity,
			HasTV:           hasTV == 1,
			IsZoomRoom:      false,
		})
	}
	return out, rows.Err()
}

// DeleteCustomRoom removes an admin-created room.
func (d *DB) DeleteCustomRoom(workspaceID string) error {
	_, err := d.sql.Exec(`DELETE FROM custom_rooms WHERE workspace_id = ?`, workspaceID)
	return err
}

// RoomOverride patches a Zoom-synced room's fields. Sentinels mean "keep the
// Zoom value": Name "" / Capacity -1 / HasTV -1 (else 0/1).
type RoomOverride struct {
	WorkspaceID string
	Name        string
	Capacity    int
	HasTV       int
}

// SaveRoomOverride upserts an override, merging: an incoming sentinel field
// preserves whatever the existing override already pinned.
func (d *DB) SaveRoomOverride(o RoomOverride) error {
	_, err := d.sql.Exec(`INSERT INTO room_overrides (workspace_id, name, capacity, has_tv) VALUES (?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET
			name     = CASE WHEN excluded.name <> '' THEN excluded.name ELSE room_overrides.name END,
			capacity = CASE WHEN excluded.capacity >= 0 THEN excluded.capacity ELSE room_overrides.capacity END,
			has_tv   = CASE WHEN excluded.has_tv >= 0 THEN excluded.has_tv ELSE room_overrides.has_tv END`,
		o.WorkspaceID, o.Name, o.Capacity, o.HasTV)
	return err
}

// RoomOverrides returns every Zoom-room override.
func (d *DB) RoomOverrides() ([]RoomOverride, error) {
	rows, err := d.sql.Query(`SELECT workspace_id, name, capacity, has_tv FROM room_overrides`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoomOverride
	for rows.Next() {
		var o RoomOverride
		if err := rows.Scan(&o.WorkspaceID, &o.Name, &o.Capacity, &o.HasTV); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ClearRoomOverride resets a Zoom room to Zoom truth.
func (d *DB) ClearRoomOverride(workspaceID string) error {
	_, err := d.sql.Exec(`DELETE FROM room_overrides WHERE workspace_id = ?`, workspaceID)
	return err
}

// UpdateUserName renames an account (admin panel edit).
func (d *DB) UpdateUserName(userID, name string) error {
	_, err := d.sql.Exec(`UPDATE users SET name = ? WHERE user_id = ?`, name, userID)
	return err
}
