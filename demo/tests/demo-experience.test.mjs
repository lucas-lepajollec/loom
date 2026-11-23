import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

function storage(initial) {
  const values = { ...initial };
  Object.defineProperties(values, {
    getItem: { value: (key) => values[key] ?? null },
    setItem: { value: (key, value) => { values[key] = String(value); } },
    removeItem: { value: (key) => { delete values[key]; } },
  });
  return values;
}

function runDemoChrome({ search = '', htmlLang = 'en', session = {}, local = {} } = {}) {
  const callbacks = new Map();
  let focusedWith = null;
  const heading = { focus(options) { focusedWith = options; } };
  const dialog = {
    classList: { add() {}, remove() {} },
    scrollTop: 0,
    innerHTML: '',
    setAttribute(name, value) { this[name] = value; },
    addEventListener(name, callback) { this[name] = callback; },
    querySelector(selector) {
      return selector === '#lh-demo-title' ? heading : null;
    },
    showModal() { this.scrollTop = 200; this._open = true; },
    close() { this._open = false; },
  };
  const chip = { classList: { add() {}, remove() {} }, innerHTML: '' };
  let created = 0;
  let reloads = 0;
  const sessionStorage = storage(session);
  const localStorage = storage(local);
  const document = {
    readyState: 'complete',
    documentElement: { lang: htmlLang },
    createElement() { return created++ === 0 ? dialog : chip; },
    body: { appendChild() {} },
    getElementById(id) {
      return { addEventListener(name, callback) { callbacks.set(`${id}:${name}`, callback); } };
    },
  };

  runInNewContext(readFileSync(new URL('../DemoExperience.js', import.meta.url), 'utf8'), {
    document,
    sessionStorage,
    localStorage,
    URLSearchParams,
    window: { location: { search, reload() { reloads += 1; } } },
  });

  return { dialog, chip, callbacks, sessionStorage, localStorage, getReloads: () => reloads, getFocusedWith: () => focusedWith };
}

test('demo reset removes Loom state without clearing unrelated origin data', () => {
  const env = runDemoChrome({
    session: {
      'lh-demo-intro-seen': '1',
      'loom_demo_state_v1': '{"lang":"en"}',
      other_session: 'keep',
    },
    local: {
      'loom-theme': 'dark',
      'loom-lang': 'fr',
      'loom.next.proj': '["proj-homelab"]',
      other_local: 'keep',
    },
  });

  env.callbacks.get('lh-demo-chip-reset:click')();

  assert.equal(env.getReloads(), 1);
  assert.equal(env.sessionStorage.getItem('loom_demo_state_v1'), null);
  assert.equal(env.sessionStorage.getItem('lh-demo-intro-seen'), null);
  assert.equal(env.localStorage.getItem('loom-theme'), null);
  assert.equal(env.localStorage.getItem('loom-lang'), null);
  assert.equal(env.localStorage.getItem('loom.next.proj'), null);
  assert.equal(env.sessionStorage.getItem('other_session'), 'keep');
  assert.equal(env.localStorage.getItem('other_local'), 'keep');
});

test('intro defaults to English, opens at its heading, and links Website/Docs/Source', () => {
  const env = runDemoChrome();

  assert.equal(env.dialog['aria-labelledby'], 'lh-demo-title');
  assert.equal(env.dialog.scrollTop, 0);
  assert.deepEqual({ ...env.getFocusedWith() }, { preventScroll: true });
  assert.match(env.dialog.innerHTML, /id="lh-demo-title" tabindex="-1"/);
  assert.doesNotMatch(env.dialog.innerHTML, /autofocus/);
  assert.match(env.dialog.innerHTML, /ISOLATED PUBLIC DEMO/);
  assert.match(env.dialog.innerHTML, /You can try/);
  assert.match(env.dialog.innerHTML, /What is simulated/);
  assert.match(env.dialog.innerHTML, /What never happens/);
  assert.match(env.dialog.innerHTML, /https:\/\/loom\.lucas-homelab\.fr/);
  assert.match(env.dialog.innerHTML, /https:\/\/docs\.loom\.lucas-homelab\.fr/);
  assert.match(env.dialog.innerHTML, /https:\/\/github\.com\/lucas-lepajollec\/loom/);
  assert.match(env.chip.innerHTML, />DEMO</);
});

test('?lang=fr renders localized French Pass 9 demo chrome', () => {
  const env = runDemoChrome({ search: '?lang=fr', htmlLang: 'fr' });

  assert.match(env.dialog.innerHTML, /DÉMONSTRATION PUBLIQUE ISOLÉE/);
  assert.match(env.dialog.innerHTML, /Vous pouvez tester/);
  assert.match(env.dialog.innerHTML, /Ce qui est simulé/);
  assert.match(env.dialog.innerHTML, /Ce qui n'arrive jamais/);
  assert.match(env.chip.innerHTML, />DÉMO</);
});

test('demo static shell enforces 3-layer noindex, CSP, clean favicon, and zero private IPs', () => {
  const indexHtml = readFileSync(new URL('../index.html', import.meta.url), 'utf8');
  const vercelJson = JSON.parse(readFileSync(new URL('../vercel.json', import.meta.url), 'utf8'));
  const robotsTxt = readFileSync(new URL('../robots.txt', import.meta.url), 'utf8');
  const manifest = readFileSync(new URL('../manifest.webmanifest', import.meta.url), 'utf8');
  const sw = readFileSync(new URL('../sw.js', import.meta.url), 'utf8');
  const mockEngine = readFileSync(new URL('../mock-engine.js', import.meta.url), 'utf8');

  // 3-layer noindex
  assert.match(indexHtml, /<meta\s+name="robots"\s+content="noindex,\s*nofollow,\s*noarchive"/i);
  const headers = vercelJson.headers[0].headers;
  assert.equal(headers.find(h => h.key === 'X-Robots-Tag')?.value, 'noindex, nofollow, noarchive');
  assert.ok(headers.find(h => h.key === 'Content-Security-Policy')?.value.includes("default-src 'self'"));
  assert.equal(vercelJson.git?.deploymentEnabled?.['dependabot/**'], false);
  assert.match(robotsTxt, /User-agent:\s*\*/);
  assert.match(robotsTxt, /Disallow:\s*\//);

  // Clean Loom weave favicon (no broken d='t')
  assert.ok(existsSync(new URL('../favicon.svg', import.meta.url)));
  for (const [name, content] of [['index.html', indexHtml], ['manifest', manifest], ['sw.js', sw]]) {
    assert.doesNotMatch(content, /d=['"]t['"]/, `${name} must not contain broken d='t' SVG path`);
  }
  assert.match(indexHtml, /<html\s+lang="en"/);
  assert.match(indexHtml, /src="\/next\/js\/app\/main\.js"/);

  // Zero RFC1918 private IPs in demo runtime files
  const privateIpRe = /\b(?:192\.168\.\d+\.\d+|10\.\d+\.\d+\.\d+|172\.(?:1[6-9]|2\d|3[01])\.\d+\.\d+)\b/;
  for (const [name, content] of [['index.html', indexHtml], ['mock-engine.js', mockEngine]]) {
    assert.doesNotMatch(content, privateIpRe, `${name} must not contain private LAN IP addresses`);
  }
});
