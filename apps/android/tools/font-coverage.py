#!/usr/bin/env python3
"""Report which bundled fallback fonts cover the non-ASCII codepoints used by subtitle
files. Answers "do we need font X?" with data instead of guesses.

Usage: font-coverage.py --fonts <dir> <subtitle files or dirs...>
"""
import argparse
import os
import re
import sys
import unicodedata
from collections import defaultdict

from fontTools.ttLib import TTFont

ASS_TAG = re.compile(r"\{[^}]*\}")


def subtitle_text(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        s = f.read()
    out = []
    for line in s.splitlines():
        if line.startswith("Dialogue:"):
            parts = line.split(",", 9)
            if len(parts) == 10:
                out.append(ASS_TAG.sub("", parts[9]))
        elif not line.startswith(("Style:", "Format:", "[", ";")) and "-->" not in line:
            out.append(line)
    return "".join(out)


def font_cmap(path):
    try:
        f = TTFont(path, lazy=True, fontNumber=0)
        cmap = f.getBestCmap() or {}
        name = f["name"].getDebugName(1) or os.path.basename(path)
        return name, set(cmap.keys())
    except Exception as e:  # noqa: BLE001
        return os.path.basename(path), set()


def main():
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")  # Windows consoles default to cp1252
    ap = argparse.ArgumentParser()
    ap.add_argument("--fonts", required=True)
    ap.add_argument("inputs", nargs="+")
    args = ap.parse_args()

    cps = set()
    for inp in args.inputs:
        paths = [inp]
        if os.path.isdir(inp):
            paths = [os.path.join(d, n) for d, _, ns in os.walk(inp) for n in ns if n.lower().endswith((".ass", ".ssa", ".srt", ".vtt"))]
        for p in paths:
            cps |= {ord(c) for c in subtitle_text(p) if ord(c) > 0x7F and not unicodedata.category(c).startswith("C")}
    cps = sorted(cps)
    print(f"{len(cps)} distinct non-ASCII codepoints in input\n")

    fonts = []
    for n in sorted(os.listdir(args.fonts)):
        if n.lower().endswith((".ttf", ".otf", ".ttc")):
            fonts.append(font_cmap(os.path.join(args.fonts, n)))

    # Per-font coverage of the input set.
    print(f"{'font':36} {'covers':>6}  missing (first 12)")
    for name, cm in sorted(fonts, key=lambda t: -len(set(cps) & t[1])):
        missing = [c for c in cps if c not in cm]
        print(f"{name:36} {len(cps) - len(missing):>6}  " + " ".join(f"U+{c:04X}" for c in missing[:12]))

    # Codepoints nobody covers, and codepoints only one font covers (= that font is load-bearing).
    print()
    uncovered = [c for c in cps if not any(c in cm for _, cm in fonts)]
    print("uncovered by every font:", " ".join(f"U+{c:04X}({chr(c)})" for c in uncovered) or "none")
    only = defaultdict(list)
    for c in cps:
        holders = [n for n, cm in fonts if c in cm]
        if len(holders) == 1:
            only[holders[0]].append(c)
    print("\nsole provider for:")
    for n, cs in sorted(only.items(), key=lambda t: -len(t[1])):
        print(f"  {n:36} {len(cs):>3}  " + " ".join(f"U+{c:04X}({chr(c)})" for c in cs[:10]))


if __name__ == "__main__":
    main()
