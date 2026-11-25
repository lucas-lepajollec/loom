import { html } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { useMedia } from '../core/viewport.js';
import { Drawer } from './drawer.js';
import { Icon } from './icons.js';

export function SessionPanel({ title, onClose, children }) {
  const overlay = useMedia('(max-width: 1179px)');
  if (overlay && onClose) return html`<${Drawer} title=${title} onClose=${onClose} class="session-drawer">${children}</${Drawer}>`;
  return html`<aside class="session-panel" aria-label=${title}>${children}
    ${onClose && html`<button class="icon-btn session-panel-close" aria-label=${t('ui.drawer.fermer')} onClick=${onClose}><${Icon} n="close" /></button>`}
  </aside>`;
}

export function PanelTabs({ label, value, tabs, onChange }) {
  return html`<nav class="panel-tabs" aria-label=${label}>${tabs.map(([id, text]) => html`
    <button key=${id} type="button" aria-pressed=${String(value === id)} onClick=${() => onChange(id)}>${text}</button>`)}
  </nav>`;
}
