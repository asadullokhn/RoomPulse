# User Management + Admin Booking Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give admins the ability to list/delete user accounts, view any user's bookings, and cancel any app-sourced booking regardless of owner — both as HTTP APIs (for Rei's integration and general API surface) and as a new Admin UI section.

**Architecture:** Three new `store.DB` methods (`Users`, `DeleteUser`, `DeleteSessionsForUser`) back a new `users.go` handler file (`GET /users`, `GET /users/{id}/reservations`, `DELETE /users/{id}`). A new `adminCancelReservation` handler in `booking.go` adds `POST /admin/reservations/{id}/cancel` — the ownership-check-free counterpart to the existing self-service cancel. Frontend gets a `UsersPanel.vue` admin section.

**Tech Stack:** Go 1.26 (existing stack), Vue 3 + TypeScript (existing frontend stack). No new dependencies.

## Global Constraints

- Every new admin endpoint is unauthenticated, matching every existing admin endpoint (beacons CRUD, reservations list, notifications) — this codebase's admin surface has no auth model by design.
- Admin cancel is scoped to app-sourced (`Source == "app"`) bookings only — Zoom stays authoritative for Zoom-sourced ones (403 if attempted).
- `DELETE /users/{id}` cascades: cancels the user's open (`booked`) app-sourced reservations and revokes all their sessions, best-effort (logged, not fatal) on the secondary side effects — the user row deletion is what must succeed for a 200.
- `go vet ./...` and `go test ./...` must stay green after every task; `npm run build` must stay clean.

---

## File Structure

```
backend/internal/store/sqlite.go       — modify: add Users, DeleteUser, DeleteSessionsForUser
backend/internal/store/sqlite_test.go  — modify: add tests for the 3 new methods
backend/internal/api/users.go          — new: listUsers, userReservations, deleteUser
backend/internal/api/users_test.go     — new
backend/internal/api/booking.go        — modify: add adminCancelReservation
backend/internal/api/booking_test.go   — modify: add admin-cancel tests
backend/internal/api/server.go         — modify: register 4 new routes
backend/internal/api/openapi.yaml      — modify: document the 4 new endpoints + cancelled status + Source/BookedByUserID fields
frontend/src/api/types.ts              — modify: add User type, Source/BookedByUserID to Reservation, "cancelled" to ReservationStatus
frontend/src/api/client.ts             — modify: add getUsers, getUserReservations, deleteUser, adminCancelReservation
frontend/src/components/admin/UsersPanel.vue — new
frontend/src/views/AdminView.vue       — modify: add Users section
```

---

### Task 1: `store.DB` — `Users`, `DeleteUser`, `DeleteSessionsForUser`

**Files:**
- Modify: `backend/internal/store/sqlite.go`
- Modify: `backend/internal/store/sqlite_test.go`

**Interfaces:**
- Consumes: `scanUser(row interface{ Scan(...any) error }) (domain.User, error)` (existing helper, already satisfied by both `*sql.Row` and `*sql.Rows`).
- Produces: `func (d *DB) Users() ([]domain.User, error)`, `func (d *DB) DeleteUser(userID string) error`, `func (d *DB) DeleteSessionsForUser(userID string) error`.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/store/sqlite_test.go`:

```go
func TestUsersListAndDelete(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().Truncate(time.Second)
	db.UpsertUser(domain.User{UserID: "usr_a", AppleSub: "sub-a", Email: "a@example.com", Name: "A", CreatedAt: now})
	db.UpsertUser(domain.User{UserID: "usr_b", AppleSub: "sub-b", Email: "b@example.com", Name: "B", CreatedAt: now})

	users, err := db.Users()
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("Users() returned %d, want 2", len(users))
	}

	if err := db.CreateSession("hash-a", "usr_a", now, now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.DeleteSessionsForUser("usr_a"); err != nil {
		t.Fatalf("DeleteSessionsForUser: %v", err)
	}
	if _, ok, err := db.SessionUserID("hash-a", now); err != nil || ok {
		t.Fatalf("session should be gone after DeleteSessionsForUser: ok=%v err=%v", ok, err)
	}

	if err := db.DeleteUser("usr_a"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, ok, err := db.UserByID("usr_a"); err != nil || ok {
		t.Fatalf("user should be gone after DeleteUser: ok=%v err=%v", ok, err)
	}
	users, _ = db.Users()
	if len(users) != 1 {
		t.Fatalf("Users() after delete returned %d, want 1", len(users))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/... -run TestUsersListAndDelete -v`
Expected: FAIL to compile — `db.Users`, `db.DeleteSessionsForUser`, `db.DeleteUser` undefined.

- [ ] **Step 3: Add the methods to `backend/internal/store/sqlite.go`**

Append after `UserByID` (before `CreateSession`):

```go
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
```

Append after `DeleteSession`:

```go
// DeleteSessionsForUser revokes every session belonging to a user (e.g. on
// account deletion) — unlike DeleteSession, which revokes one session by
// token hash for a single-device logout.
func (d *DB) DeleteSessionsForUser(userID string) error {
	_, err := d.sql.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/... -run TestUsersListAndDelete -v`
Expected: PASS.

- [ ] **Step 5: Run the full store package**

Run: `cd backend && go test ./internal/store/...`
Expected: PASS, no regressions.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go
git commit -m "Add store.DB.Users, DeleteUser, DeleteSessionsForUser"
```

---

### Task 2: `GET /users`, `GET /users/{id}/reservations`, `DELETE /users/{id}`

**Files:**
- Create: `backend/internal/api/users.go`
- Test: `backend/internal/api/users_test.go`
- Modify: `backend/internal/api/server.go`

**Interfaces:**
- Consumes: `store.DB.Users/DeleteUser/DeleteSessionsForUser` (Task 1); `s.upsertReservation` (existing); `s.store.Reservations` (existing).
- Produces: routes `GET /users`, `GET /users/{id}/reservations`, `DELETE /users/{id}`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/users_test.go`:

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

func TestListUsersAndUserReservations(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-list-1", "lister@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token, "name": "Lister"})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID string `json:"user_id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(authRec.Body.Bytes(), &authResp)

	listReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /users status = %d, want 200", listRec.Code)
	}
	var listResp struct {
		Users []struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
		} `json:"users"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	found := false
	for _, u := range listResp.Users {
		if u.UserID == authResp.User.UserID && u.Email == "lister@example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("GET /users = %s, want to find user %s", listRec.Body.String(), authResp.User.UserID)
	}

	start := time.Now().Add(96 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-mengwi", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create reservation status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	resReq := httptest.NewRequest(http.MethodGet, "/users/"+authResp.User.UserID+"/reservations", nil)
	resRec := httptest.NewRecorder()
	h.ServeHTTP(resRec, resReq)
	if resRec.Code != http.StatusOK {
		t.Fatalf("GET /users/{id}/reservations status = %d, want 200", resRec.Code)
	}
	var resResp struct {
		Reservations []struct {
			WorkspaceID string `json:"zoom_workspace_id"`
		} `json:"reservations"`
	}
	_ = json.Unmarshal(resRec.Body.Bytes(), &resResp)
	if len(resResp.Reservations) != 1 || resResp.Reservations[0].WorkspaceID != "ws-mengwi" {
		t.Fatalf("user reservations = %s, want 1 booking for ws-mengwi", resRec.Body.String())
	}
}

func TestUserReservationsUnknownUser404s(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/users/usr_does_not_exist/reservations", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteUserCascades(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-del-1", "deleteme@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID string `json:"user_id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(authRec.Body.Bytes(), &authResp)

	start := time.Now().Add(120 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-sanur", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	var created struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	delReq := httptest.NewRequest(http.MethodDelete, "/users/"+authResp.User.UserID, nil)
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE /users/{id} status = %d, want 200", delRec.Code)
	}

	mineReq := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	mineReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	mineRec := httptest.NewRecorder()
	h.ServeHTTP(mineRec, mineReq)
	if mineRec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /reservations/mine after user deletion: status = %d, want 401", mineRec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/reservations", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	var listResp struct {
		Reservations []struct {
			ReservationID string `json:"reservation_id"`
			Status        string `json:"status"`
		} `json:"reservations"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	foundCancelled := false
	for _, r := range listResp.Reservations {
		if r.ReservationID == created.ReservationID && r.Status == "cancelled" {
			foundCancelled = true
		}
	}
	if !foundCancelled {
		t.Fatalf("reservation %s not found cancelled after user deletion: %s", created.ReservationID, listRec.Body.String())
	}

	delReq2 := httptest.NewRequest(http.MethodDelete, "/users/"+authResp.User.UserID, nil)
	delRec2 := httptest.NewRecorder()
	h.ServeHTTP(delRec2, delReq2)
	if delRec2.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want 404", delRec2.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run 'TestListUsersAndUserReservations|TestUserReservationsUnknownUser404s|TestDeleteUserCascades' -v`
Expected: FAIL — 404s across the board (no `/users*` routes registered yet).

- [ ] **Step 3: Create `backend/internal/api/users.go`**

```go
package api

import (
	"net/http"

	"quickroom/internal/domain"
)

// listUsers returns every app account.
func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.db.Users()
	if err != nil {
		s.log.Error("list users", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// userReservations returns one user's bookings (any status/source).
func (s *Server) userReservations(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, ok, err := s.db.UserByID(userID); err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	out := []domain.Reservation{}
	for _, res := range s.store.Reservations() {
		if res.BookedByUserID == userID {
			out = append(out, res)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reservations": out})
}

// deleteUser removes an account: cancels their open app-sourced bookings
// (a removed account can't be left holding a room), revokes every session
// (forces logout everywhere), then deletes the user row. The cancellation
// and session revocation are best-effort — logged, not fatal — but the
// user row deletion itself must succeed for a 200.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, ok, err := s.db.UserByID(userID); err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	for _, res := range s.store.Reservations() {
		if res.Source != "app" || res.BookedByUserID != userID {
			continue
		}
		if res.Status != domain.StatusBooked {
			continue
		}
		res.Status = domain.StatusCancelled
		s.upsertReservation(res)
	}

	if err := s.db.DeleteSessionsForUser(userID); err != nil {
		s.log.Warn("delete sessions for user", "user", userID, "err", err)
	}
	if err := s.db.DeleteUser(userID); err != nil {
		s.log.Error("delete user", "user", userID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Register the routes in `backend/internal/api/server.go`**

Add next to the existing `GET /reservations/mine` / booking routes:

```go
	mux.HandleFunc("GET /users", s.listUsers)
	mux.HandleFunc("GET /users/{id}/reservations", s.userReservations)
	mux.HandleFunc("DELETE /users/{id}", s.deleteUser)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/api/... -run 'TestListUsersAndUserReservations|TestUserReservationsUnknownUser404s|TestDeleteUserCascades' -v`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/users.go backend/internal/api/users_test.go backend/internal/api/server.go
git commit -m "Add GET /users, GET /users/{id}/reservations, DELETE /users/{id}"
```

---

### Task 3: `POST /admin/reservations/{id}/cancel`

**Files:**
- Modify: `backend/internal/api/booking.go`
- Modify: `backend/internal/api/booking_test.go`
- Modify: `backend/internal/api/server.go`

**Interfaces:**
- Consumes: `s.store.Reservation`, `s.upsertReservation` (existing).
- Produces: route `POST /admin/reservations/{id}/cancel`.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/api/booking_test.go`:

```go
func TestAdminCancelAnyReservation(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-admin-cancel", "owner@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
	}
	_ = json.Unmarshal(authRec.Body.Bytes(), &authResp)

	start := time.Now().Add(144 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-nusadua", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	var created struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	// Admin cancel — no Authorization header at all, unlike the self-service path.
	cancelReq := httptest.NewRequest(http.MethodPost, "/admin/reservations/"+created.ReservationID+"/cancel", nil)
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("admin cancel status = %d, body = %s, want 200", cancelRec.Code, cancelRec.Body.String())
	}
}

func TestAdminCancelZoomSourcedForbidden(t *testing.T) {
	h := newTestHandler(t)
	// res-petang is the mock seed's zoom-sourced reservation (see newTestHandler's comment).
	req := httptest.NewRequest(http.MethodPost, "/admin/reservations/res-petang/cancel", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run 'TestAdminCancelAnyReservation|TestAdminCancelZoomSourcedForbidden' -v`
Expected: FAIL — 404 (no `/admin/reservations/{id}/cancel` route yet).

- [ ] **Step 3: Add `adminCancelReservation` to `backend/internal/api/booking.go`**

Append at the end of the file:

```go
// adminCancelReservation cancels any app-sourced booking regardless of
// owner — the admin-facing counterpart to cancelReservation, which is
// scoped to the caller's own booking via session. Unauthenticated, like
// every other admin endpoint in this codebase.
func (s *Server) adminCancelReservation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if res.Source != "app" {
		writeError(w, http.StatusForbidden, "not cancellable this way")
		return
	}
	res.Status = domain.StatusCancelled
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}
```

- [ ] **Step 4: Register the route in `backend/internal/api/server.go`**

Add next to the `POST /reservations/{id}/cancel` self-service route:

```go
	mux.HandleFunc("POST /admin/reservations/{id}/cancel", s.adminCancelReservation)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/api/... -v 2>&1 | tail -60`
Expected: every test in the package passes, including the 2 new ones.

- [ ] **Step 6: Run the full suite**

Run: `cd backend && go vet ./... && go test ./...`
Expected: all packages green.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/booking.go backend/internal/api/booking_test.go backend/internal/api/server.go
git commit -m "Add POST /admin/reservations/{id}/cancel — cancel any app-sourced booking"
```

---

### Task 4: OpenAPI docs

**Files:**
- Modify: `backend/internal/api/openapi.yaml`

- [ ] **Step 1: Update the `Reservation` schema and add a `User` schema**

The `Reservation` schema currently lacks `source`/`booked_by_user_id` and its `status` enum lacks `cancelled` (added by PR #12 but never documented). Find the existing `Reservation:` schema and replace it:

```yaml
    Reservation:
      type: object
      properties:
        reservation_id: { type: string }
        room_id: { type: string }
        zoom_workspace_id: { type: string }
        user_id: { type: string }
        user_email: { type: string }
        start_time: { type: string, format: date-time }
        end_time: { type: string, format: date-time }
        status: { type: string, enum: [booked, no_show, released, cancelled] }
        check_in_status: { type: string, enum: [not_checked_in, checked_in, checked_out] }
        source: { type: string, enum: [zoom, app] }
        booked_by_user_id: { type: string }
    User:
      type: object
      properties:
        user_id: { type: string }
        email: { type: string }
        name: { type: string }
        created_at: { type: string, format: date-time }
```

(If a `User` schema already exists from PR #12's `/auth/apple` docs, merge into it rather than duplicating — check first with `grep -n "    User:" backend/internal/api/openapi.yaml`.)

- [ ] **Step 2: Add the new paths**

Add near the existing `/reservations/*` entries:

```yaml
  /users:
    get:
      tags: [Auth]
      summary: List all app accounts
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  users: { type: array, items: { $ref: '#/components/schemas/User' } }
  /users/{id}/reservations:
    get:
      tags: [Auth]
      summary: A specific user's bookings
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  reservations: { type: array, items: { $ref: '#/components/schemas/Reservation' } }
        "404": { $ref: '#/components/responses/NotFound' }
  /users/{id}:
    delete:
      tags: [Auth]
      summary: Delete a user account
      description: Cancels their open app-sourced bookings and revokes all sessions first.
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200": { $ref: '#/components/responses/Ok' }
        "404": { $ref: '#/components/responses/NotFound' }
  /admin/reservations/{id}/cancel:
    post:
      tags: [Reservations]
      summary: Cancel any app-sourced booking (admin)
      description: Unlike POST /reservations/{id}/cancel, this has no ownership check and no session requirement.
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200": { description: Cancelled, content: { application/json: { schema: { $ref: '#/components/schemas/Reservation' } } } }
        "403": { description: Not app-sourced (Zoom-synced reservations aren't cancellable this way), content: { application/json: { schema: { $ref: '#/components/schemas/Error' } } } }
        "404": { $ref: '#/components/responses/NotFound' }
```

- [ ] **Step 3: Validate YAML**

Run: `cd backend && python3 -c "import yaml; yaml.safe_load(open('internal/api/openapi.yaml')); print('valid')"`
Expected: `valid`.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/openapi.yaml
git commit -m "Document /users*, POST /admin/reservations/{id}/cancel, Reservation.source/booked_by_user_id"
```

---

### Task 5: `UsersPanel.vue` — Admin UI

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/components/admin/UsersPanel.vue`
- Modify: `frontend/src/views/AdminView.vue`

**Interfaces:**
- Consumes: none new beyond the backend endpoints from Tasks 2-3.
- Produces: `User` type in `types.ts`; `getUsers`, `getUserReservations`, `deleteUser`, `adminCancelReservation` in `client.ts`.

- [ ] **Step 1: Update `frontend/src/api/types.ts`**

Add `"cancelled"` to `ReservationStatus`, and `source`/`booked_by_user_id` to `Reservation`:

```ts
export type ReservationStatus = 'booked' | 'no_show' | 'released' | 'cancelled'
```

```ts
export interface Reservation {
  reservation_id: string
  room_id: string
  zoom_workspace_id: string
  user_id: string
  user_email?: string
  start_time: string // RFC3339
  end_time: string
  status: ReservationStatus
  check_in_status: CheckInStatus
  source: 'zoom' | 'app'
  booked_by_user_id?: string
}
```

Add a new `User` interface (after `Reservation`):

```ts
export interface User {
  user_id: string
  email?: string
  name?: string
  created_at: string
}
```

- [ ] **Step 2: Add to `frontend/src/api/client.ts`**

```ts
export const getUsers = () => getJSON<{ users: User[] }>('/users').then(d => d.users ?? [])
export const getUserReservations = (userId: string) =>
  getJSON<{ reservations: Reservation[] }>(`/users/${encodeURIComponent(userId)}/reservations`).then(d => d.reservations ?? [])
export const deleteUser = (userId: string) =>
  fetch(`/users/${encodeURIComponent(userId)}`, { method: 'DELETE' }).then(r => { if (!r.ok) throw new Error(r.statusText) })
export const adminCancelReservation = (id: string) =>
  fetch(`/admin/reservations/${encodeURIComponent(id)}/cancel`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error(r.statusText) })
```

Add `User` to the existing `import type { ... } from './types'` line at the top of the file.

- [ ] **Step 3: Write `frontend/src/components/admin/UsersPanel.vue`**

```vue
<script setup lang="ts">
import { ref } from 'vue'
import type { User, Reservation } from '@/api/types'
import { getUserReservations, deleteUser, adminCancelReservation } from '@/api/client'

defineProps<{ users: User[] }>()
const emit = defineEmits<{ changed: [] }>()

const expandedId = ref<string | null>(null)
const expandedReservations = ref<Reservation[]>([])
const busy = ref(false)
const error = ref('')

async function toggleExpand(userId: string) {
  if (expandedId.value === userId) {
    expandedId.value = null
    return
  }
  expandedId.value = userId
  error.value = ''
  try {
    expandedReservations.value = await getUserReservations(userId)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load bookings'
  }
}

async function cancelBooking(reservationId: string, userId: string) {
  busy.value = true
  error.value = ''
  try {
    await adminCancelReservation(reservationId)
    expandedReservations.value = await getUserReservations(userId)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'cancel failed'
  } finally {
    busy.value = false
  }
}

async function removeUser(userId: string) {
  if (!confirm('Delete this user? Their open bookings will be cancelled and all sessions revoked.')) return
  busy.value = true
  error.value = ''
  try {
    await deleteUser(userId)
    if (expandedId.value === userId) expandedId.value = null
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'delete failed'
  } finally {
    busy.value = false
  }
}

function fmtTime(s?: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
function fmtDate(s?: string) { return s ? new Date(s).toLocaleDateString() : '—' }
</script>

<template>
  <div class="card">
    <div class="scroll">
      <table>
        <thead><tr><th>Email</th><th>Name</th><th>Joined</th><th></th></tr></thead>
        <tbody>
          <template v-for="u in users" :key="u.user_id">
            <tr>
              <td>{{ u.email || '—' }}</td>
              <td>{{ u.name || '—' }}</td>
              <td class="mono">{{ fmtDate(u.created_at) }}</td>
              <td class="actions">
                <button class="btn-ghost" @click="toggleExpand(u.user_id)">{{ expandedId === u.user_id ? 'Hide bookings' : 'View bookings' }}</button>
                <button class="btn-ghost" :disabled="busy" @click="removeUser(u.user_id)">Delete</button>
              </td>
            </tr>
            <tr v-if="expandedId === u.user_id" class="expand-row">
              <td colspan="4">
                <div v-if="!expandedReservations.length" class="empty">No bookings.</div>
                <table v-else class="nested">
                  <thead><tr><th>Room</th><th>Window</th><th>Status</th><th></th></tr></thead>
                  <tbody>
                    <tr v-for="r in expandedReservations" :key="r.reservation_id">
                      <td class="mono id">{{ r.zoom_workspace_id }}</td>
                      <td class="mono">{{ fmtTime(r.start_time) }}–{{ fmtTime(r.end_time) }}</td>
                      <td>{{ r.status }}</td>
                      <td>
                        <button v-if="r.source === 'app' && r.status === 'booked'" class="btn-ghost" :disabled="busy" @click="cancelBooking(r.reservation_id, u.user_id)">Cancel</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
          <tr v-if="!users.length"><td colspan="4" class="empty">No users yet.</td></tr>
        </tbody>
      </table>
    </div>
    <div v-if="error" class="err">{{ error }}</div>
  </div>
</template>

<style scoped>
.actions { display: flex; gap: 6px; white-space: nowrap; }
button { font-family: var(--f-body); font-size: 12px; font-weight: 500; cursor: pointer;
  border-radius: 8px; padding: 6px 11px; border: 1px solid transparent; }
.btn-ghost { background: transparent; color: var(--text); border-color: var(--line); }
.btn-ghost:hover { border-color: var(--accent); }
.btn-ghost:disabled { opacity: .5; cursor: default; }
.expand-row td { padding: 12px 18px; background: rgba(150,170,220,.03); }
.nested { min-width: 0; }
.nested th, .nested td { padding: 8px 12px; font-size: 12.5px; }
.err { padding: 10px 16px; color: var(--danger); font-size: 12.5px; border-top: 1px solid var(--line-soft); }
</style>
```

- [ ] **Step 4: Wire `UsersPanel` into `frontend/src/views/AdminView.vue`**

Add the import:
```ts
import UsersPanel from '@/components/admin/UsersPanel.vue'
import { getUsers } from '@/api/client'
import type { User } from '@/api/types'
```

Add a ref next to `beacons`:
```ts
const users = ref<User[]>([])
```

Extend `refresh()`'s `Promise.all` to include `getUsers()` and assign it — show the full updated function:

```ts
async function refresh() {
  try {
    const [u, res, r, occ, col, over, notes, beac, usrs] = await Promise.all([
      getUtilization(), getReservations(), getRooms(), getOccupancy(), getCollisions(), getOverstays(), getNotifications(30), getBeacons(), getUsers(),
    ])
    util.value = u; reservations.value = res; rooms.value = r; occupancy.value = occ
    collisions.value = col; overstays.value = over; notifications.value = notes; beacons.value = beac; users.value = usrs
    markUp(); loaded.value = true
  } catch {
    markDown()
  }
}
```

Add a new section after the Beacons section:

```html
<section class="block">
  <div class="eyebrow"><span class="n">06</span> Users <span class="aside">{{ users.length }} accounts</span></div>
  <UsersPanel :users="users" @changed="refresh" />
</section>
```

- [ ] **Step 5: Build and typecheck**

Run: `cd frontend && npm run build`
Expected: exits 0.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts frontend/src/components/admin/UsersPanel.vue frontend/src/views/AdminView.vue
git commit -m "Add UsersPanel: list/view-bookings/delete users, cancel any booking from the Admin UI"
```

---

### Task 6: Full verification pass

- [ ] **Step 1: Backend tests + vet**

Run: `cd backend && go vet ./... && go test ./... -v 2>&1 | tail -100`
Expected: all packages pass, including every new test from Tasks 1-3.

- [ ] **Step 2: Frontend build**

Run: `cd frontend && npm run build`
Expected: exits 0.

- [ ] **Step 3: Docker build**

Run: `cd backend && docker build -t quickroom-users-test .`
Expected: succeeds.

- [ ] **Step 4: Manual in-browser verification**

Run the backend (`DB_PATH=/tmp/quickroom-users-check.db APPLE_BUNDLE_ID=test.bundle.id go run ./cmd/quickroom`) and frontend dev server. Since a real Apple token can't be minted from the shell, confirm via curl that `GET /users` returns `{"users":[]}` on a fresh DB, and that the Admin UI's Users section renders "No users yet." without errors. Confirm `POST /admin/reservations/{id}/cancel` against `res-petang` (mock zoom-sourced) returns 403.

- [ ] **Step 5: Clean up**

Remove test containers/images and temp files.
