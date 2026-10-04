import { t } from './i18n.js';
// État global de l'application : route, moteur, matériel, espace de travail,
// liste des discussions. Les pages lisent ce store ; les actions l'actualisent.
import { createStore } from './lib.js';
import { get } from './api.js';
import { visibleRefresh, singleFlight } from './poll.js';

export const app = createStore({
  route: parseRoute(),
  theme: localStorage.getItem('loom-theme') || 'dark',
  sideOpen: false, palette: false, inspector: innerWidth > 1100 && localStorage.getItem('loom.next.insp') !== '0',
  status: null, serverInfo: null, gpus: [], ram: null, engineNode: null,
  workspace: null, presets: [], models: [],
  nav: { conversations: [], projects: [], active: '' },
});

// ---------- routes : #/section/sous-section/id ----------
export function parseRoute() {
  const [section, sub, id] = location.hash.replace(/^#\/?/, '').split('/').map(decodeURIComponent);
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
export async function refreshEngineNode() {
  try { const r = await get('/api/engine/node'); app.set({ engineNode: r.ok && r.remote ? r : null }); } catch (_) {}
}

export const refreshStatus = singleFlight(async () => {
  // /status belongs to the engine and may be forwarded to another machine.
  // /ping always identifies this control plane, including after an update.
  await Promise.allSettled([
    get('/api/status', { timeout: 6000, retryAuth: false }).then(status => observe('status', status)),
    get('/api/ping', { timeout: 6000, retryAuth: false }).then(serverInfo => observe('serverInfo', serverInfo)),
  ]);
});
function observe(key, value) {
  if (value?.ok === false) return;
  if (JSON.stringify(app.get()[key]) !== JSON.stringify(value)) app.set({ [key]: value });
}
export const refreshHardware = singleFlight(async () => {
  if (document.hidden) return;
  try { const gpus = await get('/api/vram', { timeout: 6000, retryAuth: false }); if (Array.isArray(gpus)) observe('gpus', gpus); } catch (_) {}
});
export const refreshWorkspace = singleFlight(async () => {
  try { const w = await get('/api/workspace'); if (w.ok) app.set({ workspace: w }); return w; } catch (_) { return null; }
});
export const refreshLibrary = singleFlight(async () => {
  try {
    const [models, presets] = await Promise.all([get('/api/models'), get('/api/presets')]);
    app.set({ models: models || [], presets: presets || [] });
  } catch (_) {}
});
export async function refreshNav() {
  try {
    const [hist, unified] = await Promise.all([get('/api/chat/history'), get('/api/runtime/sessions')]);
    const sessions = unified.ok ? unified.sessions : [];
    const imported = new Set(sessions.flatMap(s => [s.source_archive, s.native_archive]).filter(Boolean));
    // Une discussion n'apparaît qu'après son premier message.
    const conversations = [...sessions.filter(s => s.message_count > 0).map(s => ({ ...s, workspace: true })), ...((hist && hist.conversations) || []).filter(c => !imported.has(c.id) && c.turns > 0)];
    const bound = sessions.find(s => s.native_archive === hist.active && s.runtime_id === 'llama.cpp');
    app.set({ nav: { conversations, projects: hist.projects || [], active: bound ? bound.id : hist.active || '', projectId: hist.project_id || '' } });
  } catch (_) {}
}

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
  refreshEngineNode(); refreshWorkspace(); refreshNav(); refreshLibrary();
  const stops = [visibleRefresh(refreshStatus, 4000), visibleRefresh(refreshHardware, 4000)];
  stopPolling = () => { stops.forEach(stop => stop()); stopPolling = null; };
  return stopPolling;
}
