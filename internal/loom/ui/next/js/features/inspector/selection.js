import { t, tSource } from '../../core/i18n.js';
// Catalog inspection is read-only; it does not alter the chat selection or
// contact a provider/native CLI. Unknown model limits remain unknown.
import { html } from '../../core/lib.js';

const CAP_LABELS = () => ({ 'fresh-text-handoff': t("inspector.selection.contexte_transmis_en_texte"), chat: t("inspector.selection.discussion"), stream: t("inspector.selection.flux_de_reponse"), tools: t("inspector.selection.outils"), attachments: t("inspector.selection.pieces_jointes"), cancel: t("inspector.selection.annulation"), connect: t("inspector.selection.connexion"), quota: t("inspector.selection.lecture_des_quotas"), usage: t("inspector.selection.consommation"), 'native-events': t("inspector.selection.evenements_natifs"), 'reasoning-summary': t("inspector.selection.resume_de_reflexion"), approvals: t("inspector.selection.autorisations"), skills: 'Skills', mcp: 'MCP' });
const KV = ({ label, children }) => html`<div class="kv"><span>${label}</span><span>${children}</span></div>`;

export function SelectionInfo({ model, provider, runtime, models = [] }) {
  const harness = !!runtime;
  const native = harness ? models.filter(m => m.runtime_id === runtime.id) : [];
  const name = model?.name || runtime?.name || provider?.name;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${name}</b><span>${harness ? 'harness · ' + runtime.name : t("inspector.selection.modele_cloud") + provider.name}</span></div>
    ${runtime?.description && html`<p class="note">${tSource(runtime.description)}</p>`}
    ${model && html`<${KV} label="${t("inspector.selection.identifiant")}"><code class="mono">${model.model}</code></${KV}>
      <${KV} label="${t("inspector.selection.dans_le_selecteur")}">${model.enabled ? t('common.yes_lower') : t('common.no_lower')}</${KV}>`}
    ${provider && html`<${KV} label="${t("inspector.selection.connexion")}">${provider.ready ? t("inspector.selection.cle_en_memoire") : t("inspector.selection.cle_a_reconnecter")}</${KV}>
      <label class="field"><span>${t("inspector.selection.url_de_base")}</span><code class="mono" style="overflow-wrap:anywhere">${provider.endpoint}</code></label>
      <p class="note">${t("inspector.selection.api_chat_completions_les_limites_de_contexte_et_capacites_propres")}</p>`}
    ${runtime && html`<${KV} label="${t("inspector.selection.adaptateur")}">${runtime.implemented ? t('common.available_lower') : t("inspector.selection.en_preparation")}</${KV}>
      ${runtime.cli && html`<${KV} label="CLI"><code class="mono">${runtime.cli}</code></${KV}>`}
      <${KV} label="${t("inspector.selection.catalogue")}">${native.length ? native.length + t("inspector.selection.modeles_declares") : t("inspector.selection.non_connecte")}</${KV}>
      ${model && html`<${KV} label="${t("inspector.selection.execution")}">${model.ready ? t("inspector.selection.cli_disponible") : t("inspector.selection.cli_indisponible")}</${KV}>
        <${KV} label="${t("inspector.selection.reflexion")}">${model.reasoning_efforts?.length ? model.reasoning_efforts.join(' · ') : t("inspector.selection.non_declaree")}</${KV}>
        ${model.default_effort && html`<${KV} label="${t("inspector.selection.niveau_par_defaut")}">${model.default_effort}</${KV}>`}`}
      <div class="sec-h"><h2>${t("inspector.selection.capacites_de_l_adaptateur")}</h2></div>
      ${(runtime.capabilities || []).map(c => html`<${KV} label=${CAP_LABELS()[c] || c}><span class="tag green">${t("inspector.selection.oui")}</span></${KV}>`)}
      ${!runtime.capabilities?.length && html`<p class="note">${t("inspector.selection.aucune_capacite_active_pour_cet_adaptateur")}</p>`}
      <p class="note">${t("inspector.selection.le_harness_conserve_ses_permissions_sa_memoire_et_ses_reglages_na")}</p>`}
  </div>`;
}
