// Registre des sections de l'application. Ajouter une page = une entrée ici :
// `nav` la place dans la barre latérale, `page` est le composant rendu pour
// #/<id>/<sub>/<id>. Les pages principales (discussions, agents, onglets
// téléphone) sont chargées d'emblée ; les pages secondaires se chargent à la
// première visite pour alléger le démarrage.
import { html, useState, useEffect } from '../core/lib.js';
import { ChatView } from '../features/chat/view.js';
import { AgentsPage } from '../features/agents/page.js';
import { GROUPS } from './sections.js';
import { SessionsPage } from './sessions.js';
import { InboxPage } from './inbox.js';
import { MorePage } from './more.js';

function lazyPage(load) {
  let resolved = null;
  let pending = null;
  return function LazyPage(props) {
    const [loaded, setLoaded] = useState(() => resolved);
    const [err, setErr] = useState(null);
    useEffect(() => {
      if (resolved) return;
      let alive = true;
      (pending ||= load()).then(
        C => { resolved = C; if (alive) setLoaded(() => C); },
        e => { pending = null; if (alive) setErr(e); },
      );
      return () => { alive = false; };
    }, []);
    if (err) throw err;
    const Comp = loaded;
    if (!Comp) return html`<div class="view page"><div class="cm-blank"><span class="spinner"></span></div></div>`;
    return html`<${Comp} ...${props} />`;
  };
}

export const ROUTES = [
  { id: 'sessions', page: SessionsPage },
  { id: 'inbox', page: InboxPage },
  { id: 'more', page: MorePage },
  { id: 'chat', page: ChatView },
  { id: 'agents', page: AgentsPage },
  { id: 'local', page: lazyPage(() => import('../features/local/page.js').then(m => m.LocalPage)) },
  { id: 'cloud', page: lazyPage(() => import('../features/cloud/page.js').then(m => m.CloudPage)) },
  { id: 'voice', page: lazyPage(() => import('../features/voice/page.js').then(m => m.VoicePage)) },
  { id: 'jarvis', page: lazyPage(() => import('../features/voice/jarvis.js').then(m => m.JarvisPage)) },
  { id: 'terminals', page: lazyPage(() => import('../features/terminals/page.js').then(m => m.TerminalsPage)) },
  { id: 'usage', page: lazyPage(() => import('../features/usage/page.js').then(m => m.UsagePage)) },
  { id: 'quotas', page: lazyPage(() => import('../features/agents/quotas.js').then(m => m.QuotasPage)) },
  { id: 'market', page: lazyPage(() => import('../features/market/page.js').then(m => m.MarketPage)) },
  { id: 'machines', page: lazyPage(() => import('../features/machines/page.js').then(m => m.MachinesPage)) },
  { id: 'files', page: lazyPage(() => import('../features/files/page.js').then(m => m.FilesPage)) },
  { id: 'connections', page: lazyPage(() => import('../features/connections/page.js').then(m => m.ConnectionsPage)) },
  { id: 'engine', page: lazyPage(() => import('../features/machines/panes.js').then(m => m.EnginePage)) },
  { id: 'workspaces', page: lazyPage(() => import('../features/machines/panes.js').then(m => m.WorkspacesPage)) },
  { id: 'settings', page: lazyPage(() => import('../features/settings/page.js').then(m => m.SettingsPage)) },
];
export const pageFor = id => (ROUTES.find(r => r.id === id) || {}).page;
// Une entrée de barre latérale par groupe, qui ouvre sa première page.
export const NAV_ITEMS = GROUPS.map(g => ({ id: g.id, mode: g.mode || 'both', href: '#/' + g.tabs[0][0], nav: { get label() { return g.label; }, icon: g.icon } }));
