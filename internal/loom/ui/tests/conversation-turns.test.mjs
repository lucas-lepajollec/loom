import test from 'node:test';
import assert from 'node:assert/strict';
import { buildTurns, changedFiles, duration, screenChoices } from '../next/js/features/conversation/turns.js';

const at = s => new Date(Date.UTC(2026, 10, 26, 10, 0, s)).toISOString();

test('a finished turn folds its work above the final answer', () => {
  const turns = buildTurns([
    { kind: 'response', role: 'assistant', text: 'banner before any prompt', timestamp: at(0) },
    { kind: 'prompt', role: 'user', text: 'Add pagination', timestamp: at(1) },
    { kind: 'response', role: 'assistant', text: 'Reading the list code.', timestamp: at(5) },
    { kind: 'tool', role: 'assistant', text: 'go test ./...\nok', timestamp: at(20) },
    { kind: 'response', role: 'assistant', text: 'Done: next_cursor added.', timestamp: at(135) },
  ], [{ id: 'e1', ts: Date.parse(at(30)), relPath: 'a/b.go', added: 4, removed: 1 }]);
  assert.equal(turns.length, 1);
  const [turn] = turns;
  assert.equal(turn.prompt, 'Add pagination');
  assert.equal(turn.answer, 'Done: next_cursor added.');
  assert.deepEqual(turn.work.map(x => [x.k, x.text]), [['say', 'Reading the list code.'], ['step', 'go test ./...']]);
  assert.equal(turn.ms, 134000);
  assert.equal(turn.edits.length, 1);
});

test('the live turn shows everything and has no final answer yet', () => {
  const turns = buildTurns([
    { kind: 'prompt', role: 'user', text: 'one', timestamp: at(1) },
    { kind: 'response', role: 'assistant', text: 'first answer', timestamp: at(2) },
    { kind: 'prompt', role: 'user', text: 'two', timestamp: at(10) },
    { kind: 'response', role: 'assistant', text: 'on it', timestamp: at(12) },
  ], [], true);
  assert.equal(turns[0].answer, 'first answer');
  assert.equal(turns[1].live, true);
  assert.equal(turns[1].answer, '');
  assert.equal(turns[1].work.length, 1);
});

test('edits land in the turn where they happened and sum per file', () => {
  const turns = buildTurns([
    { kind: 'prompt', role: 'user', text: 'one', timestamp: at(1) },
    { kind: 'prompt', role: 'user', text: 'two', timestamp: at(50) },
  ], [
    { id: 'a', ts: Date.parse(at(10)), relPath: 'x.go', added: 3, removed: 1 },
    { id: 'b', ts: Date.parse(at(20)), relPath: 'x.go', added: 2, removed: 0 },
    { id: 'c', ts: Date.parse(at(60)), relPath: 'y.go', added: 1, removed: 1 },
  ]);
  assert.deepEqual(turns.map(t => t.edits.map(e => e.id)), [['a', 'b'], ['c']]);
  assert.deepEqual(changedFiles(turns[0].edits), { files: [{ path: 'x.go', added: 5, removed: 1, created: false, ids: ['a', 'b'] }], added: 5, removed: 1 });
});

test('durations read naturally', () => {
  assert.equal(duration(48000), '48 s');
  assert.equal(duration(134000), '2 min 14 s');
  assert.equal(duration(3900000), '1 h 5 min');
});

test('approval choices are read off a Codex screen with their keys', () => {
  const screen = `Allow command?\n  curl -sI https://nas.example.org\n› 1. Yes, proceed (y)\n  2. Yes, and don't ask again for this command (a)\n  3. No, and tell Codex what to do differently (esc)\n`;
  const c = screenChoices(screen);
  assert.deepEqual(c.map(x => [x.n, x.hint, x.deny]), [[1, 'Y', false], [2, 'A', false], [3, 'Esc', true]]);
  assert.equal(c[0].key, 'y');
  assert.equal(c[2].key, '\u001b');
  assert.equal(c[1].label, "Yes, and don't ask again for this command");
  assert.deepEqual(screenChoices('no dialog here'), []);
});
