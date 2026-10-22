// Module Agents : une interface Loom pour un serveur Codeman. Codeman reste
// le moteur (sessions, tmux, CLI natifs, orchestration) ; Loom affiche et
// pilote par son API officielle. Les sessions vivent dans la barre latérale ;
// la page montre une session seule ou une grille de volets. Routes :
// #/agents, #/agents/s/<id>, #/agents/control, #/agents/cron,
// #/agents/settings (#/agents/grid ouvre la grille).
import { lt } from './labels.js';
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls, createStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Menu, Seg, Empty } from '../../ui/controls.js';
import { prompt, confirm, toast } from '../../ui/dialog.js';
import { Drawer } from '../../ui/drawer.js';
import { TabNav } from '../../ui/tabnav.js';
import { app, go } from '../../core/state.js';
import { api } from './client.js';
import { agents, useAgentsLive, refreshConnection, stateOf, cliOf, projectOf, upsertSession } from './store.js';
import { closeSession, renameSession } from './actions.js';
import { Rail, StateDot } from './rail.js';
import { layout, COUNTS, setCount, toggleGrid, placeSession, viewOf, setViewOf } from './layout.js';
import { Conversation } from './conversation.js';
import { AgentTerminal } from './terminal.js';
import { Inspector } from './inspector.js';
import { CliMark } from './new-session.js';
import { ControlCenter } from './control.js';
import { Grid, ShapeIcon } from './grid.js';
import { CodemanSettings } from './settings.js';
import { CronPage } from './cron.js';
import { ConnectionPanel } from './connection.js';
import { openSession, officialLink } from './nav.js';

const narrow = () => matchMedia('(max-width: 760px)').matches;
function useNarrow() {
  const [n, setN] = useState(narrow());
  useEffect(() => { const m = matchMedia('(max-width: 760px)'); const f = () => setN(m.matches); m.addEventListener('change', f); return () => m.removeEventListener('change', f); }, []);
  return n;
}

function SessionHeader({ s, view, setView, narrowView, insp, setInsp }) {
  const [menu, setMenu] = useState(null);
  const state = stateOf(s), cli = cliOf(s.mode);
  const rename = async () => {
    const name = await prompt(t('agents.session.rename'), { value: s.name || '' });
    if (name && name !== s.name) renameSession(s.id, name).catch(e => toast(e.message, 'err'));
  };
  const close = async killMux => {
    const ok = await confirm(killMux ? t('agents.session.close_title') : t('agents.session.detach_title'),
      killMux ? t('agents.session.close_text', { name: s.name || s.id.slice(0, 8) }) : t('agents.session.detach_text'), { ok: killMux ? t('agents.session.close') : t('agents.session.detach'), danger: killMux });
    if (!ok) return;
    try { await closeSession(s.id, killMux); go('agents'); } catch (e) { toast(e.message, 'err'); }
  };
  const restart = () => api.start(s.id, s.mode === 'shell').then(() => api.session(s.id).then(upsertSession)).catch(e => toast(e.message, 'err'));
  const link = officialLink(s.id);
  const items = [
    { label: t('agents.session.rename'), icon: 'edit', run: rename },
    ...(state === 'stopped' ? [{ label: t('agents.session.restart'), icon: 'play', run: restart }] : []),
    ...(link ? [{ label: t('agents.session.official'), icon: 'link', run: () => window.open(link, 'codeman') }] : []),
    '-',
    { label: t('agents.session.detach'), icon: 'power', run: () => close(false) },
    { label: t('agents.session.close'), icon: 'trash', danger: true, run: () => close(true) },
  ];
  const opts = [{ value: 'chat', label: t('agents.view.chat') }, { value: 'term', label: t('agents.view.terminal') }, ...(narrowView ? [] : [{ value: 'split', label: t('agents.view.split') }])];
  return html`<header class="cm-sh">
    ${narrowView && html`<button class="icon-btn" aria-label=${t('agents.back')} onClick=${() => go('agents')}><${Icon} n="left" /></button>`}
    <${CliMark} mode=${s.mode} />
    <div class="cm-sh-t">
      <button class="cm-sh-name trunc" onClick=${rename} title=${t('agents.session.rename')}>${s.name || s.id.slice(0, 8)}</button>
      <small class="trunc"><${StateDot} state=${state} />${lt('state', state)} · ${cli.label}${s.displayModel?.model ? ' · ' + s.displayModel.model : ''} · <span class="mono" title=${s.workingDir}>${projectOf(s.workingDir)}</span></small>
    </div>
    ${state === 'stopped' && html`<button class="btn sm" onClick=${restart}><${Icon} n="play" />${t('agents.session.restart')}</button>`}
    ${s.mode !== 'shell' && html`<${Seg} size="sm" value=${view} options=${opts} onChange=${setView} label=${t('agents.view.label')} />`}
    <button class=${cls('icon-btn', insp && 'on')} aria-label=${t('agents.insp.title')} title=${t('agents.insp.title')} onClick=${() => setInsp(!insp)}><${Icon} n="panel" /></button>
    <button class="icon-btn" aria-label=${t('agents.session.actions')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
  </header>`;
}

function SessionPane({ s, narrowView }) {
  const [view, setViewState] = useState(viewOf(s));
  const [insp, setInspState] = useState(() => { try { return !narrow() && innerWidth > 1180 && localStorage.getItem('loom.codeman.inspOpen') !== '0'; } catch (_) { return false; } });
  useEffect(() => { setViewState(viewOf(s)); }, [s.id]);
  const setView = v => { setViewOf(s.id, v); setViewState(v); };
  const setInsp = v => { setInspState(v); if (!narrow()) try { localStorage.setItem('loom.codeman.inspOpen', v ? '1' : '0'); } catch (_) {} };
  const mode = s.mode === 'shell' ? 'term' : (narrowView && view === 'split' ? 'chat' : view);
  const toTerm = () => setView(narrowView ? 'term' : 'split');
  return html`<div class=${cls('cm-pane', insp && !narrowView && 'with-insp')}>
    <div class="cm-main">
      <${SessionHeader} s=${s} view=${mode} setView=${setView} narrowView=${narrowView} insp=${insp} setInsp=${setInsp} />
      <div class=${cls('cm-body', 'v-' + mode)}>
        ${(mode === 'chat' || mode === 'split') && html`<${Conversation} key=${'c' + s.id} session=${s} onTerminal=${toTerm} terminalVisible=${mode === 'split'} />`}
        ${(mode === 'term' || mode === 'split') && html`<div class="cm-term-wrap"><${AgentTerminal} key=${'t' + s.id} sessionId=${s.id} focus=${mode === 'term'} /></div>`}
      </div>
    </div>
    ${insp && (narrowView ? html`<${Drawer} title=${t('agents.insp.title')} onClose=${() => setInsp(false)}><${Inspector} session=${s} /></${Drawer}>`
      : html`<${Inspector} session=${s} onClose=${() => setInsp(false)} />`)}
  </div>`;
}

// Accueil sans session ouverte : la liste complète (recherche, sessions par
// dossier, historique à reprendre). Sur téléphone, c'est l'écran principal.
function Home({ narrowView, activeId }) {
  return html`<div class=${cls('cm-home', narrowView && 'narrow')}>
    ${!narrowView && html`<div class="cm-home-h"><h1>${t('agents.home.title')}</h1><p class="muted">${t('agents.home.lead')}</p></div>`}
    <${Rail} activeId=${activeId} onOpen=${openSession} onNew=${() => agents.set({ creating: {} })} />
  </div>`;
}

// Grille : l'inspecteur suit le volet qui a le focus.
function GridArea() {
  const focusId = useStore(layout, l => l.cells[l.focus]);
  const session = useStore(agents, s => (focusId && s.sessions[focusId]) || null);
  const insp = useStore(gridInsp, g => g.open);
  return html`<div class=${cls('cm-pane', insp && session && 'with-insp')}>
    <div class="cm-main"><${Grid} /></div>
    ${insp && session && html`<${Inspector} key=${session.id} session=${session} onClose=${() => gridInsp.set({ open: false })} />`}
  </div>`;
}
const gridInsp = createStore({ open: false });

function Workspace({ route }) {
  const narrowView = useNarrow();
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
      : html`<${Home} narrowView=${narrowView} activeId=${id} />`}
  </div>`;
}

// Disposition : vue seule ou grille, comme les volets de tmux.
function LayoutPicker({ currentId }) {
  const count = useStore(layout, l => l.count);
  const insp = useStore(gridInsp, s => s.open);
  return html`<div class="cm-layouts" role="radiogroup" aria-label=${t('agents.grid.layout')}>
    ${COUNTS.map(n => html`<button key=${n} role="radio" aria-checked=${String(count === n)} class=${cls(count === n && 'on')}
      title=${n === 1 ? t('agents.grid.single') : t('agents.grid.panes', { n })} aria-label=${n === 1 ? t('agents.grid.single') : t('agents.grid.panes', { n })}
      onClick=${() => setCount(n, currentId)}><${ShapeIcon} n=${n} /></button>`)}
    ${count > 1 && html`<span class="cm-layouts-sep"></span><button class=${cls(insp && 'on')} aria-pressed=${String(insp)} title=${t('agents.insp.title')} aria-label=${t('agents.insp.title')}
      onClick=${() => gridInsp.set({ open: !insp })}><${Icon} n="panel" /></button>`}
  </div>`;
}

// Quotas d'abonnement en un coup d'œil ; le détail est dans Activité.
export function QuotaChip() {
  const usage = useStore(agents, s => s.planUsage);
  const pct = w => (w && Number.isFinite(w.usedPercentage) ? Math.round(w.usedPercentage) : null);
  const codex = usage?.codex?.sevenDay || usage?.codex?.fiveHour;
  const parts = [[t('quotas.w5h'), pct(usage?.fiveHour)], [t('quotas.w7d'), pct(usage?.sevenDay)]].filter(p => p[1] != null);
  if (!parts.length && pct(codex) != null) parts.push(['Codex', pct(codex)]);
  if (!parts.length) return null;
  const tone = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : '');
  return html`<a class="cm-quota hide-narrow" href="#/quotas" title=${t('quotas.chip')}>${parts.map(([label, v]) => html`<span class=${tone(v)}><em>${label}</em>${v}%</span>`)}</a>`;
}

const SUBS = ['', 'control', 'cron', 'settings'];
function Top({ sub, currentId }) {
  const conn = useStore(agents, s => s.conn);
  const live = useStore(agents, s => s.live);
  const waiting = useStore(agents, s => s.approvals.filter(a => a.kind !== 'idle').length);
  const narrowView = useNarrow();
  const current = sub === 's' || sub === 'grid' ? '' : sub;
  const tabs = SUBS.map(v => ({ href: '#/agents' + (v ? '/' + v : ''), label: lt('nav', v) + (v === 'control' && waiting ? ' · ' + waiting : ''), current: current === v }));
  const link = officialLink('');
  return html`<div class="cm-top">
    <button class="icon-btn only-mobile" aria-label=${t('app.main.menu')} onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button>
    <${TabNav} id="agents" label=${t('agents.title')} tabs=${tabs} />
    <span class="grow"></span>
    ${current === '' && !narrowView && conn.state === 'ready' && html`<${LayoutPicker} currentId=${currentId} />`}
    <${QuotaChip} />
    <span class=${cls('cm-conn', conn.state === 'ready' && live && 'ok')} title=${conn.url}>
      <i class=${cls('dot', conn.state === 'ready' ? (live ? 'green' : 'amber') : 'red')}></i>
      <span class="hide-narrow">${conn.state === 'ready' ? 'Codeman ' + (conn.version || '') : lt('conn', conn.state)}</span></span>
    ${link && html`<a class="icon-btn hide-narrow" href=${link} target="codeman" rel="noopener" title=${t('agents.official')} aria-label=${t('agents.official')}><${Icon} n="link" /></a>`}
  </div>`;
}

export function AgentsPage({ route }) {
  const state = useStore(agents, s => s.conn.state);
  const ready = state === 'ready';
  useEffect(() => { refreshConnection(); }, []);
  // Le flux d'événements ne s'ouvre qu'une fois connecté ; sinon on revérifie
  // régulièrement (Codeman redémarré, mis à jour, réinstallé).
  useEffect(() => {
    if (ready) return useAgentsLive();
    const timer = setInterval(() => { if (!document.hidden) refreshConnection(); }, 10000);
    return () => clearInterval(timer);
  }, [ready]);
  const sub = SUBS.includes(route.sub) || route.sub === 's' || route.sub === 'grid' ? route.sub : '';
  const currentId = sub === 's' ? route.id : '';
  // Ctrl+Maj+G ouvre ou referme la grille ; l'ancienne adresse #/agents/grid l'ouvre.
  useEffect(() => { if (sub === 'grid' && innerWidth > 760) { if (layout.get().count === 1) toggleGrid(currentId); go('agents'); } }, [sub]);
  useEffect(() => {
    const key = e => { if ((e.metaKey || e.ctrlKey) && e.shiftKey && !e.altKey && e.key.toLowerCase() === 'g' && innerWidth > 760) { e.preventDefault(); e.stopPropagation(); toggleGrid(currentId); } };
    addEventListener('keydown', key, true); return () => removeEventListener('keydown', key, true);
  }, [currentId]);
  const gated = sub !== 'settings' && state !== 'ready';
  return html`<div class="view cm">
    <${Top} sub=${sub} currentId=${currentId} />
    ${gated ? html`<div class="cm-gate">${state === 'idle' || state === 'connecting' ? html`<div class="cm-gate-in"><span class="spinner"></span><p class="muted">${t('agents.conn.connecting')}</p></div>` : html`<${ConnectionPanel} gate />`}</div>`
      : sub === 'control' ? html`<${ControlCenter} />`
      : sub === 'cron' ? html`<${CronPage} />`
      : sub === 'settings' ? html`<div class="cm-scroll"><${CodemanSettings} /></div>`
      : html`<${Workspace} route=${route} />`}
  </div>`;
}
