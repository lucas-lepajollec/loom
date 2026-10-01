import { html } from '../core/lib.js';
import { Empty } from '../ui/controls.js';

// Page temporaire pendant la reconstruction : renvoie vers l'interface actuelle.
export function Placeholder({ route }) {
  return html`<div class="view page"><div class="page-in">
    <${Empty} icon="sparkle" title="Page en reconstruction" text="Cette section arrive dans la nouvelle interface. En attendant, elle reste disponible dans l’interface actuelle.">
      <a class="btn" href=${'/classic#' + (route.section === 'local' ? 'hub' : route.section)}>Ouvrir l’interface actuelle</a>
    </${Empty}></div></div>`;
}
