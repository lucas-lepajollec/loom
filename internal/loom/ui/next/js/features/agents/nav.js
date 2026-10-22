// Navigation du module, partagée par les vues sans dépendre de la page.
import { go } from '../../core/state.js';
import { agents } from './store.js';
import { gridOn, placeSession } from './layout.js';

// Grille ouverte : la session s'affiche dans un volet (celui qui a le focus si
// elle n'y est pas encore). Sinon elle remplace la vue seule.
export const openSession = id => { if (gridOn() && innerWidth > 760) placeSession(id); go('agents', 's', id); };
// Interface officielle de Codeman, sur la session voulue (#session=<id> est
// un lien stable de Codeman).
export const officialLink = id => { const u = agents.get().conn.ui_url; return u ? u + (id ? '/#session=' + encodeURIComponent(id) : '/') : ''; };
