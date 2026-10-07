import { t } from '../core/i18n.js';
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
import { groupOf } from './sections.js';
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
    if (!cur.ok) return toast(cur.error || t("app.shell.discussion_introuvable"), 'err');
    r = await post('/api/runtime/sessions/configure', { id: c.id, title: cur.session.title || '', project_id: projectId, instructions: cur.session.instructions || '', context_revision: (cur.context && cur.context.revision) || '' });
  }
  if (r.ok === false) toast(r.error || t("app.shell.deplacement_impossible"), 'err'); refreshNav();
}

function ChatLink({ c, active }) {
  const [menu, setMenu] = useState(null);
  const running = c.status === 'running';
  const items = [
    { label: t("app.shell.renommer"), icon: 'edit', run: async () => {
      const name = await prompt(t("app.shell.renommer_la_discussion"), { value: c.title || '' });
      if (!name) return;
      let r;
      if (c.workspace) {
        const cur = await get('/api/runtime/sessions?id=' + encodeURIComponent(c.id));
        r = cur.ok ? await post('/api/runtime/sessions/configure', { id: c.id, title: name, project_id: cur.session.project_id || '', instructions: cur.session.instructions || '', context_revision: (cur.context && cur.context.revision) || '' }) : cur;
      } else r = await post('/api/chat/history/rename', { id: c.id, title: name });
      if (!r.ok) toast(r.error || t("app.shell.impossible_de_renommer"), 'err'); refreshNav();
    } },
    ...(!c.workspace ? [{ label: c.fav ? t("app.shell.desepingler") : t("app.shell.epingler"), icon: 'star', run: async () => {
      const r = await post('/api/chat/history/fav', { id: c.id, fav: !c.fav }); if (r.ok === false) toast(r.error || t("app.shell.impossible"), 'err'); refreshNav();
    } }] : []),
    ...app.get().nav.projects.filter(p => p.id !== c.project_id).map(p => ({ label: t("app.shell.deplacer_vers") + p.name, icon: 'folder', run: () => moveTo(c, p.id) })),
    ...(c.project_id ? [{ label: t("app.shell.retirer_du_projet"), icon: 'folder', run: () => moveTo(c, '') }] : []),
    '-',
    { label: t("app.shell.supprimer"), icon: 'trash', danger: true, run: async () => {
      if (!await confirm(t("app.shell.supprimer_la_discussion"), '« ' + (c.title || t("app.shell.discussion")) + t("app.shell.sera_supprimee_definitivement"), { ok: t("app.shell.supprimer"), danger: true })) return;
      const r = c.workspace ? await post('/api/runtime/sessions/delete', { id: c.id }) : await post('/api/chat/history/delete', { id: c.id });
      if (!r.ok) toast(r.error || t("app.shell.suppression_impossible"), 'err'); refreshNav();
    } },
  ];
  return html`<div class="chat-link" aria-current=${active ? 'page' : undefined} role="button" tabindex="0"
      onClick=${() => { open(c.id); go('chat'); }} onKeyDown=${e => e.key === 'Enter' && (open(c.id), go('chat'))}>
    ${running && html`<i class="run"></i>`}<span>${c.title || t("app.shell.nouvelle_discussion")}</span>
    <button class="icon-btn more" aria-label="${t("app.shell.actions")}" onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
  </div>`;
}

function EngineCard() {
  const { status, gpus } = useStore(app, s => ({ status: s.status, gpus: s.gpus }));
  const st = engineState(status), g = (gpus || [])[0];
  const pct = g && g.total ? Math.round(g.used * 100 / g.total) : 0;
  return html`<button class="engine-card" onClick=${() => go('engine')} aria-label=${t("app.shell.moteur_local") + st.label}>
    <span class="top"><i class=${'dot ' + st.tone}></i><span>${st.label}</span>
      <em>${g ? `${(g.used / 1024).toFixed(1)} / ${(g.total / 1024).toFixed(1)}${t('common.units.gb')}` : 'llama.cpp'}</em></span>
    ${g && html`<span class="gauge"><i style=${`width:${pct}%`}></i></span>`}
  </button>`;
}

export function Sidebar() {
  const { route, nav, open: sideOpen, serverInfo } = useStore(app, s => ({ route: s.route, nav: s.nav, open: s.sideOpen, serverInfo: s.serverInfo }));
  const current = useStore(chat, s => s.sessionId);
  const activeId = current || nav.active;
  const [openProj, setOpenProj] = useState(() => new Set(JSON.parse(localStorage.getItem('loom.next.proj') || '[]')));
  const toggleProj = id => { const n = new Set(openProj); n.has(id) ? n.delete(id) : n.add(id); setOpenProj(n); localStorage.setItem('loom.next.proj', JSON.stringify([...n])); };
  const newProject = async () => {
    const name = await prompt(t("app.shell.nouveau_projet"), { placeholder: t("app.shell.nom_du_projet"), ok: t("app.shell.creer") });
    if (!name) return;
    const r = await post('/api/projects/context', { id: '', name, directory: '', instructions: '', capability_ids: [] });
    if (!r.ok) toast(r.error || t("app.shell.creation_impossible"), 'err'); refreshNav();
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
  return html`<aside class="side" aria-label="${t("app.shell.navigation")}">
    <div class="brand"><b><svg viewBox="0 0 16 16" aria-hidden="true"><rect width="16" height="16" rx="4.5" fill="currentColor"></rect><g fill="none" stroke="var(--bg)" stroke-width="1.3" stroke-linecap="round"><path d="M4 6.2h8M4 9.8h8M6.2 4v8M9.8 4v8"></path></g></svg>${t("app.shell.loom")}</b>
      <span class="brand-acts"><button class="icon-btn" aria-label="${t("app.shell.rechercher")}" title="${t("app.shell.rechercher_ctrl_k")}" onClick=${openPalette}><${Icon} n="search" /></button><${Activity} /></span>
      <button class="icon-btn only-mobile" aria-label="${t("app.shell.fermer")}" onClick=${() => app.set({ sideOpen: false })}><${Icon} n="close" /></button></div>
    <button class="new-chat" onClick=${() => newDiscussion()}><${Icon} n="plus" />${t("app.shell.nouvelle_discussion")}<kbd><span class="kbd">${t("app.shell.ctrl")}</span><span class="kbd">${t("app.shell.maj")}</span><span class="kbd">${t("app.shell.o")}</span></kbd></button>
    <nav class="nav">${NAV_ITEMS.map(r => html`<a href=${r.href} aria-current=${groupOf(route.section)?.id === r.id ? 'page' : undefined}><${Icon} n=${r.nav.icon} />${r.nav.label}</a>`)}</nav>
    <div class="side-sec">
      <div class="side-sec-h"><span>${t("app.shell.projets")}</span><button class="icon-btn" style="width:24px;height:24px" aria-label="${t("app.shell.nouveau_projet")}" onClick=${newProject}><${Icon} n="plus" /></button></div>
      <div class="side-list">${nav.projects.map(p => html`<div class="proj">
        <button class="chat-link" onClick=${() => toggleProj(p.id)} aria-expanded=${String(openProj.has(p.id))}>
          <i class="proj-dot" style=${`background:${projColor(p.id)}`}></i><span>${p.name}</span>
          <span class="icon-btn more" role="button" tabindex="0" aria-label="${t("app.shell.reglages_du_projet")}" onClick=${e => { e.stopPropagation(); go('project', p.id); }}><${Icon} n="gear" /></span>
          <${Icon} n=${openProj.has(p.id) ? 'chevron' : 'right'} class="proj-caret" /></button>
        ${openProj.has(p.id) && html`<div class="proj-chats anim-fade">
          ${nav.conversations.filter(c => c.project_id === p.id).map(c => html`<${ChatLink} key=${c.id} c=${c} active=${route.section === 'chat' && c.id === activeId} />`)}
          <button class="chat-link sub" onClick=${() => newProjectDiscussion(p)}><${Icon} n="plus" /><span>${t("app.shell.nouvelle_discussion")}</span></button>
        </div>`}</div>`)}
      </div>
    </div>
    <div class="side-sec grow">
      <div class="side-sec-h"><span>${t("app.shell.recents")}</span></div>
      <div class="side-list">${loose.map(c => html`<${ChatLink} key=${c.id} c=${c} active=${route.section === 'chat' && c.id === activeId} />`)}</div>
    </div>
    <div class="side-foot">
      <${EngineCard} />
      <div class="me">
        <span class="avatar-i" aria-hidden="true">${(serverInfo && serverInfo.hostname || 'L').slice(0, 1).toUpperCase()}</span>
        <span class="who"><b>${(serverInfo && serverInfo.hostname) || t("app.shell.cette_machine")}</b><small>${serverInfo && serverInfo.version ? 'Loom ' + serverInfo.version : 'Loom'}</small></span>
        <a class="icon-btn" href="#/settings" aria-label="${t("app.shell.reglages")}" title="${t("app.shell.reglages")}" aria-current=${route.section === 'settings' ? 'page' : undefined}><${Icon} n="gear" /></a>
      </div>
    </div>
  </aside>`;
}
