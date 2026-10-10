import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';

function component(file, name, overrides = {}) {
  const source = fs.readFileSync(name === 'AgentDetail' && process.env.LOOM_HARNESS_BASELINE || new URL('../next/js/features/harnesses/' + file, import.meta.url), 'utf8');
  const states = [], env = {}; let cursor = 0;
  for (const match of source.matchAll(/^import \{([^}]+)\}/gm)) for (const binding of match[1].split(',')) {
    const local = binding.trim().split(/\s+as\s+/).pop(); env[local] = local;
  }
  Object.assign(env, { t: key => key, html: htm.bind((tag, props, ...children) => ({ tag, props: props || {}, children: children.flat(Infinity) })),
    cls: (...args) => args.filter(Boolean).join(' '), useEffect() {},
    useState(initial) { const index = cursor++; if (!(index in states)) states[index] = typeof initial === 'function' ? initial() : initial; return [states[index], v => states[index] = typeof v === 'function' ? v(states[index]) : v]; },
    ...overrides,
  });
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = ' + name + ';', env);
  return props => { cursor = 0; return env.view(props); };
}
const flat = node => Array.isArray(node) ? node.flatMap(flat) : node && typeof node === 'object' ? [node, ...(node.children || []).flatMap(flat)] : [];

test('extracted models keep default selection, expansion and exact choice dispatch', () => {
  const render = component('models.js', 'AgentModels'), selected = [];
  const props = { groups: Array.from({ length: 10 }, (_, i) => ({ name: 'model-' + i, levels: [{ value: 'model-' + i + '-low', level: 'low' }, { value: 'model-' + i + '-medium', level: 'medium' }] })),
    modelOpt: { currentValue: 'model-0-low' }, list: [], used: true, choiceFor: id => ({ id }), startWith: choice => selected.push(choice.id) };
  let nodes = flat(render(props));
  let chips = nodes.filter(n => n.props.class?.includes('mchip'));
  assert.equal(chips.length, 8);
  assert.equal(chips[0].props.class, 'mchip on');
  chips[0].props.onClick(); chips[1].props.onClick();
  assert.deepEqual(selected, ['model-0-low', 'model-1-medium']);
  nodes.find(n => n.props.class?.includes('more-models')).props.onClick();
  nodes = flat(render(props)); assert.equal(nodes.filter(n => n.props.class?.includes('mchip')).length, 10);
  nodes.find(n => n.props.class?.includes('more-models')).props.onClick();
  assert.equal(flat(render({ ...props, used: false })).filter(n => n.props.class?.includes('mchip') && n.props.disabled).length, 8);
});

test('extracted settings respect supported sources and preserve resource dialog lifecycle', () => {
  const render = component('settings.js', 'AgentSettings'), rt = { id: 'fixture', name: 'Fixture', features: { model_sources: ['native', 'loom'], mcp_selection: true } };
  let nodes = flat(render({ rt }));
  assert.ok(nodes.some(n => typeof n.tag === 'function' && n.tag.name === 'ModelSource'));
  const choose = nodes.find(n => n.tag === 'button'); assert.ok(choose); choose.props.onClick();
  nodes = flat(render({ rt })); const modal = nodes.find(n => n.tag === 'Modal');
  assert.ok(modal); assert.equal(modal.props.wide, true); assert.equal(modal.props.title, 'agents.resources');
  assert.ok(flat(render({ rt, enabled: false })).some(n => n.tag === 'Modal'), 'an open resource dialog survives observation changes');
  assert.ok(!flat(render({ rt, enabled: false })).some(n => n.tag === 'section'), 'unmanaged settings remain hidden');
  modal.props.onClose(); assert.ok(!flat(render({ rt })).some(n => n.tag === 'Modal'));
  assert.ok(!flat(render({ rt: { ...rt, features: {} } })).some(n => typeof n.tag === 'function' && n.tag.name === 'ModelSource'));
});

test('machine switches keep explicit enable consent and disabled readiness', async () => {
  const calls = [], confirmations = [];
  const render = component('agent-machines.js', 'MachineRow', { lifecycleVersion: s => s, lifecycleCurrent: () => false,
    confirm: async (...args) => { confirmations.push(args); return true; }, refreshWorkspace: async () => {},
    post: async (url, data) => { calls.push({ url, data }); return { ok: true }; },
  });
  const i = { machine: 'local', harness: 'codex', name: 'Codex', installed: true, managed: true, ready: true, version: '1.2.3' };
  const switches = flat(render({ i, onChanged() {} })).filter(n => n.tag === 'Switch');
  assert.equal(switches.length, 2); await switches[1].props.onChange(true);
  assert.equal(confirmations.length, 1); assert.equal(calls[0].url, '/api/agents/installations');
  assert.equal(calls[0].data.harness, 'codex'); assert.equal(calls[0].data.enabled, true); assert.equal(calls[0].data.consent, true);
  assert.ok(flat(render({ i: { ...i, ready: false }, onChanged() {} })).find(n => n.tag === 'Switch' && n.props.label === 'agents.use').props.disabled);
});


test('agent header separates identity/refresh from discussion actions', () => {
  const rt = { id: 'codex', name: 'Codex', kind: 'harness', available: true, connected: true, features: {}, capabilities: ['workdir'] };
  const initial = [[{ machine: 'local', harness: rt.id, runtime_id: rt.id, managed: true }], { config: [] }, { installed: true, auth: { connected: false } }]; let cursor = 0;
  const render = component('agent-page.js', 'AgentDetail', { useState: value => [cursor < initial.length ? initial[cursor++] : value, () => {}],
    useStore: (_store, select) => select({ nav: { conversations: [] } }), configOptions: (_f, config) => config,
    byCat: (config, cat) => config.find(o => o.category === cat), optValues: o => o?.options || [], splitEffort: () => [], tSource: s => s, protocolLabel: () => 'ACP',
  });
  const tree = render({ rt, models: [{ runtime_id: rt.id, id: 'codex:native' }], onEdit() {} }), nodes = flat(tree);
  const head = nodes.find(n => n.props.class?.includes('agent-h'));
  assert.ok(head.children.some(n => n?.props?.class?.includes('object-refresh')), 'refresh belongs to the identity header');
  const actions = head.children.find(n => n?.props?.class?.includes('acts'));
  assert.ok(actions); assert.ok(!flat(actions).some(n => n.props.class?.includes('object-refresh')));
  assert.ok(flat(actions).some(n => n.props.class?.includes('primary')), 'discussion action remains present');
  assert.ok(nodes.some(n => n.props.class?.includes('object-state')), 'state has its own phone grid placement');
  assert.ok(nodes.some(n => n.props.class?.includes('object-description')), 'description has its own full-width placement');
});

test('machine detail agent controls retain visible manage/use labels on phones', () => {
  const source = fs.readFileSync(process.env.LOOM_MACHINES_BASELINE || new URL('../next/js/features/settings/machines.js', import.meta.url), 'utf8');
  const env = { html: htm.bind((tag, props, ...children) => ({ tag, props: props || {}, children: children.flat(Infinity) })),
    useState: v => [v === null ? [{ harness: 'codex', name: 'Codex', installed: true, managed: true, ready: true, version: '1.2.3' }] : v, () => {}],
    useEffect() {}, t: key => key,
  };
  for (const match of source.matchAll(/^import \{([^}]+)\}/gm)) for (const binding of match[1].split(',')) {
    const local = binding.trim().split(/\s+as\s+/).pop(); if (!(local in env)) env[local] = local;
  }
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = HarnessesSection;', env);
  const nodes = flat(env.view({ m: null }));
  const row = nodes.find(n => n.props.class === 'ag-row'); assert.ok(row);
  for (const label of ['agents.manage', 'agents.use']) assert.ok(flat(row).some(n => n.tag === 'span' && n.children.includes(label)), label + ' needs a visible row label');
});
