import { html, useStore } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { app } from '../core/state.js';
import { usePhone } from '../core/viewport.js';
import { Icon } from '../ui/icons.js';
import { lt } from '../features/agents/labels.js';
import { Activity } from './activity.js';
import { TabNav } from '../ui/tabnav.js';

export const GROUPS = [
  { id: 'models', icon: 'chip', get label() { return t('app.groups.models'); }, tabs: [
    ['local', () => t('local.page.bibliotheque')],
    ['local/hub', () => t('local.page.hub')],
    ['cloud', () => t('app.routes.cloud')],
    ['voice', () => t('voice.title')],
    ['jarvis'],
    ['engine', () => t('models.engine.tab')],
  ] },
  { id: 'agents', icon: 'agent', get label() { return t('agents.title'); }, tabs: [['agents', () => lt('nav', '')], ['agents/cron', () => lt('nav', 'cron')], ['agents/settings', () => lt('nav', 'settings')]] },
  { id: 'machines', icon: 'server', get label() { return t('settings.page.machines'); }, tabs: [['machines', () => t('settings.page.machines')]] },
  { id: 'files', icon: 'folder', get label() { return t('files.title'); }, tabs: [['files', () => t('files.title')]] },
  { id: 'activity', icon: 'chart', get label() { return t('app.groups.activity'); }, tabs: [['quotas', () => t('quotas.title')], ['usage', () => t('app.routes.usage')], ['market', () => t('market.title')]] },
];

export const groupOf = section => GROUPS.find(g => g.tabs.some(([id]) => id.split('/')[0] === section));

export function ContextNav({ title, items, children }) {
  return html`<aside class="loom-context">
    <nav aria-label=${title}>
      <h2>${title}</h2>
      ${items.map(({ href, label, icon, current }) => html`<a key=${href} href=${href} aria-current=${current ? 'page' : undefined}>
        ${icon && html`<${Icon} n=${icon} />`}<span>${label}</span></a>`)}
    </nav>
    ${children}
  </aside>`;
}

export function ContextLinks({ route }) {
  const group = groupOf(route.section);
  if (!group || group.tabs.length < 2) return null;
  const exact = route.section + (route.sub ? '/' + route.sub : '');
  const here = route.section === 'jarvis' ? 'voice' : group.tabs.some(([id]) => id === exact) ? exact : route.section;
  return html`<${ContextNav} title=${group.label} items=${group.tabs.filter(([, label]) => label).map(([id, label]) => ({
    href: '#/' + id, label: label(), icon: group.icon, current: id === here,
  }))}>${group.id === 'activity' && html`<div class="context-activity"><${Activity} /></div>`}</${ContextNav}>`;
}

export function SectionTabs() {
  const { section, sub } = useStore(app, s => ({ section: s.route.section, sub: s.route.sub }));
  const phone = usePhone();
  const group = groupOf(section);
  const tabs = group ? group.tabs.filter(([, label]) => label) : [];
  if (tabs.length < 2) return null;
  // Un onglet peut viser une sous-page (« local/hub ») : il l'emporte alors.
  const exact = section + (sub ? '/' + sub : '');
  const phoneChips = phone && group.id === 'models';
  const here = tabs.some(([id]) => id === exact) ? exact : phoneChips && section === 'jarvis' ? 'voice' : section;
  return html`<${TabNav} id=${group.id} label=${group.label} phoneChips=${phoneChips}
    tabs=${tabs.map(([id, label]) => ({ href: '#/' + id, label: phoneChips && id === 'engine' ? t('local.page.moteur') : label(), current: id === here }))} />`;
}

// Une page de groupe qui accueille un ancien panneau des réglages.
export function GroupPage({ title, lead, children }) {
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} />
    <div class="page-head"><div><h1>${title}</h1>${lead && html`<p>${lead}</p>`}</div></div>
    <div class="set-body">${children}</div>
  </div></div>`;
}
