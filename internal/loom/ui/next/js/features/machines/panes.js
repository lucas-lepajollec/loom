// Panneaux qui vivaient dans les réglages et appartiennent à un domaine :
// le moteur aux modèles, les dossiers et le démarrage aux machines.
import { html } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { GroupPage } from '../../app/sections.js';
import { Engine, EngineLocation, ModelDirs } from '../settings/page.js';
import { VLLMEngine } from '../settings/vllm.js';
import { EngineRuntime } from '../local/page.js';
import { EngineMemory, EngineKeys } from '../local/service.js';
import { useVllm, VllmEngine } from '../settings/vllm.js';
import { WorkspaceManager } from '../workspaces/folders.js';

// Une seule page moteur : d'abord ce qui tourne (état, API, slots), puis où
// et avec quoi il tourne (emplacement, llama.cpp, vLLM, installation).
export function EnginePage() {
  const v = useVllm();
  // 1. où tourne le moteur ; 2. moteurs installés ; 3. état en direct, gestion
  // (mémoire, slots, dossiers) et accès réseau (exposition, clés).
  return html`<${GroupPage} title=${t('engine.page.title')} lead=${t('engine.page.lead')}>
    <${EngineLocation} />
    <section class="set-group"><h3>${t('engine.page.installed')}</h3><div class="eng-list"><${Engine} /><${VLLMEngine} /></div></section>
    ${v.x && v.x.installed && (v.x.running || v.x.job) && html`<section class="set-group"><h3>vLLM</h3><${VllmEngine} /></section>`}
    <${EngineRuntime} management=${html`<${EngineMemory} /><${ModelDirs} />`} access=${html`<${EngineKeys} />`} />
  </${GroupPage}>`;
}
export const WorkspacesPage = () => html`<${GroupPage} title=${t('workspaces.title')} lead=${t('workspaces.page.lead')}><${WorkspaceManager} /></${GroupPage}>`;
