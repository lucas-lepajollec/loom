// Panneaux qui vivaient dans les réglages et appartiennent à un domaine :
// le moteur aux modèles, les dossiers et le démarrage aux machines.
import { html } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { GroupPage } from '../../app/sections.js';
import { Engine } from '../settings/page.js';
import { EngineRuntime } from '../local/page.js';
import { useVllm, VllmEngine } from '../settings/vllm.js';
import { WorkspaceManager } from '../workspaces/folders.js';

// Une seule page moteur : d'abord ce qui tourne (état, API, slots), puis où
// et avec quoi il tourne (emplacement, llama.cpp, vLLM, installation).
export function EnginePage() {
  const v = useVllm();
  return html`<${GroupPage} title=${t('engine.page.title')} lead=${t('engine.page.lead')}>
    <${EngineRuntime} />
    ${v.x && v.x.installed && html`<section class="set-group"><h3>vLLM</h3><${VllmEngine} /></section>`}
    <${Engine} />
  </${GroupPage}>`;
}
export const WorkspacesPage = () => html`<${GroupPage} title=${t('workspaces.title')} lead=${t('workspaces.page.lead')}><${WorkspaceManager} /></${GroupPage}>`;
