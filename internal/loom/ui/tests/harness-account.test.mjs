import { validShape, validData } from '../next/js/core/shape.js';
import { singleFlight } from '../next/js/core/poll.js';
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { webcrypto } from 'node:crypto';
import htm from '../next/vendor/htm.mjs';
import { french } from './i18n-fixture.mjs';

const html = htm.bind((type, props, ...children) => ({ type, props: props || {}, children }));
const source = fs.readFileSync(new URL('../next/js/features/harnesses/account.js', import.meta.url), 'utf8');
const flatten = x => Array.isArray(x) ? x.flatMap(flatten) : x && typeof x === 'object' ? [x, ...flatten(x.children)] : [];
const text = x => Array.isArray(x) ? x.map(text).join('') : x && typeof x === 'object' ? text(x.children) : String(x ?? '');
const button = (tree, name) => flatten(tree).find(x => x.type === 'button' && text(x) === name);

function view(rt) {
  const slots = [], posts = [], timers = [];
  let cursor = 0, effects = [], cleanups = [], tree, closed = 0, connected = 0, changed = 0;
  const env = {
    html, t: french, Modal: 'Modal', TermView: 'TermView', Uint8Array, crypto: webcrypto,
    setTimeout: fn => { timers.push(fn); return fn; }, clearTimeout: fn => { const i = timers.indexOf(fn); if (i >= 0) timers.splice(i, 1); },
    useState(initial) { const i = cursor++; if (!(i in slots)) slots[i] = initial; return [slots[i], v => { slots[i] = typeof v === 'function' ? v(slots[i]) : v; }]; },
    useRef(initial) { const i = cursor++; return slots[i] ||= { current: initial }; },
    useEffect(fn, deps) {
      const i = cursor++, old = slots[i];
      if (!old || deps.some((x, j) => !Object.is(x, old[j]))) { slots[i] = deps; effects.push(() => { cleanups[i]?.(); cleanups[i] = fn(); }); }
    },
    post: async (url, body) => {
      posts.push({ url, body });
      if (url.endsWith('/account') && !body.cancel) return { ok: true, login: { id: 'fixture-login', state: 'starting' } };
      if (url === '/api/terminals') return { ok: true, terminal: { id: 'fixture-terminal', title: 'Native login', running: true } };
      return { ok: true };
    },
    get: async url => url.endsWith('/login')
      ? { ok: true, target: 'local', command: 'pi', title: 'Pi account' }
      : { ok: true, login: { id: 'fixture-login', state: 'waiting', url: 'https://auth.openai.com/codex/device', code: 'ABCD-1234' } },
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.component = HarnessAccount;', env);
  const render = () => {
    cursor = 0;
    tree = env.component({ rt, onClose: () => closed++, onConnect: () => connected++, onChanged: () => changed++ });
    effects.splice(0).forEach(fn => fn()); return tree;
  };
  return { env, posts, timers, render, unmount: () => cleanups.forEach(fn => fn?.()), get closed() { return closed; }, get connected() { return connected; }, get changed() { return changed; } };
}

test('Codex uses native device sign-in, then explicit catalog consent; closing cancels its own job', async () => {
  const h = view({ id: 'codex', name: 'Codex' });
  await button(h.render(), 'Se connecter avec ChatGPT').props.onClick();
  assert.equal(h.posts[0].url, '/api/runtimes/codex/account');
  assert.equal(h.posts[0].body.consent, true);
  h.render(); await h.timers.shift()();
  const waiting = h.render();
  const link = flatten(waiting).find(x => x.type === 'a');
  assert.equal(link.props.href, 'https://auth.openai.com/codex/device');
  assert.equal(link.props.rel, 'noopener noreferrer');
  assert.match(text(waiting), /ABCD-1234/);
  assert.equal(button(waiting.props.foot, 'Vérifier et connecter à Loom').props.disabled, true);
  h.env.get = async () => ({ ok: true, login: { id: 'fixture-login', state: 'completed' } });
  await h.timers.shift()();
  const completed = h.render();
  assert.ok(!text(completed).includes('ABCD-1234'));
  await button(completed.props.foot, 'Vérifier et connecter à Loom').props.onClick();
  assert.equal(h.connected, 1); assert.equal(h.closed, 1);
  assert.ok(h.posts.some(x => x.body.job === 'fixture-login' && x.body.cancel));
  assert.ok(!h.posts.some(x => /thread|prompt|turn|token/.test(x.url)));
  h.unmount();
});

test('Unsupported browser login stays in its modal and cleans up its terminal without changing the route', async () => {
  const h = view({ id: 'pi', name: 'Pi' });
  await button(h.render(), 'Utiliser la connexion native').props.onClick();
  const tree = h.render();
  assert.equal(h.posts[0].url, '/api/terminals');
  assert.equal(h.posts[0].body.command, 'pi');
  assert.ok(flatten(tree).some(x => x.type === 'TermView'));
  assert.match(text(tree), /instructions de connexion/);
  assert.equal(h.connected, 0);
  h.unmount();
  await new Promise(resolve => setImmediate(resolve));
  assert.ok(h.posts.some(x => x.url === '/api/terminals/close' && x.body.id === 'fixture-terminal'));
});

test('a login response arriving after modal unmount is cancelled rather than orphaned', async () => {
  const h = view({ id: 'codex', name: 'Codex' });
  let resolve;
  const normal = h.env.post;
  h.env.post = (url, body) => body.consent ? new Promise(r => { resolve = r; }) : normal(url, body);
  const starting = button(h.render(), 'Se connecter avec ChatGPT').props.onClick();
  await new Promise(resolve => setImmediate(resolve));
  h.unmount();
  resolve({ ok: true, login: { id: 'late-fixture', state: 'starting' } });
  await starting;
  assert.ok(h.posts.some(x => x.body.job === 'late-fixture' && x.body.cancel));
});

test('control-plane identity is polled independently from a linked engine status', async () => {
  const stateSource = fs.readFileSync(new URL('../next/js/core/state.js', import.meta.url), 'utf8');
  let state;
  const env = { validShape, validData, singleFlight, t: x => x, createStore: value => ({ get: () => state, set: patch => Object.assign(state, patch) }),
    localStorage: { getItem: () => null }, location: { hash: '#/harnesses' }, innerWidth: 1200, addEventListener() {},
    get: async url => url === '/api/status' ? { active: false, version: '0.1.4', hostname: 'engine-fixture' } : { version: '0.2.1', hostname: 'control-fixture' },
  };
  vm.runInNewContext(stateSource.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.refresh = refreshStatus;', env);
  state = { unavailable: {} };
  await env.refresh();
  assert.equal(state.status.version, '0.1.4');
  assert.equal(state.serverInfo.version, '0.2.1');
  assert.equal(state.serverInfo.hostname, 'control-fixture');
});

test('sidebar renders control-plane version, name and avatar independently from its engine', () => {
  const shell = fs.readFileSync(new URL('../next/js/app/shell.js', import.meta.url), 'utf8');
  const app = {}, chat = {};
  const state = { route: { section: 'harnesses' }, nav: { conversations: [], projects: [] },
    status: { hostname: 'engine-fixture', version: '0.1.4' }, serverInfo: { hostname: 'control-fixture', version: '0.2.1' } };
  const env = { html, t: french, app, chat, cls: (...a) => a.filter(Boolean).join(' '), Icon: 'Icon', Menu: 'Menu', shortVersion: v => String(v || ''), Activity: 'Activity', NAV_ITEMS: [],
    localStorage: { getItem: () => null }, useStore: (store, select) => select(store === app ? state : { sessionId: '' }),
    useState: value => [typeof value === 'function' ? value() : value, () => {}], useEffect() {}, openPalette() {},
  };
  vm.runInNewContext(shell.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.sidebar = Sidebar;', env);
  const tree = env.sidebar();
  assert.match(text(tree), /control-fixtureLoom 0\.2\.1/);
  assert.ok(!text(tree).includes('0.1.4'));
  assert.equal(text(flatten(tree).find(x => x.props.class === 'avatar-i')), 'C');
});

for (const [rt, label] of [[{ id: 'claude-code', name: 'Claude' }, 'Se connecter avec Claude'], [{ id: 'antigravity', name: 'Antigravity' }, 'Se connecter avec Google']]) {
  test(label + ' uses browser link and code input without a terminal', async () => {
    const h = view(rt);
    await button(h.render(), label).props.onClick();
    h.env.get = async () => ({ ok: true, login: { id: 'fixture-login', state: 'waiting', url: 'https://claude.ai/oauth/authorize?state=fixture&client_id=fixture', input_required: true } });
    h.render(); await h.timers.shift()();
    let tree = h.render();
    assert.ok(flatten(tree).some(x => x.type === 'a'));
    const input = flatten(tree).find(x => x.type === 'input');
    assert.equal(input.props.type, 'password');
    input.props.onInput({ target: { value: 'TEST-CODE' } });
    await button(h.render(), 'Valider le code').props.onClick();
    assert.ok(h.posts.some(x => x.body.job === 'fixture-login' && x.body.code === 'TEST-CODE'));
    assert.ok(!h.posts.some(x => x.url === '/api/terminals'));
    assert.equal(flatten(h.render()).find(x => x.type === 'input')?.props.value || '', '');
    h.unmount();
  });
}
