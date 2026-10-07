// Panneaux qui vivaient dans les réglages et appartiennent à un domaine :
// le moteur aux modèles, les dossiers et le démarrage aux machines.
import { html } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { GroupPage } from '../../app/sections.js';
import { Engine } from '../settings/page.js';
import { StartupSettings } from '../settings/startup.js';
import { WorkspaceManager } from '../workspaces/folders.js';

export const EnginePage = () => html`<${GroupPage} title=${t('engine.page.title')} lead=${t('engine.page.lead')}><${Engine} /></${GroupPage}>`;
export const StartupPage = () => html`<${GroupPage} title=${t('startup.title')} lead=${t('startup.page.lead')}><${StartupSettings} /></${GroupPage}>`;
export const WorkspacesPage = () => html`<${GroupPage} title=${t('workspaces.title')} lead=${t('workspaces.page.lead')}><${WorkspaceManager} /></${GroupPage}>`;
