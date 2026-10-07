// Groupes de la barre latérale : une entrée par domaine (ce qu'on utilise, qui
// travaille, où ça tourne, ce que Loom sait, ce qui s'est passé). Les pages
// d'un même groupe partagent une rangée d'onglets.
import { html, useStore } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { app } from '../core/state.js';

export const GROUPS = [
  { id: 'models', icon: 'chip', get label() { return t('app.groups.models'); }, tabs: [['local', () => t('local.page.local')], ['cloud', () => t('app.routes.cloud')]] },
  { id: 'agents', icon: 'terminal', get label() { return t('app.groups.agents'); }, tabs: [['harnesses', () => t('app.routes.harnesses')]] },
  { id: 'machines', icon: 'server', get label() { return t('settings.page.machines'); }, tabs: [['machines', () => t('settings.page.machines')], ['terminals', () => t('app.routes.terminaux')], ['environment', () => t('app.routes.environnement')]] },
  { id: 'brain', icon: 'brain', get label() { return t('resources.page.brain'); }, tabs: [['brain', () => t('resources.page.brain')], ['resources']] },
  { id: 'activity', icon: 'chart', get label() { return t('app.groups.activity'); }, tabs: [['usage', () => t('app.routes.usage')], ['bench', () => t('app.routes.bench')]] },
];

export const groupOf = section => GROUPS.find(g => g.tabs.some(([id]) => id === section));

export function SectionTabs() {
  const section = useStore(app, s => s.route.section);
  const group = groupOf(section);
  const tabs = group ? group.tabs.filter(([, label]) => label) : [];
  if (tabs.length < 2) return null;
  return html`<nav class="sec-tabs" aria-label=${group.label}>${tabs.map(([id, label]) => html`<a href=${'#/' + id} aria-current=${id === section ? 'page' : undefined}>${label()}</a>`)}</nav>`;
}
