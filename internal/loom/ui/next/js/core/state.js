import { t } from './i18n.js';
// État global de l'application : route, moteur, matériel, espace de travail,
// liste des discussions. Les pages lisent ce store ; les actions l'actualisent.
import { createStore } from './lib.js';
import { get } from './api.js';
import { visibleRefresh, singleFlight } from './poll.js';
import { validShape, validData } from './shape.js';
import { isPhone } from './viewport.js';

const CHAT_MODE_SECTIONS = new Set(['chat', 'local', 'cloud', 'voice', 'jarvis', 'engine']);
const AGENT_MODE_SECTIONS = new Set(['agents', 'machines', 'files', 'connections', 'terminals']);
function modeForRoute(route, fallback) {
  const sec = route?.section || '';
  if (AGENT_MODE_SECTIONS.has(sec)) return 'agents';
  if (CHAT_MODE_SECTIONS.has(sec)) return 'chat';
  return fallback === 'agents' ? 'agents' : 'chat';
}
function savedWorkspaceMode() {
  try { return localStorage.getItem('loom.workspaceMode') === 'agents' ? 'agents' : 'chat'; } catch (_) { return 'chat'; }
}

const initialRoute = parseRoute();
const observation = url => value => value === null || validData(value, url);
export const app = createStore({
  route: initialRoute,
  workspaceMode: modeForRoute(initialRoute, savedWorkspaceMode()),
  theme: localStorage.getItem('loom-theme') || 'dark',
  palette: false, inspector: false, phoneDraft: null, phoneChatRevision: 0,
  status: null, serverInfo: null, gpus: [], ram: null, engineNode: null,
  workspace: null, presets: [], models: [],
  nav: { conversations: [], projects: [], active: '' }, unavailable: {},
}, { models: v => validData(v, '/api/models'), presets: v => validData(v, '/api/presets'), gpus: v => validData(v, '/api/vram'), status: observation('/api/status'), serverInfo: observation('/api/ping'), ram: observation('/api/ram'), engineNode: value => value === null || typeof value?.remote === 'boolean' && validData(value, '/api/engine/node'),
  workspace: observation('/api/workspace'), nav: { conversations: [], projects: [], active: '' }, unavailable: {}, voiceMode: value => value === null || validShape(value, { discussion: '', internet: false }), welcome: false, newPreset: false });

export function setWorkspaceMode(mode) {
  const next = mode === 'agents' ? 'agents' : 'chat';
  try { localStorage.setItem('loom.workspaceMode', next); } catch (_) {}
  if (app.get().workspaceMode !== next) app.set({ workspaceMode: next });
}

// ---------- routes : #/section/sous-section/id ----------
export function parseRoute() {
  const [section, sub, id] = location.hash.replace(/^#\/?/, '').split('/').map(part => {
    try { return decodeURIComponent(part); } catch (_) { return part; }
  });
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
  return { section: section || (isPhone() ? 'sessions' : 'chat'), sub: sub || '', id: id || '' };
}
export function go(section, sub, id) {
  const hash = '#/' + [section, sub, id].filter(Boolean).map(encodeURIComponent).join('/');
  if (location.hash !== hash) location.hash = hash; else app.set({ route: parseRoute() });
}
function markBack(available) {
  if (history.state?.loomBack === undefined) history.replaceState({ ...history.state, loomBack: available }, '');
}
if (isPhone()) markBack(false);
addEventListener('hashchange', () => {
  const route = parseRoute();
  if (isPhone()) markBack(true);
  const phoneDraft = route.section === 'chat' && !route.sub ? app.get().phoneDraft : null;
  const workspaceMode = modeForRoute(route, app.get().workspaceMode);
  try { localStorage.setItem('loom.workspaceMode', workspaceMode); } catch (_) {}
  app.set({ route, workspaceMode, phoneDraft, inspector: isPhone() ? false : app.get().inspector,
    voiceMode: route.section === 'chat' ? app.get().voiceMode ?? null : null });
});
globalThis.matchMedia?.('(max-width: 760px)').addEventListener('change', () => {
  if (!location.hash) app.set({ route: parseRoute() });
});

export function enterPhoneChat(draft) {
  if (location.hash !== '#/chat') {
    markBack(false);
    history.pushState({ loomBack: true }, '', '#/chat');
  }
  app.set({ route: parseRoute(), phoneDraft: draft, phoneChatRevision: app.get().phoneChatRevision + 1 });
}

export function phoneTab({ section, sub, id }) {
  if (section === 'sessions' || section === 'inbox') return section;
  if (section === 'files' && !id) return 'files';
  if (section === 'machines' && !sub) return 'machines';
  if (['quotas', 'usage', 'market'].includes(section)) return 'usage';
  return '';
}

export function phoneDrawerSection({ section, sub }) {
  if (section === 'settings' && sub) return false;
  if (section === 'agents' && ['s', 'grid', 'new'].includes(sub)) return false;
  return ['local', 'cloud', 'voice', 'jarvis', 'engine', 'agents', 'connections', 'settings'].includes(section);
}

export function phoneParent({ section, sub }) {
  if (section === 'machines' && sub) return 'machines';
  if (section === 'settings' && sub) return 'settings';
  return '';
}

export function phoneBack() {
  const route = app.get().route;
  const destination = phoneParent(route);
  if (destination) return go(destination);
  if (history.state?.loomBack) return history.back();
  const { section, sub, id } = route;
  const session = section === 'chat' || (section === 'agents' && ['s', 'grid'].includes(sub));
  const parent = session ? 'sessions' : section === 'agents' && sub === 'new' ? 'agents' : section === 'terminals' ? 'machines' : section === 'files' && id ? 'files' : 'sessions';
  history.replaceState({ ...history.state, loomBack: false }, '', '#/' + parent);
  app.set({ route: parseRoute(), inspector: false, phoneDraft: null, voiceMode: null });
}

// ---------- thème ----------
export function setTheme(localT) {
  document.documentElement.dataset.theme = localT;
  try { localStorage.setItem('loom-theme', localT); } catch (_) {}
  const meta = document.querySelector('meta[name=theme-color]');
  if (meta) meta.content = localT === 'light' ? '#ffffff' : '#101012'; // fond des pages : la barre d'état s'y fond
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

// Nature d'un runtime (local, cloud ou archive) d'après le registre du serveur.
export function runtimeKind(id) {
  if (!id || id === 'llama.cpp') return 'local';
  if (id === 'openai-compatible') return 'cloud';
  const r = ((app.get().workspace && app.get().workspace.runtimes) || []).find(x => x.id === id);
  return r?.kind === 'cloud' ? 'cloud' : 'retired';
}

export const runtimeCaps = id => ((((app.get().workspace && app.get().workspace.runtimes) || []).find(x => x.id === id)) || {}).capabilities || [];

// État lisible du moteur local pour la carte et l'en-tête.
export function engineState(s) {
  if (!s) return { tone: '', label: t("core.state.lecture_de_l_etat") };
  if (s.load_error) return { tone: 'red', label: t("core.state.erreur_de_chargement"), detail: s.load_error };
  // Modèle chargé par une application via l'API /v1 (pas la sélection enregistrée).
  if (s.health && s.model && s.via_api) return { tone: 'green', label: t('core.state.via_api', { name: s.model_name }) };
  if (s.health && s.model) return { tone: 'green', label: s.preset_name || s.model_name };
  if (s.active && s.model) return { tone: 'amber', label: t("core.state.chargement") + (s.preset_name || s.model_name) };
  if (s.active) return { tone: 'blue', label: t("core.state.moteur_pret_aucun_modele") };
  return { tone: '', label: t("core.state.moteur_arrete") };
}

let stopPolling;
const ENGINE_SECTIONS = new Set(['chat', 'local', 'engine', 'machines', 'voice', 'jarvis']);
export function startPolling() {
  if (stopPolling) return stopPolling;

  // Quand le moteur local est arrêté et qu'on travaille sur une autre section
  // (sessions d'agent, fichiers, réglages…), inutile de sonder le GPU et l'état
  // toutes les 4 s : on relâche à 12 s tout en restant à 4 s dès qu'il tourne.
  let statusTicks = 0, hwTicks = 0;
  const fastPoll = () => {
    const s = app.get();
    return !s.status || s.status.active || !!s.status.model || ENGINE_SECTIONS.has(s.route?.section);
  };
  const tickStatus = () => {
    if (!fastPoll() && ++statusTicks % 3 !== 0) return;
    statusTicks = 0;
    return refreshStatus();
  };
  const tickHardware = () => {
    if (!fastPoll() && ++hwTicks % 3 !== 0) return;
    hwTicks = 0;
    return refreshHardware();
  };

  // A suspended initial load must recover without forcing a page reload.
  const stops = [visibleRefresh(tickStatus, 4000), visibleRefresh(tickHardware, 4000), visibleRefresh(() => Promise.allSettled([refreshEngineNode(), refreshWorkspace(), refreshNav(), refreshLibrary()]), 30000)];
  stopPolling = () => { stops.forEach(stop => stop()); stopPolling = null; };
  return stopPolling;
}

// Shared lists (agents, models, providers) follow any change made elsewhere.
if (typeof window !== 'undefined') window.addEventListener('loom:changed', () => { refreshWorkspace(); refreshNav(); refreshLibrary(); refreshEngineNode(); });
