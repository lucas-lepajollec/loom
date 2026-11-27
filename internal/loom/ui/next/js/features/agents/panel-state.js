// État partagé du panneau latéral d'agent : onglet actif, largeur
// redimensionnable persistée, fichiers ouverts en aperçu et documents cités
// par l'agent dans le fil de conversation.
import { createStore } from '../../core/lib.js';
import {
  DEFAULT_PANEL_WIDTH, MIN_PANEL_WIDTH, MAX_PANEL_WIDTH,
  clampPanelWidth, isMarkdownPath, normalizeSessionPath, extractMentionedFiles,
} from './panel-helpers.js';

export {
  DEFAULT_PANEL_WIDTH, MIN_PANEL_WIDTH, MAX_PANEL_WIDTH,
  clampPanelWidth, isMarkdownPath, normalizeSessionPath, extractMentionedFiles,
};

const WIDTH_KEY = 'loom.codeman.inspWidth';

function loadWidth() {
  try {
    const raw = localStorage.getItem(WIDTH_KEY);
    if (!raw) return DEFAULT_PANEL_WIDTH;
    return clampPanelWidth(Number(raw));
  } catch (_) {
    return DEFAULT_PANEL_WIDTH;
  }
}

export function setLivePanelWidth(w) {
  const clamped = clampPanelWidth(w);
  if (panelState.get().width !== clamped) panelState.set({ width: clamped });
  return clamped;
}

export function savePanelWidth(w) {
  const clamped = clampPanelWidth(w);
  panelState.set({ width: clamped });
  try { localStorage.setItem(WIDTH_KEY, String(clamped)); } catch (_) {}
  return clamped;
}

export const panelState = createStore({
  tab: 'session',
  width: loadWidth(),
  // Par sessionId : { active: string, recent: string[], detected: string[] }
  previews: {},
  // Incrémenté quand la conversation demande d'ouvrir le panneau sur un aperçu.
  openRequest: 0,
}, {
  previews: v => !!v && typeof v === 'object' && !Array.isArray(v),
});

export function previewFor(state, sessionId) {
  return (state.previews && sessionId && state.previews[sessionId]) || EMPTY_PREVIEW;
}
const EMPTY_PREVIEW = { active: '', recent: [], detected: [] };

export function setPanelTab(tab) {
  if (panelState.get().tab !== tab) panelState.set({ tab });
}

export function openPanelTab(tab) {
  const st = panelState.get();
  panelState.set({ tab, openRequest: st.openRequest + 1 });
}

export function openPreview(sessionId, rawPath, { openPanel = true } = {}) {
  if (!sessionId || !rawPath) return;
  const path = String(rawPath).trim();
  if (!path) return;
  const st = panelState.get();
  const cur = st.previews[sessionId] || EMPTY_PREVIEW;
  const recent = [path, ...cur.recent.filter(p => p !== path)].slice(0, 12);
  const next = { ...st.previews, [sessionId]: { ...cur, active: path, recent } };
  const width = st.width < 460 ? clampPanelWidth(480) : st.width;
  if (width !== st.width) {
    try { localStorage.setItem(WIDTH_KEY, String(width)); } catch (_) {}
  }
  panelState.set({
    tab: 'preview',
    width,
    previews: next,
    ...(openPanel ? { openRequest: st.openRequest + 1 } : {}),
  });
}

export function closePreview(sessionId) {
  const st = panelState.get();
  const cur = st.previews[sessionId];
  const next = cur ? { ...st.previews, [sessionId]: { ...cur, active: '' } } : st.previews;
  panelState.set({
    tab: st.tab === 'preview' ? 'session' : st.tab,
    previews: next,
  });
}

export function setDetectedDocs(sessionId, paths) {
  if (!sessionId) return;
  const list = Array.isArray(paths) ? paths.slice(0, 8) : [];
  const st = panelState.get();
  const cur = st.previews[sessionId] || EMPTY_PREVIEW;
  if (cur.detected.length === list.length && cur.detected.every((p, i) => p === list[i])) return;
  panelState.set({
    previews: { ...st.previews, [sessionId]: { ...cur, detected: list } },
  });
}
