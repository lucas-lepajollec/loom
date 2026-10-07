// Machines : une page à part entière (aperçu de chaque machine, ses agents,
// son moteur, ses services), au lieu d'une section des réglages.
import { html } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { SectionTabs } from '../../app/sections.js';
import { MachinesSettings } from '../settings/machines.js';

export function MachinesPage({ route }) {
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} />
    ${!route.sub && html`<div class="page-head"><div><h1>${t('settings.page.machines')}</h1><p>${t("settings.machines.loom_travaille_sur_cette_machine_et_sur_les_machines_que_tu_conne")}</p></div></div>`}
    <div class="set-body machines-body"><${MachinesSettings} route=${{ section: 'settings', sub: 'machines', id: route.sub }} /></div>
  </div></div>`;
}
