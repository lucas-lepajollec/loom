#!/usr/bin/env python3
"""Explicit download only. Never imported/run by the evaluator or CI."""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--cache', type=Path, required=True)
    args = parser.parse_args()
    # Prevent accidentally placing licensed histories among tracked fixtures.
    if '.project-local' not in args.cache.resolve().parts:
        parser.error('cache must be under gitignored .project-local/')
    manifest = json.loads(args.manifest.read_text())
    source = manifest['upstream']
    args.cache.mkdir(parents=True, exist_ok=True)
    target = args.cache / source['file']
    if not target.exists():
        url = (f"https://huggingface.co/datasets/{source['repository']}/resolve/"
               f"{source['revision']}/{source['file']}")
        partial = target.with_suffix('.part')
        with urllib.request.urlopen(url, timeout=60) as response, partial.open('wb') as out:
            total = 0
            while block := response.read(1024 * 1024):
                total += len(block)
                if total > source['bytes']:
                    raise ValueError('download exceeds pinned source size')
                out.write(block)
        partial.replace(target)
    with target.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    if digest != source['sha256'] or target.stat().st_size != source['bytes']:
        raise ValueError('pinned source digest/size mismatch; remove invalid cache file')
    # The licence declaration and immutable source provenance travel with cache.
    (args.cache / 'licence-manifest.json').write_text(json.dumps(source, indent=2) + '\n')
    print(f"Verified {target.name}: {digest}; {source['licence']}")


if __name__ == '__main__':
    main()
