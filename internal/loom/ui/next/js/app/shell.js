// Coquille : barre latérale (navigation, projets, récents, carte moteur) et
// zone principale routée.
import { html, useState, useStore, useEffect, cls } from '../core/lib.js';
import { newProjectDiscussion } from '../features/projects/page.js';
import { Icon } from '../ui/icons.js';
import { Menu } from '../ui/controls.js';
import { prompt, confirm, toast } from '../ui/dialog.js';
import { get, post } from '../core/api.js';
import { app, go, engineState, refreshNav } from '../core/state.js';
import { chat, open, newDiscussion } from '../features/chat/engine.js';
import { NAV_ITEMS } from './routes.js';
import { Activity } from './activity.js';
import { openPalette } from './palette.js';

const COLORS = ['#7c93e8', '#d49a4a', '#5bb58a', '#c07ad8', '#d9776d', '#63b3c9'];
const projColor = id => COLORS[[...String(id)].reduce((a, c) => a + c.charCodeAt(0), 0) % COLORS.length];

// Déplacer une discussion : archive native ou discussion commune (on garde son titre et ses instructions).
async function moveTo(c, projectId) {
  let r;
  if (!c.workspace) r = await post('/api/chat/history/move', { id: c.id, project_id: projectId });
  else {
    const cur = await get('/api/runtime/sessions?id=' + encodeURIComponent(c.id));
    if (!cur.ok) return toast(cur.error || 'Discussion introuvable', 'err');
    r = await post('/api/runtime/sessions/configure', { id: c.id, title: cur.session.title || '', project_id: projectId, instructions: cur.session.instructions || '', context_revision: (cur.context && cur.context.revision) || '' });
  }
  if (r.ok === false) toast(r.error || 'Déplacement impossible', 'err'); refreshNav();
}

function ChatLink({ c, active }) {
  const [menu, setMenu] = useState(null);
  const running = c.status === 'running';
  const items = [
    { label: 'Renommer', icon: 'edit', run: async () => {
      const name = await prompt('Renommer la discussion', { value: c.title || '' });
      if (!name) return;
      let r;
      if (c.workspace) {
        const cur = await get('/api/runtime/sessions?id=' + encodeURIComponent(c.id));
        r = cur.ok ? await post('/api/runtime/sessions/configure', { id: c.id, title: name, project_id: cur.session.project_id || '', instructions: cur.session.instructions || '', context_revision: (cur.context && cur.context.revision) || '' }) : cur;
      } else r = await post('/api/chat/history/rename', { id: c.id, title: name });
      if (!r.ok) toast(r.error || 'Impossible de renommer', 'err'); refreshNav();
    } },
    ...(!c.workspace ? [{ label: c.fav ? 'Désépingler' : 'Épingler', icon: 'star', run: async () => {
      const r = await post('/api/chat/history/fav', { id: c.id, fav: !c.fav }); if (r.ok === false) toast(r.error || 'Impossible', 'err'); refreshNav();
    } }] : []),
    ...app.get().nav.projects.filter(p => p.id !== c.project_id).map(p => ({ label: 'Déplacer vers ' + p.name, icon: 'folder', run: () => moveTo(c, p.id) })),
    ...(c.project_id ? [{ label: 'Retirer du projet', icon: 'folder', run: () => moveTo(c, '') }] : []),
    '-',
    { label: 'Supprimer', icon: 'trash', danger: true, run: async () => {
      if (!await confirm('Supprimer la discussion', '« ' + (c.title || 'Discussion') + ' » sera supprimée définitivement.', { ok: 'Supprimer', danger: true })) return;
      const r = c.workspace ? await post('/api/runtime/sessions/delete', { id: c.id }) : await post('/api/chat/history/delete', { id: c.id });
      if (!r.ok) toast(r.error || 'Suppression impossible', 'err'); refreshNav();
    } },
  ];
  return html`<div class="chat-link" aria-current=${active ? 'page' : undefined} role="button" tabindex="0"
      onClick=${() => { open(c.id); go('chat'); }} onKeyDown=${e => e.key === 'Enter' && (open(c.id), go('chat'))}>
    ${running && html`<i class="run"></i>`}<span>${c.title || 'Nouvelle discussion'}</span>
    <button class="icon-btn more" aria-label="Actions" onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
  </div>`;
}

function EngineCard() {
  const { status, gpus } = useStore(app, s => ({ status: s.status, gpus: s.gpus }));
  const st = engineState(status), g = (gpus || [])[0];
  const pct = g && g.total ? Math.round(g.used * 100 / g.total) : 0;
  return html`<button class="engine-card" onClick=${() => go('local', 'engine')} aria-label=${'Moteur local : ' + st.label}>
    <span class="top"><i class=${'dot ' + st.tone}></i><span>${st.label}</span>
      <em>${g ? `${(g.used / 1024).toFixed(1)} / ${(g.total / 1024).toFixed(1)} Go` : 'llama.cpp'}</em></span>
    ${g && html`<span class="gauge"><i style=${`width:${pct}%`}></i></span>`}
  </button>`;
}

export function Sidebar() {
  const { route, nav, open: sideOpen, status } = useStore(app, s => ({ route: s.route, nav: s.nav, open: s.sideOpen, status: s.status }));
  const current = useStore(chat, s => s.sessionId);
  const activeId = current || nav.active;
  const [openProj, setOpenProj] = useState(() => new Set(JSON.parse(localStorage.getItem('loom.next.proj') || '[]')));
  const toggleProj = id => { const n = new Set(openProj); n.has(id) ? n.delete(id) : n.add(id); setOpenProj(n); localStorage.setItem('loom.next.proj', JSON.stringify([...n])); };
  const newProject = async () => {
    const name = await prompt('Nouveau projet', { placeholder: 'Nom du projet', ok: 'Créer' });
    if (!name) return;
    const r = await post('/api/projects/context', { id: '', name, directory: '', instructions: '', capability_ids: [] });
    if (!r.ok) toast(r.error || 'Création impossible', 'err'); refreshNav();
  };
  useEffect(() => {
    const key = e => {
      const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
      if (mod && !e.shiftKey && k === 'k') { e.preventDefault(); openPalette(); }
      else if (mod && e.shiftKey && k === 'o') { e.preventDefault(); newDiscussion(); }
    };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
  const loose = nav.conversations.filter(c => !c.project_id);
  return html`<aside class="side" aria-label="Navigation">
    <div class="brand"><b><svg viewBox="0 0 16 16" aria-hidden="true"><rect width="16" height="16" rx="4.5" fill="currentColor"></rect><g fill="none" stroke="var(--bg)" stroke-width="1.3" stroke-linecap="round"><path d="M4 6.2h8M4 9.8h8M6.2 4v8M9.8 4v8"></path></g></svg>Loom</b>
      <span class="brand-acts"><button class="icon-btn" aria-label="Rechercher" title="Rechercher · Ctrl K" onClick=${openPalette}><${Icon} n="search" /></button><${Activity} /></span>
      <button class="icon-btn only-mobile" aria-label="Fermer" onClick=${() => app.set({ sideOpen: false })}><${Icon} n="close" /></button></div>
    <button class="new-chat" onClick=${() => newDiscussion()}><${Icon} n="plus" />Nouvelle discussion<kbd><span class="kbd">Ctrl</span><span class="kbd">Maj</span><span class="kbd">O</span></kbd></button>
    <nav class="nav">${NAV_ITEMS.map(r => html`<a href=${'#/' + r.id} aria-current=${route.section === r.id ? 'page' : undefined}><${Icon} n=${r.nav.icon} />${r.nav.label}</a>`)}</nav>
    <div class="side-sec">
      <div class="side-sec-h"><span>Projets</span><button class="icon-btn" style="width:24px;height:24px" aria-label="Nouveau projet" onClick=${newProject}><${Icon} n="plus" /></button></div>
      <div class="side-list">${nav.projects.map(p => html`<div class="proj">
        <button class="chat-link" onClick=${() => toggleProj(p.id)} aria-expanded=${String(openProj.has(p.id))}>
          <i class="proj-dot" style=${`background:${projColor(p.id)}`}></i><span>${p.name}</span>
          <span class="icon-btn more" role="button" tabindex="0" aria-label="Réglages du projet" onClick=${e => { e.stopPropagation(); go('project', p.id); }}><${Icon} n="gear" /></span>
          <${Icon} n=${openProj.has(p.id) ? 'chevron' : 'right'} class="proj-caret" /></button>
        ${openProj.has(p.id) && html`<div class="proj-chats anim-fade">
          ${nav.conversations.filter(c => c.project_id === p.id).map(c => html`<${ChatLink} key=${c.id} c=${c} active=${route.section === 'chat' && c.id === activeId} />`)}
          <button class="chat-link sub" onClick=${() => newProjectDiscussion(p)}><${Icon} n="plus" /><span>Nouvelle discussion</span></button>
        </div>`}</div>`)}
      </div>
    </div>
    <div class="side-sec grow">
      <div class="side-sec-h"><span>Récents</span></div>
      <div class="side-list">${loose.map(c => html`<${ChatLink} key=${c.id} c=${c} active=${route.section === 'chat' && c.id === activeId} />`)}</div>
    </div>
    <div class="side-foot">
      <${EngineCard} />
      <div class="me">
        <span class="avatar-i" aria-hidden="true">${(status && status.hostname || 'L').slice(0, 1).toUpperCase()}</span>
        <span class="who"><b>${(status && status.hostname) || 'Cette machine'}</b><small>${status && status.version ? 'Loom ' + status.version : 'Loom'}</small></span>
        <a class="icon-btn" href="#/settings" aria-label="Réglages" title="Réglages" aria-current=${route.section === 'settings' ? 'page' : undefined}><${Icon} n="gear" /></a>
      </div>
    </div>
  </aside>`;
}
