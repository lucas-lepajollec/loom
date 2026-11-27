// État du domaine Agents : connexion à Codeman, sessions vivantes, historique,
// CLI disponibles, approbations. Tenu à jour par le flux SSE ; chaque
// (re)connexion relit l'état complet pour rattraper ce qui a été manqué.
import { createStore, useStore, useEffect, useMemo } from '../../core/lib.js';
import { singleFlight } from '../../core/poll.js';
import { api, gateway, connectEvents, sendPrompt } from './client.js';

const SEEN_KEY = 'loom.agents.seen';
function loadSeen() { try { return JSON.parse(localStorage.getItem(SEEN_KEY) || '{}') || {}; } catch (_) { return {}; } }

export const agents = createStore({
  conn: { state: 'idle', configured: false, auth: '', version: '', error: '', ui_url: '', url: '', password_set: false },
  sessions: {}, order: [], unified: [], historyOffset: 0, historyTotal: -1, historyError: '',
  clis: [], cases: [], approvals: [], restore: [], completed: {},
  // Dernière ouverture de chaque session, pour « terminé, pas encore vu ».
  seen: loadSeen(),
  live: false, loaded: false,
  // Dernière mesure des quotas d'abonnement (Claude 5 h / 7 j, Codex).
  planUsage: null,
  // Réglages du serveur Codeman (CLI et dossier par défaut des nouvelles sessions).
  settings: {},
  // Nouvelle session demandée depuis la barre latérale ou la page.
  creating: null,
  // Versions de transcription : un événement de tour incrémente, la vue relit.
  turns: {},
});

// ---------- modèle ----------
// Logo des CLI connus (ui/next/logos) ; sinon le badge court du registre.
const LOGOS = { claude: 'claude', codex: 'openai', gemini: 'gemini', antigravity: 'google', deepseek: 'deepseek', grok: 'xai', copilot: '', opencode: '', pi: '', omp: '' };
export const cliLogo = mode => LOGOS[mode] || '';
export function cliOf(mode) {
  const c = agents.get().clis.find(x => x.id === mode);
  return c || { id: mode, label: mode || '?', shortBadge: String(mode || '?').slice(0, 2).toUpperCase(), kind: mode === 'shell' ? 'shell' : 'agent' };
}

// Les signaux structurés diffèrent selon le CLI (docs Codeman, « Signals ») :
// les hooks n'existent que pour Claude ; Codex expose ses dialogues lus à
// l'écran ; les autres n'ont que idle/working/exit, détectés à l'écran.
export const CAPS = {
  claude: { approvals: 'answer', stop: true, transcript: 'jsonl' },
  codex: { approvals: 'terminal', stop: false, transcript: 'rollout' },
  deepseek: { approvals: 'terminal', stop: true, transcript: 'jsonl' },
};
export const capsOf = mode => CAPS[mode] || (mode === 'shell' ? { approvals: 'none', stop: false, transcript: 'none' } : { approvals: 'terminal', stop: false, transcript: 'screen' });

// Vue conversation : seulement pour les CLI dont Codeman lit une vraie
// transcription (Claude, Codex…). Les autres ne montrent que leur terminal.
export const hasConversation = mode => ['jsonl', 'rollout'].includes(capsOf(mode).transcript);

export function approvalsFor(id) { return agents.get().approvals.filter(a => a.sessionId === id); }

// État lisible d'une session, du plus urgent au plus calme.
export function stateOf(s) {
  if (!s) return 'gone';
  if (approvalsFor(s.id).some(a => a.kind !== 'idle')) return 'waiting';
  if (s.status === 'error') return 'error';
  // Arrêtée sur une limite d'abonnement, Codeman la relancera à la remise à zéro.
  if (s.autoResumeAt && s.autoResumeAt > Date.now()) return 'limited';
  if (s.pid == null) return Date.now() - (s.createdAt || 0) < 90000 && s.status !== 'stopped' ? 'starting' : 'stopped';
  if (s.isWorking || s.status === 'busy') return 'working';
  return 'idle';
}
// Ce qui demande l'attention de l'utilisateur passe avant l'état technique :
// un accord, une question, ou un travail fini qu'il n'a pas encore regardé.
export function attentionOf(s) {
  if (!s) return 'gone';
  const asks = approvalsFor(s.id).filter(a => a.kind !== 'idle');
  if (asks.some(a => a.kind === 'question')) return 'question';
  if (asks.length) return 'approval';
  const state = stateOf(s);
  if (state !== 'idle') return state;
  const st = agents.get(), done = st.completed[s.id];
  return done && done > (st.seen[s.id] || 0) ? 'unseen' : 'idle';
}
export function markSeen(id) {
  if (!id) return;
  const seen = { ...agents.get().seen, [id]: Date.now() };
  // Ne garde que les sessions encore connues, pour borner le stockage.
  const live = agents.get().sessions;
  for (const k of Object.keys(seen)) if (!live[k] && k !== id) delete seen[k];
  agents.set({ seen });
  try { localStorage.setItem(SEEN_KEY, JSON.stringify(seen)); } catch (_) {}
}
export const ATTENTION = new Set(['approval', 'question', 'unseen']);
// Une session ouverte et visible est vue, y compris quand elle finit sous nos yeux.
export function useSeen(id) {
  const done = useStore(agents, s => s.completed[id] || 0);
  useEffect(() => {
    if (!id) return;
    const look = () => { if (!document.hidden) markSeen(id); };
    look();
    document.addEventListener('visibilitychange', look);
    return () => document.removeEventListener('visibilitychange', look);
  }, [id, done]);
}

export const TONE = { approval: '', question: '', unseen: '', waiting: 'red', error: 'red', working: 'amber', starting: 'amber', limited: 'amber', idle: 'green', stopped: '', gone: '' };

export const sessionList = () => { const st = agents.get(); return st.order.map(id => st.sessions[id]).filter(Boolean); };
export const projectOf = dir => String(dir || '').replace(/\/+$/, '').split('/').pop() || '/';

// ---------- lectures ----------
function setSessions(list) {
  const sessions = {}, order = [];
  for (const s of list || []) { if (s && s.id) { sessions[s.id] = s; order.push(s.id); } }
  agents.set({ sessions, order });
}
function upsert(s) {
  if (!s || !s.id) return;
  const st = agents.get();
  const order = st.order.includes(s.id) ? st.order : [...st.order, s.id];
  agents.set({ sessions: { ...st.sessions, [s.id]: { ...(st.sessions[s.id] || {}), ...s } }, order });
}
function drop(id) {
  const st = agents.get();
  if (!st.sessions[id]) return;
  const sessions = { ...st.sessions }; delete sessions[id];
  const completed = { ...st.completed }; delete completed[id];
  agents.set({ sessions, completed, order: st.order.filter(x => x !== id), approvals: st.approvals.filter(a => a.sessionId !== id) });
}
const bumpTimers = new Map();
function bumpTurnNow(id) {
  if (!id) return;
  const pending = bumpTimers.get(id);
  if (pending) { clearTimeout(pending); bumpTimers.delete(id); }
  const t = agents.get().turns;
  agents.set({ turns: { ...t, [id]: (t[id] || 0) + 1 } });
}
function bumpTurn(id, immediate = false) {
  if (!id) return;
  if (immediate) return bumpTurnNow(id);
  if (bumpTimers.has(id)) return;
  bumpTimers.set(id, setTimeout(() => bumpTurnNow(id), 250));
}

export async function refreshConnection() {
  try {
    const [cfg, st] = await Promise.all([gateway.config(), gateway.status()]);
    const state = !st.reachable ? 'unreachable' : st.auth === 'ok' ? 'ready' : st.auth === 'rejected' ? (cfg.password_set ? 'rejected' : 'auth') : 'error';
    const conn = { state, configured: !!cfg.configured, auth: st.auth, version: st.version || '', error: st.error || '', ui_url: st.ui_url || '', url: cfg.url, username: cfg.username, password_set: cfg.password_set, multi_user: !!st.multi_user, role: st.role };
    const prev = agents.get().conn;
    if (conn.url !== prev.url || conn.username !== prev.username || conn.role !== prev.role) {
      historyRevision++;
      historyRequested = false;
      agents.set({
        conn, sessions: {}, order: [], unified: [], historyOffset: 0, historyTotal: -1, historyError: '',
        approvals: [], restore: [], clis: [], cases: [], settings: {}, planUsage: null, turns: {}, completed: {}, loaded: false, live: false,
      });
      if (stop) {
        stop();
        stop = state === 'ready' ? openEventStream() : null;
      }
    } else agents.set({ conn });
    return state;
  } catch (e) {
    agents.set({ conn: { ...agents.get().conn, state: 'error', error: e.message } });
    return 'error';
  }
}

export const refreshAll = singleFlight(async () => {
  const conn = agents.get().conn;
  // Ne relance l'historique unifié (scan disque de 500 sessions) que s'il a
  // déjà été demandé : évite de saturer Codeman à chaque retour d'onglet.
  if (historyRequested) loadHistory().catch(() => {});
  const [sessions, clis, approvals, restore, cases, status, settings] = await Promise.allSettled([api.sessions(), api.clis(), api.approvals(), api.rebootRestore(), api.cases(), api.status(), api.settings()]);
  if (!sameServer(conn)) return;
  if (sessions.status === 'fulfilled' && Array.isArray(sessions.value)) setSessions(sessions.value);
  const patch = { loaded: true };
  if (clis.status === 'fulfilled' && Array.isArray(clis.value)) patch.clis = clis.value;
  if (approvals.status === 'fulfilled') patch.approvals = approvals.value?.approvals || [];
  if (restore.status === 'fulfilled') patch.restore = restore.value?.sessions || [];
  if (cases.status === 'fulfilled' && Array.isArray(cases.value)) patch.cases = cases.value;
  if (status.status === 'fulfilled' && status.value?.planUsage) patch.planUsage = status.value.planUsage;
  if (settings.status === 'fulfilled' && settings.value) patch.settings = settings.value;
  agents.set(patch);
  if (sessions.status === 'rejected') throw sessions.reason;
});
const sameServer = conn => {
  const current = agents.get().conn;
  return current.state === 'ready' && current.url === conn.url && current.username === conn.username && current.role === conn.role;
};
let unifiedTimer, historyRevision = 0, historyRequested = false;
export function ensureHistory() {
  if (historyRequested && agents.get().historyTotal >= 0) return;
  historyRequested = true;
  loadHistory().catch(() => {});
}
export async function loadHistory(more = false) {
  historyRequested = true;
  const revision = ++historyRevision;
  const { conn, unified, historyOffset } = agents.get();
  const offset = more ? historyOffset : 0;
  const current = () => revision === historyRevision && sameServer(conn);
  try {
    const data = await api.unified(offset);
    if (!current()) return false;
    const rows = data?.sessions || [];
    const end = offset + rows.length;
    const byId = new Map((more ? unified : []).map(row => [row.sessionId, row]));
    for (const row of rows) byId.set(row.sessionId, row);
    agents.set({ unified: [...byId.values()], historyOffset: end, historyTotal: rows.length ? data?.total ?? end : end, historyError: '' });
    return true;
  } catch (e) {
    if (current()) agents.set({ historyError: e.message });
    return false;
  }
}
export function refreshUnified() {
  if (!historyRequested) return;
  clearTimeout(unifiedTimer);
  unifiedTimer = setTimeout(() => loadHistory().catch(() => {}), 600);
}

// ---------- événements ----------
const sessionFrom = d => d && (d.session && d.session.id ? d.session : d.id ? d : null);
const idOf = d => d && (d.id || d.sessionId || d.session?.id);
export const recentCompletionMs = 24 * 60 * 60 * 1000;
// Commandes de réglage (/model, /effort) demandées pendant un tour : elles
// partent quand l'agent a fini, pour ne pas couper son travail.
const pendingCommands = {};
export function queueCommand(id, cmd) {
  const s = agents.get().sessions[id];
  if (s && stateOf(s) === 'idle') return sendPrompt(id, cmd).then(() => 'sent');
  (pendingCommands[id] ||= []).push(cmd);
  return Promise.resolve('queued');
}
async function flushCommands(id) {
  const list = pendingCommands[id]; delete pendingCommands[id];
  for (const cmd of list || []) { try { await sendPrompt(id, cmd); } catch (_) { /* la session a pu se fermer */ } }
}

function recordCompletion(id) {
  if (!id) return;
  if (pendingCommands[id]) flushCommands(id);
  const now = Date.now();
  const completed = Object.fromEntries(Object.entries(agents.get().completed).filter(([, at]) => now - at < recentCompletionMs));
  agents.set({ completed: { ...completed, [id]: now } });
}

function onEvent(name, d) {
  switch (name) {
    case 'session:created': {
      const s = sessionFrom(d);
      if (s && s.mode) upsert(s);
      else if (s) api.session(s.id).then(upsert).catch(() => {});
      refreshUnified(); break;
    }
    case 'session:updated': upsert(sessionFrom(d)); break;
    case 'session:deleted': drop(idOf(d)); refreshUnified(); break;
    case 'session:working': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, isWorking: true, status: 'busy' }); break; }
    case 'session:idle': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, isWorking: false, status: 'idle' }); bumpTurn(id, true); break; }
    case 'session:exit': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, pid: null, isWorking: false, status: 'stopped' }); bumpTurn(id, true); break; }
    case 'session:error': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, lastError: d.error, status: 'error' }); break; }
    case 'session:interactive': case 'session:running': { const id = idOf(d); if (id) api.session(id).then(upsert).catch(() => {}); break; }
    case 'session:completion': case 'hook:stop': case 'transcript:complete': {
      const id = d?.sessionId || idOf(d);
      recordCompletion(id); bumpTurn(id, true); break;
    }
    case 'hook:prompt_submitted': case 'transcript:tool_end':
      bumpTurn(idOf(d)); break;
    case 'approval:pending': {
      const a = d?.id ? d : d?.item;
      if (a?.id) agents.set({ approvals: [...agents.get().approvals.filter(x => x.id !== a.id), a] });
      break;
    }
    case 'approval:updated': {
      const a = d?.id ? d : d?.item;
      if (a?.id) agents.set({ approvals: agents.get().approvals.map(x => x.id === a.id ? { ...x, ...a } : x) });
      break;
    }
    case 'approval:resolved': agents.set({ approvals: agents.get().approvals.filter(x => x.id !== d?.id) }); break;
    // Limite d'abonnement atteinte : Codeman programme la reprise (auto-resume).
    case 'session:limitPauseScheduled': { const id = idOf(d); if (agents.get().sessions[id] && Number.isFinite(d.resumeAt)) upsert({ id, autoResumeAt: d.resumeAt, isWorking: false }); break; }
    case 'session:limitResume': case 'session:limitResumeCancelled': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, autoResumeAt: null }); break; }
    case 'session:statusTelemetry': if (d && typeof d === 'object') agents.set({ planUsage: d }); break;
    case 'session:orderChanged': api.sessions().then(setSessions).catch(() => {}); break;
    default:
      if (name.startsWith('cron:')) listeners.cron.forEach(fn => fn(name, d));
      if (name.startsWith('subagent:')) listeners.subagent.forEach(fn => fn(name, d));
  }
}
// Abonnements ciblés des vues (planification, sous-agents).
const listeners = { cron: new Set(), subagent: new Set() };
export function onDomain(kind, fn) { listeners[kind].add(fn); return () => listeners[kind].delete(fn); }

// Une seule connexion SSE pour tout le module, ouverte tant qu'une vue Agents
// est montée (compteur de références).
let refs = 0, stop = null;
function openEventStream() {
  return connectEvents({
    onEvent,
    onOpen: () => { agents.set({ live: true }); refreshAll().catch(() => {}); if (agents.get().conn.state !== 'ready') refreshConnection(); },
    onDown: code => { agents.set({ live: false }); if (code === 'CODEMAN_AUTH' || code === 'CODEMAN_UNREACHABLE' || /^HTTP/.test(code)) refreshConnection(); },
  });
}
export function useAgentsLive() {
  refs++;
  if (!stop) {
    agents.set({ conn: { ...agents.get().conn, state: agents.get().conn.state === 'ready' ? 'ready' : 'connecting' } });
    stop = openEventStream();
  }
  return () => { if (--refs <= 0) { refs = 0; stop?.(); stop = null; agents.set({ live: false }); } };
}

export function upsertSession(s) { upsert(s); }
export function dropSession(id) { drop(id); }

// Présence de Codeman dans toute l'application (barre latérale) : l'état est
// lu au démarrage, le flux reste ouvert tant que Codeman répond, et une
// connexion configurée mais perdue est revérifiée régulièrement.
export function useAgentsPresence() {
  const ready = useStore(agents, s => s.conn.state === 'ready');
  const configured = useStore(agents, s => s.conn.configured);
  useEffect(() => { refreshConnection(); }, []);
  useEffect(() => {
    if (ready) return useAgentsLive();
    if (!configured) return;
    const timer = setInterval(() => { if (!document.hidden) refreshConnection(); }, 20000);
    return () => clearInterval(timer);
  }, [ready, configured]);
}

// Sessions qu'un redémarrage a coupées et que Codeman peut relancer (hors sessions déjà rouvertes).
export const restorable = s => {
  if (!(s.restore || []).length) return [];
  // Le plan désigne une session par son ancien identifiant ou sa conversation ;
  // une session relancée garde la conversation dans resumeSessionId / claudeSessionId.
  const live = new Set();
  for (const x of Object.values(s.sessions || {})) for (const k of [x.id, x.resumeSessionId, x.claudeSessionId]) if (k) live.add(k);
  return s.restore.filter(e => e && e.id && !live.has(e.id));
};
export function useRestorable() {
  const { restore, sessions } = useStore(agents, s => ({ restore: s.restore, sessions: s.sessions }));
  return useMemo(() => restorable({ restore, sessions }), [restore, sessions]);
}
