// Navigation du module, partagée par les vues sans dépendre de la page.
import { go } from '../../core/state.js';
import { agents } from './store.js';

export const openSession = id => go('agents', 's', id);
// Interface officielle de Codeman, sur la session voulue (#session=<id> est
// un lien stable de Codeman).
export const officialLink = id => { const u = agents.get().conn.ui_url; return u ? u + (id ? '/#session=' + encodeURIComponent(id) : '/') : ''; };
