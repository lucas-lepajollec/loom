// État du domaine Agents : connexion à Codeman, sessions vivantes, historique,
// CLI disponibles, approbations. Tenu à jour par le flux SSE ; chaque
// (re)connexion relit l'état complet pour rattraper ce qui a été manqué.
import { createStore, useStore, useEffect } from '../../core/lib.js';
import { api, gateway, connectEvents } from './client.js';

export const agents = createStore({
  conn: { state: 'idle', configured: false, auth: '', version: '', error: '', ui_url: '', url: '', password_set: false },
  sessions: {}, order: [], unified: [], clis: [], cases: [], approvals: [], restore: [],
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

export function approvalsFor(id) { return agents.get().approvals.filter(a => a.sessionId === id); }

// État lisible d'une session, du plus urgent au plus calme.
export function stateOf(s) {
  if (!s) return 'gone';
  if (approvalsFor(s.id).some(a => a.kind !== 'idle')) return 'waiting';
  if (s.status === 'error') return 'error';
  if (s.pid == null) return Date.now() - (s.createdAt || 0) < 90000 && s.status !== 'stopped' ? 'starting' : 'stopped';
  if (s.isWorking || s.status === 'busy') return 'working';
  return 'idle';
}
export const TONE = { waiting: 'red', error: 'red', working: 'amber', starting: 'amber', idle: 'green', stopped: '', gone: '' };

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
  agents.set({ sessions, order: st.order.filter(x => x !== id), approvals: st.approvals.filter(a => a.sessionId !== id) });
}
function bumpTurn(id) { const t = agents.get().turns; agents.set({ turns: { ...t, [id]: (t[id] || 0) + 1 } }); }

export async function refreshConnection() {
  try {
    const [cfg, st] = await Promise.all([gateway.config(), gateway.status()]);
    const state = !st.reachable ? 'unreachable' : st.auth === 'ok' ? 'ready' : st.auth === 'rejected' ? (cfg.password_set ? 'rejected' : 'auth') : 'error';
    agents.set({ conn: { state, configured: !!cfg.configured, auth: st.auth, version: st.version || '', error: st.error || '', ui_url: st.ui_url || '', url: cfg.url, username: cfg.username, password_set: cfg.password_set, multi_user: !!st.multi_user } });
    return state;
  } catch (e) {
    agents.set({ conn: { ...agents.get().conn, state: 'error', error: e.message } });
    return 'error';
  }
}

export async function refreshAll() {
  const [sessions, clis, approvals, unified, restore, cases, status, settings] = await Promise.allSettled([api.sessions(), api.clis(), api.approvals(), api.unified(), api.rebootRestore(), api.cases(), api.status(), api.settings()]);
  if (sessions.status === 'fulfilled' && Array.isArray(sessions.value)) setSessions(sessions.value);
  const patch = { loaded: true };
  if (clis.status === 'fulfilled' && Array.isArray(clis.value)) patch.clis = clis.value;
  if (approvals.status === 'fulfilled') patch.approvals = approvals.value?.approvals || [];
  if (unified.status === 'fulfilled') patch.unified = unified.value?.sessions || [];
  if (restore.status === 'fulfilled') patch.restore = restore.value?.sessions || [];
  if (cases.status === 'fulfilled' && Array.isArray(cases.value)) patch.cases = cases.value;
  if (status.status === 'fulfilled' && status.value?.planUsage) patch.planUsage = status.value.planUsage;
  if (settings.status === 'fulfilled' && settings.value) patch.settings = settings.value;
  agents.set(patch);
  if (sessions.status === 'rejected') throw sessions.reason;
}
let unifiedTimer;
export function refreshUnified() {
  clearTimeout(unifiedTimer);
  unifiedTimer = setTimeout(() => api.unified().then(r => agents.set({ unified: r?.sessions || [] })).catch(() => {}), 600);
}
export async function refreshApprovals() {
  try { const r = await api.approvals(); agents.set({ approvals: r?.approvals || [] }); } catch (_) {}
}

// ---------- événements ----------
const sessionFrom = d => d && (d.session && d.session.id ? d.session : d.id ? d : null);
const idOf = d => d && (d.id || d.sessionId || d.session?.id);
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
    case 'session:idle': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, isWorking: false, status: 'idle' }); bumpTurn(id); break; }
    case 'session:exit': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, pid: null, isWorking: false }); bumpTurn(id); break; }
    case 'session:error': { const id = idOf(d); if (agents.get().sessions[id]) upsert({ id, lastError: d.error }); break; }
    case 'session:interactive': case 'session:running': { const id = idOf(d); if (id) api.session(id).then(upsert).catch(() => {}); break; }
    case 'session:completion': case 'hook:stop': case 'transcript:complete': case 'hook:prompt_submitted': case 'transcript:tool_end':
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
export function useAgentsLive() {
  refs++;
  if (!stop) {
    agents.set({ conn: { ...agents.get().conn, state: agents.get().conn.state === 'ready' ? 'ready' : 'connecting' } });
    stop = connectEvents({
      onEvent,
      onOpen: () => { agents.set({ live: true }); refreshAll().catch(() => {}); if (agents.get().conn.state !== 'ready') refreshConnection(); },
      onDown: code => { agents.set({ live: false }); if (code === 'CODEMAN_AUTH' || code === 'CODEMAN_UNREACHABLE' || /^HTTP/.test(code)) refreshConnection(); },
    });
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
