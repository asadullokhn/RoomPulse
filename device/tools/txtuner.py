#!/usr/bin/env python3
"""Phone TX tuner for the RoomPulse nRF52840 validation beacon.

Holds the beacon's USB-serial port open and serves a mobile page on the LAN
for changing TX power live. Python stdlib only.

Usage:
    python3 device/tools/txtuner.py [--port /dev/cu.usbmodemXXXX] [--http 8880]
"""

import argparse
import fcntl
import glob
import json
import os
import re
import socket
import struct
import sys
import termios
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TX_LEVELS = [-40, -20, -16, -12, -8, -4, 0, 2, 3, 4, 5, 6, 7, 8]


def parse_show(text):
    """Parse the firmware's `show` output into a state dict, or None."""
    m_id = re.search(r"major (\d+)\s+minor (\d+)", text)
    m_tx = re.search(r"tx (-?\d+) dBm\s+adv (\d+) ms", text)
    m_uuid = re.search(r"uuid\s+([0-9a-fA-F-]{36})", text)
    if not (m_id and m_tx):
        return None
    return {
        "major": int(m_id.group(1)),
        "minor": int(m_id.group(2)),
        "tx": int(m_tx.group(1)),
        "adv": int(m_tx.group(2)),
        "uuid": m_uuid.group(1) if m_uuid else None,
    }


class BeaconError(Exception):
    def __init__(self, message, code=504):
        super().__init__(message)
        self.code = code


class Beacon:
    """Owns the serial port. One lock-guarded exchange at a time."""

    def __init__(self, port):
        self.port = port
        self.lock = threading.Lock()
        self.fd = os.open(port, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
        attrs = termios.tcgetattr(self.fd)
        attrs[0] = 0                                              # iflag: raw
        attrs[1] = 0                                              # oflag: raw
        attrs[2] = termios.CREAD | termios.CLOCAL | termios.CS8   # cflag: 8N1, no flow control
        attrs[3] = 0                                              # lflag: raw
        attrs[4] = termios.B115200
        attrs[5] = termios.B115200
        termios.tcsetattr(self.fd, termios.TCSANOW, attrs)
        # Assert DTR: the firmware only prints when a host is attached (if (Serial)),
        # and macOS does not raise DTR on open() the way pySerial does.
        tiocmbis = getattr(termios, "TIOCMBIS", 0x8004746C)
        dtr_rts = getattr(termios, "TIOCM_DTR", 0x002) | getattr(termios, "TIOCM_RTS", 0x004)
        fcntl.ioctl(self.fd, tiocmbis, struct.pack("I", dtr_rts))
        termios.tcflush(self.fd, termios.TCIOFLUSH)

    def exchange(self, command, expect, timeout=3.0):
        """Send `command`, read until output matches regex `expect`.

        Returns everything read. Raises BeaconError(503) on disconnect,
        BeaconError(504) on timeout."""
        with self.lock:
            try:
                termios.tcflush(self.fd, termios.TCIFLUSH)  # drop stale bytes from prior exchanges
                os.write(self.fd, (command + "\n").encode())
            except OSError as e:
                raise BeaconError(f"beacon disconnected: {e}", code=503) from e
            buf = ""
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                try:
                    chunk = os.read(self.fd, 4096).decode(errors="replace")
                except BlockingIOError:
                    time.sleep(0.02)
                    continue
                except OSError as e:
                    raise BeaconError(f"beacon disconnected: {e}", code=503) from e
                buf += chunk
                if re.search(expect, buf):
                    return buf
            raise BeaconError("no ack from beacon (timeout)", code=504)


def find_port():
    candidates = sorted(glob.glob("/dev/cu.usbmodem*"))
    if not candidates:
        sys.exit("No /dev/cu.usbmodem* port found - is the beacon plugged in?")
    if len(candidates) > 1:
        print(f"Multiple ports found {candidates}, using {candidates[0]}")
    return candidates[0]


PAGE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Beacon TX Tuner</title>
<style>
  * { box-sizing: border-box; margin: 0; -webkit-tap-highlight-color: transparent; }
  body { font-family: -apple-system, system-ui, sans-serif; background: #0b1220; color: #e8eef7;
         padding: max(16px, env(safe-area-inset-top)) 16px 32px; max-width: 480px; margin: 0 auto; }
  h1 { font-size: 17px; margin: 6px 0 14px; color: #7dd3c8; font-weight: 600; }
  .card { background: #141d30; border-radius: 14px; padding: 16px; margin-bottom: 14px; }
  .tx-now { font-size: 44px; font-weight: 700; line-height: 1.1; }
  .tx-now small { font-size: 18px; color: #8fa3bf; font-weight: 400; }
  .meta { color: #8fa3bf; font-size: 13px; margin-top: 6px; }
  .barwrap { height: 8px; background: #0b1220; border-radius: 4px; overflow: hidden; margin-top: 14px; }
  #bar { height: 100%; width: 0%; background: #35b8a5; border-radius: 4px; }
  #banner { font-size: 14px; min-height: 20px; margin-top: 10px; }
  #banner.ok { color: #6ee7b7; } #banner.err { color: #fda4af; }
  .primary { display: grid; gap: 10px; margin-bottom: 12px; }
  .secondary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
  button { border: 1px solid #26314a; background: #182238; color: #e8eef7; border-radius: 12px;
           padding: 14px 12px; font-size: 16px; font-weight: 600; }
  button .lbl { display: block; font-size: 12px; font-weight: 400; color: #8fa3bf; margin-top: 2px; }
  button.active { border-color: #35b8a5; background: #143029; }
  button:disabled { opacity: .45; }
  .secondary button { padding: 12px 4px; font-size: 15px; }
</style>
</head>
<body>
<h1>RoomPulse beacon &mdash; TX tuner</h1>
<div class="card">
  <div class="tx-now"><span id="tx">&ndash;</span> <small>dBm</small></div>
  <div class="meta" id="meta">connecting&hellip;</div>
  <div class="barwrap"><div id="bar"></div></div>
  <div id="banner"></div>
</div>
<div class="primary" id="primary"></div>
<div class="secondary" id="secondary"></div>
<script>
const LABELS = {"8": "max range", "0": "tag default", "-12": "C6 floor",
                "-16": "room start", "-20": "room tight"};
const PRIMARY = [8, 0, -12, -16, -20];
const SECONDARY = [7, 6, 5, 4, 3, 2, -4, -8, -40];
let busy = false;
const $ = (id) => document.getElementById(id);
const fmt = (n) => (n > 0 ? "+" + n : String(n));

function buttons() {
  for (const [ids, levels] of [["primary", PRIMARY], ["secondary", SECONDARY]]) {
    $(ids).innerHTML = levels.map((l) =>
      `<button data-level="${l}" onclick="setTx(${l})">${fmt(l)} dBm` +
      (LABELS[l] ? `<span class="lbl">${LABELS[l]}</span>` : "") + `</button>`).join("");
  }
}

function render(s) {
  $("tx").textContent = fmt(s.tx);
  $("meta").textContent = `minor ${s.minor} · adv ${s.adv} ms · major ${s.major}`;
  document.querySelectorAll("button").forEach((b) =>
    b.classList.toggle("active", Number(b.dataset.level) === s.tx));
}

function banner(text, ok) {
  const el = $("banner");
  el.textContent = text;
  el.className = text ? (ok ? "ok" : "err") : "";
}

function setDisabled(v) {
  document.querySelectorAll("button").forEach((b) => (b.disabled = v));
}

async function refresh() {
  try {
    const r = await fetch("/api/state");
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || "HTTP " + r.status);
    render(data);
  } catch (e) { banner(e.message, false); }
}

async function setTx(level) {
  if (busy) return;
  busy = true; setDisabled(true); banner("");
  const bar = $("bar");
  bar.style.transition = "none"; bar.style.width = "0%";
  void bar.offsetWidth;
  bar.style.transition = "width 1.4s cubic-bezier(.2,.7,.3,1)";
  bar.style.width = "88%";
  try {
    const r = await fetch("/api/tx", { method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ level }) });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || "HTTP " + r.status);
    bar.style.transition = "width .2s"; bar.style.width = "100%";
    render(data);
    banner("Applied " + fmt(level) + " dBm", true);
  } catch (e) {
    bar.style.transition = "width .2s"; bar.style.width = "0%";
    banner(e.message, false);
  } finally {
    setTimeout(() => { bar.style.transition = "width .3s"; bar.style.width = "0%";
                       busy = false; setDisabled(false); }, 700);
  }
}

buttons();
refresh();
</script>
</body>
</html>
"""


class Handler(BaseHTTPRequestHandler):
    beacon = None  # assigned in main()

    def _json(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_state(self):
        out = Handler.beacon.exchange("show", r"measured-power")
        state = parse_show(out)
        if state is None:
            raise BeaconError("could not parse beacon state", code=504)
        return state

    def do_GET(self):
        if self.path == "/":
            body = PAGE.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif self.path == "/api/state":
            try:
                self._json(200, self._read_state())
            except BeaconError as e:
                self._json(e.code, {"error": str(e)})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/api/tx":
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", 0))
            level = json.loads(self.rfile.read(length))["level"]
        except (ValueError, KeyError, json.JSONDecodeError):
            self._json(400, {"error": 'body must be {"level": <dBm>}'})
            return
        if level not in TX_LEVELS:
            self._json(400, {"error": f"level must be one of {TX_LEVELS}"})
            return
        try:
            Handler.beacon.exchange(f"tx {level}", rf"ok tx={level} dBm")
            self._json(200, self._read_state())
        except BeaconError as e:
            self._json(e.code, {"error": str(e)})

    def log_message(self, *args):
        pass  # keep the terminal clean


def lan_ip():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("192.0.2.1", 80))  # UDP connect sends no packets
        return s.getsockname()[0]
    except OSError:
        return "127.0.0.1"
    finally:
        s.close()


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--port", help="serial port (default: auto-detect /dev/cu.usbmodem*)")
    ap.add_argument("--http", type=int, default=8880, help="HTTP port (default 8880)")
    args = ap.parse_args()

    port = args.port or find_port()
    Handler.beacon = Beacon(port)
    try:
        state = parse_show(Handler.beacon.exchange("show", r"measured-power"))
    except BeaconError as e:
        sys.exit(f"Beacon on {port} not responding: {e}")
    print(f"Beacon on {port}: tx {state['tx']} dBm, minor {state['minor']}, adv {state['adv']} ms")

    host = socket.gethostname()
    if not host.endswith(".local"):
        host += ".local"
    print("Open on your phone (same Wi-Fi):")
    print(f"  http://{lan_ip()}:{args.http}")
    print(f"  http://{host}:{args.http}")
    ThreadingHTTPServer(("0.0.0.0", args.http), Handler).serve_forever()


if __name__ == "__main__":
    main()
