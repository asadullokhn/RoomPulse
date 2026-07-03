#!/usr/bin/env python3
"""Phone TX tuner for the RoomPulse nRF52840 validation beacon.

Holds the beacon's USB-serial port open and serves a mobile page on the LAN
for changing TX power live. Python stdlib only.

Usage:
    python3 device/tools/txtuner.py [--port /dev/cu.usbmodemXXXX] [--http 8880]
"""

import fcntl
import glob
import os
import re
import struct
import sys
import termios
import threading
import time

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
