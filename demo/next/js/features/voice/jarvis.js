// Jarvis vit désormais sur la page unifiée Voix (deux colonnes). La route
// #/jarvis ouvre cette même page et amène le focus sur la colonne Jarvis.
import { html } from '../../core/lib.js';
import { VoicePage } from './page.js';

export function JarvisPage({ route }) {
  return html`<${VoicePage} route=${route} focus="jarvis" />`;
}
