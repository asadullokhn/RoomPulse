# Vue 3 Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the 3 data-driven static HTML pages (Dashboard, Admin, Floor plan) with a Vue 3 + TypeScript SPA, built with Vite and embedded into the Go binary, preserving the exact current visual design and behavior.

**Architecture:** `frontend/` (Vite + Vue 3 + TypeScript + vue-router) builds to `backend/internal/api/web/dist/` (Vite `outDir`, so Go's `//go:embed` — which cannot reach outside its package directory — can pick it up). The Go server serves the built `index.html` for `/`, `/admin`, `/floor` and the hashed JS/CSS under `/assets/`. Every other route (`/how`, `/battery`, `/hardware`, `/scenarios`, `/decide`, and all JSON APIs) is untouched.

**Tech Stack:** Vue 3 (Composition API, `<script setup>`), TypeScript, Vite, vue-router 4. No Pinia, no Tailwind, no test framework (per spec — manual verification only).

## Global Constraints

- Design tokens, fonts (Space Grotesk / IBM Plex Mono / IBM Plex Sans), and the teal "signal" (`#2FE6B0`) accent must match the current pages exactly — no redesign.
- Polling cadence: Dashboard 3000ms, Admin 4000ms, Floor 3000ms (matches current `setInterval` values).
- Same-origin fetches only (`fetch("/rooms")`, no base URL) — must work identically in Vite dev (via proxy) and the embedded prod build.
- Only `/`, `/admin`, `/floor` become Vue routes. `/how`, `/battery`, `/hardware`, `/scenarios`, `/decide` stay Go-embedded static HTML, unchanged.
- No backend logic changes beyond static-file serving — every JSON endpoint's handler, request/response shape, and behavior stays exactly as-is.
- Node 24 / npm 11 / Go 1.26 are the installed toolchain versions (confirmed in this environment) — no engine pinning needed beyond what's already in `backend/go.mod` (`go 1.26`).

---

## File Structure

```
frontend/
  index.html
  package.json
  tsconfig.json
  tsconfig.node.json
  vite.config.ts
  .gitignore
  src/
    main.ts
    App.vue
    router/index.ts
    api/types.ts
    api/client.ts
    composables/usePoll.ts
    composables/useConnection.ts
    styles/theme.css
    components/
      AppHeader.vue
      VitalCard.vue
      Badge.vue
      DataTable.vue
      dashboard/OccupancyGrid.vue
      dashboard/RoomCard.vue
      admin/KpiRow.vue
      admin/AlertsList.vue
      admin/RoomsGrid.vue
      admin/NotificationsList.vue
      floor/FloorPlanCanvas.vue
      floor/RoomDetailModal.vue
    views/
      DashboardView.vue
      AdminView.vue
      FloorView.vue

backend/internal/api/server.go        — modify (embed + routes)
backend/internal/api/dashboard.html   — delete
backend/internal/api/admin.html       — delete
backend/internal/api/floor.html       — delete
backend/Dockerfile                    — modify (node build stage)
.gitignore                            — modify (frontend/node_modules, backend/internal/api/web)
```

**Reference source files** (read, port from, do not modify): `backend/internal/api/dashboard.html`, `admin.html`, `floor.html` — used as the exact source of truth for markup structure, CSS values, and interaction logic being ported. Delete them only in Task 9, after the SPA fully replaces their behavior.

---

### Task 1: Scaffold the Vite + Vue 3 + TypeScript project

**Files:**
- Create: `frontend/package.json`, `frontend/tsconfig.json`, `frontend/tsconfig.node.json`, `frontend/vite.config.ts`, `frontend/index.html`, `frontend/.gitignore`
- Create: `frontend/src/main.ts`, `frontend/src/App.vue`, `frontend/src/router/index.ts`
- Create placeholder: `frontend/src/views/DashboardView.vue`, `AdminView.vue`, `FloorView.vue` (each just `<template><div>{{ name }} placeholder</div></template>` for now — filled in later tasks)

**Interfaces:**
- Produces: a working `npm run dev` and `npm run build` in `frontend/`, and 3 routable paths (`/`, `/admin`, `/floor`) that later tasks fill with real content.

- [ ] **Step 1: Create `frontend/package.json`**

```json
{
  "name": "quickroom-frontend",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc -b && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "vue": "^3.5.13",
    "vue-router": "^4.5.0"
  },
  "devDependencies": {
    "@types/node": "^22.10.2",
    "@vitejs/plugin-vue": "^5.2.1",
    "@vue/tsconfig": "^0.7.0",
    "typescript": "^5.7.2",
    "vite": "^6.0.5",
    "vue-tsc": "^2.2.0"
  }
}
```

- [ ] **Step 2: Create `frontend/tsconfig.json`**

```json
{
  "files": [],
  "references": [
    { "path": "./tsconfig.app.json" },
    { "path": "./tsconfig.node.json" }
  ]
}
```

- [ ] **Step 3: Create `frontend/tsconfig.app.json`**

```json
{
  "extends": "@vue/tsconfig/tsconfig.dom.json",
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.app.tsbuildinfo",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  },
  "include": ["src/**/*.ts", "src/**/*.d.ts", "src/**/*.vue"]
}
```

The `paths` entry is required for `vue-tsc` to resolve the `@/` alias — Vite's own `resolve.alias` (Step 5) only affects the dev server/bundler, not the TypeScript compiler.

- [ ] **Step 4: Create `frontend/tsconfig.node.json`**

```json
{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.node.tsbuildinfo",
    "target": "ES2023",
    "lib": ["ES2023"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "types": ["node"],
    "strict": true,
    "noEmit": true
  },
  "include": ["vite.config.ts"]
}
```

Add `@types/node` to `devDependencies` (`"@types/node": "^22.10.2"`).

- [ ] **Step 5: Create `frontend/vite.config.ts`**

```ts
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Build output lands inside the Go module (backend/internal/api/web/dist) so
// //go:embed can pick it up — go:embed can't reach outside its package dir.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: fileURLToPath(new URL('../backend/internal/api/web/dist', import.meta.url)),
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/rooms': 'http://localhost:8080',
      '/reservations': 'http://localhost:8080',
      '/occupancy': 'http://localhost:8080',
      '/devices': 'http://localhost:8080',
      '/beacons': 'http://localhost:8080',
      '/events': 'http://localhost:8080',
      '/utilization': 'http://localhost:8080',
      '/collisions': 'http://localhost:8080',
      '/overstays': 'http://localhost:8080',
      '/notifications': 'http://localhost:8080',
      '/floor/rooms': 'http://localhost:8080',
      '/floor/image': 'http://localhost:8080',
      '/info': 'http://localhost:8080',
      '/sync': 'http://localhost:8080',
      '/favicon.svg': 'http://localhost:8080',
    },
  },
})
```

- [ ] **Step 6: Create `frontend/index.html`**

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>QuickRoom</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<meta name="theme-color" content="#0A0F1F">
<meta name="color-scheme" content="dark">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@500;600;700&family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap" rel="stylesheet">
</head>
<body>
<div id="app"></div>
<script type="module" src="/src/main.ts"></script>
</body>
</html>
```

- [ ] **Step 7: Create `frontend/src/styles/theme.css`**

Port the canonical design tokens (union of all three source pages' `:root` blocks — `dashboard.html:14-28`, `admin.html:14-27`, `floor.html:14-24`) plus the page-agnostic reset/base. Use exactly these values:

```css
:root {
  --ink: #0A0F1F; --ink-2: #0C1428;
  --panel: #111A33; --panel-2: #16203C; --raised: #1C2747;
  --line: rgba(150,170,220,.12); --line-soft: rgba(150,170,220,.07);
  --text: #EAEEFB; --muted: #818DB4; --faint: #5C668A;
  --signal: #2FE6B0; --signal-dim: rgba(47,230,176,.14); --signal-line: rgba(47,230,176,.4);
  --amber: #F4B740; --amber-dim: rgba(244,183,64,.14); --amber-line: rgba(244,183,64,.4);
  --open-text: #C7D0EA; --open-line: rgba(176,190,224,.30); --open-fill: rgba(124,140,170,.12);
  --accent: #2FE6B0; --accent-dim: rgba(47,230,176,.16);
  --danger: #FF6B6B; --danger-dim: rgba(255,107,107,.13); --danger-line: rgba(255,107,107,.4);
  --r: 14px;
  --f-display: "Space Grotesk", system-ui, sans-serif;
  --f-body: "IBM Plex Sans", system-ui, sans-serif;
  --f-mono: "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
}
* { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0; color: var(--text); font-family: var(--f-body); font-size: 14px; line-height: 1.55;
}
::selection { background: var(--signal-dim); }
a { color: var(--accent); }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation: none !important; } }

/* ---- shared card / table / badge utilities (used by DataTable, VitalCard, Badge) ---- */
.card { background: linear-gradient(180deg, var(--panel-2), var(--panel)); border: 1px solid var(--line);
  border-radius: var(--r); overflow: hidden; }
.scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; min-width: 460px; }
th, td { text-align: left; padding: 12px 18px; border-bottom: 1px solid var(--line-soft); white-space: nowrap; }
th { font-family: var(--f-mono); font-size: 10.5px; text-transform: uppercase; letter-spacing: 1px;
  color: var(--muted); font-weight: 500; background: rgba(255,255,255,.012); }
tbody tr:last-child td { border-bottom: 0; }
tbody tr { transition: background .12s; }
tbody tr:hover { background: rgba(47,230,176,.05); }
td.num { font-family: var(--f-mono); font-variant-numeric: tabular-nums; }
.mono { font-family: var(--f-mono); font-size: 12px; color: var(--muted); }
.mono.id { color: var(--text); }
tr.stale td { opacity: .45; }

.badge { display: inline-block; padding: 3px 10px; border-radius: 999px; font-size: 11px; font-weight: 600;
  font-family: var(--f-mono); border: 1px solid transparent; letter-spacing: .2px; }
.b-signal { background: var(--signal-dim); color: var(--signal); border-color: var(--signal-line); }
.b-amber { background: var(--amber-dim); color: var(--amber); border-color: var(--amber-line); }
.b-muted { background: rgba(129,141,180,.12); color: var(--muted); border-color: var(--line); }
.b-danger { background: var(--danger-dim); color: var(--danger); border-color: var(--danger-line); }

.empty { padding: 26px 18px; color: var(--muted); font-size: 13px; text-align: center; }
.empty b { display: block; color: var(--text); font-weight: 600; margin-bottom: 3px; font-family: var(--f-display); }

.eyebrow { font-family: var(--f-mono); font-size: 11px; font-weight: 500; text-transform: uppercase;
  letter-spacing: 2px; color: var(--signal); display: flex; align-items: baseline; gap: 10px; margin: 0 2px 13px; }
.eyebrow .n { color: var(--faint); }
.block { margin-bottom: 34px; }

.vital { position: relative; overflow: hidden; background: linear-gradient(180deg, var(--panel-2), var(--panel));
  border: 1px solid var(--line); border-radius: var(--r); padding: 16px 18px; }
.vital .k { font-family: var(--f-mono); font-size: 11px; text-transform: uppercase; letter-spacing: 1.2px; color: var(--muted); }
.vital .v { font-family: var(--f-display); font-size: 30px; font-weight: 600; line-height: 1.05; margin-top: 8px;
  font-variant-numeric: tabular-nums; letter-spacing: -.5px; }
.vital .v small { font-size: 15px; color: var(--muted); font-weight: 500; }
.vital .sub { font-size: 12px; color: var(--faint); margin-top: 3px; }
.vital.hero { border-color: var(--signal-line); }
.vital.hero .v { color: var(--signal); }
.vital.hero::after { content: ""; position: absolute; right: -40px; top: -40px; width: 130px; height: 130px;
  background: radial-gradient(circle, var(--signal-dim), transparent 70%); pointer-events: none; }
.vital.good .v { color: var(--signal); }
.vital.warn .v { color: var(--amber); }
.vital.bad .v { color: var(--danger); }
```

- [ ] **Step 8: Create `frontend/src/main.ts`**

```ts
import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './styles/theme.css'

createApp(App).use(router).mount('#app')
```

- [ ] **Step 9: Create `frontend/src/App.vue`**

```vue
<template>
  <router-view />
</template>
```

- [ ] **Step 10: Create `frontend/src/router/index.ts`**

```ts
import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import AdminView from '@/views/AdminView.vue'
import FloorView from '@/views/FloorView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/admin', name: 'admin', component: AdminView },
    { path: '/floor', name: 'floor', component: FloorView },
  ],
})
```

- [ ] **Step 11: Create placeholder views**

`frontend/src/views/DashboardView.vue`, `AdminView.vue`, `FloorView.vue` — each:

```vue
<script setup lang="ts">
const name = 'Dashboard' // 'Admin' / 'Floor' respectively
</script>

<template>
  <div>{{ name }} placeholder</div>
</template>
```

- [ ] **Step 12: Create `frontend/.gitignore`**

```
node_modules
dist
*.tsbuildinfo
```

- [ ] **Step 13: Install and verify dev server**

Run: `cd frontend && npm install`
Expected: installs without error, creates `frontend/package-lock.json`.

Run: `npm run dev -- --port 5173 &` then `curl -s http://localhost:5173/ | grep -o '<title>[^<]*</title>'`
Expected: `<title>QuickRoom</title>`. Stop the dev server after checking (`kill %1` or Ctrl-C).

- [ ] **Step 14: Verify build succeeds**

Run: `npm run build`
Expected: exits 0, creates `backend/internal/api/web/dist/index.html` and `backend/internal/api/web/dist/assets/*.js`.

- [ ] **Step 15: Commit**

```bash
git add frontend/ .gitignore
git commit -m "Scaffold Vue 3 + Vite + TypeScript frontend"
```

(Note: `backend/internal/api/web/` must NOT be committed — add it to root `.gitignore` in Task 9 before this becomes relevant, or add it now: `echo "/backend/internal/api/web/" >> .gitignore`.)

---

### Task 2: API types and client

**Files:**
- Create: `frontend/src/api/types.ts`
- Create: `frontend/src/api/client.ts`

**Interfaces:**
- Consumes: nothing (leaf module).
- Produces: `Room`, `Reservation`, `OccupancyEntry`, `Device`, `Beacon`, `EventEntry`, `Utilization`, `Collision`, `Overstay`, `Notification`, `FloorRoom`, `FloorData` types; and fetch functions `getRooms()`, `getReservations()`, `getOccupancy()`, `getDevices()`, `getBeacons()`, `getEvents(workspaceId, limit?)`, `getUtilization()`, `getCollisions()`, `getOverstays()`, `getNotifications(limit?)`, `getFloorRooms()`, `getInfo()`, `postSync()` — each returning a `Promise` of the typed shape (unwrapped from its envelope object, e.g. `getRooms()` resolves to `Room[]`, not `{rooms: Room[]}`).

- [ ] **Step 1: Write `frontend/src/api/types.ts`**

Field names and types below are taken directly from the Go JSON tags in `backend/internal/domain/domain.go`, `backend/internal/store/sqlite.go`, and the handler response structs in `backend/internal/api/{collision,overstay,utilization,notify,server}.go`.

```ts
export type CheckInStatus = 'not_checked_in' | 'checked_in' | 'checked_out'
export type ReservationStatus = 'booked' | 'no_show' | 'released'

export interface Room {
  room_id: string
  zoom_workspace_id: string
  name: string
  floor: string
  capacity: number
  has_tv: boolean
  is_zoom_room: boolean
  beacon_uuid?: string
  beacon_major?: number
  beacon_minor?: number
}

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
}

export interface OccupancyEntry {
  workspace_id: string
  count: number
  users: string[]
}

export interface Device {
  device_id: string
  display_name: string
  workspace_id: string // '' = not in any room
  last_seen_sec: number
}

export interface Beacon {
  workspace_id: string
  uuid: string
  major: number
  minor: number
  name: string
}

export interface EventEntry {
  kind: 'enter' | 'leave'
  name: string
  actor: string
  ago_sec: number
}

export interface Utilization {
  bookings: number
  checked_in: number
  no_show_released: number
  booked: number
  no_show_rate: number
  rooms_total: number
  rooms_occupied: number
  people_present: number
  generated_at: string
}

export interface Collision {
  workspace_id: string
  room_name: string
  reservation_id: string
  booker: string
  occupants: string[]
  since: string
}

export interface Overstay {
  workspace_id: string
  room_name: string
  reservation_id: string
  booker: string
  occupants: string[]
  ended_at: string
  over_by_sec: number
}

export interface Notification {
  id: number
  type: 'grace_reminder' | 'no_show_released' | 'room_freed' | 'collision' | 'overstay'
  level?: number
  workspace_id?: string
  reservation_id?: string
  recipient?: string
  title: string
  body: string
  created_at: string
}

export interface FloorRoom {
  name: string
  points: number[][]
  kind: 'room' | 'workspace'
  capacity: number
  screens: number
  busy: boolean
}

export interface FloorData {
  rooms: FloorRoom[]
  view_box: { x: number; y: number; w: number; h: number }
  image: { w: number; h: number }
}

export interface Info {
  zoom_mode: string
  authorized: boolean
}
```

- [ ] **Step 2: Write `frontend/src/api/client.ts`**

```ts
import type {
  Room, Reservation, OccupancyEntry, Device, Beacon, EventEntry,
  Utilization, Collision, Overstay, Notification, FloorData, Info,
} from './types'

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${url}: ${res.status}`)
  return res.json() as Promise<T>
}

export const getRooms = () => getJSON<{ rooms: Room[] }>('/rooms').then(d => d.rooms ?? [])
export const getReservations = () => getJSON<{ reservations: Reservation[] }>('/reservations').then(d => d.reservations ?? [])
export const getOccupancy = () => getJSON<{ occupancy: OccupancyEntry[] }>('/occupancy').then(d => d.occupancy ?? [])
export const getDevices = () => getJSON<{ devices: Device[] }>('/devices').then(d => d.devices ?? [])
export const getBeacons = () => getJSON<{ beacons: Beacon[] }>('/beacons').then(d => d.beacons ?? [])
export const getEvents = (workspaceId: string, limit = 25) =>
  getJSON<{ events: EventEntry[] }>(`/events?workspace_id=${encodeURIComponent(workspaceId)}&limit=${limit}`).then(d => d.events ?? [])
export const getUtilization = () => getJSON<Utilization>('/utilization')
export const getCollisions = () => getJSON<{ collisions: Collision[] }>('/collisions').then(d => d.collisions ?? [])
export const getOverstays = () => getJSON<{ overstays: Overstay[] }>('/overstays').then(d => d.overstays ?? [])
export const getNotifications = (limit = 30) => getJSON<{ notifications: Notification[] }>(`/notifications?limit=${limit}`).then(d => d.notifications ?? [])
export const getFloorRooms = () => getJSON<FloorData>('/floor/rooms')
export const getInfo = () => getJSON<Info>('/info')
export const postSync = () => fetch('/sync', { method: 'POST' })
```

- [ ] **Step 3: Verify it compiles**

Run: `cd frontend && npx vue-tsc -b`
Expected: exits 0, no type errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/
git commit -m "Add typed API client for QuickRoom's REST endpoints"
```

---

### Task 3: Composables — `usePoll` and `useConnection`

**Files:**
- Create: `frontend/src/composables/usePoll.ts`
- Create: `frontend/src/composables/useConnection.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `usePoll(fn: () => Promise<void>, intervalMs: number): void` — calls `fn` immediately on `onMounted`, then every `intervalMs`, clearing the interval `onUnmounted`. `useConnection(): { connected: Ref<boolean>, markUp(): void, markDown(): void }`.

- [ ] **Step 1: Write `frontend/src/composables/usePoll.ts`**

```ts
import { onMounted, onUnmounted } from 'vue'

// Runs `fn` immediately, then every `intervalMs`, until the owning component unmounts.
export function usePoll(fn: () => void | Promise<void>, intervalMs: number) {
  let timer: ReturnType<typeof setInterval> | undefined

  onMounted(() => {
    void fn()
    timer = setInterval(() => void fn(), intervalMs)
  })

  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })
}
```

- [ ] **Step 2: Write `frontend/src/composables/useConnection.ts`**

```ts
import { ref } from 'vue'

// Tracks the live/reconnecting chip state shared by all 3 views' fetch loops.
export function useConnection() {
  const connected = ref(false)
  return {
    connected,
    markUp: () => { connected.value = true },
    markDown: () => { connected.value = false },
  }
}
```

- [ ] **Step 3: Verify compile**

Run: `cd frontend && npx vue-tsc -b`
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/composables/
git commit -m "Add usePoll and useConnection composables"
```

---

### Task 4: Shared components — `AppHeader`, `Badge`, `VitalCard`, `DataTable`

**Files:**
- Create: `frontend/src/components/AppHeader.vue`
- Create: `frontend/src/components/Badge.vue`
- Create: `frontend/src/components/VitalCard.vue`
- Create: `frontend/src/components/DataTable.vue`

**Interfaces:**
- `AppHeader` props: `active: 'dashboard' | 'admin' | 'floor' | 'other'`, `connected: boolean`. No emits — nav items are plain `<a>` (cross-page navigation for the 5 static routes; `<router-link>` for the 3 SPA routes).
- `Badge` props: `tone: 'signal' | 'amber' | 'muted' | 'danger'`; default slot = label text.
- `VitalCard` props: `label: string`, `value: string | number`, `sub?: string`, `tone?: 'default' | 'hero' | 'good' | 'warn' | 'bad'`. `sub` slot content may contain HTML (the dashboard's progress bar) — expose a named slot `#sub` as an alternative to the `sub` prop for that case.
- `DataTable` props: `columns: string[]`, `emptyTitle: string`, `emptyBody: string`, `rows: unknown[]` (just used for the empty-check; actual row rendering is via the default slot so callers keep full control of `<tr>`/`<td>` markup, matching how dashboard/admin's tables differ in columns).
- Produces: these 4 components consumed by every view/component in Tasks 5-7.

- [ ] **Step 1: Write `frontend/src/components/AppHeader.vue`**

Port structure and CSS from `dashboard.html:176-194` (markup) and `dashboard.html:42-78` (header/brand/beacon/seg/chip/button CSS — scope it here since `AppHeader` is the only place that markup exists now). Use the full 8-item nav (matching `dashboard.html`'s nav list) on all 3 views, standardizing Admin's currently-trimmed nav.

```vue
<script setup lang="ts">
defineProps<{
  active: 'dashboard' | 'admin' | 'floor' | 'other'
  connected: boolean
}>()
</script>

<template>
  <header>
    <a class="brand" href="/">
      <span class="beacon"><i /></span>
      <div><h1>Quick<b>Room</b></h1><div class="tag">Apple Developer Academy · Bali</div></div>
    </a>
    <nav class="seg">
      <router-link to="/" :class="{ on: active === 'dashboard' }" :aria-current="active === 'dashboard' ? 'page' : undefined">Dashboard</router-link>
      <router-link to="/admin" :class="{ on: active === 'admin' }" :aria-current="active === 'admin' ? 'page' : undefined">Admin</router-link>
      <router-link to="/floor" :class="{ on: active === 'floor' }" :aria-current="active === 'floor' ? 'page' : undefined">Floor plan</router-link>
      <a href="/how">How it works</a>
      <a href="/battery">Battery</a>
      <a href="/hardware">Hardware</a>
      <a href="/scenarios">Scenarios</a>
      <a href="/decide">Next</a>
    </nav>
    <div class="right">
      <span class="chip" :class="connected ? 'live' : 'down'">
        <span class="led" /><span>{{ connected ? 'Live' : 'Reconnecting…' }}</span>
      </span>
    </div>
  </header>
</template>

<style scoped>
header {
  position: sticky; top: 0; z-index: 20;
  display: flex; align-items: center; gap: 16px; flex-wrap: wrap;
  padding: 13px 22px; border-bottom: 1px solid var(--line);
  background: rgba(10,15,31,.72); backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px);
}
.brand { display: flex; align-items: center; gap: 11px; text-decoration: none; color: inherit; }
.beacon { position: relative; width: 11px; height: 11px; flex: none; }
.beacon i { position: absolute; inset: 0; margin: auto; width: 9px; height: 9px; border-radius: 50%;
  background: #2FE6B0; box-shadow: 0 0 12px #2FE6B0; }
.beacon::before, .beacon::after { content: ""; position: absolute; inset: -2px; border-radius: 50%;
  border: 1.5px solid #2FE6B0; opacity: 0; animation: ping 2.6s cubic-bezier(.2,.6,.3,1) infinite; }
.beacon::after { animation-delay: 1.3s; }
@keyframes ping { 0% { transform: scale(.7); opacity: .7; } 80%,100% { transform: scale(2.6); opacity: 0; } }
.brand h1 { font-family: var(--f-display); font-size: 18px; font-weight: 700; letter-spacing: .2px; margin: 0; }
.brand h1 b { color: #2FE6B0; }
.brand .tag { font-size: 11px; color: var(--muted); margin-top: 1px; letter-spacing: .2px; }
.seg { display: flex; gap: 3px; padding: 3px; border: 1px solid var(--line); border-radius: 999px; background: rgba(150,170,220,.06); }
.seg a { font-family: var(--f-mono); font-size: 12px; text-decoration: none; color: var(--muted); padding: 7px 14px; border-radius: 999px; white-space: nowrap; transition: color .15s, background .15s; }
.seg a.on { background: rgba(47,230,176,.14); color: #2FE6B0; }
.seg a:hover:not(.on) { color: var(--text); }
.right { margin-left: auto; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.chip { font-family: var(--f-mono); font-size: 11px; color: var(--muted);
  border: 1px solid var(--line); border-radius: 999px; padding: 5px 11px; display: inline-flex; gap: 6px; align-items: center; }
.chip .led { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); }
.chip.live .led { background: #2FE6B0; box-shadow: 0 0 8px #2FE6B0; }
.chip.down { color: var(--amber); border-color: rgba(244,183,64,.4); }
.chip.down .led { background: var(--amber); box-shadow: 0 0 8px var(--amber); }
@media (max-width: 560px) {
  header { padding: 12px 14px; }
  .brand .tag { display: none; }
  nav.seg { order: 3; width: 100%; flex-wrap: nowrap; overflow-x: auto; -webkit-overflow-scrolling: touch; scrollbar-width: none; }
  nav.seg::-webkit-scrollbar { display: none; }
  .right { order: 2; }
}
</style>
```

- [ ] **Step 2: Write `frontend/src/components/Badge.vue`**

```vue
<script setup lang="ts">
defineProps<{ tone: 'signal' | 'amber' | 'muted' | 'danger' }>()
</script>

<template>
  <span class="badge" :class="`b-${tone}`"><slot /></span>
</template>
```

(`.badge`/`.b-signal`/`.b-amber`/`.b-muted`/`.b-danger` come from `theme.css`, Task 1 Step 7 — no scoped style needed here.)

- [ ] **Step 3: Write `frontend/src/components/VitalCard.vue`**

```vue
<script setup lang="ts">
withDefaults(defineProps<{
  label: string
  value: string | number
  sub?: string
  tone?: 'default' | 'hero' | 'good' | 'warn' | 'bad'
}>(), { tone: 'default' })
</script>

<template>
  <div class="vital" :class="tone !== 'default' ? tone : ''">
    <div class="k">{{ label }}</div>
    <div class="v">{{ value }}</div>
    <div class="sub" v-if="sub || $slots.sub"><slot name="sub">{{ sub }}</slot></div>
  </div>
</template>
```

(`.vital` family comes from `theme.css` — includes `.hero`/`.good`/`.warn`/`.bad` modifiers already.)

- [ ] **Step 4: Write `frontend/src/components/DataTable.vue`**

```vue
<script setup lang="ts">
defineProps<{
  columns: string[]
  rows: unknown[]
  emptyTitle: string
  emptyBody: string
}>()
</script>

<template>
  <div class="card">
    <div class="scroll" v-if="rows.length">
      <table>
        <thead><tr><th v-for="c in columns" :key="c">{{ c }}</th></tr></thead>
        <tbody><slot /></tbody>
      </table>
    </div>
    <div class="empty" v-else><b>{{ emptyTitle }}</b>{{ emptyBody }}</div>
  </div>
</template>
```

- [ ] **Step 5: Verify compile**

Run: `cd frontend && npx vue-tsc -b`
Expected: exits 0.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/AppHeader.vue frontend/src/components/Badge.vue frontend/src/components/VitalCard.vue frontend/src/components/DataTable.vue
git commit -m "Add shared AppHeader, Badge, VitalCard, DataTable components"
```

---

### Task 5: Dashboard view

**Files:**
- Create: `frontend/src/components/dashboard/RoomCard.vue`
- Create: `frontend/src/components/dashboard/OccupancyGrid.vue`
- Modify: `frontend/src/views/DashboardView.vue`

**Interfaces:**
- Consumes: `Room`, `Reservation`, `OccupancyEntry`, `Device`, `Beacon` types and `getRooms`/`getReservations`/`getOccupancy`/`getDevices`/`getBeacons`/`postSync` from `api/client.ts` (Task 2); `usePoll`, `useConnection` (Task 3); `AppHeader`, `VitalCard`, `DataTable`, `Badge` (Task 4).
- `RoomCard` props: `room: Room`, `count: number`, `users: string[]`, `booked?: Reservation`. Internally computes its busy/booked/empty state from these.
- `OccupancyGrid` props: `rooms: Room[]`, `occupancyByWs: Record<string, OccupancyEntry>`, `reservations: Reservation[]`.
- Produces: fully working `/` route matching `dashboard.html`'s behavior (vitals, occupancy grid sorted busy→booked→empty, reservations table, collapsible devices/beacons tables, sync button, 3s polling, live/reconnecting chip).

- [ ] **Step 1: Write `frontend/src/components/dashboard/RoomCard.vue`**

Port state logic from `dashboard.html:302-333` (the `cards.map`/sort/render logic) and CSS from `dashboard.html:100-127` (`.grid`/`.room` family — scoped here, this is the dashboard-specific room-card variant, distinct from Admin's simpler `.room` in Task 6).

```vue
<script setup lang="ts">
import { computed } from 'vue'
import type { Room, Reservation } from '@/api/types'

const props = defineProps<{
  room: Room
  count: number
  users: string[]
  booked?: Reservation
}>()

const state = computed<'busy' | 'booked' | 'empty'>(() =>
  props.count > 0 ? 'busy' : props.booked ? 'booked' : 'empty')

const cap = computed(() => props.room.capacity ? `${props.count}/${props.room.capacity}` : `${props.count}`)
const loc = computed(() => [props.room.floor, props.room.has_tv ? 'Zoom Room' : null].filter(Boolean).join(' · ') || 'Reservation-only')

function fmtTime(s?: string) {
  if (!s) return '—'
  return new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="room" :class="state">
    <div class="top">
      <div><div class="name">{{ room.name }}</div><div class="loc">{{ loc }}</div></div>
      <span class="cap">{{ cap }}</span>
    </div>
    <template v-if="state === 'busy'">
      <div class="state">
        <span class="ico"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg></span>
        In use
        <span class="pill"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ count }}</span>
      </div>
      <div class="who">{{ users.join(', ') }}</div>
    </template>
    <template v-else-if="state === 'booked'">
      <div class="state">
        <span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7.5V12l3 1.8"/></svg></span>
        Booked
      </div>
      <div class="booked-t">{{ fmtTime(booked?.start_time) }}–{{ fmtTime(booked?.end_time) }} · awaiting check-in</div>
    </template>
    <template v-else>
      <div class="state">
        <span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M8.5 12.5l2.4 2.4 4.6-5"/></svg></span>
        Available
      </div>
    </template>
  </div>
</template>

<style scoped>
.room { position: relative; background: linear-gradient(180deg, var(--panel-2), var(--panel));
  border: 1px solid var(--line); border-radius: var(--r); padding: 16px 17px 14px; }
.room.busy { border-color: var(--signal-line); }
.room.booked { border-left: 2px solid var(--amber); }
.room.empty { opacity: .72; border-left: 2px solid var(--open-line); }
.room.busy::after { content: ""; position: absolute; inset: -1px; border-radius: inherit; pointer-events: none;
  border: 1px solid var(--signal); opacity: 0; animation: pulse 2.8s ease-out infinite; }
@keyframes pulse { 0% { opacity: .55; } 70%,100% { opacity: 0; } }
.top { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.name { font-family: var(--f-display); font-weight: 600; font-size: 16px; }
.loc { font-size: 11.5px; color: var(--faint); margin-top: 2px; }
.cap { font-family: var(--f-mono); font-size: 12px; color: var(--muted); white-space: nowrap; }
.state { display: flex; align-items: center; gap: 7px; margin: 13px 0 4px; font-size: 13px; font-weight: 600; }
.state .ico { width: 15px; height: 15px; flex: none; display: inline-flex; }
.state .ico svg { width: 100%; height: 100%; }
.room.busy .state { color: var(--signal); }
.room.booked .state { color: var(--amber); }
.room.empty .state { color: var(--muted); }
.pill { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; font-family: var(--f-mono);
  font-weight: 600; font-size: 13px; font-variant-numeric: tabular-nums; background: var(--signal); color: #06231B;
  padding: 2px 10px; border-radius: 999px; box-shadow: 0 0 10px rgba(47,230,176,.45); }
.pill svg { width: 13px; height: 13px; }
.who { font-size: 12.5px; color: var(--text); min-height: 17px; }
.booked-t { font-size: 12px; color: var(--amber); margin-top: 2px; }
</style>
```

- [ ] **Step 2: Write `frontend/src/components/dashboard/OccupancyGrid.vue`**

Port sort/grouping logic from `dashboard.html:302-312`. CSS `.grid` from `dashboard.html:101`.

```vue
<script setup lang="ts">
import { computed } from 'vue'
import type { Room, Reservation, OccupancyEntry } from '@/api/types'
import RoomCard from './RoomCard.vue'

const props = defineProps<{
  rooms: Room[]
  occupancyByWs: Record<string, OccupancyEntry>
  reservations: Reservation[]
}>()

const cards = computed(() => {
  const list = props.rooms.map(r => {
    const ws = r.zoom_workspace_id
    const o = props.occupancyByWs[ws]
    const count = o?.count ?? 0
    const booked = props.reservations.find(v => v.zoom_workspace_id === ws && v.check_in_status === 'not_checked_in')
    const rank = count > 0 ? 0 : booked ? 1 : 2
    return { room: r, count, users: o?.users ?? [], booked, rank }
  })
  return list.sort((a, b) => a.rank - b.rank || b.count - a.count || a.room.name.localeCompare(b.room.name))
})
</script>

<template>
  <div class="grid" v-if="cards.length">
    <RoomCard v-for="c in cards" :key="c.room.zoom_workspace_id" :room="c.room" :count="c.count" :users="c.users" :booked="c.booked" />
  </div>
  <div class="card" v-else><div class="empty"><b>No rooms yet</b>Sync from Zoom to load your workspaces.</div></div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(258px, 1fr)); gap: 14px; }
</style>
```

- [ ] **Step 3: Write `frontend/src/views/DashboardView.vue`**

Port polling/refresh logic from `dashboard.html:267-333` (data shaping) and `336-368` (devices/beacons/reservations tables), `378-384` (sync), `386-391` (mode). CSS: `main`/`.advanced` from `dashboard.html:80-172`, body background scoped here since only Dashboard/Admin want the gradient (Floor doesn't).

```vue
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import VitalCard from '@/components/VitalCard.vue'
import DataTable from '@/components/DataTable.vue'
import Badge from '@/components/Badge.vue'
import OccupancyGrid from '@/components/dashboard/OccupancyGrid.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getRooms, getReservations, getOccupancy, getDevices, getBeacons, getInfo, postSync } from '@/api/client'
import type { Room, Reservation, OccupancyEntry, Device, Beacon } from '@/api/types'

document.title = 'QuickRoom'

const { connected, markUp, markDown } = useConnection()
const rooms = ref<Room[]>([])
const reservations = ref<Reservation[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const devices = ref<Device[]>([])
const beacons = ref<Beacon[]>([])
const zoomMode = ref('')
const syncing = ref(false)

const STALE_SEC = 120

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})
const present = computed(() => rooms.value.reduce((n, r) => n + (occByWs.value[r.zoom_workspace_id]?.count ?? 0), 0))
const occupied = computed(() => rooms.value.filter(r => (occByWs.value[r.zoom_workspace_id]?.count ?? 0) > 0).length)
const occPct = computed(() => rooms.value.length ? Math.round(occupied.value / rooms.value.length * 100) : 0)
const activeDev = computed(() => devices.value.filter(d => d.last_seen_sec <= STALE_SEC).length)

function roomName(ws: string) {
  return rooms.value.find(r => r.zoom_workspace_id === ws)?.name ?? ws
}
function fmtTime(s?: string) {
  if (!s) return '—'
  return new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
function fmtAgo(sec: number | null | undefined) {
  if (sec == null) return '—'
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  return `${Math.floor(sec / 3600)}h ago`
}
function checkLabel(s: string) {
  return s === 'checked_in' ? 'checked in' : s === 'checked_out' ? 'checked out' : 'awaiting'
}
function checkTone(s: string): 'signal' | 'muted' | 'amber' {
  return s === 'checked_in' ? 'signal' : s === 'checked_out' ? 'muted' : 'amber'
}

async function refresh() {
  try {
    const [r, res, occ, dev, bea] = await Promise.all([
      getRooms(), getReservations(), getOccupancy(), getDevices(), getBeacons(),
    ])
    rooms.value = r; reservations.value = res; occupancy.value = occ; devices.value = dev; beacons.value = bea
    markUp()
  } catch {
    markDown()
  }
}
usePoll(refresh, 3000)

onMounted(async () => {
  try { zoomMode.value = (await getInfo()).zoom_mode } catch { /* leave blank */ }
})

async function sync() {
  syncing.value = true
  try { await postSync() } catch { /* best-effort */ }
  syncing.value = false
  await refresh()
}
</script>

<template>
  <div class="page">
    <AppHeader active="dashboard" :connected="connected" />
    <main>
      <section class="vitals" aria-label="Live vitals">
        <VitalCard label="People present" :value="present" :sub="present === 1 ? '1 person in a room' : 'across all rooms'" tone="hero" />
        <VitalCard label="Rooms occupied" :value="`${occupied}`" tone="default">
          <template #sub><div class="bar"><i :style="{ width: occPct + '%' }" /></div></template>
        </VitalCard>
        <VitalCard label="Active devices" :value="activeDev" :sub="`${devices.length} known · ${devices.length - activeDev} idle`" />
        <VitalCard label="Rooms" :value="rooms.length" :sub="`${beacons.length} beacons mapped`" />
      </section>

      <section class="block">
        <div class="eyebrow">Live occupancy <span class="n">{{ present ? `${present} present` : 'all rooms quiet' }}</span></div>
        <OccupancyGrid :rooms="rooms" :occupancy-by-ws="occByWs" :reservations="reservations" />
      </section>

      <section class="block">
        <div class="eyebrow">Reservations <span class="n">today</span></div>
        <DataTable :columns="['Room', 'Booked by', 'Start', 'End', 'Check-in']" :rows="reservations"
          empty-title="No reservations" empty-body="Nothing booked in the current window.">
          <tr v-for="v in reservations" :key="v.reservation_id">
            <td>{{ roomName(v.zoom_workspace_id) }}</td>
            <td>{{ v.user_email || v.user_id || '—' }}</td>
            <td class="mono">{{ fmtTime(v.start_time) }}</td>
            <td class="mono">{{ fmtTime(v.end_time) }}</td>
            <td><Badge :tone="checkTone(v.check_in_status)">{{ checkLabel(v.check_in_status) }}</Badge></td>
          </tr>
        </DataTable>
      </section>

      <details class="advanced">
        <summary>Advanced · diagnostics</summary>
        <div class="adv-body">
          <div class="adv-row">
            <span class="chip">mode {{ zoomMode }}</span>
            <button class="btn-ghost" :disabled="syncing" @click="sync">{{ syncing ? 'Syncing…' : 'Sync from Zoom' }}</button>
          </div>
          <section class="block">
            <div class="eyebrow">Devices <span class="n">phones reporting presence</span></div>
            <DataTable :columns="['Device', 'Name', 'Room', 'Last seen']" :rows="devices"
              empty-title="No phones yet" empty-body="Open QuickRoom on a phone and turn on auto check-in — it'll appear here within seconds.">
              <tr v-for="d in devices" :key="d.device_id" :class="{ stale: d.last_seen_sec > STALE_SEC }">
                <td class="mono id">{{ d.device_id }}</td>
                <td>{{ d.display_name || '—' }}</td>
                <td><Badge v-if="d.workspace_id" tone="signal">{{ roomName(d.workspace_id) }}</Badge><Badge v-else tone="muted">no room</Badge></td>
                <td class="mono">{{ fmtAgo(d.last_seen_sec) }}</td>
              </tr>
            </DataTable>
          </section>
          <section class="block">
            <div class="eyebrow">Beacons <span class="n">iBeacon identity per room</span></div>
            <DataTable :columns="['Room', 'Workspace', 'UUID', 'Major', 'Minor']" :rows="beacons"
              empty-title="No beacons registered" empty-body="Add a BEACONS_FILE or use the built-in defaults.">
              <tr v-for="b in beacons" :key="b.workspace_id">
                <td>{{ b.name || roomName(b.workspace_id) }}</td>
                <td class="mono id">{{ b.workspace_id }}</td>
                <td class="mono">{{ b.uuid }}</td>
                <td class="num">{{ b.major }}</td>
                <td class="num">{{ b.minor }}</td>
              </tr>
            </DataTable>
          </section>
        </div>
      </details>
      <footer>QuickRoom</footer>
    </main>
  </div>
</template>

<style scoped>
.page {
  background:
    radial-gradient(900px 500px at 82% -8%, rgba(47,230,176,.10), transparent 60%),
    radial-gradient(800px 520px at 10% 0%, rgba(47,230,176,.06), transparent 55%),
    linear-gradient(180deg, var(--ink-2), var(--ink) 38%);
  background-attachment: fixed; min-height: 100vh;
}
main { padding: 26px 24px 60px; max-width: 1180px; margin: 0 auto; }
.vitals { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; margin-bottom: 34px; }
.vital .bar { height: 4px; border-radius: 2px; background: rgba(124,140,170,.18); margin-top: 10px; overflow: hidden; }
.vital .bar i { display: block; height: 100%; background: var(--signal); border-radius: 2px; transition: width .4s ease; }
button { font-family: var(--f-body); font-size: 13px; font-weight: 500; cursor: pointer;
  border-radius: 9px; padding: 8px 14px; border: 1px solid transparent; transition: transform .06s, background .15s, border-color .15s; }
button:active { transform: translateY(1px); }
.btn-ghost { background: transparent; color: var(--text); border-color: var(--line); }
.btn-ghost:hover { border-color: var(--accent); color: #fff; }
.advanced { border-top: 1px solid var(--line); margin-top: 8px; }
.advanced > summary { cursor: pointer; list-style: none; font-family: var(--f-mono); font-size: 11px;
  text-transform: uppercase; letter-spacing: 1.6px; color: var(--muted); padding: 16px 2px; display: flex; align-items: center; gap: 9px; }
.advanced > summary::-webkit-details-marker { display: none; }
.advanced > summary::before { content: "›"; display: inline-block; transition: transform .2s; font-size: 14px; }
.advanced[open] > summary::before { transform: rotate(90deg); }
.advanced > summary:hover { color: var(--text); }
.adv-body { padding-top: 6px; }
.adv-row { display: flex; gap: 10px; align-items: center; margin-bottom: 18px; flex-wrap: wrap; }
footer { text-align: center; color: var(--faint); font-size: 11.5px; font-family: var(--f-mono); padding: 8px 0 0; }
@media (max-width: 860px) { .vitals { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px) { main { padding: 20px 14px 48px; } }
</style>
```

- [ ] **Step 4: Verify against the running backend**

Run: `cd backend && go run ./cmd/quickroom &` (waits for `Listening` in logs), then `cd frontend && npm run dev`.
Open `http://localhost:5173/` in a browser (or `curl -s http://localhost:5173/ | head -5` for a smoke check that it serves HTML).
Expected: vitals populate with the mock-seeded room/reservation, occupancy grid renders, reservations table renders, advanced disclosure toggles devices/beacons tables, "Live" chip shows (not "Reconnecting…").

Stop both processes after checking.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/dashboard/ frontend/src/views/DashboardView.vue
git commit -m "Implement Dashboard view"
```

---

### Task 6: Admin view

**Files:**
- Create: `frontend/src/components/admin/KpiRow.vue`
- Create: `frontend/src/components/admin/AlertsList.vue`
- Create: `frontend/src/components/admin/RoomsGrid.vue`
- Create: `frontend/src/components/admin/NotificationsList.vue`
- Modify: `frontend/src/views/AdminView.vue`

**Interfaces:**
- Consumes: `getUtilization`, `getReservations`, `getRooms`, `getOccupancy`, `getCollisions`, `getOverstays`, `getNotifications` from `api/client.ts`; `usePoll`, `useConnection`; `AppHeader`, `VitalCard`, `DataTable`, `Badge`.
- `KpiRow` props: `util: Utilization`.
- `AlertsList` props: `collisions: Collision[]`, `overstays: Overstay[]`.
- `RoomsGrid` props: `rooms: Room[]`, `occupancyByWs: Record<string, OccupancyEntry>`.
- `NotificationsList` props: `notifications: Notification[]`.
- Produces: fully working `/admin` route matching `admin.html`'s behavior (6 KPIs, alerts, reservations table, rooms grid, notifications feed, 4s polling).

- [ ] **Step 1: Write `frontend/src/components/admin/KpiRow.vue`**

Port from `admin.html:176-182` (markup) — logic ported into the view (Step 5) since it derives `noShowPct`/`noShowClass` from `util`.

```vue
<script setup lang="ts">
import { computed } from 'vue'
import VitalCard from '@/components/VitalCard.vue'
import type { Utilization } from '@/api/types'

const props = defineProps<{ util: Utilization }>()
const noShowPct = computed(() => Math.round((props.util.no_show_rate || 0) * 100))
const noShowTone = computed<'good' | 'warn' | 'bad'>(() => noShowPct.value >= 40 ? 'bad' : noShowPct.value >= 20 ? 'warn' : 'good')
</script>

<template>
  <section class="vitals">
    <VitalCard label="Bookings" :value="util.bookings" />
    <VitalCard label="Checked in" :value="util.checked_in" tone="good" />
    <VitalCard label="Reclaimed" :value="util.no_show_released" />
    <VitalCard label="No-show rate" :value="`${noShowPct}%`" :tone="noShowTone" />
    <VitalCard label="Rooms in use" :value="`${util.rooms_occupied}/${util.rooms_total}`" tone="good" />
    <VitalCard label="People present" :value="util.people_present" tone="good" />
  </section>
</template>

<style scoped>
.vitals { display: grid; grid-template-columns: repeat(6, 1fr); gap: 12px; margin-bottom: 34px; }
@media (max-width: 900px) { .vitals { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 540px) { .vitals { grid-template-columns: repeat(2, 1fr); } }
</style>
```

- [ ] **Step 2: Write `frontend/src/components/admin/AlertsList.vue`**

Port from `admin.html:184-206` and CSS `admin.html:93-105`.

```vue
<script setup lang="ts">
import type { Collision, Overstay } from '@/api/types'

const props = defineProps<{ collisions: Collision[]; overstays: Overstay[] }>()

function fmtDur(sec: number) {
  sec = Math.max(0, sec | 0)
  if (sec < 60) return `${sec}s`
  const m = Math.round(sec / 60)
  if (m < 60) return `${m}m`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}
</script>

<template>
  <div v-if="collisions.length || overstays.length" class="alerts">
    <div v-for="c in collisions" :key="'c' + c.reservation_id" class="alert">
      <div class="ic">!</div>
      <div>
        <div class="t">Room conflict — {{ c.room_name }}</div>
        <div class="d">Booked to <b>{{ c.booker }}</b>, but occupied by <b>{{ (c.occupants || []).join(', ') }}</b>. The booker never showed.</div>
      </div>
    </div>
    <div v-for="o in overstays" :key="'o' + o.reservation_id" class="alert over">
      <div class="ic">◷</div>
      <div>
        <div class="t">Overstay — {{ o.room_name }}</div>
        <div class="d">Booking for <b>{{ o.booker }}</b> ended <b>{{ fmtDur(o.over_by_sec) }} ago</b> but the room is still in use.</div>
      </div>
    </div>
  </div>
  <div v-else class="allclear"><b>All clear.</b> No conflicts or overstays right now.</div>
</template>

<style scoped>
.alerts { display: grid; gap: 12px; }
.alert { display: flex; gap: 13px; align-items: flex-start; padding: 14px 16px; border-radius: var(--r);
  border: 1px solid var(--danger-line); background: var(--danger-dim); }
.alert.over { border-color: var(--amber-line); background: var(--amber-dim); }
.alert .ic { width: 30px; height: 30px; border-radius: 8px; flex: none; display: grid; place-items: center;
  background: rgba(255,107,107,.18); color: var(--danger); font-weight: 700; font-family: var(--f-display); }
.alert.over .ic { background: rgba(244,183,64,.2); color: var(--amber); }
.alert .t { font-weight: 600; }
.alert .d { color: var(--muted); font-size: 13px; margin-top: 2px; }
.alert .d b { color: var(--text); font-weight: 600; }
.allclear { padding: 18px 16px; color: var(--muted); text-align: center; border: 1px dashed var(--line); border-radius: var(--r); }
.allclear b { color: var(--signal); }
</style>
```

- [ ] **Step 3: Write `frontend/src/components/admin/RoomsGrid.vue`**

Port from `admin.html:230-241` and CSS `admin.html:125-137`.

```vue
<script setup lang="ts">
import type { Room, OccupancyEntry } from '@/api/types'

const props = defineProps<{ rooms: Room[]; occupancyByWs: Record<string, OccupancyEntry> }>()

function occCount(ws: string) { return props.occupancyByWs[ws]?.count ?? 0 }
function occUsers(ws: string) { return props.occupancyByWs[ws]?.users?.join(', ') ?? '' }
</script>

<template>
  <div class="rooms">
    <div v-for="rm in rooms" :key="rm.zoom_workspace_id" class="room" :class="{ busy: occCount(rm.zoom_workspace_id) > 0 }">
      <div class="rn">{{ rm.name }} <span class="dot" :class="{ on: occCount(rm.zoom_workspace_id) > 0 }" /></div>
      <div class="head"><span class="c">{{ occCount(rm.zoom_workspace_id) }}</span><span class="cap">/ {{ rm.capacity }} seats</span></div>
      <div class="who">{{ occUsers(rm.zoom_workspace_id) || 'empty' }}</div>
    </div>
  </div>
</template>

<style scoped>
.rooms { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.room { background: linear-gradient(180deg, var(--panel-2), var(--panel)); border: 1px solid var(--line);
  border-radius: var(--r); padding: 14px 15px; }
.room.busy { border-color: var(--signal-line); }
.rn { font-weight: 600; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head { display: flex; align-items: baseline; gap: 8px; margin-top: 10px; }
.head .c { font-family: var(--f-display); font-size: 26px; font-weight: 700; line-height: 1; }
.room.busy .head .c { color: var(--signal); }
.head .cap { font-size: 12px; color: var(--muted); }
.who { margin-top: 9px; font-size: 12px; color: var(--muted); min-height: 16px; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); display: inline-block; }
.dot.on { background: var(--signal); box-shadow: 0 0 7px var(--signal); }
</style>
```

- [ ] **Step 4: Write `frontend/src/components/admin/NotificationsList.vue`**

Port from `admin.html:244-262` and CSS `admin.html:139-148`.

```vue
<script setup lang="ts">
import type { Notification } from '@/api/types'

defineProps<{ notifications: Notification[] }>()

function noteTone(t: string): 'danger' | 'amber' | 'muted' | 'signal' {
  return t === 'collision' ? 'danger' : t === 'overstay' ? 'amber' : t === 'no_show_released' ? 'muted' : t === 'room_freed' ? 'signal' : 'amber'
}
function noteLabel(t: string) { return (t || '').replace(/_/g, ' ') }
function fmtClock(s: string) {
  return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''
}
</script>

<template>
  <div class="notes">
    <div v-for="n in notifications" :key="n.id" class="note">
      <span class="badge" :class="`b-${noteTone(n.type)}`">{{ noteLabel(n.type) }}</span>
      <div>
        <div class="nt">{{ n.title }}</div>
        <div class="nb">{{ n.body }}</div>
      </div>
      <div class="meta">
        <div class="muted" style="font-size:12px">{{ n.recipient || 'broadcast' }}</div>
        <div class="time">{{ fmtClock(n.created_at) }}</div>
      </div>
    </div>
    <div v-if="!notifications.length" class="empty">No notifications yet.</div>
  </div>
</template>

<style scoped>
.notes { display: grid; gap: 8px; }
.note { display: flex; gap: 12px; align-items: flex-start; padding: 11px 14px; border: 1px solid var(--line);
  border-radius: 11px; background: rgba(150,170,220,.03); }
.note .nt { font-weight: 600; font-size: 13px; }
.note .nb { color: var(--muted); font-size: 12.5px; margin-top: 1px; }
.note .meta { margin-left: auto; text-align: right; flex: none; }
.note .meta .time { font-family: var(--f-mono); font-size: 10.5px; color: var(--faint); }
</style>
```

- [ ] **Step 5: Write `frontend/src/views/AdminView.vue`**

```vue
<script setup lang="ts">
import { ref, computed } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import DataTable from '@/components/DataTable.vue'
import Badge from '@/components/Badge.vue'
import KpiRow from '@/components/admin/KpiRow.vue'
import AlertsList from '@/components/admin/AlertsList.vue'
import RoomsGrid from '@/components/admin/RoomsGrid.vue'
import NotificationsList from '@/components/admin/NotificationsList.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getUtilization, getReservations, getRooms, getOccupancy, getCollisions, getOverstays, getNotifications } from '@/api/client'
import type { Utilization, Reservation, Room, OccupancyEntry, Collision, Overstay, Notification } from '@/api/types'

document.title = 'QuickRoom · Admin'

const { connected, markUp, markDown } = useConnection()
const util = ref<Utilization>({ bookings: 0, checked_in: 0, no_show_released: 0, booked: 0, no_show_rate: 0, rooms_total: 0, rooms_occupied: 0, people_present: 0, generated_at: '' })
const reservations = ref<Reservation[]>([])
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const collisions = ref<Collision[]>([])
const overstays = ref<Overstay[]>([])
const notifications = ref<Notification[]>([])
const loaded = ref(false)

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})

function roomName(ws: string) { return rooms.value.find(r => r.zoom_workspace_id === ws)?.name ?? ws }
function occCount(ws: string) { return occByWs.value[ws]?.count ?? 0 }
function fmtTime(s?: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
function statusTone(s: string): 'muted' | 'danger' | 'signal' { return s === 'released' ? 'muted' : s === 'no_show' ? 'danger' : 'signal' }
function checkTone(s: string): 'signal' | 'muted' | 'amber' { return s === 'checked_in' ? 'signal' : s === 'checked_out' ? 'muted' : 'amber' }
function checkLabel(s: string) { return s === 'checked_in' ? 'checked in' : s === 'checked_out' ? 'checked out' : 'awaiting' }

async function refresh() {
  try {
    const [u, res, r, occ, col, over, notes] = await Promise.all([
      getUtilization(), getReservations(), getRooms(), getOccupancy(), getCollisions(), getOverstays(), getNotifications(30),
    ])
    util.value = u; reservations.value = res; rooms.value = r; occupancy.value = occ
    collisions.value = col; overstays.value = over; notifications.value = notes
    markUp(); loaded.value = true
  } catch {
    markDown()
  }
}
usePoll(refresh, 4000)
</script>

<template>
  <div class="page">
    <AppHeader active="admin" :connected="connected" />
    <main>
      <div v-if="!loaded" class="skeleton">Loading admin data…</div>
      <template v-else>
        <KpiRow :util="util" />

        <section class="block">
          <div class="eyebrow"><span class="n">01</span> Needs attention
            <span class="aside">{{ collisions.length + overstays.length }} open</span>
          </div>
          <AlertsList :collisions="collisions" :overstays="overstays" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">02</span> Reservations</div>
          <DataTable :columns="['Room', 'Booker', 'Window', 'Status', 'Check-in', 'Present']" :rows="reservations"
            empty-title="No reservations" empty-body="No reservations in the window.">
            <tr v-for="r in reservations" :key="r.reservation_id">
              <td class="room-cell">{{ roomName(r.zoom_workspace_id) }}</td>
              <td class="muted">{{ r.user_email || r.user_id || '—' }}</td>
              <td class="mono muted">{{ fmtTime(r.start_time) }}–{{ fmtTime(r.end_time) }}</td>
              <td><Badge :tone="statusTone(r.status)">{{ r.status }}</Badge></td>
              <td><Badge :tone="checkTone(r.check_in_status)">{{ checkLabel(r.check_in_status) }}</Badge></td>
              <td class="mono">{{ occCount(r.zoom_workspace_id) }}</td>
            </tr>
          </DataTable>
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">03</span> Rooms &amp; occupancy <span class="aside">live headcount</span></div>
          <RoomsGrid :rooms="rooms" :occupancy-by-ws="occByWs" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">04</span> Notification outbox <span class="aside">{{ notifications.length }} recent</span></div>
          <NotificationsList :notifications="notifications" />
        </section>
      </template>
    </main>
  </div>
</template>

<style scoped>
.page {
  background:
    radial-gradient(900px 500px at 82% -8%, rgba(47,230,176,.10), transparent 60%),
    radial-gradient(800px 520px at 10% 0%, rgba(47,230,176,.06), transparent 55%),
    linear-gradient(180deg, var(--ink-2), var(--ink) 38%);
  background-attachment: fixed; min-height: 100vh;
}
main { padding: 26px 24px 60px; max-width: 1180px; margin: 0 auto; }
.eyebrow .aside { margin-left: auto; text-transform: none; letter-spacing: normal; color: var(--muted); font-size: 11px; }
.room-cell { font-weight: 600; }
.muted { color: var(--muted); }
.skeleton { color: var(--faint); text-align: center; padding: 40px; }
</style>
```

- [ ] **Step 6: Verify against the running backend**

Run: `cd backend && go run ./cmd/quickroom &`, `cd frontend && npm run dev`, open `http://localhost:5173/admin`.
Expected: 6 KPI tiles populate, "All clear" (or real alerts) renders, reservations/rooms/notifications sections render, no console errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/admin/ frontend/src/views/AdminView.vue
git commit -m "Implement Admin view"
```

---

### Task 7: Floor plan view

**Files:**
- Create: `frontend/src/components/floor/FloorPlanCanvas.vue`
- Create: `frontend/src/components/floor/RoomDetailModal.vue`
- Modify: `frontend/src/views/FloorView.vue`

**Interfaces:**
- Consumes: `getFloorRooms`, `getRooms`, `getOccupancy`, `getEvents`, `getReservations` from `api/client.ts`; `usePoll`, `useConnection`.
- `FloorPlanCanvas` props: `floorData: FloorData | null`, `occupancyByName: Record<string, number>` (normalized room name → count). Emits `roomClick(name: string)`.
- `RoomDetailModal` props: `open: boolean`, `roomName: string | null`, `room: Room | null`, `occupancy: OccupancyEntry | null`. Emits `close()`. Internally fetches events/reservation for the room when `open` becomes true (matches original's fetch-on-open behavior).
- Produces: fully working `/floor` route matching `floor.html`'s SVG overlay, fit-to-screen scaling, legend, and room detail modal.

This is the highest-risk port — the viewBox/fit-to-screen math must be copied exactly, not re-derived, or room polygons will misalign with the floor image.

- [ ] **Step 1: Write `frontend/src/components/floor/FloorPlanCanvas.vue`**

Port `fit()` from `floor.html:224-234`, `build()`/`paint()` polygon+label rendering from `floor.html:246-316` (adapted to Vue's reactive rendering instead of manual DOM string-building — same math, same data flow), and all CSS from `floor.html:60-114`.

```vue
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import type { FloorData, FloorRoom } from '@/api/types'

const props = defineProps<{
  floorData: FloorData | null
  occupancyByName: Record<string, number>
}>()
const emit = defineEmits<{ roomClick: [name: string] }>()

const NAT_W = 2489, NAT_H = 1380
// Bound dynamically (not a static `src="..."`) so Vue's asset-URL transform
// doesn't try to resolve it as a local file — it's a backend API route.
const floorImageSrc = '/floor/image'

const vp = ref<HTMLElement | null>(null)
const stage = ref<HTMLElement | null>(null)
const transform = ref('')

function norm(s: string) { return String(s || '').toLowerCase().replace(/\s+/g, ' ').trim() }

const vb = computed(() => props.floorData?.view_box ?? { x: 1.9, y: 153.0, w: 1209.3, h: 682.0 })
const rooms = computed<FloorRoom[]>(() => props.floorData?.rooms ?? [])

function centroid(pts: number[][]): [number, number] {
  let x = 0, y = 0
  for (const p of pts) { x += p[0]; y += p[1] }
  return [x / pts.length, y / pts.length]
}

const cells = computed(() => rooms.value.map(rm => {
  const [cx, cy] = centroid(rm.points)
  const left = ((cx - vb.value.x) / vb.value.w) * NAT_W
  const top = ((cy - vb.value.y) / vb.value.h) * NAT_H
  const count = props.occupancyByName[norm(rm.name)] ?? 0
  return { room: rm, points: rm.points.map(p => p.join(',')).join(' '), left, top, count, busy: count > 0 }
}))

const busyCount = computed(() => cells.value.filter(c => c.busy).length)

function fit() {
  if (!vp.value || !stage.value) return
  const r = vp.value.getBoundingClientRect()
  const pad = r.width < 560 ? 12 : 28
  const scale = Math.min((r.width - pad * 2) / NAT_W, (r.height - pad * 2) / NAT_H)
  const x = (r.width - NAT_W * scale) / 2
  const freeY = r.height - NAT_H * scale
  const y = freeY > 0 ? Math.min(freeY / 2, r.height * 0.1 + pad) : freeY / 2
  transform.value = `translate(${x}px,${y}px) scale(${scale})`
}

watch(() => props.floorData, () => nextTick(fit))
onMounted(() => { window.addEventListener('resize', fit); fit() })
onUnmounted(() => window.removeEventListener('resize', fit))
</script>

<template>
  <div class="viewport" ref="vp">
    <div class="stage" ref="stage" :style="{ transform }">
      <img class="floorimg" :src="floorImageSrc" alt="Floor plan of Apple Developer Academy Bali" draggable="false" />
      <div class="scrim" />
      <svg class="overlay" :viewBox="`${vb.x} ${vb.y} ${vb.w} ${vb.h}`" preserveAspectRatio="none" aria-hidden="true">
        <polygon v-for="c in cells" :key="c.room.name" :points="c.points" :class="{ busy: c.busy }"
          role="img" :aria-label="`${c.room.name}, ${c.busy ? c.count + (c.count === 1 ? ' person' : ' people') : 'available'}`"
          @click="emit('roomClick', c.room.name)" />
      </svg>
      <div class="labels" aria-live="polite" aria-label="Rooms">
        <div v-for="c in cells" :key="c.room.name" class="lbl" :class="{ busy: c.busy, sm: c.room.kind !== 'room' }"
          :style="{ left: c.left + 'px', top: c.top + 'px' }" tabindex="0" role="button"
          :aria-label="`${c.room.name} — details`"
          @click="emit('roomClick', c.room.name)"
          @keydown.enter.prevent="emit('roomClick', c.room.name)"
          @keydown.space.prevent="emit('roomClick', c.room.name)">
          <span class="ico">
            <svg v-if="c.room.kind === 'room'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="7" width="18" height="13" rx="2"/><path d="M8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
          </span>
          <span class="nm">{{ c.room.name }}</span>
          <span class="chip-slot">
            <span v-if="c.busy" class="pill"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ c.count }}</span>
            <span v-else class="ghost">Open</span>
          </span>
        </div>
      </div>
    </div>
    <div class="legend" aria-label="Legend">
      <span class="lg-item"><span class="sw sw-busy" />Occupied <em>{{ busyCount }}</em></span>
      <span class="lg-item"><span class="sw sw-open" />Open <em>{{ cells.length - busyCount }}</em></span>
    </div>
  </div>
</template>

<style scoped>
.legend { position: absolute; left: 16px; bottom: 16px; z-index: 4;
  display: flex; gap: 14px; align-items: center; font-size: 12px; color: var(--muted);
  background: rgba(17,26,51,.82); border: 1px solid var(--line); border-radius: 12px;
  padding: 9px 13px; backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px); }
.lg-item { display: inline-flex; align-items: center; gap: 7px; }
.lg-item em { font-style: normal; font-family: var(--f-mono); color: var(--text); }
.sw { width: 22px; height: 14px; border-radius: 4px; display: inline-block; }
.sw-busy { background: var(--signal-dim); border: 2px solid var(--signal); box-shadow: 0 0 7px rgba(47,230,176,.5); }
.sw-open { background: transparent; border: 1.5px dashed rgba(176,190,224,.6); }
.viewport { position: relative; flex: 1; overflow: hidden; background: var(--ink); min-height: 0; }
.stage { position: absolute; top: 0; left: 0; width: 2489px; height: 1380px; transform-origin: 0 0; }
.floorimg { display: block; width: 2489px; height: 1380px; user-select: none; -webkit-user-drag: none;
  filter: grayscale(.85) brightness(.5) contrast(1.1); }
.scrim { position: absolute; inset: 0; pointer-events: none;
  background: radial-gradient(120% 90% at 50% 42%, rgba(10,15,31,.40), rgba(10,15,31,.66)); }
.overlay { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
.overlay polygon { fill: var(--open-fill); stroke: var(--open-line); stroke-width: 1.5;
  stroke-dasharray: 7 5; vector-effect: non-scaling-stroke; pointer-events: all; cursor: pointer;
  transition: fill .18s ease, stroke .18s ease; }
.overlay polygon:hover { fill: rgba(176,190,224,.20); }
.overlay polygon.busy:hover { fill: rgba(47,230,176,.42); }
.overlay polygon.busy { fill: var(--signal-dim); stroke: var(--signal-line); stroke-width: 2.5;
  stroke-dasharray: none; filter: drop-shadow(0 0 7px rgba(47,230,176,.55));
  animation: liveGlow 2.8s ease-in-out infinite; }
@keyframes liveGlow { 0%,100% { filter: drop-shadow(0 0 5px rgba(47,230,176,.40)); } 50% { filter: drop-shadow(0 0 11px rgba(47,230,176,.70)); } }
.labels { position: absolute; inset: 0; pointer-events: none; }
.lbl { position: absolute; transform: translate(-50%,-50%); display: flex; flex-direction: column;
  align-items: center; gap: 5px; text-align: center; max-width: 172px; pointer-events: auto; cursor: pointer;
  background: rgba(8,12,24,.46); backdrop-filter: blur(5px); -webkit-backdrop-filter: blur(5px);
  border-radius: 11px; padding: 6px 10px; box-shadow: 0 2px 8px rgba(7,11,22,.45); transition: background .15s; }
.lbl:hover { background: rgba(8,12,24,.68); }
.lbl .ico { width: 25px; height: 25px; opacity: .9; color: var(--open-text); }
.lbl .ico svg { width: 100%; height: 100%; }
.lbl .nm { font-family: var(--f-display); font-weight: 600; font-size: 20px; line-height: 1.12; color: var(--open-text); }
.lbl.busy .ico, .lbl.busy .nm { color: #fff; }
.lbl.sm { padding: 5px 8px; max-width: 130px; }
.lbl.sm .nm { font-size: 15px; }
.lbl.sm .ico { width: 18px; height: 18px; }
.pill { display: inline-flex; align-items: center; gap: 5px; font-family: var(--f-mono); font-weight: 600;
  font-size: 15px; font-variant-numeric: tabular-nums; background: var(--signal); color: #06231B;
  padding: 3px 11px; border-radius: 999px; box-shadow: 0 0 12px rgba(47,230,176,.55); }
.pill svg { width: 14px; height: 14px; }
.ghost { font-family: var(--f-mono); font-size: 13px; color: var(--open-text);
  padding: 2px 10px; border: 1px dashed rgba(176,190,224,.5); border-radius: 999px; }
.lbl.sm .pill { font-size: 13px; padding: 2px 9px; }
.lbl.sm .ghost { font-size: 11px; }
@media (max-width: 560px) {
  .legend { font-size: 11px; gap: 10px; padding: 6px 11px; left: 12px; bottom: 12px; }
}
</style>
```

- [ ] **Step 2: Write `frontend/src/components/floor/RoomDetailModal.vue`**

Port from `floor.html:189-201` (markup) and `318-372` (`openRoom`/`updateModal` logic — adapted to a `watch` on `open`/`roomName`) and CSS `floor.html:116-144`.

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { getEvents, getReservations } from '@/api/client'
import type { Room, OccupancyEntry, Reservation, EventEntry } from '@/api/types'

const props = defineProps<{
  open: boolean
  roomName: string | null
  room: Room | null
  occupancy: OccupancyEntry | null
}>()
const emit = defineEmits<{ close: [] }>()

const booking = ref<Reservation | null>(null)
const events = ref<EventEntry[]>([])
const loading = ref(false)

function fmtTime(s?: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
function fmtAgoShort(s: number) {
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  return `${Math.floor(s / 86400)}d`
}

watch(() => [props.open, props.room?.zoom_workspace_id], async ([open]) => {
  if (!open || !props.room) { booking.value = null; events.value = []; return }
  const ws = props.room.zoom_workspace_id
  loading.value = true
  try {
    const [ev, resv] = await Promise.all([getEvents(ws, 25), getReservations()])
    if (!props.open || props.room?.zoom_workspace_id !== ws) return // closed/switched while fetching
    events.value = ev
    booking.value = resv.find(v => v.zoom_workspace_id === ws) ?? null
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="modal" v-if="open" role="presentation">
    <div class="modal-bg" @click="emit('close')" />
    <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="m-name">
      <button class="sheet-x" aria-label="Close" @click="emit('close')">✕</button>
      <div class="sheet-head">
        <div>
          <h2 id="m-name">{{ roomName }}</h2>
          <div class="sheet-sub">{{ [room?.has_tv ? 'Zoom Room' : 'Reservation-only', room?.capacity ? `Capacity ${room.capacity}` : ''].filter(Boolean).join('  ·  ') }}</div>
        </div>
        <span v-if="(occupancy?.count ?? 0) > 0" class="pill"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ occupancy?.count }}</span>
        <span v-else class="ghost">Open</span>
      </div>
      <div class="m-sec">
        <div class="m-h">Inside now</div>
        <div v-if="(occupancy?.count ?? 0) > 0">
          <span v-for="u in occupancy?.users ?? []" :key="u" class="who-chip">
            <svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ u }}
          </span>
        </div>
        <div v-else class="m-empty">No one here right now.</div>
      </div>
      <div class="m-sec">
        <div class="m-h">Booking</div>
        <div v-if="!room" class="m-empty">—</div>
        <div v-else-if="booking">
          <div class="m-line"><strong>{{ booking.user_email || booking.user_id || '—' }}</strong></div>
          <div class="m-sub2">{{ fmtTime(booking.start_time) }}–{{ fmtTime(booking.end_time) }} · {{ booking.check_in_status.replace(/_/g, ' ') }}</div>
        </div>
        <div v-else class="m-empty">No booking today.</div>
      </div>
      <div class="m-sec">
        <div class="m-h">Recent activity</div>
        <div v-if="events.length">
          <div v-for="(e, i) in events" :key="i" class="act" :class="e.kind === 'enter' ? 'enter' : 'leave'">
            <span class="dot" />
            <span>{{ e.name || e.actor }} {{ e.kind === 'enter' ? 'entered' : 'left' }}</span>
            <span class="ago">{{ fmtAgoShort(e.ago_sec) }} ago</span>
          </div>
        </div>
        <div v-else class="m-empty">No activity recorded yet.</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal { position: fixed; inset: 0; z-index: 50; display: flex; align-items: center; justify-content: center; padding: 20px; }
.modal-bg { position: absolute; inset: 0; background: rgba(6,9,18,.62); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px); }
.sheet { position: relative; width: 100%; max-width: 430px; max-height: 84vh; overflow: auto;
  background: linear-gradient(180deg, #16203C, #111A33); border: 1px solid var(--line);
  border-radius: 18px; padding: 20px; box-shadow: 0 24px 60px rgba(0,0,0,.5); animation: sheetIn .18s ease; }
@keyframes sheetIn { from { opacity: 0; transform: translateY(10px) scale(.985); } to { opacity: 1; transform: none; } }
.sheet-x { position: absolute; top: 13px; right: 13px; width: 30px; height: 30px; border-radius: 50%;
  border: 1px solid var(--line); background: rgba(255,255,255,.04); color: var(--muted); font-size: 17px; line-height: 1; cursor: pointer; }
.sheet-x:hover { color: #fff; border-color: var(--accent); }
.sheet-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; padding-right: 34px; }
.sheet-head h2 { font-family: var(--f-display); font-size: 21px; margin: 0; }
.sheet-sub { font-size: 12px; color: var(--muted); margin-top: 3px; font-family: var(--f-mono); }
.m-sec { margin-top: 18px; }
.m-h { font-family: var(--f-mono); font-size: 10.5px; text-transform: uppercase; letter-spacing: 1.5px; color: var(--muted); margin-bottom: 9px; }
.who-chip { display: inline-flex; align-items: center; gap: 6px; background: rgba(47,230,176,.12);
  border: 1px solid var(--signal-line); color: #DFF6EE; border-radius: 999px; padding: 4px 11px; font-size: 13px; margin: 0 6px 7px 0; }
.who-chip svg { width: 13px; height: 13px; }
.m-empty { color: var(--faint); font-size: 13px; }
.m-line strong { font-weight: 600; }
.m-sub2 { font-size: 12px; color: var(--muted); margin-top: 2px; font-family: var(--f-mono); }
.act { display: flex; align-items: center; gap: 10px; padding: 8px 0; border-bottom: 1px solid rgba(150,170,220,.07); font-size: 13px; }
.act:last-child { border-bottom: 0; }
.act .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
.act.enter .dot { background: var(--signal); box-shadow: 0 0 7px var(--signal); }
.act.leave .dot { background: rgba(176,190,224,.7); }
.act .ago { margin-left: auto; color: var(--faint); font-family: var(--f-mono); font-size: 11.5px; }
.pill { display: inline-flex; align-items: center; gap: 5px; font-family: var(--f-mono); font-weight: 600;
  font-size: 15px; background: var(--signal); color: #06231B; padding: 3px 11px; border-radius: 999px; }
.pill svg { width: 14px; height: 14px; }
.ghost { font-family: var(--f-mono); font-size: 13px; color: var(--open-text);
  padding: 2px 10px; border: 1px dashed rgba(176,190,224,.5); border-radius: 999px; }
@media (max-width: 560px) { .modal { padding: 12px; align-items: flex-end; } .sheet { max-height: 88vh; } }
</style>
```

- [ ] **Step 3: Write `frontend/src/views/FloorView.vue`**

```vue
<script setup lang="ts">
import { ref, computed } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import FloorPlanCanvas from '@/components/floor/FloorPlanCanvas.vue'
import RoomDetailModal from '@/components/floor/RoomDetailModal.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getFloorRooms, getRooms, getOccupancy } from '@/api/client'
import type { FloorData, Room, OccupancyEntry } from '@/api/types'

document.title = 'QuickRoom · Floor plan'

const { connected, markUp, markDown } = useConnection()
const floorData = ref<FloorData | null>(null)
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const openRoomName = ref<string | null>(null)

function norm(s: string) { return String(s || '').toLowerCase().replace(/\s+/g, ' ').trim() }

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})
const occupancyByName = computed(() => {
  const m: Record<string, number> = {}
  for (const r of rooms.value) m[norm(r.name)] = occByWs.value[r.zoom_workspace_id]?.count ?? 0
  return m
})
const openRoom = computed(() => rooms.value.find(r => norm(r.name) === norm(openRoomName.value ?? '')) ?? null)
const openRoomOcc = computed(() => openRoom.value ? occByWs.value[openRoom.value.zoom_workspace_id] ?? null : null)

async function loadFloor() {
  try { floorData.value = await getFloorRooms() } catch { /* keep last-known layout */ }
}
async function refresh() {
  try {
    const [r, occ] = await Promise.all([getRooms(), getOccupancy()])
    rooms.value = r; occupancy.value = occ
    markUp()
  } catch {
    markDown()
  }
}
loadFloor()
usePoll(refresh, 3000)
</script>

<template>
  <div class="page">
    <AppHeader active="floor" :connected="connected" />
    <FloorPlanCanvas :floor-data="floorData" :occupancy-by-name="occupancyByName" @room-click="name => openRoomName = name" />
    <RoomDetailModal :open="openRoomName !== null" :room-name="openRoomName" :room="openRoom" :occupancy="openRoomOcc" @close="openRoomName = null" />
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; flex: 1; min-height: 0; background: var(--ink); }
</style>
```

Note: `FloorView`'s `.page` relies on its parent chain (`App.vue` → `#app`) being a full-height flex column so `flex:1; min-height:0` actually fills the viewport below the header. Add this to `frontend/src/styles/theme.css` (Task 1 Step 7) if not already effectively covered — append:

```css
html, body, #app { height: 100%; }
#app { display: flex; flex-direction: column; }
```

- [ ] **Step 4: Verify against the running backend**

Run: `cd backend && go run ./cmd/quickroom &`, `cd frontend && npm run dev`, open `http://localhost:5173/floor`.
Expected: floor image renders full-bleed below the header, room polygons align with the image, legend counts are correct, clicking a room opens the modal with inside-now/booking/activity sections, closing works, resizing the window keeps the plan fit-to-screen.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/floor/ frontend/src/views/FloorView.vue frontend/src/styles/theme.css
git commit -m "Implement Floor plan view"
```

---

### Task 8: Go backend wiring — embed the SPA, replace the 3 HTML handlers

**Files:**
- Modify: `backend/internal/api/server.go`
- Delete: `backend/internal/api/dashboard.html`, `backend/internal/api/admin.html`, `backend/internal/api/floor.html`
- Create: `backend/internal/api/spa.go`

**Interfaces:**
- Consumes: `backend/internal/api/web/dist/` (built by Task 1's `npm run build`, must exist before `go build`/`go test` run — the plan's Step 3 below verifies this ordering).
- Produces: `GET /`, `GET /admin`, `GET /floor` serve the SPA shell; `GET /assets/*` serves the built JS/CSS.

- [ ] **Step 1: Remove the old embeds and handlers from `server.go`**

In `backend/internal/api/server.go`, delete these `go:embed` declarations (lines 23-45 in the current file):

```go
//go:embed dashboard.html
var dashboardHTML []byte

//go:embed floor.html
var floorHTML []byte
```

(Keep `how.html`, `battery.html`, `hardware.html`, `scenarios.html`, `decide.html`, `admin.html`'s embed removal — wait, `admin.html` is also being removed:)

```go
//go:embed admin.html
var adminHTML []byte
```

Delete the `dashboard`, `floor`, and `admin` handler functions (current lines 319-322, 330-333, 360-363):

```go
func (s *Server) dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}
```
```go
func (s *Server) floor(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(floorHTML)
}
```
```go
func (s *Server) admin(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(adminHTML)
}
```

In `Handler()`, replace these three lines:

```go
	mux.HandleFunc("GET /{$}", s.dashboard)
```
```go
	mux.HandleFunc("GET /floor", s.floor)
```
```go
	mux.HandleFunc("GET /admin", s.admin)
```

with:

```go
	mux.HandleFunc("GET /{$}", s.spaIndex)
	mux.HandleFunc("GET /admin", s.spaIndex)
	mux.HandleFunc("GET /floor", s.spaIndex)
	mux.Handle("GET /assets/", http.FileServerFS(webDist))
```

(Keep `/how`, `/battery`, `/hardware`, `/scenarios`, `/decide`, `/floor/image`, `/floor/rooms` registrations exactly as they are — those handlers and their embeds are untouched.)

- [ ] **Step 2: Create `backend/internal/api/spa.go`**

```go
package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// webDist holds the built Vue 3 SPA (frontend/, built via `npm run build`,
// which Vite is configured to output straight into web/dist so go:embed —
// which cannot reach outside its package directory — can pick it up).
//
//go:embed web/dist
var webDistRaw embed.FS

// webDist strips the "web/dist" prefix so /assets/foo.js maps to
// web/dist/assets/foo.js on disk, matching Vite's build output layout.
var webDist = mustSub(webDistRaw, "web/dist")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // build-time invariant: web/dist always exists before `go build`
	}
	return sub
}

// spaIndex serves the built SPA shell for /, /admin, and /floor. Vue Router's
// history mode takes over client-side routing from there.
func (s *Server) spaIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, webDist, "index.html")
}
```

- [ ] **Step 3: Build the frontend before building Go (required — `go:embed` needs the directory to exist)**

Run: `cd frontend && npm run build`
Expected: produces `backend/internal/api/web/dist/index.html` and `backend/internal/api/web/dist/assets/*`.

- [ ] **Step 4: Delete the old HTML files**

```bash
git rm backend/internal/api/dashboard.html backend/internal/api/admin.html backend/internal/api/floor.html
```

- [ ] **Step 5: Build and test the Go backend**

Run: `cd backend && go build ./...`
Expected: exits 0. (If it fails with "pattern web/dist: no matching files found", re-run Task 8 Step 3 first — the embed directory must exist at build time.)

Run: `cd backend && go test ./...`
Expected: all existing tests pass unchanged (no test references the deleted HTML files or handlers — confirmed during planning by grepping `server_test.go`).

- [ ] **Step 6: Manual end-to-end check**

Run: `cd backend && go run ./cmd/quickroom &`
Run: `curl -s http://localhost:8080/ | grep -o '<title>[^<]*</title>'` → expect `<title>QuickRoom</title>`
Run: `curl -s http://localhost:8080/admin | grep -o '<title>[^<]*</title>'` → expect `<title>QuickRoom</title>` (same SPA shell)
Run: `curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/assets/$(curl -s http://localhost:8080/ | grep -o 'assets/[^"]*\.js' | head -1)` → expect `200`
Run: `curl -s http://localhost:8080/how | grep -o '<title>[^<]*</title>'` → expect `<title>QuickRoom · How it works</title>` (untouched static page still works)
Stop the server after checking.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/server.go backend/internal/api/spa.go
git commit -m "Serve the Vue SPA for /, /admin, /floor; remove the old static HTML pages"
```

---

### Task 9: Dockerfile and `.gitignore`

**Files:**
- Modify: `backend/Dockerfile`
- Modify: `.gitignore` (repo root)

**Interfaces:**
- Produces: `docker build` from `backend/` alone succeeds, producing an image that serves the SPA — provided `frontend/` has been built first (`npm run build`, which writes into `backend/internal/api/web/dist`).

**Revised approach (deviates from the original plan):** the established VPS deploy process (see `[[project-deployment]]` memory) ships only `backend/`'s contents to a flat `/root/roompulse` directory with no sibling `frontend/` — it doesn't tar the repo root. Rather than restructure that live layout, the frontend is built **locally, before** the Docker build/deploy, and its output (`backend/internal/api/web/dist`, already gitignored) ships as part of the normal `backend/` tar — exactly how the vendored Swagger UI assets already work (pre-built files sitting in the `api` package, not generated in-image). This keeps `docker-compose.yml`'s `build: .` and the whole deploy runbook unchanged.

- [ ] **Step 1: Add a build-prerequisite comment to `backend/Dockerfile`; no new stage**

`backend/Dockerfile` stays single-stage (Go build → distroless runtime), unchanged except a comment noting the prerequisite:

```dockerfile
# Requires the Vue SPA to be pre-built before this runs: `cd frontend && npm
# run build`, which writes straight into internal/api/web/dist (see
# frontend/vite.config.ts) so go:embed picks it up here. Not built as a Docker
# stage — the deploy dir on the VPS ships only backend/, matching how the
# vendored Swagger UI assets already work (pre-built, not generated in-image).

# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quickroom ./cmd/quickroom
# Writable token dir, owned by the non-root runtime user (distroless can't mkdir).
RUN mkdir -p /data && chown 65532:65532 /data

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/quickroom /quickroom
COPY --from=build --chown=nonroot:nonroot /data /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/quickroom"]
```

- [ ] **Step 2: `backend/docker-compose.yml` needs no change**

`build: .` already works, since the Docker build context (`backend/`) now contains the pre-built `internal/api/web/dist` on disk before `docker build`/`docker compose build` runs.

- [ ] **Step 3: Update root `.gitignore`**

Add:
```
# --- Frontend build output / deps ---
/frontend/node_modules/
/frontend/dist/
/backend/internal/api/web/
```

- [ ] **Step 4: Verify the full Docker build**

Run: `cd frontend && npm run build` (ensures `backend/internal/api/web/dist` is current)
Run (from `backend/`, matching exactly what the VPS deploy does): `docker build -t quickroom-test .`
Expected: builds succeed, final image builds.

Run: `docker run --rm -p 8081:8080 -e ZOOM_MODE=mock quickroom-test &`
Run: `sleep 2 && curl -s http://localhost:8081/ | grep -o '<title>[^<]*</title>'` → expect `<title>QuickRoom</title>`
Stop the container after checking (`docker stop` on its container ID).

- [ ] **Step 5: Commit**

```bash
git add backend/Dockerfile .gitignore
git commit -m "Prepare Docker image to serve the pre-built Vue SPA"
```

---

### Task 10: Full manual verification pass

**Files:** none (verification only).

- [ ] **Step 1: Run the backend and frontend dev server together**

`cd backend && go run ./cmd/quickroom &`
`cd frontend && npm run dev`

- [ ] **Step 2: Exercise the Dashboard (`http://localhost:5173/`)**

Confirm: vitals update every ~3s, occupancy grid shows the mock-seeded room, reservations table populates, Advanced disclosure expands to show devices/beacons tables, "Sync from Zoom" button works (triggers `/sync`, then refreshes), connection chip shows "Live".

- [ ] **Step 3: Exercise Admin (`http://localhost:5173/admin`)**

Confirm: 6 KPI tiles populate, alerts section shows "All clear" or real collisions/overstays, reservations/rooms/notifications sections populate, updates every ~4s.

- [ ] **Step 4: Exercise Floor plan (`http://localhost:5173/floor`)**

Confirm: floor image + room polygons render aligned, legend counts match, clicking a room opens the detail modal with inside-now/booking/activity, Escape or the X button or backdrop click closes it, browser resize keeps the plan fit-to-screen.

- [ ] **Step 5: Confirm untouched pages still work**

`curl -s http://localhost:8080/how`, `/battery`, `/hardware`, `/scenarios`, `/decide` each return 200 with their original content (spot-check one, e.g. `/how`, renders in a browser unchanged).

- [ ] **Step 6: Confirm nav crosses correctly between SPA and static pages**

From `/` (SPA), click "How it works" in the nav → full page load to `/how` (static Go HTML). From `/how`, click "Dashboard" → full page load back to `/` (SPA shell boots fresh). No console errors either direction.

- [ ] **Step 7: Stop dev processes**

Kill both the `go run` and `npm run dev` background processes.
