#!/usr/bin/env python3
"""Check local file targets in tracked Markdown (stdlib only; no network).

Fragments are ignored: this checks file existence, not generated heading IDs.
HTML src/href, inline links/images and reference definitions are included.
"""
import argparse
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
PENDING_SCREENSHOTS = {
    f"docs/screenshots/{name}.png"
    for name in ("chat", "local", "harnesses", "usage", "brain", "engines", "terminals", "welcome")
}


def targets(text):
    # Ignore fenced examples, which can contain illustrative links/HTML.
    text = re.sub(r"^\s*(`{3,}|~{3,})[^\n]*\n.*?^\s*\1\s*$", "", text,
                  flags=re.M | re.S)
    # Inline code is not a link, including example API paths.
    text = re.sub(r"(`+)[^`\n]*\1", "", text)
    for match in re.finditer(r"!?\[[^\]\n]*\]\(\s*(?:<([^>]+)>|([^\s)]+))", text):
        yield match.group(1) or match.group(2)
    for match in re.finditer(r"^\s*\[[^\]\n]+\]:\s*(?:<([^>]+)>|(\S+))", text, re.M):
        yield match.group(1) or match.group(2)
    for match in re.finditer(r"\b(?:src|href)\s*=\s*([\"'])(.*?)\1", text, re.I):
        yield match.group(2)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--allow-pending-screenshots", action="store_true",
                        help="Report the eight planned README screenshots separately until supplied")
    args = parser.parse_args()
    files = subprocess.check_output(["git", "ls-files", "-z", "--", "*.md"], cwd=ROOT)
    missing, pending = set(), set()
    checked = 0
    for name in files.decode().split("\0"):
        if not name:
            continue
        document = ROOT / name
        if not document.exists():
            continue
        for target in targets(document.read_text(encoding="utf-8")):
            url = urlsplit(target)
            if url.scheme or url.netloc or not url.path:
                continue
            path = unquote(url.path)
            resolved = (ROOT / path.lstrip("/") if path.startswith("/")
                        else document.parent / path).resolve()
            checked += 1
            if resolved.exists():
                continue
            try:
                relative = resolved.relative_to(ROOT).as_posix()
            except ValueError:
                relative = str(resolved)
            if args.allow_pending_screenshots and relative in PENDING_SCREENSHOTS:
                pending.add(relative)
            else:
                missing.add((name, target))
    for name, target in sorted(missing):
        print(f"MISSING {name}: {target}")
    for path in sorted(pending):
        print(f"PENDING {path}")
    print(f"Checked {checked} local targets: {len(missing)} missing, {len(pending)} pending screenshots.")
    return 1 if missing else 0


if __name__ == "__main__":
    raise SystemExit(main())
