# nRF52840 Validation iBeacon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Flash the ProMicro nRF52840 (nice!nano clone) as a RoomPulse iBeacon with live serial TX-power tuning, so the nRF52 successor path can be range-validated on-site today.

**Architecture:** Single Arduino sketch on the Adafruit nRF52 core. The Nordic SoftDevice broadcasts a non-connectable iBeacon frame and sleeps the CPU between adverts; a USB-CDC serial command loop retunes TX power / minor / interval live without reflashing. No persistence — this is a validation rig, not the production firmware.

**Tech Stack:** Arduino C++, `adafruit:nrf52` core (Bluefruit / S140 SoftDevice), `arduino-cli`, UF2/serial-DFU flashing, macOS CoreBluetooth scanner for verification.

**Spec:** `docs/superpowers/specs/2026-07-03-nrf52840-validation-beacon-design.md`

## Global Constraints

- Board FQBN: `adafruit:nrf52:feather52840` (pin map irrelevant — no LED/GPIO used; the board is a nice!nano clone with the Adafruit UF2 bootloader, enumerates VID 0x239A).
- Org UUID: `11111111-2222-3333-4444-555555555555`. Defaults: major `1`, minor `101`, TX `-16 dBm`, interval `300 ms`, measured-power `-75`.
- Advertising type: **non-connectable** (`BLE_GAP_ADV_TYPE_NONCONNECTABLE_NONSCANNABLE_UNDIRECTED`). This is the nRF52 — the C6's "NONCONN never radiates" gotcha does not apply, and non-connectable is what production tags use.
- **No persistence** — settings reset to defaults on power cycle by design.
- The existing C6 sketch `device/roompulse_beacon/roompulse_beacon.ino` is NOT touched.
- Verification truth source is the **BLE scan** (`device/tools/blescan.swift`), never the serial log (C6 lesson).
- Auto-commit is allowed for this project (project memory `feedback_auto_commit_deploy`); never add `Co-Authored-By`.
- No unit-test framework exists for Arduino sketches here — each task's test cycle is an observable check (compile output, radiated-frame scan, serial response) with expected output stated.

---

### Task 1: Install the Adafruit nRF52 core

**Files:** none (toolchain only)

**Interfaces:**
- Produces: installed `adafruit:nrf52` platform so `arduino-cli compile --fqbn adafruit:nrf52:feather52840` works in later tasks.

- [ ] **Step 1: Add the Adafruit board index and install the core**

```bash
arduino-cli config add board_manager.additional_urls https://adafruit.github.io/arduino-board-index/package_adafruit_index.json
arduino-cli core update-index
arduino-cli core install adafruit:nrf52
```

Expected: install completes; last line similar to `Platform adafruit:nrf52@1.7.x installed`. (Download is ~150 MB with tools; the core bundles `adafruit-nrfutil`, no pip install needed.)

- [ ] **Step 2: Verify the target board definition exists**

```bash
arduino-cli board listall | grep -i feather52840
```

Expected: a line containing `adafruit:nrf52:feather52840`.

---

### Task 2: Add the BLE scan verification tool to the repo

**Files:**
- Create: `device/tools/blescan.swift`

**Interfaces:**
- Produces: `swift device/tools/blescan.swift` — 10-second CoreBluetooth scan printing one line per device: `dev rssi=<n> name="<s>" svc=[...] mfg=<hex>`, then a summary `--- scan ended: N devices, M with mfg-data, K iBeacon-format ---`. iBeacon frames appear as `mfg=4C000215<uuid16B><major2B><minor2B><power1B>`.

- [ ] **Step 1: Copy the script from the old session scratchpad (it still exists)**

```bash
mkdir -p device/tools
cp "/private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/b5ef3fac-1d82-45b8-94aa-1df9f18cde1d/scratchpad/blescan.swift" device/tools/blescan.swift
```

If the old scratchpad is gone, create `device/tools/blescan.swift` with exactly this content:

```swift
import Foundation
import CoreBluetooth

final class Scanner: NSObject, CBCentralManagerDelegate {
    var central: CBCentralManager!
    var total = 0
    var withMfg = 0
    var ibeacons = 0
    var seen = Set<UUID>()
    override init() { super.init(); central = CBCentralManager(delegate: self, queue: nil) }

    func centralManagerDidUpdateState(_ c: CBCentralManager) {
        switch c.state {
        case .poweredOn:
            print("BT poweredOn — scanning 10s (all devices)...")
            c.scanForPeripherals(withServices: nil,
                                 options: [CBCentralManagerScanOptionAllowDuplicatesKey: false])
        case .poweredOff:    print("RESULT: Mac Bluetooth is OFF"); exit(2)
        case .unauthorized:  print("RESULT: Bluetooth permission denied for this process"); exit(3)
        case .unsupported:   print("RESULT: BLE unsupported"); exit(4)
        default:             print("BT state=\(c.state.rawValue)")
        }
    }

    func centralManager(_ c: CBCentralManager, didDiscover p: CBPeripheral,
                        advertisementData: [String: Any], rssi RSSI: NSNumber) {
        guard !seen.contains(p.identifier) else { return }
        seen.insert(p.identifier)
        total += 1
        let name = (advertisementData[CBAdvertisementDataLocalNameKey] as? String) ?? p.name ?? "—"
        var mfgHex = ""
        if let mfg = advertisementData[CBAdvertisementDataManufacturerDataKey] as? Data {
            withMfg += 1
            let b = [UInt8](mfg)
            mfgHex = b.map { String(format:"%02X",$0) }.joined()
            if b.count >= 4, b[0]==0x4C, b[1]==0x00, b[2]==0x02, b[3]==0x15 { ibeacons += 1 }
        }
        let svc = (advertisementData[CBAdvertisementDataServiceUUIDsKey] as? [CBUUID])?.map{$0.uuidString}.joined(separator:",") ?? ""
        print("dev rssi=\(RSSI) name=\"\(name)\" svc=[\(svc)] mfg=\(mfgHex.isEmpty ? "none" : mfgHex)")
    }
}

let s = Scanner()
RunLoop.main.run(until: Date().addingTimeInterval(11))
print("--- scan ended: \(s.total) devices, \(s.withMfg) with mfg-data, \(s.ibeacons) iBeacon-format ---")
```

- [ ] **Step 2: Baseline scan (proves the tool runs; also shows what's on air before our beacon)**

```bash
swift device/tools/blescan.swift
```

Expected: `BT poweredOn — scanning 10s`, some `dev ...` lines, then `--- scan ended: ...`. Note whether any `4C000215111111112222...` frame already exists (that would be the ESP32-C6 still powered somewhere — fine, its minor differs from 101 unless overridden).

- [ ] **Step 3: Commit**

```bash
git add device/tools/blescan.swift
git commit -m "Add CoreBluetooth scan tool for beacon verification" -- device/tools/blescan.swift
```

---

### Task 3: Write the beacon sketch and compile it

**Files:**
- Create: `device/roompulse_beacon_nrf52/roompulse_beacon_nrf52.ino`

**Interfaces:**
- Consumes: `adafruit:nrf52` core from Task 1.
- Produces: compiled sketch; serial command protocol `show` / `tx <dBm>` / `minor <n>` / `adv <ms>` used by Task 5.

- [ ] **Step 1: Check the core's BLEBeacon byte order (determines whether the sketch must swap major/minor)**

```bash
find ~/Library/Arduino15/packages/adafruit/hardware/nrf52 -name "BLEBeacon.cpp" -exec grep -n "swap16\|_major" {} \;
```

Expected: lines from `BLEBeacon::setMajorMinor` showing whether the library applies `__swap16` itself. If it **does** swap, pass plain `1` / `101` (as the code below does). If it does **not**, wrap both with `__swap16(...)` in the `BLEBeacon` constructor call below. The radiated-frame check in Task 4 Step 3 is the final arbiter.

- [ ] **Step 2: Create the sketch**

Create `device/roompulse_beacon_nrf52/roompulse_beacon_nrf52.ino`:

```cpp
// RoomPulse validation beacon — ProMicro nRF52840 (nice!nano clone).
// Validation rig: no persistence, settings reset to defaults on power cycle.
// Spec: docs/superpowers/specs/2026-07-03-nrf52840-validation-beacon-design.md

#include <bluefruit.h>

// iBeacon identity — must match the backend registry.
static const uint8_t BEACON_UUID[16] = {
  0x11, 0x11, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33,
  0x44, 0x44, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55,
};
static const int8_t MEASURED_POWER = -75;  // rough 1 m RSSI at -16 dBm TX; calibrate on-site later

static uint16_t g_major   = 1;
static uint16_t g_minor   = 101;   // ws-nusadua
static int8_t   g_txPower = -16;   // dBm
static uint16_t g_advMs   = 300;

// TX levels the nRF52840 SoftDevice accepts.
static const int8_t TX_LEVELS[] = { -40, -20, -16, -12, -8, -4, 0, 2, 3, 4, 5, 6, 7, 8 };

static bool txLevelValid(int v) {
  for (int8_t lvl : TX_LEVELS) {
    if (lvl == v) return true;
  }
  return false;
}

static void startAdvertising() {
  Bluefruit.Advertising.stop();
  Bluefruit.Advertising.clearData();
  Bluefruit.ScanResponse.clearData();

  BLEBeacon beacon(BEACON_UUID, g_major, g_minor, MEASURED_POWER);
  beacon.setManufacturer(0x004C);  // Apple

  Bluefruit.Advertising.setBeacon(beacon);
  Bluefruit.Advertising.setType(BLE_GAP_ADV_TYPE_NONCONNECTABLE_NONSCANNABLE_UNDIRECTED);
  Bluefruit.Advertising.restartOnDisconnect(false);
  Bluefruit.Advertising.setIntervalMS(g_advMs, g_advMs);
  Bluefruit.Advertising.setFastTimeout(0);
  Bluefruit.Advertising.start(0);  // 0 = advertise forever
}

static void printSettings() {
  Serial.println("RoomPulse nRF52840 validation beacon");
  Serial.println("uuid  11111111-2222-3333-4444-555555555555");
  Serial.printf("major %u  minor %u\n", g_major, g_minor);
  Serial.printf("tx %d dBm  adv %u ms  measured-power %d\n", g_txPower, g_advMs, MEASURED_POWER);
}

static void handleCommand(char *line) {
  int v;
  if (strcmp(line, "show") == 0) {
    printSettings();
  } else if (sscanf(line, "tx %d", &v) == 1) {
    if (!txLevelValid(v)) {
      Serial.println("tx must be one of: -40 -20 -16 -12 -8 -4 0 2 3 4 5 6 7 8");
      return;
    }
    g_txPower = (int8_t)v;
    Bluefruit.setTxPower(g_txPower);
    startAdvertising();
    Serial.printf("ok tx=%d dBm\n", g_txPower);
  } else if (sscanf(line, "minor %d", &v) == 1) {
    if (v < 1 || v > 65535) {
      Serial.println("minor must be 1..65535");
      return;
    }
    g_minor = (uint16_t)v;
    startAdvertising();
    Serial.printf("ok minor=%u\n", g_minor);
  } else if (sscanf(line, "adv %d", &v) == 1) {
    if (v < 100 || v > 2000) {
      Serial.println("adv must be 100..2000 ms");
      return;
    }
    g_advMs = (uint16_t)v;
    startAdvertising();
    Serial.printf("ok adv=%u ms\n", g_advMs);
  } else if (line[0] != '\0') {
    Serial.println("commands: show | tx <dBm> | minor <n> | adv <ms>");
  }
}

void setup() {
  Serial.begin(115200);  // USB CDC — baud value is ignored

  Bluefruit.begin();
  Bluefruit.autoConnLed(false);
  Bluefruit.setTxPower(g_txPower);
  Bluefruit.setName("rp-nrf52");

  startAdvertising();
  printSettings();
}

void loop() {
  static char buf[32];
  static uint8_t len = 0;
  while (Serial.available()) {
    char c = (char)Serial.read();
    if (c == '\r') continue;
    if (c == '\n') {
      buf[len] = '\0';
      len = 0;
      handleCommand(buf);
      continue;
    }
    if (len < sizeof(buf) - 1) buf[len++] = c;
  }
  delay(50);  // FreeRTOS vTaskDelay — lets the SoftDevice sleep between adverts
}
```

- [ ] **Step 3: Compile**

```bash
arduino-cli compile --fqbn adafruit:nrf52:feather52840 device/roompulse_beacon_nrf52
```

Expected: `Sketch uses NNNNN bytes` and exit 0. (First Bluefruit compile is slow — a minute or two.) If `setIntervalMS` is not found on the installed core version, replace with `Bluefruit.Advertising.setInterval((uint16_t)(g_advMs / 0.625f), (uint16_t)(g_advMs / 0.625f));` (units of 0.625 ms).

- [ ] **Step 4: Commit**

```bash
git add device/roompulse_beacon_nrf52/roompulse_beacon_nrf52.ino
git commit -m "Add nRF52840 validation beacon sketch with live serial tuning" -- device/roompulse_beacon_nrf52
```

---

### Task 4: Flash and verify the radiated frame

**Files:** none (flash + verify)

**Interfaces:**
- Consumes: compiled sketch (Task 3), `device/tools/blescan.swift` (Task 2).
- Produces: board on-air as `UUID 1111…5555 / major 1 / minor 101` — the state Task 5 tunes.

- [ ] **Step 1: Find the board's state and port**

```bash
ls /Volumes/ | grep -iE "nice|nano|boot" ; ls /dev/cu.usbmodem*
```

Two possible states: a `NICENANO`-style volume mounted (board is sitting in the UF2 bootloader) or only a CDC port (application/bootloader CDC). Either way note the `/dev/cu.usbmodem*` port name.

- [ ] **Step 2: Flash — Option A, serial DFU (try first, hands-free)**

```bash
arduino-cli upload --fqbn adafruit:nrf52:feather52840 -p /dev/cu.usbmodem1101 device/roompulse_beacon_nrf52
```

(Substitute the actual port from Step 1.) Expected: `adafruit-nrfutil` reports `Device programmed.` The port may re-enumerate under a new name afterwards.

**Option B — UF2 drag-drop (if Option A fails against the clone bootloader):** ask the user to double-tap the RST button; a `NICENANO` volume mounts. Then:

```bash
arduino-cli compile --fqbn adafruit:nrf52:feather52840 --output-dir /private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/05ee78b8-4cdd-4ba6-b109-538985c4bad4/scratchpad/nrf52build device/roompulse_beacon_nrf52
cd /private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/05ee78b8-4cdd-4ba6-b109-538985c4bad4/scratchpad
curl -sLO https://raw.githubusercontent.com/microsoft/uf2/master/utils/uf2conv.py
curl -sLO https://raw.githubusercontent.com/microsoft/uf2/master/utils/uf2families.json
python3 uf2conv.py -c -f 0xADA52840 -b 0x26000 -o beacon.uf2 nrf52build/roompulse_beacon_nrf52.ino.hex
cp beacon.uf2 /Volumes/NICENANO/
```

(Use the actual volume name from Step 1 if it differs.) The board reboots into the app when the copy finishes.

- [ ] **Step 3: Verify the radiated iBeacon frame — the truth source**

```bash
swift device/tools/blescan.swift 2>&1 | grep -iE "4C000215|scan ended"
```

Expected: a line whose mfg hex is `4C000215` + `11111111222233334444555555555555` (UUID) + `0001` (major) + `0065` (minor 101) + `B5` (measured power −75), and the summary shows at least 1 `iBeacon-format`.

If major/minor read byte-swapped (`0100` / `6500`): apply the `__swap16` wrap noted in Task 3 Step 1, recompile, reflash, re-verify.

- [ ] **Step 4: Confirm serial boot log is reachable**

```bash
PORT=$(ls /dev/cu.usbmodem*); stty -f $PORT 115200
(cat $PORT > /private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/05ee78b8-4cdd-4ba6-b109-538985c4bad4/scratchpad/serial_out.txt &); sleep 0.5
printf 'show\n' > $PORT; sleep 1; pkill -f "cat $PORT"
cat /private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/05ee78b8-4cdd-4ba6-b109-538985c4bad4/scratchpad/serial_out.txt
```

Expected output contains:

```
RoomPulse nRF52840 validation beacon
uuid  11111111-2222-3333-4444-555555555555
major 1  minor 101
tx -16 dBm  adv 300 ms  measured-power -75
```

---

### Task 5: Verify live tuning and hand off the on-site walk-test

**Files:** none (verification + handoff)

**Interfaces:**
- Consumes: on-air beacon (Task 4), serial protocol (Task 3).
- Produces: validated tuning loop; range findings recorded by the user on-site.

- [ ] **Step 1: Verify `tx` changes take effect on-air**

Send `tx -40`, then scan and compare RSSI to the Task 4 baseline (Mac and board must not move between scans):

```bash
PORT=$(ls /dev/cu.usbmodem*); printf 'tx -40\n' > $PORT; sleep 1
swift device/tools/blescan.swift 2>&1 | grep -iE "4C000215111111112222" 
```

Expected: the beacon line still appears, with RSSI clearly lower (typically 15–25 dB down at −40 vs −16). Restore with `printf 'tx -16\n' > $PORT`.

- [ ] **Step 2: Verify `minor` switches identity**

```bash
PORT=$(ls /dev/cu.usbmodem*); printf 'minor 105\n' > $PORT; sleep 1
swift device/tools/blescan.swift 2>&1 | grep -iE "4C000215111111112222"
```

Expected: mfg hex now ends `...00010069B5` (105 = 0x69). Restore: `printf 'minor 101\n' > $PORT`.

- [ ] **Step 3: Hand off the walk-test to the user (they are at the Academy)**

Tell the user the beacon is live and how to drive it:

- Beacon: UUID `1111…5555`, major 1, minor 101 (ws-nusadua) at −16 dBm / 300 ms. Power the board from any USB source; keep the Mac connected if live tuning is wanted.
- In the app: **Advanced → live-signal meter** — stand at the door, then the far wall, then outside the room.
- Tune: `tx -20` (or `-40`) over serial if the region reaches past the room; `tx -12`/`-8` if detection is flaky inside the room.
- Check-in/out: region enter/exit for minor 101 should fire against the backend with no app/backend change.

- [ ] **Step 4: Record results**

After the user reports range findings, append a "Validation results — 2026-07-03" section to the Obsidian note `Projects/Personal/RoomPulse/Beacon Hardware — nRF52 Coin-Cell Plan.md` (which TX level gave a room-sized region, detection reliability, any flapping) and update project memory `project_beacon_device.md` with the nRF52840 toolchain facts (core install, FQBN, flash path, scan tool now at `device/tools/blescan.swift`).

- [ ] **Step 5: Commit any doc updates**

```bash
git add docs/superpowers/plans/2026-07-03-nrf52840-validation-beacon.md
git commit -m "Add nRF52840 validation beacon implementation plan" -- docs/superpowers/plans/2026-07-03-nrf52840-validation-beacon.md
```
