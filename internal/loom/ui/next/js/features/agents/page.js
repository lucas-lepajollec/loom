// Module Agents : une interface Loom pour un serveur Codeman. Codeman reste
// le moteur (sessions, tmux, CLI natifs, orchestration) ; Loom affiche et
// pilote par son API officielle. Les sessions vivent dans la barre latérale ;
// la page montre une session seule ou une grille de volets. Routes :
// #/agents, #/agents/s/<id>, #/agents/control, #/agents/cron,
// #/agents/settings (#/agents/grid ouvre la grille).
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls, createStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Menu, Seg, Empty, Popover, Switch } from '../../ui/controls.js';
import { prompt, confirm, toast } from '../../ui/dialog.js';
import { Drawer } from '../../ui/drawer.js';
import { TabNav } from '../../ui/tabnav.js';
import { app, go } from '../../core/state.js';
import { api } from './client.js';
import { agents, useAgentsLive, refreshConnection, stateOf, cliOf, projectOf, upsertSession } from './store.js';
import { closeSession, renameSession, createSession } from './actions.js';
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
    <button class="icon-btn" aria-label=${t('agents.session.actions')} onClick=${e => setMenu(menu ? null : e.currentTarget)}><${Icon} n="more" /></button>
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
        ${(mode === 'term' || mode === 'split') && html`<div class="cm-term-wrap"><${AgentTerminal} key=${'t' + s.id} sessionId=${s.id} mode=${s.mode} shell=${s.mode === 'shell'} scrollback=${agents.get().settings?.terminalScrollbackLines || 50000} focus=${mode === 'term'} /></div>`}
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
      <div class="cm-layout-grid" role="radiogroup" aria-label=${t('agents.grid.layout')}>${COUNTS.map(n => html`<button key=${n} role="radio" aria-checked=${String(count === n)} class=${cls(count === n && 'on')}
        title=${label(n)} aria-label=${label(n)} onClick=${() => { setCount(n, currentId); setAnchor(null); }}><${ShapeIcon} n=${n} /><span>${n}</span></button>`)}</div>
      ${count > 1 && html`<label class="cm-layout-row"><span>${t('agents.insp.title')}</span><${Switch} checked=${insp} onChange=${v => gridInsp.set({ open: v })} /></label>`}
      <p class="cm-layout-hint muted">${t('agents.grid.keys')}</p>
    </${Popover}>`}`;
}

// Aperçu des quotas d'abonnement : une jauge dans la barre, le détail en un
// clic (fenêtres de Claude et de Codex, remise à zéro), la page complète
// dans Activité.
function QuotaPeek() {
  const usage = useStore(agents, s => s.planUsage);
  const [anchor, setAnchor] = useState(null);
  const win = (label, w) => (w && Number.isFinite(w.usedPercentage) ? { label, used: Math.round(w.usedPercentage), reset: w.resetAt } : null);
  const rows = [
    ['Claude', [win(t('quotas.win_5h'), usage?.fiveHour), win(t('quotas.win_7d'), usage?.sevenDay)]],
    ['Codex', [win(t('quotas.win_5h'), usage?.codex?.fiveHour), win(t('quotas.win_7d'), usage?.codex?.sevenDay)]],
  ].map(([name, ws]) => [name, ws.filter(Boolean)]).filter(([, ws]) => ws.length);
  if (!rows.length) return null;
  const top = Math.max(...rows.flatMap(([, ws]) => ws.map(w => w.used)));
  const tone = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : '');
  const until = ms => { const m = Math.max(0, Math.round((ms - Date.now()) / 60000)); return m >= 1440 ? t('quotas.in_dh', { d: Math.floor(m / 1440), h: Math.floor((m % 1440) / 60) }) : m >= 60 ? t('quotas.in_hm', { h: Math.floor(m / 60), m: m % 60 }) : t('quotas.in_m', { m }); };
  return html`<button class=${cls('icon-btn cm-quota-btn', anchor && 'on', tone(top))} aria-label=${t('quotas.chip')} title=${t('quotas.chip') + ' · ' + top + '%'}
      aria-expanded=${String(!!anchor)} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="gauge" /><i class="cm-quota-dot"></i></button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${() => setAnchor(null)} width=${272} class="cm-quota-pop">
      ${rows.map(([name, ws]) => html`<div class="cm-qp-acc" key=${name}><b>${name}</b>
        ${ws.map(w => html`<div class=${cls('cm-qp-win', tone(w.used))} key=${w.label}>
          <div class="cm-qp-h"><span>${w.label}</span><em>${w.used}%</em></div>
          <div class="cm-qp-bar"><i style=${`width:${Math.min(100, w.used)}%`}></i></div>
          ${w.reset ? html`<small class="muted">${t('quotas.resets_in', { in: until(w.reset), at: new Date(w.reset).toLocaleString(locale(), w.reset - Date.now() > 86400000 ? { weekday: 'short', hour: '2-digit', minute: '2-digit' } : { hour: '2-digit', minute: '2-digit' }) })}</small>` : ''}
        </div>`)}</div>`)}
      <a class="cm-qp-more" href="#/quotas" onClick=${() => setAnchor(null)}>${t('quotas.open')}<${Icon} n="right" /></a>
    </${Popover}>`}`;
}

// Lancement rapide : un shell ou l'agent par défaut, dans le dossier de la
// session affichée (sinon le dossier par défaut de Codeman, sinon le plus
// récent). Le menu propose les autres CLI et la fenêtre complète.
function QuickLaunch({ currentId, narrowView }) {
  const clis = useStore(agents, s => s.clis);
  const settings = useStore(agents, s => s.settings || {});
  const [busy, setBusy] = useState('');
  const [menu, setMenu] = useState(null);
  const folder = () => {
    const l = layout.get(), st = agents.get();
    const id = l.count > 1 && !narrowView ? l.cells[l.focus] : currentId;
    return (id && st.sessions[id]?.workingDir) || settings.defaultWorkingDir || (st.unified.find(r => r.workingDir) || {}).workingDir || '';
  };
  const list = clis.filter(c => c.kind !== 'shell' && c.enabled !== false && c.installed !== false);
  const def = list.find(c => c.id === settings.runMode) || list.find(c => c.id === 'claude') || list[0];
  const launch = async mode => {
    const dir = folder();
    if (!dir) return agents.set({ creating: { preset: { mode } } });
    setBusy(mode);
    try { openSession(await createSession({ mode, workingDir: dir })); }
    catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  const where = projectOf(folder());
  const items = [...(narrowView ? [{ label: t('agents.quick.shell'), icon: 'terminal', run: () => launch('shell') }] : []), ...list.map(c => ({ label: c.label, icon: 'agent', run: () => launch(c.id) })), '-',
    { label: t('agents.quick.more'), icon: 'sliders', run: () => agents.set({ creating: { preset: { workingDir: folder() } } }) }];
  // Sur téléphone, un seul bouton « + » ouvre le menu (shell compris).
  if (narrowView) return html`<div class="cm-quick"><button class="btn sm primary cm-quick-one" aria-label=${t('agents.rail.new')} disabled=${!!busy}
      onClick=${e => setMenu(menu ? null : e.currentTarget)}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="plus" />`}</button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}</div>`;
  return html`<div class="cm-quick">
    <button class="icon-btn" disabled=${!!busy} title=${t('agents.quick.shell_hint', { dir: where })} aria-label=${t('agents.quick.shell')} onClick=${() => launch('shell')}>
      ${busy === 'shell' ? html`<span class="spinner"></span>` : html`<${Icon} n="terminal" />`}</button>
    ${def && html`<div class="cm-split">
      <button class="btn sm primary" disabled=${!!busy} title=${t('agents.quick.agent_hint', { cli: def.label, dir: where })} aria-label=${def.label} onClick=${() => launch(def.id)}>
        ${busy && busy !== 'shell' ? html`<span class="spinner"></span>` : html`<${Icon} n="plus" />`}${!narrowView && def.label}</button>
      <button class="btn sm primary cm-split-caret" aria-label=${t('agents.quick.choose')} title=${t('agents.quick.choose')} onClick=${e => setMenu(menu ? null : e.currentTarget)}><${Icon} n="chevron" /></button>
    </div>`}
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
  </div>`;
}

const SUBS = ['', 'control', 'cron', 'settings'];
function Top({ sub, currentId }) {
  const conn = useStore(agents, s => s.conn);
  const live = useStore(agents, s => s.live);
  const waiting = useStore(agents, s => s.approvals.filter(a => a.kind !== 'idle').length);
  const narrowView = useNarrow();
  const current = sub === 's' || sub === 'grid' ? '' : sub;
  const tabs = SUBS.map(v => ({ href: '#/agents' + (v ? '/' + v : ''), label: lt('nav', v) + (v === 'control' && waiting ? ' · ' + waiting : ''), current: current === v }));
  return html`<div class="cm-top">
    <button class="icon-btn only-mobile" aria-label=${t('app.main.menu')} onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button>
    <${TabNav} id="agents" label=${t('agents.title')} tabs=${tabs} />
    <span class="grow"></span>
    ${conn.state === 'ready' && html`<${QuickLaunch} currentId=${currentId} narrowView=${narrowView} />`}
    ${current === '' && !narrowView && conn.state === 'ready' && html`<${LayoutPicker} currentId=${currentId} />`}
    ${conn.state === 'ready' && !narrowView && html`<${QuotaPeek} />`}
    <a class=${cls('cm-conn', conn.state === 'ready' && 'ok')} href="#/agents/settings"
      title=${(conn.state === 'ready' ? 'Codeman ' + (conn.version || '') + (live ? '' : ' · ' + t('agents.conn.connecting')) : lt('conn', conn.state)) + (conn.url ? ' · ' + conn.url : '')}>
      <i class=${cls('dot', conn.state === 'ready' ? (live ? 'green' : 'amber') : 'red')}></i>${conn.state !== 'ready' && html`<span class="hide-narrow">${lt('conn', conn.state)}</span>`}</a>
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
