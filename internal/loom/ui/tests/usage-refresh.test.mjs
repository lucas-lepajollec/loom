import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const source = fs.readFileSync(new URL('../next/js/core/poll.js', import.meta.url), 'utf8');
const env = {};
vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.refresh = visibleRefresh;', env);
const settle = () => new Promise(resolve => setImmediate(resolve));

test('Usage refresh starts automatically, serializes reads and pauses while hidden', async () => {
  const timers = new Map(), listeners = new Map(); let sequence = 0, reads = 0, resolve, alive;
  const surface = {
    document: { hidden: false, addEventListener: (name, fn) => listeners.set(name, fn), removeEventListener: name => listeners.delete(name) },
    setTimeout: (fn, delay) => { const id = ++sequence; timers.set(id, { fn, delay }); return id; },
    clearTimeout: id => timers.delete(id),
  };
  const stop = env.refresh(valid => { reads++; alive = valid; return new Promise(r => { resolve = r; }); }, 30000, surface);
  assert.equal(reads, 1);
  listeners.get('visibilitychange')();
  assert.equal(reads, 1);
  surface.document.hidden = true;
  resolve(); await settle();
  assert.equal(timers.size, 0);
  surface.document.hidden = false; listeners.get('visibilitychange')();
  assert.equal(reads, 2);
  resolve(); await settle();
  assert.equal(timers.size, 1);
  assert.equal([...timers.values()][0].delay, 30000);
  [...timers.values()][0].fn();
  assert.equal(reads, 3);
  stop(); resolve(); await settle();
  assert.equal(alive(), false);
  assert.equal(timers.size, 0);
  assert.equal(listeners.size, 0);
});

test('Usage transient errors retain periodic retries without overlapping requests', async () => {
  let timer, reads = 0;
  const surface = { document: { hidden: false, addEventListener() {}, removeEventListener() {} },
    setTimeout: fn => { timer = fn; return 1; }, clearTimeout() {} };
  const stop = env.refresh(async () => { reads++; throw new Error('synthetic unavailable'); }, 4000, surface);
  await settle(); assert.equal(reads, 1);
  await timer(); assert.equal(reads, 2);
  stop();
});

test('resume while an aborted observation settles starts a fresh read immediately', async () => {
  const listeners = new Map(); let reads = 0, finish;
  const surface = { document: { hidden: false, addEventListener: (n, f) => listeners.set(n, f), removeEventListener() {} },
    addEventListener: (n, f) => listeners.set(n, f), removeEventListener() {}, setTimeout: () => 1, clearTimeout() {} };
  const stop = env.refresh(() => { reads++; return new Promise(r => { finish = r; }); }, 30000, surface);
  surface.document.hidden = true; listeners.get('visibilitychange')();
  surface.document.hidden = false; listeners.get('pageshow')();
  finish(); await settle(); assert.equal(reads, 2);
  stop(); finish(); await settle();
});
