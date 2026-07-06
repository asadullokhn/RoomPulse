# QuickRoom

Presence-aware meeting-room management for the Apple Developer Academy. One iBeacon per room, an iPhone that checks you in and out automatically even with the app closed, and a backend that turns physical presence into Zoom Workspace check-in/out, no-show auto-release with proportional grace, and push notifications.

> **Read the [Tech Report](TECH_REPORT.md)** — the full journey: starting assumptions, what we built and field-tested, what we tried and dropped, the limitations we hit, and the final architecture decisions.

## How it works

```
Room beacon (UUID, major, minor)
        │ iBeacon advertisement
        ▼
iPhone — CoreLocation region monitoring (works with the app closed)
        │ enter / exit
        ▼
Go backend — presence, booking, grace engine, JWT auth, APNs
        │
        ├─► Zoom Workspace check-in / check-out
        ├─► Push notifications (grace nudges, room freed, collisions)
        └─► Vue 3 admin panel (day schedule grid, rooms, users, beacons)
```

Beacons are dumb, fixed `(UUID, major, minor)` broadcasters; which room a beacon belongs to lives in the backend. Moving a beacon to another room is a backend edit, never a device visit.

## Repository layout

| Path | What it is |
|---|---|
| `backend/` | Go backend (`cmd/quickroom`): presence, reservations, no-show grace engine, Sign in with Apple, JWT auth, admin CRUD, APNs push, OpenAPI spec. SQLite storage, single-binary deploy via Docker. |
| `frontend/` | Vue 3 + TypeScript admin SPA (login, day schedule grid, reservations, rooms, beacons, users, notification outbox). Built into the Go binary via `go:embed`. |
| `device/roompulse_beacon/` | ESP32-C6 beacon firmware — MAC-derived identity, USB-serial provisioning (dev/validation hardware). |
| `device/roompulse_beacon_nrf52/` | nRF52840 validation beacon — the production hardware path (coin-cell class, lower TX floor). |
| `device/tools/` | `blescan.swift` (Mac CoreBluetooth ground-truth scanner), `txtuner.py` (serial-to-HTTP bridge for phone-driven TX power tuning). |
| `mobile/RoomPulseBeaconLab/` | iOS dev-lab app: diagnostics readiness panel, background check-in counter, event log, phone-as-beacon broadcaster, TX tuner tab. |
| `scripts/seed-academy.sh` | Academy-scale seed (230 accounts, fully booked day) for load-testing the admin panel. |
| `docs/superpowers/` | Design specs and implementation plans for each feature. |
| [`TECH_REPORT.md`](TECH_REPORT.md) | Challenge tech report — the project's story and the reasoning behind every major decision. |

The production iOS app lives in its own repository: [Reishandy/QuickRoom](https://github.com/Reishandy/QuickRoom). The `mobile/` app here is the development and field-testing tool.

## Running the backend locally

The frontend must be built first — the Go binary embeds it:

```bash
cd frontend && npm install && npm run build   # writes to backend/internal/api/web/dist
cd ../backend && go run ./cmd/quickroom
```

Or with Docker:

```bash
cd backend && docker compose up --build
```

API docs (Swagger UI) are served at `/docs`, the OpenAPI spec at `/openapi.yaml`, and the admin panel at `/admin`. Zoom integration runs in mock mode unless real credentials are configured via environment variables (see `backend/docker-compose.yml` for the pass-throughs; secrets live in a gitignored `.env`).

## Key decisions (short version)

- **Region monitoring is the single source of presence truth** — low power, private, works with the app closed. Ranging was built first and dropped; the check-in boundary is the beacon's RF range, so TX-power tuning is a product feature.
- **Notifications are the spine** — a proportional grace ladder (nudge at 5% of the booking elapsed, auto-release at 10%) drives no-show handling, delivered over APNs.
- **Backend owns everything mutable** — beacon registry, room mapping, rules, docs, admin UI. Devices are install-and-forget.
- **ESP32-C6 for development, nRF52 for production** — the C6 can't sleep its radio and bottoms out at −12 dBm; nRF52 coin-cell tags run months and go quieter.

The reasoning behind each of these — including what broke along the way — is in the [Tech Report](TECH_REPORT.md).
