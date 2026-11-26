// Registre des sections de l'application. Ajouter une page = une entrée ici :
// `nav` la place dans la barre latérale, `page` est le composant rendu pour
// #/<id>/<sub>/<id>. Rien d'autre à modifier dans la coquille.
import { ChatView } from '../features/chat/view.js';
import { LocalPage } from '../features/local/page.js';
import { CloudPage } from '../features/cloud/page.js';
import { UsagePage } from '../features/usage/page.js';
import { MarketPage } from '../features/market/page.js';
import { SettingsPage } from '../features/settings/page.js';
import { TerminalsPage } from '../features/terminals/page.js';
import { MachinesPage } from '../features/machines/page.js';
import { EnginePage, WorkspacesPage } from '../features/machines/panes.js';
import { VoicePage } from '../features/voice/page.js';
import { JarvisPage } from '../features/voice/jarvis.js';
import { AgentsPage } from '../features/agents/page.js';
import { QuotasPage } from '../features/agents/quotas.js';
import { FilesPage } from '../features/files/page.js';
import { ConnectionsPage } from '../features/connections/page.js';
import { GROUPS } from './sections.js';
import { SessionsPage } from './sessions.js';
import { InboxPage } from './inbox.js';
import { MorePage } from './more.js';

export const ROUTES = [
  { id: 'sessions', page: SessionsPage },
  { id: 'inbox', page: InboxPage },
  { id: 'more', page: MorePage },
  { id: 'chat', page: ChatView },
  { id: 'agents', page: AgentsPage },
  { id: 'local', page: LocalPage },
  { id: 'cloud', page: CloudPage },
  { id: 'voice', page: VoicePage },
  { id: 'jarvis', page: JarvisPage },
  { id: 'terminals', page: TerminalsPage },
  { id: 'usage', page: UsagePage },
  { id: 'quotas', page: QuotasPage },
  { id: 'market', page: MarketPage },
  { id: 'machines', page: MachinesPage },
  { id: 'files', page: FilesPage },
  { id: 'connections', page: ConnectionsPage },
  { id: 'engine', page: EnginePage },
  { id: 'workspaces', page: WorkspacesPage },
  { id: 'settings', page: SettingsPage },
];
export const pageFor = id => (ROUTES.find(r => r.id === id) || {}).page;
// Une entrée de barre latérale par groupe, qui ouvre sa première page.
export const NAV_ITEMS = GROUPS.map(g => ({ id: g.id, href: '#/' + g.tabs[0][0], nav: { get label() { return g.label; }, icon: g.icon } }));
