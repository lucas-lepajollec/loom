import { t } from '../../core/i18n.js';
import { SectionTabs } from '../../app/sections.js';
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Drawer } from '../../ui/drawer.js';
import { SelectionInfo } from '../inspector/selection.js';
import { Icon } from '../../ui/icons.js';
import { get, onChanged as onStateChanged } from '../../core/api.js';
import { app, go } from '../../core/state.js';
import { HarnessHistory } from './history.js';
import { isACP } from './capabilities.js';
import { Card, installationsOf } from './catalog.js';
import { AddAgentDialog, CustomDialog } from './dialogs.js';
import { AgentDetail, Detail } from './agent-page.js';

export function HarnessesPage({ route }) {
  const ws = useStore(app, s => s.workspace);
  const all = ((ws && ws.runtimes) || []).filter(r => r.kind === 'harness');
  // La carte d'attente « Hermes » disparaît dès qu'un Hermes est branché.
  const runtimes = all.filter(r => !(r.id === 'hermes' && !(r.capabilities || []).length && all.some(x => x.id !== r.id && /(^|-)hermes$/.test(x.id))));
  const models = (ws && ws.models) || [];
  const [selected, setSelected] = useState(null);
  const [dlg, setDlg] = useState(null);
  const [installs, setInstalls] = useState([]);
  const loadInstalls = () => get('/api/agents/installations').then(r => r.ok && setInstalls(r.installations || [])).catch(() => {});
  useEffect(() => onStateChanged(loadInstalls), []);
  useEffect(() => { loadInstalls(); }, []);
  useEffect(() => { setSelected(s => s && s.runtime !== route.sub ? null : s); }, [route.sub]);
  // Une carte par famille gérée quelque part ; un agent personnalisé a la sienne.
  const families = runtimes.filter(r => !r.machine && (r.custom || installationsOf(r, installs).length > 0));
  const used = r => r.custom ? !!r.connected : installationsOf(r, installs).some(i => i.enabled);
  const selectedRuntime = runtimes.find(r => r.id === selected?.runtime);
  const selectedModel = models.find(m => m.id === selected?.model);
  const cur = runtimes.find(r => r.id === route.sub);
  const order = r => (r.implemented && r.capabilities && r.capabilities.length ? 0 : 1);
  if (route.sub === 'history') return html`<${HarnessHistory} />`;
  return html`<div class="view page"><div class="page-in wide">
    ${cur ? html`<button class="btn ghost sm back" onClick=${() => go('harnesses')}><${Icon} n="left" />${t('app.groups.agents')}</button>
      <div style="margin-top:14px">${isACP(cur) ? html`<${AgentDetail} key=${cur.id} rt=${cur} models=${models} onEdit=${a => setDlg({ agent: a })} />`
        : html`<${Detail} key=${cur.id} rt=${cur} models=${models} onInspect=${m => setSelected({ runtime: cur.id, model: m.id })} />`}</div>`
    : html`<${SectionTabs} /><div class="page-head"><div><h1>${t('app.groups.agents')}</h1><p>${t("harnesses.page.des_agents_qui_gardent_leurs_outils_leur_compte_et_leurs_permissi")}</p></div>
        <div class="acts"><a class="btn" href="#/machines"><${Icon} n="server" />${t("harnesses.page.machines")}</a><button class="btn primary" onClick=${() => setDlg({ add: true })}><${Icon} n="plus" />${t('agents.add.title')}</button></div></div>
      ${!ws ? html`<div class="skeleton" style="height:220px"></div>` : html`${[true, false].filter(connected => families.some(r => used(r) === connected)).map(connected => html`<section class="sec"><div class="sec-h"><h2>${connected ? t('agents.section.used') : t('agents.section.managed')}</h2></div><div class="hx-grid stagger">${families.filter(r => used(r) === connected).sort((a, b) => order(a) - order(b)).map(r => html`<${Card} key=${r.id} rt=${r} models=${models} installs=${installationsOf(r, installs)} />`)}</div></section>`)}`}
`}
    ${dlg && dlg.add && html`<${AddAgentDialog} installs=${installs} onClose=${() => setDlg(null)} onCustom=${() => setDlg({})} onChanged=${list => list ? setInstalls(list) : loadInstalls()} />`}
    ${dlg && !dlg.add && !dlg.machine && html`<${CustomDialog} agent=${dlg.agent} onClose=${a => { setDlg(null); if (a && !dlg.agent) go('harnesses', a.id); }} />`}
    ${selectedRuntime && html`<${Drawer} title=${selectedModel?.name || selectedRuntime.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selectedModel} runtime=${selectedRuntime} models=${models} /></${Drawer}>`}
  </div></div>`;
}
