import { t } from './i18n.js';
// Human access uses an HttpOnly session cookie. Keep an existing legacy key
// in memory only until the operator sets the first password.
import { ask } from '../ui/dialog.js';

let token = '';
try { token = localStorage.getItem('loom.key') || ''; localStorage.removeItem('loom.key'); } catch (_) {}
let asking = null;
const TIMEOUT = 30000;

function headers(extra) {
  const h = { ...(extra || {}) };
  if (token) h.Authorization = 'Bearer ' + token;
  return h;
}
export async function authStatus() {
  const r = await fetch('/api/auth/status', { headers: headers(), credentials: 'same-origin', cache: 'no-store' });
  if (!r.ok) throw new Error(t('access.unavailable'));
  return r.json();
}
export async function authAction(path, body, key = '') {
  const r = await fetch('/api/auth/' + path, { method: 'POST', credentials: 'same-origin', cache: 'no-store',
    headers: { 'Content-Type': 'application/json', ...(key ? { Authorization: 'Bearer ' + key } : headers()) },
    body: JSON.stringify(body || {}) });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(r.status === 429 ? t('access.too_many') : r.status === 401 ? t('access.incorrect') : data.error || t('access.unavailable'));
  token = '';
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
  let timer = null;
  if (!o.signal) { const ac = new AbortController(); o.signal = ac.signal; timer = setTimeout(() => ac.abort(), timeout); }
  let r;
  try { r = await fetch(url, { ...o, credentials: 'same-origin' }); }
  catch (e) { if (e.name === 'AbortError' && timer) throw new Error(t("core.api.le_serveur_ne_repond_pas")); throw e; }
  finally { if (timer) clearTimeout(timer); }
  if (r.status === 401 && retryAuth && await askPassword()) { o.headers = headers(opts.headers); r = await fetch(url, { ...o, credentials: 'same-origin' }); }
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

export async function get(url) {
  const r = await request(url);
  return r.json();
}

export async function post(url, body, opts = {}) {
  const r = await request(url, { ...opts, method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
  const data = await r.json().catch(() => ({ ok: r.ok }));
  if (data && data.ok === undefined) data.ok = r.ok;
  if (!r.ok && data && !data.error) data.error = 'HTTP ' + r.status;
  data.status = r.status;
  return data;
}

// Lecture d'un flux SSE POST (le journal de la conversation native). Rappelle
// onEvent(delta) pour chaque événement ; retourne quand le flux se termine.
export async function stream(url, body, onEvent, signal) {
  const r = await request(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal });
  const reader = r.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) return;
    buf += dec.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf('\n\n')) >= 0) {
      const chunk = buf.slice(0, i); buf = buf.slice(i + 2);
      for (const line of chunk.split('\n')) {
        if (!line.startsWith('data:')) continue;
        const data = line.slice(5).trim();
        if (!data || data === '[DONE]') continue;
        try { const o = JSON.parse(data); onEvent((o.choices && o.choices[0] && o.choices[0].delta) || {}); } catch (_) {}
      }
    }
  }
}
