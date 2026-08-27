#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Draw the desktop icons from the same shape as the web app icon.

The web icon is `internal/webui/assets/icon.svg`: a rounded square in the
accent green with a white check mark. This script draws that shape with the
standard library only, so a build needs no image tool and no SVG renderer.
It writes the bundle icons and one menu bar icon.

Run it from any directory:

    python3 desktop/tools/make-icons.py

The output is deterministic. A second run writes the same bytes.
"""

import struct
import zlib
from pathlib import Path

OUT = Path(__file__).resolve().parents[1] / "src-tauri" / "icons"

GREEN = (0x2F, 0x6B, 0x4F)
CREAM = (0xF6, 0xF7, 0xF3)
SS = 4  # Supersample factor. Four gives a clean edge at 32 pixels.

# The three points of the check mark and the corner radius, in the 192 unit
# space of icon.svg.
BOX = 192.0
RADIUS = 42.0
CHECK = ((52.0, 100.0), (78.0, 126.0), (140.0, 64.0))
STROKE = 16.0


def rounded_box(x, y, size, radius):
    """Return True when the point is inside the rounded square."""
    cx = min(max(x, radius), size - radius)
    cy = min(max(y, radius), size - radius)
    dx = x - cx
    dy = y - cy
    return dx * dx + dy * dy <= radius * radius


def near_segment(x, y, a, b, half):
    """Return True when the point is within half a stroke of the segment."""
    ax, ay = a
    bx, by = b
    vx = bx - ax
    vy = by - ay
    wx = x - ax
    wy = y - ay
    length = vx * vx + vy * vy
    t = 0.0 if length == 0 else (wx * vx + wy * vy) / length
    t = min(max(t, 0.0), 1.0)
    dx = wx - t * vx
    dy = wy - t * vy
    return dx * dx + dy * dy <= half * half


def check_mask(x, y, scale):
    """Return True when the point is inside the check mark, caps included."""
    half = STROKE * scale / 2.0
    points = [(px * scale, py * scale) for px, py in CHECK]
    for a, b in zip(points, points[1:]):
        if near_segment(x, y, a, b, half):
            return True
    return False


def draw(size, background):
    """Return RGBA rows. A background of None leaves the square transparent."""
    scale = size / BOX
    radius = RADIUS * scale
    rows = []
    for py in range(size):
        row = bytearray()
        for px in range(size):
            hits_box = 0
            hits_check = 0
            for sy in range(SS):
                for sx in range(SS):
                    x = px + (sx + 0.5) / SS
                    y = py + (sy + 0.5) / SS
                    if background is not None and rounded_box(x, y, size, radius):
                        hits_box += 1
                    if check_mask(x, y, scale):
                        hits_check += 1
            total = SS * SS
            if background is None:
                # A menu bar icon is a mask. macOS reads the alpha channel and
                # paints the colour itself, so the check mark is black here.
                row += bytes((0, 0, 0, round(255 * hits_check / total)))
                continue
            box_a = hits_box / total
            check_a = hits_check / total
            colour = [
                round(background[i] * (1 - check_a) + CREAM[i] * check_a)
                for i in range(3)
            ]
            row += bytes((colour[0], colour[1], colour[2], round(255 * box_a)))
        rows.append(bytes(row))
    return rows


def png(rows, size):
    """Return the bytes of an 8-bit RGBA PNG."""
    raw = b"".join(b"\x00" + row for row in rows)

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))

    header = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def icns(entries):
    """Return an ICNS file. Each entry is a type and the bytes of a PNG."""
    body = b""
    for kind, data in entries:
        body += kind + struct.pack(">I", len(data) + 8) + data
    return b"icns" + struct.pack(">I", len(body) + 8) + body


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    sizes = {
        "32x32.png": 32,
        "128x128.png": 128,
        "128x128@2x.png": 256,
        "icon.png": 512,
    }
    made = {}
    for name, size in sizes.items():
        data = png(draw(size, GREEN), size)
        made[size] = data
        (OUT / name).write_bytes(data)
        print(f"{name}: {len(data)} bytes")

    # The macOS bundle wants one .icns file. Two sizes cover the Dock and the
    # Finder list, and the file stays small.
    bundle = icns([(b"ic07", made[128]), (b"ic09", made[512])])
    (OUT / "icon.icns").write_bytes(bundle)
    print(f"icon.icns: {len(bundle)} bytes")

    # 44 pixels is 22 points at 2x, which is the menu bar height on macOS.
    tray = png(draw(44, None), 44)
    (OUT / "tray.png").write_bytes(tray)
    print(f"tray.png: {len(tray)} bytes")


if __name__ == "__main__":
    main()
