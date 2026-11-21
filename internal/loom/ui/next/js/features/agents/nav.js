// Navigation du module, partagée par les vues sans dépendre de la page.
import { go } from '../../core/state.js';
import { agents } from './store.js';
import { gridOn, layout, placeSession, setViewOf } from './layout.js';
import { isPhone } from '../../core/viewport.js';

// Grille ouverte : la session s'affiche dans un volet (celui qui a le focus si
// elle n'y est pas encore). Sinon elle remplace la vue seule.
export const openSession = (id, view) => {
  if (isPhone()) layout.set({ phoneTerminal: view === 'term' ? id : '' });
  else {
    if (view) setViewOf(id, view);
    if (gridOn()) placeSession(id);
  }
  go('agents', 's', id);
};
// Interface officielle de Codeman, sur la session voulue (#session=<id> est
// un lien stable de Codeman).
export const officialLink = id => { const u = agents.get().conn.ui_url; return u ? u + (id ? '/#session=' + encodeURIComponent(id) : '/') : ''; };
