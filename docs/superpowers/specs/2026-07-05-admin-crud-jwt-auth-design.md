# Admin CRUD + JWT Authentication — Design

**Date:** 2026-07-05
**Decisions (Asadullokh):** JWT everywhere (mobile too), full rooms CRUD with
persistent overrides for Zoom rooms, **keep current data** (additive migration
only — existing users, APNs tokens, app bookings, devices, events all survive).

## Goal

Give the admin panel real authentication (email+password → JWT) and full CRUD
over its resources; switch mobile auth from opaque sessions to JWTs issued by
the same signer. Rei's app keeps working without code changes (one forced
re-login after deploy).

## 1. Auth

- **Tokens:** HS256 JWTs (`golang-jwt/v5`, already a dep). Claims: `sub`,
  `role` (`admin` | `user`), `iat`, `exp`. TTLs: admin 12 h, user 720 h
  (matches today's SESSION_TTL). Secret: `JWT_SECRET` env; if unset, random
  32 bytes generated once and persisted to `/data/jwt_secret` so restarts
  don't invalidate tokens.
- **Admins:** new `admins` table (`admin_id, email UNIQUE, password_hash,
  created_at`), bcrypt (`golang.org/x/crypto` — new dep). Seeded at boot from
  `ADMIN_EMAIL` / `ADMIN_PASSWORD` env **only when the table is empty**; code
  defaults are the placeholder creds Asadullokh supplied (admin@example.com /
  SuperAdmin123!) — real values go in the VPS `.env`, rotate later.
- **Endpoints:** `POST /auth/login` `{email,password}` → `{token, email}`
  (admin). `POST /auth/apple` unchanged in shape — the `session_token` field
  now carries a user JWT (opaque to the client, so Rei's app is untouched).
  `POST /auth/logout` becomes a client-side no-op endpoint (200 ok) — JWTs
  aren't server-revocable.
- **Middleware:** `requireUser` / `requireAdmin` — verify signature + role,
  then confirm the principal still exists (users table / admins table; one
  indexed lookup, same cost as the old session lookup). Deleting a user still
  cuts their access instantly.
- **Sessions:** code paths deleted (`CreateSession`, `SessionUserID`,
  `DeleteSession`, `DeleteSessionsForUser` and callers). The `sessions`
  TABLE stays in the schema untouched (keep-current-data: no drops).

## 2. Protection matrix

- **Open:** `/health/*`, `/info`, `/auth/login`, `/auth/apple`,
  `POST /presence`, `POST /presence/heartbeat`, `/openapi.yaml`, `/docs`,
  SPA assets, and mobile reads: `GET /rooms`, `GET /reservations`,
  `GET /beacons`, `GET /occupancy`, `GET /floor/*` (the app browses before
  sign-in — "Skip for now" flow).
- **User JWT:** `POST /reservations`, `POST /reservations/{id}/cancel`,
  `GET /reservations/mine`, `POST /devices/apns`, `POST /auth/logout`.
- **Admin JWT:** `/users*` (all), `POST /admin/reservations*`,
  `PUT/DELETE /beacons/{id}`, `POST /sync`, `GET /notifications` +
  new deletes, `GET /collisions|/overstays|/utilization|/devices|/events`,
  `/diag*`, `/history*`, `/decision*`, `/scenario-answers*`,
  `POST /reservations/{id}/check-in|check-out` (manual override lever;
  the app never calls these — presence drives check-in internally), and all
  new CRUD below.

## 3. Rooms CRUD (custom rooms + overrides)

- New tables:
  - `custom_rooms(workspace_id PK ("cr-"+8 hex), name, capacity, has_tv,
    created_at)` — admin-created rooms, `is_zoom_room=false`.
  - `room_overrides(workspace_id PK, name TEXT '' = keep, capacity INTEGER
    -1 = keep, has_tv INTEGER -1 = keep)` — patches for Zoom-synced rooms.
- **Sync integration:** `sync.Service.Run` gains a final step that re-upserts
  custom rooms and re-applies overrides after every Zoom sync (and after the
  boot sync), so admin edits always win over the mirror. `store.Memory` gains
  `DeleteRoom(workspaceID)`.
- Endpoints (admin JWT):
  - `POST /rooms` `{name, capacity, has_tv}` → creates a custom room.
  - `PATCH /rooms/{workspace_id}` `{name?, capacity?, has_tv?}` — custom room:
    edits it; Zoom room: writes/updates its override. Applied to the live
    mirror immediately.
  - `DELETE /rooms/{workspace_id}` — custom room: removes it + its beacon
    mapping + cancels its open app bookings (delete-cascade precedent);
    Zoom room: **clears the override** (reset to Zoom truth, room stays).

## 4. Reservations CRUD (admin)

- `POST /admin/reservations` `{workspace_id, start_time, end_time,
  user_email?}` — conflict-checked like user booking; source `app`,
  `booked_by_user_id: "admin"`, recipient = given email (notifications
  route there).
- `PATCH /admin/reservations/{id}` `{start_time?, end_time?}` — app-sourced
  only (403 otherwise), conflict-checked.
- Cancel exists. Zoom-sourced reservations stay read-only: the 60 s sync
  rewrites them, so edits would be fiction — rooms get overrides because they
  are stable entities; live meeting mirrors are not.

## 5. Users + notifications

- `PATCH /users/{id}` `{name}` (admin) — rename. No user create: identities
  only come from Sign in with Apple.
- `DELETE /notifications/{id}` and `DELETE /notifications` (clear all) —
  ring-buffer removals on the notifier (admin).

## 6. Admin UI (Vue)

- `LoginView` (email/password → `POST /auth/login`), JWT in localStorage.
  `client.ts` gains a shared `authFetch` attaching `Authorization: Bearer`;
  any 401 clears the token and routes to login. Header gets a logout button.
- Reservations section: "New booking" form (room select, window, email) +
  Edit/Cancel on app-sourced rows.
- RoomsGrid: add/edit/delete room (inline-edit pattern copied from
  BeaconsPanel; Zoom rooms show "override" affordance, delete = reset).
- NotificationsList: per-row dismiss + clear-all.
- UsersPanel: inline rename.
- Swagger: `bearerAuth` (JWT) scheme; document every new/changed path.

## 7. Rei's side

No code changes required: the token field name and bearer semantics are
unchanged; his 401 handling already routes to the sign-in sheet. After
deploy his old opaque token dies → one re-login; his registered APNs token
row survives (keep-current-data), so pushes continue. Verify his flows live
(sign-in → book → cancel → push) and tell him on issue #8 / iMessage.
If live verification finds an app-side gap, it becomes a small PR.

## 8. Migration & deploy (keep current data)

- Schema changes are `CREATE TABLE IF NOT EXISTS` only; no drops, no volume
  wipe; `/data/roompulse.db` keeps users/apns_tokens/app_reservations/
  devices/events.
- VPS `.env` additions (values never in git): `JWT_SECRET` (random),
  `ADMIN_EMAIL`, `ADMIN_PASSWORD`. Compose gets value-free passthroughs.
- Deploy = usual tar + rebuild; verify: admin login works, protected
  endpoints 401 without JWT, mobile booking works after re-sign-in,
  APNs pushes still deliver.

## Error handling

- Login: 401 bad creds (uniform message), 422 missing fields.
- Admin CRUD: 404 unknown ids, 409 booking conflicts, 403 zoom-sourced
  reservation edits, 422 validation.
- Middleware: 401 missing/expired/invalid token or vanished principal;
  403 wrong role.

## Testing

- Go: login matrix, JWT round-trip + expiry, role middleware (user token on
  admin route → 403, deleted principal → 401), rooms CRUD incl.
  override-survives-sync and delete-cascade, admin reservation
  create/edit conflicts + zoom 403, notification deletes, protected-route
  sweep (table-driven: path → expected status without token).
- Frontend: `npm run build` green; live browser pass (login → each CRUD).
- Live: mobile re-sign-in + booking + push on production after deploy.

## Out of scope

- Refresh tokens / token revocation lists; multi-admin management UI;
  password change UI (env rotation covers it for now); audit log.
