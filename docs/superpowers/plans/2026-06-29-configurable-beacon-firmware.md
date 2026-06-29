# Configurable Beacon Firmware Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the ESP32-C6 beacon self-identify from its chip MAC (zero per-unit setup), allow installer-only serial overrides persisted in NVS, and run battery-efficiently — with no field config interface.

**Architecture:** Single Arduino sketch (`device/roompulse_beacon/roompulse_beacon.ino`), organized into sections: identity derivation, NVS-backed resolution, serial provisioning, advertising, power. The device is a dumb fixed `(UUID, major, minor)` broadcaster; room assignment lives in the backend (separate plan).

**Tech Stack:** Arduino C++, esp32 Arduino core 3.3.10, Bluedroid BLE (`BLEDevice.h`), `Preferences` (NVS). Verified with `arduino-cli` + serial + a macOS CoreBluetooth scanner.

## Global Constraints

- Board FQBN: `esp32:esp32:esp32c6:CDCOnBoot=cdc` (CDC required for USB serial).
- Advertise **connectable `ADV_IND` with no GATT services**. Do NOT force `ADV_TYPE_NONCONN_IND` (0x03) — it silently fails to radiate on this core (verified).
- Org UUID default: `11111111-2222-3333-4444-555555555555`. Provider-controlled; never surfaced to owners.
- **Wi-Fi is never initialized.**
- Provisioning is **USB-serial only** (physical-access gate). No wireless config interface.
- **Do not auto-commit** — the user handles all git operations. "Checkpoint" steps mark a reviewable stopping point only.
- Verification scanner: `/private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/b5ef3fac-1d82-45b8-94aa-1df9f18cde1d/scratchpad/blescan.swift` (prints any iBeacon's `uuid/major/minor`). Build/flash with:
  ```bash
  FQBN=esp32:esp32:esp32c6:CDCOnBoot=cdc
  arduino-cli compile --fqbn $FQBN device/roompulse_beacon
  arduino-cli upload  --fqbn $FQBN -p /dev/cu.usbmodem1101 device/roompulse_beacon
  ```

---

### Task 1: MAC-derived auto-ID + boot identity print

**Files:**
- Modify: `device/roompulse_beacon/roompulse_beacon.ino`

**Interfaces:**
- Produces: `uint16_t deriveMajor()`, `uint16_t deriveMinor()` (deterministic from `ESP.getEfuseMac()`); globals `g_major`, `g_minor` set in `setup()` before building the payload.

- [ ] **Step 1: Define the expected observation**

After flashing, the serial boot log must print a stable major/minor derived from the chip, e.g.:
```
RoomPulse beacon
identity: UUID=11111111-2222-3333-4444-555555555555 major=<N> minor=<M>  (auto)
```
`<N>`/`<M>` are identical on every reboot of the same board, and the BLE scan shows that exact major/minor.

- [ ] **Step 2: Add the CRC-16 helper and derivation**

Add near the top (after includes):
```cpp
#include "esp_mac.h"

static uint16_t crc16_ccitt(const uint8_t *data, size_t len, uint16_t seed) {
  uint16_t crc = seed;
  for (size_t i = 0; i < len; i++) {
    crc ^= (uint16_t)data[i] << 8;
    for (int b = 0; b < 8; b++)
      crc = (crc & 0x8000) ? (uint16_t)((crc << 1) ^ 0x1021) : (uint16_t)(crc << 1);
  }
  return crc;
}

static void chipMac(uint8_t mac[6]) {
  uint64_t e = ESP.getEfuseMac();          // 48-bit factory MAC
  for (int i = 0; i < 6; i++) mac[i] = (uint8_t)((e >> (8 * i)) & 0xFF);
}

static uint16_t deriveMajor() {
  uint8_t mac[6]; chipMac(mac);
  uint16_t v = crc16_ccitt(mac, 6, 0xFFFF);
  return (v == 0xFFFF) ? 0xFFFE : v;       // avoid the wildcard value
}
static uint16_t deriveMinor() {
  uint8_t mac[6]; chipMac(mac);
  uint16_t v = crc16_ccitt(mac, 6, 0x1D0F); // distinct seed
  return (v == 0xFFFF) ? 0xFFFE : v;
}
```

- [ ] **Step 3: Use the derived values and print them**

Replace the fixed `BEACON_MAJOR`/`BEACON_MINOR` use in `buildPayload()` and `setup()` with globals. Add globals and set them at the top of `setup()`:
```cpp
static uint16_t g_major;
static uint16_t g_minor;
```
In `setup()`, before `buildPayload()`:
```cpp
  g_major = deriveMajor();
  g_minor = deriveMinor();
  Serial.printf("identity: UUID=%s major=%u minor=%u  (auto)\n",
                BEACON_UUID, g_major, g_minor);
```
Change `buildPayload()` to use `g_major`/`g_minor` instead of the `#define`s:
```cpp
  adv += (char)((g_major >> 8) & 0xFF); adv += (char)(g_major & 0xFF);
  adv += (char)((g_minor >> 8) & 0xFF); adv += (char)(g_minor & 0xFF);
```

- [ ] **Step 4: Compile, flash, and read the boot log**

```bash
FQBN=esp32:esp32:esp32c6:CDCOnBoot=cdc
arduino-cli compile --fqbn $FQBN device/roompulse_beacon
arduino-cli upload  --fqbn $FQBN -p /dev/cu.usbmodem1101 device/roompulse_beacon
# capture ~4s of serial:
PORT=/dev/cu.usbmodem1101; stty -f "$PORT" 115200 raw -echo
( cat "$PORT" & cp=$!; sleep 4; kill $cp )
```
Expected: an `identity: ... major=<N> minor=<M> (auto)` line. Reset the board (re-flash or power cycle) and confirm `<N>/<M>` are unchanged.

- [ ] **Step 5: Confirm the radiated frame via BLE scan**

```bash
cd <scratchpad>; swift blescan.swift 2>&1 | grep -iE "11111111|scan ended"
```
Expected: `uuid=111111112222...555 major=<N> minor=<M>` — matching the serial line.

- [ ] **Step 6: Checkpoint** (no commit — user handles git). Confirm with the user that auto-ID is correct and stable.

---

### Task 2: NVS-backed overrides + serial provisioning

**Files:**
- Modify: `device/roompulse_beacon/roompulse_beacon.ino`

**Interfaces:**
- Consumes: `deriveMajor()`, `deriveMinor()`, `g_major`, `g_minor` from Task 1.
- Produces: NVS namespace `rpbeacon` with optional keys `uuid` (String), `major`/`minor` (int); `String g_uuid`; serial commands `show`, `set uuid <v>`, `set major <n>`, `set minor <n>`, `clear`.

- [ ] **Step 1: Define the expected observation**

`set minor 102` then reboot → serial shows `minor=102 (override)` and BLE scan shows minor 102. `clear` then reboot → back to `(auto)` with the MAC-derived minor. `set uuid <v>` changes the advertised UUID. Persists across power cycles.

- [ ] **Step 2: Load NVS on boot (resolution)**

Add include and globals:
```cpp
#include <Preferences.h>
static Preferences prefs;
static String   g_uuid;
static bool      g_majOverride = false, g_minOverride = false;
```
Replace the identity block in `setup()` with:
```cpp
  prefs.begin("rpbeacon", true);                 // read-only
  g_uuid = prefs.getString("uuid", BEACON_UUID);
  int mj = prefs.getInt("major", -1);
  int mn = prefs.getInt("minor", -1);
  prefs.end();
  g_majOverride = (mj >= 0); g_minOverride = (mn >= 0);
  g_major = g_majOverride ? (uint16_t)mj : deriveMajor();
  g_minor = g_minOverride ? (uint16_t)mn : deriveMinor();
  Serial.printf("identity: UUID=%s major=%u(%s) minor=%u(%s)\n",
                g_uuid.c_str(),
                g_major, g_majOverride ? "override" : "auto",
                g_minor, g_minOverride ? "override" : "auto");
```
Change `buildPayload()` to parse `g_uuid` (instead of the `BEACON_UUID` macro): `parseUuid(g_uuid.c_str(), uuid);`

- [ ] **Step 3: Add the serial command handler**

```cpp
static void applyAndRestart() {
  Serial.println("stored; rebooting to apply...");
  Serial.flush();
  ESP.restart();
}

static void processCommand(String line) {
  line.trim();
  if (line == "show") {
    Serial.printf("UUID=%s major=%u(%s) minor=%u(%s)\n",
      g_uuid.c_str(), g_major, g_majOverride?"override":"auto",
      g_minor, g_minOverride?"override":"auto");
    return;
  }
  if (line == "clear") {
    prefs.begin("rpbeacon", false); prefs.clear(); prefs.end();
    applyAndRestart(); return;
  }
  int sp = line.indexOf(' ');
  if (line.startsWith("set ") && sp > 0) {
    String rest = line.substring(4); rest.trim();
    int sp2 = rest.indexOf(' ');
    if (sp2 < 0) { Serial.println("usage: set <uuid|major|minor> <value>"); return; }
    String key = rest.substring(0, sp2);
    String val = rest.substring(sp2 + 1); val.trim();
    prefs.begin("rpbeacon", false);
    if (key == "uuid")       prefs.putString("uuid", val);
    else if (key == "major") prefs.putInt("major", val.toInt());
    else if (key == "minor") prefs.putInt("minor", val.toInt());
    else { prefs.end(); Serial.println("unknown key"); return; }
    prefs.end();
    applyAndRestart(); return;
  }
  Serial.println("commands: show | set uuid <v> | set major <n> | set minor <n> | clear");
}

static void pumpSerial() {
  static String line;
  while (Serial.available()) {
    char c = (char)Serial.read();
    if (c == '\n' || c == '\r') { if (line.length()) { processCommand(line); line = ""; } }
    else line += c;
  }
}
```
Call `pumpSerial();` at the top of `loop()` (before the heartbeat delay; reduce the delay to e.g. 1000 ms so commands are responsive).

- [ ] **Step 4: Compile, flash, exercise the commands**

```bash
arduino-cli compile --fqbn $FQBN device/roompulse_beacon
arduino-cli upload  --fqbn $FQBN -p /dev/cu.usbmodem1101 device/roompulse_beacon
# open an interactive monitor and type commands:
arduino-cli monitor -p /dev/cu.usbmodem1101 -c baudrate=115200
# type: set minor 102   -> "stored; rebooting..."   then on reboot: minor=102(override)
# type: clear           -> reboots back to (auto)
```
Expected: overrides persist across the reboot; `clear` returns to auto-ID.

- [ ] **Step 5: Confirm override on air**

After `set minor 102`, run the BLE scan; expected `minor=102`. After `clear`, expected the auto-ID minor again.

- [ ] **Step 6: Checkpoint** (no commit). Confirm provisioning + persistence with the user.

---

### Task 3: Battery optimization

**Files:**
- Modify: `device/roompulse_beacon/roompulse_beacon.ino`

**Interfaces:**
- Consumes: the advertising setup from Tasks 1–2.
- Produces: lower-power steady state; `ADV_INTERVAL_MS` default raised; CPU clock lowered; TX power reduced; optional light sleep.

- [ ] **Step 1: Define the expected observation**

The beacon still appears in the BLE scan (now at ~1 s cadence) at a noticeably weaker RSSI (lower TX power), and the board keeps advertising indefinitely (heartbeat still prints). If a USB power meter is available, average current drops versus the 100 ms / full-power baseline.

- [ ] **Step 2: Raise interval, drop clock, lower TX power**

Change the interval default:
```cpp
#define ADV_INTERVAL_MS 1000        // was 100 — big battery saving, ~1s detection latency
```
At the very start of `setup()` (before `BLEDevice::init`):
```cpp
  setCpuFrequencyMhz(80);
```
After `BLEDevice::init(...)` add:
```cpp
  BLEDevice::setPower(ESP_PWR_LVL_N12, ESP_BLE_PWR_TYPE_ADV);  // ~-12 dBm: covers one room, saves power
```

- [ ] **Step 3: Attempt automatic light sleep (with fallback)**

Add include and, after advertising starts, configure power management:
```cpp
#include "esp_pm.h"
  esp_pm_config_t pm = { .max_freq_mhz = 80, .min_freq_mhz = 10, .light_sleep_enable = true };
  if (esp_pm_configure(&pm) != ESP_OK) {
    Serial.println("light sleep unavailable on this build — relying on BLE modem sleep");
  }
```
Fallback if light sleep breaks advertising or isn't compiled in: remove the `esp_pm_configure` block; the controller's default BLE modem sleep between adv events plus the 1 s interval and 80 MHz clock still cut power substantially.

- [ ] **Step 4: Compile, flash, verify advertising survives**

```bash
arduino-cli compile --fqbn $FQBN device/roompulse_beacon
arduino-cli upload  --fqbn $FQBN -p /dev/cu.usbmodem1101 device/roompulse_beacon
cd <scratchpad>; swift blescan.swift 2>&1 | grep -iE "11111111|scan ended"
```
Expected: still detected (weaker RSSI than before), seen across a 10 s scan at ~1 s cadence; serial heartbeat continues (proves it didn't hang in sleep).

- [ ] **Step 5: Record a rough battery estimate**

If a meter is available, note average mA and divide the intended cell mAh to estimate runtime; write it into the spec's Risks section. If no meter, state the assumption used.

- [ ] **Step 6: Checkpoint** (no commit). Confirm power behavior + detection still works with the user.

---

### Task 4 (stretch): Retry non-connectable advertising

**Files:**
- Modify: `device/roompulse_beacon/roompulse_beacon.ino`

**Interfaces:**
- Consumes: advertising setup from prior tasks.

- [ ] **Step 1: Define the expected observation**

Goal: a non-connectable advert (more efficient, truly untouchable) that *actually radiates* on the C6. Success = BLE scan still shows the iBeacon. Failure = 0 adverts (the known core bug) → revert to connectable.

- [ ] **Step 2: Try the scannable, non-connectable type**

After `setAdvertisementData(advData)`:
```cpp
  advertising->setScanResponse(false);
  advertising->setAdvertisementType(0x02);   // ADV_TYPE_SCAN_IND (try; 0x03 NONCONN is known-broken here)
```

- [ ] **Step 3: Compile, flash, scan**

```bash
arduino-cli compile --fqbn $FQBN device/roompulse_beacon
arduino-cli upload  --fqbn $FQBN -p /dev/cu.usbmodem1101 device/roompulse_beacon
cd <scratchpad>; swift blescan.swift 2>&1 | grep -iE "11111111|scan ended"
```
Decision: if it shows the iBeacon, keep `0x02`. If "0 seen", remove the `setAdvertisementType` line entirely (default connectable `ADV_IND`, the proven-working path) and accept connectable-with-no-services.

- [ ] **Step 4: Checkpoint** (no commit). Report which type radiates.

---

## Self-Review

**Spec coverage:** Firmware-section requirements — auto-ID (Task 1), UUID/override in NVS + serial provisioning (Task 2), battery optimization incl. no-Wi-Fi/low-power (Task 3), non-connectable attempt + connectable fallback (Task 4), no field config interface (serial-only; nothing wireless added). Backend/app sections are explicitly out of this plan. Covered.

**Placeholder scan:** All code steps contain complete code; commands have expected output; `<scratchpad>` is the path given in Global Constraints. No "TBD"/"handle errors"/"similar to" placeholders.

**Type consistency:** `deriveMajor()/deriveMinor()` (Task 1) reused in Task 2; `g_major/g_minor/g_uuid` consistent across tasks; `processCommand/pumpSerial/applyAndRestart` defined where used; NVS namespace `rpbeacon` and keys `uuid/major/minor` consistent in load (Task 2 Step 2) and writes (Task 2 Step 3).
