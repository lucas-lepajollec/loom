import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { lifecycleVersion, lifecycleCurrent, lifecycleResult } from '../next/js/features/harnesses/lifecycle-state.js';
import en from '../next/js/i18n/en.js';
import fr from '../next/js/i18n/fr.js';

test('unknown installed/latest versions never claim currency', () => {
  for (const version of ['', '?', 'unknown']) {
    assert.equal(lifecycleVersion(version), '');
    assert.equal(lifecycleCurrent({ installed: true, version, latest: '2.0.0', update_available: false }), false);
    assert.equal(lifecycleCurrent({ installed: true, version: '2.0.0', latest: version }), false);
  }
  assert.equal(lifecycleCurrent({ installed: true, version: 'codex-cli 2.0.0', latest: '2.0.0' }), true);
  assert.equal(lifecycleCurrent({ installed: true, version: '1.0.0', latest: '2.0.0', update_available: true }), false);
  assert.equal(lifecycleCurrent({ installed: false, version: '2.0.0', latest: '2.0.0' }), false);
});

test('native updates distinguish unchanged from version transitions in both languages', () => {
  for (const dictionary of [en, fr]) {
    const translate = (key, vars = {}) => dictionary[key].replace(/\{(\w+)\}/g, (_, name) => vars[name]);
    assert.equal(lifecycleResult({ result: 'unchanged' }, translate), dictionary['harnesses.lifecycle.unchanged']);
    assert.match(lifecycleResult({ result: 'updated', from_version: 'agy 1.0.0', version: 'agy 2.0.0' }, translate), /1\.0\.0 → 2\.0\.0/);
    assert.ok(dictionary['harnesses.lifecycle.check_update']);
    assert.ok(dictionary['harnesses.lifecycle.version_unknown']);
  }
});

test('machine rows offer installation on paired machines and retain lifecycle logs', () => {
  const page = readFileSync(new URL('../next/js/features/harnesses/page.js', import.meta.url), 'utf8');
  const row = page.slice(page.indexOf('function MachineRow'), page.indexOf('// Discussions :'));
  assert.match(row, /target=\$\{i\.machine\}/);
  assert.match(row, /const list = installs;/);
  assert.match(row, /x\.check_update/);
  assert.match(row, /lifecycleCurrent\(x\)/);
  assert.match(row, /<pre>\$\{log\.text\}<\/pre>/);
  assert.doesNotMatch(row, /version \|\| '\?'/);
});

test('channel labels and repair selection are shared by machine and lifecycle controls', async () => {
  const { lifecycleChannel, lifecycleInstallAction } = await import('../next/js/features/harnesses/lifecycle-state.js');
  assert.equal(lifecycleInstallAction({ installed: false, can_repair: true }), 'repair');
  assert.equal(lifecycleInstallAction({ installed: false, can_repair: false }), 'install');
  for (const dictionary of [en, fr]) {
    const translate = key => dictionary[key];
    for (const channel of ['npm', 'native', 'homebrew', 'unknown']) {
      assert.ok(lifecycleChannel({ channel }, translate));
    }
    assert.equal(lifecycleResult({ result: 'repaired' }, translate), dictionary['harnesses.lifecycle.repaired']);
    assert.ok(dictionary['harnesses.lifecycle.repair']);
    assert.ok(dictionary['harnesses.lifecycle.manual_update']);
  }
  const page = readFileSync(new URL('../next/js/features/harnesses/page.js', import.meta.url), 'utf8');
  const row = page.slice(page.indexOf('function MachineRow'), page.indexOf('// Discussions :'));
  assert.match(row, /lifecycleChannel\(x, t\)/);
  const lifecycle = readFileSync(new URL('../next/js/features/harnesses/lifecycle.js', import.meta.url), 'utf8');
  assert.match(lifecycle, /lifecycleInstallAction\(x\)/);
  assert.match(lifecycle, /run\(installAction\)/);
});
