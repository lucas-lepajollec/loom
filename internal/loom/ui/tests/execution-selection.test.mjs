import { chatShape, validShape, eventShape } from '../next/js/core/shape.js';
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { localChoice, executionKey } from '../next/js/features/chat/execution.js';
import { french } from './i18n-fixture.mjs';
import htm from '../next/vendor/htm.mjs';

const model = { id: 'local:/engine/gemma.gguf', kind: 'local', model: '/engine/gemma.gguf', engine_value: 'gemma.gguf', enabled: true };
test('local execution resolves engine paths and aliases without guessing from filenames', () => {
  const other = { ...model, id: 'local:/other/gemma.gguf', model: '/other/gemma.gguf', engine_value: '/other/gemma.gguf' };
  assert.equal(localChoice([other, model], { path: model.model, model: 'gemma.gguf' }), model);
  assert.equal(localChoice([other, model], { model: 'gemma.gguf' }), model);
  assert.equal(localChoice([other], { model: 'gemma.gguf' }), null);
  assert.equal(localChoice([model, { ...other, engine_value: 'gemma.gguf' }], { model: 'gemma.gguf' }), null);
  assert.equal(localChoice([{ ...model, enabled: false }], { path: model.model }), null);
  assert.equal(localChoice([{ ...model, model: 'C:\\models\\gemma.gguf' }], { path: 'C:/models/gemma.gguf' }).id, model.id);
});

function engine({ choices = [model], selectError, openError, loadError } = {}) {
  const calls = [], notices = [];
  let session = { id: 'same-discussion', runtime_id: 'pi', provider_name: 'Pi', model: 'loom:gemma.gguf', messages: [{ role: 'user', content: 'keep' }] };
  const appState = { unavailable: {}, workspace: { models: [] }, status: { model: 'gemma.gguf', health: true }, nav: { conversations: [] } };
  const env = {
    chatShape, validShape, eventShape, t: french, localChoice, createStore: state => ({ get: () => state, set: patch => Object.assign(state, patch) }),
    document: { addEventListener() {} }, app: { get: () => appState },
    localStorage: { setItem() {}, removeItem() {} }, setTimeout: () => {}, Date, Map, Set,
    toast: (...args) => notices.push(args), refreshNav: async () => {},
    refreshWorkspace: async () => ({ ok: true, models: choices }),
    get: async path => { calls.push(path); return openError ? { ok: false, error: openError } : { ok: true, session, context: { revision: 'local' } }; },
    post: async (path, body) => {
      calls.push([path, body]);
      if (path.endsWith('/select')) {
        if (selectError) return { ok: false, error: selectError };
        session = { ...session, runtime_id: 'llama.cpp', provider_name: 'llama.cpp', model: model.model };
      }
      if (path.endsWith('/local')) return { ok: true, session, context: { revision: 'local' } };
      if (path === '/api/load-model' && loadError) return { ok: false, error: loadError };
      return { ok: true };
    },
  };
  const source = fs.readFileSync(new URL('../next/js/features/chat/engine.js', import.meta.url), 'utf8');
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.choose = chooseLocal; globalThis.store = chat;', env);
  env.store.set({ mode: 'thread', sessionId: session.id, session, items: [{ k: 'user', text: 'keep' }], harness: { mode: 'agent', config: [{ id: 'pi-mode' }] } });
  return { env, calls, notices };
}

test('Pi plus Gemma to direct Gemma changes runtime before engine loading and retains discussion identity', async () => {
  const h = engine();
  assert.equal(await h.env.choose({ path: model.model, model: 'gemma.gguf', name: 'Gemma' }), true);
  const state = h.env.store.get();
  assert.equal(state.sessionId, 'same-discussion');
  assert.equal(state.mode, 'native');
  assert.equal(state.session.runtime_id, 'llama.cpp');
  assert.equal(state.session.messages[0].content, 'keep');
  assert.equal(state.frozen[0].text, 'keep');
  assert.equal(state.harness.mode, '');
  assert.equal(state.harness.config.length, 0);
  assert.equal(h.calls[0][0], '/api/runtime/sessions/select');
  assert.equal(h.calls[0][1].choice_id, model.id);
  assert.equal(h.calls.at(-1)[0], '/api/load-model');
});

test('a missing choice or failed switch cannot silently load a model while retaining Pi', async () => {
  for (const options of [{ choices: [] }, { selectError: 'route rejected' }, { openError: 'open failed' }]) {
    const h = engine(options);
    assert.equal(await h.env.choose({ path: model.model, model: 'gemma.gguf', name: 'Gemma' }), false);
    assert.equal(h.calls.some(c => Array.isArray(c) && c[0] === '/api/load-model'), false);
    assert.equal(h.env.store.get().mode, 'thread');
    assert.ok(h.notices.some(n => n[1] === 'err'));
  }
});

test('failed loading is reported with the selected direct execution, never labeled as Pi', async () => {
  const h = engine({ loadError: 'engine refused model' });
  assert.equal(await h.env.choose({ path: model.model, model: 'gemma.gguf', name: 'Gemma' }), false);
  assert.equal(h.env.store.get().mode, 'native');
  assert.equal(h.env.store.get().session.runtime_id, 'llama.cpp');
  assert.equal(h.notices.at(-1)[0], 'engine refused model');
});

test('execution identity distinguishes the same model across harnesses and cloud providers', () => {
  const state = { mode: 'thread', session: { runtime_id: 'pi', provider_id: '', provider_name: 'Pi', model: 'gemma' } };
  const key = executionKey(state);
  assert.notEqual(key, executionKey({ ...state, session: { ...state.session, runtime_id: 'codex', provider_name: 'Codex' } }));
  assert.notEqual(key, executionKey({ ...state, mode: 'native' }));
  const cloud = { mode: 'thread', session: { runtime_id: 'openai-compatible', provider_id: 'a', model: 'same' } };
  assert.notEqual(executionKey(cloud), executionKey({ ...cloud, session: { ...cloud.session, provider_id: 'b' } }));
});

test('picker keeps an execution label next to the same model on local and harness routes', () => {
  const html = htm.bind((type, props, ...children) => ({ type, props: props || {}, children }));
  const flatten = x => Array.isArray(x) ? x.flatMap(flatten) : x && typeof x === 'object' ? [x, ...flatten(x.children)] : [];
  const text = x => Array.isArray(x) ? x.map(text).join('') : x && typeof x === 'object' ? text(x.children) : String(x ?? '');
  let state = { mode: 'thread', session: { runtime_id: 'pi', provider_name: 'Pi', model: 'loom:gemma.gguf' } };
  const appState = { unavailable: {}, workspace: { models: [] }, models: [{ name: 'gemma.gguf', path: model.model, value: 'gemma.gguf' }], presets: [{ id: 'preset', name: 'Gemma preset', model: 'gemma.gguf' }], status: { model: 'gemma.gguf', health: true, preset_id: 'preset' } };
  let hook = 0;
  const env = { html, chatShape, validShape, eventShape, t: french, getLang: () => 'fr', executionKey, runtimeKind: id => id === 'pi' ? 'harness' : 'local',
    app: { get: () => appState }, chat: { get: () => state }, Icon: 'Icon', cls: (...v) => v.filter(Boolean).join(' '),
    baseName: p => String(p || '').split('/').pop(), useStore: (store, select) => select(store.get()), useState: v => [hook++ === 0 ? {} : hook === 2 ? 'local' : v, () => {}], useMemo: f => f(), useRef: () => ({ current: null }), Popover: 'Popover', Seg: 'Seg', fmtBytes: () => '1 GB' };
  const source = fs.readFileSync(new URL('../next/js/features/chat/picker.js', import.meta.url), 'utf8');
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.picker = Picker;', env);
  const render = () => { hook = 0; return env.picker(); };
  const label = tree => text(flatten(tree).find(n => n.props.class === 'exec-route'));
  let tree = render();
  assert.equal(label(tree), 'Pi');
  assert.equal(flatten(tree).find(n => n.type?.name === 'Row' && n.props.title === 'Gemma preset').props.active, false, 'a loaded engine preset is not the selected harness execution');
  state = { mode: 'native', session: { runtime_id: 'llama.cpp', model: 'gemma.gguf' } };
  tree = render();
  assert.equal(label(tree), 'Local');
  assert.equal(flatten(tree).find(n => n.type?.name === 'Row' && n.props.title === 'Gemma preset').props.active, true);
});

test('closed Picker does not traverse large model or workspace catalogs', () => {
  const untouched = new Proxy([], { get(target, key) { if (key === 'filter' || key === Symbol.iterator) throw new Error('catalog traversed while closed'); return Reflect.get(target, key); } });
  const state = { status: null, workspace: { models: untouched }, models: untouched, presets: untouched, unavailable: {} };
  const env = { html: () => null, t: french, getLang: () => 'fr', executionKey, runtimeKind: () => 'local',
    app: { get: () => state }, chat: { get: () => ({ mode: 'native' }) }, Icon: 'Icon', cls: () => '', baseName: String,
    useStore: (store, select) => select(store.get()), useState: value => [value, () => {}], useMemo: f => f(), useRef: () => ({}) };
  const source = fs.readFileSync(process.env.LOOM_PICKER_BASELINE || new URL('../next/js/features/chat/picker.js', import.meta.url), 'utf8');
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.picker = Picker;', env);
  assert.doesNotThrow(() => env.picker());
});
