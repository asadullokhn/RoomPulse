# Beacon (Room Assignment) CRUD — Design Spec

Date: 2026-07-03
Status: design approved (proceeding on best judgment — user was away for the scoping questions; see Open decisions)
Scope: backend (`backend/internal/api`, `backend/internal/store`) + frontend (`frontend/src/components/admin`, `frontend/src/views/AdminView.vue`)

## Problem

The admin panel is entirely read-only today. The one area with a genuine prior design ([[2026-06-29-configurable-beacon-design]]) — letting an admin assign/reassign a beacon's iBeacon identity (UUID/major/minor) to a room "from the backend website" — was never built past the firmware half: `store.SetBeacon` (upsert) exists but has zero callers, `store.SaveBeacons` (persist to `BeaconsFile`) exists but has zero callers, and there is no HTTP endpoint that mutates a beacon mapping. `GET /beacons` is the only beacon route.

## Goals

- Full CRUD on the room↔beacon mapping (`domain.Beacon{WorkspaceID, UUID, Major, Minor}`) via the HTTP API.
- Every mutation persists to `BeaconsFile` (closing the gap the config.go comment already claimed was true: "admin edits survive restart").
- A Beacons section in the Admin Vue UI: list, assign-to-a-room, edit, delete.
- Keep the existing one-beacon-per-workspace data model — no re-architecture to the design doc's later-phase "many beacons per room" (out of scope; not requested).

## Non-goals

- Not touching Rooms, Reservations, Users, or Devices CRUD — no prior design intent for those (confirmed by research), and the request's clearest signal (Beacons has an approved spec) doesn't extend to them.
- Not merging PR #12 (Sign in with Apple + booking) as a prerequisite — beacon assignment is unrelated to auth/booking and works fully on `main`'s current state.
- Not adding role-based access control — this admin panel has always been single-operator, unauthenticated-by-design (matches every other admin endpoint today).
- Not changing the firmware or the beacon's own auto-derived `(major, minor)` fingerprint scheme — this is purely the backend mapping layer the original design doc scoped as "Component 2."

## API

Two new routes; `GET /beacons` is unchanged.

- **`PUT /beacons/{workspace_id}`** — create-or-replace the beacon mapping for a room (idempotent upsert, standard PUT semantics — avoids an artificial POST-vs-PUT split for what's the same operation either way). Body: `{uuid, major, minor}`. Validates: `workspace_id` must match an existing room (404 if not — mirrors `createReservation`'s existing `RoomByWorkspace` check), `uuid` non-empty and ≤128 chars, `major`/`minor` in `[0, 65535]` (iBeacon's actual field width). Returns the updated `Beacon` joined to room name, same shape as a `GET /beacons` entry.
- **`DELETE /beacons/{workspace_id}`** — remove the mapping. 404 if none exists.

Both mutations: apply to the in-memory store (`SetBeacon`/new `RemoveBeacon`), then best-effort persist the *full* current beacon list to `BeaconsFile` via the existing `SaveBeacons` (log a warning on write failure, don't fail the request — matches how `upsertReservation` treats its SQLite write as best-effort).

## Data layer

One new `store.Memory` method: `RemoveBeacon(workspaceID string)` (delete from the map, mirroring `SetBeacon`'s shape). No SQLite involved — beacons stay in the existing in-memory-plus-JSON-file persistence model (`BeaconsFile`), unchanged from today.

`Server` gains a `beaconsFile string` field, set via a new `ConfigureBeaconsFile(path string)` setter (matches the existing `ConfigureGrace`/`ConfigureNotify`/`ConfigureOverstay` pattern), called from `main.go` with `cfg.BeaconsFile`.

## Frontend

New `frontend/src/components/admin/BeaconsPanel.vue`, added as a new numbered section in `AdminView.vue` (after Rooms & occupancy). Contents:
- A table (room name, workspace id, UUID, major, minor, actions) — reuses the existing `DataTable` shell.
- An "Assign beacon" row: a `<select>` of rooms from `GET /rooms` (any room — reassigning an already-mapped one is valid), plus UUID/major/minor inputs, plus a Save button that calls `PUT /beacons/{workspace_id}`.
- Each existing row: an Edit toggle (turns that row's UUID/major/minor cells into inputs in place, Save/Cancel) and a Delete button (browser `confirm()` before calling `DELETE`, since it's destructive and this panel has no undo).
- Refreshes the beacon list (re-fetches `GET /beacons`) after any successful mutation, reusing the view's existing poll cycle rather than a separate refresh path.

## Error handling

- `PUT` with an unknown `workspace_id` → 404 (room doesn't exist).
- `PUT` with invalid `uuid`/`major`/`minor` → 422, matching every other validation failure in this API.
- `DELETE` with no existing mapping → 404.
- Persist-to-disk failure → logged, request still succeeds (in-memory state is authoritative for the running process; matches the existing best-effort persistence philosophy used throughout this codebase).

## Testing

New `backend/internal/api/beacons_test.go`: PUT creates, PUT updates an existing mapping, PUT against an unknown workspace 404s, DELETE removes, DELETE on absent mapping 404s, validation bounds (major/minor out of `[0,65535]`, empty uuid) 422. Manual verification: exercise the new BeaconsPanel in-browser against the dev server, confirm a restart reloads the edited mapping from `BeaconsFile`.

## Open decisions

- **Proceeding without confirmation**: the scoping questions (which entities to CRUD; whether to merge PR #12 first) went unanswered (user away). Chose Beacons — the only area with genuine prior design intent — and chose to stay independent of PR #12 since beacon assignment doesn't need it. Flagging this clearly for review since it was a judgment call, not a confirmed decision.
