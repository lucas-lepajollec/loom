import test from 'node:test';
import assert from 'node:assert/strict';
import { visibleParentOf, childrenOf, descendantsOf, rootIdOf } from '../next/js/features/agents/orchestration-tree.js';

const NODES = {
  root: { session_id: 'root', root_id: 'root', depth: 0, created_at: 10 },
  w1: { session_id: 'w1', parent_id: 'root', root_id: 'root', depth: 1, role: 'backend', created_at: 20 },
  w2: { session_id: 'w2', parent_id: 'root', root_id: 'root', depth: 1, role: 'frontend', created_at: 30 },
  sub1: { session_id: 'sub1', parent_id: 'w1', root_id: 'root', depth: 2, role: 'tests', created_at: 40 },
};

test('visibleParentOf nests children under live parent and bubbles up when parent closed', () => {
  const allLive = new Set(['root', 'w1', 'w2', 'sub1']);
  assert.equal(visibleParentOf('root', allLive, NODES), '');
  assert.equal(visibleParentOf('w1', allLive, NODES), 'root');
  assert.equal(visibleParentOf('sub1', allLive, NODES), 'w1');

  // When w1 closes, sub1 gracefully attaches to live root instead of vanishing.
  const withoutW1 = new Set(['root', 'w2', 'sub1']);
  assert.equal(visibleParentOf('sub1', withoutW1, NODES), 'root');

  // When all ancestors are closed, surviving child rises to top level.
  const onlySub = new Set(['sub1']);
  assert.equal(visibleParentOf('sub1', onlySub, NODES), '');
});

test('childrenOf and descendantsOf return deterministic tree order', () => {
  assert.deepEqual(childrenOf('root', NODES).map(n => n.session_id), ['w1', 'w2']);
  assert.deepEqual(descendantsOf('root', NODES).map(n => n.session_id), ['w1', 'w2', 'sub1']);
  assert.equal(rootIdOf('sub1', NODES), 'root');
});
