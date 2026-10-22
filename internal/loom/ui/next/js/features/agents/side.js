// Sessions Codeman dans la barre latérale de Loom, à côté des projets et des
// discussions : l'espace central reste aux conversations et aux terminaux.
// La section n'apparaît qu'une fois Codeman configuré.
import { t } from '../../core/i18n.js';
import { html, useState, useStore, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { confirm, toast } from '../../ui/dialog.js';
import { app } from '../../core/state.js';
import { agents, stateOf, projectOf, sessionList, useAgentsPresence } from './store.js';
import { layout, placeSession } from './layout.js';
import { resumeSession, resumeSupport } from './actions.js';
import { CliMark, NewSession } from './new-session.js';
import { openSession } from './nav.js';
import { lt } from './labels.js';
import { DRAG_TYPE } from './grid.js';

const pref = (k, d) => { try { const v = localStorage.getItem(k); return v === null ? d : v === '1'; } catch (_) { return d; } };
const savePref = (k, v) => { try { localStorage.setItem(k, v ? '1' : '0'); } catch (_) {} };

function Row({ s, current, inGrid }) {
  const state = stateOf(s);
  const busy = state === 'working' || state === 'starting';
  return html`<a class=${cls('chat-link cm-side-row', 'st-' + state, inGrid && 'in-grid')} href=${'#/agents/s/' + encodeURIComponent(s.id)} aria-current=${current ? 'page' : undefined}
      draggable="true" title=${s.workingDir || ''}
      onDragStart=${e => { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData(DRAG_TYPE, s.id); }}
      onClick=${e => { if (e.metaKey || e.ctrlKey || e.button) return; e.preventDefault(); openSession(s.id); app.set({ sideOpen: false }); }}>
    <${CliMark} mode=${s.mode} size="xs" />
    <span>${s.name || s.id.slice(0, 8)}</span>
    ${state === 'waiting' ? html`<em class="cm-side-flag" title=${lt('state', state)}>${t('agents.side.waiting')}</em>`
      : busy ? html`<i class="run" title=${lt('state', state)}></i>`
      : state === 'error' ? html`<i class="dot red" title=${lt('state', state)}></i>` : ''}
  </a>`;
}

function HistoryRow({ r }) {
  const [busy, setBusy] = useState(false);
  const resume = async () => {
    if (busy) return;
    const support = resumeSupport(r);
    const text = support === 'none' ? t('agents.history.no_resume', { cli: r.mode }) : support === 'latest' ? t('agents.history.resume_latest') : t('agents.history.resume_exact');
    if (!await confirm(t('agents.history.resume_title'), text, { ok: support === 'none' ? t('agents.history.new_here') : t('agents.history.resume') })) return;
    setBusy(true);
    try { openSession(await resumeSession(r)); } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  return html`<button class="chat-link sub cm-side-hist" disabled=${busy} onClick=${resume} title=${r.workingDir}>
    <${CliMark} mode=${r.mode || 'claude'} size="xs" />
    <span>${r.name || (r.firstPrompt ? r.firstPrompt.slice(0, 60) : r.sessionId.slice(0, 8))}</span>
    ${busy ? html`<span class="spinner"></span>` : html`<small>${projectOf(r.workingDir)}</small>`}</button>`;
}

// Fenêtre « nouvelle session », ouverte depuis la barre latérale, la page ou
// un volet vide de la grille (qui reçoit alors la session).
function NewSessionHost() {
  const creating = useStore(agents, s => s.creating);
  useEffect(() => {
    const key = e => { if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'n' && agents.get().conn.state === 'ready') { e.preventDefault(); agents.set({ creating: {} }); } };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
  if (!creating) return null;
  const cell = creating.cell;
  return html`<${NewSession} preset=${creating.preset} onClose=${() => agents.set({ creating: null })}
    onCreated=${id => { if (cell != null) placeSession(id, cell); openSession(id); }} />`;
}

export function AgentsSide() {
  useAgentsPresence();
  const conn = useStore(agents, s => s.conn);
  const route = useStore(app, s => s.route);
  useStore(agents, s => [s.order, s.sessions, s.approvals]);
  const unified = useStore(agents, s => s.unified);
  const grid = useStore(layout, l => (l.count > 1 ? l : null));
  const [open, setOpen] = useState(() => pref('loom.side.agents', true));
  const [hist, setHist] = useState(() => pref('loom.side.agents.hist', false));
  if (!conn.configured) return html`<${NewSessionHost} />`;
  const ready = conn.state === 'ready';
  const list = sessionList();
  const ids = new Set(list.map(s => s.id));
  const history = unified.filter(r => !(r.sources || []).includes('live') && !ids.has(r.sessionId))
    .sort((a, b) => (b.lastActivityAt || 0) - (a.lastActivityAt || 0)).slice(0, 8);
  const onAgents = route.section === 'agents';
  const current = onAgents ? (grid && innerWidth > 760 ? grid.cells[grid.focus] : route.sub === 's' ? route.id : '') : '';
  const waiting = list.filter(s => stateOf(s) === 'waiting').length;
  const toggle = () => { setOpen(!open); savePref('loom.side.agents', !open); };
  return html`<div class="side-sec side-agents">
    <div class="side-sec-h">
      <button class="side-sec-t" aria-expanded=${String(open)} onClick=${toggle}>
        <span>${t('agents.side.title')}</span>${waiting > 0 && html`<em class="cm-side-count">${waiting}</em>`}
        ${!ready && html`<i class="dot amber" title=${lt('conn', conn.state)}></i>`}<${Icon} n=${open ? 'chevron' : 'right'} class="proj-caret" /></button>
      <button class="icon-btn" style="width:24px;height:24px" aria-label=${t('agents.rail.new')} title=${t('agents.side.new_hint')} disabled=${!ready}
        onClick=${() => agents.set({ creating: {} })}><${Icon} n="plus" /></button>
    </div>
    ${open && html`<div class="side-list anim-fade">
      ${!ready ? html`<a class="chat-link sub" href="#/agents"><span>${lt('conn', conn.state)}</span></a>`
        : !list.length ? html`<button class="chat-link sub" onClick=${() => agents.set({ creating: {} })}><${Icon} n="plus" /><span>${t('agents.side.empty')}</span></button>`
        : list.map(s => html`<${Row} key=${s.id} s=${s} current=${s.id === current} inGrid=${!!grid && grid.cells.includes(s.id)} />`)}
      ${ready && history.length > 0 && html`<button class="chat-link sub cm-side-more" aria-expanded=${String(hist)} onClick=${() => { setHist(!hist); savePref('loom.side.agents.hist', !hist); }}>
        <${Icon} n="history" /><span>${t('agents.rail.history')}</span><${Icon} n=${hist ? 'chevron' : 'right'} class="proj-caret" /></button>`}
      ${ready && hist && history.map(r => html`<${HistoryRow} key=${r.sessionId} r=${r} />`)}
    </div>`}
    <${NewSessionHost} />
  </div>`;
}
