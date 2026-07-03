# Vue 3 Frontend — Design Spec

Date: 2026-07-03
Status: design approved (user to review written spec)
Scope: frontend only (backend `ZOOM_MODE` stays `mock`; that's a separate future task)

## Problem

QuickRoom's web surface is 8 static HTML files, each a self-contained `<style>` + vanilla-JS (or, for `/admin`, Vue-via-CDN) blob, embedded into the Go binary with `go:embed` and served byte-for-byte. This worked for a hackathon prototype but doesn't scale as component logic grows (each page re-implements the same header, table, badge, and polling boilerplate) and has no real build tooling, type safety, or component reuse.

Three of the eight pages are genuinely data-driven and change together: **Dashboard** (`/`), **Admin** (`/admin`), **Floor plan** (`/floor`) — all poll the same live-occupancy/reservation APIs every few seconds. The other five (`/how`, `/battery`, `/hardware`, `/scenarios`, `/decide`) are static/informational or one-off internal tools and don't need a framework.

## Goals

- Rebuild Dashboard, Admin, and Floor plan as a real Vue 3 SPA (Vite, TypeScript, Vue Router) with shared, reusable components.
- Preserve the existing visual design exactly — same dark theme tokens, same fonts (Space Grotesk / IBM Plex), same teal "signal" accent, same layout and interaction flow (advanced disclosure, room detail modal, live polling).
- Keep the single-binary deployment model: the Go backend embeds the built SPA via `go:embed`, same as it already self-hosts Swagger UI assets. No separate frontend host, no CDN.
- Zero behavior change from the backend's point of view — same REST endpoints, same response shapes, same polling cadence.

## Non-goals

- Not touching `/how`, `/battery`, `/hardware`, `/scenarios`, `/decide` — they stay Go-embedded static HTML, unchanged.
- Not switching `ZOOM_MODE` from `mock` to `live`/`user` — bookings stay simulated.
- Not adding automated frontend tests (Vitest) in this pass — verification is manual, in-browser.
- Not adopting Tailwind/shadcn-vue — the current design is already a bespoke theme; re-deriving it in a utility framework adds work with no payoff.
- Not adding Pinia — no state is genuinely shared across the three routes today (each page polls and owns its own data independently), so a global store would be a speculative abstraction.

## Architecture overview

New `frontend/` directory, sibling to `backend/`, `device/`, `mobile/`:

```
frontend/
  index.html
  package.json
  vite.config.ts            — dev server proxies /rooms, /reservations, /occupancy, etc. → localhost:8080
  tsconfig.json
  src/
    main.ts
    router/index.ts         — 3 routes: /, /admin, /floor
    api/
      client.ts             — typed fetch wrappers, one per endpoint used
      types.ts               — Room, Reservation, Occupancy, Device, Beacon, Collision, Overstay,
                                Notification, Utilization, FloorRoom — mirrors openapi.yaml
    composables/
      usePoll.ts             — setInterval-driven refresh; clears on unmount
      useConnection.ts        — tracks live/reconnecting state from fetch success/failure
    components/
      layout/AppHeader.vue    — brand, nav (8 items), connection chip — shared by all 3 views
      ConnChip.vue
      VitalCard.vue
      RoomCard.vue             — occupancy-grid card (Dashboard)
      DataTable.vue            — generic bordered/striped table shell used by reservations/devices/beacons/admin tables
      Badge.vue
      floor/FloorPlanCanvas.vue — SVG overlay + fit-to-screen transform, ported as-is
      floor/RoomDetailModal.vue
      admin/AlertsList.vue
      admin/KpiRow.vue
      admin/NotificationsList.vue
    views/
      DashboardView.vue
      AdminView.vue
      FloorView.vue
    styles/
      theme.css                — :root design tokens + shared header/nav/card/table/badge classes, ported verbatim
```

## Routing & page scope

Vue Router in history mode owns exactly `/`, `/admin`, `/floor`. Navigating to any of the other 5 paths (`/how`, `/battery`, `/hardware`, `/scenarios`, `/decide`) is a plain `<a href>` full-page load to the Go-served static HTML — exactly how the nav bar already behaves today (it's already plain anchors, not an SPA router, so this is a no-op change in UX). `AppHeader` renders the full 8-item nav on all 3 SPA views (Admin's nav is currently trimmed to 5 items — this standardizes it to match Dashboard/Floor's fuller nav).

## Component breakdown

**Dashboard** (`DashboardView.vue`): `VitalsRow` (4 stat cards) → `OccupancyGrid` of `RoomCard`s (busy/booked/empty states, sorted) → `DataTable` for reservations → collapsible `<details>` "Advanced" section with `DataTable` for devices and beacons, plus the "Sync from Zoom" button.

**Admin** (`AdminView.vue`): `KpiRow` (6 stats: bookings, checked-in, reclaimed, no-show rate, rooms in use, people present) → `AlertsList` (collisions + overstays, or an all-clear state) → `DataTable` for reservations (reused component, different column set) → `RoomsGrid` (occupancy-by-room cards, simpler than Dashboard's `RoomCard`) → `NotificationsList` (outbox feed).

**Floor plan** (`FloorView.vue`): `FloorPlanCanvas` — the SVG polygon overlay + label layer + fit-to-screen scale/translate math, ported directly from `floor.html`'s `build()`/`paint()`/`fit()` logic (this is calibrated viewBox math tied to `floor.png`'s pixel dimensions; rewriting the approach isn't worth the risk, just re-host it as a component with props/emits instead of global functions). `RoomDetailModal` — opens on room click/tap, shows inside-now occupants, today's booking, and recent activity (fetches `/events?workspace_id=...` and `/reservations` on open, matching current behavior).

## Data layer

One composable per view — `useDashboardData()`, `useAdminData()`, `useFloorData()` — each doing the same `Promise.all([...])` fetch-and-shape work the current inline `<script>` blocks do, returning reactive refs the view template consumes directly. Wrapped in `usePoll(fn, intervalMs)`:

- Dashboard: 3000ms (matches today)
- Admin: 4000ms (matches today)
- Floor: 3000ms (matches today)

No response caching/dedup layer (no TanStack Query) — plain polling is sufficient at this scale and matches current behavior exactly. `useConnection()` centralizes the live/reconnecting chip state (fetch success ⇒ live, failure ⇒ reconnecting), reused by all 3 views instead of each page hand-rolling `setConn()`.

`api/client.ts` wraps every endpoint the 3 views call: `/rooms`, `/reservations`, `/occupancy`, `/devices`, `/beacons`, `/events`, `/utilization`, `/collisions`, `/overstays`, `/notifications`, `/floor/rooms`, `/info`, `/sync` (POST). Types in `api/types.ts` mirror the JSON shapes already visible in the current pages' JS (cross-checked against `openapi.yaml` during implementation).

## Styling

`styles/theme.css` ports the `:root` custom properties (colors, fonts, radii) and the shared structural classes (`header`, `.brand`, `.seg`, `.chip`, `.vital`, `.card`, `table`/`th`/`td`, `.badge`, `.room`, `.empty`) verbatim from the current pages — these are byte-identical across `dashboard.html`/`admin.html`/`floor.html`/`how.html` today, so consolidating them into one file is a pure de-duplication, not a redesign. Page-specific styling (floor plan SVG/legend/modal, admin alert cards) lives in scoped `<style>` blocks on the relevant components. Google Fonts (Space Grotesk, IBM Plex Mono/Sans) stay loaded via the same `<link>` tags, now in `frontend/index.html`.

## Build & serving

- `npm run build` in `frontend/` produces `frontend/dist/` (`index.html` + hashed `assets/*.js`/`*.css`).
- `backend/internal/api/server.go` gets a `//go:embed dist` (or equivalent) pointing at the built output, replacing the `dashboardHTML`, `adminHTML`, `floorHTML` embeds and their raw-byte handlers.
- `mux.HandleFunc("GET /{$}", ...)`, `GET /admin`, `GET /floor` all serve the embedded `index.html` — Vue Router's history mode takes over client-side.
- A new `mux.Handle("GET /assets/", http.FileServerFS(...))`-style handler serves the embedded `dist/assets` directory.
- Every JSON API and `/favicon.svg`, `/floor/image`, `/floor/rooms` stay exactly as they are — zero backend logic changes beyond the static-file wiring above.
- Vite dev server (`npm run dev`) proxies API paths to `http://localhost:8080` so `fetch("/rooms")` works identically in dev and prod without an env-specific base URL.

## Docker

`backend/Dockerfile` gains a `node:alpine` build stage ahead of the existing Go stage:

```
FROM node:22-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

FROM golang:1.26-alpine AS build
...
COPY --from=frontend-build /src/frontend/dist /src/internal/api/dist
RUN CGO_ENABLED=0 GOOS=linux go build ...
```

(Exact embed path finalized during implementation to match wherever `server.go` declares the `go:embed` directive.)

## Migration plan

1. Scaffold `frontend/` (Vite + Vue 3 + TS + vue-router), theme.css port, `AppHeader`.
2. Build Dashboard view + its components against the live backend (`go run ./cmd/quickroom`, `npm run dev` with proxy).
3. Build Admin view.
4. Build Floor plan view (highest-risk piece — the SVG viewBox/fit math).
5. Wire the Go embed + static handlers; delete `dashboard.html`, `admin.html`, `floor.html` and their old embeds/handlers.
6. Update `Dockerfile` with the node build stage.
7. Manual verification pass (see Testing) on all 3 routes, including a full `docker build` to catch embed/path issues before deploy.

## Testing

Manual, in-browser verification via the `/run` skill: exercise each view's golden path (data loads, polling updates counts, advanced disclosure toggles, floor modal opens/closes, sync button works) plus edge cases already visible in the current pages (empty states — no rooms/no reservations/no devices — and the reconnecting-chip state on a backend restart). No Vitest/component-test scaffolding in this pass.

## Open decisions (confirmed with user)

- **TypeScript**: yes (matches the user's standing Vue Enterprise Rules; this is now a real MVP).
- **State management**: composables, not Pinia (no genuine cross-route shared state exists today).
- **Page scope**: only Dashboard/Admin/Floor convert to Vue; the other 5 pages stay static Go-HTML.
- **Backend Zoom integration**: out of scope — `ZOOM_MODE` stays `mock`.
