import test from 'node:test';
import assert from 'node:assert/strict';
import { modeOptions, configOptions, permissionOptions, protocolLabel } from '../next/js/features/harnesses/options.js';
import fr from '../next/js/i18n/fr.js';
import en from '../next/js/i18n/en.js';

test('controls cannot exceed the backend contract, including stale probes', () => {
  for (const features of [
    { modes: ['accept-edits', 'plan', 'full'], permissions: [], config_options: ['model', 'reasoning_effort', 'sandbox'] },
    { modes: [], permissions: [], config_options: ['model', 'reasoning_effort'] },
    { modes: [], permissions: ['ask', 'full'], config_options: ['model', 'reasoning_effort'] },
    { modes: ['code'], permissions: ['ask', 'edits', 'full'], config_options: ['model'] },
  ]) {
    const modes = ['default', 'accept-edits', 'plan', 'full', 'code'].map(id => ({ id }));
    const permissions = ['ask', 'edits', 'full', 'deny'].map(value => ({ value }));
    const options = ['model', 'reasoning_effort', 'sandbox', 'ignored'].map(id => ({ id }));
    assert.deepEqual(modeOptions(features, modes).map(m => m.id), features.modes);
    assert.deepEqual(permissionOptions(features, permissions).map(o => o.value), features.permissions);
    assert.deepEqual(configOptions(features, options).map(o => o.id), features.config_options);
  }
  assert.deepEqual(modeOptions(undefined, [{ id: 'default' }]), []);
});

test('agent connection labels identify the actual transport', () => {
  for (const [protocol, label] of Object.entries({ 'app-server': 'App Server', 'pi-rpc': 'RPC', 'opencode-http': 'HTTP', 'agy-stream-json': 'Stream JSON', acp: 'ACP' })) {
    assert.equal(protocolLabel({ protocol }), label);
  }
});

test('agent and memory copy never offers Gemini CLI', () => {
  for (const dict of [fr, en]) {
    for (const [key, value] of Object.entries(dict)) {
      if (/^(agents\.|harnesses\.|memory\.agents\.)/.test(key)) {
        assert.doesNotMatch(JSON.stringify(value), /gemini/i, key);
      }
    }
    assert.match(dict['memory.agents.tip'], /Antigravity/);
  }
});
