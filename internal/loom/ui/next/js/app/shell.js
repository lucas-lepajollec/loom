import { shortVersion } from '../core/version.js';
import { t, locale } from '../core/i18n.js';
// Coquille : barre latérale (navigation, sessions, carte moteur) et zone
// principale routée.
import { html, useStore, useEffect } from '../core/lib.js';
import { Icon, LoomMark } from '../ui/icons.js';
import { app, go, engineState } from '../core/state.js';
import { newDiscussion } from '../features/chat/engine.js';
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

export function Sidebar() {
  const { route, serverInfo } = useStore(app, s => ({ route: s.route, serverInfo: s.serverInfo }));
  const waiting = useStore(agents, s => s.approvals.filter(a => a.kind !== 'idle').length);
  const dev = /-dev|edge/.test((serverInfo && serverInfo.version) || '');
  useEffect(() => {
    const key = e => {
      const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
      if (mod && !e.shiftKey && k === 'k') { e.preventDefault(); openPalette(); }
      else if (mod && e.shiftKey && k === 'o') { e.preventDefault(); newDiscussion(); }
    };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
  return html`<aside class="side" aria-label="${t("app.shell.navigation")}">
    <div class="brand"><b><${LoomMark} />${t("app.shell.loom")}</b>
      <span class="brand-acts"><button class="icon-btn" aria-label="${t("app.shell.rechercher")}" title="${t("app.shell.rechercher_ctrl_k")}" onClick=${openPalette}><${Icon} n="search" /></button><${Activity} /></span>
      <button class="icon-btn only-mobile" aria-label="${t("app.shell.fermer")}" onClick=${() => app.set({ sideOpen: false })}><${Icon} n="close" /></button></div>
    <button class="new-chat" title=${isMac ? '⇧⌘O' : t('side.shortcut_new')} onClick=${() => newDiscussion()}><${Icon} n="plus" />${t("side.new_session")}</button>
    <nav class="nav">${NAV_ITEMS.map(r => html`<a href=${r.href} aria-current=${groupOf(route.section)?.id === r.id ? 'page' : undefined}><${Icon} n=${r.nav.icon} /><span>${r.nav.label}</span>${r.id === 'agents' && waiting > 0 && html`<em class="nav-n" title=${t('agents.cc.waiting')}>${waiting}</em>`}</a>`)}</nav>
    <${Sessions} />
    <div class="side-foot">
      <${EngineCard} />
      <div class="me">
        <span class="who"><b>${(serverInfo && serverInfo.hostname) || t("app.shell.cette_machine")}</b><small title=${serverInfo && serverInfo.version || ''}>${serverInfo && serverInfo.version ? 'Loom ' + shortVersion(serverInfo.version) + (dev ? ' · ' + t('side.channel_dev') : '') : 'Loom'}</small></span>
        <a class="icon-btn" href="#/settings" aria-label="${t("app.shell.reglages")}" title="${t("app.shell.reglages")}" aria-current=${route.section === 'settings' ? 'page' : undefined}><${Icon} n="gear" /></a>
      </div>
    </div>
  </aside>`;
}
