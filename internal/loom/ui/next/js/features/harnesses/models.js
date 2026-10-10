import { t } from '../../core/i18n.js';
import { html, useState, cls } from '../../core/lib.js';

export const optValues = o => { const out = []; const walk = l => (l || []).forEach(x => x.options ? walk(x.options) : out.push(x)); walk(o && o.options); return out; };
export const byCat = (cfg, cat) => (cfg || []).find(o => o.category === cat) || (cfg || []).find(o => o.id === cat);

const MODEL_LIMIT = 8;

export function AgentModels({ groups, modelOpt, used, startWith, choiceFor, list }) {
  const [allModels, setAllModels] = useState(false);
  return groups.length > 0 && html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.modeles")} <span class="count">${groups.length}</span></h2></div>
      <div class="model-chips">${(allModels ? groups : groups.slice(0, MODEL_LIMIT)).map(g => { const def = g.levels.find(l => l.value === modelOpt.currentValue) || g.levels.find(l => l.level === 'medium') || g.levels[0]; const isDef = g.levels.some(l => l.value === modelOpt.currentValue);
        return html`<button type="button" class=${cls('mchip', isDef && 'on')} disabled=${!used} title=${used ? t("harnesses.page.discuter") : ''} onClick=${() => startWith(choiceFor(def.value))}><b>${g.name}</b>${g.levels.length > 1 ? html`<small>${g.levels.map(l => l.label || l.level).join(' · ')}</small>` : (list.find(x => x.value === def.value) || {}).description ? html`<small>${(list.find(x => x.value === def.value) || {}).description}</small>` : ''}${isDef && html`<em>${t("harnesses.page.par_defaut_2")}</em>`}</button>`; })}</div>${groups.length > MODEL_LIMIT && html`<button class="btn sm ghost more-models" onClick=${() => setAllModels(!allModels)}>${allModels ? t('agents.models.less') : t('agents.models.all', { n: groups.length })}</button>`}</section>`;
}
