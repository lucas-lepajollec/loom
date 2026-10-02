import { t } from './i18n.js';
// État global de l'application : route, moteur, matériel, espace de travail,
// liste des discussions. Les pages lisent ce store ; les actions l'actualisent.
import { createStore } from './lib.js';
import { get } from './api.js';

export const app = createStore({
  route: parseRoute(),
  theme: localStorage.getItem('loom-theme') || 'dark',
  sideOpen: false, palette: false, inspector: innerWidth > 1100 && localStorage.getItem('loom.next.insp') !== '0',
  status: null, gpus: [], ram: null, engineNode: null,
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

export async function refreshStatus() {
  try { app.set({ status: await get('/api/status') }); } catch (_) {}
}
export async function refreshHardware() {
  if (document.hidden) return;
  try { app.set({ gpus: (await get('/api/vram')) || [] }); } catch (_) {}
}
export async function refreshWorkspace() {
  try { const w = await get('/api/workspace'); if (w.ok) app.set({ workspace: w }); return w; } catch (_) { return null; }
}
export async function refreshLibrary() {
  try {
    const [models, presets] = await Promise.all([get('/api/models'), get('/api/presets')]);
    app.set({ models: models || [], presets: presets || [] });
  } catch (_) {}
}
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

export function startPolling() {
  refreshEngineNode();
  refreshStatus(); refreshHardware(); refreshWorkspace(); refreshNav(); refreshLibrary();
  setInterval(refreshStatus, 4000);
  setInterval(refreshHardware, 4000);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) { refreshStatus(); refreshHardware(); } });
}
