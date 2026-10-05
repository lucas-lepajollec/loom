#!/usr/bin/env python3
"""Fail closed on stale compiled Windows properties; no Windows host required."""
import argparse
import json
from pathlib import Path
import re
import struct

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--windows-exe", nargs="*", default=[])
args = parser.parse_args()
version = re.search(r'const Version = "([0-9]+\.[0-9]+\.[0-9]+)"', (root / "internal/loom/run.go").read_text()).group(1)
parts = tuple(map(int, version.split("."))) + (0,)
info = json.loads((root / "cmd/loom/versioninfo.json").read_text())
for label in ("FileVersion", "ProductVersion"):
    actual = tuple(info["FixedFileInfo"][label][key] for key in ("Major", "Minor", "Patch", "Build"))
    if actual != parts:
        raise SystemExit(f"versioninfo.json {label} differs from source {version}")
if info["StringFileInfo"]["ProductVersion"] != version:
    raise SystemExit("Windows product version string differs from source")
expected = (parts[0] << 16 | parts[1], parts[2] << 16 | parts[3]) * 2
signature = struct.pack("<II", 0xFEEF04BD, 0x10000)
def check(path, machine, executable=False):
    data = path.read_bytes()
    offset = struct.unpack_from("<I", data, 0x3C)[0] + 4 if executable else 0
    if executable and (data[:2] != b"MZ" or data[offset-4:offset] != b"PE\0\0"):
        raise SystemExit(f"{path.name}: not a PE executable")
    if struct.unpack_from("<H", data, offset)[0] != machine:
        raise SystemExit(f"{path.name}: wrong architecture")
    if data.count(signature) != 1:
        raise SystemExit(f"{path.name}: missing/ambiguous fixed version resource")
    fixed = struct.unpack_from("<13I", data, data.index(signature))
    if fixed[2:6] != expected or version.encode("utf-16le") not in data:
        raise SystemExit(f"{path.name}: stale Windows properties; run go generate ./cmd/loom")
    print(f"PASS {path.name}: Windows properties match {version}")
for arch, machine in (("amd64", 0x8664), ("arm64", 0xAA64)):
    check(root / f"cmd/loom/resource_windows_{arch}.syso", machine)
for name in args.windows_exe:
    path = Path(name)
    machine = {"loom-windows.exe": 0x8664, "loom-windows-arm.exe": 0xAA64}.get(path.name)
    if machine is None:
        raise SystemExit("unknown release asset name")
    check(path, machine, True)
