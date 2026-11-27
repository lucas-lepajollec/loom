import test from 'node:test';
import assert from 'node:assert/strict';
import {
  normalizeSessionPath, extractMentionedFiles, extractFilesFromText,
  extractOsc8Uris, findFileLinksInLine, isMarkdownPath, clampPanelWidth,
} from '../next/js/features/agents/panel-helpers.js';

test('normalizeSessionPath resolves file:// URLs, strips line anchors, and drops web links', () => {
  assert.equal(normalizeSessionPath('file:///home/lucas/Codes/projets/loom/docs/plan.md#L10-L24', '/home/lucas/Codes/projets/loom'), 'docs/plan.md');
  assert.equal(normalizeSessionPath('file://localhost/home/lucas/Codes/ecosystem-control/NOW.md', '/home/lucas/Codes'), 'ecosystem-control/NOW.md');
  assert.equal(normalizeSessionPath('./internal/loom/codeman.go:42', '/home/lucas/Codes/projets/loom'), 'internal/loom/codeman.go');
  assert.equal(normalizeSessionPath('https://github.com/lucas-lepajollec/loom/blob/main/README.md', '/home/lucas/Codes/projets/loom'), '');
  assert.equal(normalizeSessionPath('../secret.md', '/home/lucas/Codes/projets/loom'), '');
  assert.equal(normalizeSessionPath('not-a-file', '/home/lucas/Codes/projets/loom'), '');
});

test('extractMentionedFiles prioritizes markdown docs from recent turns and deduplicates', () => {
  const turns = [
    { answer: 'Updated `internal/loom/web_server.go` and wrote [notes](docs/architecture.md#L5).' },
    { answer: 'See [RFC](file:///repo/docs/rfc.md) and `internal/loom/web_server.go` plus plain ecosystem-control/NOW.md.' },
  ];
  const files = extractMentionedFiles(turns, '/repo');
  assert.deepEqual(files, ['docs/rfc.md', 'ecosystem-control/NOW.md', 'docs/architecture.md', 'internal/loom/web_server.go']);
  assert.equal(isMarkdownPath(files[0]), true);
  assert.equal(isMarkdownPath(files[3]), false);
});

test('extractOsc8Uris and extractFilesFromText detect terminal CLI links and plain paths', () => {
  const raw = '\x1b[1mDocs:\x1b[0m \x1b]8;;file:///home/lucas/Codes/ecosystem-control/NOW.md\x07NOW.md\x1b]8;;\x07 and projets/loom/CHANGELOG.md and https://example.com/foo/README.md';
  const uris = extractOsc8Uris(raw);
  assert.deepEqual(uris, ['file:///home/lucas/Codes/ecosystem-control/NOW.md']);
  const files = extractFilesFromText(raw, '/home/lucas/Codes', uris);
  assert.deepEqual(files, ['ecosystem-control/NOW.md', 'projets/loom/CHANGELOG.md']);
});

test('findFileLinksInLine finds clickable spans and resolves bare filenames via knownPaths', () => {
  const line = 'Open NOW.md or projets/loom/internal/loom/ui/next/js/features/agents/panel-state.js (skip https://github.com/a/b/README.md)';
  const links = findFileLinksInLine(line, '/home/lucas/Codes', ['ecosystem-control/NOW.md']);
  assert.equal(links.length, 2);
  assert.equal(links[0].raw, 'NOW.md');
  assert.equal(links[0].path, 'ecosystem-control/NOW.md');
  assert.equal(links[1].path, 'projets/loom/internal/loom/ui/next/js/features/agents/panel-state.js');
});

test('clampPanelWidth stays within safe viewport bounds', () => {
  assert.equal(clampPanelWidth(100, 1440), 280);
  assert.equal(clampPanelWidth(1200, 1440), 780);
  assert.equal(clampPanelWidth(700, 1000), 650);
});
