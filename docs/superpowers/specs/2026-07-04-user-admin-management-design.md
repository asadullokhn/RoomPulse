# User Management + Admin Booking Management — Design Spec

Date: 2026-07-04
Status: design approved (user explicitly requested implementation now, for Rei's integration — proceeding without a Q&A round given stated urgency and an unambiguous requirements list)
Scope: backend (`backend/internal/api`, `backend/internal/store`) + frontend (`frontend/src/components/admin`, `frontend/src/views/AdminView.vue`)

## Problem

PR #12 (just merged) gave the mobile app booking, check-in/out, personal-bookings, and Sign in with Apple. Two things are still missing for Rei's integration and for admin operation:

1. **User management** — no way to list or remove app accounts. `store.DB` has `UpsertUser`/`UserByAppleSub`/`UserByID` (single-row lookups) but no `Users()` list method, and no delete path at all.
2. **Admin booking management** — the mobile self-service endpoints (`POST /reservations/{id}/cancel`) only let a user cancel their *own* booking. There's no way for an admin to view a specific user's bookings or cancel any booking on their behalf.

## Goals

- `GET /users` — list every app account (admin-facing, unauthenticated like every other admin endpoint in this codebase).
- `GET /users/{id}/reservations` — a specific user's bookings (admin-facing).
- `DELETE /users/{id}` — remove an account. Cascades: cancels their non-resolved app-sourced bookings (can't leave a phantom booking holding a room with no valid owner) and revokes all their sessions (forces logout everywhere).
- `POST /admin/reservations/{id}/cancel` — cancel *any* app-sourced booking regardless of owner, admin-facing (distinct path from the session-gated self-service `POST /reservations/{id}/cancel`, which is unchanged).
- Admin UI: a `UsersPanel.vue` section — list users, expand to see + cancel their bookings, delete a user with confirm.
- OpenAPI docs for all four new endpoints.

## Non-goals

- Not adding admin cancel for Zoom-sourced reservations — Zoom stays the source of truth for those; admin override is scoped to app-sourced bookings only, matching the existing self-service scope.
- Not adding any auth/role model to the admin surface — every admin endpoint in this codebase (beacons, reservations list, notifications) is deliberately unauthenticated (single trusted operator), and these four follow the same pattern.
- Not building account creation for admins — accounts are always Apple-Sign-In-originated; admin can only list/delete, never create.

## API

| Method | Path | Notes |
|---|---|---|
| GET | `/users` | All accounts, no filtering |
| GET | `/users/{id}/reservations` | One user's bookings (any status/source) |
| DELETE | `/users/{id}` | Cascades: cancel their open app-sourced bookings, revoke sessions, delete the account. 404 if unknown. |
| POST | `/admin/reservations/{id}/cancel` | Cancel any app-sourced booking. 404 if unknown, 403 if it's a Zoom-sourced reservation (not cancellable this way) |

## Data layer

New `store.DB` methods:
- `Users() ([]domain.User, error)` — read-all, mirrors `Devices()`.
- `DeleteUser(userID string) error`
- `DeleteSessionsForUser(userID string) error` — bulk-delete by `user_id` rather than by token hash (the existing `DeleteSession` is keyed by hash, for the self-service logout path — this is a different query).

No new `store.Memory` methods needed — cancelling a user's bookings reuses the existing `Reservations()`/`UpsertReservation` plus the `Server.upsertReservation` helper (already applies the SQLite write-through for app-sourced reservations).

## Frontend

New `frontend/src/components/admin/UsersPanel.vue`, added as a new numbered section in `AdminView.vue`. A table (email, name, created_at, booking count), each row expandable to show that user's reservations (room, window, status) with a Cancel action per non-resolved one, and a Delete-user button with a browser `confirm()`.

## Error handling

- `DELETE /users/{id}` / `GET /users/{id}/reservations` on an unknown id → 404.
- `POST /admin/reservations/{id}/cancel` on a Zoom-sourced reservation → 403 (matches the existing `cancelReservation`'s "not cancellable this way" semantics, just without the ownership check).
- Session-revocation or booking-cancellation failures during a `DELETE /users/{id}` cascade are logged, not fatal — the user row deletion still proceeds (best-effort, matching this codebase's established philosophy for secondary side effects).

## Testing

New `backend/internal/api/users_test.go`: list users, get one user's reservations, delete cascades (bookings cancelled, sessions revoked, second delete 404s), admin cancel works on an app-sourced booking regardless of owner, admin cancel 403s on a Zoom-sourced one. `go vet` + `go test ./...` green. Manual in-browser verification of `UsersPanel`.
