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
static int8_t   g_txPower = 8;     // dBm — max, per on-site range test; retune to -16/-20 for room-sizing
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
  if (!Serial) return;  // no host attached — printing would wedge the CDC buffer
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
      if (Serial) Serial.println("tx must be one of: -40 -20 -16 -12 -8 -4 0 2 3 4 5 6 7 8");
      return;
    }
    g_txPower = (int8_t)v;
    Bluefruit.setTxPower(g_txPower);
    startAdvertising();
    if (Serial) Serial.printf("ok tx=%d dBm\n", g_txPower);
  } else if (sscanf(line, "minor %d", &v) == 1) {
    if (v < 1 || v > 65535) {
      if (Serial) Serial.println("minor must be 1..65535");
      return;
    }
    g_minor = (uint16_t)v;
    startAdvertising();
    if (Serial) Serial.printf("ok minor=%u\n", g_minor);
  } else if (sscanf(line, "major %d", &v) == 1) {
    if (v < 1 || v > 65535) {
      if (Serial) Serial.println("major must be 1..65535");
      return;
    }
    g_major = (uint16_t)v;
    startAdvertising();
    if (Serial) Serial.printf("ok major=%u\n", g_major);
  } else if (sscanf(line, "adv %d", &v) == 1) {
    if (v < 100 || v > 2000) {
      if (Serial) Serial.println("adv must be 100..2000 ms");
      return;
    }
    g_advMs = (uint16_t)v;
    startAdvertising();
    if (Serial) Serial.printf("ok adv=%u ms\n", g_advMs);
  } else if (line[0] != '\0') {
    if (Serial) Serial.println("commands: show | tx <dBm> | major <n> | minor <n> | adv <ms>");
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

  // Watchdog: SoftDevice advertising has died silently twice in the field
  // (board powered, firmware responsive, zero adverts on air; cause unknown).
  // A beacon that isn't advertising is a brick, so check every 5 s and
  // restart if it stopped without being asked to.
  static uint32_t lastAdvCheck = 0;
  if (millis() - lastAdvCheck >= 5000) {
    lastAdvCheck = millis();
    if (!Bluefruit.Advertising.isRunning()) {
      startAdvertising();
      if (Serial) Serial.println("watchdog: advertising restarted");
    }
  }

  delay(50);  // FreeRTOS vTaskDelay — lets the SoftDevice sleep between adverts
}
