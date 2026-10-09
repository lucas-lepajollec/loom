import { t } from '../core/i18n.js';
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
import { MachinesPage } from '../features/machines/page.js';
import { EnginePage, WorkspacesPage } from '../features/machines/panes.js';
import { TasksPage } from '../features/tasks/page.js';
import { GROUPS } from './sections.js';

export const ROUTES = [
  { id: 'chat', page: ChatView },
  { id: 'local', page: LocalPage },
  { id: 'cloud', page: CloudPage },
  { id: 'harnesses', page: HarnessesPage },
  { id: 'tasks', page: TasksPage },
  { id: 'brain', page: ResourcesPage },
  { id: 'resources', page: ResourcesPage },
  { id: 'terminals', page: TerminalsPage },
  { id: 'environment', page: EnvironmentPage },
  { id: 'bench', page: BenchPage },
  { id: 'usage', page: UsagePage },
  { id: 'machines', page: MachinesPage },
  { id: 'engine', page: EnginePage },
  { id: 'workspaces', page: WorkspacesPage },
  { id: 'project', page: ProjectPage },
  { id: 'settings', page: SettingsPage },
];
export const pageFor = id => (ROUTES.find(r => r.id === id) || {}).page;
// Une entrée de barre latérale par groupe, qui ouvre sa première page.
export const NAV_ITEMS = GROUPS.map(g => ({ id: g.id, href: '#/' + g.tabs[0][0], nav: { get label() { return g.label; }, icon: g.icon } }));
