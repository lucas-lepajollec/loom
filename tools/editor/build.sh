#!/bin/sh
# Rebuilds the vendored note editor. Run from the repository root:
#   sh tools/editor/build.sh
set -e
cd "$(dirname "$0")"
npm install --no-audit --no-fund --ignore-scripts
./node_modules/@esbuild/linux-x64/bin/esbuild entry.js --bundle --format=esm --minify \
  --target=es2020 --legal-comments=none --outfile=../../internal/loom/ui/next/vendor/editor/editor.mjs
