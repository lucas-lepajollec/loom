import test from 'node:test';
import assert from 'node:assert/strict';
import { Config, fromMap } from '../next/js/features/inspector/config.js';

test('reads and writes KEY=value lines in the server format', () => {
  const c = new Config('MODEL=/m/q.gguf\nCTX=8192\n');
  assert.equal(c.get('CTX'), '8192');
  c.set('CTX', '32768').set('TEMP', '0.6');
  assert.match(c.text, /^CTX=32768$/m);
  assert.match(c.text, /^TEMP=0.6$/m);
  c.set('TEMP', '');
  assert.doesNotMatch(c.text, /TEMP=/, 'une valeur vide retire la ligne');
});

test('quotes values with spaces and keeps multi-line system prompts on one line', () => {
  const c = new Config('').set('SYSPROMPT', 'Réponds en français.\nSois bref.');
  assert.match(c.text, /^SYSPROMPT="Réponds en français.\\nSois bref."$/m);
  assert.equal(c.get('SYSPROMPT'), 'Réponds en français.\nSois bref.');
});

test('EXTRA_ARGS flags and valued flags round-trip without duplicates', () => {
  const c = new Config('');
  c.flag('--jinja', true).setArg('--spec-type', 'draft-mtp').flag('--jinja', true);
  assert.deepEqual(c.tokens(), ['--spec-type', 'draft-mtp', '--jinja'].sort((a, b) => c.tokens().indexOf(a) - c.tokens().indexOf(b)));
  assert.equal(c.arg('--spec-type'), 'draft-mtp');
  c.setArg('--spec-type', '');
  assert.equal(c.arg('--spec-type'), '');
  assert.ok(c.has('--jinja'));
});

test('catalog flags use their Loom key when one exists, EXTRA_ARGS otherwise', () => {
  const ctx = { id: 'ctx-size', flag: '--ctx-size', key: 'CTX', kind: 'int' };
  const fa = { id: 'flash-attn', flag: '--flash-attn', kind: 'enum' };
  const mm = { id: 'mmap', flag: '--mmap', kind: 'bool' };
  const c = new Config('');
  c.setFlagValue(ctx, '4096').setFlagValue(fa, 'on').setFlagValue(mm, 'on');
  assert.equal(c.get('CTX'), '4096');
  assert.equal(c.flagValue(fa), 'on');
  assert.equal(c.flagValue(mm), 'on');
  c.setFlagValue(mm, '');
  assert.equal(c.flagValue(mm), '');
});

test('KV cache pairs and machine keys are handled like the server expects', () => {
  const c = new Config('KV_TYPE=q8_0\n');
  assert.equal(c.kv(), 'q8_0|q8_0');
  c.setKv('q8_0|q5_1');
  assert.equal(c.get('KV_TYPE'), '');
  assert.equal(c.kv(), 'q8_0|q5_1');
  const m = fromMap({ MODEL: '/m.gguf', HOST: '127.0.0.1', CTX: '2048', MEM_MODE: 'off' });
  assert.equal(m.get('MODEL'), '/m.gguf');
  assert.equal(m.get('HOST'), '', 'les clés machine ne partent pas dans le panneau');
});
