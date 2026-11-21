// Panneaux qui appartiennent aux groupes Modèles et Machines.
import { html, useState, useStore } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { app, go, refreshEngineNode, refreshLibrary, refreshStatus } from '../../core/state.js';
import { post } from '../../core/api.js';
import { GroupPage, SectionTabs } from '../../app/sections.js';
import { confirm, toast } from '../../ui/dialog.js';
import { Drawer } from '../../ui/drawer.js';
import { ListPick } from '../../ui/listpick.js';
import { Tip } from '../../ui/controls.js';
import { DirectEngineForm, Engine, ModelDirs } from '../settings/page.js';
import { ApiSettings } from '../settings/api.js';
import { useVllm, VllmEngine, VLLMEngine, ownsVllm } from '../settings/vllm.js';
import { engineKind, EngineRuntime } from '../local/page.js';
import { EngineMemory, EngineSlots, EngineRecent } from '../local/service.js';
import { WorkspaceManager } from '../workspaces/folders.js';

function LocationRow({ node }) {
  const [direct, setDirect] = useState(false);
  const [busy, setBusy] = useState(false);
  const label = t('models.engine.location');
  const current = node ? 'linked' : 'local';
  const options = [
    { value: 'local', label: t('api.this_machine') },
    node ? { value: 'linked', label: node.hostname || t('api.remote') }
      : { value: 'direct', label: t('settings.page.lier_un_moteur_par_son_adresse') },
    { value: 'machines', label: t('settings.page.loom_d_une_autre_machine') },
  ];
  const choose = async value => {
    if (value === 'direct') return setDirect(true);
    if (value === 'machines') return go('machines');
    if (value !== 'local' || !node) return;
    if (!await confirm(t('settings.page.revenir_au_moteur_de_cette_machine'),
      t('settings.page.loom_n_utilisera_plus_le_moteur_de') + node.hostname + t('settings.page.rien_n_est_modifie_sur_cette_machine_la'),
      { ok: t('settings.page.revenir') })) return;
    setBusy(true);
    const result = await post('/api/engine/node', { unlink: true });
    if (result.ok) {
      await refreshEngineNode();
      await Promise.allSettled([refreshStatus(), refreshLibrary()]);
    } else toast(result.error || t('local.page.action_impossible'), 'err');
    setBusy(false);
  };
  return html`<div class="models-row">
    <div class="models-row-main">
      <span>${label} <${Tip} text=${t('settings.page.sur_cette_machine_sur_une_machine_ou_loom_est_installe_reglages_m')} /></span>
      ${node && html`<small>${[node.direct && engineKind(node.kind), node.url, node.reachable === false && t('common.unreachable')].filter(Boolean).join(' · ')}</small>`}
      ${node?.direct && node.model && html`<small>${t('local.page.modele_utilise') + node.model}</small>`}
    </div>
    <div class="models-choice"><${ListPick} label=${label} value=${current} options=${options} onChange=${choose} disabled=${busy} /></div>
    ${direct && html`<${Drawer} title=${t('settings.page.lier_un_moteur_par_son_adresse')} onClose=${() => setDirect(false)}>
      <${DirectEngineForm} onDone=${() => setDirect(false)} />
    </${Drawer}>`}
  </div>`;
}

function EnginePanels({ node }) {
  const v = useVllm();
  const ownedVllm = ownsVllm(node, v.x);
  return html`<${EngineRuntime} node=${node} location=${html`<${LocationRow} node=${node} />`} vllm=${ownedVllm ? v : null}
    management=${server => html`
      <${EngineMemory} local=${node?.direct}><${EngineSlots} server=${server} /></${EngineMemory}>
      ${!ownedVllm && server?.engine_kind !== 'vllm' && (v.x?.running || v.x?.job === 'start') && html`<${VllmEngine} v=${v} />`}`}
    access=${server => html`<${ApiSettings} compact server=${server}><${EngineRecent} server=${server} /></${ApiSettings}>`}
    maintenance=${html`<details class="models-extra models-card models-engine-maintenance">
      <summary>${t('models.engine.advanced')}</summary>
      <div class="models-group">
        <div class="models-section-head"><h2>${t('engine.page.installed')}</h2></div>
        <div class="eng-list"><${Engine} /><${VLLMEngine} /></div>
        <${ModelDirs} />
      </div>
    </details>`} />`;
}

export function EnginePage() {
  const node = useStore(app, s => s.engineNode);
  return html`<div class="view page models-page"><div class="page-in models-layout models-engine-layout">
    <${SectionTabs} />
    <h1 class="sr">${t('models.engine.tab')}</h1>
    <${EnginePanels} key=${node?.url || 'local'} node=${node} />
  </div></div>`;
}

export const WorkspacesPage = () => html`<${GroupPage} title=${t('workspaces.title')} lead=${t('workspaces.page.lead')}><${WorkspaceManager} /></${GroupPage}>`;
