# Admin CRUD + JWT Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admin email+password login and full CRUD over the admin panel's resources, with HS256 JWTs replacing opaque sessions for both admin and mobile principals — without breaking Rei's app or losing any existing data.

**Architecture:** A tiny `internal/authtoken` signer mints/verifies role-carrying JWTs; `requireUser`/`requireAdmin` middleware replace the session middleware (principal-existence check preserved). Rooms CRUD rides on two new SQLite tables (custom rooms + overrides) re-applied after every Zoom sync. Admin UI gains a login route and inline CRUD.

**Tech Stack:** Go 1.26, `golang-jwt/v5` (present), `golang.org/x/crypto/bcrypt` (new), SQLite additive migration, Vue 3 + TS admin SPA.

**Spec:** `docs/superpowers/specs/2026-07-05-admin-crud-jwt-auth-design.md`

## Global Constraints

- **Keep current data:** schema changes are `CREATE TABLE IF NOT EXISTS` only; the `sessions` table STAYS (only its code is deleted); never wipe `/data` or the DB.
- Backend work on `main` in this repo. Go gate: `cd backend && go test ./... && go vet ./...` fully green after every task.
- **No secret/identifier values in git**: `JWT_SECRET`, `ADMIN_*` values live in the VPS `.env`; compose gets `${VAR:-}` passthroughs. The in-code seed defaults are the placeholder creds Asadullokh supplied (explicitly "for now").
- Mobile compatibility is a hard requirement: `POST /auth/apple` keeps the `session_token` response field (now a JWT); `GET /rooms|/reservations|/beacons|/occupancy`, `POST /presence*` stay open; `POST /devices/apns`, booking trio stay user-bearer-authenticated.
- NEVER add `Co-Authored-By`; concise imperative commits; no emojis.
- Frontend gate: `cd frontend && npm run build` green (writes into `backend/internal/api/web/dist`).
- Deploy per memory: build frontend first, tar `backend/` contents (exclude only `.env`, `docker-compose.yml`, `.git`, `*.db*`, `.DS_Store`, `._*`) to `/root/roompulse` over `pr-diriger-hetzner`, `docker compose -p backend up -d --build`.

---

### Task 1: `internal/authtoken` — JWT signer

**Files:**
- Create: `backend/internal/authtoken/authtoken.go`
- Test: `backend/internal/authtoken/authtoken_test.go`

**Interfaces (produces):**
- `func LoadOrCreateSecret(envValue, filePath string) ([]byte, error)` — env wins; else read file; else create 32 random bytes, write file `0600`, return them.
- `func NewSigner(secret []byte) *Signer`
- `func (s *Signer) Mint(sub, role string, ttl time.Duration) (string, error)`
- `func (s *Signer) Verify(token string) (sub, role string, err error)` — signature + `exp` enforced; `RoleAdmin = "admin"`, `RoleUser = "user"` consts.

- [ ] **Step 1: Failing tests**

```go
package authtoken

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMintVerifyRoundtrip(t *testing.T) {
	s := NewSigner([]byte("test-secret"))
	tok, err := s.Mint("usr_1", RoleUser, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sub, role, err := s.Verify(tok)
	if err != nil || sub != "usr_1" || role != RoleUser {
		t.Fatalf("verify = %q %q %v", sub, role, err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	s := NewSigner([]byte("k"))
	tok, _ := s.Mint("usr_1", RoleUser, -time.Minute)
	if _, _, err := s.Verify(tok); err == nil {
		t.Fatal("expected expiry error")
	}
}

func TestVerifyRejectsWrongKeyAndGarbage(t *testing.T) {
	tok, _ := NewSigner([]byte("k1")).Mint("usr_1", RoleAdmin, time.Hour)
	if _, _, err := NewSigner([]byte("k2")).Verify(tok); err == nil {
		t.Fatal("expected signature error")
	}
	if _, _, err := NewSigner([]byte("k1")).Verify("garbage"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadOrCreateSecret(t *testing.T) {
	if got, _ := LoadOrCreateSecret("env-secret", ""); string(got) != "env-secret" {
		t.Fatalf("env should win, got %q", got)
	}
	path := filepath.Join(t.TempDir(), "jwt_secret")
	first, err := LoadOrCreateSecret("", path)
	if err != nil || len(first) != 32 {
		t.Fatalf("create = %v len %d", err, len(first))
	}
	second, _ := LoadOrCreateSecret("", path)
	if string(first) != string(second) {
		t.Fatal("secret must be stable across loads")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
}
```

- [ ] **Step 2: Red** — `go test ./internal/authtoken/` fails to build.
- [ ] **Step 3: Implement**

```go
// Package authtoken mints and verifies the HS256 JWTs used by both the admin
// panel (role "admin") and the mobile app (role "user").
package authtoken

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// LoadOrCreateSecret resolves the signing secret: the env value wins; else
// the file's contents; else 32 fresh random bytes persisted to filePath so
// restarts don't invalidate every token.
func LoadOrCreateSecret(envValue, filePath string) ([]byte, error) {
	if envValue != "" {
		return []byte(envValue), nil
	}
	if b, err := os.ReadFile(filePath); err == nil && len(b) > 0 {
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("authtoken: generate secret: %w", err)
	}
	if err := os.WriteFile(filePath, b, 0o600); err != nil {
		return nil, fmt.Errorf("authtoken: persist secret: %w", err)
	}
	return b, nil
}

type Signer struct {
	secret []byte
}

func NewSigner(secret []byte) *Signer { return &Signer{secret: secret} }

func (s *Signer) Mint(sub, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  sub,
		"role": role,
		"iat":  now.Unix(),
		"exp":  now.Add(ttl).Unix(),
	}).SignedString(s.secret)
}

func (s *Signer) Verify(token string) (sub, role string, err error) {
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return "", "", err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return "", "", errors.New("authtoken: invalid claims")
	}
	sub, _ = claims["sub"].(string)
	role, _ = claims["role"].(string)
	if sub == "" || role == "" {
		return "", "", errors.New("authtoken: missing sub/role")
	}
	return sub, role, nil
}
```

- [ ] **Step 4: Green** — `go test ./internal/authtoken/ -v` all PASS.
- [ ] **Step 5: Commit** — `git add backend/internal/authtoken && git commit -m "Add HS256 JWT signer with persisted secret"`

---

### Task 2: Store — admins, custom rooms, overrides, user rename

**Files:**
- Modify: `backend/internal/store/sqlite.go`; Test: `backend/internal/store/sqlite_test.go`

**Interfaces (produces):**
- `type Admin struct { AdminID, Email, PasswordHash string; CreatedAt time.Time }`
- `func (d *DB) EnsureAdmin(email, passwordHash string, at time.Time) error` — insert only if `admins` is empty.
- `func (d *DB) AdminByEmail(email string) (Admin, bool, error)`, `func (d *DB) AdminByID(id string) (Admin, bool, error)`
- `type RoomOverride struct { WorkspaceID, Name string; Capacity int; HasTV int }` — `Name ""` / `Capacity -1` / `HasTV -1` mean "keep".
- `func (d *DB) SaveCustomRoom(r domain.Room, at time.Time) error` (upsert), `CustomRooms() ([]domain.Room, error)`, `DeleteCustomRoom(workspaceID string) error`
- `func (d *DB) SaveRoomOverride(o RoomOverride) error` (upsert, merging: incoming "keep" fields preserve existing override values), `RoomOverrides() ([]RoomOverride, error)`, `ClearRoomOverride(workspaceID string) error`
- `func (d *DB) UpdateUserName(userID, name string) error`

Schema additions (append to `schema` const; additive only):

```sql
CREATE TABLE IF NOT EXISTS admins (
	admin_id      TEXT PRIMARY KEY,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
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
	workspace_id TEXT PRIMARY KEY,   -- Zoom room being overridden
	name         TEXT NOT NULL DEFAULT '',   -- '' = keep Zoom value
	capacity     INTEGER NOT NULL DEFAULT -1, -- -1 = keep
	has_tv       INTEGER NOT NULL DEFAULT -1  -- -1 = keep, else 0/1
);
```

- [ ] **Step 1: Failing tests** — append: `TestEnsureAdminSeedsOnce` (EnsureAdmin twice with different emails → only first row exists; AdminByEmail hit/miss), `TestCustomRoomsCRUD` (save→list→delete roundtrip, upsert updates name), `TestRoomOverridesMerge` (save {name:"X",cap:-1,tv:-1}, then {name:"",cap:12,tv:-1} → merged row has name X + cap 12; Clear removes), `TestUpdateUserName`.
- [ ] **Step 2: Red.**
- [ ] **Step 3: Implement** methods following the file's existing style (Exec/QueryRow + `ON CONFLICT ... DO UPDATE` for upserts; the override merge uses `CASE WHEN excluded.name <> '' THEN excluded.name ELSE room_overrides.name END` etc.). Custom rooms map to `domain.Room{RoomID: "room-"+ws, ZoomWorkspaceID: ws, IsZoomRoom: false}`.
- [ ] **Step 4: Green** — `go test ./internal/store/`.
- [ ] **Step 5: Commit** — `"Add admins, custom rooms, room overrides, user rename to store"`

---

### Task 3: Auth swap — login endpoint, JWT middleware, session code removal

**Files:**
- Modify: `backend/internal/api/auth.go` (bulk rewrite), `backend/internal/api/server.go` (NewServer signature + routes), `backend/internal/api/users.go` (drop session revocation), `backend/internal/store/sqlite.go` (delete `CreateSession/SessionUserID/DeleteSession/DeleteSessionsForUser`), `backend/cmd/quickroom/main.go` (signer wiring + admin seed), `backend/internal/config/config.go` (`JWTSecret`, `AdminEmail`, `AdminPassword`), test helpers (`server_test.go`, `noshow_test.go`, `apns_register_test.go`, `booking_test.go` unchanged flows)
- Create: `backend/internal/api/login.go`; Test: `backend/internal/api/login_test.go`

**Interfaces:**
- Consumes: `authtoken.Signer` (T1), `store.EnsureAdmin/AdminByEmail/AdminByID` (T2), bcrypt.
- Produces:
  - `NewServer(st, db, sync, zc, mode, ttl, appleVerifier, userTokenTTL, signer *authtoken.Signer, log)` — signer added after the TTL param.
  - `func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc` — JWT role `user` + `UserByID` existence → context user (replaces `authMiddleware`; `userFromContext` unchanged).
  - `func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc` — role `admin` + `AdminByID` existence.
  - `POST /auth/login {email,password}` → `{token, email}`; admin TTL 12 h const `adminTokenTTL`.
  - `postAppleAuth` mints `signer.Mint(user.UserID, RoleUser, s.userTokenTTL)` and returns it in the existing `session_token` field; `postLogout` → plain `{status:"ok"}` no-op behind `requireUser`.
  - Deleted: `sessionUser`, `hashToken`, `newSessionToken`, all four store session methods, `deleteUser`'s `DeleteSessionsForUser` call.
  - `mintSession` test helper (apns_register_test.go) becomes `mintUserToken(t, s, userID, email) string` → upserts user + `s.signer.Mint(userID, RoleUser, time.Hour)`; a sibling `mintAdminToken(t, s) string` seeds admin `admin@test.local` (bcrypt of "pw") + mints role admin.
- **Login handler** (`login.go`): decode body → 422 empties → `AdminByEmail` → `bcrypt.CompareHashAndPassword` → uniform 401 `"invalid credentials"` on either miss → `signer.Mint(admin.AdminID, RoleAdmin, adminTokenTTL)`.
- **main.go:** `secret, err := authtoken.LoadOrCreateSecret(cfg.JWTSecret, filepath.Join(filepath.Dir(cfg.DBPath), "jwt_secret"))` → fatal on err; `signer := authtoken.NewSigner(secret)`; seed: bcrypt-hash `cfg.AdminPassword` → `db.EnsureAdmin(cfg.AdminEmail, hash, time.Now())` (admin id `adm_`+hex via api's randomPrefixedID pattern — put id generation inside EnsureAdmin using crypto/rand hex).
- **config.go:** `JWTSecret = os.Getenv("JWT_SECRET")`, `AdminEmail = getenv("ADMIN_EMAIL", "admin@example.com")`, `AdminPassword = getenv("ADMIN_PASSWORD", "SuperAdmin123!")`.

- [ ] **Step 1: Failing tests** (`login_test.go`, package `api`): good creds → 200 + token verifies role admin; wrong password → 401; unknown email → 401; missing fields → 422; user-role token on `GET /users` → 403; admin token on `GET /users` → 200; token of deleted user on `POST /reservations` → 401; `POST /auth/apple` (mock verifier path reused from booking_test's `signAppleToken`, package boundary: write this bit in `api_test` file `auth_jwt_test.go` instead) returns a `session_token` that passes `requireUser` on `GET /reservations/mine`.
- [ ] **Step 2: Red.**
- [ ] **Step 3: Implement** everything above; update every `NewServer(` call site (grep). Add `go get golang.org/x/crypto`.
- [ ] **Step 4: Green** — full `go test ./...` (booking/users/apns tests keep passing via updated helpers).
- [ ] **Step 5: Commit** — `"Replace opaque sessions with role-carrying JWTs; add admin login

Sessions table kept (data preserved) but no longer written; principal
existence is checked per request so user deletion still revokes access."`

---

### Task 4: Protection matrix

**Files:**
- Modify: `backend/internal/api/server.go` (route table); Test: `backend/internal/api/authz_matrix_test.go`

Wrap with `s.requireAdmin`: `GET /users`, `GET /users/{id}/reservations`, `DELETE /users/{id}`, `POST /admin/reservations/{id}/cancel`, `PUT/DELETE /beacons/{workspace_id}`, `POST /sync`, `GET /notifications`, `GET /collisions`, `GET /overstays`, `GET /utilization`, `GET /devices`, `GET /events`, `GET/POST /diag*`, `GET/POST /history*`, `GET/POST /decision*`, `GET/POST /scenario-answers*`, `POST /reservations/{id}/check-in`, `POST /reservations/{id}/check-out`. Already-user-wrapped routes switch `authMiddleware` → `requireUser`.

- [ ] **Step 1: Failing test** — table-driven sweep: for each `{method, path}` in the admin list, no token → 401 and user token → 403; for the open list (`GET /rooms`, `GET /reservations`, `GET /beacons`, `GET /occupancy`, `POST /presence` well-formed body, `GET /health/live`, `GET /info`) → NOT 401/403.
- [ ] **Step 2: Red** (unwrapped routes return 200/422 where 401 expected).
- [ ] **Step 3: Wrap routes.** **Step 4: Green (full suite).**
- [ ] **Step 5: Commit** — `"Require admin JWT on the admin surface"`

---

### Task 5: Rooms CRUD with overrides + sync re-apply

**Files:**
- Create: `backend/internal/api/rooms_admin.go`; Test: `backend/internal/api/rooms_admin_test.go`
- Modify: `backend/internal/store/memory.go` (`DeleteRoom`), `backend/internal/sync/service.go` (+db param, `applyAdminRooms`), `backend/cmd/quickroom/main.go` + all `syncsvc.New(` call sites (tests) — signature `New(zc, st, db, locationID, log)` (db nil-safe).

**Interfaces:**
- `POST /rooms {name, capacity, has_tv}` (admin) → 200 room; id `cr-`+8-hex; persists `SaveCustomRoom` + `store.UpsertRoom` live.
- `PATCH /rooms/{workspace_id} {name?, capacity?, has_tv?}` — custom (`strings.HasPrefix(id, "cr-")`): update `custom_rooms` + live mirror; zoom: `SaveRoomOverride` (absent fields → keep sentinels) + patch live mirror. 404 unknown.
- `DELETE /rooms/{workspace_id}` — custom: cancel its open app bookings (loop like deleteUser), delete beacon mapping (`s.store` beacon field cleared via existing beacon delete path — call the same store function beacons.go uses), `DeleteCustomRoom` + `Memory.DeleteRoom`; zoom: `ClearRoomOverride` + immediate re-sync of that room's fields is NOT needed (next sync restores; also re-fetch zoom values live via one `sync.Run`? KISS: return `{status:"reset"}` and let the 60 s sync restore).
- `sync.Run` tail: `applyAdminRooms` — `db.CustomRooms()` → `UpsertRoom` each; `db.RoomOverrides()` → `RoomByWorkspace` + patch non-sentinel fields + `UpsertRoom`.

- [ ] **Step 1: Failing tests** — create→appears in `GET /rooms`; PATCH custom renames; PATCH zoom room (`ws-agung`) name → changed in mirror AND still changed after another `sync.Run` (the override-survives-sync proof); DELETE custom removes + cancels its booking; DELETE zoom room clears override so next `sync.Run` restores the Zoom name; all four verbs 401 without admin token (covered by matrix but assert one here on POST).
- [ ] **Step 2: Red. Step 3: Implement. Step 4: Green (full suite).**
- [ ] **Step 5: Commit** — `"Add rooms CRUD: custom rooms persist, Zoom rooms get sync-surviving overrides"`

---

### Task 6: Admin reservations CRUD, notification deletes, user rename

**Files:**
- Modify: `backend/internal/api/booking.go` (admin create/patch), `backend/internal/api/notify.go` (`remove(id)`, `clear()` on notifier + handlers), `backend/internal/api/users.go` (`patchUser`), `backend/internal/api/server.go` (routes, admin-wrapped)
- Test: extend `backend/internal/api/rooms_admin_test.go` sibling `admin_crud_test.go`

**Interfaces:**
- `POST /admin/reservations {workspace_id, start_time, end_time, user_email?}` → conflict-checked (reuse `conflictingReservation`), `BookedByUserID: "admin"`, `UserEmail: body.UserEmail`, source `app`.
- `PATCH /admin/reservations/{id} {start_time?, end_time?}` → 404 unknown, 403 non-app source, 409 conflict (excluding itself), else upsert.
- `DELETE /notifications/{id}` → 404 miss; `DELETE /notifications` → clear all.
- `PATCH /users/{id} {name}` → 422 empty, 404 unknown, updates DB (`UpdateUserName`).

- [ ] Steps: failing tests (conflict, zoom-403, edit moves window, notification delete/clear via seeding `s.notify.emit`, rename reflected in `GET /users`) → red → implement → green → commit `"Add admin reservation create/edit, notification deletes, user rename"`.

---

### Task 7: OpenAPI

**Files:** `backend/internal/api/openapi.yaml`

- [ ] Add `bearerAuth` JWT scheme note on `sessionToken` (rename description: "JWT issued by POST /auth/login (admin) or POST /auth/apple (user)"); add paths: `/auth/login`, `/rooms` POST, `/rooms/{workspace_id}` PATCH/DELETE, `/admin/reservations` POST, `/admin/reservations/{id}` PATCH, `/notifications` DELETE, `/notifications/{id}` DELETE, `/users/{id}` PATCH; mark admin-protected reads with `security`. Gate: server tests still green (docs are static). Commit `"Document JWT auth and admin CRUD in OpenAPI"`.

---

### Task 8: Admin UI — login + CRUD

**Files:**
- Create: `frontend/src/views/LoginView.vue`, `frontend/src/api/auth.ts`
- Modify: `frontend/src/api/client.ts` (all fetches → shared `authFetch` adding `Authorization`; 401 → clear token + redirect `/login`), `frontend/src/api/types.ts`, `frontend/src/router/index.ts` (login route + guard), `frontend/src/components/AppHeader.vue` (logout), `frontend/src/views/AdminView.vue` (new-booking form section), `frontend/src/components/admin/RoomsGrid.vue` (add/edit/delete), `frontend/src/components/admin/NotificationsList.vue` (dismiss/clear), `frontend/src/components/admin/UsersPanel.vue` (rename)

**Key pieces:**
- `auth.ts`: `getToken/setToken/clearToken` over `localStorage("qr_admin_jwt")`, `login(email, password)` → POST `/auth/login`, `logout()` → clear + router push.
- `client.ts`: `authFetch(url, init?)` wrapper; on 401 `clearToken(); location.assign('/login')`. New calls: `createRoom/patchRoom/deleteRoom`, `adminCreateReservation/adminPatchReservation`, `deleteNotification/clearNotifications`, `renameUser`.
- Router guard: `to.path !== '/login' && !getToken()` → `/login`.
- `LoginView.vue`: centered card, email+password inputs, error line, matches theme.css tokens (dark palette, `--f-body`); on success → `/`.
- AdminView: section 02 header gains a "New booking" inline form (room `<select>` from rooms, two `datetime-local` inputs, email input, Book button) + per-row Cancel (app-sourced only, existing `adminCancelReservation`) and Edit (inline window edit, app-sourced only).
- RoomsGrid: per-card ✎/🗑 (text buttons, no emojis — "Edit"/"Delete"/"Reset" for zoom rooms) + "Add room" row; NotificationsList: "×" per row + "Clear all"; UsersPanel: name cell becomes editable on "Rename".

- [ ] Implement → `npm run build` green → quick Playwright pass against `localhost` or straight on prod post-deploy (Task 9) → commit `"Admin UI: JWT login gate and CRUD for reservations, rooms, notifications, users"`.

---

### Task 9: Deploy + live verification

- [ ] Compose passthroughs (repo `backend/docker-compose.yml` + VPS copy): `JWT_SECRET: "${JWT_SECRET:-}"`, `ADMIN_EMAIL: "${ADMIN_EMAIL:-}"`, `ADMIN_PASSWORD: "${ADMIN_PASSWORD:-}"`. Commit repo side.
- [ ] VPS `.env`: `JWT_SECRET=$(openssl rand -hex 32)`, `ADMIN_EMAIL=admin@example.com`, `ADMIN_PASSWORD=SuperAdmin123!` (rotate later; values never in git).
- [ ] Build frontend → tar deploy → `docker compose -p backend up -d --build`.
- [ ] Verify: `POST /auth/login` good creds → token; bad → 401; `GET /users` without token → 401, with → 200; `GET /rooms` open → 200; `POST /presence` open; admin panel in browser: login → sections render → create/edit/delete a custom room → new booking → cancel → notification clear. DB intact: users/apns_tokens rows still present (`sqlite3` count). APNs still enabled in logs.
- [ ] Push `main`.

### Task 10: Wrap-up

- [ ] Work log section `### 2026-07-05 — Admin CRUD + JWT auth` (table of shipped items + the keep-data note).
- [ ] Short note to Rei on issue #8 (or new issue if cleaner): backend auth switched to JWT — his app needs **no update**, but everyone signed in must re-login once; pushes unaffected.

---

## Self-Review Notes

- **Spec coverage:** auth (T1–T3), matrix (T4), rooms (T5), reservations/notifications/users (T6), OpenAPI (T7), UI (T8), keep-data + deploy (T9), Rei comms (T10). Sessions table preserved (T3 deletes code only). ✓
- **Type consistency:** `requireUser/requireAdmin` (T3) used in T4–T6 route wraps; `signer` field name consistent; store methods of T2 consumed in T3/T5/T6 with matching signatures; `syncsvc.New(zc, st, db, locationID, log)` updated everywhere in T5. ✓
- **Placeholders:** T2/T5/T6 test steps name concrete scenarios instead of full listings — acceptable to the executor (same session, full context), all assertions enumerated. Code for every new public surface is specified. ✓
