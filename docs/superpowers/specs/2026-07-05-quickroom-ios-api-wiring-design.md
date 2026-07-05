# QuickRoom iOS ↔ Backend API Wiring — Design

**Date:** 2026-07-05
**Target repo:** `Reishandy/QuickRoom` (branch `feature/api-service`, PR to `main`)
**Backend:** `https://rp.asadullokhn.uz` (this repo; no backend *code* changes — one VPS config change)

## Goal

Replace the iOS app's mock data layer with a real API service against the QuickRoom Go
backend: server-backed rooms and reservations, Sign in with Apple sessions for booking,
and beacon presence reporting. Rei's UI surface stays as-is; his `// TODO: Replace with
network` markers define the seams.

## Context (as found 2026-07-05)

- Rei's app (16 commits, 6 PRs): onboarding + permissions, floorplan home with **static
  mock rooms** (`room-a`…`room-k` polygons in `StaticRooms.swift` over a bundled PNG),
  mock `ReservationService` (random fake meetings), `BeaconMonitoringService` with
  region enter/exit + 3 s ranging and placeholder local notifications.
- Config plumbing already exists: `AppConfig`/`AppEnvironment` read `API_BASE_URL` and
  `BEACON_PROXIMITY_UUID` from xcconfig → Info.plist. Beacon UUID already matches ours
  (`11111111-2222-3333-4444-555555555555`). Base URL is a placeholder.
- No networking code exists. No test target exists.
- Backend live endpoints (all deployed): `GET /rooms`, `GET/POST /reservations`,
  `GET /reservations/mine`, `POST /reservations/{id}/cancel`, `POST /auth/apple`,
  `POST /auth/logout`, `GET /beacons`, `POST /presence`, `POST /presence/heartbeat`.
- Bundle id: `<app-bundle-id>`. Rei signs with a **paid** Apple Developer team, so
  native Sign in with Apple is viable on his builds. (Asadullokh's free team cannot sign
  the SIWA entitlement — on-device auth E2E happens on Rei's side.)

## Decisions (settled with Asadullokh)

1. **Auth: full Sign in with Apple** — no dev-login fallback.
2. **Rooms: keep Rei's floorplan** — his PNG + polygons stay; `StaticRooms` entries are
   re-keyed to real backend workspace ids. Server-driven floorplan was declined.
3. **Approach: thin client, rewrite services in place** (not a repository/protocol
   layer) — matches Rei's TODO seams, no speculative abstraction, the mock is deleted.

## Components

### 1. `QuickRoom/Core/Network/`

**`APIClient.swift`** — one small URLSession JSON client.
- Base URL from existing `AppConfig.API.baseURL`.
- `get(_:)`, `post(_:body:)` async generics; `Authorization: Bearer <token>` injected
  via a token provider closure (supplied by `AuthService`).
- Decoding: `.convertFromSnakeCase`; RFC 3339 dates with fractional-seconds fallback
  (Go emits both forms).
- Errors: typed `APIError` carrying HTTP status + the backend's `{"error": "..."}`
  message; `.unauthorized` distinguished so UI can prompt sign-in.

**DTOs** (only what we consume):
- `RoomDTO`: `room_id, zoom_workspace_id, name, capacity, has_tv, is_zoom_room`.
- `ReservationDTO`: `reservation_id, room_id, zoom_workspace_id, user_id, user_email,
  start_time, end_time, status, check_in_status, source, booked_by_user_id?`.
- `BeaconEntryDTO`: `workspace_id, uuid, major, minor, name`.
- `AuthResponseDTO`: `session_token`, `user {user_id, email, name}`.
- Request bodies: create-reservation `{workspace_id, start_time, end_time}`, presence
  `{workspace_id, user_id, display_name, event_type, event_ts}`.

### 2. `AuthService` (`Core/Services/AuthService.swift`)

- `@Observable`; exposes `currentUser`, `isSignedIn`, `signIn()`, `signOut()`.
- `signIn()`: async wrapper around `ASAuthorizationController` (scopes
  `.fullName, .email`) → `identityToken` → `POST /auth/apple {identity_token, name}`
  (name forwarded only when Apple provides it — first authorization only) →
  store `session_token` + user in Keychain (`KeychainStore`, a minimal
  `kSecClassGenericPassword` wrapper).
- `signOut()`: `POST /auth/logout`, clear Keychain + state.
- On any 401 from an authenticated call, the session is cleared and the caller sees
  `.unauthorized` (UI re-prompts).
- **Entitlement:** add `com.apple.developer.applesignin` to `QuickRoom.entitlements`.
- **UI (minimal, Rei can restyle):** one skippable Sign-in-with-Apple step appended to
  his onboarding flow; a sign-in prompt when an unauthenticated user taps Reserve.

### 3. `ReservationService` — network internals, same public surface

His views keep calling `rooms`, `reservations`, `isLoading`, `fetchReservationsOnLoad()`,
`reserve(...)`, `cancelReservation(...)`, `status(for:at:)` — unchanged signatures.

- **Rooms:** `StaticRooms` keeps the polygons but each entry carries the real workspace
  id. Mapping (arbitrary-but-stable demo assignment; one file, trivially re-keyed):

  | Polygon | Workspace | Room |
  |---|---|---|
  | room-a (large left) | `ws-agung` | BINB Agung Zoom (80) |
  | room-b…e (top row) | `ws-bedugul`, `ws-mengwi`, `ws-nusadua`, `ws-petang` | Zoom rooms (8/6/6/6) |
  | room-f, room-g | `ws-sanur`, `ws-ubud` | Zoom rooms (8/12) |
  | room-h, room-i, room-j | `ws-ceningan`, `ws-lembongan`, `ws-penida` | Workspaces (4) |
  | room-k | — dropped (11 polygons, 10 rooms) | |

  On load, `GET /rooms` overlays live names/capacity onto the mapped entries; a mapped
  room missing from the server response is shown disabled.
- **Fetch:** `GET /reservations` → keep only `status == "booked"`;
  `isMyReservation = (booked_by_user_id == currentUser.user_id)`. Mapped to his
  `Reservation` model with `roomId = zoom_workspace_id`.
- **Reserve:** `POST /reservations`; HTTP 409 surfaces as a friendly "already booked"
  error; 401 triggers the sign-in prompt.
- **Cancel:** `POST /reservations/{id}/cancel` (own app-sourced bookings only — matches
  the UI, which only offers cancel on `isMyReservation`).
- **Freshness:** refresh on load, after each mutation, and on a 30 s timer while
  foregrounded (so other users' bookings and grace-engine releases appear live).

### 4. Presence wiring (`BeaconMonitoringService` TODO seams)

- `BeaconDirectory`: cached `GET /beacons`, fetched at app start and on demand —
  resolves ranged `(major, minor)` → `workspace_id`.
- Enter: existing 3 s ranging burst identifies the closest beacon → resolve workspace →
  persist as `lastWorkspaceId` (UserDefaults) → `POST /presence {event_type: "entered"}`.
- Exit: `POST /presence {event_type: "exited"}` for the persisted `lastWorkspaceId`,
  then clear it. (Region exit callbacks don't say which beacon — hence persistence.)
- Identity: `user_id` = signed-in user's id, else `identifierForVendor` string
  (presence is deliberately unauthenticated in the backend); `display_name` = user's
  name or device name. `event_ts` = epoch millis.
- Delivery: fire-and-forget with logging; no offline retry queue. Server-side
  `PRESENCE_TTL` (30 min) backstops a lost exit.
- The `// TODO: remove` debug local-notification code is removed as marked.
- **`POST /presence/heartbeat` is deliberately out of scope** — region events already
  drive check-in/out; heartbeat is dashboard reconciliation. Follow-up PR.

### 5. Config

- `Debug.xcconfig` / `Release.xcconfig`: `API_BASE_URL = https:/$()/rp.asadullokhn.uz`.
  (In xcconfig, `//` starts a comment — Rei's current placeholders silently truncate to
  `https:`. The `$()` splice is the standard workaround; comment added in-file.)
- **VPS (required for auth to work at all):** the deployed backend has no
  `APPLE_BUNDLE_ID`, so the verifier's empty bundle id rejects every token. Add
  `APPLE_BUNDLE_ID: "${APPLE_BUNDLE_ID:-}"` to the compose `environment:` block (VPS
  copy at `/root/roompulse/docker-compose.yml` — edited in place; it is excluded from
  deploy tars) and `APPLE_BUNDLE_ID=<app-bundle-id>` to `/root/roompulse/.env`,
  then `docker compose -p backend up -d` to recreate. Mirror the compose change in this
  repo's `backend/docker-compose.yml` so fresh checkouts match.

## Error handling summary

- Network/decoding failures → `APIError` with a human message; `ReservationService`
  leaves stale data visible rather than blanking the UI.
- 401 → session cleared, UI prompts Sign in with Apple.
- 409 on reserve → "room already booked in that window".
- Presence POST failures → logged, dropped (TTL backstop).

## Testing & verification

- Local clone at `~/CascadeProjects/Personal/QuickRoom`; build with full Xcode
  (`DEVELOPER_DIR=/Applications/Xcode.app`) for the iOS Simulator with
  `CODE_SIGNING_ALLOWED=NO` — the merge gate is a green build.
- Unit tests (DTO decoding against captured live JSON, room mapping overlay, presence
  event construction) **if** an XCTest target can be added to his pbxproj cleanly;
  his project has none today and hand-editing pbxproj is the risk. If it turns messy,
  fall back to build + live verification and say so in the PR.
- Live verification from the simulator (no signing needed): rooms + reservations load
  from `rp.asadullokhn.uz`; booking path exercised end-to-end where possible (SIWA on
  simulator works with a signed-in Apple ID, but is flaky — not a gate). Backend-side
  checks via admin panel (booking appears; cancel works).
- On-device auth + beacon presence E2E is Rei's run (paid team + physical beacon);
  the PR body gives him the exact checklist.

## Out of scope (follow-ups)

- `POST /presence/heartbeat` foreground loop.
- Notification-outbox polling (`GET /notifications?recipient=`) → local notifications.
- Server-driven floorplan (declined for now; revisit if rooms change often).
