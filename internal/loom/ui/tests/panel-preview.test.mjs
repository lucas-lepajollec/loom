import test from 'node:test';
import assert from 'node:assert/strict';
import { normalizeSessionPath, extractMentionedFiles, isMarkdownPath, clampPanelWidth } from '../next/js/features/agents/panel-helpers.js';

test('normalizeSessionPath resolves file:// URLs, strips line anchors, and drops web links', () => {
  assert.equal(normalizeSessionPath('file:///home/lucas/Codes/projets/loom/docs/plan.md#L10-L24', '/home/lucas/Codes/projets/loom'), 'docs/plan.md');
  assert.equal(normalizeSessionPath('./internal/loom/codeman.go:42', '/home/lucas/Codes/projets/loom'), 'internal/loom/codeman.go');
  assert.equal(normalizeSessionPath('https://github.com/lucas-lepajollec/loom/blob/main/README.md', '/home/lucas/Codes/projets/loom'), '');
  assert.equal(normalizeSessionPath('../secret.md', '/home/lucas/Codes/projets/loom'), '');
  assert.equal(normalizeSessionPath('not-a-file', '/home/lucas/Codes/projets/loom'), '');
});

test('extractMentionedFiles prioritizes markdown docs from recent turns and deduplicates', () => {
  const turns = [
    { answer: 'Updated `internal/loom/web_server.go` and wrote [notes](docs/architecture.md#L5).' },
    { answer: 'See [RFC](file:///repo/docs/rfc.md) and `internal/loom/web_server.go` plus `README.md`.' },
  ];
  const files = extractMentionedFiles(turns, '/repo');
  assert.deepEqual(files, ['docs/rfc.md', 'README.md', 'docs/architecture.md', 'internal/loom/web_server.go']);
  assert.equal(isMarkdownPath(files[0]), true);
  assert.equal(isMarkdownPath(files[3]), false);
});

test('clampPanelWidth stays within safe viewport bounds', () => {
  assert.equal(clampPanelWidth(100, 1440), 280);
  assert.equal(clampPanelWidth(1200, 1440), 780);
  assert.equal(clampPanelWidth(700, 1000), 650);
});
