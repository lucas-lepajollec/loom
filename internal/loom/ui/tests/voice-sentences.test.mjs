import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

// takeSentences decides when Jarvis can start speaking: complete sentences go
// to the voice as soon as they end, the rest waits for more text.
const src = fs.readFileSync(new URL('../next/js/features/voice/mode.js', import.meta.url), 'utf8');
const fn = src.slice(src.indexOf('export function takeSentences'), src.indexOf('\n}\n', src.indexOf('export function takeSentences')) + 2).replace('export ', '');
const takeSentences = vm.runInNewContext(fn + '\ntakeSentences');

test('complete sentences are released while streaming, the tail waits', () => {
  const r = takeSentences('Bonjour Lucas. Le moteur est prêt ! Je vér', false);
  assert.deepEqual([...r.sentences], ['Bonjour Lucas.', 'Le moteur est prêt !']);
  assert.equal(r.rest, 'Je vér');
});

test('a sentence ending exactly at the end waits until more text or the final flush', () => {
  assert.deepEqual([...takeSentences('Tout va bien.', false).sentences], []);
  assert.deepEqual([...takeSentences('Tout va bien.', true).sentences], ['Tout va bien.']);
});

test('very long text without punctuation is cut at a comma', () => {
  const long = 'a'.repeat(120) + ', ' + 'b'.repeat(120);
  const r = takeSentences(long, false);
  assert.equal(r.sentences.length, 1);
  assert.ok(r.sentences[0].endsWith(','));
});
