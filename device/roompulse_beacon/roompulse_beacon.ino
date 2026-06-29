// RoomPulse room beacon — ESP32-C6 Super Mini
// =============================================
// Broadcasts a static Apple iBeacon advertisement so the RoomPulse iPhone app
// (CoreLocation region monitoring) detects the room and auto checks the user in.
// The phone can't be the permanent beacon — iOS stops advertising in the
// background — so this little board does it 24/7 instead.
//
// Identity scheme (must match the backend + app):
//   UUID  = org/building   (one value for the whole deployment)
//   Major = floor / zone
//   Minor = room
// Backend default registry (backend/internal/store/persist.go):
//   1/101 ws-nusadua  1/102 ws-petang  1/103 ws-bedugul  ... 1/110 ws-penida
//
// --- CONFIG: edit these to repurpose this board for a different room, then
// --- reflash. (Secure over-the-air / authenticated config is a later iteration.)
#define BEACON_UUID    "11111111-2222-3333-4444-555555555555"  // org/building
#define BEACON_MAJOR   1     // floor / zone
#define BEACON_MINOR   101   // room  -> 101 = ws-nusadua
#define DEVICE_NAME    "RoomPulse-101"
// Measured RSSI at 1 m, used by the phone for distance estimates. Calibrate in
// the field (read RSSI at exactly 1 m, put that value here as a signed dBm).
#define MEASURED_POWER (-59)
// iBeacon advertising interval (ms). Balance: long enough to save some power,
// short enough for reliable/snappy check-in. NOTE: on this C6 the CPU can't sleep
// (see battery note below), so the CPU draw dominates and the interval is only a
// minor battery factor — hence we keep it short for reliable detection. ~1s made
// detection sparse/intermittent. Units are 0.625 ms.
#define ADV_INTERVAL_MS 300
// =============================================

#include <BLEDevice.h>
#include <BLEUtils.h>
#include <BLEServer.h>
#include <Preferences.h>

static BLEAdvertising *advertising;
static Preferences prefs;
static uint16_t g_major;
static uint16_t g_minor;
static String   g_uuid;
static bool      g_majOverride = false, g_minOverride = false;

// Derive a unique, stable 16-bit value from the chip's factory MAC. Same chip ->
// same value on every boot, so the beacon's identity needs no per-unit setup and
// "persists across restart" by construction.
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

// Parse "11111111-2222-..." into 16 raw bytes, big-endian as written (the order
// iBeacon transmits the UUID on the wire).
static void parseUuid(const char *s, uint8_t out[16]) {
  int n = 0;
  for (const char *p = s; *p && n < 16; ) {
    if (*p == '-') { p++; continue; }
    auto hex = [](char c) -> int {
      if (c >= '0' && c <= '9') return c - '0';
      if (c >= 'a' && c <= 'f') return c - 'a' + 10;
      if (c >= 'A' && c <= 'F') return c - 'A' + 10;
      return 0;
    };
    out[n++] = (hex(p[0]) << 4) | hex(p[1]);
    p += 2;
  }
}

// Build the full 30-byte advertising payload by hand so byte order is explicit
// (major/minor are big-endian per spec — no reliance on library quirks).
static String buildPayload() {
  uint8_t uuid[16];
  parseUuid(g_uuid.c_str(), uuid);

  String adv = "";
  // AD: Flags — LE General Discoverable + BR/EDR not supported
  adv += (char)0x02; adv += (char)0x01; adv += (char)0x06;
  // AD: Manufacturer Specific Data (iBeacon), 26 bytes follow
  adv += (char)0x1A; adv += (char)0xFF;
  adv += (char)0x4C; adv += (char)0x00;   // Apple company id (0x004C, little-endian)
  adv += (char)0x02; adv += (char)0x15;   // iBeacon type + remaining length (21)
  for (int i = 0; i < 16; i++) adv += (char)uuid[i];        // proximity UUID
  adv += (char)((g_major >> 8) & 0xFF); adv += (char)(g_major & 0xFF);  // major BE
  adv += (char)((g_minor >> 8) & 0xFF); adv += (char)(g_minor & 0xFF);  // minor BE
  adv += (char)((int8_t)MEASURED_POWER);   // measured power (signed)
  return adv;
}

// --- Provisioning over USB serial (installer-only; physical access is the gate).
// Persists to NVS, then reboots so the new identity takes effect cleanly.
static void applyAndRestart() {
  Serial.println("stored; rebooting to apply...");
  Serial.flush();
  ESP.restart();
}

static void processCommand(String line) {
  line.trim();
  if (line == "show") {
    Serial.printf("UUID=%s major=%u(%s) minor=%u(%s)\n",
      g_uuid.c_str(), g_major, g_majOverride ? "override" : "auto",
      g_minor, g_minOverride ? "override" : "auto");
    return;
  }
  if (line == "clear") {
    prefs.begin("rpbeacon", false); prefs.clear(); prefs.end();
    applyAndRestart(); return;
  }
  if (line.startsWith("set ")) {
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

static void logHex(const String &p) {
  Serial.printf("payload (%d bytes): ", p.length());
  for (size_t i = 0; i < p.length(); i++) Serial.printf("%02X", (uint8_t)p[i]);
  Serial.println();
}

void setup() {
  Serial.begin(115200);
  delay(300);
  Serial.println();
  Serial.println("RoomPulse beacon starting");

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

  BLEDevice::init(DEVICE_NAME);

  String payload = buildPayload();
  logHex(payload);

  BLEAdvertisementData advData;
  advData.addData(payload);

  advertising = BLEDevice::getAdvertising();
  advertising->setAdvertisementData(advData);
  advertising->setScanResponse(false);                               // no scan response
  // Advert type: keep the DEFAULT connectable ADV_IND (do NOT call
  // setAdvertisementType). It's the only type that radiates RELIABLY on this C6 +
  // esp32 core 3.3.10: 0x03 (NONCONN) never transmits at all; 0x02 (SCAN_IND)
  // transmits only intermittently (missed ~1 scan in 3). We expose no GATT
  // services, so there's nothing to connect to — connectable is secure in practice.
  uint16_t intervalUnits = (uint16_t)(ADV_INTERVAL_MS / 0.625);
  advertising->setMinInterval(intervalUnits);
  advertising->setMaxInterval(intervalUnits);
  advertising->start();

  // Battery: on this C6 + esp32 core 3.3.10, BOTH lowering the CPU clock
  // (setCpuFrequencyMhz) and automatic light sleep (esp_pm light_sleep_enable)
  // SILENTLY STOP the radio — the loop still prints "advertising" but nothing
  // goes on air. So savings here come from the long advertising interval (1 s),
  // the controller's own BLE modem sleep (automatic between adverts), and Wi-Fi
  // never being initialized. Lower power than this needs a different stack/chip.
  Serial.println("advertising");
}

void loop() {
  // The BLE controller advertises on its own. Just service serial provisioning
  // commands and emit an occasional heartbeat.
  static uint32_t lastBeat = 0;
  pumpSerial();
  if (millis() - lastBeat > 10000) { lastBeat = millis(); Serial.println("alive, advertising"); }
  delay(50);
}
