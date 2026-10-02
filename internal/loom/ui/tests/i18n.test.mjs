import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import fr from '../next/js/i18n/fr.js';
import en from '../next/js/i18n/en.js';
import { scan, templateCopy, frenchLooking } from './i18n-scan.mjs';

const root = new URL('../next/js/', import.meta.url);
const files = [];
function walk(dir) {
  for (const item of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, item.name);
    if (item.isDirectory()) { if (item.name !== 'i18n') walk(file); }
    else if (file.endsWith('.js')) files.push(file);
  }
}
walk(root.pathname);
const own = (object, key) => Object.prototype.hasOwnProperty.call(object, key);
const placeholders = text => [...text.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort();

test('English and French have matching keys, plural forms and interpolation variables', () => {
  assert.deepEqual(Object.keys(en).sort(), Object.keys(fr).sort());
  for (const key of Object.keys(fr)) {
    assert.equal(typeof en[key], typeof fr[key], key);
    if (typeof fr[key] === 'object') {
      assert.deepEqual(Object.keys(fr[key]).sort(), ['one', 'other'], key);
      assert.deepEqual(Object.keys(en[key]).sort(), ['one', 'other'], key);
      for (const form of ['one', 'other']) assert.deepEqual(placeholders(en[key][form]), placeholders(fr[key][form]), key + '.' + form);
    } else {
      assert.equal(typeof fr[key], 'string', key);
      assert.deepEqual(placeholders(en[key]), placeholders(fr[key]), key);
    }
  }
});

test('every literal t() key and stored translation key exists in both dictionaries', () => {
  const missing = [];
  for (const file of files) {
    const { tokens } = scan(fs.readFileSync(file, 'utf8'));
    for (let i = 0; i < tokens.length; i++) {
      const token = tokens[i];
      const callKey = token.type === 'string' && tokens[i - 1]?.value === '(' && tokens[i - 2]?.value === 't';
      const storedKey = token.type === 'string' && /^(settings\.page\.|runtime\.copy\.)/.test(token.value) && token.value !== 'runtime.copy.';
      if ((callKey || storedKey) && (!own(fr, token.value) || !own(en, token.value))) missing.push(path.relative(root.pathname, file) + ': ' + token.value);
    }
  }
  assert.deepEqual(missing, []);
});

test('html templates contain no French copy outside t(), with documented exceptions', () => {
  const allowed = JSON.parse(fs.readFileSync(new URL('./i18n-allowlist.json', import.meta.url), 'utf8'));
  assert.ok(allowed.every(item => item.file && item.literal && item.reason));
  const found = [], used = new Set();
  for (const file of files) {
    const source = fs.readFileSync(file, 'utf8'), relative = path.relative(root.pathname, file);
    const { templates, stringsInHTML } = scan(source);
    const candidates = [...templates.flatMap(templateCopy), ...stringsInHTML];
    for (const item of candidates) {
      if (own(fr, item.value) || !frenchLooking(item.value)) continue;
      const exception = allowed.find(x => x.file === relative && x.literal === item.value.trim());
      if (exception) used.add(exception); else found.push(relative + ':' + (source.slice(0, item.start).split('\n').length) + ': ' + item.value);
    }
  }
  assert.deepEqual(found, []);
  assert.equal(used.size, allowed.length, 'remove obsolete exceptions');
});

test('template heuristic sees nested copy and attributes but ignores tags, comments and translated keys', () => {
  const sample = '// html`<b>Ignoré</b>`\nconst view = html`<${Line} title="Bonjour">${ok && html`<b>Enregistrer</b>`}${t("chat.composer.placeholder")}</${Line}>`;';
  const parsed = scan(sample);
  const found = parsed.templates.flatMap(templateCopy).filter(x => frenchLooking(x.value)).map(x => x.value);
  assert.deepEqual(found.sort(), ['Bonjour', 'Enregistrer']);
  assert.ok(parsed.tokens.some(token => token.value === 'chat.composer.placeholder'));
});

const source = fs.readFileSync(new URL('../next/js/core/i18n.js', import.meta.url), 'utf8')
  .replace(/^import .*;\n/gm, '').replace(/^export /gm, '').replaceAll("import('./api.js')", 'Promise.resolve(api)');
function runtime({ browser = 'en-US', saved, blockedStorage = false, api = {} } = {}) {
  const storage = new Map(saved === undefined ? [] : [['loom-lang', saved]]);
  const posts = [];
  const environment = {
    fr, en, navigator: { language: browser }, document: { documentElement: {} },
    localStorage: { getItem: key => { if (blockedStorage) throw new Error('blocked'); return storage.get(key); }, setItem: (key, value) => { if (blockedStorage) throw new Error('blocked'); storage.set(key, value); } },
    api: { get: async () => ({ prefs: {} }), post: async (url, body) => { posts.push([url, body]); return { ok: true, prefs: body }; }, ...api },
  };
  vm.runInNewContext(source + '\nglobalThis.i18n = { t, tSource, getLang, locale, language, setLang, initLang };', environment);
  return { ...environment.i18n, storage, posts, document: environment.document };
}

test('language defaults, storage fallback, interpolation, plurals and document language', async () => {
  assert.equal(runtime({ browser: 'fr-CA' }).getLang(), 'fr');
  assert.equal(runtime({ browser: 'de-DE' }).getLang(), 'en');
  assert.equal(runtime({ browser: 'fr', saved: 'en' }).getLang(), 'en');
  assert.equal(runtime({ browser: 'en', saved: 'bogus' }).getLang(), 'en');
  const r = runtime({ browser: 'fr', blockedStorage: true });
  assert.equal(r.getLang(), 'fr');
  assert.equal(r.t('n_models', { n: 2 }), '2 modèles');
  assert.equal(await r.setLang('en'), true);
  assert.equal(r.document.documentElement.lang, 'en');
  assert.equal(r.t('n_models', { n: 1 }), '1 model');
  assert.equal(r.t('n_models', { n: 0 }), '0 models');
  assert.equal(r.t('n_models', { n: 2 }), '2 models');
  assert.equal(r.t('n_models'), '{n} models');
  assert.equal(r.t('missing.key'), 'missing.key');
  assert.equal(r.t('runtime.copy.acp_custom', { command: '<ssh>' }), 'Custom ACP harness launched by <ssh>.');
  assert.equal(r.tSource('Harness ACP personnalisé, lancé par ssh.'), 'Custom ACP harness launched by ssh.');
  assert.equal(r.tSource('An unknown native description'), 'An unknown native description');
  assert.equal(r.tSource(fr['inspector.catalog.gpu-layers.tip']), en['inspector.catalog.gpu-layers.tip']);
});

test('language changes immediately, saves only lang, notifies subscribers and serializes rapid changes', async () => {
  const r = runtime(); let changed = 0;
  const unsubscribe = r.language.subscribe(() => changed++);
  const first = r.setLang('fr');
  assert.equal(r.getLang(), 'fr');
  assert.equal(r.storage.get('loom-lang'), 'fr');
  const second = r.setLang('en');
  await Promise.all([first, second]);
  assert.equal(changed, 2);
  assert.deepEqual(r.posts.map(([url, body]) => [url, Object.keys(body), body.lang]), [['/api/prefs', ['lang'], 'fr'], ['/api/prefs', ['lang'], 'en']]);
  unsubscribe(); await r.setLang('fr'); assert.equal(changed, 2);
  assert.equal(await r.setLang('de'), false);
  assert.equal(r.getLang(), 'fr');
  assert.equal(r.posts.length, 3);
});

test('saved server language wins on startup; a late read cannot overwrite an explicit choice', async () => {
  const normal = runtime({ saved: 'fr', api: { get: async () => ({ prefs: { lang: 'en' } }) } });
  await normal.initLang(); assert.equal(normal.getLang(), 'en'); assert.equal(normal.storage.get('loom-lang'), 'en');
  let resolve;
  const r = runtime({ api: { get: () => new Promise(done => { resolve = done; }) } });
  const init = r.initLang(); await Promise.resolve();
  await r.setLang('fr'); resolve({ prefs: { lang: 'en' } }); await init;
  assert.equal(r.getLang(), 'fr');
  const failed = runtime({ api: { post: async () => ({ ok: false }) } });
  assert.equal(await failed.setLang('fr'), false); assert.equal(failed.getLang(), 'fr');
});

test('the curated parameter catalog is translated without changing native IDs or choices', () => {
  const catalog = JSON.parse(fs.readFileSync(new URL('../../engine/llamacpp/params/llamacpp.json', import.meta.url), 'utf8'));
  const r = runtime();
  for (const param of catalog.params) {
    assert.equal(r.tSource(param.label), en['inspector.catalog.' + param.id + '.label'], param.id);
    assert.equal(r.tSource(param.tip), en['inspector.catalog.' + param.id + '.tip'], param.id);
  }
  assert.equal(r.tSource('q8_0'), 'q8_0');
  assert.equal(r.tSource('--ctx-size'), '--ctx-size');
});

test('provider catalogs retain object identity while their labels follow the current language', () => {
  let lang = 'fr';
  const t = key => ({ fr, en })[lang][key];
  const catalog = fs.readFileSync(new URL('../next/js/features/cloud/catalog.js', import.meta.url), 'utf8').replace(/^import .*;\n/gm, '').replace(/^export /gm, '');
  const env = { t, URL };
  vm.runInNewContext(catalog + '\nglobalThis.catalog = { GROUPS, ALL, entryFor };', env);
  const entry = env.catalog.entryFor({ endpoint: 'https://api.groq.com/openai/v1' });
  assert.equal(entry.hint, 'Inférence très rapide');
  lang = 'en';
  assert.equal(entry.hint, 'Very fast inference');
  assert.equal(env.catalog.entryFor({ endpoint: 'https://api.groq.com/openai/v1' }), entry);
  assert.ok(env.catalog.GROUPS.some(group => group.items.includes(entry)));
});

test('language-aware component wrappers retain identity and subscribe to live changes', async () => {
  const r = runtime(); let updates = 0;
  const lib = fs.readFileSync(new URL('../next/js/core/lib.js', import.meta.url), 'utf8')
    .replace(/^import .*;\n/gm, '').replace(/^export \{.*\};\n/gm, '').replace(/^export /gm, '');
  const env = {
    language: r.language, t: r.t,
    preactH: (type, props) => ({ type, props }), Fragment: () => {},
    useReducer: () => [0, () => updates++], useLayoutEffect: fn => fn(),
    htm: { bind: fn => fn },
  };
  vm.runInNewContext(lib + '\nglobalThis.make = h;', env);
  const View = () => r.t('settings.page.apparence');
  const before = env.make(View, {});
  assert.equal(before.type(before.props), 'Appearance');
  const pending = r.setLang('fr');
  assert.equal(updates, 1);
  const after = env.make(View, {});
  assert.equal(after.type, before.type, 'same component identity preserves hooks and focus');
  assert.equal(after.type(after.props), 'Apparence');
  assert.equal(env.make('input', {}).type, 'input');
  await pending;
});
