#!/usr/bin/env node
/**
 * Synchronizes the static Loom ui/next frontend assets from internal/loom/ui/
 * into demo/ so the public demo runs the real current Preact/HTM interface
 * statically on Vercel with zero Go server or build step required.
 */
import { cpSync, existsSync, mkdirSync, rmSync, copyFileSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const demoDir = resolve(__dirname, '..');
const repoRoot = resolve(demoDir, '..');
const uiRoot = join(repoRoot, 'internal', 'loom', 'ui');
const uiNextSrc = join(uiRoot, 'next');
const uiFontsSrc = join(uiRoot, 'fonts');
const markedSrc = join(uiRoot, 'marked.min.js');
const faviconSrc = join(repoRoot, 'docs', 'brand', 'loom-favicon.svg');

const checkOnly = process.argv.includes('--check');

function walkFiles(dir, ignoreNames = new Set()) {
  const out = [];
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (ignoreNames.has(entry.name)) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walkFiles(full, ignoreNames));
    else if (entry.isFile()) out.push(full);
  }
  return out.sort();
}

function compareTrees(srcDir, dstDir, ignoreRootFiles = new Set()) {
  const diffs = [];
  const srcFiles = walkFiles(srcDir);
  const dstFiles = walkFiles(dstDir);
  const srcRel = new Map();
  for (const f of srcFiles) {
    const rel = relative(srcDir, f);
    if (ignoreRootFiles.has(rel)) continue;
    srcRel.set(rel, f);
  }
  const dstRel = new Map(dstFiles.map(f => [relative(dstDir, f), f]));
  for (const [rel, srcFile] of srcRel) {
    const dstFile = dstRel.get(rel);
    if (!dstFile) diffs.push(`missing in demo: ${rel}`);
    else if (!readFileSync(srcFile).equals(readFileSync(dstFile))) diffs.push(`out of date: ${rel}`);
  }
  for (const rel of dstRel.keys()) {
    if (!srcRel.has(rel)) diffs.push(`extra in demo: ${rel}`);
  }
  return diffs;
}

if (checkOnly) {
  const diffs = [
    ...compareTrees(uiNextSrc, join(demoDir, 'next'), new Set(['index.html'])),
    ...compareTrees(uiFontsSrc, join(demoDir, 'fonts')),
  ];
  for (const [src, dst, label] of [
    [markedSrc, join(demoDir, 'marked.min.js'), 'marked.min.js'],
    [faviconSrc, join(demoDir, 'favicon.svg'), 'favicon.svg'],
  ]) {
    if (!existsSync(dst) || !readFileSync(src).equals(readFileSync(dst))) {
      diffs.push(`out of date: ${label}`);
    }
  }
  if (diffs.length) {
    console.error('Demo UI assets are out of sync with internal/loom/ui:');
    for (const d of diffs.slice(0, 20)) console.error(' -', d);
    if (diffs.length > 20) console.error(` ... and ${diffs.length - 20} more`);
    console.error('Run: npm --prefix demo run sync');
    process.exit(1);
  }
  console.log('Demo UI assets match internal/loom/ui.');
  process.exit(0);
}

const nextDst = join(demoDir, 'next');
const fontsDst = join(demoDir, 'fonts');

rmSync(nextDst, { recursive: true, force: true });
mkdirSync(nextDst, { recursive: true });
cpSync(uiNextSrc, nextDst, {
  recursive: true,
  filter: src => relative(uiNextSrc, src) !== 'index.html',
});

rmSync(fontsDst, { recursive: true, force: true });
mkdirSync(fontsDst, { recursive: true });
cpSync(uiFontsSrc, fontsDst, { recursive: true });

copyFileSync(markedSrc, join(demoDir, 'marked.min.js'));
copyFileSync(faviconSrc, join(demoDir, 'favicon.svg'));

const count = walkFiles(nextDst).length + walkFiles(fontsDst).length + 2;
console.log(`Synced ${count} static UI assets into demo/`);
