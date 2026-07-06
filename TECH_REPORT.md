# Tech Report — QuickRoom

QuickRoom detects who is physically inside which meeting room — one iBeacon per room, an iPhone that checks you in and out automatically even with the app closed — and drives Zoom Workspace check-in/out plus notification-based no-show handling. Journey: 2026-06-25 to 2026-07-06.

---

## Present your team

- **Ali** — Go backend, beacon hardware and firmware, admin panel, deployment.
- **Rei** — native iOS app ([Reishandy/QuickRoom](https://github.com/Reishandy/QuickRoom), Swift / SwiftUI).
- **Abu** — main designer: app UI and visual language.
- **Diyora** — project management and team organization.
- **Reno** — booking model: the proportional grace-period and notification ladder.

---

## Starting Assumption

We think we'll end up using:

CoreLocation beacon **ranging** (continuous RSSI/distance) + CoreBluetooth, an **ESP32 dev board** as the room beacon, and a small **Go backend** that mostly proxies check-ins to the Zoom Workspace API.

Because:

"Who is in which room" sounded like a distance problem, and ranging gives you distance. The ESP32 was already on the desk. Go compiles to a single binary that deploys anywhere.

---

## The Exploration Log

**What we browsed, and what surprised us:**

- **Region monitoring vs. ranging.** Ranging is foreground-only; region monitoring works with the app closed but only reports enter/exit crossings — never distance, never "still here."
- **The iBeacon format.** A beacon transmits only `(UUID, major, minor)` — no payload, no room name. So all meaning has to live in the backend, and the beacon can be completely dumb.
- **Beacon hardware.** Battery life and minimum TX power vary hugely across boards that all "support iBeacon."

**What we actually built or tested in code:**

- **Week 1: end-to-end prototype.** Go + SQLite backend, iOS app, ESP32-C6 firmware, Zoom check-in/out (mock mode), deployed on a VPS.
- **Field test at the Academy** with two rooms broadcasting at once: closed-app check-in verified (the background counter went 0 → 56 on a real crossing), clean room handoff, no double-occupancy. A spare iPhone broadcasting iBeacon acted as the second room — no extra hardware needed.
- **Remote diagnostics in the app**: permission state, monitored regions, a background check-in counter, and a send-to-backend snapshot — the only way to debug a closed app on a tester's phone.
- **TX-power tuner**: a Mac serial bridge plus a live-RSSI tab in the app, so we could walk the building and size the beacon's range to the room.
- **nRF52840 validation**: the same beacon identity on Nordic hardware, verified on air with a Mac BLE scanner.
- **Backend build-out**: proportional grace engine, Sign in with Apple, JWT auth, APNs push (verified with real pushes to Rei's phone), Swagger docs, and a Vue 3 admin panel load-tested with 230 seeded accounts and a fully booked day.

**What we discovered that we didn't expect:**

- **"Background check-in is broken" was our test being wrong.** Region monitoring only fires on a boundary crossing — standing next to an always-on beacon produces nothing, forever. Toggle the beacon or walk out and back, and it had worked all along.
- **The serial log lies.** The ESP32 printed "advertising" while radiating nothing. Only an independent BLE scan is proof a beacon is alive.
- **There is no "3 meters" setting.** The check-in boundary is the beacon's RF range, so room-sizing is TX power plus placement — not a number in software.
- **Notifications are the spine.** The team's answers to the problem scenarios independently converged on notification-driven design, formalized in Reno's grace ladder: nudge at 5% of the booking elapsed, auto-release at 10%.

---

## What We Tried and Dropped

**We considered: continuous ranging with an RSSI threshold** — and built it first.
We dropped it because: it dies the moment the app closes (which is a phone's normal state), drains battery, keeps the location indicator on, and missed check-ins. Region monitoring became the single source of presence truth. We traded sub-meter precision for battery, privacy, and works-with-the-app-closed.

**We considered: reusing the building's Wi-Fi access points as beacons.**
We dropped it because: one AP covers several rooms. Room-level presence needs a per-room radio.

**We considered: the ESP32-C6 as the production beacon.**
We dropped it (kept it as the dev board) because: it can't sleep its radio (~25 mA, days on battery, not months) and its −12 dBm minimum TX is too loud for one room. Production moves to nRF52 coin-cell tags — validated on real nRF52840 hardware with the identical identity scheme, so the app and backend need zero changes when the hardware swaps.

---

## Real Limitations Hit

- **The ESP32-C6 BLE stack doesn't behave as documented.** Non-connectable advertising — the textbook mode for a beacon — silently never radiates, while the firmware logs "advertising." Documentation and AI assistants both said it should work; only a real BLE scan settled it. Workaround: connectable advertising with no GATT services, a self-healing advertiser restart, and a standing rule — verify on air, never trust the device's own log.
- **Region-monitoring semantics cost us days.** There is no way to ask "is the phone near the beacon right now?" in the background — only crossings. It changed how we test (force crossings) and what we promise (presence, not position).
- **Radio physics.** BLE spills through walls even at minimum power. We stopped fighting it in software: TX calibration became an installer workflow, and the room boundary is accepted as approximate.
- **Apple signing.** A free developer team can't sign the Sign in with Apple capability, so on-device auth testing had to run on Rei's paid team.
- **Landmines found only against the real thing:** Go emits nanosecond timestamp fractions that iOS's ISO8601 parser (exactly 3 digits) rejects; `//` starts a comment mid-value in xcconfig, truncating URLs to `https:`; the admin SPA's routes collided with the JSON API and had to move to hash routing.

---

## The Revised Decision

Final decision:

- **CoreLocation region monitoring as the single presence truth**; ranging survives only as a foreground diagnostic.
- **Notifications first**: the proportional grace ladder plus collision and room-freed alerts, delivered over APNs.
- **Dumb beacons, smart backend**: beacons broadcast only `(UUID, major, minor)`; rooms, rules, auth, and admin live in the Go backend with a Vue 3 admin panel. Moving a beacon is a backend edit, never a device visit.
- **nRF52 coin-cell beacons for production**; the ESP32-C6 stays as the dev board.

What changed since Section 1, and why:

Almost everything except Go. Ranging flipped to region monitoring because ranging fails exactly when it matters — with the app closed. The ESP32 was demoted once we measured its power draw and radio quirks. And the backend grew from a thin Zoom proxy into the core of the product, because the hard problems — no-shows, grace timing, collisions — turned out to be booking-and-notification problems, not detection problems.

---

## App Track Addendum

### About the Frameworks

We genuinely need both. **CoreLocation** alone gives silent presence — the backend knows who is where, and nothing changes for anyone. **UserNotifications** (fed by APNs) alone is a naggy calendar with no ground truth. The product exists at the joint: presence-verified notifications, fired because a phone physically crossed a beacon boundary. CoreBluetooth helped in tooling (phone-as-beacon test rig, Mac scanner), and AuthenticationServices ties bookings to the person the presence belongs to.

### About Accessibility and Localization

We didn't localize, deliberately: the pilot audience is the Academy cohort, whose working language is English, and the strings were still changing weekly. For accessibility we stayed on standard SwiftUI controls and the system font, and status is never conveyed by color alone. We have not done a formal accessibility audit — that's a gap, not a claim.

### About Privacy

The app needs three things: **region enter/exit events** (which room, when — never coordinates, never a movement trail), the **Apple ID email** at sign-in, and an optional **push token**. If the user denies Always-location, QuickRoom becomes a plain booking app — browsing and booking still work, only auto check-in stops, and the diagnostics panel says exactly which permission is missing. Denying notifications just means the grace nudges don't reach you. Account deletion cascades server-side: bookings canceled, sessions revoked, push tokens removed.
