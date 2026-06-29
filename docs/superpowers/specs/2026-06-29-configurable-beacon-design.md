# Configurable RoomPulse Beacons — Design Spec

Date: 2026-06-29
Status: design approved (user to review written spec)
Scope: firmware **and** backend (decomposed below)

## Problem

RoomPulse beacons (ESP32-C6) currently broadcast a hard-coded iBeacon identity baked into the firmware. We need them to be:

- **Configurable** per deployment (room assignment), without reflashing each unit.
- **Persistent** — identity survives power cycles and dead batteries.
- **Secure** — nobody in the room can change or hijack a beacon.
- **Battery-efficient** — they run on a cell, placed under/on/center of a table, install-once-forget.
- **Easy to manage** — the place owner manages ~10 beacons per org from the backend website, not by touching devices.
- **UUID locked to the provider** — the owner can never change the org UUID; at most they reassign rooms.

## Core insight: two-tier identity

The device's `(UUID, major, minor)` is a **permanent hardware fingerprint** — like a serial number. *"Which room is this beacon?"* is answered by a **backend mapping** from that fingerprint to a Zoom workspace.

Therefore: to move a beacon to a different room, you edit the **backend**, never the device. The beacon stays dumb and untouched for its whole life.

| What | Who sets it | When | Owner editable? |
|---|---|---|---|
| UUID (org/building) | Provider | flashed / one-time serial | No — never exposed |
| Beacon fingerprint (major, minor) | Auto-derived from chip MAC | every boot, deterministically | No |
| Room assignment (fingerprint → workspace) | **Owner** | backend website | **Yes** |
| TX power / advertising interval | firmware default | — | No |

This makes "persists across restart" automatic: the fingerprint is recomputed identically from the chip MAC on every boot — there is nothing per-unit to store or that can drift.

## Goals / Non-goals

**Goals**
- Auto-ID firmware: unique, stable `(major, minor)` from the factory MAC; no per-unit setup.
- Org UUID configurable once (compile default + optional serial override in NVS), provider-controlled.
- No field config interface on the device (no listening BLE GATT, no Wi-Fi) → max battery, max security.
- Battery optimization: light-sleep, long advertising interval, low TX power, Wi-Fi never initialized.
- Backend: support **many beacons → one room** and an owner-facing room-assignment screen.

**Non-goals (this design)**
- Remote/OTA reconfiguration of the device (deliberately omitted — it would cost battery and add attack surface; the two-tier model removes the need).
- Per-user accounts/roles for the owner UI beyond existing backend auth (out of scope; reuse what the backend has).
- Months-per-charge battery life (ESP32-C6 can't reach nRF52-class beacon longevity; see Risks).

## Component 1 — Firmware (`device/roompulse_beacon/`)

### Identity
- **Default: auto-ID.** `major` and `minor` derived deterministically from `ESP.getEfuseMac()` (e.g. two CRC-16s over the 6 MAC bytes with distinct seeds → ~32-bit spread; avoid `0xFFFF` wildcard). Same chip → same ID forever. Collision probability across a 10-unit fleet is negligible.
- **Optional explicit override.** A serial command can pin `major`/`minor` to chosen values, stored in NVS; takes precedence over auto-ID. Covers fleets that want human-friendly numbers (101, 102, …) or making a replacement unit inherit a dead one's identity. ("Both" modes supported; auto-ID is the default path.)
- **UUID.** Compile-time default (`11111111-2222-3333-4444-555555555555`) overridable by serial → NVS, so identical firmware can serve a second org without recompiling.

### Storage (NVS via `Preferences`)
- Namespace `rpbeacon`: optional `uuid` (string), optional `major`/`minor` (uint16) overrides.
- Boot logic: load UUID (NVS → else compile default); load major/minor (NVS override → else auto-ID from MAC).
- Default path needs **no** NVS writes at all — determinism does the persistence.

### Provisioning (USB serial, installer-only)
- Physical USB access is the security gate; the owner/occupants never use it.
- Minimal line commands: `show` (print current identity), `set uuid <uuid>`, `set major <n>`, `set minor <n>`, `clear` (wipe overrides → return to auto-ID). Changes persist to NVS immediately.
- **At every boot the device prints its resolved `UUID / major / minor`** so the installer can read it off to register/label the unit.

### Advertising + battery
- iBeacon payload built by hand (proven correct frame); **connectable `ADV_IND` with no GATT services** by default. (Non-connectable `ADV_NONCONN_IND` is more efficient and untouchable, but it does **not radiate** on the C6 + esp32 core 3.3.10 — see prior finding. Implementation will retry non-connectable via an alternate path/NimBLE; if it still won't radiate, connectable-with-no-services is the fallback — connecting yields nothing, so nothing is exposed.)
- **Advertising interval** default ~1000 ms (vs 100 ms) — trades 1–2 s of detection latency for a large power saving.
- **Light sleep** between advertising events (`esp_pm` automatic light sleep + BLE modem sleep).
- **Low TX power** tuned to cover one room — saves power and limits cross-room signal bleed (better room accuracy).
- **Wi-Fi never initialized.** CPU clock lowered (80 MHz).

### Verification
- `swift blescan.swift` (CoreBluetooth scanner) confirms the exact frame radiates and is correct; macOS surfaces iBeacon mfg-data, so a real beacon shows up.
- Flash two units → confirm distinct auto-IDs.
- Sanity-check current draw before/after light-sleep if a meter is available; record rough battery-life estimate for the chosen cell.

## Component 2 — Backend (`backend/`)

### Data model: many beacons → one room
- Current store keys **one beacon per workspace** (`beacons map[string]domain.Beacon // workspaceID -> Beacon`). Change to key by beacon fingerprint so multiple beacons can map to the same workspace:
  - `beacons map[BeaconKey]domain.Beacon` where `BeaconKey = (uuid, major, minor)` (or a beacon id), each carrying its `WorkspaceID`.
- `GET /beacons` continues to return the full list (now possibly several entries per workspace). The app already keys regions per entry, so this composes.
- Persistence file (`beacons.json`) format updated accordingly; migration: read old shape, convert to list.

### Admin API + owner UI
- Endpoints to list unassigned/seen beacon fingerprints and assign/unassign them to a workspace (extend existing `SetBeacon`).
- Owner-facing screen: list beacons (by fingerprint/label), assign each to a room, support two beacons → one room. Reuse existing backend admin/auth; UUID is shown read-only (provider-controlled), only the room mapping is editable.
- Nice-to-have: a beacon is "seen" once its fingerprint is registered (manually from the serial readout, or later auto-registered when a phone first ranges an unknown fingerprint).

## Component 3 — App (`mobile/RoomPulseBeaconLab/`)

- **Ranging** already matches on UUID only (`CLBeaconIdentityConstraint(uuid:)`), so it sees all fingerprints with no change.
- **Region monitoring** registers one `CLBeaconRegion` per beacon today via `RoomPreset`. For many-per-room:
  - `RoomPreset.id` is currently `workspaceID` (unique) — breaks when two beacons share a workspace. Introduce a per-beacon identity for the registry/regions while keeping room resolution by workspace.
  - Watch iOS's **20 monitored-region limit**: with ≤20 beacons monitor per fingerprint; beyond that, fall back to monitoring one region for the org UUID (building enter/exit) and rely on ranging for room precision.
- `room(major:minor:)` resolution maps any of a room's fingerprints to the same workspace.

## Security model

- **No device attack surface:** no listening radio for config; identity is fixed/derived. Reconfiguration requires physical USB + the serial command — i.e. the device in hand.
- **UUID containment:** owners never see or set the UUID; it's provider-set and shown read-only in the admin UI.
- **Backend auth:** room assignment goes through the backend's existing auth; no new public mutation surface.

## Decomposition & phasing

Three sub-projects, each independently shippable; build in order:

1. **Firmware** (in hand, verifiable now) — auto-ID + UUID/override in NVS + serial provisioning + battery optimization. Verified via BLE scan.
2. **Backend** — many-per-room data model + admin assign API + persistence migration.
3. **App** — many-per-room region handling + 20-region strategy.

Each gets its own implementation plan. This round starts with the firmware.

## Risks / open items

- **Battery life:** ESP32-C6 is not a coin-cell beacon chip. Even optimized, expect days–weeks on a small LiPo, not months. If months-per-charge is required across 10 beacons, that's a hardware decision (e.g. nRF52), not firmware. Flag the measured estimate after implementation.
- **Non-connectable advertising on C6:** may remain unavailable on this core; connectable-with-no-services is the accepted fallback.
- **Auto-ID labeling:** with auto-ID, the installer must capture each unit's fingerprint (serial readout / label) to register it. An "assign the beacon I'm next to" app flow could remove this later (future).
