# Phone TX Tuner — Design

**Date:** 2026-07-04
**Status:** Approved
**Context:** nRF52840 validation beacon (`device/roompulse_beacon_nrf52/`) exposes a USB-serial protocol (`tx <dBm>` / `minor <n>` / `adv <ms>` / `show`, acks like `ok tx=-16 dBm`). The user wants to experiment with TX power on their own from their iPhone while walking the floor, with a progress bar confirming each change.

## Shape

One file: `device/tools/txtuner.py`. Python 3 **stdlib only** — serial I/O via `os.open` + `termios` (115200 8N1, no flow control), web via `http.server.ThreadingHTTPServer`. Run on the Mac that powers the beacon:

```bash
python3 device/tools/txtuner.py            # auto-detects /dev/cu.usbmodem*
python3 device/tools/txtuner.py --port /dev/cu.usbmodemXXXX --http 8880
```

On start it prints the LAN URLs to open on the phone (`http://<lan-ip>:8880` and `http://<hostname>.local:8880`). Phone and Mac must share Wi-Fi; macOS may prompt once to allow incoming connections for Python.

## Serial handling

- The script opens the port **once at startup and holds it** (DTR stays asserted → firmware acks always flow; no wedge, no per-request open/close glitches).
- All serial access goes through one function guarded by a `threading.Lock`: write command, read lines until the expected ack pattern or a 3 s timeout, return parsed result. Concurrent taps queue; commands never interleave.
- If the port is missing at startup: exit with a clear message listing candidates. If it dies mid-run (unplug): API returns `503 {"error": "beacon disconnected"}`; the page shows it.

## HTTP API (page is the only client)

- `GET /` → the embedded HTML page (single string in the script).
- `GET /api/state` → sends `show`, parses, returns `{"tx": 8, "minor": 101, "adv": 300, "uuid": "11111111-…"}`.
- `POST /api/tx` body `{"level": -16}` → validates against the 14-level set, sends `tx -16`, waits for `ok tx=-16 dBm`, re-sends `show` to confirm, returns the fresh state. Invalid level → `400`; no/garbled ack → `504 {"error": "no ack from beacon"}`.

## Page (embedded, mobile-first, dark)

- **State card** at top: current TX (large), minor, adv interval; refreshed on load and after every apply.
- **14 TX buttons** in a grid. Five are full-width/highlighted with labels: `+8 max range` · `0 tag default` · `−12 C6 floor` · `−16 room start` · `−20 room tight`. The rest (+7…+2, −4, −8, −40) are smaller, secondary. Active level visibly marked.
- **Progress bar** on tap: animates while the request is in flight and completes only when the server returns the verified state (`ok` ack + `show` re-read). Success banner "Applied −16 dBm"; failure banner with the error text. Buttons disabled while a change is in flight.
- No frameworks — inline CSS/JS, one HTTP file, works in iOS Safari.

## Out of scope

- Editing minor / adv from the page (displayed read-only)
- Auth/HTTPS (LAN-only test rig, run it only while experimenting)
- Persisting TX across beacon power cycles (firmware default stays +8; the tuner is for live experiments)
- Packaging, launchd, multi-beacon support
