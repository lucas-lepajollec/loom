// Registre des sections de l'application. Ajouter une page = une entrée ici :
// `nav` la place dans la barre latérale, `page` est le composant rendu pour
// #/<id>/<sub>/<id>. Rien d'autre à modifier dans la coquille.
import { ChatView } from '../features/chat/view.js';
import { LocalPage } from '../features/local/page.js';
import { CloudPage } from '../features/cloud/page.js';
import { HarnessesPage } from '../features/harnesses/page.js';
import { ResourcesPage } from '../features/resources/page.js';
import { UsagePage } from '../features/usage/page.js';
import { BenchPage } from '../features/bench/page.js';
import { SettingsPage } from '../features/settings/page.js';
import { ProjectPage } from '../features/projects/page.js';
import { TerminalsPage } from '../features/terminals/page.js';
import { EnvironmentPage } from '../features/environment/page.js';

export const ROUTES = [
  { id: 'chat', page: ChatView },
  { id: 'local', page: LocalPage, nav: { label: 'Local', icon: 'chip' } },
  { id: 'cloud', page: CloudPage, nav: { label: 'Cloud', icon: 'cloud' } },
  { id: 'harnesses', page: HarnessesPage, nav: { label: 'Harnesses', icon: 'terminal' } },
  { id: 'resources', page: ResourcesPage, nav: { label: 'Ressources', icon: 'box' } },
  { id: 'terminals', page: TerminalsPage, nav: { label: 'Terminaux', icon: 'prompt' } },
  { id: 'environment', page: EnvironmentPage, nav: { label: 'Environnement', icon: 'globe' } },
  { id: 'bench', page: BenchPage, nav: { label: 'Bench', icon: 'gauge' } },
  { id: 'usage', page: UsagePage, nav: { label: 'Usage', icon: 'chart' } },
  { id: 'project', page: ProjectPage },
  { id: 'settings', page: SettingsPage },
];
export const pageFor = id => (ROUTES.find(r => r.id === id) || {}).page;
export const NAV_ITEMS = ROUTES.filter(r => r.nav);
