import { shortVersion } from '../core/version.js';
import { t, locale } from '../core/i18n.js';
// Coquille : barre latérale avec bascule de mode Discussion / Agents (façon
// Claude Desktop), recherche + nouvelle session côte à côte, navigation filtrée
// par mode, sessions séparées et carte moteur réservée au mode Discussion.
import { html, useStore, useEffect } from '../core/lib.js';
import { Icon, LoomMark } from '../ui/icons.js';
import { Seg } from '../ui/controls.js';
import { app, go, engineState, setWorkspaceMode } from '../core/state.js';
import { newDiscussion } from '../features/chat/engine.js';
import { NAV_ITEMS } from './routes.js';
import { groupOf } from './sections.js';
import { Activity } from './activity.js';
import { openPalette } from './palette.js';
import { Sessions } from './sessions.js';
import { agents } from '../features/agents/store.js';

const CHAT_ONLY = new Set(['chat', 'local', 'cloud', 'voice', 'jarvis', 'engine']);
const AGENT_ONLY = new Set(['agents', 'machines', 'files', 'connections', 'terminals']);

// Pastille du moteur local : état ou modèle chargé, et la mémoire vidéo (mode Discussion uniquement).
function EngineCard() {
  const { status, gpus } = useStore(app, s => ({ status: s.status, gpus: s.gpus }));
  const st = engineState(status), g = (gpus || [])[0];
  const gb = n => (n / 1024).toLocaleString(locale(), { maximumFractionDigits: 1, minimumFractionDigits: 1 });
  return html`<button class="engine-card" onClick=${() => go('engine')} aria-label=${t("app.shell.moteur_local") + st.label}>
    <i class=${'dot ' + st.tone}></i><span>${st.label}</span>
    <em>${g && g.total ? `${gb(g.used)} / ${gb(g.total)}${t('common.units.gb')}` : 'llama.cpp'}</em>
  </button>`;
}

export function useShortcuts(phone) {
  useEffect(() => {
    const key = e => {
      const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
      if (mod && !e.shiftKey && k === 'k') { e.preventDefault(); openPalette(); }
      else if (mod && e.shiftKey && k === 'o') {
        e.preventDefault();
        if (phone) agents.set({ creating: { type: 'chat' } });
        else if (app.get().workspaceMode === 'agents') go('agents', 'new');
        else newDiscussion();
      }
    };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, [phone]);
}

export function Sidebar() {
  const { route, serverInfo, mode } = useStore(app, s => ({ route: s.route, serverInfo: s.serverInfo, mode: s.workspaceMode }));
  const waiting = useStore(agents, s => s.approvals.filter(a => a.kind !== 'idle').length);
  const dev = /-dev|edge/.test((serverInfo && serverInfo.version) || '');
  const navItems = NAV_ITEMS.filter(r => r.mode === 'both' || r.mode === mode);
  const switchMode = next => {
    setWorkspaceMode(next);
    if (next === 'agents') {
      if (CHAT_ONLY.has(route.section)) go('agents', 'new');
    } else if (AGENT_ONLY.has(route.section)) {
      go('chat');
    }
  };
  const startNew = () => {
    if (mode === 'agents') go('agents', 'new');
    else newDiscussion();
  };
  const isNavActive = r => r.id === 'agents'
    ? route.section === 'agents' && !['s', 'new', 'grid'].includes(route.sub)
    : groupOf(route.section)?.id === r.id;
  return html`<aside class="side" aria-label="${t("app.shell.navigation")}">
    <div class="brand"><b><${LoomMark} />${t("app.shell.loom")}</b>
      <span class="brand-acts"><${Activity} /></span></div>
    <div class="side-mode">
      <${Seg} value=${mode} size="sm" onChange=${switchMode} label=${t('side.mode_label')} options=${[
        { value: 'chat', label: html`<${Icon} n="chat" /><span>${t('side.mode_chat')}</span>` },
        { value: 'agents', label: html`<${Icon} n="agent" /><span>${t('side.mode_agents')}</span>` },
      ]} />
    </div>
    <div class="side-search-row">
      <button type="button" class="side-search" aria-label="${t("app.shell.rechercher")}" title="${t("app.shell.rechercher_ctrl_k")}" onClick=${openPalette}>
        <${Icon} n="search" /><span>${t("app.shell.rechercher")}…</span>
      </button>
      <button type="button" class="icon-btn side-new-btn" aria-label="${t("side.new_session")}" title="${t("side.new_session")}" onClick=${startNew}>
        <${Icon} n="compose" />
      </button>
    </div>
    <nav class="nav" key=${mode}>${navItems.map(r => html`<a href=${r.href} aria-current=${isNavActive(r) ? 'page' : undefined}><${Icon} n=${r.nav.icon} /><span>${r.nav.label}</span>${r.id === 'agents' && waiting > 0 && html`<em class="nav-n" title=${t('agents.cc.waiting')}>${waiting}</em>`}</a>`)}</nav>
    <${Sessions} mode=${mode} />
    <div class="side-foot">
      ${mode === 'chat' && html`<${EngineCard} />`}
      <div class="me">
        <span class="who"><b>${(serverInfo && serverInfo.hostname) || t("app.shell.cette_machine")}</b><small title=${serverInfo && serverInfo.version || ''}>${serverInfo && serverInfo.version ? 'Loom ' + shortVersion(serverInfo.version) + (dev ? ' · ' + t('side.channel_dev') : '') : 'Loom'}</small></span>
        <a class="icon-btn" href="#/settings" aria-label="${t("app.shell.reglages")}" title="${t("app.shell.reglages")}" aria-current=${route.section === 'settings' ? 'page' : undefined}><${Icon} n="gear" /></a>
      </div>
    </div>
  </aside>`;
}
