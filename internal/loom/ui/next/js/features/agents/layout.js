// Disposition de l'espace de travail, à la manière de tmux : une session seule
// ou une grille de 2 à 6 volets. Chaque volet garde sa session ; l'un d'eux a
// le focus et reçoit le clavier. Tout reste sur cet appareil (identifiants et
// tailles seulement, jamais de contenu).
import { createStore } from '../../core/lib.js';
import { agents, stateOf, sessionList } from './store.js';

export const COUNTS = [1, 2, 3, 4, 6];
// Formes : 3 volets = un grand à gauche et deux empilés (main-vertical de tmux).
export const SHAPES = {
  1: { cols: 1, rows: 1, areas: [[0]] },
  2: { cols: 2, rows: 1, areas: [[0, 1]] },
  3: { cols: 2, rows: 2, areas: [[0, 1], [0, 2]] },
  4: { cols: 2, rows: 2, areas: [[0, 1], [2, 3]] },
  6: { cols: 3, rows: 2, areas: [[0, 1, 2], [3, 4, 5]] },
};

const KEY = 'loom.codeman.layout';
const VIEW_KEY = 'loom.codeman.views';
const read = (k, fallback) => { try { const v = JSON.parse(localStorage.getItem(k) || 'null'); return v ?? fallback; } catch (_) { return fallback; } };
const write = (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch (_) {} };

function sanitize(v) {
  const count = COUNTS.includes(v?.count) ? v.count : 1;
  const cells = Array.from({ length: count }, (_, i) => (Array.isArray(v?.cells) && typeof v.cells[i] === 'string' ? v.cells[i] : null));
  const shape = SHAPES[count];
  const fr = (a, n) => Array.isArray(a) && a.length === n && a.every(x => Number.isFinite(x) && x > 0) ? a : Array(n).fill(1);
  return {
    count, cells, focus: Math.min(Math.max(0, v?.focus | 0), count - 1), zoom: !!v?.zoom && count > 1,
    colFr: fr(v?.colFr, shape.cols), rowFr: fr(v?.rowFr, shape.rows),
    last: COUNTS.includes(v?.last) && v.last > 1 ? v.last : 4,
  };
}

// Les cases vides sont des null : le schéma par défaut des stores les refuserait.
const list = v => Array.isArray(v);
export const layout = createStore(sanitize(read(KEY, null)), { cells: list, colFr: list, rowFr: list });
const commit = patch => { const next = sanitize({ ...layout.get(), ...patch }); layout.set(next); write(KEY, next); };

export const gridOn = () => layout.get().count > 1;
export const focusedId = () => { const l = layout.get(); return l.count > 1 ? l.cells[l.focus] : null; };

// Vue de chaque session (conversation, terminal, côte à côte), partagée par
// la vue seule et les volets.
const views = read(VIEW_KEY, {});
export const viewOf = (s, fallback) => views[s.id] || fallback || (s.mode === 'shell' ? 'term' : 'chat');
export function setViewOf(id, v) { views[id] = v; const keys = Object.keys(views); if (keys.length > 200) delete views[keys[0]]; write(VIEW_KEY, views); }

// Les sessions qui méritent un volet : celles qui travaillent, puis celles qui
// attendent quelqu'un, puis les plus récemment actives ; les arrêtées en dernier.
const RANK = { working: 0, waiting: 1, starting: 2, error: 3, idle: 4, stopped: 5, gone: 6 };
export function ranked() {
  return sessionList().map((s, i) => ({ s, i })).sort((a, b) => {
    const r = RANK[stateOf(a.s)] - RANK[stateOf(b.s)];
    return r || (b.s.lastActivityAt || 0) - (a.s.lastActivityAt || 0) || a.i - b.i;
  }).map(x => x.s.id);
}

// Change le nombre de volets : le volet qui a le focus reste, les autres
// gardent leur place si elle existe encore, les trous se remplissent par rang.
export function setCount(count, currentId) {
  const l = layout.get();
  if (count === l.count) return;
  if (count === 1) { commit({ count: 1, cells: [currentId || l.cells[l.focus] || null], focus: 0, zoom: false, last: l.count > 1 ? l.count : l.last }); return; }
  const keep = l.count > 1 ? l.cells.filter(Boolean) : [];
  const first = currentId || (l.count > 1 ? l.cells[l.focus] : null);
  const ids = [...new Set([first, ...keep].filter(id => id && agents.get().sessions[id]))];
  for (const id of ranked()) { if (ids.length >= count) break; if (!ids.includes(id) && stateOf(agents.get().sessions[id]) !== 'stopped') ids.push(id); }
  commit({ count, cells: Array.from({ length: count }, (_, i) => ids[i] || null), focus: 0, zoom: false, colFr: null, rowFr: null, last: count });
}
export const toggleGrid = currentId => setCount(gridOn() ? 1 : layout.get().last, currentId);

// Ouvre une session dans la grille : elle prend le focus si elle y est déjà,
// sinon la première case vide, sinon le volet qui a le focus.
export function placeSession(id, cell) {
  const l = layout.get();
  if (l.count === 1) { commit({ cells: [id] }); return; }
  const cells = [...l.cells];
  const at = cells.indexOf(id);
  if (cell == null && at >= 0) { commit({ focus: at }); return; }
  const target = cell != null ? cell : cells.indexOf(null) >= 0 ? cells.indexOf(null) : l.focus;
  if (at >= 0) cells[at] = cells[target]; // glissé d'un volet sur un autre : échange
  cells[target] = id;
  commit({ cells, focus: target, zoom: l.zoom && target === l.focus });
}
export const focusCell = i => { const l = layout.get(); if (i !== l.focus) commit({ focus: i, zoom: false }); };
export const toggleZoom = () => { const l = layout.get(); if (l.count > 1) commit({ zoom: !l.zoom }); };
export function clearCell(i) {
  const l = layout.get(); const cells = [...l.cells]; cells[i] = null;
  commit({ cells, zoom: l.zoom && i !== l.focus });
}
export const setFractions = (colFr, rowFr) => commit({ colFr, rowFr });
// Une session disparue libère son volet.
export function pruneCells() {
  const l = layout.get(); const st = agents.get();
  if (!st.loaded) return;
  if (l.cells.some(id => id && !st.sessions[id])) commit({ cells: l.cells.map(id => (id && st.sessions[id] ? id : null)) });
}

// Volet voisin dans une direction, en sautant les cases vides.
export function neighbor(dir) {
  const l = layout.get(); const { areas } = SHAPES[l.count];
  const pos = []; areas.forEach((row, r) => row.forEach((cell, c) => { (pos[cell] = pos[cell] || []).push([r, c]); }));
  const [dr, dc] = { left: [0, -1], right: [0, 1], up: [-1, 0], down: [1, 0] }[dir];
  for (const [r0, c0] of pos[l.focus] || []) {
    for (let r = r0 + dr, c = c0 + dc; r >= 0 && c >= 0 && r < areas.length && c < areas[0].length; r += dr, c += dc) {
      const cell = areas[r][c];
      if (cell !== l.focus && l.cells[cell]) return cell;
    }
  }
  return -1;
}
