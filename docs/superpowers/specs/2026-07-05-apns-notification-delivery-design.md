# APNs Delivery for Outbox Notifications — Design

**Date:** 2026-07-05
**Trigger:** Rei's question on Reishandy/QuickRoom#8 — "the APN implementation hasn't been
done yet correct? currently it is only in the notification outbox." Correct: grace
reminders, no-show releases, and room-freed events stop at the in-memory outbox
(`GET /notifications`). This feature pushes them to phones.
**Repos:** backend + compose in this repo; a small registration PR to `Reishandy/QuickRoom`.

## Decision

Direct APNs from the Go backend using token-based (p8/ES256) auth. No new dependencies:
`golang-jwt/v5` is already used by Sign in with Apple verification, and Go's stdlib
`http.Client` negotiates HTTP/2 over TLS. Alternatives rejected: app-side outbox polling
(no background delivery; Rei asked for APN), third-party push services (external
dependency for ~200 lines).

## Components (backend)

### 1. `internal/apns` — the push client

- `New(keyPEM []byte, keyID, teamID, topic, host string) (*Client, error)` — parses the
  `.p8` PKCS#8 EC key once; errors on bad key.
- Provider JWT: ES256, claims `iss=<team id>`, `iat=now`, header `kid=<key id>`.
  Cached and reused for 50 minutes (Apple wants 20–60 min), regenerated under lock.
- `Push(ctx, deviceToken string, n Notification) error` —
  `POST https://<host>/3/device/<token>`, headers `authorization: bearer <jwt>`,
  `apns-topic: <topic>`, `apns-push-type: alert`, `apns-priority: 10`.
  Payload:
  ```json
  {"aps": {"alert": {"title": "...", "body": "..."}, "sound": "default"},
   "type": "grace_reminder", "workspace_id": "ws-...", "reservation_id": "res-..."}
  ```
  (custom keys let the app deep-link later; empty ones omitted)
- Typed sentinel `ErrUnregistered` for HTTP 410 (and 400 `BadDeviceToken`) so the caller
  prunes dead tokens. Other non-200s return an error carrying APNs' `reason` string.
- Host is injected (config `sandbox` → `api.sandbox.push.apple.com`, `production` →
  `api.push.apple.com`) and overridable in tests via `httptest` URL.

### 2. Device-token registry (SQLite)

- New table: `apns_tokens (token TEXT PRIMARY KEY, user_id TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL)` — token is the key so a device that switches accounts
  re-homes to the new user on upsert.
- Store methods: `SaveAPNSToken(token, userID string, at time.Time)`,
  `APNSTokensForUser(userID) ([]string, error)`, `AllAPNSTokens() ([]string, error)`,
  `DeleteAPNSToken(token string)`, plus `UserByEmail(email string)` (needed because
  notification recipients are email-or-user-id).
- Endpoint: `POST /devices/apns` `{token}` — bearer-session authenticated (same
  `bearerToken`/`sessionUser` pattern as booking). 401 without session, 422 on empty
  token, 200 `{status:"ok"}`. Documented in OpenAPI under the existing Auth/session
  scheme.

### 3. Delivery hook

- `notifier` gains `onEmit func(Notification)`, invoked (when set) only when `emit`
  actually appends (dedup miss). The server sets it iff APNs is configured.
- Fan-out per notification, in a goroutine (emit is called from sweep loops — never
  block them):
  - `Recipient == ""` (room_freed broadcasts) → every registered token.
  - Otherwise resolve recipient → user via `UserByID`, falling back to `UserByEmail`
    (recipients are `bookerOf()` = email if known, else user id) → that user's tokens.
    No user or no tokens → log at debug, drop (Zoom-sourced bookers without app
    accounts are normal).
- `ErrUnregistered` → `DeleteAPNSToken`; other errors logged, no retry queue (the
  outbox itself remains the source of truth; a missed push is visible in-app).

### 4. Config & deploy

- Env: `APNS_KEY_FILE` (path to `.p8`), `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_TOPIC`
  (the app bundle id), `APNS_ENV` (`sandbox` default — dev-signed builds produce
  sandbox tokens; `production` for TestFlight/App Store).
- All values live in the VPS's gitignored `.env`; the key file goes into the `/data`
  volume over ssh, never into git. Compose (repo + VPS) gets value-free passthroughs
  only, per the no-config-ids-in-git rule.
- Any variable missing → APNs disabled, one startup log line, everything else runs
  unchanged. The backend deploys ahead of the key without behavior change.

## Component (iOS — PR to Rei's repo)

- Request `registerForRemoteNotifications` once notification permission is granted
  (his `NotificationPermissionService` flow already asks).
- `AppDelegate.didRegisterForRemoteNotificationsWithDeviceToken` → hex-encode →
  `POST /devices/apns` through the existing `APIClient` (bearer injected). If the user
  isn't signed in yet, stash the token in UserDefaults and retry after
  `AuthService.completeSignIn` succeeds and on next launch.
- Add `aps-environment: development` to the entitlements (pairs with the Push
  Notifications capability Rei enables on his paid team).
- If Rei is already building this himself ("should be done tomorrow"), the PR is his
  to merge or close as reference — coordinated on issue #8.

## What we need from Rei (issue #8 reply)

- APNs auth key (`.p8`), Key ID, Team ID — **via a private channel (Telegram/AirDrop),
  never the issue or a repo**.
- Push Notifications capability enabled on the app target.

## Error handling summary

- Bad/missing key at startup → APNs disabled + logged; never crashes the server.
- Push failures → logged with APNs reason; 410 prunes the token.
- Registration endpoint: 401 no session, 422 empty token.

## Testing

- `internal/apns`: JWT claims/kid/expiry-cache, request headers/URL/payload shape,
  410 → `ErrUnregistered`, host selection — against `httptest.Server` (client accepts
  an injected base URL + `http.Client`).
- Store: token CRUD, re-home on account switch, `UserByEmail`.
- API: register endpoint auth matrix; emit-hook fan-out (recipient, email fallback,
  broadcast, no-tokens drop) with a fake pusher.
- Live E2E deferred until the key + a real device token exist (sandbox push observed
  on Rei's phone; tracked on issue #8).

## Out of scope

- Notification-outbox polling in the app (superseded by push).
- Retry/queueing for failed pushes; delivery receipts.
- Silent/background pushes, badges, notification actions.
