// Module Agents : une interface Loom pour un serveur Codeman. Codeman reste
// le moteur (sessions, tmux, CLI natifs, orchestration) ; Loom affiche et
// pilote par son API officielle. Les sessions vivent dans la liste de la
// barre latérale ; la page Agents sert à gérer Codeman. Routes :
// #/agents (vue d'ensemble), #/agents/cron (tâches), #/agents/connections,
// #/agents/settings,
// #/agents/s/<id> (une session) et #/agents/grid (la mosaïque).
import { lt } from './labels.js';
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls, createStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Popover, Seg, Switch } from '../../ui/controls.js';
import { prompt, toast } from '../../ui/dialog.js';
import { Drawer } from '../../ui/drawer.js';
import { TabNav } from '../../ui/tabnav.js';
import { go } from '../../core/state.js';
import { isPhone, usePhone } from '../../core/viewport.js';
import { BackButton } from '../../app/phone-nav.js';
import { api } from './client.js';
import { agents, refreshConnection, stateOf, cliOf, projectOf, upsertSession, hasConversation } from './store.js';
import { renameSession } from './actions.js';
import { StateDot } from './rail.js';
import { layout, COUNTS, setCount, toggleGrid, placeSession, viewOf, setViewOf } from './layout.js';
import { Conversation } from './conversation.js';
import { AgentTerminal } from './terminal.js';
import { Inspector } from './inspector.js';
import { CliMark } from './new-session.js';
import { ControlCenter } from './control.js';
import { Grid, ShapeIcon } from './grid.js';
import { CodemanSettings } from './settings.js';
import { CronPage } from './cron.js';
import { ConnectionsPage } from './connections.js';
import { ConnectionPanel } from './connection.js';
import { openSession } from './nav.js';

// En-tête d'une session : le harness et son modèle, le nom, l'état et le
// bouton du panneau. Tout le reste est dans le panneau, comme pour une
// discussion.
function SessionHeader({ s, narrowView, insp, setInsp }) {
  const state = stateOf(s), cli = cliOf(s.mode);
  const rename = async () => {
    const name = await prompt(t('agents.session.rename'), { value: s.name || '' });
    if (name && name !== s.name) renameSession(s.id, name).catch(e => toast(e.message, 'err'));
  };
  const restart = () => api.start(s.id, s.mode === 'shell').then(() => api.session(s.id).then(upsertSession)).catch(e => toast(e.message, 'err'));
  if (narrowView) return html`<header class="cm-sh phone-session-head">
    <${BackButton} />
    <div class="phone-agent-title">
      <span><${CliMark} mode=${s.mode} size="xs" /><b class="trunc">${s.name || s.id.slice(0, 8)}</b></span>
      <small class=${'st-' + state}><${StateDot} state=${state} /><span class="trunc">${[lt('state', state), s.displayModel?.model, s.workingDir && projectOf(s.workingDir)].filter(Boolean).join(' · ')}</span></small>
    </div>
    <button type="button" class=${cls('icon-btn', insp && 'on')} aria-label=${t('agents.insp.title')} aria-expanded=${String(insp)} onClick=${() => setInsp(!insp)}><${Icon} n="more" /></button>
  </header>`;
  return html`<header class="cm-sh">
    <span class="cm-sh-cli"><${CliMark} mode=${s.mode} size="sm" /><b>${cli.label}</b>${s.displayModel?.model && html`<span class="muted">${s.displayModel.model}</span>`}</span>
    <span class="cm-sh-sep" aria-hidden="true">/</span>
    <button class="btn sm cm-sh-name" onClick=${rename} title=${t('agents.session.rename')}><span class="trunc">${s.name || s.id.slice(0, 8)}</span></button>
    <span class="grow"></span>
    ${state === 'stopped' ? html`<button class="btn sm" onClick=${restart}><${Icon} n="play" />${t('agents.session.restart')}</button>`
      : html`<span class=${'cm-sh-state st-' + state}><${StateDot} state=${state} />${lt('state', state)}</span>`}
    <button class=${cls('icon-btn', insp && 'on')} aria-label=${t('agents.insp.title')} title=${t('agents.insp.title')} aria-pressed=${String(!!insp)} onClick=${() => setInsp(!insp)}><${Icon} n="panel" /></button>
  </header>`;
}

function SessionPane({ s, narrowView }) {
  const [view, setViewState] = useState(viewOf(s));
  const [insp, setInspState] = useState(() => { try { return !narrowView && innerWidth > 1180 && localStorage.getItem('loom.codeman.inspOpen') !== '0'; } catch (_) { return false; } });
  const phoneTerminal = useStore(layout, l => l.phoneTerminal === s.id);
  useEffect(() => { setViewState(viewOf(s)); }, [s.id]);
  useEffect(() => () => { if (layout.get().phoneTerminal === s.id) layout.set({ phoneTerminal: '' }); }, [s.id]);
  const setView = v => {
    if (narrowView) layout.set({ phoneTerminal: v === 'term' ? s.id : '' });
    else { setViewOf(s.id, v); setViewState(v); }
  };
  const setInsp = v => { setInspState(v); if (!isPhone()) try { localStorage.setItem('loom.codeman.inspOpen', v ? '1' : '0'); } catch (_) {} };
  const mode = narrowView ? (phoneTerminal || s.mode === 'shell' ? 'term' : 'chat') : hasConversation(s.mode) ? view : 'term';
  const toTerm = () => setView(narrowView ? 'term' : 'split');
  return html`<div class=${cls('cm-pane', insp && !narrowView && 'with-insp')}>
    <div class="cm-main">
      <${SessionHeader} s=${s} narrowView=${narrowView} insp=${insp} setInsp=${setInsp} />
      ${narrowView && s.mode !== 'shell' && html`<div class="phone-agent-views">
        <${Seg} value=${mode} onChange=${setView} label=${t('agents.view.label')} options=${[
          { value: 'chat', label: t('agents.view.chat') }, { value: 'term', label: t('agents.view.terminal') },
        ]} />
      </div>`}
      <div class=${cls('cm-body', 'v-' + mode)}>
        ${(mode === 'chat' || mode === 'split') && html`<${Conversation} key=${'c' + s.id} session=${s} onTerminal=${toTerm} terminalVisible=${mode === 'split'} phone=${narrowView} />`}
        ${(mode === 'term' || mode === 'split') && html`<div class="cm-term-wrap"><${AgentTerminal} key=${'t' + s.id} sessionId=${s.id} mode=${s.mode} shell=${s.mode === 'shell'} scrollback=${agents.get().settings?.terminalScrollbackLines || 50000} focus=${mode === 'term'} phoneControls=${narrowView} /></div>`}
      </div>
    </div>
    ${insp && (narrowView ? html`<${Drawer} title=${t('agents.insp.info')} onClose=${() => setInsp(false)}><${Inspector} session=${s} view=${mode} setView=${setView} narrowView /></${Drawer}>`
      : html`<${Inspector} session=${s} onClose=${() => setInsp(false)} view=${mode} setView=${setView} />`)}
  </div>`;
}

// Aucune session ouverte : la liste est dans la barre latérale.
function Home() {
  return html`<div class="cm-main cm-blank"><${Empty} icon="agent" title=${t('agents.home.pick')} text=${t('agents.home.pick_text')}>
    <button class="btn primary" onClick=${() => agents.set({ creating: {} })}><${Icon} n="plus" />${t('agents.rail.new')}</button></${Empty}></div>`;
}

// Grille : l'inspecteur suit le volet qui a le focus.
function GridArea() {
  const focusId = useStore(layout, l => l.cells[l.focus]);
  const session = useStore(agents, s => (focusId && s.sessions[focusId]) || null);
  const insp = useStore(gridInsp, g => g.open);
  return html`<div class=${cls('cm-pane', insp && session && 'with-insp')}>
    <div class="cm-main">
      <header class="cm-sh cm-grid-h"><b>${t('agents.nav.grid')}</b><span class="grow"></span>
        <${LayoutPicker} currentId=${focusId || ''} />
        <button class="icon-btn" aria-label=${t('agents.grid.close')} title=${t('agents.grid.close')} onClick=${() => { toggleGrid(focusId || ''); if (focusId) openSession(focusId); }}><${Icon} n="close" /></button></header>
      <${Grid} />
    </div>
    ${insp && session && html`<${Inspector} key=${session.id} session=${session} onClose=${() => gridInsp.set({ open: false })} />`}
  </div>`;
}
const gridInsp = createStore({ open: false });

function Workspace({ route }) {
  const narrowView = usePhone();
  const id = route.sub === 's' ? route.id : '';
  const session = useStore(agents, s => s.sessions[id] || null);
  const loaded = useStore(agents, s => s.loaded);
  const grid = useStore(layout, l => l.count > 1) && !narrowView;
  // Dernière session ouverte, sur grand écran ; une session demandée par
  // l'adresse entre dans la grille quand elle est ouverte.
  useEffect(() => {
    if (id) {
      try { localStorage.setItem('loom.codeman.last', id); } catch (_) {}
      if (grid && loaded && agents.get().sessions[id] && !layout.get().cells.includes(id)) placeSession(id);
      return;
    }
    if (narrowView || !loaded || grid) return;
    let last = ''; try { last = localStorage.getItem('loom.codeman.last') || ''; } catch (_) {}
    if (last && agents.get().sessions[last]) openSession(last);
  }, [id, loaded, narrowView, grid]);
  if (grid) return html`<div class="cm-ws"><${GridArea} /></div>`;
  return html`<div class=${cls('cm-ws', narrowView && id && 'focus')}>
    ${session ? html`<${SessionPane} key=${session.id} s=${session} narrowView=${narrowView} />`
      : id && loaded ? html`<div class="cm-main cm-blank"><${Empty} icon="info" title=${t('agents.session.missing')} text=${t('agents.session.missing_text')}><button class="btn" onClick=${() => go('agents')}>${t('agents.back')}</button></${Empty}></div>`
      : id ? html`<div class="cm-main cm-blank"><span class="spinner"></span></div>`
      : html`<${Home} />`}
  </div>`;
}

// Disposition : un bouton qui montre la forme actuelle et ouvre le choix
// (vue seule ou grille de volets, comme tmux), plus l'inspecteur de la grille.
function LayoutPicker({ currentId }) {
  const count = useStore(layout, l => l.count);
  const insp = useStore(gridInsp, s => s.open);
  const [anchor, setAnchor] = useState(null);
  const label = n => (n === 1 ? t('agents.grid.single') : t('agents.grid.panes', { n }));
  return html`<button class=${cls('icon-btn cm-layout-btn', anchor && 'on')} aria-label=${t('agents.grid.layout')} title=${t('agents.grid.layout') + ' · ' + label(count)}
      aria-expanded=${String(!!anchor)} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${ShapeIcon} n=${count} /></button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${() => setAnchor(null)} width=${236} class="cm-layout-pop">
      <div class="cm-layout-grid" role="radiogroup" aria-label=${t('agents.grid.layout')}>${COUNTS.map(n => html`<button key=${n} role="radio" aria-checked=${String(count === n)} class="btn sm"
        title=${label(n)} aria-label=${label(n)} onClick=${() => { setCount(n, currentId); setAnchor(null); }}><${ShapeIcon} n=${n} /><span>${n}</span></button>`)}</div>
      ${count > 1 && html`<label class="cm-layout-row"><span>${t('agents.insp.title')}</span><${Switch} checked=${insp} onChange=${v => gridInsp.set({ open: v })} /></label>`}
      <p class="cm-layout-hint muted">${t('agents.grid.keys')}</p>
    </${Popover}>`}`;
}

const SUBS = ['', 'cron', 'connections', 'settings'];
function Top({ sub, phone }) {
  const tabs = SUBS.map(v => ({ href: '#/agents' + (v ? '/' + v : ''), label: lt('nav', v), current: sub === v }));
  return html`<header class="cm-admin-top">
    <${TabNav} id="agents" label=${t('agents.title')} tabs=${tabs} phoneChips=${phone} />
  </header>`;
}

export function AgentsPage({ route }) {
  const conn = useStore(agents, s => s.conn);
  const { state } = conn;
  const ready = state === 'ready';
  useEffect(() => { refreshConnection(); }, []);
  useEffect(() => {
    if (ready) return;
    const timer = setInterval(() => { if (!document.hidden) refreshConnection(); }, 10000);
    return () => clearInterval(timer);
  }, [ready]);
  const sub = SUBS.includes(route.sub) || route.sub === 's' || route.sub === 'grid' ? route.sub : route.sub === 'control' ? '' : '';
  const work = sub === 's' || sub === 'grid';
  const currentId = sub === 's' ? route.id : '';
  // #/agents/grid ouvre la mosaïque ; Ctrl+Maj+G l'ouvre ou la referme.
  useEffect(() => { if (sub === 'grid' && innerWidth > 760 && layout.get().count === 1) toggleGrid(''); }, [sub]);
  useEffect(() => {
    const key = e => { if ((e.metaKey || e.ctrlKey) && e.shiftKey && !e.altKey && e.key.toLowerCase() === 'g' && innerWidth > 760) { e.preventDefault(); e.stopPropagation(); if (layout.get().count === 1) { toggleGrid(currentId); go('agents', 'grid'); } else { toggleGrid(currentId); if (currentId) openSession(currentId); } } };
    addEventListener('keydown', key, true); return () => removeEventListener('keydown', key, true);
  }, [currentId]);
  // Réglages et Connexions restent utiles sans Codeman (dossier, skills, MCP Loom).
  const gated = sub !== 'settings' && sub !== 'connections' && state !== 'ready';
  const narrowView = usePhone();
  const hasSession = useStore(agents, s => !!s.sessions[currentId]);
  const server = conn.url + '\n' + conn.username;
  const gate = html`<div class="cm-gate">${state === 'idle' || state === 'connecting' ? html`<div class="cm-gate-in"><span class="spinner"></span><p class="muted">${t('agents.conn.connecting')}</p></div>` : html`<${ConnectionPanel} gate />`}</div>`;
  return html`<div class="view cm">
    ${narrowView && work && (!hasSession || gated) && html`<header class="phone-subhead"><${BackButton} /><b>${t('agents.title')}</b></header>`}
    ${!work && html`<${Top} sub=${sub} phone=${narrowView} />`}
    ${gated ? gate
      : work ? html`<${Workspace} route=${route} />`
      : sub === 'cron' ? html`<${CronPage} key=${server} phone=${narrowView} />`
      : sub === 'connections' ? html`<${ConnectionsPage} phone=${narrowView} />`
      : sub === 'settings' ? html`<div class="cm-scroll"><${CodemanSettings} /></div>`
      : html`<${ControlCenter} key=${server} phone=${narrowView} />`}
  </div>`;
}
