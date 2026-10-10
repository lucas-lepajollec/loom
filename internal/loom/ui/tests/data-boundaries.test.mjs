import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const root = process.env.LOOM_UI_BASELINE ? new URL('file://' + process.env.LOOM_UI_BASELINE + '/') : new URL('../next/js/core/', import.meta.url);
const read = f => fs.readFileSync(new URL(f, root), 'utf8');
const plain = s => s.replace(/^import .*;\n/gm, '').replace(/^export /gm, '');
function harness() {
  const replies = new Map();
  const env = { t: x => x, localStorage: { getItem: () => null, removeItem() {} }, innerWidth: 390,
    location: { hash: '#/chat' }, history: {}, document: { hidden: false, addEventListener() {} },
    addEventListener() {}, setTimeout, clearTimeout, AbortController,
    clearObservations() {}, singleFlight: f => f, visibleRefresh: () => () => {},
    createStore: initial => { let state = initial; return { get: () => state, set: patch => { state = { ...state, ...patch }; } }; },
    fetch: async url => { const r = replies.get(url); if (r instanceof Error) throw r; return new Response(r?.raw ?? JSON.stringify(r?.body ?? []), { status: r?.status ?? 200 }); },
  };
  const helper = new URL('shape.js', root);
  if (fs.existsSync(helper)) vm.runInNewContext(plain(fs.readFileSync(helper, 'utf8')), env);
  vm.runInNewContext(plain(read('api.js')), env);
  vm.runInNewContext(plain(read('state.js')) + '\nglobalThis.state = {app,refreshLibrary,refreshNav,refreshWorkspace,refreshStatus,refreshEngineNode,refreshHardware,get};', env);
  return { ...env.state, replies };
}
test('502 models error cannot poison Chat; presets refresh independently and models recover without reload', async () => {
  const h = harness(), previous = [{ name: 'kept.gguf', path: 'kept' }];
  h.app.set({ models: previous });
  h.replies.set('/api/models', { status: 502, body: { ok: false, error: 'remote engine unreachable' } });
  h.replies.set('/api/presets', { body: [{ id: 'new', name: 'New preset' }] });
  await h.refreshLibrary();
  assert.equal(h.app.get().models, previous);
  assert.equal(h.app.get().presets[0].id, 'new');
  assert.equal(h.app.get().unavailable.models, true);
  assert.doesNotThrow(() => h.app.get().models.filter(m => m.name));
  h.replies.set('/api/models', { body: [{ name: 'recovered.gguf' }] });
  await h.refreshLibrary();
  assert.equal(h.app.get().models[0].name, 'recovered.gguf');
  assert.equal(h.app.get().unavailable.models, false);
});

test('malformed collection responses preserve all shared observations and independent navigation', async () => {
  const h = harness();
  const ws = { ok: true, models: [], runtimes: [], providers: [], projects: [], capabilities: [] };
  h.replies.set('/api/workspace', { body: ws }); await h.refreshWorkspace();
  const previous = h.app.get().workspace;
  h.replies.set('/api/workspace', { body: { ok: true, models: {}, runtimes: [] } });
  await h.refreshWorkspace(); assert.equal(h.app.get().workspace, previous);
  assert.equal(h.app.get().unavailable.workspace, true);
  h.replies.set('/api/chat/history', { body: { conversations: [{ id: 'local', turns: 1 }], projects: [], active: 'local' } });
  h.replies.set('/api/runtime/sessions', { body: { ok: true, sessions: [{ id: 'cloud', runtime_id: 'cloud', message_count: 1 }] } });
  await h.refreshNav();
  h.replies.set('/api/chat/history', new Error('offline'));
  h.replies.set('/api/runtime/sessions', { body: { ok: true, sessions: [{ id: 'agent', runtime_id: 'codex', message_count: 1 }] } });
  await h.refreshNav();
  assert.equal(h.app.get().nav.conversations[0].id, 'agent');
  assert.equal(h.app.get().nav.conversations[1].id, 'local');
  assert.equal(h.app.get().unavailable.history, true);
  h.replies.set('/api/runtime/sessions', { body: { ok: true, session: { id: 'wrong-envelope' } } });
  await h.refreshNav(); assert.equal(h.app.get().nav.conversations[0].id, 'agent');
  assert.equal(h.app.get().unavailable.sessions, true);
});
test('all refreshes reject array/object mismatches and recover on the same store', async () => {
  const h = harness();
  for (const [refresh, key, url, good, bad] of [
    [h.refreshHardware, 'gpus', '/api/vram', [{ name: 'GPU' }], {}],
    [h.refreshEngineNode, 'engineNode', '/api/engine/node', { ok: true, remote: true, hostname: 'node' }, []],
    [h.refreshStatus, 'status', '/api/status', { active: true }, []],
  ]) {
    h.replies.set('/api/ping', { body: { ok: true, version: 'fixture' } });
    h.replies.set(url, { body: good }); await refresh(); const previous = h.app.get()[key];
    h.replies.set(url, { body: bad }); await refresh(); assert.equal(h.app.get()[key], previous); assert.equal(h.app.get().unavailable[key], true);
    h.replies.set(url, { body: good }); await refresh(); assert.equal(h.app.get().unavailable[key], false);
  }
});
test('store writes cannot replace collections with error objects, including nested harness lists', () => {
  const source = read('lib.js').slice(read('lib.js').indexOf('export function createStore'), read('lib.js').indexOf('export function useStore'));
  const env = { requestAnimationFrame: () => 1, cancelAnimationFrame() {}, setTimeout: () => 2, clearTimeout() {} };
  const helper = new URL('shape.js', root);
  if (fs.existsSync(helper)) vm.runInNewContext(plain(fs.readFileSync(helper, 'utf8')), env);
  vm.runInNewContext(plain(source) + '\nglobalThis.store = createStore({models:[],harness:{commands:[],config:[]}});', env);
  const before = env.store.get();
  env.store.set({ models: { ok: false }, harness: { commands: {}, config: [] }, busy: true });
  assert.equal(env.store.get().models, before.models); assert.equal(env.store.get().harness, before.harness);
  assert.equal(env.store.get().busy, true);
});

test('read boundary rejects HTTP, JSON and collection failures in every catalog family', async () => {
  const h = harness();
  for (const [url, body] of [
    ['/api/models', [null]], ['/api/presets', {}], ['/api/vram', {}],
    ['/api/models/download/status', {}], ['/api/agents/catalog', {}], ['/api/backends/custom', {}],
    ['/api/machines', { ok: true, machines: {} }], ['/api/machines', { ok: true, offers: { fixture: {} } }],
    ['/api/agents/installations', { ok: true, installations: {} }], ['/api/voice/models', { models: {} }],
    ['/api/voice/models', { models: [{ id: 'tts', languages: {} }] }],
    ['/api/hub/search', { models: {} }], ['/api/hub/model', { files: {} }],
    ['/api/brain/sources', { sources: {} }], ['/api/brain/memory', { items: {} }],
    ['/api/skills/targets', { targets: {} }], ['/api/mcp', { servers: {} }],
    ['/api/usage', { quotas: {} }], ['/api/usage/native', { harnesses: {} }],
    ['/api/tasks', { tasks: {} }], ['/api/terminals', { terminals: {} }],
    ['/api/engines/vllm/models', { models: {} }], ['/api/bench/queue', { job: { rows: {} } }],
    ['/api/runtimes/codex/probe', { probe: { config: [{ options: {} }] } }],
    ['/api/runtimes/pi/probe', { probe: { features: { model_sources: {} } } }],
    ['/api/mcp/sources', { sources: [{ servers: {} }] }],
    ['/api/brain/sources', { sources: [{ include: {} }] }],
    ['/api/brain/search', { hits: [{ heading: {}, highlights: [] }] }],
    ['/api/skills/targets', { targets: [{ harnesses: null }] }],
    ['/api/startup', { engines: { vllm_models: {} } }],
    ['/api/policy', { rules: [{ conditions: { command_prefixes: {} } }] }],
    ['/api/providers/models', { models: null }], ['/api/fs/dirs', { ok: true }],
  ]) {
    h.replies.set(url, { body }); await assert.rejects(h.get(url), /Invalid API data/, url);
  }
  for (const r of [
    { status: 502, body: { ok: false, error: 'remote engine unreachable' } },
    { status: 200, body: { ok: false, error: 'failed observation' } },
    { raw: '{broken' }, new Error('offline'),
  ]) {
    h.replies.set('/api/models', r); await assert.rejects(h.get('/api/models'));
  }
  h.replies.set('/api/models', { body: [] }); assert.equal((await h.get('/api/models')).length, 0);
  h.replies.set('/api/env/matrix', { body: { results: [{ ok: false, error: 'service unreachable' }] } });
  assert.equal((await h.get('/api/env/matrix')).results[0].ok, false, 'negative observations are legitimate records, not API error envelopes');
  h.replies.set('/api/engine/node', { body: { kind: 'vllm', models: ['served-model'] } });
  assert.equal((await h.get('/api/engine/node')).models[0], 'served-model', 'shared endpoint also supports direct-engine probe responses');
});
test('vLLM shared observations retain the last valid engine and library on failed refresh', async () => {
  const h = harness(), env = { get: h.get };
  const source = fs.readFileSync(process.env.LOOM_VLLM_BASELINE || new URL('../next/js/features/settings/vllm.js', import.meta.url), 'utf8');
  vm.runInNewContext(plain(source) + '\nglobalThis.refresh = [vload,vloadLib]; globalThis.store = vstore;', env);
  h.replies.set('/api/engines/vllm', { body: { ok: true, installed: true, job: '' } });
  h.replies.set('/api/engines/vllm/models', { body: { models: [{ id: 'fixture' }], cache: '' } });
  await Promise.all(env.refresh.map(f => f())); const engine = env.store.x, library = env.store.lib;
  h.replies.set('/api/engines/vllm', new Error('offline')); h.replies.set('/api/engines/vllm/models', new Error('offline'));
  await Promise.all(env.refresh.map(f => f())); assert.equal(env.store.x, engine); assert.equal(env.store.lib, library);
});

test('usage observation writes reject invalid lists and retain a successful snapshot', () => {
  const env = {}, helper = new URL('shape.js', root);
  if (fs.existsSync(helper)) vm.runInNewContext(plain(fs.readFileSync(helper, 'utf8')), env);
  vm.runInNewContext(plain(read('observations.js')) + '\nglobalThis.observation = {remember,recall};', env);
  const rows = [{ runtime_id: 'codex' }]; env.observation.remember('native:7', rows);
  env.observation.remember('native:7', { ok: false, error: 'offline' });
  assert.equal(env.observation.recall('native:7'), rows);
});

test('the Composer retains cached harness controls during a failed command refresh', async () => {
  const source = fs.readFileSync(process.env.LOOM_COMPOSER_BASELINE || new URL('../next/js/features/chat/composer.js', import.meta.url), 'utf8');
  const effects = [], env = {};
  for (const match of source.matchAll(/^import \{([^}]+)\}/gm)) for (const symbol of match[1].split(',')) env[symbol.trim()] = symbol.trim();
  const state = { mode: 'thread', session: { runtime_id: 'pi' }, harness: {} };
  Object.assign(env, { t: x => x, html: () => null, cls: () => '', Set,
    chat: { get: () => state }, app: { get: () => ({}) }, useStore: (store, select) => select(store.get()),
    useState: v => [v, () => {}], useRef: v => ({ current: v }), useEffect: f => effects.push(f),
    currentExec: () => ({}), runtimeKind: () => 'harness', runtimeCaps: () => [], slashEntries: () => [],
    configOptions: () => [], modeOptions: () => [], localStorage: { getItem: () => null },
    get: async () => { throw new Error('offline'); },
  });
  vm.runInNewContext(plain(source) + '\nglobalThis.view = Composer; globalThis.cache = probes;', env);
  const previous = { commands: [], modes: [{ id: 'plan' }] };
  env.cache.pi = previous; env.view(); effects.forEach(f => f());
  assert.equal(env.cache.pi, previous, 'pending refresh must not erase the previous observation');
  await new Promise(r => setImmediate(r)); assert.equal(env.cache.pi, previous);
});

test('raw upload responses cannot publish an object as an attachment path', async () => {
  const { validData } = await import('../next/js/core/shape.js');
  let body = { ok: true, id: 'fixture', path: {} };
  const env = { validData, AbortController, t: x => x, request: async () => new Response(JSON.stringify(body)),
    FileReader: class { readAsDataURL() { this.result = 'data:;base64,eA=='; this.onload(); } } };
  const source = fs.readFileSync(process.env.LOOM_COMPOSER_BASELINE || new URL('../next/js/features/chat/composer.js', import.meta.url), 'utf8');
  vm.runInNewContext(plain(source) + '\nglobalThis.attach = upload;', env);
  const file = { size: 1, name: 'file.txt', slice: () => null };
  await assert.rejects(env.attach(file));
  body = { ok: true, path: 'uploads/file.txt' };
  assert.equal((await env.attach(file)).path, 'uploads/file.txt');
});

test('raw benchmark cancellation retains the job when the response has malformed rows', async () => {
  const { validData } = await import('../next/js/core/shape.js');
  const source = fs.readFileSync(process.env.LOOM_BENCH_BASELINE || new URL('../next/js/features/bench/page.js', import.meta.url), 'utf8');
  const previous = { id: 'fixture', status: 'running', rows: [] }, slots = []; let cursor = 0;
  const env = {}; let body = { ok: true, job: { rows: {} } };
  for (const match of source.matchAll(/^import \{([^}]+)\}/gm)) for (const symbol of match[1].split(',')) env[symbol.trim()] = symbol.trim();
  Object.assign(env, { validData, t: x => x, cls: () => '', benchChoices: () => [], useEffect() {}, useVisibleRefresh() {}, toast() {},
    app: { get: () => ({ models: [], presets: [] }) }, useStore: (store, select) => select(store.get()), useRef: value => ({ current: value }),
    useState: value => { const i = cursor++; slots[i] = i === 9 ? previous : value; return [slots[i], v => { slots[i] = v; }]; },
    html: (_strings, ...values) => { for (const value of values) if (typeof value === 'function' && value.name === 'stop') env.cancel = value; return null; },
    request: async () => ({ ok: true, json: async () => body }),
  });
  vm.runInNewContext(plain(source) + '\nglobalThis.view = BenchPage;', env);
  env.view(); assert.equal(typeof env.cancel, 'function'); await env.cancel(); assert.equal(slots[9], previous);
  body = { ok: true, job: { id: 'fixture', status: 'cancel', rows: [] } }; await env.cancel(); assert.equal(slots[9].status, 'cancel');
});
