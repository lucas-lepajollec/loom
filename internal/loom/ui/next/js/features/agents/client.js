// Client de la passerelle Codeman de Loom. Seul module du domaine Agents qui
// parle au réseau : les composants appellent ces fonctions, jamais fetch.
// Codeman répond { success, data } ou { success:false, error, errorCode } ;
// la passerelle Loom ajoute CODEMAN_UNREACHABLE et CODEMAN_AUTH.
import { request } from '../../core/api.js';
import { visibleConnection } from '../../core/poll.js';

const BASE = '/api/codeman/v1';

export class CodemanError extends Error {
  constructor(message, code, status) { super(message); this.code = code || ''; this.status = status || 0; }
}

async function call(method, path, body, opts = {}) {
  const init = { method, retryAuth: method !== 'GET', timeout: opts.timeout ?? 30000, signal: opts.signal, headers: {} };
  if (body instanceof FormData) init.body = body;
  else if (body !== undefined) { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(body); }
  let r;
  try { r = await request(BASE + path, init); }
  catch (e) { if (e.name === 'AbortError') throw e; throw new CodemanError(e.message || 'network', 'NETWORK', 0); }
  let json = null;
  const type = r.headers.get('Content-Type') || '';
  if (type.includes('json')) { try { json = await r.json(); } catch (_) {} }
  if (!r.ok || (json && json.success === false)) {
    throw new CodemanError(json?.error || 'HTTP ' + r.status, json?.errorCode || (r.status === 404 ? 'NOT_FOUND' : ''), r.status);
  }
  // Quelques anciennes routes Codeman renvoient la charge à la racine.
  return json ? (json.data !== undefined ? json.data : json) : null;
}

export const cm = {
  get: (path, opts) => call('GET', path, undefined, opts),
  post: (path, body = {}, opts) => call('POST', path, body, opts),
  put: (path, body = {}, opts) => call('PUT', path, body, opts),
  patch: (path, body = {}, opts) => call('PATCH', path, body, opts),
  del: (path, opts) => call('DELETE', path, undefined, opts),
};
const enc = encodeURIComponent;

// ---------- configuration de la passerelle (côté Loom) ----------
async function loomJSON(url, init) {
  const r = await request(url, { cache: 'no-store', ...init });
  let data = {};
  try { data = await r.json(); } catch (_) {}
  if (!r.ok || data.ok === false) throw new CodemanError(data.error || 'HTTP ' + r.status, 'LOOM', r.status);
  return data;
}
export const gateway = {
  config: () => loomJSON('/api/codeman/config', { retryAuth: false }),
  save: body => loomJSON('/api/codeman/config', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  status: () => loomJSON('/api/codeman/status', { retryAuth: false, timeout: 10000 }),
  discover: () => loomJSON('/api/codeman/discover', { retryAuth: false, timeout: 20000 }),
  // Une réponse { ok:false, code } est un résultat de test, pas une panne.
  connect: async body => { const r = await request('/api/codeman/connect', { method: 'POST', timeout: 20000, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); let d = {}; try { d = await r.json(); } catch (_) {} if (!r.ok && !d.code) throw new CodemanError(d.error || 'HTTP ' + r.status, 'LOOM', r.status); return d; },
  disconnect: () => loomJSON('/api/codeman/disconnect', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }),
};

// ---------- sessions ----------
export const api = {
  sessions: () => cm.get('/sessions'),
  unified: (q = '') => cm.get('/sessions/unified' + (q ? '?q=' + enc(q) : '')),
  session: id => cm.get('/sessions/' + enc(id)),
  clis: () => cm.get('/clis'),
  cases: () => cm.get('/cases'),
  browse: path => cm.get('/filesystem/browse' + (path ? '?path=' + enc(path) : '')),
  create: body => cm.post('/sessions', body),
  start: (id, shell) => cm.post('/sessions/' + enc(id) + (shell ? '/shell' : '/interactive')),
  remove: (id, killMux = true) => cm.del('/sessions/' + enc(id) + '?killMux=' + (killMux ? 'true' : 'false')),
  rename: (id, name) => cm.put('/sessions/' + enc(id) + '/name', { name }),
  input: (id, body, opts) => cm.post('/sessions/' + enc(id) + '/input', body, opts),
  transcript: id => cm.get('/sessions/' + enc(id) + '/last-response?context=full'),
  terminal: (id, query = 'tail=400000') => cm.get('/sessions/' + enc(id) + '/terminal?' + query, { timeout: 45000 }),
  activeTools: id => cm.get('/sessions/' + enc(id) + '/active-tools'),
  runSummary: id => cm.get('/sessions/' + enc(id) + '/run-summary'),
  gitStatus: (id, fresh) => cm.get('/sessions/' + enc(id) + '/git-status' + (fresh ? '?fresh=1' : ''), { timeout: 45000 }),
  gitDiff: (id, f) => cm.get('/sessions/' + enc(id) + '/git-diff?repo=' + enc(f.repo) + '&path=' + enc(f.path) + '&kind=' + enc(f.kind), { timeout: 45000 }),
  files: id => cm.get('/sessions/' + enc(id) + '/files', { timeout: 20000 }),
  fileContent: (id, path) => cm.get('/sessions/' + enc(id) + '/file-content?path=' + enc(path)),
  subagents: id => cm.get('/sessions/' + enc(id) + '/subagents'),
  subagentTranscript: agentId => cm.get('/subagents/' + enc(agentId) + '/transcript?format=formatted'),
  upload: (id, file) => { const f = new FormData(); f.append('image', file, file.name); return cm.post('/sessions/' + enc(id) + '/paste-image' + (/^video\//.test(file.type) ? '?kind=video' : ''), f, { timeout: 300000 }); },
  settings: () => cm.get('/settings'),
  saveSettings: patch => cm.put('/settings', patch),
  setCli: (id, enabled) => cm.put('/clis/' + enc(id), { enabled }),
  installCli: id => cm.post('/clis/' + enc(id) + '/install', {}, { timeout: 600000 }),
  tokenStats: () => cm.get('/token-stats'),
  autoCompact: (id, enabled, threshold) => cm.post('/sessions/' + enc(id) + '/auto-compact', { enabled, ...(threshold ? { threshold } : {}) }),
  autoClear: (id, enabled, threshold) => cm.post('/sessions/' + enc(id) + '/auto-clear', { enabled, ...(threshold ? { threshold } : {}) }),
  approvals: () => cm.get('/approvals'),
  answer: (itemId, body) => cm.post('/approvals/' + enc(itemId) + '/answer', body),
  dismiss: itemId => cm.post('/approvals/' + enc(itemId) + '/dismiss'),
  viewed: sessionId => cm.post('/approvals/session/' + enc(sessionId) + '/viewed'),
  rebootRestore: () => cm.get('/reboot-restore'),
  restore: sessionIds => cm.post('/reboot-restore/restore', sessionIds ? { sessionIds } : {}, { timeout: 120000 }),
  dismissRestore: () => cm.post('/reboot-restore/dismiss'),
  cronJobs: () => cm.get('/cron/jobs'),
  cronRuns: () => cm.get('/cron/runs'),
  cronCreate: body => cm.post('/cron/jobs', body),
  cronUpdate: (id, body) => cm.put('/cron/jobs/' + enc(id), body),
  cronEnable: (id, enabled) => cm.put('/cron/jobs/' + enc(id) + '/enabled', { enabled }),
  cronRun: id => cm.post('/cron/jobs/' + enc(id) + '/run', {}, { timeout: 60000 }),
  cronDelete: id => cm.del('/cron/jobs/' + enc(id)),
  updateCheck: () => cm.get('/system/update/check', { timeout: 20000 }),
  status: () => cm.get('/status'),
};

// ---------- envoi fiable d'un prompt ----------
// Codeman applique chaque paire (clientId, seq) au plus une fois : un renvoi
// après coupure ne retape jamais le prompt. L'identité vit le temps du
// chargement de la page (un onglet dupliqué ne la partage donc jamais, ce qui
// ferait ignorer ses envois comme doublons).
const cid = (() => {
  const b = new Uint8Array(12); crypto.getRandomValues(b);
  return 'loom-' + Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
})();
export const terminalClientId = () => cid + '-term';
const seqs = new Map();
function nextSeq(sessionId) { const n = (seqs.get(sessionId) || 0) + 1; seqs.set(sessionId, n); return n; }
const clientId = () => cid;

// Une ligne par appel : Codeman retire les sauts de ligne, et n'envoie Entrée
// que si l'entrée se termine par \r.
export const flattenPrompt = text => String(text || '').replace(/\r?\n+/g, ' ').replace(/\s+$/g, '');
export async function sendPrompt(sessionId, text) {
  const line = flattenPrompt(text);
  if (!line) return null;
  const r = await api.input(sessionId, { input: line + '\r', useMux: true, clientId: clientId(), seq: nextSeq(sessionId) });
  if (r && r.delivered === false && !r.duplicate) throw new CodemanError('not delivered', 'NOT_DELIVERED', 0);
  return r;
}
// Touches brutes (Échap pour interrompre, flèches, chiffres d'un dialogue).
export const sendKeys = (sessionId, keys) => api.input(sessionId, { input: keys, useMux: true, clientId: clientId(), seq: nextSeq(sessionId) });

// ---------- flux d'événements ----------
// Lecture du SSE Codeman relayé par Loom. Un battement arrive toutes les 15 s :
// au-delà de 45 s de silence, le flux est considéré mort et rouvert. Chaque
// (re)connexion appelle onOpen pour relire l'état complet.
export function connectEvents({ onEvent, onOpen, onDown }) {
  return visibleConnection(async owner => {
    let watchdog;
    const kick = () => { clearTimeout(watchdog); watchdog = setTimeout(() => owner.retry(), 45000); };
    let reader;
    try {
      // ?sessions= ne garde le flux terminal que pour les sessions listées : aucune
      // ici, les terminaux ont leur WebSocket. Les événements d'état passent tous.
      const r = await request(BASE + '/events?sessions=loom-no-terminal-stream', { retryAuth: false, timeout: 15000, signal: owner.signal, headers: { Accept: 'text/event-stream' } });
      if (!owner.alive()) return;
      if (!r.ok || !(r.headers.get('Content-Type') || '').includes('text/event-stream')) {
        let code = '';
        try { code = (await r.json()).errorCode || ''; } catch (_) {}
        onDown?.(code || 'HTTP ' + r.status); owner.retry(); return;
      }
      reader = r.body.getReader();
      onOpen?.(); kick();
      const dec = new TextDecoder();
      let buf = '';
      (async () => {
        try {
          for (;;) {
            const { done, value } = await reader.read();
            if (done || !owner.alive()) break;
            kick();
            buf += dec.decode(value, { stream: true });
            let i;
            while ((i = buf.indexOf('\n\n')) >= 0) {
              const frame = buf.slice(0, i); buf = buf.slice(i + 2);
              let name = 'message', data = '';
              for (const line of frame.split('\n')) {
                if (line.startsWith('event:')) name = line.slice(6).trim();
                else if (line.startsWith('data:')) data += line.slice(5).trim();
              }
              if (name === 'sse:heartbeat') continue;
              let payload = null;
              try { payload = data ? JSON.parse(data) : null; } catch (_) { continue; }
              onEvent(name, payload);
            }
            if (buf.length > 4 * 1024 * 1024) buf = '';
          }
        } catch (_) {}
        if (owner.alive()) { onDown?.('closed'); owner.retry(); }
      })();
    } catch (e) {
      if (owner.alive()) { onDown?.(e.code || 'NETWORK'); owner.retry(); }
      return;
    }
    return () => { clearTimeout(watchdog); reader?.cancel().catch(() => {}); };
  }, 2000);
}
