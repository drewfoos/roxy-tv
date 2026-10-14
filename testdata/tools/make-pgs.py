#!/usr/bin/env python3
"""Write a Blu-ray PGS (.sup) subtitle stream from a list of text cues.

ffmpeg has no PGS encoder, so this renders each cue to a bitmap with ffmpeg's drawtext,
quantises it to a 4-colour palette, RLE-encodes it, and emits the PGS segment sequence
(PCS, WDS, PDS, ODS, END per display set, plus a clearing set at each cue's end).
The output muxes into MKV with `ffmpeg -i subs.sup -c:s copy` as hdmv_pgs_subtitle.

Usage: make-pgs.py --ffmpeg <ffmpeg.exe> --font <font.ttf> --out subs.sup [--width 1920 --height 1080]
Cues are built in; this is test tooling, not a general converter.
"""
import argparse
import struct
import subprocess
import sys

# (start_ms, end_ms, text). Each line says what correct rendering looks like.
CUES = [
    (500, 5000, "PGS bitmap subtitle: white text, blue outline, bottom centre"),
    (5000, 10000, "If you can read this, PGS decode + palette + alpha work"),
    (10000, 16000, "Second cue replaced the first with no overlap or ghosting"),
    (16000, 24000, "Long cue: stays on screen for eight seconds, then clears"),
    (26000, 34000, "Gap test: nothing was shown between 24 s and 26 s"),
    (34000, 44000, "Last PGS cue. Seeking back should re-show earlier cues."),
]

BOX_W, BOX_H = 1600, 160  # bitmap canvas for one cue (within the video frame)


def render_cue(ffmpeg: str, font: str, text: str) -> bytes:
    """Return BOX_W*BOX_H*3 bytes of RGB from ffmpeg's drawtext on black."""
    font_ff = font.replace("\\", "/").replace(":", "\\:")
    safe = text.replace("\\", "\\\\").replace(":", "\\:").replace("'", "’").replace(",", "\\,")
    vf = (
        f"drawtext=fontfile='{font_ff}':text='{safe}':fontsize=56:fontcolor=white:"
        f"borderw=4:bordercolor=0x00A0FF:x=(w-text_w)/2:y=(h-text_h)/2"
    )
    cmd = [
        ffmpeg, "-v", "error", "-f", "lavfi", "-i", f"color=black:s={BOX_W}x{BOX_H}:d=1",
        "-vf", vf, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-",
    ]
    out = subprocess.run(cmd, capture_output=True, check=True).stdout
    if len(out) != BOX_W * BOX_H * 3:
        raise RuntimeError(f"unexpected frame size {len(out)}")
    return out


def quantise(rgb: bytes):
    """Map RGB to palette indices: 0 transparent, 1 white, 2 blue outline, 3 soft edge."""
    px = bytearray(BOX_W * BOX_H)
    for i in range(BOX_W * BOX_H):
        r, g, b = rgb[3 * i], rgb[3 * i + 1], rgb[3 * i + 2]
        m = max(r, g, b)
        if m < 40:
            px[i] = 0
        elif r > 200 and g > 200 and b > 200:
            px[i] = 1
        elif b > 120 and r < 120:
            px[i] = 2
        else:
            px[i] = 3
    return px


def rle_encode(px, w: int, h: int) -> bytes:
    out = bytearray()
    for y in range(h):
        row = px[y * w:(y + 1) * w]
        x = 0
        while x < w:
            c = row[x]
            n = 1
            while x + n < w and row[x + n] == c and n < 16383:
                n += 1
            if c == 0:
                if n < 64:
                    out += bytes([0, n])
                else:
                    out += bytes([0, 0x40 | (n >> 8), n & 0xFF])
            elif n == 1:
                out.append(c)
            elif n < 64:
                out += bytes([0, 0x80 | n, c])
            else:
                out += bytes([0, 0xC0 | (n >> 8), n & 0xFF, c])
            x += n
        out += b"\x00\x00"  # end of line
    return bytes(out)


def ycc(r, g, b, a):
    """BT.709 limited-range YCbCr for a PDS entry."""
    y = 0.2126 * r + 0.7152 * g + 0.0722 * b
    cb = 128 + 0.5 * (b - y) / (1 - 0.0722) * (224 / 255)
    cr = 128 + 0.5 * (r - y) / (1 - 0.2126) * (224 / 255)
    y = 16 + y * 219 / 255
    return bytes([int(round(y)), int(round(cr)), int(round(cb)), a])


PALETTE = {
    0: ycc(0, 0, 0, 0),
    1: ycc(255, 255, 255, 255),
    2: ycc(0, 160, 255, 255),
    3: ycc(200, 200, 200, 150),
}


def segment(pts_ms: int, seg_type: int, payload: bytes) -> bytes:
    pts = int(pts_ms * 90)
    return b"PG" + struct.pack(">IIBH", pts, 0, seg_type, len(payload)) + payload


def pcs(width, height, comp_num, state, objects):
    body = struct.pack(">HHBHBBBB", width, height, 0x10, comp_num, state, 0, 0, len(objects))
    for obj_id, win_id, x, y in objects:
        body += struct.pack(">HBBHH", obj_id, win_id, 0, x, y)
    return body


def wds(win_id, x, y, w, h):
    return struct.pack(">BBHHHH", 1, win_id, x, y, w, h)


def pds(pal_id):
    body = struct.pack(">BB", pal_id, 0)
    for idx, entry in PALETTE.items():
        body += bytes([idx]) + entry
    return body


def ods(obj_id, w, h, rle: bytes):
    # single fragment: sequence flag 0xC0 (first + last); data length counts w/h header too
    return struct.pack(">HBB", obj_id, 0, 0xC0) + struct.pack(">I", len(rle) + 4)[1:] + struct.pack(">HH", w, h) + rle


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ffmpeg", required=True)
    ap.add_argument("--font", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--width", type=int, default=1920)
    ap.add_argument("--height", type=int, default=1080)
    args = ap.parse_args()

    x = (args.width - BOX_W) // 2
    y = args.height - BOX_H - 60
    stream = bytearray()
    comp = 0
    for i, (start, end, text) in enumerate(CUES):
        px = quantise(render_cue(args.ffmpeg, args.font, text))
        rle = rle_encode(px, BOX_W, BOX_H)
        if len(rle) + 4 > 0xFFFF:
            sys.exit(f"cue {i}: RLE too large for one ODS fragment ({len(rle)} bytes)")
        obj_id, win_id, pal_id = 0, 0, 0
        # display set: epoch start with one object
        stream += segment(start, 0x16, pcs(args.width, args.height, comp, 0x80, [(obj_id, win_id, x, y)]))
        stream += segment(start, 0x17, wds(win_id, x, y, BOX_W, BOX_H))
        stream += segment(start, 0x14, pds(pal_id))
        stream += segment(start, 0x15, ods(obj_id, BOX_W, BOX_H, rle))
        stream += segment(start, 0x80, b"")
        comp += 1
        # clearing set: no objects
        stream += segment(end, 0x16, pcs(args.width, args.height, comp, 0x00, []))
        stream += segment(end, 0x17, wds(win_id, x, y, BOX_W, BOX_H))
        stream += segment(end, 0x80, b"")
        comp += 1
        print(f"cue {i}: {start}-{end} ms, rle {len(rle)} bytes")

    with open(args.out, "wb") as f:
        f.write(stream)
    print(f"wrote {args.out} ({len(stream)} bytes, {len(CUES)} cues)")


if __name__ == "__main__":
    main()
