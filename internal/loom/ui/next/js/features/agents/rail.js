// Petits éléments partagés par les vues de sessions : durée écoulée et
// pastille d'état.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, cls } from '../../core/lib.js';
import { TONE } from './store.js';

export function ago(ms) {
  if (!ms) return '';
  const s = Math.max(0, (Date.now() - ms) / 1000);
  // Durée compacte (« 4 min », « 3 h », « 8 j ») : le contexte dit déjà « il y a ».
  const unit = (n, u) => new Intl.NumberFormat(locale(), { style: 'unit', unit: u, unitDisplay: 'short' }).format(n);
  if (s < 50) return t('agents.now');
  if (s < 3600) return unit(Math.round(s / 60), 'minute');
  if (s < 86400) return unit(Math.round(s / 3600), 'hour');
  return unit(Math.round(s / 86400), 'day');
}

export const StateDot = ({ state }) => html`<i class=${cls('dot', TONE[state])} title=${lt('state', state)}></i>`;
