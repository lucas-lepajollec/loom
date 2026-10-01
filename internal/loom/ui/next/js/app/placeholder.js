import { html } from '../core/lib.js';
import { Empty } from '../ui/controls.js';
import { go } from '../core/state.js';

// Adresse inconnue : retour à la discussion.
export function Placeholder() {
  return html`<div class="view page"><div class="page-in">
    <${Empty} icon="sparkle" title="Page introuvable" text="Cette adresse ne correspond à aucune page de Loom.">
      <button class="btn" onClick=${() => go('chat')}>Revenir aux discussions</button>
    </${Empty}></div></div>`;
}
