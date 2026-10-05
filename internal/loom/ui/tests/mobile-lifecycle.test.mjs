import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const read = file => fs.readFileSync(new URL('../next/js/core/' + file, import.meta.url), 'utf8');
const flush = () => new Promise(resolve => setImmediate(resolve));
function api() {
  const timers = new Map(), listeners = new Map(); let seq = 0, calls = 0, canceled = 0;
  const env = { AbortController, TextDecoder, t: x => x, localStorage: { getItem: () => null, removeItem() {} },
    document: { hidden: false, addEventListener: (n, f) => listeners.set(n, f) }, window: { addEventListener: (n, f) => listeners.set(n, f) },
    setTimeout: (f, ms) => { timers.set(++seq, { f, ms }); return seq; }, clearTimeout: id => timers.delete(id),
    fetch: async (_url, opts) => {
      calls++; const stall = () => new Promise((_resolve, reject) => {
        if (opts.signal.aborted) reject(Object.assign(new Error('aborted'), { name: 'AbortError' }));
        else opts.signal.addEventListener('abort', () => reject(Object.assign(new Error('aborted'), { name: 'AbortError' })), { once: true });
      });
      return { ok: true, status: 200, json: stall, headers: { get: () => 'text/event-stream' }, body: { getReader: () => ({ read: stall, cancel: () => { canceled++; return Promise.resolve(); }, releaseLock() {} }) } };
    },
  };
  vm.runInNewContext(read('api.js').replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.api = {get,post,stream,authStatus};', env);
  return { env, listeners, timers, calls: () => calls, canceled: () => canceled };
}
test('a stalled mutation body returns an error so controls can recover; no replay', async () => {
  const h = api(), promise = h.env.api.post('/select', { choice: 'new' }); await flush();
  [...h.timers.values()][0].f(); const result = await promise;
  assert.equal(result.ok, false); assert.equal(result.status, 0); assert.equal(h.calls(), 1); assert.equal(h.timers.size, 0);
});
test('page suspension releases outstanding reads and a resumed request is independent', async () => {
  const h = api(), promise = h.env.api.get('/workspace'); await flush();
  h.env.document.hidden = true; h.listeners.get('visibilitychange')(); await assert.rejects(promise);
  h.env.document.hidden = false; const next = h.env.api.get('/workspace'); await flush();
  h.listeners.get('pagehide')(); await assert.rejects(next); assert.equal(h.calls(), 2); assert.equal(h.timers.size, 0);
});
test('an idle SSE connection expires, releases its reader and permits reconnect', async () => {
  const h = api(), promise = h.env.api.stream('/events', {}, () => {}); await flush();
  assert.equal([...h.timers.values()][0].ms, 25000); [...h.timers.values()][0].f();
  await assert.rejects(promise); assert.equal(h.canceled(), 1); assert.equal(h.timers.size, 0);
  const next = h.env.api.stream('/events', {}, () => {}); await flush(); h.listeners.get('offline')();
  await assert.rejects(next); assert.equal(h.canceled(), 2); assert.equal(h.calls(), 2);
});
test('authentication status also bounds a stalled JSON body', async () => {
  const h = api(), promise = h.env.api.authStatus(); await flush(); [...h.timers.values()][0].f(); await assert.rejects(promise);
});
test('a browser that ignores transport abort cannot hold a UI request forever', async () => {
  const h = api(); h.env.fetch = async () => ({ok:true,status:200,json:()=>new Promise(()=>{})});
  const pending = h.env.api.post('/select', {}); await flush(); [...h.timers.values()][0].f();
  const result = await pending; assert.equal(result.ok, false); assert.equal(h.timers.size, 0);
});
test('a throttled animation frame cannot hold store notifications indefinitely', () => {
  let frame, fallback, notifications = 0;
  const source = read('lib.js').slice(read('lib.js').indexOf('export function createStore'), read('lib.js').indexOf('export function useStore'));
  const env = { requestAnimationFrame: f => { frame = f; return 1; }, cancelAnimationFrame() {}, setTimeout: f => { fallback = f; return 2; }, clearTimeout() {} };
  vm.runInNewContext(source.replace('export ', '') + '\nglobalThis.store = createStore({executor:"old"});', env);
  env.store.subscribe(() => notifications++); env.store.set({executor: 'new'}); env.store.set({loading: false});
  assert.equal(notifications, 0); fallback(); assert.equal(notifications, 1); frame(); assert.equal(notifications, 1);
  assert.equal(env.store.get().executor, 'new'); env.store.set({executor: 'next'}); frame(); fallback(); assert.equal(notifications, 2);
});
