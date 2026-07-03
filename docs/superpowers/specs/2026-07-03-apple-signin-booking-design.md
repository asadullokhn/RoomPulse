# Sign in with Apple + Native Room Booking — Design Spec

Date: 2026-07-03
Status: design approved
Scope: backend only (`backend/`). The consumer mobile app is Rei's separate repo (`Reishandy/QuickRoom`); this repo's `mobile/RoomPulseBeaconLab` is an unrelated iBeacon test harness, not the production app, and is untouched by this work.

## Problem

Rei's iOS app is going to let users sign in with Apple and book a room. Today's backend has:

- **No end-user auth at all.** Every endpoint is intentionally open (explicit CORS comment: "the API is public and carries no cookies/credentials"). The only OAuth flow that exists is Zoom's, and it's single-tenant/process-global (one admin authorizes the whole backend once) — architecturally unrelated to per-end-user identity.
- **No way to create a reservation.** `zoom.Client` has exactly three methods — `ListWorkspaces`, `ListReservations`, `SendEvent` (check-in/out only) — and reservations exist purely as a read-only mirror pulled from Zoom by `sync.Service`. There is no `POST /reservations` anywhere, and no local user/account table to attach a booking to (`Reservation.UserID`/`UserEmail` are opaque strings sourced from Zoom, not foreign keys).

## Goals

- Verify Apple identity tokens server-side and issue our own session so the app can make authenticated requests.
- Let a signed-in user create, list, and cancel their own room bookings.
- New bookings can't collide with existing reservations, regardless of whether those came from Zoom sync or another app booking.
- App-native bookings survive a backend restart (they're not recoverable from any external system the way Zoom-mirrored ones are).
- Reuse the existing scenario engine (no-show grace, notifications, occupancy driving, admin panel) for app bookings instead of building a parallel system.

## Non-goals

- Not creating real Zoom reservations. Confirmed with the user: bookings are QuickRoom-native for now; Zoom sync stays a separate, read-only concern.
- Not restricting sign-in to an email allowlist/domain. Confirmed: any Apple ID can sign in and book, for now.
- Not implementing the Apple server-to-server authorization-code flow (client-secret JWT, Apple refresh tokens, revocation webhooks) — the app does the native on-device Sign in with Apple flow and hands us just the identity token; we only need to verify it, not exchange codes with Apple ourselves.
- Not touching `mobile/RoomPulseBeaconLab` or any other mobile source — this repo's mobile-facing contract is the HTTP API only.

## Architecture overview

```
Rei's app                         This backend
──────────                        ────────────
ASAuthorizationAppleIDProvider
  → identityToken (JWT)
        │
        ▼
POST /auth/apple {identity_token, name?}
        │                          verify JWT against Apple's JWKS
        │                          (iss/aud/exp/signature)
        │                          upsert User by apple_sub
        │                          create session row (SQLite)
        ◄─────────────────────────  {session_token, user}
        │
Authorization: Bearer <session_token>
        │
POST /reservations {workspace_id, start_time, end_time}
        │                          resolve user from session
        │                          check conflicts (zoom + app sources)
        │                          create domain.Reservation{Source:"app"}
        │                          persist to SQLite + in-memory store
        ◄─────────────────────────  the created reservation
```

## Components

### 1. Apple identity verification (`backend/internal/appleauth/`, new package)

- `VerifyIdentityToken(ctx, tokenString string) (Claims, error)` — fetches and caches Apple's public keys from `https://appleid.apple.com/auth/keys` (refetch on cache miss / key-id not found, since Apple rotates keys), parses the JWT with `github.com/golang-jwt/jwt/v5`, verifies the RS256 signature, and checks `iss == "https://appleid.apple.com"`, `aud == cfg.AppleBundleID`, `exp` not passed. Returns `Claims{Sub, Email, EmailVerified}`.
- No Apple client secret, no code exchange, no Apple refresh tokens — the app sends us the identity token straight from the native `ASAuthorizationAppleIDProvider` flow.
- New dependency: `github.com/golang-jwt/jwt/v5`. The JWKS fetch/cache is small enough to hand-roll with stdlib `crypto/rsa`/`encoding/json`, matching this codebase's preference for minimal dependencies.
- New config: `APPLE_BUNDLE_ID` (required for this feature to work — the app's Bundle ID, checked against the token's `aud`).

### 2. Users and sessions (new SQLite tables, alongside the existing `devices`/`events` tables in `store/sqlite.go`)

```sql
CREATE TABLE IF NOT EXISTS users (
  user_id    TEXT PRIMARY KEY,           -- generated (e.g. "usr_" + random hex)
  apple_sub  TEXT NOT NULL UNIQUE,       -- Apple's stable per-app user identifier
  email      TEXT NOT NULL DEFAULT '',   -- from Apple; may be a private-relay address
  name       TEXT NOT NULL DEFAULT '',   -- only sent by Apple on first authorization
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,           -- SHA-256 of the opaque token; raw token never stored
  user_id    TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
```

Sessions are **opaque, DB-backed, hashed-at-rest tokens** — not JWTs. Rationale: trivial, immediate revocation (logout, account issues) with no extra machinery, at a scale where an SQLite lookup per request is free — same reasoning the existing device registry already uses SQLite for durability. Default TTL 30 days via new `SESSION_TTL` config (same pattern as `PRESENCE_TTL`).

`POST /auth/apple`: verify the identity token, look up `users` by `apple_sub`; if absent, create it (capturing `name` from the request body if this is a first-time sign-in — Apple only sends the user's name once, on the very first authorization, so the app must forward it to us then). Generate a session token, store its hash, return the raw token once (never persisted in plaintext) plus the user profile.

`POST /auth/logout`: delete the caller's session row.

### 3. Bookings reuse `domain.Reservation`

Add two fields to the existing struct rather than create a parallel type:

```go
type Reservation struct {
    // ...existing fields unchanged...
    Source         string `json:"source"`                     // "zoom" | "app"
    BookedByUserID string `json:"booked_by_user_id,omitempty"` // set only when Source == "app"
}
```

This means an app booking automatically participates in the existing no-show grace engine, grace-reminder ladder, collision/overstay detection, occupancy driving, and admin panel — all of which already operate on `domain.Reservation` — with no duplicated logic.

Add `StatusCancelled ReservationStatus = "cancelled"` alongside the existing `booked`/`no_show`/`released`, so a user-initiated cancellation is distinguishable from an auto-reclaimed no-show in the admin panel and notification feed.

### 4. Conflict checking

`POST /reservations` computes the requested room's existing reservations (`store.Memory`, which already holds both Zoom-synced and app-native reservations together) and rejects the request (409) if `[start_time, end_time)` overlaps any non-cancelled, non-released reservation for that `workspace_id` — regardless of `Source`. One source of truth for "is this room free," matching the user's explicit answer.

### 5. Durability

Zoom-mirrored reservations (`Source: "zoom"`) stay in-memory only, as today — recoverable by re-sync on restart. App-native reservations (`Source: "app"`) are the *only* record of themselves, so:

- `POST /reservations` (app source) writes through to both the in-memory store (so existing occupancy/collision/grace logic sees it immediately) and a new SQLite `reservations` table.
- On startup, after loading beacons, the backend loads persisted app-native reservations from SQLite into the in-memory store, alongside the initial Zoom sync — so a restart doesn't silently lose a real user's booking.
- Cancellation (`POST /reservations/{id}/cancel`) updates both the in-memory store and the SQLite row.

### 6. New endpoints

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | `/auth/apple` | none | body: `{identity_token, name?}` → `{session_token, user}` |
| POST | `/auth/logout` | session | deletes the caller's session |
| POST | `/reservations` | session | body: `{workspace_id, start_time, end_time}` → the created reservation, or 409 on conflict |
| GET | `/reservations/mine` | session | the caller's own reservations |
| POST | `/reservations/{id}/cancel` | session, must own the reservation | 403 if the reservation belongs to someone else |

A new `authMiddleware` (extracts `Authorization: Bearer <token>`, resolves the session → user, attaches the user to request context, 401 on missing/invalid/expired) wraps only these session-marked routes. Every existing GET endpoint stays open and unchanged, matching the current public-read-API design. Request bodies follow the existing `maxBody`/`decodeBody`/`clamp` validation conventions already used throughout `server.go`.

### 7. Docs

New paths and an `AppleAuth`/`ArrayBearer`-style security scheme added to `openapi.yaml`, following the same hand-authored pattern as every other endpoint in this file.

## Error handling

- Invalid/expired/malformed identity token → 401 with a clear message (not a 500) — this is an expected, common case (app-side token expiry, clock skew, wrong bundle ID during dev).
- Apple's JWKS endpoint unreachable → the verification call fails closed (401), never falls back to "trust the token anyway."
- Booking conflict → 409, with the conflicting reservation's window in the error body so the app can show a useful message.
- Cancelling someone else's reservation → 403, not 404 (don't leak whether the ID exists — actually: matching existing `checkEvent`'s 404-on-absent pattern is fine here too, but ownership must still be checked before mutating).
- Session lookup for an expired token → treated the same as "no session" (401), matching how `ErrNotAuthorized` is already used for the Zoom user-OAuth flow.

## Testing

Unit tests following the existing `*_test.go` pattern (`collision_test.go`, `overstay_test.go`, etc.): Apple JWT verification against fixture keys/tokens (valid, expired, wrong audience, wrong issuer, bad signature), session creation/lookup/expiry, booking conflict detection (app-vs-app, app-vs-zoom-sourced, non-overlapping windows allowed), cancel ownership enforcement, and a restart-durability test (create an app booking, reopen the SQLite DB, confirm it reloads into the store). `go vet` + `go test ./...` must stay green.

## Open decisions (confirmed with user)

- **Booking source**: backend-native, not real Zoom reservations.
- **Conflict checking**: app bookings check against both Zoom-synced and app-native reservations.
- **Access control**: open to any Apple ID; no domain/allowlist restriction for now.
