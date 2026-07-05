# APNs Notification Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Push the backend's outbox notifications (grace reminders, no-show releases, room-freed) to iPhones via APNs, with device-token registration from the QuickRoom app.

**Architecture:** A small `internal/apns` client (ES256 provider JWT from a `.p8`, HTTP/2 POST per Apple's API) is fanned out to by a hook on the existing outbox `notifier.emit`. Device tokens live in a new SQLite table, registered by the app through a session-authenticated endpoint. Everything is env-configured and inert until the key from Rei's Apple account is installed.

**Tech Stack:** Go 1.26 (stdlib HTTP/2, `golang-jwt/v5` already a dep), SQLite (`modernc.org/sqlite`), Swift/SwiftUI on the app side.

**Spec:** `docs/superpowers/specs/2026-07-05-apns-notification-delivery-design.md`

## Global Constraints

- Backend work in `/Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend` (commits on `main`, this repo). iOS work in `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom` on branch `feature/apns-registration` → PR to `Reishandy/QuickRoom` referencing issue #8.
- **No config identifier values in git** (bundle id, team id, key id, `.p8`): env names + `${VAR:-}` passthroughs only; values go in the VPS's gitignored `.env`, the key file into the `/data` volume over ssh.
- NEVER add `Co-Authored-By`. Concise imperative commits, no emojis. Swift files: tabs, Rei's header style.
- Go tests: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./...` — must stay fully green.
- iOS build/test command (from the QuickRoom clone): `DEVELOPER_DIR=/Applications/Xcode.app xcodebuild -project QuickRoom.xcodeproj -scheme QuickRoom -destination 'platform=iOS Simulator,name=iPhone 17' test` (boot the sim first if flaky: `simctl boot "iPhone 17"` + `simctl bootstatus "iPhone 17"`).
- Deploy pipeline (memory: `project_deployment.md`): build the Vue frontend first (`cd frontend && npm run build`), then tar the `backend/` contents to `/root/roompulse` over ssh `pr-diriger-hetzner` (exclude ONLY `.env`, `docker-compose.yml`, `.git`, `*.db*`, `.DS_Store`, `._*`), rebuild with `docker compose -p backend up -d --build`.

---

### Task 1: `internal/apns` client

**Files:**
- Create: `backend/internal/apns/apns.go`
- Test: `backend/internal/apns/apns_test.go`

**Interfaces:**
- Produces (Task 4 consumes):
  - `func New(keyPEM []byte, keyID, teamID, topic, host string) (*Client, error)`
  - `func HostForEnv(env string) string` — `"production"` → `api.push.apple.com`, anything else → `api.sandbox.push.apple.com`
  - `func (c *Client) Push(ctx context.Context, deviceToken string, n Notification) error`
  - `type Notification struct { Title, Body, Type, WorkspaceID, ReservationID string }`
  - `var ErrUnregistered error` — device token is dead, delete it
  - Test seams: exported fields `c.BaseURL string`, `c.HTTPClient *http.Client`

- [ ] **Step 1: Write the failing tests**

`backend/internal/apns/apns_test.go`:

```go
package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestHostForEnv(t *testing.T) {
	if got := HostForEnv("production"); got != "api.push.apple.com" {
		t.Fatalf("production host = %q", got)
	}
	if got := HostForEnv("sandbox"); got != "api.sandbox.push.apple.com" {
		t.Fatalf("sandbox host = %q", got)
	}
	if got := HostForEnv(""); got != "api.sandbox.push.apple.com" {
		t.Fatalf("default host = %q", got)
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New([]byte("not a key"), "KEY1", "TEAM1", "com.example.app", "h"); err == nil {
		t.Fatal("expected error for garbage key")
	}
}

func TestPushSendsWellFormedRequest(t *testing.T) {
	var gotPath, gotAuth, gotTopic, gotPushType string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTopic = r.Header.Get("apns-topic")
		gotPushType = r.Header.Get("apns-push-type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c, err := New(testKeyPEM(t), "KEY1", "TEAM1", "com.example.app", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	err = c.Push(context.Background(), "devtok123", Notification{
		Title: "Are you coming?", Body: "Room frees in 3 min",
		Type: "grace_reminder", WorkspaceID: "ws-x", ReservationID: "res-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/3/device/devtok123" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotTopic != "com.example.app" || gotPushType != "alert" {
		t.Fatalf("headers topic=%q pushType=%q", gotTopic, gotPushType)
	}

	// Provider JWT: bearer, ES256, kid + iss claims.
	raw := strings.TrimPrefix(gotAuth, "bearer ")
	parsed, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("auth header not a JWT: %v (auth=%q)", err, gotAuth)
	}
	if parsed.Header["kid"] != "KEY1" || parsed.Header["alg"] != "ES256" {
		t.Fatalf("jwt header = %v", parsed.Header)
	}
	if iss, _ := parsed.Claims.(jwt.MapClaims)["iss"].(string); iss != "TEAM1" {
		t.Fatalf("iss = %q", iss)
	}

	aps := gotBody["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["title"] != "Are you coming?" || aps["sound"] != "default" {
		t.Fatalf("payload aps = %v", aps)
	}
	if gotBody["type"] != "grace_reminder" || gotBody["workspace_id"] != "ws-x" {
		t.Fatalf("custom keys = %v", gotBody)
	}
}

func TestPushReusesProviderToken(t *testing.T) {
	var tokens []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	_ = c.Push(context.Background(), "a", Notification{Title: "x"})
	_ = c.Push(context.Background(), "b", Notification{Title: "y"})
	if len(tokens) != 2 || tokens[0] != tokens[1] {
		t.Fatalf("expected cached provider token reuse, got %v", tokens)
	}
}

func TestPushUnregistered(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	err := c.Push(context.Background(), "dead", Notification{Title: "x"})
	if !errors.Is(err, ErrUnregistered) {
		t.Fatalf("expected ErrUnregistered, got %v", err)
	}
}

func TestPushOtherErrorCarriesReason(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"InvalidProviderToken"}`))
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	err := c.Push(context.Background(), "tok", Notification{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "InvalidProviderToken") {
		t.Fatalf("expected reason in error, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd backend && go test ./internal/apns/` → compile error (package doesn't exist yet is fine: create `apns.go` with just `package apns` if needed; expected FAIL on undefined `New`).

- [ ] **Step 3: Implement**

`backend/internal/apns/apns.go`:

```go
// Package apns pushes alert notifications to Apple Push Notification service
// using token-based (p8 / ES256) provider authentication. Kept intentionally
// small: one POST per device token, provider JWT cached ~50 minutes (Apple
// asks for a refresh every 20-60 min).
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrUnregistered means APNs reports the device token as dead (uninstalled
// app or invalidated token); the caller should delete it.
var ErrUnregistered = errors.New("apns: device token unregistered")

// Notification is one alert push. Type/WorkspaceID/ReservationID ride along
// as custom payload keys so the app can deep-link later.
type Notification struct {
	Title         string
	Body          string
	Type          string
	WorkspaceID   string
	ReservationID string
}

// HostForEnv maps the APNS_ENV config to Apple's host. Development-signed
// builds produce sandbox tokens, so sandbox is the default.
func HostForEnv(env string) string {
	if env == "production" {
		return "api.push.apple.com"
	}
	return "api.sandbox.push.apple.com"
}

type Client struct {
	// BaseURL and HTTPClient are exported for tests (httptest injection).
	BaseURL    string
	HTTPClient *http.Client

	topic  string
	keyID  string
	teamID string
	key    *ecdsa.PrivateKey

	mu       sync.Mutex
	jwt      string
	jwtUntil time.Time
}

// New parses the .p8 provider key and returns a ready client.
func New(keyPEM []byte, keyID, teamID, topic, host string) (*Client, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("apns: key file is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse key: %w", err)
	}
	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: key is not an EC key")
	}
	return &Client{
		BaseURL:    "https://" + host,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		topic:      topic,
		keyID:      keyID,
		teamID:     teamID,
		key:        ecKey,
	}, nil
}

// providerToken returns the cached ES256 provider JWT, minting a fresh one
// when it is older than ~50 minutes.
func (c *Client) providerToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.jwt != "" && now.Before(c.jwtUntil) {
		return c.jwt, nil
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.teamID,
		"iat": now.Unix(),
	})
	tok.Header["kid"] = c.keyID
	signed, err := tok.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("apns: sign provider token: %w", err)
	}
	c.jwt = signed
	c.jwtUntil = now.Add(50 * time.Minute)
	return signed, nil
}

// Push sends one alert to one device token.
func (c *Client) Push(ctx context.Context, deviceToken string, n Notification) error {
	provider, err := c.providerToken()
	if err != nil {
		return err
	}

	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]any{"title": n.Title, "body": n.Body},
			"sound": "default",
		},
	}
	if n.Type != "" {
		payload["type"] = n.Type
	}
	if n.WorkspaceID != "" {
		payload["workspace_id"] = n.WorkspaceID
	}
	if n.ReservationID != "" {
		payload["reservation_id"] = n.ReservationID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/3/device/"+deviceToken, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+provider)
	req.Header.Set("apns-topic", c.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("apns: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	var apnsErr struct {
		Reason string `json:"reason"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = json.Unmarshal(raw, &apnsErr)
	if resp.StatusCode == http.StatusGone || apnsErr.Reason == "BadDeviceToken" {
		return fmt.Errorf("%w (reason %s)", ErrUnregistered, apnsErr.Reason)
	}
	return fmt.Errorf("apns: status %d reason %s", resp.StatusCode, apnsErr.Reason)
}
```

- [ ] **Step 4: Run to verify green** — `cd backend && go test ./internal/apns/ -v` → all PASS. Also `go build ./...`.

- [ ] **Step 5: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon && git add backend/internal/apns backend/go.mod backend/go.sum && git commit -m "Add minimal APNs client with p8 token auth

Provider JWT cached ~50 min per Apple's 20-60 min guidance; 410 and
BadDeviceToken surface as ErrUnregistered so callers can prune tokens."
```

(`go.mod`/`go.sum` change because `golang-jwt` moves from indirect to direct.)

---

### Task 2: Device-token registry in SQLite (+ `UserByEmail`)

**Files:**
- Modify: `backend/internal/store/sqlite.go` (schema const + methods at end)
- Test: `backend/internal/store/sqlite_test.go` (append)

**Interfaces:**
- Consumes: existing `store.DB`, `domain.User`, `UpsertUser`.
- Produces (Tasks 3–4 consume):
  - `func (d *DB) SaveAPNSToken(token, userID string, at time.Time) error` (upsert; re-homes a token to a new user)
  - `func (d *DB) APNSTokensForUser(userID string) ([]string, error)`
  - `func (d *DB) AllAPNSTokens() ([]string, error)`
  - `func (d *DB) DeleteAPNSToken(token string) error`
  - `func (d *DB) UserByEmail(email string) (domain.User, bool, error)`

- [ ] **Step 1: Write the failing tests** (append to `sqlite_test.go`; mirror the file's existing `openTestDB`-style helper — check its actual name at the top of the file and reuse it):

```go
func TestAPNSTokens(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	mustUpsertUser(t, db, "u-1", "sub-1", "a@b.c")
	mustUpsertUser(t, db, "u-2", "sub-2", "x@y.z")

	if err := db.SaveAPNSToken("tok1", "u-1", now); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAPNSToken("tok2", "u-1", now); err != nil {
		t.Fatal(err)
	}

	got, err := db.APNSTokensForUser("u-1")
	if err != nil || len(got) != 2 {
		t.Fatalf("tokens for u-1 = %v err=%v", got, err)
	}

	// Same device signs into another account: token re-homes.
	if err := db.SaveAPNSToken("tok1", "u-2", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ = db.APNSTokensForUser("u-1")
	if len(got) != 1 || got[0] != "tok2" {
		t.Fatalf("after re-home, u-1 tokens = %v", got)
	}

	all, err := db.AllAPNSTokens()
	if err != nil || len(all) != 2 {
		t.Fatalf("all tokens = %v err=%v", all, err)
	}

	if err := db.DeleteAPNSToken("tok1"); err != nil {
		t.Fatal(err)
	}
	all, _ = db.AllAPNSTokens()
	if len(all) != 1 {
		t.Fatalf("after delete, all = %v", all)
	}
}

func TestUserByEmail(t *testing.T) {
	db := openTestDB(t)
	mustUpsertUser(t, db, "u-1", "sub-1", "a@b.c")
	u, ok, err := db.UserByEmail("a@b.c")
	if err != nil || !ok || u.UserID != "u-1" {
		t.Fatalf("UserByEmail = %+v ok=%v err=%v", u, ok, err)
	}
	if _, ok, _ = db.UserByEmail("missing@x.y"); ok {
		t.Fatal("expected miss for unknown email")
	}
}
```

Add a `mustUpsertUser` helper only if the test file doesn't already have an equivalent; match `domain.User` field names from `backend/internal/domain` (check: `UserID`, `AppleSub`, `Email`, `Name`, `CreatedAt`).

- [ ] **Step 2: Run to verify failure** — `go test ./internal/store/` → undefined methods.

- [ ] **Step 3: Implement.** Append to the `schema` const:

```sql
CREATE TABLE IF NOT EXISTS apns_tokens (
	token      TEXT PRIMARY KEY,  -- APNs device token (hex); PK so a device re-homes on account switch
	user_id    TEXT NOT NULL,
	updated_at INTEGER NOT NULL   -- unix seconds, server clock
);
```

Methods at the end of `sqlite.go`, following the file's error-wrapping style:

```go
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
	return scanUser(row)
}
```

If `UserByID` doesn't already use a shared `scanUser(row)` helper, extract one (both queries scan the identical column set) — refactor `UserByID`/`UserByAppleSub` to use it in the same commit.

- [ ] **Step 4: Run to verify green** — `go test ./internal/store/ -v` then `go test ./...`.

- [ ] **Step 5: Commit** — `git add backend/internal/store && git commit -m "Add APNs device-token registry and user-by-email lookup"`

---

### Task 3: `POST /devices/apns` registration endpoint + OpenAPI

**Files:**
- Create: `backend/internal/api/apns_register.go`
- Modify: `backend/internal/api/server.go` (route table, next to the other auth-wrapped routes at ~line 253)
- Modify: `backend/internal/api/openapi.yaml` (path next to `/devices`)
- Test: `backend/internal/api/apns_register_test.go`

**Interfaces:**
- Consumes: `s.authMiddleware`, `userFromContext(r)`, `s.db.SaveAPNSToken` (Task 2), `decodeBody`/`writeJSON`/`writeError` (existing helpers).
- Produces: the route; nothing else.

- [ ] **Step 1: Write the failing test.** Look at an existing auth-wrapped handler test in `booking_test.go` first and mirror its server/session setup helpers exactly (it already builds a test `Server` with a temp DB and mints a session). The test:

```go
func TestRegisterAPNSToken(t *testing.T) {
	s, token, user := newAuthedTestServer(t) // reuse/adapt booking_test.go's helper

	// No session -> 401
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/devices/apns", strings.NewReader(`{"token":"abc"}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth status = %d", rec.Code)
	}

	// Empty token -> 422
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/devices/apns", strings.NewReader(`{"token":""}`))
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty-token status = %d", rec.Code)
	}

	// Happy path -> 200, token persisted for the session user
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/devices/apns", strings.NewReader(`{"token":"devtok1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	got, err := s.db.APNSTokensForUser(user.UserID)
	if err != nil || len(got) != 1 || got[0] != "devtok1" {
		t.Fatalf("persisted tokens = %v err=%v", got, err)
	}
}
```

(Adapt helper names to what `booking_test.go` actually provides — do not invent a parallel setup.)

- [ ] **Step 2: Run to verify failure** — `go test ./internal/api/ -run TestRegisterAPNSToken` → 404/undefined.

- [ ] **Step 3: Implement.** `backend/internal/api/apns_register.go`:

```go
package api

import (
	"net/http"
	"time"
)

// postRegisterAPNSToken stores the caller's APNs device token so outbox
// notifications can be pushed to their phone. Session-scoped: the token is
// attached to whoever is signed in on the device.
func (s *Server) postRegisterAPNSToken(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "no session")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Token == "" {
		writeError(w, http.StatusUnprocessableEntity, "token required")
		return
	}
	if err := s.db.SaveAPNSToken(body.Token, user.UserID, time.Now()); err != nil {
		s.log.Error("save apns token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

Route in `server.go` beside the other auth-wrapped routes:

```go
	mux.HandleFunc("POST /devices/apns", s.authMiddleware(s.postRegisterAPNSToken))
```

OpenAPI, next to the `/devices` path:

```yaml
  /devices/apns:
    post:
      tags: [Devices]
      summary: Register the caller's APNs device token
      description: |
        The app calls this after registerForRemoteNotifications succeeds so the
        backend can push outbox notifications (grace reminders, no-show
        releases, room-freed) to the phone. The token re-homes to whichever
        account is signed in on the device.
      security: [{ sessionToken: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [token]
              properties:
                token: { type: string, description: Hex APNs device token }
      responses:
        "200": { $ref: '#/components/responses/Ok' }
        "401": { $ref: '#/components/responses/Unauthorized' }
        "422": { $ref: '#/components/responses/Unprocessable' }
```

(Verify the `Devices` tag and `Ok` response component exist in `openapi.yaml`; use the file's actual tag for `/devices` and its response refs.)

- [ ] **Step 4: Run to verify green** — `go test ./internal/api/ -run TestRegisterAPNSToken -v`, then `go test ./...`.

- [ ] **Step 5: Commit** — `git add backend/internal/api && git commit -m "Add session-scoped APNs device-token registration endpoint"`

---

### Task 4: Emit hook, fan-out, config, main wiring

**Files:**
- Modify: `backend/internal/api/notify.go` (notifier callback + fan-out)
- Modify: `backend/internal/api/server.go` (`ConfigureAPNS`)
- Modify: `backend/internal/config/config.go` (5 envs)
- Modify: `backend/cmd/quickroom/main.go` (construct client, wire)
- Test: `backend/internal/api/notify_test.go` (append)

**Interfaces:**
- Consumes: `apns.Notification`, `apns.ErrUnregistered` (Task 1), store methods (Task 2).
- Produces:
  - `type notificationPusher interface { Push(ctx context.Context, deviceToken string, n apns.Notification) error }` (in `api`)
  - `func (s *Server) ConfigureAPNS(p notificationPusher)` — sets `s.notify.onEmit`
  - Config fields: `APNSKeyFile, APNSKeyID, APNSTeamID, APNSTopic, APNSEnv string`

- [ ] **Step 1: Write the failing tests** (append to `notify_test.go`; reuse its existing server-construction helper):

```go
type fakePusher struct {
	mu    sync.Mutex
	calls []string // deviceToken
	fail  map[string]error
}

func (f *fakePusher) Push(_ context.Context, tok string, _ apns.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tok)
	if f.fail != nil {
		return f.fail[tok]
	}
	return nil
}

func (f *fakePusher) tokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestEmitPushesToRecipientTokens(t *testing.T) {
	s := newTestServerWithDB(t) // reuse the file's helper that has a live *store.DB
	mustUpsertUser(t, s.db, "u-1", "sub-1", "booker@x.y")
	_ = s.db.SaveAPNSToken("tokA", "u-1", time.Now())
	_ = s.db.SaveAPNSToken("tokB", "u-1", time.Now())

	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	// Recipient by email (bookerOf prefers email).
	s.notify.emit("k1", Notification{Type: "grace_reminder", Recipient: "booker@x.y", Title: "t", Body: "b"})
	waitFor(t, func() bool { return len(fp.tokens()) == 2 })

	// Dedup miss: same key emits nothing new, no extra pushes.
	s.notify.emit("k1", Notification{Type: "grace_reminder", Recipient: "booker@x.y", Title: "t", Body: "b"})
	time.Sleep(50 * time.Millisecond)
	if got := fp.tokens(); len(got) != 2 {
		t.Fatalf("dedup should not re-push, calls = %v", got)
	}
}

func TestEmitBroadcastPushesToAllTokens(t *testing.T) {
	s := newTestServerWithDB(t)
	mustUpsertUser(t, s.db, "u-1", "sub-1", "a@x.y")
	mustUpsertUser(t, s.db, "u-2", "sub-2", "b@x.y")
	_ = s.db.SaveAPNSToken("tokA", "u-1", time.Now())
	_ = s.db.SaveAPNSToken("tokB", "u-2", time.Now())
	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	s.notify.emit("", Notification{Type: "room_freed", Recipient: "", Title: "t", Body: "b"})
	waitFor(t, func() bool { return len(fp.tokens()) == 2 })
}

func TestEmitPrunesUnregisteredTokens(t *testing.T) {
	s := newTestServerWithDB(t)
	mustUpsertUser(t, s.db, "u-1", "sub-1", "a@x.y")
	_ = s.db.SaveAPNSToken("dead", "u-1", time.Now())
	fp := &fakePusher{fail: map[string]error{"dead": apns.ErrUnregistered}}
	s.ConfigureAPNS(fp)

	s.notify.emit("k", Notification{Recipient: "a@x.y", Title: "t"})
	waitFor(t, func() bool {
		left, _ := s.db.AllAPNSTokens()
		return len(left) == 0
	})
}

// waitFor polls briefly — pushes run on goroutines.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}
```

(If `notify_test.go` has no DB-backed server helper, adapt the one from `users_test.go`/`booking_test.go` — again, reuse, don't reinvent. `mustUpsertUser` comes from Task 2's test or is shared.)

- [ ] **Step 2: Run to verify failure** — `go test ./internal/api/ -run TestEmit` → undefined `ConfigureAPNS`.

- [ ] **Step 3: Implement.**

In `notify.go` — add the callback field, invoke it outside the lock, add fan-out:

```go
type notifier struct {
	mu     sync.Mutex
	max    int
	seq    int64
	list   []Notification
	sent   map[string]bool // dedup key -> already emitted
	onEmit func(Notification) // set when APNs is configured; called on fresh emits only
}
```

Rework `emit` so the callback runs after the lock is released (emit is called from sweep loops; the callback spawns goroutines):

```go
func (n *notifier) emit(key string, note Notification) bool {
	n.mu.Lock()
	if key != "" {
		if n.sent[key] {
			n.mu.Unlock()
			return false
		}
		n.sent[key] = true
	}
	n.seq++
	note.ID = n.seq
	n.list = append(n.list, note)
	if len(n.list) > n.max {
		n.list = n.list[len(n.list)-n.max:]
	}
	cb := n.onEmit
	n.mu.Unlock()
	if cb != nil {
		cb(note)
	}
	return true
}
```

Fan-out (same file, below the handler):

```go
// notificationPusher is what the fan-out needs from the APNs client;
// interface so tests can fake it.
type notificationPusher interface {
	Push(ctx context.Context, deviceToken string, n apns.Notification) error
}

// pushNotification delivers one freshly emitted outbox notification to the
// relevant device tokens. Recipient "" = broadcast (room_freed); otherwise
// the recipient is bookerOf() output — email when known, else a user id.
// Fire-and-forget: failures are logged, the outbox stays the source of truth.
func (s *Server) pushNotification(p notificationPusher, note Notification) {
	var tokens []string
	var err error
	if note.Recipient == "" {
		tokens, err = s.db.AllAPNSTokens()
	} else {
		user, ok, lookupErr := s.db.UserByID(note.Recipient)
		if lookupErr == nil && !ok {
			user, ok, lookupErr = s.db.UserByEmail(note.Recipient)
		}
		if lookupErr != nil {
			s.log.Error("apns recipient lookup", "recipient", note.Recipient, "err", lookupErr)
			return
		}
		if !ok {
			return // Zoom-sourced booker without an app account: normal, drop
		}
		tokens, err = s.db.APNSTokensForUser(user.UserID)
	}
	if err != nil {
		s.log.Error("apns token lookup", "err", err)
		return
	}

	payload := apns.Notification{
		Title: note.Title, Body: note.Body, Type: note.Type,
		WorkspaceID: note.WorkspaceID, ReservationID: note.ReservationID,
	}
	for _, tok := range tokens {
		go func(tok string) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err := p.Push(ctx, tok, payload)
			switch {
			case errors.Is(err, apns.ErrUnregistered):
				if delErr := s.db.DeleteAPNSToken(tok); delErr != nil {
					s.log.Error("prune apns token", "err", delErr)
				}
			case err != nil:
				s.log.Error("apns push", "type", note.Type, "err", err)
			}
		}(tok)
	}
}
```

In `server.go`:

```go
// ConfigureAPNS turns on push delivery for freshly emitted outbox
// notifications. Not calling it leaves the outbox poll-only.
func (s *Server) ConfigureAPNS(p notificationPusher) {
	s.notify.onEmit = func(n Notification) { go s.pushNotification(p, n) }
}
```

In `config.go` (fields near `AppleBundleID`, loads near its load):

```go
	// APNs push delivery (all five required to enable; see internal/apns).
	APNSKeyFile string
	APNSKeyID   string
	APNSTeamID  string
	APNSTopic   string
	APNSEnv     string
```

```go
	c.APNSKeyFile = os.Getenv("APNS_KEY_FILE")
	c.APNSKeyID = os.Getenv("APNS_KEY_ID")
	c.APNSTeamID = os.Getenv("APNS_TEAM_ID")
	c.APNSTopic = os.Getenv("APNS_TOPIC")
	c.APNSEnv = getenv("APNS_ENV", "sandbox")
```

In `main.go`, after `ConfigureBeaconsFile`:

```go
	if cfg.APNSKeyFile != "" && cfg.APNSKeyID != "" && cfg.APNSTeamID != "" && cfg.APNSTopic != "" {
		keyPEM, err := os.ReadFile(cfg.APNSKeyFile)
		if err != nil {
			log.Error("apns disabled: read key", "err", err)
		} else if pushClient, err := apns.New(keyPEM, cfg.APNSKeyID, cfg.APNSTeamID, cfg.APNSTopic, apns.HostForEnv(cfg.APNSEnv)); err != nil {
			log.Error("apns disabled: bad key", "err", err)
		} else {
			apiSrv.ConfigureAPNS(pushClient)
			log.Info("apns push enabled", "env", cfg.APNSEnv)
		}
	} else {
		log.Info("apns push disabled (APNS_* not configured)")
	}
```

(add `"quickroom/internal/apns"` and `"os"` to main.go imports as needed; `notify.go` gains `"context"`, `"errors"`, `"quickroom/internal/apns"`.)

- [ ] **Step 4: Run to verify green** — `go test ./... && go vet ./...`.

- [ ] **Step 5: Commit** — `git add backend && git commit -m "Push freshly emitted outbox notifications via APNs

Hook on notifier.emit fans out to the recipient's registered device
tokens (broadcasts go to every token); dead tokens are pruned on 410.
Enabled only when the APNS_* env vars and key file are present."`

---

### Task 5: Deploy (backend live, APNs dormant) + compose passthroughs

**Files:**
- Modify: `backend/docker-compose.yml` (this repo, value-free passthroughs)
- Modify: `/root/roompulse/docker-compose.yml` + VPS deploy (over ssh)

- [ ] **Step 1: Compose passthroughs (repo).** After the `APPLE_BUNDLE_ID` line in `backend/docker-compose.yml`:

```yaml
      # APNs push delivery (values live in the gitignored .env; the .p8 key
      # file goes into the /data volume). All unset = push disabled.
      APNS_KEY_FILE: "${APNS_KEY_FILE:-}"
      APNS_KEY_ID: "${APNS_KEY_ID:-}"
      APNS_TEAM_ID: "${APNS_TEAM_ID:-}"
      APNS_TOPIC: "${APNS_TOPIC:-}"
      APNS_ENV: "${APNS_ENV:-sandbox}"
```

Commit: `git add backend/docker-compose.yml && git commit -m "Pass APNS_* env through compose"`. Apply the same block to the VPS copy over ssh (in-place edit, it's excluded from deploy tars).

- [ ] **Step 2: Deploy.** Frontend build first, then tar + rebuild (exact commands from memory/Global Constraints). Verify:

```bash
ssh pr-diriger-hetzner 'docker logs roompulse 2>&1 | grep -i apns | tail -2'
```

Expected: `apns push disabled (APNS_* not configured)`. Also `curl -s https://rp.asadullokhn.uz/health/ready` → ok, and a garbage-token `POST /devices/apns` without session → 401.

- [ ] **Step 3: Push repo main** — `git push origin main` (history is clean of identifier values).

---

### Task 6: iOS registration PR (Rei's repo)

**Files (in `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom`, branch `feature/apns-registration`):**
- Create: `QuickRoom/Core/Network/PushRegistrar.swift`
- Modify: `QuickRoom/App/AppDelegate.swift` (token callbacks), `QuickRoom/App/QuickRoomApp.swift` (`@UIApplicationDelegateAdaptor` — the delegate is currently NOT wired into the SwiftUI lifecycle at all), `QuickRoom/Core/Services/Permissions/NotificationPermissionService.swift` (trigger registration once authorized), `QuickRoom/Core/Services/AuthService.swift` (flush pending token after sign-in), `QuickRoom/QuickRoom.entitlements` (`aps-environment`)

**Interfaces:**
- Consumes: `APIClient.shared.post`, `StatusResponse`, `AuthService` (all merged in PR #7).
- Produces: `PushRegistrar.shared` with `func requestRegistration()`, `func handleToken(_ tokenData: Data) async`, `func flushPendingToken() async`.

- [ ] **Step 1: Branch** — `git checkout main && git pull && git checkout -b feature/apns-registration`.

- [ ] **Step 2: PushRegistrar.** `QuickRoom/Core/Network/PushRegistrar.swift` (tabs, Rei-style header, `Created by Asadullokh Nurullaev on 05/07/26.`):

```swift
import Foundation
import UIKit

/// Registers this device for APNs and uploads the token to the backend so
/// outbox notifications (grace reminders, no-show releases, room-freed) get
/// pushed. Upload needs a signed-in session; a token that arrives before
/// sign-in is stashed and flushed after auth.
final class PushRegistrar {
	static let shared = PushRegistrar()

	private static let pendingTokenKey = "apns.pendingToken"

	private let client: APIClient

	init(client: APIClient = .shared) {
		self.client = client
	}

	/// Ask iOS for a device token. Safe to call repeatedly.
	func requestRegistration() {
		UIApplication.shared.registerForRemoteNotifications()
	}

	func handleToken(_ tokenData: Data) async {
		let token = tokenData.map { String(format: "%02x", $0) }.joined()
		await upload(token)
	}

	/// Retry a token that arrived before the user signed in.
	func flushPendingToken() async {
		guard let token = UserDefaults.standard.string(forKey: Self.pendingTokenKey) else { return }
		await upload(token)
	}

	private func upload(_ token: String) async {
		do {
			let _: StatusResponse = try await client.post("/devices/apns", body: ["token": token])
			UserDefaults.standard.removeObject(forKey: Self.pendingTokenKey)
		} catch {
			// Not signed in yet (401) or offline: keep it for a later flush.
			UserDefaults.standard.set(token, forKey: Self.pendingTokenKey)
			print("PushRegistrar: upload deferred: \(error)")
		}
	}
}
```

(`["token": token]` works because `APIClient.post` takes any `Encodable`; `Dictionary<String,String>` encodes as the right JSON and snake_case conversion doesn't alter a lowercase key.)

- [ ] **Step 3: AppDelegate callbacks** (keep his `didFinishLaunching` body):

```swift
	func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
		Task {
			await PushRegistrar.shared.handleToken(deviceToken)
		}
	}

	func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
		print("PushRegistrar: APNs registration failed: \(error)")
	}
```

- [ ] **Step 4: Wire the delegate + triggers.**
- `QuickRoomApp.swift`: add `@UIApplicationDelegateAdaptor(AppDelegate.self) var appDelegate` as the first property (without this the AppDelegate — including Rei's existing one — never runs).
- `NotificationPermissionService.checkStatus()`: at the end, when `isAuthorized` is true, call `PushRegistrar.shared.requestRegistration()` — covers both "granted just now" and "already granted at launch" (checkStatus runs from init and on foreground).
- `AuthService.completeSignIn`: after `currentUser = response.user`, add `await PushRegistrar.shared.flushPendingToken()`.
- Entitlements: add to the dict:

```xml
	<key>aps-environment</key>
	<string>development</string>
```

- [ ] **Step 5: Build + test** — full iOS test command → `** TEST SUCCEEDED **`. (Simulator gets no real APNs token, so runtime behavior there is just the failed-registration log — fine.)

- [ ] **Step 6: Commit + PR:**

```bash
git add QuickRoom && git commit -m "Register for APNs and upload the device token to the backend

Token uploads are session-scoped; a token that arrives before sign-in is
stashed and flushed after auth. Also wires AppDelegate into the SwiftUI
lifecycle via UIApplicationDelegateAdaptor - it previously never ran."
git push -u origin feature/apns-registration
gh pr create -R Reishandy/QuickRoom --base main --head feature/apns-registration
```

PR body: what it does, that it closes the app side of issue #8, that Rei must enable the **Push Notifications capability** (aps-environment entitlement included), and that pushes start flowing once his APNs key is installed server-side. Note he's free to close it in favor of his own implementation if he already started.

---

### Task 7: Issue #8 reply + work log

- [ ] **Step 1: Reply on the issue** (`gh issue comment 8 -R Reishandy/QuickRoom --body ...`), covering:
  - Confirming his read: correct — until today notifications stopped at the outbox; APNs delivery is now implemented server-side and deployed (dormant).
  - The app-side registration PR number from Task 6.
  - What we need from him, taking up his offer: the APNs auth key (`.p8`) + Key ID + Team ID from his developer account — **sent privately (Telegram/AirDrop), never committed or posted** — plus the Push Notifications capability on the target. Once received: key → `/data` volume, ids → `.env`, container restart, sandbox push lands on his phone.
  - No bundle id or team id values in the comment itself.
- [ ] **Step 2: Work log** — append a `### 2026-07-05 — APNs notification delivery` section to the Challenge Work Log (same table format), covering backend package/endpoint/hook, dormant deploy, iOS PR, and the pending-key handoff.

---

## Self-Review Notes

- **Spec coverage:** client (T1), registry + UserByEmail (T2), endpoint + OpenAPI (T3), hook/fan-out/config/wiring (T4), deploy + passthroughs (T5), iOS registration incl. delegate adaptor gap (T6), Rei handoff (T7). Out-of-scope items have no tasks. ✓
- **Placeholders:** none; test helpers explicitly say "reuse the file's existing helper" with adaptation instructions rather than inventing parallel setups — that's a directive, not a gap. ✓
- **Type consistency:** `notificationPusher.Push(ctx, string, apns.Notification)` matches `*apns.Client.Push`; `ConfigureAPNS(p notificationPusher)` used in tests and main; store method names identical across T2/T3/T4. `StatusResponse`/`APIClient.post` match PR #7's shipped signatures. ✓
