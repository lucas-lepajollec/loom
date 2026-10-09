import { t } from './i18n.js';
// Human access uses an HttpOnly session cookie. Keep an existing legacy key
// in memory only until the operator sets the first password.
import { ask } from '../ui/dialog.js';
import { clearObservations } from './observations.js';

let token = '';
try { token = localStorage.getItem('loom.key') || ''; localStorage.removeItem('loom.key'); } catch (_) {}
let asking = null;
const TIMEOUT = 30000;

// UI deadlines cannot depend on a suspended browser settling fetch/read after
// abort. Observe cancellation independently, while still aborting the transport.
function withAbort(task, signal) {
  if (!signal) return task();
  return new Promise((resolve, reject) => {
    const canceled = () => { signal.removeEventListener('abort', canceled); reject(Object.assign(new Error('aborted'), { name: 'AbortError' })); };
    if (signal.aborted) { canceled(); return; }
    signal.addEventListener('abort', canceled, { once: true });
    Promise.resolve().then(task).then(value => { signal.removeEventListener('abort', canceled); resolve(value); }, error => { signal.removeEventListener('abort', canceled); reject(error); });
  });
}

function headers(extra) {
  const h = { ...(extra || {}) };
  if (token) h.Authorization = 'Bearer ' + token;
  return h;
}
export async function authStatus() {
  const data = await jsonRequest('/api/auth/status', { retryAuth: false, cache: 'no-store' });
  if (data?.ok === false) throw new Error(t('access.unavailable'));
  return data;
}
export async function authAction(path, body, key = '') {
  const data = await jsonRequest('/api/auth/' + path, { method: 'POST', retryAuth: false, cache: 'no-store',
    headers: { 'Content-Type': 'application/json', ...(key ? { Authorization: 'Bearer ' + key } : headers()) },
    body: JSON.stringify(body || {}) });
  if (!data.ok) throw new Error(data.status === 429 ? t('access.too_many') : data.status === 401 ? t('access.incorrect') : data.error || t('access.unavailable'));
  token = '';
  clearObservations();
  try { localStorage.removeItem('loom.key'); } catch (_) {}
  return data;
}
async function askPassword() {
  if (!asking) {
    asking = (async () => {
      const status = await authStatus();
      if (!status.password_set) { location.reload(); return false; }
      const password = await ask({ title: t('access.sign_in'), message: t('access.enter_password'),
        input: { type: 'password', autocomplete: 'current-password' }, ok: t('access.sign_in') });
      if (!password) return false;
      await authAction('login', { password });
      return true;
    })().finally(() => { asking = null; });
  }
  return asking;
}

export async function request(url, opts = {}) {
  const { retryAuth = true, timeout = TIMEOUT, ...fetchOpts } = opts;
  const o = { ...fetchOpts, headers: headers(opts.headers) };
  let timer = null, ownedController;
  if (!o.signal) { ownedController = new AbortController(); o.signal = ownedController.signal; timer = setTimeout(() => ownedController.abort(), timeout); }
  let r;
  try { r = await withAbort(() => fetch(url, { ...o, credentials: 'same-origin' }), o.signal); }
  catch (e) { if (e.name === 'AbortError' && timer) throw new Error(t("core.api.le_serveur_ne_repond_pas")); throw e; }
  finally { if (timer) clearTimeout(timer); }
  if (r.status === 401) clearObservations();
  if (r.status === 401 && retryAuth && await askPassword()) {
    o.headers = headers(opts.headers);
    if (ownedController) timer = setTimeout(() => ownedController.abort(), timeout);
    try { r = await withAbort(() => fetch(url, { ...o, credentials: 'same-origin' }), o.signal); }
    finally { if (timer) clearTimeout(timer); }
  }
  return r;
}

// Téléchargement authentifié, avec les mêmes sessions/reprises que l'API.
export async function download(url, name) {
  const r = await request(url);
  if (!r.ok) throw new Error('HTTP ' + r.status);
  const a = document.createElement('a');
  a.href = URL.createObjectURL(await r.blob()); a.download = name; a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 5000);
}

// Abort stale reads on page suspension. Never replay mutations automatically:
// an aborted request may already have been accepted by the server.
const activeRequests = new Set();
function suspendRequests() { for (const controller of activeRequests) controller.abort(); }
if (typeof document !== 'undefined') document.addEventListener('visibilitychange', () => { if (document.hidden) suspendRequests(); });
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', suspendRequests);
  window.addEventListener('pageshow', event => { if (event.persisted) suspendRequests(); });
  window.addEventListener('offline', suspendRequests);
}

async function jsonRequest(url, opts) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  if (opts.signal?.aborted) abort();
  else opts.signal?.addEventListener('abort', abort, { once: true });
  const timer = setTimeout(abort, opts.timeout ?? TIMEOUT);
  activeRequests.add(controller);
  try {
    const r = await request(url, { ...opts, signal: controller.signal });
    const data = await withAbort(() => r.json(), controller.signal);
    if (opts.method === 'POST') {
      if (data && data.ok === undefined) data.ok = r.ok;
      if (!r.ok && data) { data.ok = false; if (!data.error) data.error = 'HTTP ' + r.status; }
      data.status = r.status;
    }
    return data;
  } catch (e) {
    if (controller.signal.aborted && !opts.signal?.aborted) throw new Error(t('core.api.le_serveur_ne_repond_pas'));
    throw e;
  } finally {
    activeRequests.delete(controller);
    clearTimeout(timer); opts.signal?.removeEventListener('abort', abort);
  }
}
export const get = (url, opts = {}) => jsonRequest(url, opts);
// A change to agents, machines, the engine or providers announces itself so
// open pages reload what they show instead of waiting for a page refresh.
const MUTATIONS = /^\/api\/(agents|harness|runtimes|machines|engine|providers|network|server|voice|chat\/settings)\b/;
let mutated = 0;
export const post = async (url, body, opts = {}) => {
  let r;
  try { r = await jsonRequest(url, { ...opts, method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) }); }
  catch (e) { return { ok: false, status: 0, error: e.message || t('core.api.le_serveur_ne_repond_pas') }; }
  if (r && r.ok !== false && MUTATIONS.test(url) && !/\/(probe|preview|status|check)\b/.test(url) && typeof window !== 'undefined') {
    clearTimeout(mutated);
    mutated = setTimeout(() => window.dispatchEvent(new CustomEvent('loom:changed', { detail: { url } })), 150);
  }
  return r;
};
// Re-runs `reload` whenever another action changed shared state.
export function onChanged(reload) {
  if (typeof window === 'undefined') return () => {};
  window.addEventListener('loom:changed', reload);
  return () => window.removeEventListener('loom:changed', reload);
}

// Lecture d'un flux SSE POST (le journal de la conversation native). Rappelle
// onEvent(delta) pour chaque événement ; retourne quand le flux se termine.
export async function stream(url, body, onEvent, signal) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  if (signal?.aborted) abort(); else signal?.addEventListener('abort', abort, { once: true });
  activeRequests.add(controller);
  let timer, reader;
  // Heartbeats are sent every four seconds. Renew the idle deadline for every
  // received chunk (including comments), rather than limiting generation time.
  const arm = () => { clearTimeout(timer); timer = setTimeout(abort, 25000); };
  arm();
  try {
    const r = await request(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: controller.signal });
    if (!r.ok || !r.headers.get('Content-Type')?.includes('text/event-stream')) throw new Error('HTTP ' + r.status);
    reader = r.body.getReader();
    const dec = new TextDecoder();
    let buf = '';
    for (;;) {
      const { done, value } = await withAbort(() => reader.read(), controller.signal);
      if (done) return;
      arm();
      buf += dec.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf('\n\n')) >= 0) {
        const chunk = buf.slice(0, i); buf = buf.slice(i + 2);
        for (const line of chunk.split('\n')) {
          if (!line.startsWith('data:')) continue;
          const data = line.slice(5).trim();
          if (!data || data === '[DONE]') continue;
          let event;
          try { event = JSON.parse(data); } catch (_) { continue; }
          onEvent(event.choices?.[0]?.delta || {});
        }
      }
      if (buf.length > 8 * 1024 * 1024) throw new Error('SSE frame exceeds limit');
    }
  } finally {
    clearTimeout(timer); activeRequests.delete(controller);
    signal?.removeEventListener('abort', abort);
    controller.abort();
    if (reader) { reader.cancel().catch(() => {}); reader.releaseLock(); }
  }
}
