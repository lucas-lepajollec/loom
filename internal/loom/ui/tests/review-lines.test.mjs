import test from 'node:test';
import assert from 'node:assert/strict';
import { reviewLines } from '../next/js/features/agents/review-lines.js';

test('review references follow each side across hunks and skip file headers', () => {
  const rows = reviewLines('--- a/main\n+++ b/main\n@@ -4,2 +9,2 @@\n-before\n+after\n same\n\\ No newline at end of file\n@@ -40,0 +46 @@\n+last');
  assert.deepEqual(rows.map(row => row.number), [undefined, undefined, undefined, 4, 9, 10, undefined, undefined, 46]);
  assert.deepEqual(rows.filter(row => row.number != null).map(row => row.kind), ['del', 'add', '', 'add']);
});
