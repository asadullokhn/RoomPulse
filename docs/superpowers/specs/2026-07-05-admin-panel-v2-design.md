# Admin Panel v2 — Apple-Style Redesign

**Date:** 2026-07-05
**Decisions (Asadullokh):** real-scale data (Academy: 200 students + 30 staff,
every room near-fully booked 07:00–19:00), sidebar app shell with routed
views, reservations led by a day **schedule grid**, visual language in
**Apple's style**, "beautiful and enjoyable". Frontend-only — the API already
serves bounded payloads (users ≈ 230, reservations day-scoped, outbox ≤ 200).

## Visual language (Apple)

- **Type:** `-apple-system, BlinkMacSystemFont, "SF Pro Text", "Helvetica
  Neue", sans-serif`; mono `"SF Mono", ui-monospace, Menlo`. 13 px table
  text, 11 px secondary labels (sentence case — no uppercase eyebrows),
  28 px/700 view titles (SF Display feel, tracking -0.01em).
- **Surfaces:** page `#f5f5f7`; cards `#fff`, hairline `rgba(0,0,0,.08)`,
  radius 12 px, shadow `0 1px 3px rgba(0,0,0,.04)`. Sidebar frosted:
  `rgba(255,255,255,.72)` + `backdrop-filter: blur(20px)`.
- **Text:** `#1d1d1f` primary, `#6e6e73` secondary, `#86868b` tertiary.
- **Color:** accent/interactive Apple blue `#0071e3` (hover `#0077ed`);
  destructive `#ff3b30`; presence/positive `#34c759`; warning `#ff9500`.
  Status tints (badge bg 10–14% alpha of its color): booked=blue,
  checked-in=green, released/cancelled=gray, no-show=red, collision=red,
  overstay=orange. The old mint survives only in the beacon logo dot.
- **Controls:** primary buttons = Apple pill (radius 980 px, blue fill,
  white 13 px semibold); secondary = `#f5f5f7` fill hairline; destructive
  ghost = red text. Segmented control (tabs/filters): `#ebebf0` track,
  white raised selected segment with soft shadow. Focus ring
  `0 0 0 3.5px rgba(0,113,227,.30)`.
- **Motion:** 160–220 ms ease on hover/selection; modals fade+scale
  (.98→1); toasts slide up from bottom-center; schedule now-line and
  occupancy dots update without jumps. `prefers-reduced-motion` respected.

## Shell & routing

`AdminLayout.vue` wraps children routes: sidebar (Dashboard, Reservations,
Rooms, Beacons, Users, Notifications; live badges: open alerts on Dashboard,
outbox count on Notifications) + per-view header (title, Live chip, view's
primary action). Sidebar footer: admin email + Sign out. Mobile (< 760 px):
sidebar becomes a slide-over behind a menu button. Routes: `/` dashboard,
`/reservations`, `/rooms`, `/beacons`, `/users`, `/notifications`, `/login`
(outside the layout). Router guard unchanged.

## Views

- **Dashboard:** KPI strip (6 compact stats); Needs-attention list
  (collisions+overstays); room tiles with live headcount AND a "booked
  today" utilization bar (fraction of the 07:00–19:00 window covered by
  active bookings); last 5 notifications with link to the full view.
- **Reservations:** segmented tabs **Schedule | List**.
  - *Schedule (default):* date picker (default today, ‹ › day steppers);
    rooms as rows (sticky 160 px label column), 07:00–19:00 timeline;
    booking blocks absolutely positioned by time (Apple-Calendar-like: tinted
    fill, 3 px saturated left edge, 6 px radius, booker + window inside when
    height permits); status colors per palette; cancelled/released rendered
    faded. A red **now-line** on today. Click a block → detail modal
    (booker, window, status, source; Edit/Cancel for app-sourced booked).
    Click an empty slot → New-booking modal **pre-filled with room + slot**
    (snap 30 min). "New booking" header button opens the same modal blank.
  - *List:* toolbar (search booker/room, status chips, source segmented),
    paginated table 25/page, sticky header; row Edit/Cancel (app-sourced).
- **Rooms:** table — Name, Capacity, Type badge (Zoom / Custom /
  Overridden), live occupancy, beacon assigned (✓/—), actions Edit /
  Delete (custom) / Reset (zoom override). "Add room" header button →
  modal. Type "Overridden" = zoom room having an override row (detectable
  client-side: name/capacity differs from... not detectable — instead: mark
  custom via `cr-` prefix; zoom rooms show Reset action always, labelled
  "Reset to Zoom" with tooltip-style hint text in the modal).
- **Beacons:** existing registry as a v2-styled table, inline edit kept,
  toolbar with room-name search.
- **Users:** toolbar search (name/email), paginated table 25/page,
  expandable per-user bookings (kept), inline rename, Delete behind a
  confirm dialog stating the cascade.
- **Notifications:** type filter segmented (all / reminders / releases /
  freed / collisions / overstays), recipient search, paginated list,
  per-row Dismiss, "Clear all" behind confirm.

## Shared primitives (new `components/ui/`)

`Modal.vue` (form + confirm variants, esc/backdrop close, focus trap),
`Toolbar.vue` (search input + filter slot + action slot),
`Pagination.vue`, `SegmentedControl.vue`, `Toast.vue` + `useToast()`
(success/error feedback on every mutation), restyled `StatusBadge`.
Existing `DataTable` restyled (sticky header, hairline rows, hover).
`AdminView.vue` is dismantled into the routed views; `KpiRow`, `AlertsList`
survive restyled inside Dashboard; `RoomsGrid`/`NotificationsList`/
`UsersPanel`/`BeaconsPanel` content is absorbed into the new views.

## Data

Per-view polling via existing `usePoll` (4 s), each view fetching only its
needs; the layout polls `/collisions`+`/overstays`+`/notifications` counts
for sidebar badges (single combined 10 s poll). All search/filter/pagination
client-side.

## Verification at scale

1. Local: run the backend (mock mode, temp DB), seed **230 users and a
   fully-booked day** (10 rooms × ~10 app bookings across 07:00–19:00,
   booked by seeded users) directly into the temp SQLite before boot;
   Playwright through every view: schedule renders ~100 blocks, list
   paginates, users search narrows 230 rows, CRUD modals + toasts work.
2. `vue-tsc` + `npm run build` green.
3. Deploy (frontend build ships inside the Go binary as usual), prod
   browser pass, production data untouched.

## Out of scope

Backend changes; dark mode (single polished light theme); drag-to-create /
drag-to-move on the schedule (click-to-book only, this pass); virtualized
lists (payloads are bounded).
