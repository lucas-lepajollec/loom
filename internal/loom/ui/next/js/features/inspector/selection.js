import { t } from '../../core/i18n.js';
// Catalog inspection is read-only; it does not alter the chat selection or
// contact a provider/native CLI. Unknown model limits remain unknown.
import { html } from '../../core/lib.js';

const KV = ({ label, children }) => html`<div class="kv"><span>${label}</span><span>${children}</span></div>`;

export function SelectionInfo({ model, provider }) {
  const name = model?.name || provider?.name;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${name}</b><span>${t("inspector.selection.modele_cloud") + provider.name}</span></div>
    ${model && html`<${KV} label="${t("inspector.selection.identifiant")}"><code class="mono">${model.model}</code></${KV}>
      <${KV} label="${t("inspector.selection.dans_le_selecteur")}">${model.enabled ? t('common.yes_lower') : t('common.no_lower')}</${KV}>`}
    ${provider && html`<${KV} label="${t("inspector.selection.connexion")}">${provider.ready ? t("inspector.selection.cle_en_memoire") : t("inspector.selection.cle_a_reconnecter")}</${KV}>
      <label class="field"><span>${t("inspector.selection.url_de_base")}</span><code class="mono" style="overflow-wrap:anywhere">${provider.endpoint}</code></label>
      <p class="note">${t("inspector.selection.api_chat_completions_les_limites_de_contexte_et_capacites_propres")}</p>`}
  </div>`;
}
