import test from 'node:test';
import assert from 'node:assert/strict';
import { singleFlight } from '../next/js/core/poll.js';
import { selectedEqual } from '../next/js/core/equal.js';

test('slow observations share a read across timer and manual refresh, retry after failure', async () => {
  let reads = 0, finish;
  const refresh = singleFlight(() => { reads++; return new Promise((resolve, reject) => { finish = { resolve, reject }; }); });
  const a = refresh(), b = refresh();
  assert.equal(a, b);
  await Promise.resolve(); assert.equal(reads, 1);
  finish.resolve('snapshot'); assert.equal(await a, 'snapshot');
  const c = refresh(); await Promise.resolve(); assert.equal(reads, 2);
  finish.reject(new Error('unreachable')); await assert.rejects(c, /unreachable/);
  const d = refresh(); await Promise.resolve(); assert.equal(reads, 3);
  finish.resolve('recovered'); assert.equal(await d, 'recovered');
});

test('object selectors ignore unrelated store patches without hiding changed state', () => {
  const items = [], session = { id: 'one' };
  assert.equal(selectedEqual({items, session}, {items, session}), true);
  assert.equal(selectedEqual({items, session}, {items: [], session}), false);
  assert.equal(selectedEqual({items, session}, {items}), false);
  assert.equal(selectedEqual({value: undefined}, {other: undefined}), false);
  assert.equal(selectedEqual(null, {}), false);
  assert.equal(selectedEqual([], []), false);
  assert.equal(selectedEqual(1, 1), true);
});
