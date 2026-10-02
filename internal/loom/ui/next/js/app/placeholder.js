import { t } from '../core/i18n.js';
import { html } from '../core/lib.js';
import { Empty } from '../ui/controls.js';
import { go } from '../core/state.js';

// Adresse inconnue : retour à la discussion.
export function Placeholder() {
  return html`<div class="view page"><div class="page-in">
    <${Empty} icon="sparkle" title="${t("app.placeholder.page_introuvable")}" text="${t("app.placeholder.cette_adresse_ne_correspond_a_aucune_page_de_loom")}">
      <button class="btn" onClick=${() => go('chat')}>${t("app.placeholder.revenir_aux_discussions")}</button>
    </${Empty}></div></div>`;
}
