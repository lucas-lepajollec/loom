// Grille de sessions, à la manière de tmux : 2 à 6 volets côte à côte, chacun
// avec sa conversation ou son vrai terminal. Séparateurs déplaçables, un volet
// qui a le focus (clavier), zoom sur un volet, glisser-déposer pour ranger.
// Raccourcis : Alt+Maj+flèches (focus), Alt+Maj+Z (zoom), Ctrl+Maj+G (grille).
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { agents, stateOf, projectOf, sessionList } from './store.js';
import { layout, SHAPES, focusCell, toggleZoom, clearCell, placeSession, setFractions, neighbor, viewOf, setViewOf, pruneCells } from './layout.js';
import { AgentTerminal } from './terminal.js';
import { Conversation } from './conversation.js';
import { StateDot } from './rail.js';
import { CliMark } from './new-session.js';
import { lt } from './labels.js';

export const DRAG_TYPE = 'application/x-loom-agent';
const MIN_PX = 220;

// Position d'un volet dans la grille, séparateurs compris (une piste de 6 px
// entre deux colonnes ou deux rangées).
function placement(areas, cell) {
  let r0 = Infinity, r1 = -1, c0 = Infinity, c1 = -1;
  areas.forEach((row, r) => row.forEach((v, c) => { if (v === cell) { r0 = Math.min(r0, r); r1 = Math.max(r1, r); c0 = Math.min(c0, c); c1 = Math.max(c1, c); } }));
  return `grid-row:${2 * r0 + 1}/${2 * r1 + 2};grid-column:${2 * c0 + 1}/${2 * c1 + 2}`;
}

function dropProps(i, setOver) {
  return {
    onDragOver: e => { if ([...e.dataTransfer.types].includes(DRAG_TYPE)) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; setOver(true); } },
    onDragLeave: e => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(false); },
    onDrop: e => { setOver(false); const id = e.dataTransfer.getData(DRAG_TYPE); if (id) { e.preventDefault(); placeSession(id, i); } },
  };
}

function Tile({ s, i, focused, hidden }) {
  const state = stateOf(s);
  const zoom = useStore(layout, l => l.zoom);
  const [view, setView] = useState(() => { const v = viewOf(s); return v === 'split' ? 'chat' : v; });
  const [over, setOver] = useState(false);
  const pick = v => { setView(v); setViewOf(s.id, v); };
  const term = view === 'term' || s.mode === 'shell';
  return html`<section class=${cls('cm-tile', 'st-' + state, focused && 'focused', over && 'drop-on')} style=${hidden ? 'display:none' : ''}
      onPointerDownCapture=${() => focusCell(i)} ...${dropProps(i, setOver)} aria-label=${s.name || s.id.slice(0, 8)}>
    <header class="cm-tile-h" draggable="true" onDblClick=${toggleZoom}
        onDragStart=${e => { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData(DRAG_TYPE, s.id); }}>
      <${StateDot} state=${state} /><${CliMark} mode=${s.mode} size="xs" />
      <span class="cm-tile-t trunc"><b>${s.name || s.id.slice(0, 8)}</b><small>${state === 'idle' ? projectOf(s.workingDir) : lt('state', state)}${s.displayModel?.model ? ' · ' + s.displayModel.model : ''}</small></span>
      ${s.mode !== 'shell' && html`<button class="icon-btn" aria-label=${term ? t('agents.view.chat') : t('agents.view.terminal')} title=${term ? t('agents.view.chat') : t('agents.view.terminal')}
        onClick=${() => pick(term ? 'chat' : 'term')}><${Icon} n=${term ? 'chat' : 'terminal'} /></button>`}
      <button class="icon-btn" aria-label=${zoom && focused ? t('agents.grid.unzoom') : t('agents.grid.zoom')} title=${zoom && focused ? t('agents.grid.unzoom') : t('agents.grid.zoom_hint')}
        onClick=${() => { focusCell(i); toggleZoom(); }}><${Icon} n=${zoom && focused ? 'shrink' : 'expand'} /></button>
      <button class="icon-btn" aria-label=${t('agents.grid.remove')} title=${t('agents.grid.remove')} onClick=${() => clearCell(i)}><${Icon} n="close" /></button>
    </header>
    <div class="cm-tile-b">${term ? html`<${AgentTerminal} key=${'t' + s.id} sessionId=${s.id} mode=${s.mode} shell=${s.mode === 'shell'} scrollback=${10000} fontSize=${12.5} focus=${focused} />`
      : html`<${Conversation} key=${'c' + s.id} session=${s} compact onTerminal=${() => pick('term')} />`}</div>
  </section>`;
}

function EmptyCell({ i, focused, hidden, shown }) {
  const [over, setOver] = useState(false);
  useStore(agents, s => s.order);
  const free = sessionList().filter(s => !shown.includes(s.id)).slice(0, 6);
  return html`<section class=${cls('cm-tile cm-tile-empty', focused && 'focused', over && 'drop-on')} style=${hidden ? 'display:none' : ''}
      onPointerDownCapture=${() => focusCell(i)} ...${dropProps(i, setOver)}>
    <div class="cm-tile-free">
      <p>${t('agents.grid.free')}</p>
      ${free.length > 0 && html`<div class="cm-tile-pick">${free.map(s => html`<button key=${s.id} onClick=${() => placeSession(s.id, i)}><${StateDot} state=${stateOf(s)} /><${CliMark} mode=${s.mode} size="xs" /><span class="trunc">${s.name || s.id.slice(0, 8)}</span></button>`)}</div>`}
      <button class="btn sm" onClick=${() => agents.set({ creating: { cell: i } })}><${Icon} n="plus" />${t('agents.rail.new')}</button>
    </div>
  </section>`;
}

// Séparateur déplaçable : ajuste la part des deux colonnes (ou rangées)
// voisines, en gardant chaque volet lisible.
function Divider({ axis, index, style, box, onLive }) {
  const down = e => {
    e.preventDefault();
    const l = layout.get();
    const fr = [...(axis === 'col' ? l.colFr : l.rowFr)];
    const rect = box.current.getBoundingClientRect();
    const span = (axis === 'col' ? rect.width : rect.height) - 6 * (fr.length - 1) - 8;
    const sum = fr.reduce((a, b) => a + b, 0);
    const start = axis === 'col' ? e.clientX : e.clientY;
    const min = sum * Math.min(0.45, MIN_PX / span);
    let next = fr;
    const move = ev => {
      const d = ((axis === 'col' ? ev.clientX : ev.clientY) - start) * sum / span;
      const pair = fr[index] + fr[index + 1];
      const a = Math.max(min, Math.min(pair - min, fr[index] + d));
      next = [...fr]; next[index] = a; next[index + 1] = pair - a;
      onLive(axis, next);
    };
    const up = () => {
      removeEventListener('pointermove', move); removeEventListener('pointerup', up);
      document.body.classList.remove('cm-dragging');
      const cur = layout.get();
      setFractions(axis === 'col' ? next : cur.colFr, axis === 'row' ? next : cur.rowFr);
      onLive(axis, null);
    };
    document.body.classList.add('cm-dragging');
    addEventListener('pointermove', move); addEventListener('pointerup', up);
  };
  const reset = () => { const l = layout.get(); setFractions(axis === 'col' ? l.colFr.map(() => 1) : l.colFr, axis === 'row' ? l.rowFr.map(() => 1) : l.rowFr); };
  return html`<div class=${cls('cm-div', axis)} style=${style} role="separator" aria-orientation=${axis === 'col' ? 'vertical' : 'horizontal'}
    title=${t('agents.grid.divider')} onPointerDown=${down} onDblClick=${reset}><i></i></div>`;
}

export function Grid() {
  const l = useStore(layout, x => x);
  const loaded = useStore(agents, s => s.loaded);
  useStore(agents, s => [s.order, s.sessions, s.approvals]);
  const sessions = agents.get().sessions;
  const box = useRef();
  const [live, setLive] = useState({ col: null, row: null });
  useEffect(() => { pruneCells(); }, [loaded, Object.keys(sessions).length]);
  useEffect(() => {
    const key = e => {
      if (!e.altKey || !e.shiftKey || e.ctrlKey || e.metaKey) return;
      const dir = { ArrowLeft: 'left', ArrowRight: 'right', ArrowUp: 'up', ArrowDown: 'down' }[e.key];
      if (dir) { e.preventDefault(); e.stopPropagation(); const n = neighbor(dir); if (n >= 0) focusCell(n); }
      else if (e.code === 'KeyZ') { e.preventDefault(); e.stopPropagation(); toggleZoom(); }
    };
    addEventListener('keydown', key, true); return () => removeEventListener('keydown', key, true);
  }, []);
  const shape = SHAPES[l.count];
  const colFr = live.col || l.colFr, rowFr = live.row || l.rowFr;
  const tracks = fr => fr.map(f => `minmax(0,${f}fr)`).join(' 6px ');
  const onLive = (axis, v) => setLive(cur => ({ ...cur, [axis]: v }));
  const shown = l.cells.filter(Boolean);
  const zoomed = l.zoom && l.cells[l.focus];
  const dividers = [];
  if (!zoomed) {
    for (let c = 0; c < shape.cols - 1; c++) dividers.push(html`<${Divider} key=${'c' + c} axis="col" index=${c} box=${box} onLive=${onLive} style=${`grid-column:${2 * c + 2};grid-row:1/-1`} />`);
    for (let r = 0; r < shape.rows - 1; r++) for (let c = 0; c < shape.cols; c++) {
      if (shape.areas[r][c] !== shape.areas[r + 1][c]) dividers.push(html`<${Divider} key=${'r' + r + c} axis="row" index=${r} box=${box} onLive=${onLive} style=${`grid-row:${2 * r + 2};grid-column:${2 * c + 1}`} />`);
    }
  }
  return html`<div class=${cls('cm-grid', zoomed && 'zoomed')} ref=${box} style=${zoomed ? '' : `grid-template-columns:${tracks(colFr)};grid-template-rows:${tracks(rowFr)}`}>
    ${l.cells.map((id, i) => {
      const s = id && sessions[id];
      const hidden = zoomed && i !== l.focus;
      const style = zoomed ? '' : placement(shape.areas, i);
      return html`<div key=${'cell' + i} class="cm-cell" style=${hidden ? 'display:none' : style}>
        ${s ? html`<${Tile} key=${s.id} s=${s} i=${i} focused=${i === l.focus} />` : html`<${EmptyCell} i=${i} focused=${i === l.focus} shown=${shown} />`}</div>`;
    })}
    ${dividers}
  </div>`;
}

// Choix de la disposition : vue seule ou grille de 2, 3, 4 ou 6 volets.
const SHAPE_ICON = {
  1: [[0, 0, 16, 12]],
  2: [[0, 0, 7.5, 12], [8.5, 0, 7.5, 12]],
  3: [[0, 0, 7.5, 12], [8.5, 0, 7.5, 5.5], [8.5, 6.5, 7.5, 5.5]],
  4: [[0, 0, 7.5, 5.5], [8.5, 0, 7.5, 5.5], [0, 6.5, 7.5, 5.5], [8.5, 6.5, 7.5, 5.5]],
  6: [[0, 0, 4.67, 5.5], [5.67, 0, 4.67, 5.5], [11.33, 0, 4.67, 5.5], [0, 6.5, 4.67, 5.5], [5.67, 6.5, 4.67, 5.5], [11.33, 6.5, 4.67, 5.5]],
};
export const ShapeIcon = ({ n }) => html`<svg class="cm-shape" viewBox="-1 -1 18 14" aria-hidden="true">${SHAPE_ICON[n].map(([x, y, w, h]) => html`<rect x=${x} y=${y} width=${w} height=${h} rx="1.4" />`)}</svg>`;
