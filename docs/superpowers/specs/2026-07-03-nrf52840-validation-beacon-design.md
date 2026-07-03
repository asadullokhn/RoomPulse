# nRF52840 Validation iBeacon — Design

**Date:** 2026-07-03
**Status:** Approved
**Hardware:** ProMicro nRF52840 (nice!nano clone, Adafruit UF2 bootloader, enumerates as `nice_nano` VID 0x239A)

## Purpose

Validate the nRF52 successor path from the coin-cell beacon plan (`Projects/Personal/RoomPulse/Beacon Hardware — nRF52 Coin-Cell Plan.md`) before ordering 10–12 off-the-shelf tags:

1. Same iBeacon identity scheme as the ESP32-C6 → app and backend unchanged.
2. TX power tunable down to −20 dBm (the C6's floor is −12) → verify a genuinely room-sized region.
3. Live on-site range testing at the Academy, tuning TX power over USB serial without reflashing.

This is a **validation rig**, not production firmware. The C6's provisioning model (NVS persistence, MAC-derived identity) is explicitly out of scope.

## Firmware

New sketch `device/roompulse_beacon_nrf52/roompulse_beacon_nrf52.ino` (~80 lines). The existing C6 sketch is untouched.

- **Toolchain:** Arduino, Adafruit nRF52 core (`adafruit:nrf52`), compiled as `feather52840` (pin map irrelevant — no LED, no GPIO used).
- **Advertising:** iBeacon frame via Bluefruit `BLEBeacon`. **Non-connectable** advertising type — reliable on the Nordic SoftDevice (unlike the C6, where NONCONN silently never radiated). SoftDevice sleeps the CPU between adverts automatically.
- **Defaults:**
  - UUID `11111111-2222-3333-4444-555555555555` (org/building UUID, matches backend registry)
  - Major `1`, Minor `101` (ws-nusadua)
  - TX power **−16 dBm** (plan's starting point; nRF52840 supported set: −40, −20, −16, −12, −8, −4, 0, +2…+8 dBm)
  - Advertising interval **300 ms**
  - Measured-power byte **−75** (rough estimate for −16 dBm TX at 1 m; calibrated later against the live meter; affects distance display only, not check-in)
- **Live tuning over USB serial (CDC):**
  - `tx <dBm>` — set TX power from the SoftDevice's supported set, restart advertising
  - `minor <n>` — switch room identity
  - `adv <ms>` — set advertising interval
  - `show` — print current settings
  - No persistence: settings reset to defaults on power cycle. Fine for a test rig.

## Flashing

1. Install `adafruit:nrf52` core (one-time; board manager URL `https://adafruit.github.io/arduino-board-index/package_adafruit_index.json`).
2. Compile with FQBN `adafruit:nrf52:feather52840`.
3. Flash via UF2: double-tap RST → `NICENANO` mass-storage drive appears → copy the built `.uf2`. Serial DFU (`adafruit-nrfutil`, bundled with the core) as fallback.

## Verification

1. **Mac CoreBluetooth scan first** — confirm the iBeacon mfg-data (`4C00 0215 …`) actually radiates. C6 lesson: trust the BLE scan, never the serial log.
2. App's **Advanced live-signal meter** for the on-site walk-test: stand at the door vs. far wall, drop `tx` until the region is room-sized.
3. Backend already maps minors 101–110 to workspaces, so region enter/exit → check-in/out should fire with no backend or app change.

## Out of scope

- Coin-cell power measurement (board runs on USB for this test; battery pads exist for a later test)
- Settings persistence, MAC-derived identity, provisioning parity with the C6
- Any app or backend change
