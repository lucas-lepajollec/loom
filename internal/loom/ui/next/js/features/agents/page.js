// Module Agents : une interface Loom pour un serveur Codeman. Codeman reste
// le moteur (sessions, tmux, CLI natifs, orchestration) ; Loom affiche et
// pilote par son API officielle. Routes : #/agents (espace de travail),
// #/agents/s/<id>, #/agents/control, #/agents/grid, #/agents/cron,
// #/agents/settings.
import { lt } from './labels.js';
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
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
import { Conversation } from './conversation.js';
import { AgentTerminal } from './terminal.js';
import { Inspector } from './inspector.js';
import { NewSession, CliMark } from './new-session.js';
import { ControlCenter } from './control.js';
import { Mosaic } from './grid.js';
import { CronPage } from './cron.js';
import { ConnectionPanel } from './connection.js';
import { openSession, officialLink } from './nav.js';

const narrow = () => matchMedia('(max-width: 760px)').matches;
function useNarrow() {
  const [n, setN] = useState(narrow());
  useEffect(() => { const m = matchMedia('(max-width: 760px)'); const f = () => setN(m.matches); m.addEventListener('change', f); return () => m.removeEventListener('change', f); }, []);
  return n;
}

const VIEWS = new Map();
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
  const [view, setViewState] = useState(VIEWS.get(s.id) || (s.mode === 'shell' ? 'term' : 'chat'));
  const [insp, setInspState] = useState(() => { try { return !narrow() && innerWidth > 1180 && localStorage.getItem('loom.codeman.inspOpen') !== '0'; } catch (_) { return false; } });
  useEffect(() => { setViewState(VIEWS.get(s.id) || (s.mode === 'shell' ? 'term' : 'chat')); }, [s.id]);
  const setView = v => { VIEWS.set(s.id, v); setViewState(v); };
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

function Workspace({ route }) {
  const narrowView = useNarrow();
  const id = route.sub === 's' ? route.id : '';
  const session = useStore(agents, s => s.sessions[id] || null);
  const loaded = useStore(agents, s => s.loaded);
  const [creating, setCreating] = useState(false);
  // Dernière session ouverte, sur grand écran.
  useEffect(() => {
    if (id) { try { localStorage.setItem('loom.codeman.last', id); } catch (_) {} return; }
    if (narrowView || !loaded) return;
    let last = ''; try { last = localStorage.getItem('loom.codeman.last') || ''; } catch (_) {}
    if (last && agents.get().sessions[last]) openSession(last);
  }, [id, loaded, narrowView]);
  useEffect(() => {
    const key = e => { if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'n') { e.preventDefault(); setCreating(true); } };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
  const showRail = !narrowView || !id;
  return html`<div class=${cls('cm-ws', narrowView && id && 'focus')}>
    ${showRail && html`<${Rail} activeId=${id} onOpen=${openSession} onNew=${() => setCreating(true)} />`}
    ${(!narrowView || id) && (session ? html`<${SessionPane} key=${session.id} s=${session} narrowView=${narrowView} />`
      : html`<div class="cm-main cm-blank">${id && loaded ? html`<${Empty} icon="info" title=${t('agents.session.missing')} text=${t('agents.session.missing_text')}><button class="btn" onClick=${() => go('agents')}>${t('agents.back')}</button></${Empty}>`
        : html`<${Empty} icon="terminal" title=${t('agents.ws.title')} text=${t('agents.ws.text')}><button class="btn primary" onClick=${() => setCreating(true)}><${Icon} n="plus" />${t('agents.rail.new')}</button></${Empty}>`}</div>`)}
    ${creating && html`<${NewSession} onClose=${() => setCreating(false)} onCreated=${openSession} />`}
  </div>`;
}

const SUBS = ['', 'control', 'grid', 'cron', 'settings'];
function Top({ sub }) {
  const conn = useStore(agents, s => s.conn);
  const live = useStore(agents, s => s.live);
  const waiting = useStore(agents, s => s.approvals.filter(a => a.kind !== 'idle').length);
  const current = sub === 's' ? '' : sub;
  const tabs = SUBS.map(v => ({ href: '#/agents' + (v ? '/' + v : ''), label: lt('nav', v) + (v === 'control' && waiting ? ' · ' + waiting : ''), current: current === v }));
  const link = officialLink('');
  return html`<div class="cm-top">
    <button class="icon-btn only-mobile" aria-label=${t('app.main.menu')} onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button>
    <${TabNav} id="agents" label=${t('agents.title')} tabs=${tabs} />
    <span class="grow"></span>
    <span class=${cls('cm-conn', conn.state === 'ready' && live && 'ok')} title=${conn.url}>
      <i class=${cls('dot', conn.state === 'ready' ? (live ? 'green' : 'amber') : 'red')}></i>
      <span class="hide-narrow">${conn.state === 'ready' ? 'Codeman ' + (conn.version || '') : lt('conn', conn.state)}</span></span>
    ${link && html`<a class="icon-btn hide-narrow" href=${link} target="codeman" rel="noopener" title=${t('agents.official')} aria-label=${t('agents.official')}><${Icon} n="link" /></a>`}
  </div>`;
}

export function AgentsPage({ route }) {
  const state = useStore(agents, s => s.conn.state);
  useEffect(() => { refreshConnection(); return useAgentsLive(); }, []);
  const sub = SUBS.includes(route.sub) || route.sub === 's' ? route.sub : '';
  const gated = sub !== 'settings' && state !== 'ready';
  return html`<div class="view cm">
    <${Top} sub=${sub} />
    ${gated ? html`<div class="cm-gate">${state === 'idle' || state === 'connecting' ? html`<div class="cm-gate-in"><span class="spinner"></span><p class="muted">${t('agents.conn.connecting')}</p></div>` : html`<${ConnectionPanel} gate />`}</div>`
      : sub === 'control' ? html`<${ControlCenter} />`
      : sub === 'grid' ? html`<${Mosaic} />`
      : sub === 'cron' ? html`<${CronPage} />`
      : sub === 'settings' ? html`<div class="cm-scroll"><${ConnectionPanel} /></div>`
      : html`<${Workspace} route=${route} />`}
  </div>`;
}
