import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
import { Config } from '../next/js/features/inspector/config.js';

// Exécute les composants réels avec des hooks déterministes et des API simulées.
// Aucun socket, secret réel, service système ni permission navigateur.
const settings = fs.readFileSync(new URL('../next/js/features/settings/page.js', import.meta.url), 'utf8');
const local = fs.readFileSync(new URL('../next/js/features/local/page.js', import.meta.url), 'utf8');
const vnode = (type, props, ...children) => ({ type, props: { ...props, children } });
const html = htm.bind(vnode);
const flatten = tree => Array.isArray(tree) ? tree.flatMap(flatten) : tree && typeof tree === 'object'
  ? [tree, ...flatten(tree.props?.children), ...flatten(tree.props?.foot)] : [];
const textOf = tree => Array.isArray(tree) ? tree.map(textOf).join('') : tree && typeof tree === 'object'
  ? textOf(tree.props?.children) + textOf(tree.props?.foot) : typeof tree === 'string' ? tree : '';
const button = (tree, label) => flatten(tree).find(n => n.type === 'button' && textOf(n) === label);
const nodes = (tree, type) => flatten(tree).filter(n => n.type === type);
const settle = async () => { for (let i = 0; i < 5; i++) await new Promise(resolve => setImmediate(resolve)); };

function harness(source, view, overrides = {}) {
  const slots = [], effects = [], posts = [], notices = [], confirmations = [], refreshed = [];
  let cursor = 0, tree, currentProps = {};
  const data = {
    '/api/apikey': { set: false, required: false },
    '/api/mem/health': { encrypted: false, fully: false, locked: false, vault_copies: 0 },
    '/api/memory': { mode: 'off' },
    '/api/mem/snapshots': { ok: true, snapshots: [] },
  };
  const env = {
    html, Config, cls: (...s) => s.filter(Boolean).join(' '), fmtBytes: b => b + ' octets',
    Switch: 'Switch', Tip: 'Tip', Modal: 'Modal', Seg: 'Seg', Icon: 'Icon', Menu: 'Menu',
    Logo: 'Logo', Drawer: 'Drawer', ParamsEditor: 'ParamsEditor', Tabs: 'Tabs', Empty: 'Empty', Hub: 'Hub',
    inspectTrigger: () => ({}), baseName: p => (p || '').split('/').pop(),
    useState(initial) {
      const i = cursor++;
      if (!(i in slots)) slots[i] = typeof initial === 'function' ? initial() : initial;
      return [slots[i], v => { slots[i] = typeof v === 'function' ? v(slots[i]) : v; }];
    },
    useRef(initial) { const i = cursor++; return slots[i] ||= { current: initial }; },
    useEffect(fn, deps) {
      const i = cursor++, old = slots[i];
      if (!old || deps.some((v, j) => !Object.is(v, old[j]))) { slots[i] = deps; effects.push(fn); }
    },
    useStore: (_store, select) => select(env.state),
    state: { status: null, models: [], presets: [] }, app: {},
    navigator: { userAgent: 'Synthetic browser' }, window: { isSecureContext: false },
    Notification: { permission: 'default' }, atob, Uint8Array,
    get: async url => data[url],
    post: async (...args) => { posts.push(args); return { ok: true }; },
    toast: (...args) => notices.push(args),
    confirm: async (...args) => { confirmations.push(args); return env.accept; }, accept: true,
    refreshLibrary: async () => refreshed.push('library'), refreshStatus: () => refreshed.push('status'), refreshNav: () => refreshed.push('nav'),
    setTimeout: () => {}, prompt: async () => null, go: () => {},
    liveSource: async () => env.src,
    ...overrides,
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = ' + view + ';', env);
  const render = (props = currentProps) => { currentProps = props; cursor = 0; tree = env.view(props); effects.splice(0).forEach(fn => fn()); return tree; };
  return { env, data, posts, notices, confirmations, refreshed, render, async ready(props) { render(props); await settle(); return render(props); } };
}

function health(h, value) { h.data['/api/mem/health'] = value; }
const encrypted = locked => ({ encrypted: true, fully: true, locked, vault_copies: 3 });

test('vault unlock accepts a masked password or recovery secret, then exposes lock/decrypt/addkey', async () => {
  for (const secret of ['synthetic-password', 'SYNTHETIC-RECOVERY']) {
    const h = harness(settings, 'Security'); health(h, encrypted(true));
    button(await h.ready(), 'Déverrouiller').props.onClick();
    const form = flatten(h.render()).find(n => n.type?.name === 'SecretDialog');
    assert.match(form.props.tip, /Mot de passe ou clé de récupération/);
    h.env.post = async (...args) => { h.posts.push(args); health(h, encrypted(false)); return { ok: true }; };
    await form.props.onSubmit(secret);
    assert.equal(h.posts[0][0], '/api/mem/unlock');
    assert.equal(h.posts[0][1].secret, secret);
    assert.equal(h.posts[0][2].retryAuth, false);
    const open = h.render();
    for (const label of ['Verrouiller', 'Déchiffrer', 'Ajouter une clé']) assert.ok(button(open, label));
    assert.ok(!JSON.stringify(h.notices).includes(secret));
    assert.ok(!textOf(open).includes(secret));
  }
  const s = harness(settings, 'SecretDialog');
  let submitted;
  const props = { title: 'Secret', onSubmit: v => submitted = v };
  assert.equal(nodes(s.render(props), 'input')[0].props.type, 'password');
  nodes(s.render(props), 'input')[0].props.onInput({ target: { value: 'synthetic-password' } });
  button(s.render(props), 'Continuer').props.onClick();
  assert.equal(submitted, 'synthetic-password');
  assert.equal(nodes(s.render(props), 'input')[0].props.value, '');
});

test('encryption shows recovery only in its one-time modal and discards it on close', async () => {
  const h = harness(settings, 'Security');
  button(await h.ready(), 'Activer').props.onClick();
  const form = flatten(h.render()).find(n => n.type?.name === 'SecretDialog');
  h.env.post = async (...args) => { h.posts.push(args); health(h, encrypted(false)); return { ok: true, recovery: 'SYNTHETIC-ONCE' }; };
  await form.props.onSubmit('synthetic-password');
  assert.equal(h.posts[0][0], '/api/mem/encrypt');
  assert.equal(h.posts[0][1].password, 'synthetic-password');
  assert.equal(h.confirmations.length, 1);
  assert.equal(h.confirmations[0][0], 'Clé de récupération');
  assert.match(h.confirmations[0][1], /SYNTHETIC-ONCE/);
  assert.ok(!JSON.stringify(h.notices).includes('SYNTHETIC-ONCE'));
  assert.ok(!textOf(h.render()).includes('SYNTHETIC-ONCE'));
  button(h.render(), 'Actualiser').props.onClick(); await settle(); h.render();
  assert.equal(h.confirmations.length, 1, 'health refresh cannot redisplay recovery');
});

test('lock/addkey payloads, failed unlock, partial encryption and lost flag remain honest', async () => {
  const h = harness(settings, 'Security'); health(h, encrypted(false));
  button(await h.ready(), 'Ajouter une clé').props.onClick();
  await flatten(h.render()).find(n => n.type?.name === 'SecretDialog').props.onSubmit('synthetic-wrap');
  assert.equal(h.posts[0][0], '/api/mem/addkey'); assert.equal(h.posts[0][1].secret, 'synthetic-wrap');
  h.env.post = async (...args) => { h.posts.push(args); health(h, encrypted(true)); return { ok: true }; };
  await button(h.render(), 'Verrouiller').props.onClick();
  assert.equal(h.posts[1][0], '/api/mem/lock'); assert.ok(button(h.render(), 'Déverrouiller'));
  h.env.post = async () => ({ ok: false, status: 401, error: 'synthetic-secret-echo' });
  button(h.render(), 'Déverrouiller').props.onClick();
  await flatten(h.render()).find(n => n.type?.name === 'SecretDialog').props.onSubmit('bad-secret');
  assert.ok(button(h.render(), 'Déverrouiller')); assert.ok(!button(h.render(), 'Déchiffrer'));
  assert.ok(!JSON.stringify(h.notices).includes('synthetic-secret-echo'));
  for (const mem of [{ ...encrypted(false), fully: false }, { encrypted: false, locked: false, vault_copies: 2 }]) {
    const p = harness(settings, 'Security'); health(p, mem);
    const t = await p.ready(); assert.ok(button(t, 'Déverrouiller')); assert.ok(!button(t, 'Activer'));
    assert.ok(!nodes(t, 'span').some(n => n.props.class === 'tag green'));
  }
});

test('decrypt and snapshot restore require consent; restore uses id and refreshes state', async () => {
  const h = harness(settings, 'Security'); health(h, encrypted(false));
  h.data['/api/mem/snapshots'].snapshots = [{ id: 'synthetic-snapshot', when: '2026-09-30T10:00:00Z', pages: 3 }];
  let t = await h.ready(); assert.match(textOf(t), /Taille inconnue/);
  h.env.accept = false;
  await button(t, 'Déchiffrer').props.onClick(); await button(h.render(), 'Restaurer').props.onClick();
  assert.equal(h.posts.length, 0);
  h.env.accept = true;
  await button(h.render(), 'Déchiffrer').props.onClick();
  await button(h.render(), 'Restaurer').props.onClick();
  assert.equal(h.posts[0][0], '/api/mem/decrypt');
  assert.equal(h.posts[1][0], '/api/mem/snapshots'); assert.equal(h.posts[1][1].id, 'synthetic-snapshot');
  assert.ok(h.confirmations.every(c => c[2].danger));
  assert.ok(h.refreshed.includes('library'));
  const failed = harness(settings, 'Security', { get: async () => { throw new Error(); } });
  assert.match(textOf(await failed.ready()), /État inconnu/); assert.ok(!button(failed.render(), 'Activer'));
});

function pushFixture() {
  let sub = null;
  const events = [], manager = {
    getSubscription: async () => sub,
    subscribe: async opts => { events.push(['subscribe', opts]); sub = { endpoint: 'https://push.invalid/synthetic', toJSON: () => ({ endpoint: sub.endpoint, keys: { auth: 'synthetic' } }), unsubscribe: async () => { events.push(['unsubscribe']); sub = null; return true; } }; return sub; },
  };
  const h = harness(settings, 'PushNotifications', {
    window: { isSecureContext: true, PushManager: {}, Notification: {} },
    navigator: { userAgent: 'Synthetic browser', serviceWorker: { register: async (...args) => { events.push(['register', ...args]); return { pushManager: manager }; } } },
    Notification: { permission: 'default', requestPermission: async () => { events.push(['permission']); return 'granted'; } },
    get: async () => ({ ok: true, key: 'AQID-_8' }),
  });
  return { h, events };
}

test('push uses user permission, root service worker, VAPID bytes and subscription/unsubscription payloads', async () => {
  const { h, events } = pushFixture();
  await h.ready(); assert.ok(!events.some(e => e[0] === 'permission'));
  events.length = 0;
  await nodes(h.render(), 'Switch')[0].props.onChange(true);
  assert.equal(events[0][0], 'permission');
  assert.equal(events[1][1], '/sw.js'); assert.equal(events[1][2].scope, '/');
  const opts = events.find(e => e[0] === 'subscribe')[1];
  assert.equal(opts.userVisibleOnly, true); assert.deepEqual([...opts.applicationServerKey], [1, 2, 3, 251, 255]);
  assert.equal(h.posts[0][0], '/api/push/subscribe'); assert.equal(h.posts[0][1].keys.auth, 'synthetic');
  assert.equal(nodes(h.render(), 'Switch')[0].props.checked, true);
  await nodes(h.render(), 'Switch')[0].props.onChange(false);
  assert.equal(h.posts[1][0], '/api/push/unsubscribe'); assert.match(h.posts[1][1].endpoint, /synthetic/);
  assert.equal(nodes(h.render(), 'Switch')[0].props.checked, false);
});

test('unsupported/insecure push is disabled; permission and registration failures never claim success', async () => {
  for (const window of [{ isSecureContext: false }, { isSecureContext: true }]) {
    const h = harness(settings, 'PushNotifications', { window });
    const t = await h.ready(); assert.equal(nodes(t, 'Switch')[0].props.disabled, true);
    assert.ok(flatten(t).find(n => n.type?.name === 'Line').props.tip); assert.equal(h.posts.length, 0);
  }
  const { h, events } = pushFixture(); await h.ready();
  h.env.Notification.requestPermission = async () => 'denied';
  await nodes(h.render(), 'Switch')[0].props.onChange(true);
  assert.equal(h.posts.length, 0); assert.equal(nodes(h.render(), 'Switch')[0].props.checked, false);
  h.env.Notification.requestPermission = async () => 'granted';
  h.env.post = async () => ({ ok: false });
  await nodes(h.render(), 'Switch')[0].props.onChange(true);
  assert.ok(events.some(e => e[0] === 'unsubscribe'));
  assert.equal(nodes(h.render(), 'Switch')[0].props.checked, false);
  assert.ok(h.notices.every(n => !n[0].includes('activées')));
});

const gpu = [{ id: 'Vulkan0', name: 'GPU A', total_mib: 0 }, { id: 'Vulkan1', name: 'GPU B', total_mib: 8192 }];
function gpuHarness(presetId = 'preset') {
  const h = harness(settings, 'GpuDevices');
  h.env.src = { model: '/synthetic.gguf', presetId, presetName: 'Synthetic', base: 'MODEL=/synthetic.gguf\nBIN=/synthetic/llama-server\nEXTRA_ARGS="--jinja --tensor-split 0.5,0.5"\n' };
  h.env.post = async (...args) => { h.posts.push(args); return args[0] === '/api/backends/devices' ? { ok: true, devices: gpu } : { ok: true }; };
  return h;
}

test('GPU selection saves the preset or remembered model, preserves other flags and applies exact backend ids', async () => {
  for (const presetId of ['preset', '']) {
    const h = gpuHarness(presetId); await h.ready({ bin: '/fallback/llama-server' });
    assert.equal(h.posts[0][1].bin, '/synthetic/llama-server');
    assert.match(textOf(h.render()), /Mémoire inconnue/);
    await nodes(h.render(), 'Switch')[0].props.onChange(false);
    const save = h.posts[1];
    assert.equal(save[0], presetId ? '/api/preset/save' : '/api/naked/remember');
    const cfg = new Config(save[1].content);
    assert.equal(cfg.arg('--device'), 'Vulkan1'); assert.equal(cfg.arg('--tensor-split'), ''); assert.ok(cfg.has('--jinja'));
    assert.equal(h.posts[2][0], '/api/apply'); assert.equal(h.posts[2][1].preset_id, presetId);
    h.env.src = { ...h.env.src, base: cfg.text };
    await nodes(h.render(), 'Switch')[0].props.onChange(true);
    assert.equal(new Config(h.posts[3][1].content).arg('--device'), '', 'all devices removes the constraint');
    assert.ok(h.refreshed.includes('library'));
  }
});

test('unknown/single GPU stays read-only; last device and cancelled choice never save', async () => {
  for (const devices of [null, [], [gpu[0]]]) {
    const h = gpuHarness(); h.env.post = async () => ({ ok: !!devices, devices });
    assert.equal(nodes(await h.ready(), 'Switch').length, 0);
  }
  const h = gpuHarness(); h.env.src.base += 'EXTRA=keep\n'; h.env.src.base = new Config(h.env.src.base).setArg('--device', 'Vulkan1').text;
  await h.ready();
  await nodes(h.render(), 'Switch')[1].props.onChange(false);
  assert.equal(h.posts.length, 1);
  h.env.accept = false; await nodes(h.render(), 'Switch')[0].props.onChange(true); assert.equal(h.posts.length, 1);
  h.env.src.base = new Config(h.env.src.base).setArg('--device', 'Unknown9').text;
  const u = gpuHarness(); u.env.src = h.env.src;
  assert.equal(nodes(await u.ready(), 'Switch').length, 0); assert.match(textOf(u.render()), /Choix inconnu/);
});

test('preset moves preserve the complete filtered catalog, boundaries and failed saves', async () => {
  const h = harness(local, 'Library');
  h.env.state.presets = ['a', 'b', 'c'].map(id => ({ id, name: id }));
  let t = await h.ready();
  assert.equal(button(t, 'Monter').props.disabled, true);
  assert.equal(nodes(t, 'button').filter(n => textOf(n) === 'Descendre').at(-1).props.disabled, true);
  nodes(t, 'input')[0].props.onInput({ target: { value: 'b' } });
  t = h.render(); assert.equal(nodes(t, 'button').filter(n => textOf(n) === 'Monter').length, 1);
  await button(t, 'Monter').props.onClick();
  assert.equal(h.posts[0][0], '/api/presets/order'); assert.deepEqual([...h.posts[0][1].ids], ['b', 'a', 'c']);
  const refreshes = h.refreshed.length;
  h.env.post = async () => ({ ok: false }); await button(h.render(), 'Descendre').props.onClick();
  assert.equal(h.refreshed.length, refreshes); assert.ok(h.notices.at(-1)[0].includes('non enregistré'));
});

test('vault 401 skips API authentication retry; ordinary API requests retain it', async () => {
  const source = fs.readFileSync(new URL('../next/js/core/api.js', import.meta.url), 'utf8');
  const calls = [], asks = [];
  const env = { localStorage: { getItem: () => 'synthetic-access', setItem() {} }, AbortController,
    setTimeout: () => 1, clearTimeout() {},
    ask: async spec => { asks.push(spec); return 'synthetic-new-access'; },
    fetch: async (url, opts) => { calls.push([url, opts]); return { status: 401, ok: false, json: async () => ({ ok: false }) }; },
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.api = {post};', env);
  await env.api.post('/api/mem/unlock', { secret: 'synthetic-secret' }, { retryAuth: false });
  assert.equal(asks.length, 0); assert.equal(calls.length, 1);
  assert.equal(calls[0][1].headers.Authorization, 'Bearer synthetic-access');
  assert.ok(!('retryAuth' in calls[0][1]));
  await env.api.post('/api/other', {});
  assert.equal(asks.length, 1); assert.equal(calls.length, 3);
});
