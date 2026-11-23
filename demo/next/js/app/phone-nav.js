import { html } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { phoneBack, phoneTab } from '../core/state.js';
import { Icon } from '../ui/icons.js';

export function BackButton() {
  return html`<button type="button" class="icon-btn phone-back" aria-label=${t('mobile.back')} onClick=${phoneBack}><${Icon} n="left" /></button>`;
}

export function PhoneBackLink({ parent }) {
  const label = parent === 'machines' ? t('settings.machines.machines')
    : parent === 'settings' ? t('settings.page.reglages') : t('mobile.more');
  return html`<a class="phone-back-link" href=${'#/' + parent}><${Icon} n="left" />${label}</a>`;
}

export function PhoneTabs({ route, count }) {
  const active = phoneTab(route);
  if (!active) return null;
  const tabs = [
    ['sessions', 'sessions', 'chat', t('mobile.sessions')],
    ['inbox', 'inbox', 'inbox', t('mobile.inbox')],
    ['usage', 'quotas', 'chart', t('mobile.usage')],
    ['more', 'more', 'more', t('mobile.more')],
  ];
  return html`<nav class="phone-tabs" aria-label=${t('app.shell.navigation')}>
    ${tabs.map(([id, href, icon, label]) => html`<a key=${id} href=${'#/' + href} aria-current=${id === active ? 'page' : undefined}>
      <${Icon} n=${icon} /><span>${label}</span>
      ${id === 'inbox' && count > 0 && html`<span class="phone-badge" aria-label=${t('mobile.attention_count', { n: count })}>${count}</span>`}
    </a>`)}
  </nav>`;
}
