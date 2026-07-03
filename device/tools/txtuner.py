#!/usr/bin/env python3
"""Phone TX tuner for the RoomPulse nRF52840 validation beacon.

Holds the beacon's USB-serial port open and serves a mobile page on the LAN
for changing TX power live. Python stdlib only.

Usage:
    python3 device/tools/txtuner.py [--port /dev/cu.usbmodemXXXX] [--http 8880]
"""

import re

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
