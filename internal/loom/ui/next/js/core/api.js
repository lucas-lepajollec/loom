// Client de l'API Loom. La clé de pilotage (si le serveur en exige une) est
// envoyée en Bearer ; sur 401 on la demande une fois, puis on rejoue.
import { ask } from '../ui/dialog.js';

let token = localStorage.getItem('loom.key') || '';
let asking = null;
const TIMEOUT = 30000;

function headers(extra) {
  const h = { ...(extra || {}) };
  if (token) h.Authorization = 'Bearer ' + token;
  return h;
}

async function askKey() {
  if (!asking) {
    asking = ask({ title: 'Authentification', message: 'Ce Loom exige sa clé de pilotage.', input: { placeholder: 'clé…', type: 'password' }, ok: 'Continuer' })
      .then(k => { asking = null; if (k) { token = k.trim(); localStorage.setItem('loom.key', token); } return k; });
  }
  return asking;
}

export async function request(url, opts = {}) {
  const { retryAuth = true, ...fetchOpts } = opts;
  const o = { ...fetchOpts, headers: headers(opts.headers) };
  let timer = null;
  if (!o.signal) { const ac = new AbortController(); o.signal = ac.signal; timer = setTimeout(() => ac.abort(), TIMEOUT); }
  let r;
  try { r = await fetch(url, o); }
  catch (e) { if (e.name === 'AbortError' && timer) throw new Error('Le serveur ne répond pas.'); throw e; }
  finally { if (timer) clearTimeout(timer); }
  if (r.status === 401 && retryAuth && await askKey()) { o.headers = headers(opts.headers); r = await fetch(url, o); }
  return r;
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
