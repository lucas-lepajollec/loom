import { t } from './i18n.js';
// État global de l'application : route, moteur, matériel, espace de travail,
// liste des discussions. Les pages lisent ce store ; les actions l'actualisent.
import { createStore } from './lib.js';
import { get } from './api.js';
import { visibleRefresh, singleFlight } from './poll.js';
import { validShape, validData } from './shape.js';

const observation = url => value => value === null || validData(value, url);
export const app = createStore({
  route: parseRoute(),
  theme: localStorage.getItem('loom-theme') || 'dark',
  sideOpen: false, palette: false, inspector: innerWidth > 1100 && localStorage.getItem('loom.next.insp') !== '0',
  status: null, serverInfo: null, gpus: [], ram: null, engineNode: null,
  workspace: null, presets: [], models: [],
  nav: { conversations: [], projects: [], active: '' }, unavailable: {},
}, { models: v => validData(v, '/api/models'), presets: v => validData(v, '/api/presets'), gpus: v => validData(v, '/api/vram'), status: observation('/api/status'), serverInfo: observation('/api/ping'), ram: observation('/api/ram'), engineNode: value => value === null || typeof value?.remote === 'boolean' && validData(value, '/api/engine/node'),
  workspace: observation('/api/workspace'), nav: { conversations: [], projects: [], active: '' }, unavailable: {}, voiceMode: value => value === null || validShape(value, { discussion: '', internet: false }), welcome: false, newPreset: false });

// ---------- routes : #/section/sous-section/id ----------
export function parseRoute() {
  const [section, sub, id] = location.hash.replace(/^#\/?/, '').split('/').map(decodeURIComponent);
  // Machines left the settings: old links and bookmarks keep working.
  if (section === 'settings' && sub === 'machines') {
    history.replaceState(null, '', '#/machines' + (id ? '/' + encodeURIComponent(id) : ''));
    return { section: 'machines', sub: id || '', id: '' };
  }
  // Startup belongs to Loom's own settings.
  if (section === 'startup') {
    history.replaceState(null, '', '#/settings/startup');
    return { section: 'settings', sub: 'startup', id: '' };
  }
  // Engine and workspaces moved to their domain pages.
  if (section === 'settings' && ['engine', 'workspaces'].includes(sub)) {
    history.replaceState(null, '', '#/' + sub);
    return { section: sub, sub: '', id: '' };
  }
  return { section: section || 'chat', sub: sub || '', id: id || '' };
}
export function go(section, sub, id) {
  const hash = '#/' + [section, sub, id].filter(Boolean).map(encodeURIComponent).join('/');
  if (location.hash !== hash) location.hash = hash; else app.set({ route: parseRoute() });
}
addEventListener('hashchange', () => app.set({ route: parseRoute(), sideOpen: false }));

// ---------- thème ----------
export function setTheme(localT) {
  document.documentElement.dataset.theme = localT;
  try { localStorage.setItem('loom-theme', localT); } catch (_) {}
  const meta = document.querySelector('meta[name=theme-color]');
  if (meta) meta.content = localT === 'light' ? '#f6f5f2' : '#141413';
  app.set({ theme: localT });
}

// ---------- sondages ----------
// Moteur sur une autre machine (Loom lié) : lu au démarrage et après un changement.
// Failure state is separate from retained observations. Reads never prompt for
// sign-in or reload the page; explicit user actions still use the auth flow.
function unavailable(key, failed) {
  if (app.get().unavailable[key] !== failed) app.set({ unavailable: { ...app.get().unavailable, [key]: failed } });
}
async function read(key, url, shape, project = v => v) {
  try {
    const value = await get(url, { timeout: 6000, retryAuth: false });
    if (!validShape(value, shape) || !validData(value, url)) throw new Error('Invalid observation');
    const next = project(value);
    if (!['history', 'sessions'].includes(key) && JSON.stringify(app.get()[key]) !== JSON.stringify(next)) app.set({ [key]: next });
    unavailable(key, false);
    return value;
  } catch (_) { unavailable(key, true); return null; }
}
export const refreshEngineNode = singleFlight(() => read('engineNode', '/api/engine/node', r => typeof r?.remote === 'boolean', r => r.remote ? r : null));
export const refreshStatus = singleFlight(() => Promise.allSettled([
  read('status', '/api/status', {}), read('serverInfo', '/api/ping', {}),
]));
export const refreshHardware = singleFlight(async () => {
  if (!document.hidden) await read('gpus', '/api/vram', []);
});
export const refreshWorkspace = singleFlight(() => read('workspace', '/api/workspace', {}));
export const refreshLibrary = singleFlight(() => Promise.allSettled([
  read('models', '/api/models', []), read('presets', '/api/presets', []),
]));
let historySnapshot = null, sessionSnapshot = null;
export const refreshNav = singleFlight(async () => {
  await Promise.allSettled([
    read('history', '/api/chat/history', {}).then(r => { if (r) historySnapshot = r; }),
    read('sessions', '/api/runtime/sessions', r => Array.isArray(r?.sessions)).then(r => { if (r) sessionSnapshot = r; }),
  ]);
  if (!historySnapshot && !sessionSnapshot) return;
  const hist = historySnapshot || {}, sessions = sessionSnapshot?.sessions || [];
  const imported = new Set(sessions.flatMap(s => [s.source_archive, s.native_archive]).filter(Boolean));
  const conversations = [...sessions.filter(s => s.message_count > 0).map(s => ({ ...s, workspace: true })), ...(hist.conversations || []).filter(c => !imported.has(c.id) && c.turns > 0)];
  const bound = sessions.find(s => s.native_archive === hist.active && s.runtime_id === 'llama.cpp');
  app.set({ nav: { conversations, projects: hist.projects || [], active: bound ? bound.id : hist.active || '', projectId: hist.project_id || '' } });
});

// Nature d'un runtime (local, cloud, harness) d'après le registre du serveur.
export function runtimeKind(id) {
  if (!id || id === 'llama.cpp') return 'local';
  const r = ((app.get().workspace && app.get().workspace.runtimes) || []).find(x => x.id === id);
  return r && r.kind === 'harness' ? 'harness' : 'cloud';
}

export const runtimeCaps = id => ((((app.get().workspace && app.get().workspace.runtimes) || []).find(x => x.id === id)) || {}).capabilities || [];

// État lisible du moteur local pour la carte et l'en-tête.
export function engineState(s) {
  if (!s) return { tone: '', label: t("core.state.lecture_de_l_etat") };
  if (s.load_error) return { tone: 'red', label: t("core.state.erreur_de_chargement"), detail: s.load_error };
  if (s.health && s.model) return { tone: 'green', label: s.preset_name || s.model_name };
  if (s.active && s.model) return { tone: 'amber', label: t("core.state.chargement") + (s.preset_name || s.model_name) };
  if (s.active) return { tone: 'blue', label: t("core.state.moteur_pret_aucun_modele") };
  return { tone: '', label: t("core.state.moteur_arrete") };
}

let stopPolling;
export function startPolling() {
  if (stopPolling) return stopPolling;

  // A suspended initial load must recover without forcing a page reload.
  const stops = [visibleRefresh(refreshStatus, 4000), visibleRefresh(refreshHardware, 4000), visibleRefresh(() => Promise.allSettled([refreshEngineNode(), refreshWorkspace(), refreshNav(), refreshLibrary()]), 30000)];
  stopPolling = () => { stops.forEach(stop => stop()); stopPolling = null; };
  return stopPolling;
}

// Shared lists (agents, models, providers) follow any change made elsewhere.
if (typeof window !== 'undefined') window.addEventListener('loom:changed', () => { refreshWorkspace(); refreshNav(); refreshLibrary(); refreshEngineNode(); });
