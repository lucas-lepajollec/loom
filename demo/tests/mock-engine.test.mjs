import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { validData } from '../../internal/loom/ui/next/js/core/shape.js';

function storage(initial = {}) {
  const values = { ...initial };
  Object.defineProperties(values, {
    getItem: { value: (key) => (key in values ? values[key] : null) },
    setItem: { value: (key, value) => { values[key] = String(value); } },
    removeItem: { value: (key) => { delete values[key]; } },
  });
  return values;
}

function bootMockEngine({ search = '' } = {}) {
  const sessionStorage = storage();
  const localStorage = storage();
  const document = { documentElement: { lang: 'en' } };
  const windowObj = {
    location: { origin: 'http://127.0.0.1:2499', search },
    fetch: async () => new Response('not-api', { status: 404 }),
  };

  runInNewContext(readFileSync(new URL('../mock-engine.js', import.meta.url), 'utf8'), {
    window: windowObj,
    document,
    sessionStorage,
    localStorage,
    URL,
    URLSearchParams,
    Response,
    ReadableStream,
    TextEncoder,
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    Date,
    Math,
    JSON,
    Object,
    Array,
    Map,
    Set,
    Promise,
    Number,
    String,
    Boolean,
  });

  return { window: windowObj, document, sessionStorage, localStorage };
}

test('mock-engine defaults to English and honors ?lang=fr handoff', async () => {
  const enEnv = bootMockEngine();
  assert.equal(enEnv.document.documentElement.lang, 'en');
  assert.equal(enEnv.localStorage.getItem('loom-lang'), 'en');
  const enPrefs = await (await enEnv.window.fetch('/api/prefs')).json();
  assert.equal(enPrefs.prefs.lang, 'en');

  const frEnv = bootMockEngine({ search: '?lang=fr' });
  assert.equal(frEnv.document.documentElement.lang, 'fr');
  assert.equal(frEnv.localStorage.getItem('loom-lang'), 'fr');
  const frPrefs = await (await frEnv.window.fetch('/api/prefs')).json();
  assert.equal(frPrefs.prefs.lang, 'fr');
});

test('all core Loom ui/next GET endpoints satisfy core/shape.js validData contracts', async () => {
  const { window } = bootMockEngine();
  const routes = [
    '/api/auth/status',
    '/api/ping',
    '/api/status',
    '/api/vram',
    '/api/ram',
    '/api/engine/node',
    '/api/prefs',
    '/api/workspace',
    '/api/models',
    '/api/presets',
    '/api/chat/history',
    '/api/runtime/sessions',
    '/api/agent',
    '/api/providers',
    '/api/machines',
    '/api/machines/local',
    '/api/machines/metrics',
    '/api/machines/roles',
    '/api/terminals',
    '/api/previews',
    '/api/server',
    '/api/engine/service',
    '/api/engine/openai',
    '/api/engine/keys',
    '/api/engine/params',
    '/api/model-caps?model=/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
    '/api/voice',
    '/api/voice/packs',
    '/api/voice/models',
    '/api/voice/node',
    '/api/voice/jarvis',
    '/api/usage',
    '/api/usage/providers',
    '/api/quotas',
    '/api/market',
    '/api/files/tree',
    '/api/mcp/gateway',
    '/api/policy',
    '/api/policy/pending',
    '/api/policy/audit',
    '/api/startup',
    '/api/https',
    '/api/notify',
    '/api/mem/snapshots',
    '/api/llamacpp/job',
    '/api/hub/search?q=qwen',
  ];

  for (const url of routes) {
    const res = await window.fetch(url);
    assert.equal(res.status, 200, `${url} should return 200`);
    const data = await res.json();
    assert.equal(validData(data, url), true, `${url} failed core/shape.js validData()`);
  }
});

test('Codeman gateway, Second Brain note editing, and SSE discussion replay work end-to-end', async () => {
  const { window } = bootMockEngine();

  // Codeman endpoints
  const cmStatus = await (await window.fetch('/api/codeman/status')).json();
  assert.equal(cmStatus.reachable, true);
  assert.equal(cmStatus.auth, 'ok');

  const cmSessions = await (await window.fetch('/api/codeman/v1/sessions')).json();
  assert.equal(cmSessions.success, true);
  assert.ok(cmSessions.data.length >= 2);

  // Second Brain read & write persistence
  const tree = await (await window.fetch('/api/files/tree')).json();
  assert.equal(tree.exists, true);
  assert.ok(tree.tree.length > 0);

  const writeRes = await (await window.fetch('/api/files/write', {
    method: 'POST',
    body: JSON.stringify({ path: 'homelab/demo-note.md', content: '# Demo note\nSaved in session.' }),
  })).json();
  assert.equal(writeRes.ok, true);

  const readRes = await (await window.fetch('/api/files/read?path=homelab%2Fdemo-note.md')).json();
  assert.equal(readRes.content, '# Demo note\nSaved in session.');

  // Discussion SSE replay
  const sseRes = await window.fetch('/api/discussion/events', {
    method: 'POST',
    body: JSON.stringify({ id: '', from: 0 }),
  });
  assert.match(sseRes.headers.get('Content-Type'), /text\/event-stream/);
  const reader = sseRes.body.getReader();
  const dec = new TextDecoder();
  let seenCaughtUp = false;
  let seenText = false;
  for (let i = 0; i < 40; i++) {
    const { done, value } = await reader.read();
    if (done) break;
    const chunk = dec.decode(value);
    if (chunk.includes('"type":"text_delta"')) seenText = true;
    if (chunk.includes('"caught_up":true')) { seenCaughtUp = true; break; }
  }
  await reader.cancel();
  assert.equal(seenText, true, 'SSE replay should emit initial discussion text deltas');
  assert.equal(seenCaughtUp, true, 'SSE replay should emit caught_up');

  // EventSource & WebSocket interceptors
  const es = new window.EventSource('/api/codeman/v1/events');
  assert.equal(typeof es.close, 'function');
  es.close();

  const ws = new window.WebSocket('ws://127.0.0.1:2499/api/codeman/ws/cm-homelab-audit');
  await new Promise(resolve => { ws.onopen = resolve; });
  assert.equal(ws.readyState, 1);
  let zc = null;
  ws.onmessage = (ev) => { zc = JSON.parse(ev.data); };
  ws.send(JSON.stringify({ t: 'z', c: 120, r: 36 }));
  assert.deepEqual(zc, { t: 'zc', c: 120, r: 36 });
  ws.close();
});
