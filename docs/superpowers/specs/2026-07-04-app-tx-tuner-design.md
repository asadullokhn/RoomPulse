# TX Tuner Tab in RoomPulseBeaconLab — Design

**Date:** 2026-07-04
**Status:** Approved
**Context:** The Mac-side `device/tools/txtuner.py` exposes `GET /api/state` and `POST /api/tx {"level": <dBm>}` over the LAN, bridging HTTP to the nRF52840 beacon's USB serial. The user wants the same tuning UX inside the iOS dev-lab app so TX experiments and the live signal meter live on one screen.

## Placement

A **third tab** ("TX Tuner", `slider.horizontal.3`) in `ContentView`'s `TabView`. Not inside MonitorView's Advanced section: MonitorView has uncommitted changes from the in-flight diagnostics workstream, and the tuner needs the space. Live RSSI still appears next to the TX buttons because the new view observes `RoomMonitor.shared.liveBeacons` (already `@Published`; ranging is foreground-only, which is fine — the tuner is a foreground screen).

## New files (follow existing patterns)

- `mobile/RoomPulseBeaconLab/Sources/Net/TunerClient.swift` — static enum like `DiagClient`. `struct TunerState: Decodable { major, minor, tx, adv: Int; uuid: String? }`. Two calls, both completing on the main queue with `Result<TunerState, TunerError>`:
  - `fetchState` → `GET {AppSettings.tunerBaseURL}/api/state`
  - `setTx(level: Int)` → `POST {…}/api/tx`, JSON body `{"level": n}`
  - `TunerError` carries the server's `{"error": …}` text when present, else a transport description ("Mac unreachable — is txtuner.py running?"). Timeout 8 s (matches DiagClient).
- `mobile/RoomPulseBeaconLab/Sources/Views/TunerView.swift` — the screen, top to bottom:
  1. **State card**: current TX large (`+8 dBm` style), `minor · adv · major` metadata line, refresh button. Loads on appear.
  2. **Progress + banner**: linear progress bar animating while a change is in flight; completes only when the verified state returns. Green "Applied −16 dBm" / red error text. All TX buttons disabled while busy.
  3. **Live signal rows**: for each `RoomMonitor.shared.liveBeacons` entry: room name + RSSI dBm (foreground ranging, same data as Advanced's meter).
  4. **TX buttons**: primary five, full-width with sublabels — `+8 max range`, `0 tag default`, `−12 C6 floor`, `−16 room start`, `−20 room tight`; secondary nine (`+7 +6 +5 +4 +3 +2 −4 −8 −40`) in a 3-column grid. Active level highlighted from the last known state.
  5. **Server row**: editable `tunerBaseURL` text field (persists via AppSettings).

## Small modifications

- `Settings/AppSettings.swift`: add `tunerBaseURL: String`, default `http://Asadullokhs-MacBook-Pro.local:8880`.
- `ContentView.swift`: add the third tab.
- `Sources/Info.plist`: `NSAppTransportSecurity → NSAllowsLocalNetworking = true` (ATS blocks plain HTTP otherwise) and `NSLocalNetworkUsageDescription` (iOS local-network permission prompt on first request).

## Level set (must match firmware/tuner)

`-40 -20 -16 -12 -8 -4 0 2 3 4 5 6 7 8` — single source in `TunerClient` (`TunerClient.levels`), consumed by the view.

## Build & verify

Per deployment memory: `xcodegen generate`, build with full Xcode (`/Applications/Xcode.app`), signing team `Y7ZZ5G2T3Y`, install/launch via `xcrun devicectl` (device unlocked). Verification: build succeeds; on-device — open the tab, state card shows live values, tap a level → bar completes → beacon state changes (cross-checked with `blescan.swift` RSSI), unplug scenario shows the error banner.

## Out of scope

- minor/adv editing, Bonjour discovery, changes to MonitorView/RoomMonitor, App Store polish.
