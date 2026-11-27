import { html, useStore, useEffect, cls, baseName, createStore } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { app, engineState, phoneBack, phoneTab } from '../core/state.js';
import { Icon, LoomMark } from '../ui/icons.js';
import { Portal } from '../ui/dialog.js';
import { engineKind } from '../features/local/page.js';

export const phoneDrawer = createStore({ open: false });
export const openPhoneDrawer = () => phoneDrawer.set({ open: true });
export const closePhoneDrawer = () => phoneDrawer.set({ open: false });

export function BackButton() {
  return html`<button type="button" class="icon-btn phone-back" aria-label=${t('mobile.back')} onClick=${phoneBack}><${Icon} n="left" /></button>`;
}

export function BurgerButton() {
  return html`<button type="button" class="icon-btn phone-burger" aria-label=${t('app.shell.navigation')} aria-haspopup="dialog" onClick=${openPhoneDrawer}><${Icon} n="menu" /></button>`;
}

export function PhoneBackLink({ parent }) {
  const label = parent === 'machines' ? t('settings.machines.machines')
    : parent === 'settings' ? t('settings.page.reglages') : t('mobile.back');
  return html`<a class="phone-back-link" href=${'#/' + parent}><${Icon} n="left" />${label}</a>`;
}

export function PhoneTabs({ route, count }) {
  const active = phoneTab(route);
  if (!active) return null;
  const tabs = [
    ['sessions', 'sessions', 'chat', t('mobile.sessions')],
    ['inbox', 'inbox', 'inbox', t('mobile.inbox')],
    ['files', 'files', 'folder', t('files.title')],
    ['machines', 'machines', 'server', t('settings.machines.machines')],
    ['usage', 'quotas', 'chart', t('mobile.usage')],
  ];
  return html`<nav class="phone-tabs" aria-label=${t('app.shell.navigation')}>
    ${tabs.map(([id, href, icon, label]) => html`<a key=${id} href=${'#/' + href} aria-current=${id === active ? 'page' : undefined}>
      <${Icon} n=${icon} /><span>${label}</span>
      ${id === 'inbox' && count > 0 && html`<span class="phone-badge" aria-label=${t('mobile.attention_count', { n: count })}>${count}</span>`}
    </a>`)}
  </nav>`;
}

export function PhoneDrawerMenu({ route, count }) {
  const open = useStore(phoneDrawer, s => s.open);
  const { status, unavailable, engineNode, serverInfo } = useStore(app, s => ({
    status: s.status, unavailable: s.unavailable.status, engineNode: s.engineNode, serverInfo: s.serverInfo,
  }));
  useEffect(() => { if (open) closePhoneDrawer(); }, [route.section, route.sub, route.id]);
  useEffect(() => {
    if (!open) return;
    const root = document.getElementById('app');
    if (root && 'inert' in root) root.inert = true;
    const onKey = e => { if (e.key === 'Escape') { e.preventDefault(); closePhoneDrawer(); } };
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      if (root && 'inert' in root && !document.querySelector('.dialog[aria-modal="true"]')) root.inert = false;
    };
  }, [open]);
  if (!open) return null;

  const st = engineState(status);
  const running = !!status?.active;
  const tone = unavailable ? 'amber' : st.tone;
  const engLabel = unavailable ? t('models.engine.unavailable') : running && status?.health ? t('local.page.pret')
    : running && !status?.load_error ? t('models.engine.starting') : st.label;
  const engSub = [engineKind(engineNode?.kind || status?.engine_kind), engineNode?.hostname || serverInfo?.hostname, status?.health && !unavailable && (status?.model_name || baseName(status?.model))].filter(Boolean).join(' · ');

  const sec = route.section;
  const primary = [
    ['sessions', 'chat', t('mobile.sessions'), sec === 'sessions', 0],
    ['inbox', 'inbox', t('mobile.inbox'), sec === 'inbox', count],
    ['files', 'folder', t('files.title'), sec === 'files', 0],
    ['machines', 'server', t('settings.machines.machines'), sec === 'machines' || sec === 'terminals', 0],
    ['quotas', 'chart', t('mobile.usage'), ['quotas', 'usage', 'market'].includes(sec), 0],
  ];
  const secondary = [
    ['local', 'chip', t('chat.picker.local'), ['local', 'voice', 'jarvis', 'engine'].includes(sec)],
    ['cloud', 'cloud', t('app.routes.cloud'), sec === 'cloud'],
    ['agents', 'agent', t('agents.title'), sec === 'agents'],
    ['connections', 'plug', t('agents.links.title'), sec === 'connections'],
    ['settings', 'gear', t('app.palette.reglages'), sec === 'settings'],
    ['more/jarvis', 'waveform', t('mobile.talk_jarvis'), sec === 'more' && route.sub === 'jarvis'],
  ];

  return html`<${Portal}><div class="phone-drawer-scrim" onClick=${e => { if (e.target === e.currentTarget) closePhoneDrawer(); }}>
    <aside class="phone-drawer" role="dialog" aria-modal="true" aria-label=${t('app.shell.navigation')}>
      <header class="phone-drawer-head">
        <span class="phone-drawer-brand"><${LoomMark} size=${22} /><b>Loom</b></span>
        <button type="button" class="icon-btn" aria-label=${t('ui.dialog.fermer')} onClick=${closePhoneDrawer}><${Icon} n="close" /></button>
      </header>
      <div class="phone-drawer-body">
        <a class="phone-drawer-engine" href="#/engine" onClick=${closePhoneDrawer}>
          <i class=${cls('dot', tone)}></i>
          <span><b>${engLabel}</b>${engSub && html`<small class="trunc">${engSub}</small>`}</span>
          <${Icon} n="right" />
        </a>
        <nav class="phone-drawer-nav" aria-label=${t('app.shell.navigation')}>
          <div class="phone-drawer-group">
            ${primary.map(([href, icon, label, active, badge]) => html`<a key=${href} class="phone-drawer-item" href=${'#/' + href} aria-current=${active ? 'page' : undefined} onClick=${closePhoneDrawer}>
              <${Icon} n=${icon} /><span>${label}</span>
              ${badge > 0 && html`<em class="phone-drawer-badge">${badge}</em>`}
            </a>`)}
          </div>
          <hr />
          <div class="phone-drawer-group">
            ${secondary.map(([href, icon, label, active]) => html`<a key=${href} class="phone-drawer-item" href=${'#/' + href} aria-current=${active ? 'page' : undefined} onClick=${closePhoneDrawer}>
              <${Icon} n=${icon} /><span>${label}</span>
            </a>`)}
          </div>
        </nav>
      </div>
    </aside>
  </div></${Portal}>`;
}
