import { shortVersion } from '../core/version.js';
import { t, locale } from '../core/i18n.js';
// Coquille : barre latérale (navigation, sessions, carte moteur) et zone
// principale routée.
import { html, useStore, useEffect, useState } from '../core/lib.js';
import { Icon, LoomMark } from '../ui/icons.js';
import { app, go, engineState } from '../core/state.js';
import { NAV_ITEMS } from './routes.js';
import { groupOf } from './sections.js';
import { Activity } from './activity.js';
import { openPalette } from './palette.js';
import { Sessions } from './sessions.js';
import { agents } from '../features/agents/store.js';

// Pastille du moteur local : état ou modèle chargé, et la mémoire vidéo.
function EngineCard() {
  const { status, gpus } = useStore(app, s => ({ status: s.status, gpus: s.gpus }));
  const st = engineState(status), g = (gpus || [])[0];
  const gb = n => (n / 1024).toLocaleString(locale(), { maximumFractionDigits: 1, minimumFractionDigits: 1 });
  return html`<button class="engine-card" onClick=${() => go('engine')} aria-label=${t("app.shell.moteur_local") + st.label}>
    <i class=${'dot ' + st.tone}></i><span>${st.label}</span>
    <em>${g && g.total ? `${gb(g.used)} / ${gb(g.total)}${t('common.units.gb')}` : 'llama.cpp'}</em>
  </button>`;
}

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || '');

export function useShortcuts(phone) {
  useEffect(() => {
    const key = e => {
      const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
      if (mod && !e.shiftKey && k === 'k') { e.preventDefault(); openPalette(); }
      else if (mod && e.shiftKey && k === 'o') { e.preventDefault(); agents.set({ creating: { type: 'chat' } }); }
    };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, [phone]);
}

export function Sidebar() {
  const { route, serverInfo } = useStore(app, s => ({ route: s.route, serverInfo: s.serverInfo }));
  const waiting = useStore(agents, s => s.approvals.filter(a => a.kind !== 'idle').length);
  const dev = /-dev|edge/.test((serverInfo && serverInfo.version) || '');
  const [expanded, setExpanded] = useState(() => {
    try { return localStorage.getItem('loom.workspace.sidebar') !== 'closed'; } catch (_) { return true; }
  });
  const toggle = () => {
    setExpanded(!expanded);
    try { localStorage.setItem('loom.workspace.sidebar', expanded ? 'closed' : 'open'); } catch (_) {}
  };
  const create = () => agents.set({ creating: { type: 'chat' } });
  return html`<aside class=${'side workspace-nav' + (expanded ? '' : ' rail-only')} aria-label=${t('app.shell.navigation')}>
    <div class="workspace-rail">
      <button class="rail-mark" title=${t('workspace.sidebar')} aria-label=${t('workspace.sidebar')} aria-expanded=${expanded}
        aria-controls="workspace-library" onClick=${toggle}><${LoomMark} /></button>
      <nav class="nav">${NAV_ITEMS.map(r => html`<a key=${r.id} href=${r.href} title=${r.nav.label} aria-label=${r.nav.label}
        aria-current=${groupOf(route.section)?.id === r.id ? 'page' : undefined}><${Icon} n=${r.nav.icon} /><span>${r.nav.label}</span>
        ${r.id === 'agents' && waiting > 0 && html`<em class="nav-n" aria-label=${t('agents.cc.waiting')}>${waiting}</em>`}</a>`)}</nav>
      <div class="rail-utilities">
        <button class="icon-btn" title=${t('side.new_session')} aria-label=${t('side.new_session')} onClick=${create}><${Icon} n="plus" /></button>
        <button class="icon-btn" title=${t('app.shell.rechercher_ctrl_k')} aria-label=${t('app.shell.rechercher')} onClick=${openPalette}><${Icon} n="search" /></button>
        <${Activity} />
        <a class="icon-btn" href="#/settings" title=${t('app.shell.reglages')} aria-label=${t('app.shell.reglages')} aria-current=${route.section === 'settings' ? 'page' : undefined}><${Icon} n="gear" /></a>
      </div>
    </div>
    <div id="workspace-library" class="workspace-library" hidden=${!expanded}>
      <div class="brand"><b>${t('app.shell.loom')}</b><button class="icon-btn" title=${t('workspace.sidebar')} aria-label=${t('workspace.sidebar')} onClick=${toggle}><${Icon} n="panel" /></button></div>
      <button class="new-chat" title=${isMac ? '⇧⌘O' : t('side.shortcut_new')} onClick=${create}><${Icon} n="plus" />${t('side.new_session')}</button>
      <${Sessions} />
      <div class="side-foot"><${EngineCard} /><div class="me"><span class="who"><b>${serverInfo?.hostname || t('app.shell.cette_machine')}</b>
        <small title=${serverInfo?.version || ''}>${serverInfo?.version ? 'Loom ' + shortVersion(serverInfo.version) + (dev ? ' · ' + t('side.channel_dev') : '') : 'Loom'}</small></span></div></div>
    </div>
  </aside>`;
}
