# Admin Panel v2 (Apple-Style) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the admin SPA as an Apple-style app shell (frosted sidebar, routed views) whose centerpiece is a rooms×hours day schedule, proven against academy-scale data (230 users, fully booked day).

**Architecture:** Retoken `theme.css` to the Apple palette so shared utilities restyle for free; add small `components/ui` primitives (Modal, Toast, SegmentedControl, Toolbar, Pagination); split `AdminView` into six routed views under `AdminLayout`; the ReservationsView gains a custom schedule grid (absolute-positioned blocks over a 07:00–19:00 track). Frontend-only.

**Tech Stack:** Vue 3 + TS + vue-router (already present), scoped CSS on system fonts (no new deps).

**Spec:** `docs/superpowers/specs/2026-07-05-admin-panel-v2-design.md`

## Global Constraints

- **Read the `frontend-design:frontend-design` skill BEFORE writing any UI code** (execution step, not optional).
- No new npm dependencies; system font stack (SF via `-apple-system`), remove Space Grotesk/IBM Plex usage.
- Palette/controls exactly per spec §Visual language (page `#f5f5f7`, cards `#fff` hairline `rgba(0,0,0,.08)` r12, blue `#0071e3`, pills 980px, segmented `#ebebf0`, focus ring `rgba(0,113,227,.30)`, status tints, 160–220ms motion, `prefers-reduced-motion` respected).
- Gates: `npx vue-tsc --noEmit` + `npm run build` green after every task; app must remain fully functional after Task 3 (no half-migrated dead ends).
- Commits per task, imperative, no Co-Authored-By, no emojis anywhere (including UI copy).
- Production deploy only in Task 9 after the local scale pass; production data untouched.

## File Structure

```
frontend/src/
  styles/theme.css                  — Apple tokens + shared utilities (rewrite)
  components/ui/Modal.vue           — dialog: form + confirm variants
  components/ui/SegmentedControl.vue
  components/ui/Toolbar.vue         — search + filter slot + actions slot
  components/ui/Pagination.vue
  components/ui/ToastHost.vue       — + composables/useToast.ts
  layouts/AdminLayout.vue           — sidebar + view header + <RouterView>
  views/LoginView.vue               — restyle
  views/DashboardView.vue
  views/ReservationsView.vue        — tabs; owns modals
  components/schedule/ScheduleGrid.vue
  views/RoomsView.vue
  views/BeaconsView.vue
  views/UsersView.vue
  views/NotificationsView.vue
  router/index.ts                   — nested routes
DELETED: views/AdminView.vue, components/admin/{RoomsGrid,NotificationsList,KpiRow,AlertsList,UsersPanel,BeaconsPanel}.vue
  (their logic is absorbed; AppHeader.vue deleted — the layout owns chrome)
scripts/seed-academy.sh             — local scale seeding (repo root /scripts)
```

---

### Task 1: Apple theme tokens + global utilities

Rewrite `frontend/src/styles/theme.css`: keep the token NAMES already consumed by components (`--line`, `--line-soft`, `--text`→keep as alias of `--ink-text`? No — keep `--text`, `--muted`, `--faint`, `--accent`, `--danger`, `--amber`, `--signal`, `--panel`, `--panel-2`, `--raised`, `--r`, `--f-display`, `--f-body`, `--f-mono`) but assign Apple values so untouched components inherit sanely during the migration:

```css
:root {
  --page: #f5f5f7;
  --panel: #ffffff; --panel-2: #ffffff; --raised: #fafafc;
  --line: rgba(0,0,0,.08); --line-soft: rgba(0,0,0,.05);
  --text: #1d1d1f; --muted: #6e6e73; --faint: #86868b;
  --accent: #0071e3; --accent-hover: #0077ed; --accent-dim: rgba(0,113,227,.10);
  --signal: #34c759; --signal-dim: rgba(52,199,89,.12); --signal-line: rgba(52,199,89,.35);
  --amber: #ff9500; --amber-dim: rgba(255,149,0,.12); --amber-line: rgba(255,149,0,.35);
  --danger: #ff3b30; --danger-dim: rgba(255,59,48,.10); --danger-line: rgba(255,59,48,.35);
  --blue-dim: rgba(0,113,227,.10);
  --r: 12px; --shadow-card: 0 1px 3px rgba(0,0,0,.04);
  --focus-ring: 0 0 0 3.5px rgba(0,113,227,.30);
  --f-display: -apple-system, BlinkMacSystemFont, "SF Pro Display", "Helvetica Neue", sans-serif;
  --f-body: -apple-system, BlinkMacSystemFont, "SF Pro Text", "Helvetica Neue", sans-serif;
  --f-mono: "SF Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
}
```

Global utilities restyle: `body` bg `var(--page)` color 14px/1.5; `.card` flat white + `--shadow-card`; table: th sentence-case 11px `--muted` medium (drop uppercase/letter-spacing), 12px 16px padding, sticky-ready (`thead th { position: sticky; top: 0; background: #fff; z-index: 1; }`), row hover `rgba(0,0,0,.025)`; `.badge` tints per spec (add `.b-blue { background: var(--blue-dim); color: var(--accent); }`); buttons: `.btn-primary` (pill 980px blue), `.btn-secondary` (pill, `#f5f5f7` fill, hairline), `.btn-ghost` (borderless, `--muted`, hover text `--accent`), `.btn-danger-ghost` (red text); inputs: `.field` (white, hairline, r8, focus ring); `.empty` restyle; delete `.eyebrow`, `.vital`, gradient rules. Remove any `<link>`/`@import` for Space Grotesk/IBM Plex (check `index.html`).

- [ ] Rewrite theme.css per above; scrub `index.html` font links.
- [ ] Gate: `npx vue-tsc --noEmit && npm run build` (visual polish of old components is transitional — Task 3+ replaces them).
- [ ] Commit `"Retoken the admin theme to an Apple-style light palette"`.

### Task 2: UI primitives

**Produces (exact interfaces later tasks use):**
- `Modal.vue` — props `{ title: string; open: boolean; variant?: 'form' | 'confirm'; confirmLabel?: string; danger?: boolean; busy?: boolean }`, emits `close` and `confirm`; slots: default (body), `footer` (form variant renders its own footer buttons via slot; confirm variant renders Cancel + confirmLabel buttons). Backdrop `rgba(0,0,0,.30)` + blur(2px); panel white r14 shadow `0 20px 60px rgba(0,0,0,.18)`, fade+scale 180ms; Esc/backdrop → `close`.
- `SegmentedControl.vue` — props `{ options: { value: string; label: string }[]; modelValue: string }`, emits `update:modelValue`; Apple track/raised-segment styling.
- `Toolbar.vue` — props `{ search?: string; searchPlaceholder?: string }`, emits `update:search`; slots `filters`, `actions`; renders a search field with a magnifier glyph (CSS), wraps on mobile.
- `Pagination.vue` — props `{ total: number; page: number; perPage: number }`, emits `update:page`; renders "1–25 of 312" + ‹ › pill buttons; hides when total ≤ perPage.
- `composables/useToast.ts` — `useToast()` → `{ success(msg: string): void; error(msg: string): void }`; module-level reactive queue; `ToastHost.vue` (mounted once in AdminLayout) renders bottom-center stack, auto-dismiss 3.5s, slide-up 200ms.

- [ ] Implement all five + ToastHost; gate vue-tsc + build; commit `"Add Apple-style UI primitives: modal, segmented control, toolbar, pagination, toasts"`.

### Task 3: Shell, routing, view split (app stays functional)

- `layouts/AdminLayout.vue`: grid `240px 1fr`; frosted sidebar (`rgba(255,255,255,.72)` + `backdrop-filter: blur(20px)`, hairline right): logo row (beacon dot in mint `#2FE6B0` + "QuickRoom"), nav items (inline SVG glyphs, 13px medium, active = `--accent-dim` pill + blue text), badges (Dashboard: open alerts count red pill; Notifications: outbox count gray pill) fed by a 10s combined poll (`getCollisions`+`getOverstays`+`getNotifications(200)` lengths); footer: admin email (decode JWT payload email? not in token — store email from login response: extend `auth.ts` `login()` to also `localStorage.setItem('qr_admin_email', email)` + `getAdminEmail()`), Sign out. Mobile <760px: sidebar off-canvas, hamburger in a slim top bar.
- View header inside each view (shared `PageHeader` markup kept inline per view: h1 28px/700, subtitle 13px `--muted`, right side = Live chip + action button slot).
- `router/index.ts`: `/login` standalone; parent `/` → `AdminLayout` with children `'' → DashboardView`, `reservations`, `rooms`, `beacons`, `users`, `notifications` (names: `dashboard`, `reservations`, …). Guard unchanged.
- Create the six views by MOVING existing section markup/logic out of `AdminView.vue` (temporary look is fine): Dashboard = KpiRow + AlertsList + old RoomsGrid readonly part? — no: Dashboard = KpiRow + AlertsList + notifications preview (slice(0,5) of old NotificationsList) ; Reservations = old booking form + table + row actions; Rooms = old RoomsGrid; Beacons = BeaconsPanel; Users = UsersPanel; Notifications = NotificationsList. Each view owns its own `usePoll(refresh, 4000)` fetching only its data. Delete `AdminView.vue` + `AppHeader.vue`.
- [ ] Gate: build + manual click-through all six routes locally (`npm run dev` against prod API is CORS-fine — the API allows `*`; use `VITE`-less relative fetches via dev proxy? No proxy configured — instead build+embed and run the Go binary locally in mock mode for the click-through).
- [ ] Commit `"Split the admin panel into routed views under a frosted sidebar shell"`.

### Task 4: Dashboard v2

- KPI strip: 6 stat cards in one row (wrap on mobile): label 11px `--muted`, value 26px/700 `--f-display` tabular; no gradients.
- Needs-attention: white card list, red/orange left accent bar per row, empty state "All clear."
- Room tiles: name, live headcount dot (green when >0), `n / cap` and a **utilization bar** — fraction of 07:00–19:00 covered by today's active (booked/checked-in) reservations:

```ts
function utilizationToday(ws: string, reservations: Reservation[]): number {
  const day = new Date(); day.setHours(7, 0, 0, 0)
  const start = day.getTime(), end = start + 12 * 3600_000
  const spans = reservations
    .filter(r => r.zoom_workspace_id === ws && r.status === 'booked')
    .map(r => [Math.max(start, Date.parse(r.start_time)), Math.min(end, Date.parse(r.end_time))] as [number, number])
    .filter(([a, b]) => b > a)
    .sort((a, b) => a[0] - b[0])
  let covered = 0, cursor = start
  for (const [a, b] of spans) { if (b <= cursor) continue; covered += b - Math.max(a, cursor); cursor = Math.max(cursor, b) }
  return covered / (end - start)
}
```

  Bar: 4px track `#ebebf0`, fill blue, >85% orange (packed).
- Recent notifications: last 5, compact rows, "View all" → `/notifications`.
- [ ] Gate + commit `"Dashboard v2: KPI strip, attention list, utilization bars, recent activity"`.

### Task 5: Reservations v2 — schedule grid + list

**`components/schedule/ScheduleGrid.vue`** — props `{ rooms: Room[]; reservations: Reservation[]; date: Date }`, emits `select(reservation: Reservation)` and `create(slot: { workspaceId: string; start: Date; end: Date })`.

Geometry (07:00–19:00 = 720 min window):

```ts
const DAY_START_H = 7, WINDOW_MIN = 720
function pct(t: number, dayStart: number) { return ((t - dayStart) / 60000) / WINDOW_MIN * 100 }
// block: left = clamp(pct(start), 0, 100); right = clamp(pct(end), 0, 100); width = max(right-left, 1.2%)
// dayStart = selected date at 07:00 local
```

- Layout: sticky 160px room-label column; track area `position: relative`, hour gridlines every 60min (hairline), hour labels 11px `--faint` on top axis; row height 46px, blocks inset 5px vertically, r6, 12px text truncated (booker email prefix), tint per status (booked=blue tint+blue left bar 3px, checked-in=green, released/cancelled=gray at 55% opacity behind others via lower z-index, no_show=red tint).
- Now-line: red 1.5px vertical + dot on today within window, updated every poll tick.
- Click empty track → `create` with slot: `minutes = Math.floor((offsetX / trackWidth) * WINDOW_MIN / 30) * 30`, start = dayStart + minutes, end = min(start+60min, 19:00). Click block → `select(r)`.
- Date state in `ReservationsView`: `‹ date ›` steppers + native `<input type="date">`; reservations filtered client-side to blocks overlapping the selected 07:00–19:00 window.
- Tabs via `SegmentedControl` (`schedule` | `list`).
- Modals (owned by the view): detail (fields + Edit/Cancel when `source==='app' && status==='booked'`), new/edit form modal (room select, `datetime-local` inputs, email) prefилled from `create` slot or edited row; all mutations toast success/error and refresh.
- List tab: `Toolbar` (search matches booker email/user id + room name; status chips via SegmentedControl `all|booked|released|cancelled|no_show`; source `all|app|zoom`), table 25/page via `Pagination`, same row actions.
- [ ] Gate + commit `"Reservations v2: day schedule grid with click-to-book, filtered paginated list"`.

### Task 6: Rooms + Beacons v2

- RoomsView: table Name / Capacity / Type badge (`cr-` prefix → Custom blue tint, else Zoom gray) / Occupancy (dot + n) / Beacon (✓ mono minor or —, from `getBeacons()`) / actions: Edit (modal: name+capacity), custom→Delete (confirm modal noting booking-cancel cascade), zoom→"Reset to Zoom" (confirm modal explaining override clearing). "Add room" header action → form modal. Toasts everywhere.
- BeaconsView: registry table restyled, `Toolbar` room-name search, keep inline edit row pattern but with `.field`/pill buttons.
- [ ] Gate + commit `"Rooms and beacons views v2"`.

### Task 7: Users + Notifications v2

- UsersView: `Toolbar` search (name/email, case-insensitive), 25/page `Pagination`, expandable bookings row kept, inline rename kept (field + Save/Cancel pills), Delete → confirm `Modal` ("Cancels their open bookings and removes the account."). Toasts.
- NotificationsView: SegmentedControl type filter (`all|grace_reminder|no_show_released|room_freed|collision|overstay`), recipient search, 25/page, Dismiss per row, "Clear all" → confirm modal. Toasts.
- [ ] Gate + commit `"Users and notifications views v2"`.

### Task 8: Local scale verification (230 users, fully booked day)

Create `scripts/seed-academy.sh` (repo root): builds a temp SQLite the local backend then loads. Approach: start once to create schema, stop, seed, start again — or simpler, create schema via the Go binary run with `DB_PATH=/tmp/qr-scale/roompulse.db` for 2s, kill, then:

```bash
#!/bin/bash
# seed-academy.sh <db-path> — 230 accounts + a fully booked day (10 rooms x ~10 bookings)
set -euo pipefail
DB="$1"
TODAY=$(date +%Y-%m-%d)
WS=(ws-agung ws-bedugul ws-mengwi ws-nusadua ws-petang ws-sanur ws-ubud ws-ceningan ws-lembongan ws-penida)
{
echo "BEGIN;"
for i in $(seq 1 200); do
  printf "INSERT OR IGNORE INTO users (user_id, apple_sub, email, name, created_at) VALUES ('usr_s%03d','sub-s%03d','student%03d@academy.test','Student %03d',strftime('%%s','now'));\n" "$i" "$i" "$i" "$i"
done
for i in $(seq 1 30); do
  printf "INSERT OR IGNORE INTO users (user_id, apple_sub, email, name, created_at) VALUES ('usr_t%03d','sub-t%03d','staff%03d@academy.test','Staff %03d',strftime('%%s','now'));\n" "$i" "$i" "$i" "$i"
done
n=0
for ws in "${WS[@]}"; do
  # 10 bookings: 07:00-19:00 in 60+12min-gap steps
  for h in 0 1 2 3 4 5 6 7 8 9; do
    n=$((n+1))
    start=$(( $(date -j -f "%Y-%m-%d %H:%M" "$TODAY 07:00" +%s) + h*4320 ))   # 72min pitch
    end=$(( start + 3600 ))
    u=$(( (n % 200) + 1 ))
    printf "INSERT OR REPLACE INTO app_reservations (reservation_id, room_id, zoom_workspace_id, booked_by_user_id, user_email, start_time, end_time, status, check_in_status) VALUES ('seed-%04d','room-%s','%s','usr_s%03d','student%03d@academy.test',%d,%d,'booked','not_checked_in');\n" "$n" "$ws" "$ws" "$u" "$u" "$start" "$end"
  done
done
echo "COMMIT;"
} | sqlite3 "$DB"
sqlite3 "$DB" "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM app_reservations);"
```

- [ ] Run locally: `DB_PATH=/tmp/qr-scale/data.db HTTP_ADDR=:8081 ZOOM_MODE=mock go run ./cmd/quickroom` (frontend built+embedded first), seed between first boot and restart.
- [ ] Playwright against `http://localhost:8081`: login (default dev creds), Dashboard (utilization bars near-full), Reservations schedule (~100 blocks render, click a gap → prefilled modal, click a block → detail), List (pagination shows "1–25 of ~100+", search narrows), Users ("1–25 of 230", search "Student 042" narrows to 1), Notifications filters, Rooms/Beacons tables. Screenshots to scratchpad for the record.
- [ ] Fix whatever the scale pass surfaces; gate vue-tsc+build; commit `"Add academy-scale seed script"` (+ any fixes).

### Task 9: Deploy + prod verify + wrap-up

- [ ] `npm run build` → tar deploy → rebuild container (usual pipeline; compose/.env untouched).
- [ ] Prod browser pass: login, all six views render prod data, one CRUD smoke (create+delete custom room), data intact (users/apns counts).
- [ ] Push `main`; work log section `### 2026-07-05 — Admin panel v2 (Apple redesign)`.

## Self-Review Notes

- Spec coverage: tokens/controls (T1), primitives (T2), shell+routes+badges+admin email (T3), dashboard incl. utilization (T4), schedule+list (T5), rooms/beacons (T6), users/notifications (T7), scale verification exactly as spec §Verification (T8), deploy (T9). Out-of-scope items have no tasks. ✓
- Interfaces consistent: Modal/SegmentedControl/Toolbar/Pagination/useToast signatures defined in T2 and consumed by name in T4–T7; `utilizationToday` and grid geometry given as code. ✓
- No placeholders: styling tasks specify exact values from the spec's token sheet; behavior enumerated per view. ✓
