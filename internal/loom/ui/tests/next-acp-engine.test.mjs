import { french } from './i18n-fixture.mjs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

function reducer() {
  const source = fs.readFileSync(new URL('../next/js/features/chat/engine.js', import.meta.url), 'utf8')
    .replace(/^import .*;\n/gm, '').replace(/^export /gm, '');
  const context = vm.createContext({
    t: french,
    createStore: state => ({ get: () => state, set: patch => Object.assign(state, patch) }),
    document: { addEventListener() {} }, refreshNav() {}, toast() {},
    setTimeout, Date, Map, Set,
  });
  vm.runInContext(source + '\nglobalThis.store = chat; globalThis.event = onEvent;', context);
  return context;
}

test('ACP tools retain full objects and IDs are scoped to each turn', () => {
  const c = reducer();
  c.event({ reset: true, replay: true });
  c.event({ type: 'turn_start', text: 'one' });
  const first = { id: 'reused', title: 'Write', kind: 'edit', status: 'pending', diffs: [{ old: '', new: 'x' }], input: '{"path":"x"}', output: '' };
  c.event({ type: 'tool_start', tool: first });
  c.event({ type: 'tool_start', tool: { id: 'parallel', title: 'Read', status: 'in_progress' } });
  c.event({ type: 'tool_end', tool: { ...first, status: 'completed', output: 'done' } });
  c.event({ type: 'turn_done' });
  c.event({ type: 'turn_start', text: 'two' });
  c.event({ type: 'tool_start', tool: { ...first, title: 'Second' } });
  c.event({ type: 'tool_end', tool: { ...first, title: 'Second', status: 'failed', output: 'denied' } });
  const tools = c.store.get().items.filter(i => i.k === 'tool');
  assert.equal(tools.length, 3);
  assert.equal(tools[0].tool.output, 'done');
  assert.equal(tools[0].tool.diffs[0].new, 'x');
  assert.equal(tools[1].tool.title, 'Read');
  assert.equal(tools[2].tool.output, 'denied');
});

test('ACP plan, approvals and session state survive event reduction', () => {
  const c = reducer();
  c.event({ reset: true, replay: true });
  c.event({ type: 'turn_start', text: 'one' });
  c.event({ type: 'plan', entries: [{ content: 'Read', status: 'pending' }] });
  c.event({ type: 'plan', entries: [{ content: 'Read', status: 'completed' }] });
  c.event({ type: 'approval_request', approval: { id: 'approval', options: [{ id: 'once' }] } });
  c.event({ type: 'approval_resolved', id: 'approval', option_id: 'once', auto: false });
  c.event({ type: 'mode', current: 'plan', modes: [{ id: 'plan' }] });
  c.event({ type: 'config', options: [{ id: 'model', currentValue: 'reported' }] });
  c.event({ type: 'files', files: [{ path: '/work/file', add: 1 }] });
  c.event({ type: 'usage', context: { used: 24, size: 4096 }, cost: { amount: 0.01, currency: 'USD' } });
  c.event({ type: 'commands', commands: [{ name: 'review' }] });
  const state = c.store.get();
  assert.equal(state.items.filter(i => i.k === 'plan').length, 1);
  assert.equal(state.items.find(i => i.k === 'plan').entries[0].status, 'completed');
  assert.equal(state.items.find(i => i.k === 'approval').resolved.option_id, 'once');
  assert.equal(state.harness.mode, 'plan');
  assert.equal(state.harness.config[0].currentValue, 'reported');
  assert.equal(state.harness.files[0].add, 1);
  assert.equal(state.harness.usage.context.used, 24);
  assert.equal(state.harness.commands[0].name, 'review');
  c.event({ reset: true, replay: true, session: { status: 'idle', workdir: '/work', permission: 'edits', mode: 'default', available_modes: [{ id: 'default' }], available_config_options: [], changed_files: [], commands: [], harness_usage: null } });
  assert.equal(c.store.get().harness.workdir, '/work');
  assert.equal(c.store.get().harness.permission, 'edits');
  assert.equal(c.store.get().harness.mode, 'default');
});
