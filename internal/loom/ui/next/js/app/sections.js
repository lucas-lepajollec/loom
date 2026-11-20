// Pages de la barre latérale : les modèles, la gestion des agents, les
// machines et l'activité. Les pages d'un même groupe partagent une rangée
// d'onglets ; les sessions elles-mêmes sont dans la liste en dessous.
import { html, useStore } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { app } from '../core/state.js';
import { TabNav } from '../ui/tabnav.js';

export const GROUPS = [
  { id: 'models', icon: 'chip', get label() { return t('app.groups.models'); }, tabs: [['local', () => t('local.page.local')], ['cloud', () => t('app.routes.cloud')], ['voice', () => t('voice.title')], ['jarvis', () => t('jv.title')], ['engine', () => t('engine.page.title')]] },
  { id: 'agents', icon: 'agent', get label() { return t('agents.title'); }, tabs: [['agents', () => t('agents.title')]] },
  { id: 'machines', icon: 'server', get label() { return t('settings.page.machines'); }, tabs: [['machines', () => t('settings.page.machines')]] },
  { id: 'activity', icon: 'chart', get label() { return t('app.groups.activity'); }, tabs: [['quotas', () => t('quotas.title')], ['usage', () => t('app.routes.usage')], ['bench', () => t('app.routes.bench')]] },
];

export const groupOf = section => GROUPS.find(g => g.tabs.some(([id]) => id === section));

export function SectionTabs() {
  const section = useStore(app, s => s.route.section);
  const group = groupOf(section);
  const tabs = group ? group.tabs.filter(([, label]) => label) : [];
  if (tabs.length < 2) return null;
  return html`<${TabNav} id=${group.id} label=${group.label} tabs=${tabs.map(([id, label]) => ({ href: '#/' + id, label: label(), current: id === section }))} />`;
}

// Une page de groupe qui accueille un ancien panneau des réglages.
export function GroupPage({ title, lead, children }) {
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} />
    <div class="page-head"><div><h1>${title}</h1>${lead && html`<p>${lead}</p>`}</div></div>
    <div class="set-body">${children}</div>
  </div></div>`;
}
