import { shortVersion } from '../core/version.js';
import { t } from '../core/i18n.js';
// Coquille : barre latérale (navigation, sessions, carte moteur) et zone
// principale routée.
import { html, useStore, useEffect } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { app, go, engineState } from '../core/state.js';
import { newDiscussion } from '../features/chat/engine.js';
import { NAV_ITEMS } from './routes.js';
import { groupOf } from './sections.js';
import { Activity } from './activity.js';
import { openPalette } from './palette.js';
import { Sessions } from './sessions.js';

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
  const { route, serverInfo } = useStore(app, s => ({ route: s.route, serverInfo: s.serverInfo }));
  useEffect(() => {
    const key = e => {
      const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
      if (mod && !e.shiftKey && k === 'k') { e.preventDefault(); openPalette(); }
      else if (mod && e.shiftKey && k === 'o') { e.preventDefault(); newDiscussion(); }
    };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
  return html`<aside class="side" aria-label="${t("app.shell.navigation")}">
    <div class="brand"><b><svg viewBox="0 0 256 256" aria-hidden="true"><path fill="currentColor" d="M40 64H52H52V104H40A20 20 0 0 1 20 84V84A20 20 0 0 1 40 64ZM116 64H216A20 20 0 0 1 236 84V84A20 20 0 0 1 216 104H116H116V64ZM40 152H140H140V192H40A20 20 0 0 1 20 172V172A20 20 0 0 1 40 152ZM204 152H216A20 20 0 0 1 236 172V172A20 20 0 0 1 216 192H204H204V152ZM64 40A20 20 0 0 1 84 20H84A20 20 0 0 1 104 40V140V140H64ZM64 204V204H104V216A20 20 0 0 1 84 236H84A20 20 0 0 1 64 216ZM152 40A20 20 0 0 1 172 20H172A20 20 0 0 1 192 40V52V52H152ZM152 116V116H192V216A20 20 0 0 1 172 236H172A20 20 0 0 1 152 216Z"></path></svg>${t("app.shell.loom")}</b>
      <span class="brand-acts"><button class="icon-btn" aria-label="${t("app.shell.rechercher")}" title="${t("app.shell.rechercher_ctrl_k")}" onClick=${openPalette}><${Icon} n="search" /></button><${Activity} /></span>
      <button class="icon-btn only-mobile" aria-label="${t("app.shell.fermer")}" onClick=${() => app.set({ sideOpen: false })}><${Icon} n="close" /></button></div>
    <button class="new-chat" onClick=${() => newDiscussion()}><${Icon} n="plus" />${t("side.new_session")}</button>
    <nav class="nav">${NAV_ITEMS.map(r => html`<a href=${r.href} aria-current=${groupOf(route.section)?.id === r.id ? 'page' : undefined}><${Icon} n=${r.nav.icon} />${r.nav.label}</a>`)}</nav>
    <${Sessions} />
    <div class="side-foot">
      <${EngineCard} />
      <div class="me">
        <span class="who"><b>${(serverInfo && serverInfo.hostname) || t("app.shell.cette_machine")}</b><small title=${serverInfo && serverInfo.version || ''}>${serverInfo && serverInfo.version ? 'Loom ' + shortVersion(serverInfo.version) : 'Loom'}</small></span>
        <a class="icon-btn" href="#/settings" aria-label="${t("app.shell.reglages")}" title="${t("app.shell.reglages")}" aria-current=${route.section === 'settings' ? 'page' : undefined}><${Icon} n="gear" /></a>
      </div>
    </div>
  </aside>`;
}
