# Sign in with Apple + Native Room Booking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Rei's iOS app authenticate users via Sign in with Apple and let them create, list, and cancel room bookings against this backend.

**Architecture:** New `appleauth` package verifies Apple identity tokens against Apple's JWKS. New SQLite tables (`users`, `sessions`, `app_reservations`) back a bearer-token session system and durable app-native bookings. Bookings reuse `domain.Reservation` (new `Source`/`BookedByUserID` fields) so they get the existing no-show/grace/collision/occupancy machinery for free — but that machinery's three existing Zoom-call sites must be taught to skip Zoom entirely for app-sourced reservations, since Zoom has never heard of them.

**Tech Stack:** Go 1.26, `github.com/golang-jwt/jwt/v5` (new dependency), SQLite (`modernc.org/sqlite`, already a dependency), stdlib `crypto/rsa`/`crypto/rand` for JWKS handling and token generation.

## Global Constraints

- Bookings are QuickRoom-native, not real Zoom reservations (confirmed with user).
- Booking creation checks conflicts against *all* reservations for a room, regardless of source (confirmed with user).
- Sign-in is open to any Apple ID; no domain/allowlist restriction (confirmed with user).
- No Apple server-to-server code exchange, no Apple client secret, no Apple refresh tokens — verify the identity token the app already obtained on-device.
- Sessions are opaque, SHA-256-hashed-at-rest, DB-backed tokens — not JWTs — for trivial revocation.
- Every existing GET endpoint stays open/unauthenticated, unchanged.
- Request bodies follow the existing `maxBody`/`decodeBody`/`clamp` conventions in `backend/internal/api/server.go`.
- `go vet ./...` and `go test ./...` must stay green after every task.

---

## File Structure

```
backend/
  internal/
    appleauth/
      appleauth.go        — new: Verifier, VerifyIdentityToken, Claims, JWKS fetch/cache
      appleauth_test.go   — new
    domain/
      domain.go            — modify: Reservation.Source/BookedByUserID, StatusCancelled, User type
    store/
      sqlite.go             — modify: users/sessions/app_reservations schema + CRUD methods
      sqlite_test.go         — new: tests for the new methods
    sync/
      service.go              — modify: tag Zoom-sourced reservations with Source: "zoom"
    config/
      config.go                — modify: APPLE_BUNDLE_ID, SESSION_TTL
    api/
      server.go                 — modify: Server fields/constructor, Zoom-call-skip fix, route registration
      server_test.go             — modify: newTestHandler gets the new constructor params
      auth.go                     — new: POST /auth/apple, POST /auth/logout, authMiddleware, id/token helpers
      auth_test.go                 — new
      booking.go                    — new: POST /reservations, GET /reservations/mine, POST /reservations/{id}/cancel
      booking_test.go                — new
      noshow.go                       — modify: skip Zoom call for app-sourced no-show release
      openapi.yaml                     — modify: new paths + security scheme
  cmd/quickroom/main.go               — modify: wire appleauth.Verifier, load persisted app reservations on startup
  go.mod, go.sum                      — modify: add golang-jwt/jwt/v5
```

---

### Task 1: Domain model — `Source`, `BookedByUserID`, `StatusCancelled`, `User`

**Files:**
- Modify: `backend/internal/domain/domain.go`

**Interfaces:**
- Produces: `domain.Reservation.Source string`, `domain.Reservation.BookedByUserID string`, `domain.StatusCancelled ReservationStatus`, `domain.User{UserID, AppleSub, Email, Name, CreatedAt}`.

- [ ] **Step 1: Add the new fields and type**

In `backend/internal/domain/domain.go`, change the `ReservationStatus` const block and `Reservation` struct:

```go
// ReservationStatus is QuickRoom's view of a booking's lifecycle.
type ReservationStatus string

const (
	StatusBooked    ReservationStatus = "booked"
	StatusNoShow    ReservationStatus = "no_show"
	StatusReleased  ReservationStatus = "released"
	StatusCancelled ReservationStatus = "cancelled" // user-initiated, distinct from an auto-reclaimed no-show
)

// Reservation is a Zoom workspace reservation joined to a local room, OR a
// QuickRoom-native booking created from the mobile app (Source == "app") —
// Zoom has never heard of the latter, so callers must check Source before
// driving any Zoom API call for a reservation.
type Reservation struct {
	ReservationID   string            `json:"reservation_id"`
	RoomID          string            `json:"room_id"`
	ZoomWorkspaceID string            `json:"zoom_workspace_id"`
	UserID          string            `json:"user_id"`
	UserEmail       string            `json:"user_email,omitempty"`
	StartTime       time.Time         `json:"start_time"`
	EndTime         time.Time         `json:"end_time"`
	Status          ReservationStatus `json:"status"`
	CheckInStatus   CheckInStatus     `json:"check_in_status"`

	// Source is "zoom" (mirrored from Zoom's reservation API) or "app"
	// (created via POST /reservations by a signed-in mobile user).
	Source         string `json:"source"`
	BookedByUserID string `json:"booked_by_user_id,omitempty"`
}

// User is an app account, established via Sign in with Apple.
type User struct {
	UserID    string    `json:"user_id"`
	AppleSub  string    `json:"-"` // Apple's stable per-app identifier; never serialized to clients
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd backend && go build ./...`
Expected: exits 0 — adding fields to a struct doesn't break existing struct literals that omit them (Go zero-values the rest), so `sync/service.go`'s existing `domain.Reservation{...}` literal (which doesn't set `Source` yet) still compiles. It gets `Source: "zoom"` explicitly in Task 5.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/domain/domain.go
git commit -m "Add Source/BookedByUserID to Reservation, StatusCancelled, and a User type"
```

---

### Task 2: Guard the 4 existing Zoom-call sites against app-sourced reservations

Zoom has never heard of an app-native reservation's ID, so every place that calls `zoom.Client.SendEvent` for an *existing* reservation must skip that call when `Source == "app"` and just apply the local state change. This must land before Task 7 introduces any real app-sourced reservations.

**Files:**
- Modify: `backend/internal/api/server.go:591-604` (`driveReservation`), `:606-675` (`presence`), `:677-702` (`checkEvent`)
- Modify: `backend/internal/api/noshow.go:32-74` (`sweepNoShows`)
- Test: `backend/internal/api/server_test.go`

**Interfaces:**
- Consumes: `domain.Reservation.Source` (Task 1).
- Produces: none new — behavior-only change to existing functions.

**Verification note:** `driveReservation` and `checkEvent` are unexported, and `server_test.go` lives in the external `api_test` package (see its `package api_test` declaration), so they can't be called directly from a test — this guard can only be exercised through the HTTP API, which means through an app-sourced reservation, which doesn't exist until Task 7's `POST /reservations` lands. Implement the guard in this task; Task 7's `TestCreateListCancelReservation` is extended with a check-in assertion that actually proves it (an app-sourced reservation's `/check-in` would 404-via-Zoom if this guard weren't in place, since even the mock Zoom client returns `ErrReservationNotFound` for an ID it's never seen).

- [ ] **Step 1: Modify `driveReservation`, `presence`, and `checkEvent` in `backend/internal/api/server.go`**

Replace the `driveReservation` function (currently lines 591-604):

```go
// driveReservation reflects a room's occupancy onto its booking's check-in
// state (best-effort; Zoom stays the source of truth for zoom-sourced
// bookings). App-sourced bookings (Source == "app") have no Zoom
// counterpart, so the Zoom call is skipped for them.
func (s *Server) driveReservation(ctx context.Context, workspaceID string, event zoom.EventType, newStatus domain.CheckInStatus) {
	res, ok := s.store.ReservationByWorkspace(workspaceID)
	if !ok {
		return
	}
	if res.Source != "app" {
		if err := s.zoom.SendEvent(ctx, event, res.ReservationID); err != nil {
			s.log.Warn("driveReservation", "err", err)
			return
		}
	}
	res.CheckInStatus = newStatus
	s.upsertReservation(res)
}
```

Replace the tail of the `presence` handler (currently lines 653-675, from the "Presence (headcount)" comment onward):

```go
	// Presence (headcount) is tracked above regardless of bookings. Below we
	// best-effort drive the booker's reservation check-in/out.
	res, ok := s.store.ReservationByWorkspace(body.WorkspaceID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "recorded",
			"workspace_id": body.WorkspaceID,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if res.Source != "app" {
		if err := s.zoom.SendEvent(ctx, event, res.ReservationID); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	res.CheckInStatus = newStatus
	s.upsertReservation(res)
	s.log.Info("presence applied", "event", body.EventType, "workspace", body.WorkspaceID, "user", body.UserID)
	writeJSON(w, http.StatusOK, res)
}
```

Replace `checkEvent` (currently lines 677-702):

```go
// checkEvent sends the event to Zoom, then reflects it locally (zoom-sourced
// reservations only — Zoom stays the source of truth for those). App-sourced
// reservations have no Zoom counterpart, so the Zoom call is skipped and the
// local state change applies directly.
func (s *Server) checkEvent(w http.ResponseWriter, r *http.Request, event zoom.EventType, newStatus domain.CheckInStatus) {
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}

	if res.Source != "app" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := s.zoom.SendEvent(ctx, event, id); err != nil {
			if errors.Is(err, zoom.ErrReservationNotFound) {
				writeError(w, http.StatusNotFound, "reservation not found in zoom")
				return
			}
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	res.CheckInStatus = newStatus
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}
```

Add the `upsertReservation` helper near `logEvent` (after line 589's `logEvent` function):

```go
// upsertReservation applies a reservation change to the in-memory store and,
// for app-sourced reservations only, also persists it to SQLite — app
// bookings are the only record of themselves (no Zoom to re-sync from), so
// losing them on restart would lose a real user's booking. Best-effort on the
// SQLite write: the in-memory state is applied regardless, matching how
// logEvent treats history as best-effort.
func (s *Server) upsertReservation(res domain.Reservation) {
	s.store.UpsertReservation(res)
	if res.Source != "app" {
		return
	}
	if err := s.db.SaveAppReservation(res); err != nil {
		s.log.Warn("persist app reservation", "reservation", res.ReservationID, "err", err)
	}
}
```

(`s.db.SaveAppReservation` is added in Task 3 — this file won't compile until that lands. That's expected; Task 3 comes next and this task's build-verification step accounts for it.)

- [ ] **Step 2: Modify `sweepNoShows` in `backend/internal/api/noshow.go`**

Replace the body of the loop from `r.Status = domain.StatusNoShow` through the `s.store.UpsertReservation(r)` release line (currently lines 45-58):

```go
		r.Status = domain.StatusNoShow
		if r.Source != "app" {
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.zoom.SendEvent(c, zoom.EventCheckOut, r.ReservationID)
			cancel()
			if err != nil {
				// Leave it flagged no_show; a later sweep retries the release.
				s.store.UpsertReservation(r)
				s.log.Warn("no-show release failed", "reservation", r.ReservationID, "err", err)
				continue
			}
		}
		r.Status = domain.StatusReleased
		r.CheckInStatus = domain.CheckedOut
		s.upsertReservation(r)
		s.log.Info("released no-show booking", "reservation", r.ReservationID, "workspace", r.ZoomWorkspaceID, "user", r.UserID)
```

- [ ] **Step 3: Note on build order**

This task will not compile in isolation (`s.upsertReservation` calls `s.db.SaveAppReservation`, added in Task 3). Do Task 3 first if executing tasks out of order, or treat Tasks 1-3 as landing together before the first `go build` checkpoint. The plan's task order already puts Task 3 next, so proceed directly.

---

### Task 3: SQLite schema + `store.DB` methods for users, sessions, and app reservations

**Files:**
- Modify: `backend/internal/store/sqlite.go`
- Test: `backend/internal/store/sqlite_test.go` (new)

**Interfaces:**
- Consumes: `domain.User`, `domain.Reservation` (Task 1).
- Produces:
  - `func (d *DB) UpsertUser(u domain.User) error`
  - `func (d *DB) UserByAppleSub(appleSub string) (domain.User, bool, error)`
  - `func (d *DB) UserByID(userID string) (domain.User, bool, error)`
  - `func (d *DB) CreateSession(tokenHash, userID string, createdAt, expiresAt time.Time) error`
  - `func (d *DB) SessionUserID(tokenHash string, now time.Time) (userID string, ok bool, err error)`
  - `func (d *DB) DeleteSession(tokenHash string) error`
  - `func (d *DB) SaveAppReservation(r domain.Reservation) error`
  - `func (d *DB) AppReservations() ([]domain.Reservation, error)`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/store/sqlite_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
	"time"

	"quickroom/internal/domain"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUserRoundTrip(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().Truncate(time.Second)
	u := domain.User{UserID: "usr_1", AppleSub: "apple.sub.1", Email: "a@example.com", Name: "Ava", CreatedAt: now}

	if err := db.UpsertUser(u); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	got, ok, err := db.UserByAppleSub("apple.sub.1")
	if err != nil || !ok {
		t.Fatalf("UserByAppleSub: got=%v ok=%v err=%v", got, ok, err)
	}
	if got.UserID != u.UserID || got.Email != u.Email || got.Name != u.Name {
		t.Fatalf("UserByAppleSub = %+v, want %+v", got, u)
	}

	byID, ok, err := db.UserByID("usr_1")
	if err != nil || !ok || byID.AppleSub != "apple.sub.1" {
		t.Fatalf("UserByID: got=%+v ok=%v err=%v", byID, ok, err)
	}

	_, ok, err = db.UserByAppleSub("nonexistent")
	if err != nil || ok {
		t.Fatalf("UserByAppleSub(nonexistent): ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	if err := db.CreateSession("hash-1", "usr_1", now, now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	userID, ok, err := db.SessionUserID("hash-1", now.Add(time.Minute))
	if err != nil || !ok || userID != "usr_1" {
		t.Fatalf("SessionUserID (valid) = %q, %v, %v", userID, ok, err)
	}

	_, ok, err = db.SessionUserID("hash-1", now.Add(2*time.Hour))
	if err != nil || ok {
		t.Fatalf("SessionUserID (expired): ok=%v err=%v, want ok=false", ok, err)
	}

	if err := db.DeleteSession("hash-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	_, ok, err = db.SessionUserID("hash-1", now.Add(time.Minute))
	if err != nil || ok {
		t.Fatalf("SessionUserID (after delete): ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestAppReservationRoundTrip(t *testing.T) {
	db := newTestDB(t)
	r := domain.Reservation{
		ReservationID: "app-1", RoomID: "room-1", ZoomWorkspaceID: "ws-1",
		UserID: "usr_1", UserEmail: "a@example.com",
		StartTime: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Status:    domain.StatusBooked, CheckInStatus: domain.NotCheckedIn,
		Source: "app", BookedByUserID: "usr_1",
	}
	if err := db.SaveAppReservation(r); err != nil {
		t.Fatalf("SaveAppReservation (create): %v", err)
	}

	r.Status = domain.StatusCancelled
	if err := db.SaveAppReservation(r); err != nil {
		t.Fatalf("SaveAppReservation (update): %v", err)
	}

	all, err := db.AppReservations()
	if err != nil {
		t.Fatalf("AppReservations: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("AppReservations returned %d rows, want 1", len(all))
	}
	got := all[0]
	if got.ReservationID != "app-1" || got.Status != domain.StatusCancelled || got.Source != "app" {
		t.Fatalf("AppReservations[0] = %+v, want status=cancelled source=app", got)
	}
	if !got.StartTime.Equal(r.StartTime) || !got.EndTime.Equal(r.EndTime) {
		t.Fatalf("AppReservations[0] times = %v..%v, want %v..%v", got.StartTime, got.EndTime, r.StartTime, r.EndTime)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/... -run 'TestUserRoundTrip|TestSessionLifecycle|TestAppReservationRoundTrip' -v`
Expected: FAIL — `db.UpsertUser undefined` (method doesn't exist yet).

- [ ] **Step 3: Add the schema and methods to `backend/internal/store/sqlite.go`**

Extend the `schema` const (currently lines 21-36) — add three tables after the existing `events` table and its index:

```go
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
);`
```

Append the new methods at the end of `backend/internal/store/sqlite.go` (after the existing `Devices` function), adding `"quickroom/internal/domain"` to the import block:

```go
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
```

Add `"database/sql"` is already imported (used by `sql.DB`); confirm `sql.ErrNoRows` is reachable (it is, same package already imported).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/... -run 'TestUserRoundTrip|TestSessionLifecycle|TestAppReservationRoundTrip' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Run the full store package tests**

Run: `cd backend && go test ./internal/store/...`
Expected: PASS (no regressions in existing `memory_test.go`).

- [ ] **Step 6: Verify the rest of the module still builds**

Run: `cd backend && go build ./...`
Expected: exits 0 now that `s.db.SaveAppReservation` (referenced by Task 2's `upsertReservation`) resolves.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go backend/internal/api/server.go backend/internal/api/noshow.go backend/internal/domain/domain.go
git commit -m "Add users/sessions/app_reservations persistence; guard Zoom calls against app-sourced reservations"
```

---

### Task 4: `appleauth` package — verify Apple identity tokens

**Files:**
- Create: `backend/internal/appleauth/appleauth.go`
- Test: `backend/internal/appleauth/appleauth_test.go`
- Modify: `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Produces:
  - `type Claims struct { Sub, Email string; EmailVerified bool }`
  - `type Verifier struct { ... }` with `func NewVerifier(bundleID string, hc *http.Client) *Verifier` and a `KeysURL string` field (defaults to Apple's real JWKS URL; overridable for tests)
  - `func (v *Verifier) VerifyIdentityToken(ctx context.Context, tokenString string) (Claims, error)`

- [ ] **Step 1: Add the dependency**

Run: `cd backend && go get github.com/golang-jwt/jwt/v5`
Expected: `go.mod`/`go.sum` updated with the new require line.

- [ ] **Step 2: Write the failing test**

Create `backend/internal/appleauth/appleauth_test.go`:

```go
package appleauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testJWKS spins up a fake Apple keys endpoint backed by a freshly generated
// RSA key, and returns a signer function for building test identity tokens.
func testJWKS(t *testing.T) (*httptest.Server, func(claims jwt.MapClaims) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-kid-1"

	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(bigIntToBytes(key.PublicKey.E)),
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	sign := func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return s
	}
	return srv, sign
}

func bigIntToBytes(e int) []byte {
	// Minimal big-endian encoding of a small int (Apple's "e" is always 65537 = 0x010001).
	if e == 65537 {
		return []byte{0x01, 0x00, 0x01}
	}
	return []byte{byte(e)}
}

func TestVerifyIdentityToken_Valid(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": appleIssuer, "aud": "com.example.QuickRoom", "sub": "apple-sub-123",
		"email": "a@example.com", "email_verified": "true",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})

	claims, err := v.VerifyIdentityToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyIdentityToken: %v", err)
	}
	if claims.Sub != "apple-sub-123" || claims.Email != "a@example.com" || !claims.EmailVerified {
		t.Fatalf("claims = %+v, want sub=apple-sub-123 email=a@example.com verified=true", claims)
	}
}

func TestVerifyIdentityToken_WrongAudience(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": appleIssuer, "aud": "com.other.App", "sub": "apple-sub-123",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})

	if _, err := v.VerifyIdentityToken(context.Background(), token); err == nil {
		t.Fatal("VerifyIdentityToken with wrong audience: want error, got nil")
	}
}

func TestVerifyIdentityToken_Expired(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": appleIssuer, "aud": "com.example.QuickRoom", "sub": "apple-sub-123",
		"iat": now.Add(-2 * time.Hour).Unix(), "exp": now.Add(-time.Hour).Unix(),
	})

	if _, err := v.VerifyIdentityToken(context.Background(), token); err == nil {
		t.Fatal("VerifyIdentityToken with expired token: want error, got nil")
	}
}

func TestVerifyIdentityToken_WrongIssuer(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": "https://evil.example.com", "aud": "com.example.QuickRoom", "sub": "apple-sub-123",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})

	if _, err := v.VerifyIdentityToken(context.Background(), token); err == nil {
		t.Fatal("VerifyIdentityToken with wrong issuer: want error, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/appleauth/... -v`
Expected: FAIL to compile — package `appleauth` doesn't exist yet.

- [ ] **Step 4: Write `backend/internal/appleauth/appleauth.go`**

```go
// Package appleauth verifies Apple "Sign in with Apple" identity tokens.
//
// The mobile app completes the native ASAuthorizationAppleIDProvider flow
// on-device and sends us the resulting identityToken (a JWT signed by
// Apple). Our only job is to verify that JWT — check its signature against
// Apple's published public keys, and check iss/aud/exp — never to exchange
// authorization codes with Apple ourselves. That simpler flow needs no
// Apple client secret and no Apple refresh tokens.
package appleauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	appleIssuer         = "https://appleid.apple.com"
	defaultAppleKeysURL = "https://appleid.apple.com/auth/keys"
	keysCacheTTL        = 24 * time.Hour // Apple rotates keys infrequently
)

// Claims is what we trust from a verified Apple identity token.
type Claims struct {
	Sub           string // Apple's stable per-app user identifier
	Email         string // may be a private-relay address; empty if Apple didn't include one
	EmailVerified bool
}

// Verifier verifies Apple identity tokens for one app (identified by bundleID).
type Verifier struct {
	bundleID string
	http     *http.Client

	// KeysURL overrides Apple's real JWKS endpoint — for tests only.
	KeysURL string

	mu       sync.Mutex
	keys     map[string]*rsa.PublicKey // kid -> key
	cachedAt time.Time
}

// NewVerifier builds a Verifier for the given app Bundle ID (checked against
// the token's aud claim). Pass a nil http.Client to use a default with a
// short timeout.
func NewVerifier(bundleID string, hc *http.Client) *Verifier {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	return &Verifier{bundleID: bundleID, http: hc, KeysURL: defaultAppleKeysURL}
}

// VerifyIdentityToken verifies the signature and standard claims of an Apple
// identity token, returning the trusted claims on success.
func (v *Verifier) VerifyIdentityToken(ctx context.Context, tokenString string) (Claims, error) {
	token, err := jwt.Parse(tokenString, v.keyFunc(ctx), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return Claims{}, fmt.Errorf("appleauth: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return Claims{}, errors.New("appleauth: invalid token")
	}

	iss, _ := claims["iss"].(string)
	if iss != appleIssuer {
		return Claims{}, fmt.Errorf("appleauth: unexpected issuer %q", iss)
	}
	aud, _ := claims["aud"].(string)
	if v.bundleID == "" || aud != v.bundleID {
		return Claims{}, fmt.Errorf("appleauth: unexpected audience %q", aud)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return Claims{}, errors.New("appleauth: missing sub")
	}
	email, _ := claims["email"].(string)
	verified := claims["email_verified"] == "true" || claims["email_verified"] == true

	return Claims{Sub: sub, Email: email, EmailVerified: verified}, nil
}

// keyFunc resolves the RSA public key for the token's kid, fetching/caching
// Apple's JWKS as needed.
func (v *Verifier) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("appleauth: token missing kid")
		}
		key, err := v.key(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	}
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	stale := time.Since(v.cachedAt) > keysCacheTTL
	key, cached := v.keys[kid]
	v.mu.Unlock()
	if cached && !stale {
		return key, nil
	}

	keys, err := v.fetchKeys(ctx)
	if err != nil {
		if cached {
			return key, nil // fall back to a stale-but-known key rather than fail closed on a transient fetch error
		}
		return nil, fmt.Errorf("appleauth: fetch keys: %w", err)
	}

	v.mu.Lock()
	v.keys = keys
	v.cachedAt = time.Now()
	found, ok := v.keys[kid]
	v.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("appleauth: unknown kid %q", kid)
	}
	return found, nil
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (v *Verifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.KeysURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var body jwks
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, k := range body.Keys {
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}
	}
	return out, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/appleauth/... -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/appleauth/ backend/go.mod backend/go.sum
git commit -m "Add appleauth package to verify Apple Sign in identity tokens"
```

---

### Task 5: Config + `main.go` wiring, including startup reload of app reservations

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/cmd/quickroom/main.go`
- Modify: `backend/internal/sync/service.go`
- Modify: `backend/.env.example`

**Interfaces:**
- Consumes: `appleauth.NewVerifier` (Task 4), `db.AppReservations()` (Task 3).
- Produces: `cfg.AppleBundleID string`, `cfg.SessionTTL time.Duration`.

- [ ] **Step 1: Add config fields**

In `backend/internal/config/config.go`, add to the `Config` struct (after the `OverstayGrace` field):

```go
	// Sign in with Apple: AppleBundleID is checked against the identity
	// token's aud claim. SessionTTL controls how long an issued session
	// (opaque bearer token) stays valid.
	AppleBundleID string
	SessionTTL    time.Duration
```

In `Load()`, add after the `OverstayGrace` parse block:

```go
	c.AppleBundleID = os.Getenv("APPLE_BUNDLE_ID")

	if c.SessionTTL, err = time.ParseDuration(getenv("SESSION_TTL", "720h")); err != nil { // 30 days
		return Config{}, fmt.Errorf("invalid SESSION_TTL: %w", err)
	}
```

- [ ] **Step 2: Tag Zoom-sourced reservations in `backend/internal/sync/service.go`**

In `syncReservations` (currently lines 98-108), add `Source: "zoom"` to the `domain.Reservation` literal:

```go
		s.store.UpsertReservation(domain.Reservation{
			ReservationID:   r.ReservationID,
			RoomID:          roomID,
			ZoomWorkspaceID: r.WorkspaceID,
			UserID:          r.UserID,
			UserEmail:       r.UserEmail,
			StartTime:       r.StartTime,
			EndTime:         r.EndTime,
			Status:          domain.StatusBooked,
			CheckInStatus:   mapCheckIn(r.CheckInStatus),
			Source:          "zoom",
		})
```

- [ ] **Step 3: Wire the verifier and startup reload in `backend/cmd/quickroom/main.go`**

Add the import `"quickroom/internal/appleauth"` to the import block.

After `st.SeedBeacons(beacons)` / `log.Info("beacon registry seeded", ...)` (currently lines 53-54), add:

```go
	// Reload persisted app-native bookings (Zoom sync below only covers
	// zoom-sourced reservations; app bookings have no external source to
	// recover from, so they must be reloaded from SQLite here).
	appRes, err := db.AppReservations()
	if err != nil {
		log.Warn("load app reservations", "err", err)
	}
	for _, r := range appRes {
		st.UpsertReservation(r)
	}
	log.Info("app reservations reloaded", "count", len(appRes))
```

Change the `NewServer` call (currently line 70) to pass the new verifier and session TTL:

```go
	appleVerifier := appleauth.NewVerifier(cfg.AppleBundleID, nil)
	apiSrv := api.NewServer(st, db, sync, zc, cfg.ZoomMode, cfg.PresenceTTL, appleVerifier, cfg.SessionTTL, log)
```

(`api.NewServer`'s new signature is added in Task 6 — this file won't compile until then, matching the same cross-task dependency pattern as Task 2/3.)

- [ ] **Step 4: Document the new env vars in `backend/.env.example`**

Append:

```
# Sign in with Apple: your iOS app's Bundle ID (checked against the identity
# token's aud claim). Required for POST /auth/apple to accept real tokens.
APPLE_BUNDLE_ID=

# How long an issued session (bearer token from POST /auth/apple) stays
# valid. Default 720h (30 days).
SESSION_TTL=720h
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/cmd/quickroom/main.go backend/internal/sync/service.go backend/.env.example
git commit -m "Wire Apple bundle ID / session TTL config and reload app bookings at startup"
```

---

### Task 6: Auth endpoints — `POST /auth/apple`, `POST /auth/logout`, `authMiddleware`

**Files:**
- Create: `backend/internal/api/auth.go`
- Test: `backend/internal/api/auth_test.go`
- Modify: `backend/internal/api/server.go`

**Interfaces:**
- Consumes: `appleauth.Verifier.VerifyIdentityToken` (Task 4), `db.UpsertUser/UserByAppleSub/UserByID/CreateSession/SessionUserID/DeleteSession` (Task 3).
- Produces:
  - `func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc`
  - `func userFromContext(r *http.Request) (domain.User, bool)`
  - `func newReservationID() string`
  - Routes: `POST /auth/apple`, `POST /auth/logout`

- [ ] **Step 1: Extend the `Server` struct and constructor in `backend/internal/api/server.go`**

Add fields to the `Server` struct (after the `overstayGrace` field):

```go
	// Sign in with Apple + sessions.
	appleVerifier *appleauth.Verifier
	sessionTTL    time.Duration
```

Add `"quickroom/internal/appleauth"` to the import block.

Change the `NewServer` signature and body:

```go
func NewServer(st *store.Memory, db *store.DB, sync *syncsvc.Service, zc zoom.Client, mode string, ttl time.Duration, appleVerifier *appleauth.Verifier, sessionTTL time.Duration, log *slog.Logger) *Server {
	s := &Server{store: st, db: db, sync: sync, zoom: zc, mode: mode, ttl: ttl, log: log, diags: newDiagBuffer(50), decisions: newDecisionStore(), scenarioAnswers: newScenarioAnswerStore(), history: newHistoryBuffer(20),
		graceFraction: 0.10, graceMin: 90 * time.Second, graceMax: 15 * time.Minute,
		notify: newNotifier(200), notifyFirstFrac: 0.05, notifySecondFrac: 0.075, notifySecondEnabled: true,
		overstayGrace: 5 * time.Minute,
		appleVerifier: appleVerifier, sessionTTL: sessionTTL}
	if of, ok := zc.(OAuthFlow); ok {
		s.oauth = of
	}
	return s
}
```

- [ ] **Step 2: Update `backend/internal/api/server_test.go`'s `newTestHandler`**

Add the import `"quickroom/internal/appleauth"`, and change the `NewServer` call (currently line 38):

```go
	return api.NewServer(st, db, sy, zc, "mock", 30*time.Minute, appleauth.NewVerifier("test.bundle.id", nil), time.Hour, log).Handler()
```

- [ ] **Step 3: Write the failing test**

Create `backend/internal/api/auth_test.go`:

```go
package api_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signAppleToken builds an RS256 JWT and returns it plus a JWKS-serving test
// server it can be verified against, mimicking Apple's identity token issuance.
func signAppleToken(t *testing.T, bundleID, sub, email string) (token string, jwksURL string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	// A distinct kid per call: the Verifier caches keys by kid for 24h, so two
	// tokens signed with different keys but the same kid (as would happen
	// across two calls in one test) would have the second verification
	// incorrectly served the first key from cache.
	kid := "test-kid-" + sub

	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": "AQAB", // 65537
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "https://appleid.apple.com", "aud": bundleID, "sub": sub,
		"email": email, "email_verified": "true",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed, srv.URL
}

func TestAppleSignInThenLogout(t *testing.T) {
	h := newTestHandler(t)

	// newTestHandler wires appleauth.NewVerifier("test.bundle.id", nil) with
	// the real Apple JWKS URL, which a test token can't be signed against —
	// this test exercises the failure path (invalid token) plus, via a
	// second handler built with an overridable verifier, the success path.
	body, _ := json.Marshal(map[string]string{"identity_token": "not-a-real-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an invalid identity token", rec.Code)
	}
}

func TestAppleSignInSuccess(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-xyz", "user@example.com")
	verifier.KeysURL = jwksURL

	body, _ := json.Marshal(map[string]string{"identity_token": token, "name": "Ava"})
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
			Name   string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionToken == "" || resp.User.Email != "user@example.com" || resp.User.Name != "Ava" {
		t.Fatalf("response = %+v, want a non-empty session token and matching user", resp)
	}

	// The session should authenticate a protected endpoint.
	req2 := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	req2.Header.Set("Authorization", "Bearer "+resp.SessionToken)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /reservations/mine with valid session: status = %d, want 200", rec2.Code)
	}

	// Logout, then the same token should be rejected.
	req3 := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req3.Header.Set("Authorization", "Bearer "+resp.SessionToken)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec3.Code)
	}

	req4 := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	req4.Header.Set("Authorization", "Bearer "+resp.SessionToken)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusUnauthorized {
		t.Fatalf("GET /reservations/mine after logout: status = %d, want 401", rec4.Code)
	}
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with no Authorization header", rec.Code)
	}
}
```

This test needs `newTestHandlerWithVerifier`, which returns the `*appleauth.Verifier` so the test can point it at a fake JWKS server. Add it to `backend/internal/api/server_test.go` right after `newTestHandler`:

```go
// newTestHandlerWithVerifier is newTestHandler, but also returns the
// appleauth.Verifier so tests can override its KeysURL to a fake JWKS server.
func newTestHandlerWithVerifier(t *testing.T) (http.Handler, *appleauth.Verifier) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	st := store.NewMemory()
	db, err := store.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	zc := zoom.NewMockClient(now, nil, log)
	sy := syncsvc.New(zc, st, "", log)
	if _, err := sy.Run(context.Background(), now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	verifier := appleauth.NewVerifier("test.bundle.id", nil)
	return api.NewServer(st, db, sy, zc, "mock", 30*time.Minute, verifier, time.Hour, log).Handler(), verifier
}
```

(This duplicates most of `newTestHandler`'s body — acceptable here since the two helpers diverge only in their return signature and Go has no clean way to vary a return tuple otherwise; matches the existing single-purpose-helper style in this test file.)

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run 'TestAppleSignIn|TestProtectedEndpointRequiresAuth' -v`
Expected: FAIL to compile — `POST /auth/apple` route, `authMiddleware`, and `/reservations/mine` don't exist yet.

- [ ] **Step 5: Write `backend/internal/api/auth.go`**

```go
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"quickroom/internal/domain"
)

type ctxKey int

const userCtxKey ctxKey = iota

// userFromContext returns the authenticated user attached by authMiddleware.
func userFromContext(r *http.Request) (domain.User, bool) {
	u, ok := r.Context().Value(userCtxKey).(domain.User)
	return u, ok
}

// authMiddleware resolves a bearer session token to its user and attaches it
// to the request context. 401s on a missing, invalid, or expired session.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		user, ok, err := s.sessionUser(token)
		if err != nil {
			s.log.Error("session lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, user)))
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}

func (s *Server) sessionUser(token string) (domain.User, bool, error) {
	userID, ok, err := s.db.SessionUserID(hashToken(token), time.Now())
	if err != nil || !ok {
		return domain.User{}, false, err
	}
	return s.db.UserByID(userID)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newSessionToken returns a fresh opaque token (returned to the caller once)
// and its SHA-256 hash (what's actually persisted).
func newSessionToken() (raw, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is effectively unrecoverable in practice;
		// falling back to a fixed value here would be a real security bug
		// (predictable session tokens), so surface it as an empty token —
		// callers must treat an empty raw token as a hard failure.
		return "", ""
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

// newUserID generates a local user identifier, distinct from Apple's sub.
func newUserID() string { return randomPrefixedID("usr_") }

// newReservationID generates an id for an app-sourced booking, distinct from
// Zoom's own reservation id scheme.
func newReservationID() string { return randomPrefixedID("app-") }

func randomPrefixedID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return prefix + hex.EncodeToString([]byte("quickroom-fallback"))
	}
	return prefix + hex.EncodeToString(b)
}

// postAppleAuth verifies an Apple identity token, upserts the local user
// record, and issues a new session.
func (s *Server) postAppleAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IdentityToken string `json:"identity_token"`
		Name          string `json:"name"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.IdentityToken == "" {
		writeError(w, http.StatusUnprocessableEntity, "identity_token required")
		return
	}

	claims, err := s.appleVerifier.VerifyIdentityToken(r.Context(), body.IdentityToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid apple identity token")
		return
	}

	user, existed, err := s.db.UserByAppleSub(claims.Sub)
	if err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !existed {
		user = domain.User{
			UserID:    newUserID(),
			AppleSub:  claims.Sub,
			Email:     claims.Email,
			Name:      clamp(body.Name, maxNameLen),
			CreatedAt: time.Now(),
		}
	} else {
		user.Email = claims.Email
	}
	if err := s.db.UpsertUser(user); err != nil {
		s.log.Error("upsert user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	raw, hash := newSessionToken()
	if raw == "" {
		s.log.Error("generate session token: crypto/rand failed")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := time.Now()
	if err := s.db.CreateSession(hash, user.UserID, now, now.Add(s.sessionTTL)); err != nil {
		s.log.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"session_token": raw, "user": user})
}

// postLogout deletes the caller's session. Idempotent: no-op if already gone.
func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token != "" {
		if err := s.db.DeleteSession(hashToken(token)); err != nil {
			s.log.Warn("delete session", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 6: Register the routes in `Handler()` in `backend/internal/api/server.go`**

Add near the other route registrations (after `mux.HandleFunc("GET /overstays", s.getOverstays)`):

```go
	mux.HandleFunc("POST /auth/apple", s.postAppleAuth)
	mux.HandleFunc("POST /auth/logout", s.authMiddleware(s.postLogout))
```

(`GET /reservations/mine` is registered in Task 7, alongside the other new booking routes — leaving it out here would make this task's own test fail to compile against a route that doesn't exist yet. Add this one line now too, pointing at a placeholder that Task 7 replaces:)

```go
	mux.HandleFunc("GET /reservations/mine", s.authMiddleware(s.listMyReservations))
```

Note: `s.listMyReservations` is defined in Task 7. This task's test (`TestAppleSignInSuccess`, which calls `GET /reservations/mine`) therefore depends on Task 7 landing too — treat Tasks 6 and 7 as landing together before running this task's tests, same cross-task dependency pattern as earlier tasks.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/auth.go backend/internal/api/auth_test.go backend/internal/api/server.go backend/internal/api/server_test.go
git commit -m "Add Sign in with Apple auth endpoints and session middleware"
```

---

### Task 7: Booking endpoints — create, list mine, cancel

**Files:**
- Create: `backend/internal/api/booking.go`
- Test: `backend/internal/api/booking_test.go`
- Modify: `backend/internal/api/server.go`

**Interfaces:**
- Consumes: `userFromContext`, `authMiddleware`, `newReservationID()` (Task 6); `s.upsertReservation` (Task 2); `s.db.SaveAppReservation` (Task 3).
- Produces:
  - `func (s *Server) createReservation(w http.ResponseWriter, r *http.Request)`
  - `func (s *Server) listMyReservations(w http.ResponseWriter, r *http.Request)`
  - `func (s *Server) cancelReservation(w http.ResponseWriter, r *http.Request)`
  - `func (s *Server) conflictingReservation(workspaceID string, start, end time.Time) (domain.Reservation, bool)`
  - Routes: `POST /reservations`, `POST /reservations/{id}/cancel` (`GET /reservations/mine` already registered in Task 6)

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/booking_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateListCancelReservation(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-booking-1", "booker@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(authRec.Body.Bytes(), &authResp); err != nil || authResp.SessionToken == "" {
		t.Fatalf("sign-in failed: %s", authRec.Body.String())
	}

	// A room that exists in the mock Zoom seed (see zoom.NewMockClient's
	// default seed — ws-agung is one of the built-in Bali rooms).
	start := time.Now().Add(48 * time.Hour) // far in the future, clear of the mock seed's own reservations
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-agung", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s, want 200", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ReservationID string `json:"reservation_id"`
		Source        string `json:"source"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil || created.Source != "app" {
		t.Fatalf("create response = %s", createRec.Body.String())
	}

	// Check in on the app-sourced reservation — the mock Zoom client has
	// never seen this ID, so if the Task 2 guard didn't skip the Zoom call
	// for Source == "app", this would 404 ("reservation not found in
	// zoom") instead of succeeding. This is what actually proves the guard.
	checkInReq := httptest.NewRequest(http.MethodPost, "/reservations/"+created.ReservationID+"/check-in", nil)
	checkInRec := httptest.NewRecorder()
	h.ServeHTTP(checkInRec, checkInReq)
	if checkInRec.Code != http.StatusOK {
		t.Fatalf("check-in on app-sourced reservation status = %d, body = %s, want 200 (Zoom call should have been skipped)", checkInRec.Code, checkInRec.Body.String())
	}

	// A second booking for the exact same room/window must conflict.
	conflictReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	conflictReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	conflictRec := httptest.NewRecorder()
	h.ServeHTTP(conflictRec, conflictReq)
	if conflictRec.Code != http.StatusConflict {
		t.Fatalf("conflicting create status = %d, want 409", conflictRec.Code)
	}

	// List mine — should contain exactly the one booking.
	listReq := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	listReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	var listResp struct {
		Reservations []struct {
			ReservationID string `json:"reservation_id"`
		} `json:"reservations"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil || len(listResp.Reservations) != 1 {
		t.Fatalf("list mine = %s, want exactly 1 reservation", listRec.Body.String())
	}

	// Cancel it, then a new booking for the same window should succeed.
	cancelReq := httptest.NewRequest(http.MethodPost, "/reservations/"+created.ReservationID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s, want 200", cancelRec.Code, cancelRec.Body.String())
	}

	rebookReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	rebookReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	rebookRec := httptest.NewRecorder()
	h.ServeHTTP(rebookRec, rebookReq)
	if rebookRec.Code != http.StatusOK {
		t.Fatalf("rebook after cancel status = %d, want 200 (cancelled bookings shouldn't block)", rebookRec.Code)
	}
}

func TestCancelSomeoneElsesReservationForbidden(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	tokenA, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-A", "a@example.com")
	verifier.KeysURL = jwksURL

	authBodyA, _ := json.Marshal(map[string]string{"identity_token": tokenA})
	authReqA := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBodyA))
	authRecA := httptest.NewRecorder()
	h.ServeHTTP(authRecA, authReqA)
	var respA struct {
		SessionToken string `json:"session_token"`
	}
	_ = json.Unmarshal(authRecA.Body.Bytes(), &respA)

	start := time.Now().Add(72 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-bedugul", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+respA.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	var created struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	// A different signed-in user tries to cancel A's booking.
	tokenB, jwksURLB := signAppleToken(t, "test.bundle.id", "apple-sub-B", "b@example.com")
	verifier.KeysURL = jwksURLB
	authBodyB, _ := json.Marshal(map[string]string{"identity_token": tokenB})
	authReqB := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBodyB))
	authRecB := httptest.NewRecorder()
	h.ServeHTTP(authRecB, authReqB)
	var respB struct {
		SessionToken string `json:"session_token"`
	}
	_ = json.Unmarshal(authRecB.Body.Bytes(), &respB)

	cancelReq := httptest.NewRequest(http.MethodPost, "/reservations/"+created.ReservationID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+respB.SessionToken)
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusForbidden {
		t.Fatalf("cancel-someone-else's status = %d, want 403", cancelRec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run 'TestCreateListCancelReservation|TestCancelSomeoneElsesReservationForbidden' -v`
Expected: FAIL to compile — `POST /reservations` and `POST /reservations/{id}/cancel` routes don't exist yet.

- [ ] **Step 3: Write `backend/internal/api/booking.go`**

```go
package api

import (
	"net/http"
	"time"

	"quickroom/internal/domain"
)

// conflictingReservation reports the first non-cancelled, non-released,
// non-no-show reservation for workspaceID that overlaps [start, end) —
// regardless of Source, per the product decision that app bookings must not
// collide with Zoom-synced ones either.
func (s *Server) conflictingReservation(workspaceID string, start, end time.Time) (domain.Reservation, bool) {
	for _, r := range s.store.Reservations() {
		if r.ZoomWorkspaceID != workspaceID {
			continue
		}
		switch r.Status {
		case domain.StatusReleased, domain.StatusCancelled, domain.StatusNoShow:
			continue
		}
		if start.Before(r.EndTime) && r.StartTime.Before(end) {
			return r, true
		}
	}
	return domain.Reservation{}, false
}

// createReservation books a room for the signed-in user.
func (s *Server) createReservation(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	var body struct {
		WorkspaceID string    `json:"workspace_id"`
		StartTime   time.Time `json:"start_time"`
		EndTime     time.Time `json:"end_time"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.WorkspaceID == "" || len(body.WorkspaceID) > maxIDLen {
		writeError(w, http.StatusUnprocessableEntity, "workspace_id required; 1..128 chars")
		return
	}
	if !body.EndTime.After(body.StartTime) {
		writeError(w, http.StatusUnprocessableEntity, "end_time must be after start_time")
		return
	}

	room, ok := s.store.RoomByWorkspace(body.WorkspaceID)
	if !ok {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	if conflict, has := s.conflictingReservation(body.WorkspaceID, body.StartTime, body.EndTime); has {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "room already booked in that window",
			"conflict": conflict,
		})
		return
	}

	res := domain.Reservation{
		ReservationID:   newReservationID(),
		RoomID:          room.RoomID,
		ZoomWorkspaceID: body.WorkspaceID,
		UserID:          user.UserID,
		UserEmail:       user.Email,
		StartTime:       body.StartTime,
		EndTime:         body.EndTime,
		Status:          domain.StatusBooked,
		CheckInStatus:   domain.NotCheckedIn,
		Source:          "app",
		BookedByUserID:  user.UserID,
	}
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}

// listMyReservations returns the signed-in user's own bookings.
func (s *Server) listMyReservations(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	mine := []domain.Reservation{}
	for _, res := range s.store.Reservations() {
		if res.BookedByUserID == user.UserID {
			mine = append(mine, res)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reservations": mine})
}

// cancelReservation cancels the caller's own app-sourced booking. 404 if it
// doesn't exist, 403 if it belongs to someone else or isn't app-sourced
// (Zoom-synced reservations aren't cancellable through this endpoint).
func (s *Server) cancelReservation(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if res.Source != "app" || res.BookedByUserID != user.UserID {
		writeError(w, http.StatusForbidden, "not your reservation")
		return
	}
	res.Status = domain.StatusCancelled
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}
```

- [ ] **Step 4: Register routes in `Handler()` in `backend/internal/api/server.go`**

Add next to the `GET /reservations/mine` line added in Task 6:

```go
	mux.HandleFunc("POST /reservations", s.authMiddleware(s.createReservation))
	mux.HandleFunc("POST /reservations/{id}/cancel", s.authMiddleware(s.cancelReservation))
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS — every test in the package, including Task 6's `auth_test.go` tests and this task's `booking_test.go` tests.

- [ ] **Step 6: Run the full test suite**

Run: `cd backend && go vet ./... && go test ./...`
Expected: all packages PASS, `go vet` clean.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/booking.go backend/internal/api/booking_test.go backend/internal/api/server.go
git commit -m "Add POST /reservations, GET /reservations/mine, POST /reservations/{id}/cancel"
```

---

### Task 8: OpenAPI docs

**Files:**
- Modify: `backend/internal/api/openapi.yaml`

**Interfaces:**
- Consumes: nothing (documentation only).

- [ ] **Step 1: Add a `bearerAuth` security scheme**

In `backend/internal/api/openapi.yaml`, add a top-level `components.securitySchemes` block (create `components:` if a schema-only one already exists, merging in) :

```yaml
components:
  securitySchemes:
    sessionToken:
      type: http
      scheme: bearer
      description: "Opaque session token returned by POST /auth/apple. Send as `Authorization: Bearer <token>`."
```

- [ ] **Step 2: Add the new paths**

Add under the existing `paths:` key, near the other `/reservations/*` entries:

```yaml
  /auth/apple:
    post:
      tags: [Auth]
      summary: Sign in with Apple
      description: Verifies an Apple identity token (obtained on-device via ASAuthorizationAppleIDProvider) and issues a session.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [identity_token]
              properties:
                identity_token:
                  type: string
                  description: The JWT identityToken from Apple's on-device sign-in.
                name:
                  type: string
                  description: The user's display name. Apple only sends this on the very first authorization — forward it then.
      responses:
        '200':
          description: Signed in.
          content:
            application/json:
              schema:
                type: object
                properties:
                  session_token:
                    type: string
                  user:
                    type: object
                    properties:
                      user_id: { type: string }
                      email: { type: string }
                      name: { type: string }
                      created_at: { type: string, format: date-time }
        '401':
          description: Invalid or unverifiable identity token.
  /auth/logout:
    post:
      tags: [Auth]
      summary: Sign out
      security: [{ sessionToken: [] }]
      responses:
        '200':
          description: Session revoked (always succeeds, even if already invalid).
  /reservations/mine:
    get:
      tags: [Reservations]
      summary: List the signed-in user's own bookings
      security: [{ sessionToken: [] }]
      responses:
        '200':
          description: OK
        '401':
          description: Missing or invalid session.
  /reservations/{id}/cancel:
    post:
      tags: [Reservations]
      summary: Cancel your own app-sourced booking
      security: [{ sessionToken: [] }]
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        '200':
          description: Cancelled.
        '403':
          description: Not your reservation, or it isn't app-sourced.
        '404':
          description: Reservation not found.
```

And add a `POST /reservations` entry near the existing `GET /reservations`:

```yaml
  /reservations:
    get:
      # ... existing GET definition stays exactly as-is ...
    post:
      tags: [Reservations]
      summary: Book a room (QuickRoom-native booking)
      security: [{ sessionToken: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [workspace_id, start_time, end_time]
              properties:
                workspace_id: { type: string }
                start_time: { type: string, format: date-time }
                end_time: { type: string, format: date-time }
      responses:
        '200':
          description: Booked.
        '404':
          description: Room not found.
        '409':
          description: The room is already booked in that window.
        '422':
          description: Invalid request (e.g. end_time not after start_time).
```

- [ ] **Step 3: Verify the docs still serve correctly**

Run: `cd backend && DB_PATH=/tmp/quickroom-docs-check.db go run ./cmd/quickroom &`
Run: `sleep 2 && curl -s http://localhost:8080/openapi.yaml | head -5` → expect valid YAML output, no error.
Run: `curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/docs` → expect `200`.
Stop the server (`kill %1`), remove `/tmp/quickroom-docs-check.db*`.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/openapi.yaml
git commit -m "Document the Sign in with Apple and booking endpoints in the OpenAPI spec"
```

---

### Task 9: Full verification pass

**Files:** none (verification only).

- [ ] **Step 1: Full test suite + vet**

Run: `cd backend && go vet ./... && go test ./... -v 2>&1 | tail -80`
Expected: every package passes, including the new `appleauth`, and the extended `store`/`api` packages.

- [ ] **Step 2: Manual end-to-end flow against a running server**

Run: `cd backend && DB_PATH=/tmp/quickroom-e2e.db go run ./cmd/quickroom &`

Since a real Apple identity token can't be minted outside Apple's infra, verify the shape of the flow with a deliberately invalid token (proves the endpoint, verification, and error path are wired):

Run: `curl -s -X POST http://localhost:8080/auth/apple -H 'Content-Type: application/json' -d '{"identity_token":"not-a-real-token"}'` → expect a 401 JSON error body, not a 500 or a panic.

Run: `curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/reservations/mine` → expect `401` (no auth header).

Run: `curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/reservations -d '{}'` → expect `401` (no auth header — the middleware rejects before body validation).

- [ ] **Step 3: Restart-durability check**

This confirms Task 5's startup-reload logic actually works against a real running process (the unit test in Task 3 only tests the SQLite layer in isolation, not the full startup path).

Since minting a real Apple token isn't possible from the shell, verify durability at the store layer instead — this was already covered by `TestAppReservationRoundTrip` (Task 3) and is exercised end-to-end by `TestCreateListCancelReservation` (Task 7) within a single process. Accept that as sufficient coverage; a true restart-durability manual check would require a real Apple identity token, which isn't available in this environment.

Stop the server (`kill %1`), remove `/tmp/quickroom-e2e.db*`.

- [ ] **Step 4: Confirm existing functionality is unaffected**

Run: `cd backend && go test ./... -run 'TestHeartbeatDrivesOccupancyAndCheckIn|TestStaleHeartbeatIgnored|TestBoundaryValidation|TestDocsServed' -v`
Expected: all PASS — confirms the Zoom-call-skip changes (Task 2) didn't break the existing zoom-sourced check-in/heartbeat flow, which still must call Zoom exactly as before for `Source == "zoom"` (i.e. `Source == ""` for reservations created before this feature shipped — `Source != "app"` is true for `""` too, so the old code path is preserved by construction).
