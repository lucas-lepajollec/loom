import { validData, shapePatch } from '../next/js/core/shape.js';
import { french } from './i18n-fixture.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
import { Config } from '../next/js/features/inspector/config.js';

// Exécute les composants réels avec des hooks déterministes et des API simulées.
// Aucun socket, secret réel, service système ni permission navigateur.
// Les briques Line/Group vivent dans kit.js : chargées avec la page.
const settings = fs.readFileSync(new URL('../next/js/features/settings/kit.js', import.meta.url), 'utf8')
  + fs.readFileSync(new URL('../next/js/features/settings/page.js', import.meta.url), 'utf8');
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
    t: french, locale: () => 'fr-FR', getLang: () => 'fr',
    html, Config, cls: (...s) => s.filter(Boolean).join(' '), fmtBytes: b => b + ' octets',
    Switch: 'Switch', Tip: 'Tip', Modal: 'Modal', Seg: 'Seg', Icon: 'Icon', Menu: 'Menu', shortVersion: v => String(v || ''),
    Logo: 'Logo', Drawer: 'Drawer', ParamsEditor: 'ParamsEditor', Tabs: 'Tabs', Empty: 'Empty', Hub: 'Hub', StartupSettings: 'StartupSettings', HttpsSettings: 'HttpsSettings', NotificationSettings: 'NotificationSettings', PolicySettings: 'PolicySettings', DoctorSettings: 'DoctorSettings', HarnessHistory: 'HarnessHistory', Lifecycle: 'Lifecycle',
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
    state: { status: null, models: [], presets: [] }, app: { get: () => ({}), set: () => {} },
    vendorOf: () => 'Autres', Logo: 'Logo', Modal: 'Modal',
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
  const removed = [];
  const env = { validData, clearObservations() {}, t: french, localStorage: { getItem: () => 'synthetic-access', removeItem: key => removed.push(key) }, AbortController,
    setTimeout: () => 1, clearTimeout() {},
    ask: async spec => { asks.push(spec); return 'synthetic-new-access'; },
    fetch: async (url, opts) => {
      calls.push([url, opts]);
      if (url === '/api/auth/status') return { status: 200, ok: true, json: async () => ({ password_set: true }) };
      if (url === '/api/auth/login') return { status: 200, ok: true, json: async () => ({ ok: true }) };
      return { status: 401, ok: false, json: async () => ({ ok: false }) };
    },
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.api = {post};', env);
  await env.api.post('/api/mem/unlock', { secret: 'synthetic-secret' }, { retryAuth: false });
  assert.equal(asks.length, 0); assert.equal(calls.length, 1);
  assert.equal(calls[0][1].headers.Authorization, 'Bearer synthetic-access');
  assert.ok(!('retryAuth' in calls[0][1]));
  await env.api.post('/api/other', {});
  assert.equal(asks.length, 1); assert.equal(calls.length, 5);
  assert.equal(asks[0].input.type, 'password');
  assert.equal(calls[3][0], '/api/auth/login');
  assert.equal(JSON.parse(calls[3][1].body).password, 'synthetic-new-access');
  assert.ok(!('Authorization' in calls[4][1].headers));
  assert.ok(removed.includes('loom.key'));
});

test('language selector applies immediately and preserves a system prompt draft', async () => {
  let lang = 'fr'; const choices = [];
  const dictionaries = { fr: (await import('../next/js/i18n/fr.js')).default, en: (await import('../next/js/i18n/en.js')).default };
  const h = harness(settings, 'General', {
    t: key => dictionaries[lang][key], getLang: () => lang,
    setLang: async value => { lang = value; choices.push(value); return true; },
    get: async url => url === '/api/prefs' ? { prefs: { lang: 'fr' } } : url === '/api/sysprompt' ? { text: 'Original prompt' } : { compact: true },
    state: { theme: 'dark' },
  });
  let tree = await h.ready();
  assert.equal(nodes(tree, 'select')[0].props.value, 'fr');
  assert.deepEqual(nodes(tree, 'option').map(n => textOf(n)), ['Français', 'English']);
  nodes(tree, 'textarea')[0].props.onInput({ target: { value: 'Unsaved prompt' } });
  await nodes(h.render(), 'select')[0].props.onChange({ target: { value: 'en' } });
  tree = h.render();
  assert.equal(nodes(tree, 'select')[0].props.value, 'en');
  assert.equal(nodes(tree, 'textarea')[0].props.value, 'Unsaved prompt');
  assert.ok(button(tree, 'Save'));
  assert.deepEqual(choices, ['en']);
  const switchNode = nodes(tree, 'Switch').find(n => n.props.checked === false);
  await switchNode.props.onChange(true);
  assert.deepEqual(Object.keys(h.posts.at(-1)[1]), ['hide_reasoning']);
});

const updates = fs.readFileSync(new URL('../next/js/features/settings/kit.js', import.meta.url), 'utf8')
  + fs.readFileSync(new URL('../next/js/features/settings/updates.js', import.meta.url), 'utf8');

test('Loom update checks official releases, requires consent and sends reviewed version', async () => {
  const h = harness(updates, 'LoomUpdates', { location: { reload() {} } });
  h.data['/api/update'] = { current: '0.1.1', latest: '0.1.2', available: true, can_apply: true };
  await button(h.render(), 'Vérifier').props.onClick();
  assert.ok(textOf(h.render()).includes('0.1.2'));
  h.env.accept = false;
  await button(h.render(), 'Installer la mise à jour').props.onClick();
  assert.equal(h.posts.length, 0);
  h.env.accept = true;
  h.env.post = async (...args) => { h.posts.push(args); return { ok: true, version: '0.1.2', restarting: false, restart: 'Restart manually' }; };
  await button(h.render(), 'Installer la mise à jour').props.onClick();
  assert.equal(h.posts[0][0], '/api/update/apply');
  assert.equal(h.posts[0][1].version, '0.1.2');
  assert.equal(h.posts[0][2].timeout, 8 * 60 * 1000);
  assert.ok(textOf(h.render()).includes('Redémarre Loom'));
});

test('Loom update failures are not shown as up to date; blocked installations explain setup', async () => {
  const h = harness(updates, 'LoomUpdates');
  h.data['/api/update'] = { error: 'GitHub unavailable' };
  await button(h.render(), 'Vérifier').props.onClick();
  assert.ok(textOf(h.render()).includes('GitHub unavailable'));
  assert.ok(!textOf(h.render()).includes('À jour'));
  h.data['/api/update'] = { latest: '0.1.2', available: true, can_apply: false, apply_reason: 'One-time setup required' };
  await button(h.render(), 'Vérifier').props.onClick();
  assert.equal(button(h.render(), 'Installer la mise à jour').props.disabled, true);
  assert.ok(textOf(h.render()).includes('One-time setup required'));
  h.data['/api/update'].can_apply = true;
  await button(h.render(), 'Vérifier').props.onClick();
  h.env.post = async () => ({ ok: false, error: 'Checksum failed' });
  await button(h.render(), 'Installer la mise à jour').props.onClick();
  assert.ok(textOf(h.render()).includes('Checksum failed'));
  assert.ok(!textOf(h.render()).includes('Mise à jour installée'));
});

test('reconnect accepts only the new running version, with bounded failures', async () => {
  const env = {};
  vm.runInNewContext(updates.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.wait = waitForUpdatedVersion;', env);
  let reads = 0;
  assert.equal(await env.wait('0.1.2', async () => ({ version: ++reads === 3 ? '0.1.2' : '0.1.1' }), async () => {}), true);
  assert.equal(reads, 3);
  reads = 0;
  assert.equal(await env.wait('0.1.2', async () => { reads++; throw new Error('offline'); }, async () => {}), false);
  assert.equal(reads, 30);
  assert.equal(await env.wait('0.1.2', async () => { throw new Error('should not read'); }, async () => {}, () => false), false);
});

const machines = fs.readFileSync(new URL('../next/js/features/harnesses/machines.js', import.meta.url), 'utf8');
test('SSH machine can finish with no harnesses, including editing a machine without a harness list', async () => {
  for (const editing of [false, true]) {
    let closed;
    const checked = { ok: true, machine: { id: 'gpu', hostname: 'GPU', host: 'fixture', os: 'Linux', home: '/home/fixture' }, offers: [
      { id: 'codex', installed: true, ready: true, name: 'Codex' },
      { id: 'claude-code', installed: true, ready: true, name: 'Claude Code' },
    ] };
    const h = harness(machines, 'MachineDialog', {
      refreshWorkspace: async () => {}, copyText: async () => true,
      post: async (url, body) => { h.posts.push([url, body]); return body.check_only ? checked : { ok: true, machine: { ...body.machine, harnesses: [] } }; },
    });
    h.data['/api/machines'] = { ok: true, setup: 'synthetic setup' };
    const props = { machine: editing ? { id: 'gpu', name: 'GPU', host: 'fixture', user: 'fixture' } : null, onClose: value => closed = value };
    let tree = await h.ready(props);
    if (!editing) {
      const inputs = nodes(tree, 'input');
      inputs[1].props.onInput({ target: { value: 'fixture' } }); tree = h.render();
      nodes(tree, 'input')[2].props.onInput({ target: { value: 'fixture' } }); tree = h.render();
    }
    assert.equal(button(tree, 'Tester la connexion').props.disabled, false);
    await button(tree, 'Tester la connexion').props.onClick(); tree = h.render();
    for (;;) {
      const selected = nodes(tree, 'input').find(n => n.props.type === 'checkbox' && n.props.checked);
      if (!selected) break;
      selected.props.onChange(); tree = h.render();
    }
    const save = button(tree, 'Enregistrer la machine');
    assert.equal(save.props.disabled, false);
    await save.props.onClick();
    assert.equal(h.posts.at(-1)[0], '/api/machines');
    assert.deepEqual(Array.from(h.posts.at(-1)[1].harnesses), []);
    assert.equal(closed.id, editing ? 'gpu' : '');
  }
});

test('node updates target only the node and warn about stopping engines', async () => {
  const h = harness(updates, 'LoomUpdates');
  h.data['/api/engine/node/update'] = { current: '0.1.2', latest: '0.1.3', available: true, can_apply: true };
  await button(h.render({ node: true }), 'Vérifier').props.onClick();
  await button(h.render(), 'Installer la mise à jour').props.onClick();
  assert.equal(h.posts[0][0], '/api/engine/node/update/apply');
  assert.match(h.confirmations[0][1], /arrêtera ses moteurs/);
});

test('node server view uses its protected endpoint without an ineffective network toggle', async () => {
  const h = harness(local, 'EngineRuntime', { setInterval: () => 1, clearInterval: () => {}, engineState: () => ({ tone: 'muted' }) });
  h.data['/api/server'] = { node_managed: true, key_required: true, url: 'http://fixture:2511/v1', slots: { items: [] }, stats: {} };
  const tree = await h.ready();
  assert.equal(nodes(tree, 'Switch').length, 0);
  assert.match(textOf(tree), /2511\/v1/);
});


test('each saved machine node updates through its own endpoint, independent of the active engine', async () => {
  for (const id of ['gpu-a', 'gpu-b']) {
    const endpoint = '/api/machines/' + id + '/node/update';
    const h = harness(updates, 'LoomUpdates');
    h.data[endpoint] = { current: '0.1.4', latest: '0.2.1', available: true, can_apply: true };
    h.env.post = async (...args) => { h.posts.push(args); return { ok: true, version: '0.2.1', restarting: false }; };
    await button(h.render({ node: true, endpoint }), 'Vérifier').props.onClick();
    await button(h.render({ node: true, endpoint }), 'Installer la mise à jour').props.onClick();
    assert.equal(h.posts[0][0], endpoint + '/apply');
    assert.match(h.confirmations[0][1], /moteurs/);
    assert.equal(h.posts.length, 1);
  }
});

test('mobile Settings opens its index, follows section links and returns without mounting hidden forms', () => {
  let media = { matches: true, addEventListener: (_name, fn) => media.change = fn, removeEventListener: () => {} };
  const h = harness(settings, 'SettingsPage', { window: { matchMedia: () => media }, WorkspaceManager: 'WorkspaceManager', MachinesSettings: 'MachinesSettings', StartupSettings: 'StartupSettings', NotificationSettings: 'NotificationSettings', PolicySettings: 'PolicySettings', DoctorSettings: 'DoctorSettings' });
  let tree = h.render({ route: { sub: '' } });
  assert.equal(nodes(tree, 'nav').length, 1);
  assert.equal(flatten(tree).filter(n => n.props?.class === 'set-body').length, 0);
  const links = nodes(tree, 'a');
  assert.equal(links.length, 8);
  assert.equal(links.find(n => textOf(n).includes('Démarrage')).props.href, '#/settings/startup');
  for (const moved of ['Machines', 'Espaces de travail', 'Moteurs']) assert.ok(!links.some(n => textOf(n).includes(moved)), moved + ' lives on its domain page, not in Settings');
  assert.equal(links.find(n => textOf(n).includes('Internet')).props.href, '#/settings/internet');
  tree = h.render({ route: { sub: 'internet' } });
  assert.equal(nodes(tree, 'nav').length, 0);
  assert.equal(nodes(tree, 'a')[0].props.href, '#/settings');
  assert.equal(nodes(tree, 'h1').map(textOf).join(''), 'Internet');
  tree = h.render({ route: { sub: 'unknown' } });
  assert.equal(nodes(tree, 'nav').length, 1);
  assert.equal(flatten(tree).filter(n => n.props?.class === 'set-body').length, 0);
  media.matches = false; media.change();
  tree = h.render({ route: { sub: 'internet' } });
  assert.equal(nodes(tree, 'nav').length, 1);
  assert.equal(nodes(tree, 'h1').map(textOf).join(''), 'Réglages');
});


test('machine agents: managing is separate from using, enabling asks for consent', async () => {
  const source = fs.readFileSync(new URL('../next/js/features/settings/kit.js', import.meta.url), 'utf8') + fs.readFileSync(new URL('../next/js/features/settings/machines.js', import.meta.url), 'utf8');
  const h = harness(source, 'HarnessesSection', { state: { workspace: { runtimes: [] } }, refreshWorkspace: async () => {} });
  h.data['/api/agents/installations'] = { ok: true, installations: [
    { machine: 'fixture-remote', machine_name: 'Synthetic remote', harness: 'codex', name: 'Codex', runtime_id: 'custom-fixture-remote-codex', installed: true, ready: true, managed: false, enabled: false },
    { machine: 'other', harness: 'pi', name: 'Pi', installed: true, ready: true, managed: true, enabled: true },
  ] };
  const tree = await h.ready({ m: { id: 'fixture-remote', name: 'Synthetic remote' }, onChange: () => {} });
  const switches = flatten(tree).filter(n => n.type === 'Switch');
  assert.equal(switches.length, 2, 'only this machine is listed, with manage and use');
  assert.equal(switches[0].props.checked, false);
  await switches[0].props.onChange(true);
  assert.equal(JSON.stringify(h.posts.at(-1).slice(0, 2)), JSON.stringify(['/api/agents/installations', { machine: 'fixture-remote', harness: 'codex', managed: true, consent: false }]));
  h.env.accept = false;
  await switches[1].props.onChange(true);
  assert.equal(h.confirmations.length, 1);
  assert.equal(h.posts.length, 1, 'using an agent without consent posts nothing');
});

const nodeMachines = fs.readFileSync(new URL('../next/js/features/settings/machines.js', import.meta.url), 'utf8');
const nodeMachineEnv = { setInterval: () => 1, clearInterval: () => {}, refreshEngineNode: async () => {}, refreshWorkspace: async () => {}, copyText: async () => true, MachineDialog: 'MachineDialog', ModelDirs: 'ModelDirs', DirectEngineForm: 'DirectEngineForm', LoomUpdates: 'LoomUpdates' };

test('Machines has one Add a machine card and keeps SSH edit off the add path', async () => {
  const h = harness(nodeMachines, 'MachinesSettings', nodeMachineEnv);
  h.data['/api/machines'] = { ok: true, machines: [{ id: 'legacy', name: 'Old GPU', host: '192.168.1.20', user: 'gpu' }], offers: {} };
  h.data['/api/machines/local'] = { hostname: 'This machine', os: 'Linux' };
  h.data['/api/machines/metrics'] = { metrics: {} };
  h.data['/api/agents/installations'] = { installations: [] };
  h.data['/api/terminals'] = { terminals: [] };
  const tree = await h.ready({ route: {} });
  const adds = nodes(tree, 'button').filter(n => n.props.class === 'mcard add');
  assert.equal(adds.length, 1);
  assert.match(textOf(adds[0]), /Ajouter une machine/);
  adds[0].props.onClick();
  const opened = h.render();
  assert.ok(flatten(opened).some(n => n.type?.name === 'PairDialog'));
  assert.equal(nodes(opened, 'MachineDialog').length, 0);
  assert.ok(flatten(opened).some(n => n.type?.name === 'MachineMigration'));
});

test('Node stepper copies the installer, accepts a discovered URL, pairs and displays modules', async () => {
  let copied, closed;
  const h = harness(nodeMachines, 'PairDialog', { ...nodeMachineEnv, copyText: async text => { copied = text; return true; } });
  let tree = h.render({ start: {}, onClose: result => { closed = result; } });
  assert.match(textOf(tree), /Linux/);
  await button(tree, 'Copier').props.onClick();
  assert.equal(copied, 'curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh -s -- --node --listen lan');
  button(tree, 'Continuer').props.onClick();
  tree = h.render();
  const discovery = flatten(tree).find(n => n.type?.name === 'Discovered');
  discovery.props.onPair({ address: 'http://192.168.1.20:2511', port: 2511 });
  tree = h.render();
  assert.equal(nodes(tree, 'input')[0].props.value, 'http://192.168.1.20:2511');
  nodes(tree, 'input')[1].props.onInput({ target: { value: 'k7qm4xpa' } });
  h.env.post = async (...args) => { h.posts.push(args); return { ok: true, machine: { id: 'paired', name: 'GPU', modules: ['engine', 'terminal', 'observe'] } }; };
  await button(h.render(), 'Appairer').props.onClick();
  tree = h.render();
  assert.equal(h.posts[0][1].code, 'K7QM4XPA');
  assert.match(textOf(tree), /Machine appairée/);
  assert.equal(closed, undefined);
  const modules = flatten(tree).find(n => n.type?.name === 'NodeModules');
  assert.deepEqual(Array.from(modules.props.modules), ['engine', 'terminal', 'observe']);
  button(tree, 'Ouvrir la machine').props.onClick();
  assert.equal(closed.id, 'paired');
});

test('discovery uses the advertised full address once and node modules are read-only', async () => {
  let selected;
  const h = harness(nodeMachines, 'Discovered', nodeMachineEnv);
  h.data['/api/machines/discover'] = { nodes: [{ id: 'node', name: 'GPU', address: 'http://192.168.1.20:2511', port: 2511, version: '0.1.4' }] };
  const tree = await h.ready({ onPair: n => { selected = n; } });
  button(tree, 'Appairer').props.onClick();
  assert.equal(selected.address, 'http://192.168.1.20:2511');
  assert.ok(!textOf(tree).includes('2511:2511'));
  const mods = harness(nodeMachines, 'NodeModules').render({ modules: ['engine', 'terminal'] });
  assert.match(textOf(mods), /Non annoncé/);
  assert.equal(nodes(mods, 'Switch').length, 0);
});

test('SSH migration shows progress and exact failures; manual pairing targets the same id', async () => {
  let complete, tick, changed = 0;
  const h = harness(nodeMachines, 'MachineMigration', { ...nodeMachineEnv, setInterval: fn => { tick = fn; return 1; }, post: (...args) => { h.posts.push(args); return new Promise(resolve => { complete = resolve; }); } });
  const m = { id: 'ssh-gpu', name: 'GPU', host: '192.168.1.20', user: 'gpu' };
  let tree = h.render({ m, onChange: () => changed++ });
  assert.match(textOf(tree), /Connectée en SSH \(ancienne méthode\)/);
  const pending = button(tree, 'Installer Loom Node via SSH').props.onClick();
  assert.match(textOf(h.render()), /Installation et démarrage/);
  h.data['/api/machines/ssh-gpu/node/migrate'] = { ok: true, phase: 'pairing' };
  tick(); await settle();
  assert.match(textOf(h.render()), /Appairage et conservation/);
  complete({ ok: false, error: 'ssh: connection refused exactly' }); await pending;
  tree = h.render();
  assert.match(textOf(tree), /ssh: connection refused exactly/);
  assert.equal(changed, 0);
  button(tree, 'Appairer manuellement').props.onClick({ preventDefault() {}, stopPropagation() {} });
  const dialog = flatten(h.render()).find(n => n.type?.name === 'PairDialog');
  assert.equal(dialog.props.start.address, '192.168.1.20:2511');
  assert.equal(dialog.props.start.machine_id, m.id);
});

test('node update header checks on mount and applies the displayed node version', async () => {
  const endpoint = '/api/machines/gpu/node/update';
  const h = harness(updates, 'LoomUpdates');
  h.data[endpoint] = { current: '0.1.4', latest: '0.2.0', available: true, can_apply: true };
  const tree = await h.ready({ node: true, compact: true, endpoint });
  const install = button(tree, 'Mettre à jour (0.2.0)');
  assert.ok(install);
  await install.props.onClick();
  assert.equal(h.posts[0][0], endpoint + '/apply');
  assert.equal(h.posts[0][1].version, '0.2.0');
});
