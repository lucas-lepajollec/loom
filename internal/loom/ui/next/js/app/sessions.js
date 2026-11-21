// Liste unique des sessions de la barre latérale : les discussions de Loom et
// les sessions d'agent de Codeman, rangées dans les mêmes dossiers puis de la
// plus récente à la plus ancienne. Un dossier ne fait que ranger : aucun
// contexte n'est partagé entre ses sessions.
import { t } from '../core/i18n.js';
import { html, useState, useStore, useEffect, cls, createStore } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { Menu } from '../ui/controls.js';
import { prompt, confirm, toast } from '../ui/dialog.js';
import { get, post } from '../core/api.js';
import { app, go, refreshNav } from '../core/state.js';
import { chat, open, newDiscussion } from '../features/chat/engine.js';
import { agents, stateOf, sessionList, useAgentsPresence, restorable } from '../features/agents/store.js';
import { closeSession, renameSession } from '../features/agents/actions.js';
import { CliMark } from '../features/agents/new-session.js';
import { openSession } from '../features/agents/nav.js';
import { layout, toggleGrid } from '../features/agents/layout.js';
import { DRAG_TYPE } from '../features/agents/grid.js';
import { lt } from '../features/agents/labels.js';
import { NewSessionHost } from '../features/agents/side.js';
import { ago } from '../features/agents/rail.js';

const load = (k, d) => { try { return JSON.parse(localStorage.getItem(k)) ?? d; } catch (_) { return d; } };
const save = (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch (_) {} };

// Dossier de chaque session Codeman, gardé par Loom.
export const agentFolders = createStore({ map: {} });
export const refreshAgentFolders = () => get('/api/agent-folders').then(r => { if (r && r.ok) agentFolders.set({ map: r.folders || {} }); }).catch(() => {});

const FILTERS = ['all', 'chats', 'agents'];
const filterLabel = f => t(f === 'chats' ? 'side.filter_chats' : f === 'agents' ? 'side.filter_agents' : 'side.filter_all');

// Ranger une discussion : archive native ou discussion commune.
async function fileChat(c, folder) {
  let r;
  if (!c.workspace) r = await post('/api/chat/history/move', { id: c.id, project_id: folder });
  else {
    const cur = await get('/api/runtime/sessions?id=' + encodeURIComponent(c.id));
    if (!cur.ok) return toast(cur.error || t('app.shell.discussion_introuvable'), 'err');
    r = await post('/api/runtime/sessions/configure', { id: c.id, title: cur.session.title || '', project_id: folder, instructions: cur.session.instructions || '', context_revision: (cur.context && cur.context.revision) || '' });
  }
  if (r.ok === false) toast(r.error || t('app.shell.deplacement_impossible'), 'err');
  refreshNav();
}
async function fileAgent(id, folder) {
  const r = await post('/api/agent-folders', { id, folder });
  if (r.ok === false) toast(r.error || t('app.shell.deplacement_impossible'), 'err');
  else agentFolders.set({ map: r.folders || {} });
}
const fileItem = (it, folder) => (it.kind === 'chat' ? fileChat(it.c, folder) : fileAgent(it.id, folder));

function moveItems(it) {
  const folders = app.get().nav.projects || [];
  return [
    ...folders.filter(p => p.id !== it.folder).map(p => ({ label: t('side.move_to', { name: p.name }), icon: 'folder', run: () => fileItem(it, p.id) })),
    ...(it.folder ? [{ label: t('side.remove_from_folder'), icon: 'folder', run: () => fileItem(it, '') }] : []),
  ];
}

function chatMenu(it) {
  const c = it.c;
  return [
    { label: t('app.shell.renommer'), icon: 'edit', run: async () => {
      const name = await prompt(t('app.shell.renommer_la_discussion'), { value: c.title || '' });
      if (!name) return;
      let r;
      if (c.workspace) {
        const cur = await get('/api/runtime/sessions?id=' + encodeURIComponent(c.id));
        r = cur.ok ? await post('/api/runtime/sessions/configure', { id: c.id, title: name, project_id: cur.session.project_id || '', instructions: cur.session.instructions || '', context_revision: (cur.context && cur.context.revision) || '' }) : cur;
      } else r = await post('/api/chat/history/rename', { id: c.id, title: name });
      if (!r.ok) toast(r.error || t('app.shell.impossible_de_renommer'), 'err'); refreshNav();
    } },
    ...(!c.workspace ? [{ label: c.fav ? t('app.shell.desepingler') : t('app.shell.epingler'), icon: 'star', run: async () => {
      const r = await post('/api/chat/history/fav', { id: c.id, fav: !c.fav }); if (r.ok === false) toast(r.error || t('app.shell.impossible'), 'err'); refreshNav();
    } }] : []),
    ...moveItems(it),
    '-',
    { label: t('app.shell.supprimer'), icon: 'trash', danger: true, run: async () => {
      if (!await confirm(t('app.shell.supprimer_la_discussion'), '« ' + (c.title || t('app.shell.discussion')) + t('app.shell.sera_supprimee_definitivement'), { ok: t('app.shell.supprimer'), danger: true })) return;
      const r = c.workspace ? await post('/api/runtime/sessions/delete', { id: c.id }) : await post('/api/chat/history/delete', { id: c.id });
      if (!r.ok) toast(r.error || t('app.shell.suppression_impossible'), 'err'); refreshNav();
    } },
  ];
}

function agentMenu(it) {
  const s = it.s;
  return [
    { label: t('agents.session.rename'), icon: 'edit', run: async () => {
      const name = await prompt(t('agents.session.rename'), { value: s.name || '' });
      if (name && name !== s.name) renameSession(s.id, name).catch(e => toast(e.message, 'err'));
    } },
    ...moveItems(it),
    '-',
    { label: t('agents.session.close'), icon: 'trash', danger: true, run: async () => {
      if (!await confirm(t('agents.session.close_title'), t('agents.session.close_text', { name: s.name || s.id.slice(0, 8) }), { ok: t('agents.session.close'), danger: true })) return;
      try { await closeSession(s.id, true); if (app.get().route.id === s.id) go('agents'); } catch (e) { toast(e.message, 'err'); }
    } },
  ];
}

let dragged = null;
function Row({ it, current }) {
  const [menu, setMenu] = useState(null);
  const agent = it.kind === 'agent';
  const state = agent ? stateOf(it.s) : '';
  const busy = agent ? state === 'working' || state === 'starting' : it.c.status === 'running';
  const activate = () => { if (agent) openSession(it.id); else { open(it.id); go('chat'); } app.set({ sideOpen: false }); };
  return html`<div class=${cls('chat-link', 'ses-row', agent && 'st-' + state)} aria-current=${current ? 'page' : undefined} role="button" tabindex="0" draggable="true" title=${agent ? it.s.workingDir || '' : ''}
      onDragStart=${e => { e.dataTransfer.effectAllowed = 'move'; if (agent) e.dataTransfer.setData(DRAG_TYPE, it.id); else e.dataTransfer.setData('application/x-loom-chat', it.id); dragged = it; }}
      onDragEnd=${() => { dragged = null; }}
      onClick=${activate} onKeyDown=${e => e.key === 'Enter' && activate()}>
    <span class="ses-ico">${agent ? html`<${CliMark} mode=${it.s.mode} size="xs" />` : html`<${Icon} n="chat" />`}</span>
    <span>${it.title}</span>
    ${agent && state === 'waiting' ? html`<i class="dot blue" title=${lt('state', state)}></i>`
      : busy ? html`<i class="run" title=${agent ? lt('state', state) : ''}></i>`
      : agent && state === 'error' ? html`<i class="dot red" title=${lt('state', state)}></i>`
      : agent && state === 'limited' ? html`<i class="dot" title=${lt('state', state)}></i>`
      : it.at ? html`<small class="ses-when">${ago(it.at)}</small>` : ''}
    <button class="icon-btn more" aria-label=${t('app.shell.actions')} onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${agent ? agentMenu(it) : chatMenu(it)} />`}
  </div>`;
}

function dropZone(folder, setOver) {
  const accept = () => dragged && (dragged.folder || '') !== folder;
  return {
    onDragOver: e => { if (accept()) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; setOver(true); } },
    onDragLeave: () => setOver(false),
    onDrop: e => { setOver(false); if (!accept()) return; e.preventDefault(); const it = dragged; dragged = null; fileItem(it, folder); },
  };
}

function Folder({ p, items, open: isOpen, toggle, isCurrent }) {
  const [menu, setMenu] = useState(null);
  const [over, setOver] = useState(false);
  const items_ = [
    { label: t('side.new_here'), icon: 'plus', run: () => newDiscussion(p.id) },
    { label: t('side.rename_folder'), icon: 'edit', run: async () => {
      const name = await prompt(t('side.rename_folder'), { value: p.name });
      if (!name || name === p.name) return;
      const r = await post('/api/projects/rename', { id: p.id, name }); if (!r.ok) toast(r.error || t('app.shell.impossible_de_renommer'), 'err'); refreshNav();
    } },
    '-',
    { label: t('side.delete_folder'), icon: 'trash', danger: true, run: async () => {
      if (!await confirm(t('side.delete_folder'), t('side.delete_folder_text', { name: p.name }), { ok: t('app.shell.supprimer'), danger: true })) return;
      const r = await post('/api/projects/delete', { id: p.id }); if (!r.ok) toast(r.error || t('app.shell.suppression_impossible'), 'err');
      refreshNav(); refreshAgentFolders();
    } },
  ];
  return html`<div class=${cls('ses-folder', over && 'drop-on')} ...${dropZone(p.id, setOver)}>
    <div class="chat-link ses-folder-h" role="button" tabindex="0" aria-expanded=${String(isOpen)} onClick=${toggle} onKeyDown=${e => e.key === 'Enter' && toggle()}>
      <span class="ses-ico"><${Icon} n="folder" /></span><span>${p.name}</span>
      ${!isOpen && items.length > 0 && html`<small>${items.length}</small>`}
      <button class="icon-btn more" aria-label=${t('side.folder_actions')} onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    </div>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items_} />`}
    ${isOpen && html`<div class="ses-folder-in">${items.map(it => html`<${Row} key=${it.kind + it.id} it=${it} current=${isCurrent(it)} />`)}</div>`}
  </div>`;
}

export function Sessions() {
  useAgentsPresence();
  const { route, nav } = useStore(app, s => ({ route: s.route, nav: s.nav }));
  const conn = useStore(agents, s => s.conn);
  useStore(agents, s => [s.order, s.sessions]);
  const folderMap = useStore(agentFolders, s => s.map);
  const chatId = useStore(chat, s => s.sessionId) || nav.active;
  const [filter, setFilterState] = useState(() => { const f = load('loom.side.filter', 'all'); return FILTERS.includes(f) ? f : 'all'; });
  const [openFolders, setOpenFolders] = useState(() => new Set(load('loom.next.proj', [])));
  const [fmenu, setFmenu] = useState(null);
  const [overLoose, setOverLoose] = useState(false);
  useEffect(() => { refreshAgentFolders(); }, []);
  const setFilter = f => { setFilterState(f); save('loom.side.filter', f); };
  const toggleFolder = id => { const n = new Set(openFolders); n.has(id) ? n.delete(id) : n.add(id); setOpenFolders(n); save('loom.next.proj', [...n]); };
  const agentsOn = conn.configured;
  const ready = conn.state === 'ready';
  const cut = useStore(agents, restorable);

  const chats = filter === 'agents' ? [] : nav.conversations.map(c => ({ kind: 'chat', id: c.id, c, title: c.title || t('app.shell.nouvelle_discussion'), folder: c.project_id || '', pinned: !!c.fav, at: c.updated_at || c.saved_at || 0 }));
  const live = filter === 'chats' || !agentsOn ? [] : sessionList().map(s => ({ kind: 'agent', id: s.id, s, title: s.name || s.id.slice(0, 8), folder: folderMap[s.id] || '', pinned: false, at: s.lastActivityAt || s.createdAt || 0 }));
  const folders = nav.projects || [];
  const known = new Set(folders.map(p => p.id));
  const all = [...chats, ...live].map(it => (known.has(it.folder) ? it : { ...it, folder: '' }))
    .sort((a, b) => (b.pinned - a.pinned) || (b.at - a.at));
  const inFolder = id => all.filter(it => it.folder === id);
  const loose = all.filter(it => !it.folder);
  const onAgents = route.section === 'agents' && route.sub === 's';
  const isCurrent = it => (it.kind === 'agent' ? onAgents && route.id === it.id : route.section === 'chat' && it.id === chatId);

  const newFolder = async () => {
    const name = await prompt(t('side.new_folder'), { placeholder: t('side.folder_name'), ok: t('app.shell.creer') });
    if (!name) return;
    const r = await post('/api/projects', { name });
    if (!r.ok) toast(r.error || t('app.shell.creation_impossible'), 'err'); refreshNav();
  };
  const filterItems = FILTERS.map(f => ({ label: filterLabel(f), icon: f === filter ? 'check' : '', run: () => setFilter(f) }));
  const mosaic = agentsOn && ready && innerWidth > 760;

  return html`<div class="side-sec grow ses">
    <div class="side-sec-h ses-h">
      <button class="ses-filter" aria-haspopup="menu" aria-expanded=${String(!!fmenu)} onClick=${e => setFmenu(fmenu ? null : e.currentTarget)}>${filterLabel(filter)}<${Icon} n="chevron" /></button>
      <span class="grow"></span>
      <button class="icon-btn" aria-label=${t('side.new_folder')} title=${t('side.new_folder')} onClick=${newFolder}><${Icon} n="folder" /></button>
      ${mosaic && html`<button class=${cls('icon-btn', layout.get().count > 1 && route.section === 'agents' && 'on')} aria-label=${t('side.mosaic')} title=${t('side.mosaic')}
        onClick=${() => { if (layout.get().count === 1) toggleGrid(route.section === 'agents' && route.sub === 's' ? route.id : ''); go('agents', 'grid'); }}><${Icon} n="grid" /></button>`}
    </div>
    ${fmenu && html`<${Menu} anchor=${fmenu} onClose=${() => setFmenu(null)} items=${filterItems} />`}
    <div class=${cls('side-list', overLoose && 'drop-on')} ...${dropZone('', setOverLoose)}>
      ${agentsOn && ready && cut.length > 0 && filter !== 'chats' && html`<a class="chat-link sub ses-restore" href="#/agents"><${Icon} n="refresh" /><span>${t('agents.restore.side', { n: cut.length })}</span></a>`}
      ${folders.map(p => html`<${Folder} key=${p.id} p=${p} items=${inFolder(p.id)} open=${openFolders.has(p.id)} toggle=${() => toggleFolder(p.id)} isCurrent=${isCurrent} />`)}
      ${loose.map(it => html`<${Row} key=${it.kind + it.id} it=${it} current=${isCurrent(it)} />`)}
      ${!all.length && !folders.length && html`<p class="ses-empty">${t('side.empty')}</p>`}
      ${agentsOn && !ready && filter !== 'chats' && html`<a class="chat-link sub" href="#/agents"><i class="dot amber"></i><span>${lt('conn', conn.state)}</span></a>`}
    </div>
    <${NewSessionHost} />
  </div>`;
}
