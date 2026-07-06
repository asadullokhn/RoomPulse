# Tech Report — QuickRoom

QuickRoom detects who is physically inside which meeting room using iBeacon presence — one beacon per room, an iPhone that checks you in and out automatically even with the app closed — and drives Zoom Workspace check-in/out plus a notification-first no-show/grace system. This report covers the journey from 2026-06-25 to 2026-07-06.

---

## Present your team

- **Ali** — Go backend (presence, booking, grace engine, JWT auth, APNs), beacon firmware and hardware (ESP32-C6, nRF52840), admin panel (Vue 3), deployment.
- **Rei** — native iOS app ([Reishandy/QuickRoom](https://github.com/Reishandy/QuickRoom), Swift 6 / SwiftUI): booking UI, floor plan, beacon presence integration.
- **Reno** — booking model and notification design: the proportional grace-period model and the reminder ladder that shaped the backend's core logic.

---

## Starting Assumption

We think we'll end up using:

**CoreLocation beacon *ranging*** (continuous RSSI/distance measurement) paired with **CoreBluetooth**, an **ESP32 dev board** as the room beacon, and a small **Go service** that mostly proxies check-ins to the **Zoom Workspace API**.

Because:

Ranging gives you distance in meters, and "who is in which room" sounded like a distance problem — measure how close the phone is, gate check-in on an RSSI threshold. The ESP32 was chosen because we had one on the desk and it speaks BLE. Go because it compiles to a single static binary we could deploy anywhere. The backend, we assumed, would stay thin — Zoom already has rooms and reservations, so we'd just forward events.

---

## The Exploration Log

**What we browsed, and what surprised us:**

- **CoreLocation docs, region monitoring vs. ranging.** Ranging is effectively foreground-only and keeps the location indicator on; region monitoring works with the app killed, but it only reports *boundary crossings* — enter and exit — never distance, and never "still here." This distinction ended up defining the whole product (see below).
- **The iBeacon format.** A beacon transmits exactly one thing: `(UUID, major, minor)`. There is no payload, no name, no room number. That forced a decision: all *meaning* (which beacon is which room) has to live in the backend, and the beacon itself can be completely dumb.
- **Hardware survey** for the room beacon: reusing Wi-Fi access points as beacons, commercial iBeacon tags, ESP32 variants, Nordic nRF52 chips. The surprise: battery life and TX-power floor vary by two orders of magnitude across options that all "support iBeacon."

**What we actually built or tested in code (not just read about):**

- **Week 1 — end-to-end prototype.** Go + SQLite backend, iOS app, ESP32-C6 iBeacon firmware, Zoom Workspace check-in/out (mock mode). Deployed to a Hetzner VPS behind Caddy and Cloudflare.
- **Field test at the Academy (2026-06-30)** with two rooms broadcasting simultaneously: closed-app background check-in verified (the diagnostics counter went 0 → 56 the moment we forced a real region crossing), clean single-room handoff with no double-occupancy, no flapping. We used a *phone as the second room's beacon* — toggling an iBeacon broadcast on a spare iPhone cleanly simulates walking into and out of a room, so multi-room testing needed only one physical beacon.
- **Remote diagnostics in the mobile app.** You cannot attach a debugger to a closed app on someone else's phone, so the app grew a readiness panel (location permission state, precise-location flag, monitored region count, a background check-in counter, backend reachability), an on-device event log, and a "send to backend" snapshot (`POST /diag`) so we could read any tester's device state from anywhere. The background check-in counter turned out to be the single most useful signal in the whole project: permissions being green means *configured*; that counter rising means *working*.
- **Beacon firmware with zero per-unit setup.** major/minor are derived from the chip's factory MAC (CRC-16 with distinct seeds), so every board is unique and stable out of the box; installers can override identity over USB serial (persisted to flash). A Mac-side CoreBluetooth scan tool became our ground truth for "is it actually transmitting."
- **TX power as a product feature.** Because check-in fires wherever the beacon is receivable, the beacon's transmit power *is* the room boundary. We built a tuner: a Python serial-to-HTTP bridge on a Mac plus a tuner tab in the app showing live RSSI next to the 14 TX levels, so you can walk the building and size the region to the room.
- **nRF52840 validation (on-site).** Flashed the same iBeacon identity scheme onto a Nordic board, verified the radiated frame byte-for-byte with the BLE scanner, and confirmed live TX tuning on air (−40 dBm setting dropped observed RSSI from −59 to −85).
- **Backend build-out.** Scenario engine (proportional no-show grace with a reminder ladder, auto-release, booker-vs-occupant collision detection, overstay detection, utilization reporting), OpenAPI docs with Swagger UI, Sign in with Apple, JWT auth for both admin and mobile, full CRUD for rooms/reservations/users/beacons, and an APNs push pipeline — verified end-to-end with real pushes landing on Rei's phone, including automatic pruning of dead device tokens.
- **Admin panel at scale.** A Vue 3 admin app (day-schedule grid across all rooms, click-a-gap-to-book, utilization bars, search/filter/pagination) load-tested against a seeded academy-sized dataset: 230 accounts and a fully booked 07:00–19:00 day.

**What we discovered that we didn't expect:**

- **"Background check-in is broken" was our test being wrong.** For days a test phone showed zero background events and we hunted a bug that didn't exist. Region monitoring only fires on a boundary *crossing* — a beacon permanently in range produces one initial state and then silence, forever. We were testing by standing next to an always-on beacon. Toggle the broadcast or walk out of range and back, and everything worked.
- **The serial log lies.** The ESP32-C6 firmware would print "advertising" every loop while radiating nothing. Only an independent BLE scan proves a beacon is alive; we stopped trusting device-side logs entirely.
- **There is no "3 meters" setting.** With region monitoring as the presence truth, the check-in boundary is the beacon's RF range, full stop. Room-sizing is done with TX power and physical placement, not a distance threshold in software.
- **Notifications are the spine, not a feature.** When the team answered the twelve problem-scenario cases independently (no-show, ghost holds, collisions, overstays…), all three of us converged on notification-driven designs, and Reno's grace model is literally a notification-timing ladder: nudge at 5% of the booking elapsed, optionally again at 7.5%, auto-release at 10%. The backend was rebuilt around that.

---

## What We Tried and Dropped

**We considered: continuous ranging with an RSSI threshold for check-in** — our starting assumption, and we actually built it first.

We dropped it because: ranging is foreground-only, so check-in dies the moment the app closes — which is the normal state of everyone's phone. It kept the location indicator on (users read that as surveillance), drained battery, and the one-shot ranging windows *missed* check-ins. We reverted to region monitoring as the single source of truth and deliberately never reintroduced ranging into the presence path (it survives only as a foreground live-signal meter and the TX tuner). We traded sub-meter precision for battery, privacy, and works-with-the-app-closed — the right trade for this product.

**We considered: reusing the building's Wi-Fi access points as beacons** — zero new hardware sounded great.

We dropped it because: one AP covers several rooms. Room-level presence needs a per-room radio with a tunable, room-sized range; an AP can't tell you *which* room someone is in, only which floor-ish area.

**We considered: the ESP32-C6 as the production room beacon.**

We dropped it (demoted it to a dev/validation board) because: the C6 cannot sleep its radio — both CPU downclocking and light sleep silently kill advertising — so it idles at ~25 mA and lasts days, not months, on battery. Its TX floor of −12 dBm still spills through walls, too loud for a genuinely room-sized region. Production is moving to nRF52 coin-cell tags (~a year on a CR2032, TX down to −20 dBm), which we validated on real nRF52840 silicon with the identical `(UUID, major, minor)` scheme — so the app and backend need zero changes when the hardware swaps.

---

## Real Limitations Hit

**The ESP32-C6 BLE stack does not behave as documented.** Non-connectable advertising (`ADV_NONCONN_IND`) — the textbook-correct mode for a beacon — silently never radiates on this chip/core combination, and scannable advertising radiates only intermittently. The firmware loop happily logs "advertising" throughout. Documentation said it should work; AI assistants confidently said it should work; only a Mac-side CoreBluetooth scan proved it didn't, and no amount of prompting could substitute for putting a real receiver next to the board. How we worked around it: kept the default *connectable* advertising type (safe in practice because the beacon exposes no GATT services, so there is nothing to connect to), added a self-healing advertiser restart, and made "verify on air, never trust the serial log" a standing rule.

**CoreLocation region-monitoring semantics cost us days.** Not a bug — our mental model was wrong (see the exploration log) — but it's the sharpest edge we hit: the API gives you no way to ask "is the phone near the beacon right now?" in the background, only crossings. It changed how we test (force crossings), what we instrument (a background-enter counter), and what we promise (presence, not position).

**Radio physics vs. product boundaries.** Even at the C6's minimum TX power, BLE spills through walls; a "room" is not a clean RF shape. We stopped fighting it in software and made TX calibration a first-class installer workflow (the tuner), accepted the boundary is approximate, and picked hardware (nRF52) that can go quieter.

**Apple developer-program friction.** A free-team certificate cannot sign the Sign in with Apple capability, so on-device SIWA end-to-end testing had to run on Rei's paid team; our side verified against the simulator and the live backend. Separately, 7-day provisioning profiles meant periodically re-provisioning test phones mid-challenge.

**Small cross-stack landmines, each found only by testing against the real thing:**
- Go emits RFC 3339 timestamps with *nanosecond* fractions; `ISO8601DateFormatter` with `.withFractionalSeconds` parses *exactly three* fractional digits and fails on nine. Fixed with a truncate-to-milliseconds parser, unit-tested against captured live JSON.
- In an `.xcconfig` file, `//` starts a comment even mid-value, so `https://…` URLs silently truncate to `https:`. Fixed with the `$()` empty-splice idiom.
- The admin SPA's path-based routes collided with the mobile JSON API (`GET /reservations` served raw JSON to a browser refresh). Only surfaced when we seeded the academy-scale dataset and clicked around like a real admin; fixed by moving the SPA to hash routing.
- A "wedged" USB-serial port during beacon tuning turned out to be a stray process on the Mac holding the port and eating the beacon's replies — the transport was innocent. Lesson repeated all challenge: when a tool's output makes no sense, suspect the test rig before the device.

---

## The Revised Decision

Final decision:

- **CoreLocation region monitoring as the single source of presence truth** — low power, no persistent location indicator, works with the app closed. Ranging survives only as a foreground diagnostic.
- **UserNotifications + APNs as the product's spine**: the proportional grace ladder (nudge → optional second nudge → auto-release at 10% of the booking), collision and room-freed alerts, all pushed from the backend outbox.
- **Dumb beacons, smart backend**: beacons broadcast a fixed `(UUID, major, minor)`; every mapping, rule, and mutable thing lives in the Go backend (presence, booking, grace engine, JWT auth, admin CRUD, APNs, OpenAPI) with the Vue 3 admin panel on top. Moving a beacon to another room is a backend edit, never a device visit.
- **Hardware: nRF52 coin-cell tags** for production rooms (validated on nRF52840), with the ESP32-C6 retained as the USB-powered dev board.

What changed since Section 1, and why:

Almost everything except the language choice. Ranging — the reason we picked this problem's "obvious" API — was inverted into region monitoring after the first real build showed ranging fails exactly when it matters (app closed). The ESP32 went from "the product" to "the dev board" once we measured its power floor and radio quirks. And the backend grew from an assumed thin Zoom proxy into the actual core of the product, because the team's scenario work showed the hard problems (no-shows, ghost holds, collisions, grace timing) are booking-logic and notification-timing problems, not detection problems. Go itself held up: single-binary deploys with the frontend and docs embedded made every iteration a one-command ship.

---

## App Track Addendum

### About the Frameworks

The pairing is genuinely load-bearing in both directions. **CoreLocation** alone gives silent presence — the backend would know who is where, and no human behavior would change. **UserNotifications** (fed by APNs from the backend) alone would be a naggy calendar app with no ground truth. The product only exists at the joint: *presence-verified* notifications ("you're not checked in and your grace expires in 3 minutes", "your room just freed up") that fire because a phone physically crossed a beacon boundary. **CoreBluetooth** earns an honest mention as the third leg, though mostly in tooling: the phone-as-beacon broadcast that let us simulate a second room with no extra hardware, and the Mac scanner that was our only trustworthy view of what beacons actually radiate. **AuthenticationServices** (Sign in with Apple) ties bookings to the person the presence events belong to.

### About Accessibility and Localization

We did not localize, deliberately: the pilot audience is the Academy cohort, whose working language is English, and the string surface is still churning weekly — localizing mid-churn would have bought translation debt instead of features. The structure doesn't fight future localization (user-facing strings live in SwiftUI views, not in the backend). For accessibility we stayed on standard SwiftUI controls and the system SF type stack throughout (including the admin web panel), so Dynamic Type and VoiceOver get their default behavior, and status in the app is never conveyed by color alone (the diagnostics panel pairs color with labels and counters). We have not done a formal accessibility audit — that's stated as a gap, not implied as done.

### About Privacy

The app needs strikingly little: **region enter/exit events** (which room, when — never coordinates, never a movement trail; region monitoring doesn't keep the location indicator on), the **Apple ID email** at sign-in to attach bookings to an account, and an optional **APNs device token**. RSSI/ranging data never leaves the phone except in the explicit, user-initiated diagnostics snapshot. When the user says no: denying Always-location turns QuickRoom into a plain booking app — browsing, booking, and canceling rooms all still work; only auto check-in/out goes dark, and the diagnostics panel states exactly which permission is missing instead of nagging. Denying notifications means the grace nudges simply don't reach you (auto-release still protects everyone else's bookings). Account deletion cascades server-side: open bookings canceled, sessions revoked, push tokens removed.
