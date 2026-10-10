import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
test('background settings, machines, MCP, model detail and context preview reads contain network rejections', () => {
  const r = spawnSync(process.execPath, [new URL('./page-failure-fixture.mjs', import.meta.url).pathname], { encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
});
