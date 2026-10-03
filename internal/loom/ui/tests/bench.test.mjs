import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
import { french } from './i18n-fixture.mjs';

const source = fs.readFileSync(new URL('../next/js/features/bench/choices.js', import.meta.url), 'utf8');
const helpers = { t: french };
vm.runInNewContext(source.replace(/^export /gm, '') + '\nglobalThis.choices = benchChoices; globalThis.method = benchMethod;', helpers);
const models = [
  { id: 'cloud-hidden', name: 'API model', kind: 'cloud', provider_name: 'Fixture cloud', enabled: false, supported: true },
  { id: 'native', name: 'Native model', kind: 'harness', provider_name: 'Claude Code', supported: true },
  { id: 'agy', name: 'Google model', kind: 'harness', provider_name: 'Antigravity', supported: false, reason: 'no_model_only_mode' },
  { id: 'redirect', name: 'Source model', kind: 'harness', provider_name: 'Harness', supported: true, via: 'cloud' },
  { id: 'offline', name: 'Disconnected', kind: 'cloud', provider_name: 'Offline', supported: false, reason: 'provider_disconnected' },
];

test('Bench includes hidden cloud models, uses native IDs and rejects unsupported or redirected harness modes', () => {
  const choices = helpers.choices(models, french);
  assert.equal(choices.length, 4);
  assert.equal(choices[0].supported, true);
  assert.equal(choices[1].v.choice_id, 'native');
  assert.match(choices[1].sub, /sans outils/);
  assert.equal(choices[2].supported, false);
  assert.match(choices[2].sub, /ne garantit pas/);
  assert.equal(choices[3].supported, false);
  assert.match(choices[3].sub, /Reconnecte/);
});

test('Bench labels native engine timing separately from API and native account rates', () => {
  assert.match(helpers.method({ kind: 'local', result: { prompt_per_second: 100 } }, french), /timings natifs/);
  assert.match(helpers.method({ kind: 'local', result: {} }, french), /durée de requête/);
  assert.match(helpers.method({ kind: 'account' }, french), /Compte natif/);
  assert.match(helpers.method({ kind: 'cloud' }, french), /API cloud/);
});

const html = htm.bind((type, props, ...children) => ({ type, props: props || {}, children }));
const flatten = x => Array.isArray(x) ? x.flatMap(flatten) : x && typeof x === 'object' ? [x, ...flatten(x.children)] : [];
const text = x => Array.isArray(x) ? x.map(text).join('') : x && typeof x === 'object' ? text(x.children) : String(x ?? '');

test('Bench launches only supported selected models with external consent and renders full responses', async () => {
  let cursor = 0, tree;
  const slots = [], polls = [], posts = [], consents = [], cancellations = [];
  const env = { html, t: french, Icon: 'Icon', Logo: 'Logo', Empty: 'Empty', Modal: 'Modal',
    app: {}, cls: (...v) => v.filter(Boolean).join(' '), fmtBytes: () => '', vendorOf: () => '',
    useStore: () => ({ models: [], presets: [] }),
    useState: initial => { const i = cursor++; if (!(i in slots)) slots[i] = initial; return [slots[i], v => { slots[i] = typeof v === 'function' ? v(slots[i]) : v; }]; },
    useRef: initial => { const i = cursor++; return slots[i] ||= { current: initial }; },
    useEffect() {}, refreshLibrary() {}, useVisibleRefresh: task => polls.push(task),
    benchChoices: helpers.choices, benchMethod: helpers.method, toast: () => {},
    confirm: async (...args) => { consents.push(args); return true; },
    get: async path => path.startsWith('/api/bench/catalog') ? { ok: true, models } : { ok: true, job: null },
    post: async (path, body) => { posts.push({ path, body }); return { ok: true, job: { status: 'done', id: 'fixture', rows: [{ status: 'ok', kind: 'account', name: 'Native model', preview: 'Short', output: 'Full fixture response', result: {} }] } }; },
    request: async (path, options) => { cancellations.push({ path, options }); return { json: async () => ({ ok: true, job: { id: 'active-fixture', status: 'cancel', rows: [] } }) }; },
  };
  const page = fs.readFileSync(new URL('../next/js/features/bench/page.js', import.meta.url), 'utf8');
  vm.runInNewContext(page.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.page = BenchPage;', env);
  const render = () => { cursor = 0; polls.length = 0; tree = env.page(); return tree; };
  render(); await polls[1](() => true); render();
  const picks = flatten(tree).filter(x => typeof x.type === 'function' && x.type.name === 'Pick');
  assert.equal(picks.length, 4);
  picks.find(x => x.props.i.key === 'native').props.onChange(true);
  picks.find(x => x.props.i.key === 'agy').props.onChange(true); // stale/forged UI selection is filtered.
  render();
  const run = flatten(tree).find(x => x.type === 'button' && text(x).startsWith('Lancer'));
  assert.equal(run.props.disabled, false);
  await run.props.onClick(); render();
  assert.equal(consents.length, 1);
  assert.match(consents[0][1], /quotas d’abonnement/);
  assert.equal(posts[0].body.consent, true);
  assert.equal(posts[0].body.models.length, 1);
  assert.equal(posts[0].body.models[0].choice_id, 'native');
  assert.match(text(tree), /Full fixture response/);
  assert.doesNotMatch(text(tree), /<script/);
  env.get = async () => ({ ok: true, job: { id: 'active-fixture', status: 'running', index: 0, rows: [] } });
  await polls[0](() => true); render();
  await flatten(tree).find(x => x.type === 'button' && text(x).includes('Arrêter la file')).props.onClick();
  assert.equal(cancellations.length, 1);
  assert.equal(cancellations[0].path, '/api/bench/queue/cancel');
  assert.equal(cancellations[0].options.headers['X-Loom-Bench-Job'], 'active-fixture');
});
