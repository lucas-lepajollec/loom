// Liste unique des sessions de la barre latérale : les discussions de Loom et
// les sessions d'agent de Codeman, rangées dans les mêmes dossiers puis de la
// plus récente à la plus ancienne. Un dossier ne fait que ranger : aucun
// contexte n'est partagé entre ses sessions.
import { t } from '../core/i18n.js';
import { html, useState, useStore, useEffect, useRef, cls, createStore, baseName } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { Menu, Seg } from '../ui/controls.js';
import { prompt, confirm, toast } from '../ui/dialog.js';
import { get, post } from '../core/api.js';
import { app, go, refreshNav, refreshStatus, runtimeKind } from '../core/state.js';
import { chat, open, newDiscussion } from '../features/chat/engine.js';
import { agents, attentionOf, ATTENTION, sessionList, useRestorable, cliOf, projectOf } from '../features/agents/store.js';
import { orchestration, useOrchestrationLive, visibleParentOf, nodeOf, stopTree, reparentSession } from '../features/agents/orchestration.js';
import { setPanelTab, panelState } from '../features/agents/panel-state.js';
import { closeSession, renameSession } from '../features/agents/actions.js';
import { CliMark } from '../features/agents/new-session.js';
import { openSession } from '../features/agents/nav.js';
import { layout, toggleGrid } from '../features/agents/layout.js';
import { DRAG_TYPE } from '../features/agents/grid.js';
import { lt } from '../features/agents/labels.js';
import { ago, StateDot } from '../features/agents/rail.js';
import { openPalette } from './palette.js';
import { defaultModel, startPhoneDiscussion } from './phone-discussion.js';
import { refreshAgentInbox } from './inbox.js';
import { usePullRefresh } from './touch.js';

const load = (k, d) => { try { return JSON.parse(localStorage.getItem(k)) ?? d; } catch (_) { return d; } };
const save = (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch (_) {} };

// Dossier de chaque session Codeman, gardé par Loom.
export const agentFolders = createStore({ map: {} });
export const refreshAgentFolders = () => get('/api/agent-folders').then(r => { if (r && r.ok) agentFolders.set({ map: r.folders || {} }); }).catch(() => {});

const FILTERS = ['all', 'chats', 'agents'];
const filterLabel = f => f === 'chats' ? t('side.filter_chats') : f === 'agents' ? t('side.filter_agents') : t('side.filter_all');

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
export async function fileAgent(id, folder) {
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

function agentMenu(it, hasKids = false) {
  const s = it.s;
  const node = nodeOf(s.id);
  const parents = sessionList().filter(x => x.id !== s.id && x.id !== node?.parent_id && nodeOf(x.id)?.parent_id !== s.id).slice(0, 6);
  return [
    { label: t('agents.orch.new_subagent'), icon: 'plus', run: () => {
      openSession(s.id);
      setPanelTab('subagents');
      panelState.set({ openRequest: panelState.get().openRequest + 1 });
    } },
    { label: t('agents.session.rename'), icon: 'edit', run: async () => {
      const name = await prompt(t('agents.session.rename'), { value: s.name || '' });
      if (name && name !== s.name) renameSession(s.id, name).catch(e => toast(e.message, 'err'));
    } },
    ...(node?.parent_id ? [{ label: t('agents.orch.detach'), icon: 'left', run: () => reparentSession(s.id, '').then(() => toast(t('agents.orch.detached'))).catch(e => toast(e.message, 'err')) }]
      : parents.slice(0, 3).map(p => ({ label: t('agents.orch.attach_to', { name: p.name || p.id.slice(0, 8) }), icon: 'agent', run: () => reparentSession(s.id, p.id).then(() => toast(t('agents.orch.attached'))).catch(e => toast(e.message, 'err')) }))),
    ...moveItems(it),
    ...(hasKids ? ['-', { label: t('agents.orch.stop_tree'), icon: 'stop', danger: true, run: async () => {
      if (!await confirm(t('agents.orch.stop_tree_title'), t('agents.orch.stop_tree_text'), { ok: t('agents.orch.stop_tree'), danger: true })) return;
      try { await stopTree(s.id, { mode: 'close' }); toast(t('agents.orch.tree_stopped')); } catch (e) { toast(e.message, 'err'); }
    } }] : []),
    '-',
    { label: t('agents.session.close'), icon: 'trash', danger: true, run: async () => {
      if (!await confirm(t('agents.session.close_title'), t('agents.session.close_text', { name: s.name || s.id.slice(0, 8) }), { ok: t('agents.session.close'), danger: true })) return;
      try { await closeSession(s.id, true); if (app.get().route.id === s.id) go('agents'); } catch (e) { toast(e.message, 'err'); }
    } },
  ];
}

function collectTreeIds(parentId, kidsMap, out = []) {
  for (const k of kidsMap.get(parentId) || []) {
    out.push(k);
    collectTreeIds(k.id, kidsMap, out);
  }
  return out;
}

let dragged = null;
function Row({ it, current, kidsMap, collapsed, toggleCollapse, isCurrent, depth = 0 }) {
  const [menu, setMenu] = useState(null);
  const agent = it.kind === 'agent';
  const kids = agent && kidsMap ? (kidsMap.get(it.id) || []) : [];
  const hasKids = kids.length > 0;
  const isOpen = hasKids && !(collapsed && collapsed.has(it.id));
  const orchNode = useStore(orchestration, o => (agent ? o.nodes[it.id] || null : null));
  const state = useStore(agents, () => agent ? attentionOf(it.s) : '');
  const desc = hasKids ? collectTreeIds(it.id, kidsMap) : [];
  const childRollup = useStore(agents, () => {
    if (!desc.length) return { busy: false, attention: '' };
    let busy = false, attention = '';
    for (const c of desc) {
      const st = attentionOf(c.s);
      if (st === 'approval' || st === 'question') { attention = st; break; }
      if (st === 'working' || st === 'starting') busy = true;
    }
    return { busy, attention };
  });
  const busy = agent ? (state === 'working' || state === 'starting' || (!isOpen && childRollup.busy)) : it.c.status === 'running';
  const shownFlag = agent && ATTENTION.has(state) ? state : (!isOpen && childRollup.attention ? childRollup.attention : '');
  const activate = () => { if (agent) openSession(it.id); else { open(it.id); go('chat'); } };
  const branchTag = orchNode?.branch ? orchNode.branch.replace(/^loom\//, '') : '';
  const rowTip = agent ? [orchNode?.role, branchTag && ('⎇ ' + orchNode.branch), it.s.workingDir].filter(Boolean).join(' · ') : '';

  const rowEl = html`<div class=${cls('chat-link', 'ses-row', depth > 0 && 'ses-child', agent && 'st-' + state, busy && 'is-busy')} aria-current=${current ? 'page' : undefined} role="button" tabindex="0" draggable="true" title=${rowTip}
      onDragStart=${e => { e.dataTransfer.effectAllowed = 'move'; if (agent) e.dataTransfer.setData(DRAG_TYPE, it.id); else e.dataTransfer.setData('application/x-loom-chat', it.id); dragged = it; }}
      onDragEnd=${() => { dragged = null; }}
      onClick=${activate} onKeyDown=${e => e.key === 'Enter' && activate()}>
    ${hasKids && html`<button type="button" class="ses-tree-toggle" aria-label=${isOpen ? t('agents.orch.collapse') : t('agents.orch.expand')} aria-expanded=${String(isOpen)}
      onClick=${e => { e.stopPropagation(); toggleCollapse?.(it.id); }}><${Icon} n=${isOpen ? 'chevron' : 'right'} /></button>`}
    <span class="ses-ico">${agent ? html`<${CliMark} mode=${it.s.mode} size="xs" />` : html`<${Icon} n="chat" />`}</span>
    <span class=${cls('ses-title', busy && 'shimmer-text')}>${it.title}</span>
    ${hasKids && html`<em class="ses-tree-cnt" title=${t('agents.orch.children_count', { n: desc.length })}>${desc.length}</em>`}
    ${shownFlag ? html`<small class=${'ses-flag st-' + shownFlag}>${lt('state', shownFlag)}</small>`
      : busy ? html`<span class="loom-pulse ses-pulse" title=${agent ? lt('state', state) : ''} aria-hidden="true"><i></i><i></i><i></i><i></i></span>`
      : agent && state === 'error' ? html`<small class="ses-flag st-error">${lt('state', state)}</small>`
      : agent && state === 'limited' ? html`<small class="ses-flag st-limited">${lt('state', state)}</small>`
      : it.at ? html`<small class="ses-when">${ago(it.at)}</small>` : ''}
    <button class="icon-btn more" aria-label=${t('app.shell.actions')} onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${agent ? agentMenu(it, hasKids) : chatMenu(it)} />`}
  </div>`;

  if (!hasKids) return rowEl;
  return html`<div class="ses-tree">
    ${rowEl}
    ${isOpen && html`<div class="ses-tree-kids">${kids.map(k => html`<${Row} key=${k.kind + k.id} it=${k} current=${isCurrent ? isCurrent(k) : false}
      kidsMap=${kidsMap} collapsed=${collapsed} toggleCollapse=${toggleCollapse} isCurrent=${isCurrent} depth=${depth + 1} />`)}</div>`}
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

function Folder({ p, items, open: isOpen, toggle, isCurrent, phone, kidsMap, collapsed, toggleCollapse }) {
  const [menu, setMenu] = useState(null);
  const [over, setOver] = useState(false);
  const items_ = [
    { label: t('side.new_here'), icon: 'plus', run: () => phone ? agents.set({ creating: { type: 'chat', folder: p.id } }) : newDiscussion(p.id) },
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
  if (phone) return html`<div class="phone-folder">
    <div class="phone-session-row">
      <button class="phone-row-open" type="button" aria-expanded=${String(isOpen)} onClick=${toggle}><${Icon} n="folder" />
        <span><b class="trunc">${p.name}</b><small>${t('mobile.folder_count', { n: items.length })}</small></span><${Icon} n=${isOpen ? 'chevron' : 'right'} />
      </button>
      <button class="icon-btn" type="button" aria-label=${t('side.folder_actions')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="more" /></button>
    </div>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items_} />`}
    ${isOpen && html`<div class="phone-folder-items">${items.map(it => html`<${PhoneSessionRow} key=${it.kind + it.id} it=${it} kidsMap=${kidsMap} />`)}</div>`}
  </div>`;
  return html`<div class=${cls('ses-folder', over && 'drop-on')} ...${dropZone(p.id, setOver)}>
    <div class="chat-link ses-folder-h" role="button" tabindex="0" aria-expanded=${String(isOpen)} onClick=${toggle} onKeyDown=${e => e.key === 'Enter' && toggle()}>
      <span class="ses-ico"><${Icon} n="folder" /></span><span>${p.name}</span>
      ${!isOpen && items.length > 0 && html`<small>${items.length}</small>`}
      <button class="icon-btn more" aria-label=${t('side.folder_actions')} onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    </div>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items_} />`}
    ${isOpen && html`<div class="ses-folder-in">${items.map(it => html`<${Row} key=${it.kind + it.id} it=${it} current=${isCurrent(it)} kidsMap=${kidsMap} collapsed=${collapsed} toggleCollapse=${toggleCollapse} isCurrent=${isCurrent} />`)}</div>`}
  </div>`;
}

function phoneState(it) {
  if (it.kind === 'chat') return it.c.status === 'running' ? 'working' : '';
  return attentionOf(it.s);
}
const usePhoneState = it => useStore(agents, () => phoneState(it));

function PhoneSessionRow({ it, kidsMap, depth = 0 }) {
  const [menu, setMenu] = useState(null);
  const action = useRef(), swipe = useRef(null), hold = useRef(null), suppress = useRef(0);
  const agent = it.kind === 'agent';
  const kids = agent && kidsMap ? (kidsMap.get(it.id) || []) : [];
  const state = usePhoneState(it);
  const orchNode = useStore(orchestration, o => (agent ? o.nodes[it.id] || null : null));
  const model = agent ? [orchNode?.role, cliOf(it.s.mode).label, orchNode?.branch || (it.s.workingDir && projectOf(it.s.workingDir))]
    : [baseName(it.c.model).replace(/\.gguf$/i, ''), it.c.runtime_id && (runtimeKind(it.c.runtime_id) === 'cloud' ? t('chat.picker.cloud')
      : runtimeKind(it.c.runtime_id) === 'local' ? t('chat.picker.local') : t('mobile.archive'))];
  const stateLabel = ['approval', 'question', 'unseen', 'working', 'starting', 'error', 'limited'].includes(state) ? lt('state', state) : '';
  const showMenu = () => { suppress.current = Date.now() + 400; setMenu(action.current); };
  const cancelHold = () => clearTimeout(hold.current);
  useEffect(() => cancelHold, []);
  const row = html`<div class=${cls('phone-session-row', depth > 0 && 'phone-child')} onContextMenu=${e => { e.preventDefault(); showMenu(); }}
    onTouchStart=${e => {
      cancelHold();
      swipe.current = e.touches.length === 1 ? e.touches[0] : null;
      if (swipe.current && e.target.closest('.phone-row-open')) hold.current = setTimeout(showMenu, 500);
    }}
    onTouchMove=${e => {
      const start = swipe.current, p = e.touches[0];
      if (start && (Math.abs(p.clientX - start.clientX) > 10 || Math.abs(p.clientY - start.clientY) > 10)) cancelHold();
    }}
    onTouchCancel=${() => { swipe.current = null; cancelHold(); }}
    onTouchEnd=${e => {
      cancelHold();
      const start = swipe.current, end = e.changedTouches[0];
      swipe.current = null;
      if (start && Math.abs(end.clientX - start.clientX) > 65 && Math.abs(end.clientY - start.clientY) < 24) showMenu();
    }}>
    <button class="phone-row-open" type="button" onClick=${() => {
      if (menu || Date.now() < suppress.current) return;
      if (agent) openSession(it.id); else go('chat', it.id);
    }}>
      ${agent ? html`<${CliMark} mode=${it.s.mode} size="sm" />` : html`<${Icon} n="chat" />`}
      <span><b class=${cls('trunc', (state === 'working' || state === 'starting') && 'shimmer-text')}>${it.title}</b>${model.some(Boolean) && html`<small class="trunc">${model.filter(Boolean).join(' · ')}</small>`}</span>
      ${stateLabel ? html`<small class=${cls('phone-row-state', 'st-' + state, (state === 'working' || state === 'starting') && 'shimmer-text')}>${(state === 'working' || state === 'starting') && html`<span class="loom-pulse" aria-hidden="true"><i></i><i></i><i></i><i></i></span>`}${stateLabel}</small>`
        : it.at ? html`<small class="phone-row-time">${ago(it.at)}</small>` : ''}
    </button>
    <button type="button" class="icon-btn" ref=${action} aria-label=${t('app.shell.actions')} onClick=${showMenu}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${agent ? agentMenu(it, kids.length > 0) : chatMenu(it)} label=${it.title} />`}
  </div>`;
  if (!kids.length) return row;
  return html`<div class="phone-tree">${row}<div class="phone-tree-kids">${kids.map(k => html`<${PhoneSessionRow} key=${k.kind + k.id} it=${k} kidsMap=${kidsMap} depth=${depth + 1} />`)}</div></div>`;
}

function PhoneSessions({ filter, setFilter, items, kidsMap, folders, openFolders, toggleFolder, cut, conn, newFolder }) {
  // Relit les états d'attention quand un accord, une fin ou une ouverture change.
  useStore(agents, st => ({ approvals: st.approvals, completed: st.completed, seen: st.seen }));
  const nativeRunning = useStore(chat, s => s.mode === 'native' && s.busy);
  const nativeId = useStore(app, s => s.nav.active);
  const shown = nativeRunning && nativeId ? items.map(it => it.kind === 'chat' && it.id === nativeId ? { ...it, c: { ...it.c, status: 'running' } } : it) : items;
  const active = shown.filter(it => ['approval', 'question', 'unseen', 'working', 'starting'].includes(phoneState(it)));
  const recent = shown.filter(it => !it.folder && !active.includes(it));
  const model = useStore(app, defaultModel);
  useStore(chat, s => s.localTarget);
  const pull = usePullRefresh(() => Promise.allSettled([refreshNav(), refreshAgentFolders(), refreshStatus(), refreshAgentInbox()]));
  const sessionRows = rows => rows.map(it => html`<${PhoneSessionRow} key=${it.kind + it.id} it=${it} kidsMap=${kidsMap} />`);
  return html`<div class="view phone-page phone-sessions">
    <header class="phone-title"><h1>${t('mobile.sessions')}</h1>
      <button type="button" class="icon-btn" aria-label=${t('app.palette.rechercher')} onClick=${openPalette}><${Icon} n="search" /></button>
      <button type="button" class="icon-btn phone-new" aria-label=${t('side.new_session')} onClick=${() => agents.set({ creating: { type: 'chat' } })}><${Icon} n="plus" /></button>
    </header>
    <div class="phone-filters"><${Seg} label=${t('mobile.filter_sessions')} value=${filter} onChange=${setFilter} options=${FILTERS.map(value => ({ value, label: value === 'all' ? t('mobile.all') : filterLabel(value) }))} /></div>
    <div class="phone-scroll phone-session-list" ...${pull}>
      ${filter !== 'chats' && conn.state === 'ready' && cut.length > 0 && html`<a class="phone-link phone-restore" href="#/inbox"><${Icon} n="refresh" /><span>${t('agents.restore.side', { n: cut.length })}</span><${Icon} n="right" /></a>`}
      ${active.length > 0 && html`<section><h2>${t('mobile.in_progress')}</h2>${sessionRows(active)}</section>`}
      <section><div class="phone-section-head"><h2>${t('mobile.recent')}</h2><button type="button" class="icon-btn" aria-label=${t('side.new_folder')} onClick=${newFolder}><${Icon} n="folder" /></button></div>
        ${sessionRows(recent)}
        ${folders.map(p => html`<${Folder} key=${p.id} p=${p} items=${shown.filter(it => it.folder === p.id)} kidsMap=${kidsMap} open=${openFolders.has(p.id)} toggle=${() => toggleFolder(p.id)} phone />`)}
        ${!items.length && !folders.length && html`<p class="ses-empty">${t('side.empty')}</p>`}
      </section>
      ${conn.configured && conn.state !== 'ready' && filter !== 'chats' && html`<a class="phone-link" href="#/agents"><i class="dot amber"></i><span>${lt('conn', conn.state)}</span><${Icon} n="right" /></a>`}
    </div>
    <div class="phone-quick"><button type="button" class="composer" onClick=${() => startPhoneDiscussion()}><span class="trunc">${model ? t('mobile.write_to', { model }) : t('chat.composer.placeholder')}</span><${Icon} n="mic" /></button></div>
  </div>`;
}

export function SessionsPage() {
  return html`<${Sessions} phone />`;
}

export function Sessions({ phone = false, mode }) {
  const { route, nav, workspaceMode } = useStore(app, s => ({ route: s.route, nav: s.nav, workspaceMode: s.workspaceMode }));
  const conn = useStore(agents, s => s.conn);
  useStore(agents, s => ({ order: s.order, sessions: s.sessions }));
  const orchNodes = useStore(orchestration, o => o.nodes);
  useOrchestrationLive();
  const folderMap = useStore(agentFolders, s => s.map);
  const chatId = useStore(chat, s => s.sessionId) || nav.active;
  const [filter, setFilterState] = useState(() => { const f = load('loom.side.filter', 'all'); return FILTERS.includes(f) ? f : 'all'; });
  const [openFolders, setOpenFolders] = useState(() => new Set(load('loom.next.proj', [])));
  const [collapsed, setCollapsed] = useState(() => new Set(load('loom.orch.collapsed', [])));
  const [overLoose, setOverLoose] = useState(false);
  useEffect(() => { refreshAgentFolders(); }, []);
  const setFilter = f => { setFilterState(f); save('loom.side.filter', f); };
  const toggleFolder = id => { const n = new Set(openFolders); n.has(id) ? n.delete(id) : n.add(id); setOpenFolders(n); save('loom.next.proj', [...n]); };
  const toggleCollapse = id => { const n = new Set(collapsed); n.has(id) ? n.delete(id) : n.add(id); setCollapsed(n); save('loom.orch.collapsed', [...n]); };
  const agentsOn = conn.configured;
  const ready = conn.state === 'ready';
  const cut = useRestorable();
  const activeMode = mode || workspaceMode || 'chat';
  const effectiveFilter = phone ? filter : (activeMode === 'agents' ? 'agents' : 'chats');

  const chats = effectiveFilter === 'agents' ? [] : nav.conversations.map(c => ({ kind: 'chat', id: c.id, c, title: c.title || t('app.shell.nouvelle_discussion'), folder: c.project_id || '', pinned: !!c.fav, at: c.updated_at || c.saved_at || 0 }));
  const live = effectiveFilter === 'chats' || !agentsOn ? [] : sessionList().map(s => ({ kind: 'agent', id: s.id, s, title: s.name || s.id.slice(0, 8), folder: folderMap[s.id] || '', pinned: false, at: s.lastActivityAt || s.createdAt || 0 }));

  // Imbriquer les sous-sessions sous leur parent (sans jamais perdre un enfant si le parent est fermé).
  const visibleAgentIds = new Set(live.map(it => it.id));
  const kidsMap = new Map();
  const topLive = [];
  for (const it of live) {
    const pid = visibleParentOf(it.id, visibleAgentIds, orchNodes);
    if (pid) {
      const list = kidsMap.get(pid) || [];
      list.push(it);
      kidsMap.set(pid, list);
    } else {
      topLive.push(it);
    }
  }
  for (const list of kidsMap.values()) {
    list.sort((a, b) => (a.s.createdAt || 0) - (b.s.createdAt || 0));
  }

  const folders = nav.projects || [];
  const known = new Set(folders.map(p => p.id));
  const all = [...chats, ...topLive].map(it => (known.has(it.folder) ? it : { ...it, folder: '' }))
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
  if (phone) return html`<${PhoneSessions} filter=${filter} setFilter=${setFilter} items=${all} kidsMap=${kidsMap} folders=${folders} openFolders=${openFolders}
    toggleFolder=${toggleFolder} cut=${cut} conn=${conn} newFolder=${newFolder} />`;
  const mosaic = activeMode === 'agents' && agentsOn && ready && innerWidth > 760;

  return html`<div class="side-sec grow ses" key=${activeMode}>
    <div class="side-sec-h ses-h">
      <span class="ses-label">${activeMode === 'agents' ? t('side.sessions_agents') : t('side.sessions_chat')}</span>
      <span class="grow"></span>
      <button class="icon-btn" aria-label=${t('side.new_folder')} title=${t('side.new_folder')} onClick=${newFolder}><${Icon} n="folder" /></button>
      ${mosaic && html`<button class=${cls('icon-btn', layout.get().count > 1 && route.section === 'agents' && 'on')} aria-label=${t('side.mosaic')} title=${t('side.mosaic')}
        onClick=${() => { if (layout.get().count === 1) toggleGrid(route.section === 'agents' && route.sub === 's' ? route.id : ''); go('agents', 'grid'); }}><${Icon} n="grid" /></button>`}
    </div>
    <div class=${cls('side-list', overLoose && 'drop-on')} ...${dropZone('', setOverLoose)}>
      ${activeMode === 'agents' && agentsOn && ready && cut.length > 0 && html`<a class="chat-link sub ses-restore" href="#/agents"><${Icon} n="refresh" /><span>${t('agents.restore.side', { n: cut.length })}</span></a>`}
      ${folders.map(p => html`<${Folder} key=${p.id} p=${p} items=${inFolder(p.id)} kidsMap=${kidsMap} collapsed=${collapsed} toggleCollapse=${toggleCollapse} open=${openFolders.has(p.id)} toggle=${() => toggleFolder(p.id)} isCurrent=${isCurrent} />`)}
      ${loose.map(it => html`<${Row} key=${it.kind + it.id} it=${it} current=${isCurrent(it)} kidsMap=${kidsMap} collapsed=${collapsed} toggleCollapse=${toggleCollapse} isCurrent=${isCurrent} />`)}
      ${!all.length && !folders.length && html`<p class="ses-empty">${t('side.empty')}</p>`}
      ${activeMode === 'agents' && agentsOn && !ready && html`<a class="chat-link sub" href="#/agents"><i class="dot amber"></i><span>${lt('conn', conn.state)}</span></a>`}
    </div>
  </div>`;
}
