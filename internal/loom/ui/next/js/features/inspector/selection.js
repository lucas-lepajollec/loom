// Catalog inspection is read-only; it does not alter the chat selection or
// contact a provider/native CLI. Unknown model limits remain unknown.
import { html } from '../../core/lib.js';

const CAP_LABELS = { 'fresh-text-handoff': 'Contexte transmis en texte', chat: 'Discussion', stream: 'Flux de réponse', tools: 'Outils', attachments: 'Pièces jointes', cancel: 'Annulation', connect: 'Connexion', quota: 'Lecture des quotas', usage: 'Consommation', 'native-events': 'Événements natifs', 'reasoning-summary': 'Résumé de réflexion', approvals: 'Autorisations', skills: 'Skills', mcp: 'MCP' };
const KV = ({ label, children }) => html`<div class="kv"><span>${label}</span><span>${children}</span></div>`;

export function SelectionInfo({ model, provider, runtime, models = [] }) {
  const harness = !!runtime;
  const native = harness ? models.filter(m => m.runtime_id === runtime.id) : [];
  const name = model?.name || runtime?.name || provider?.name;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${name}</b><span>${harness ? 'harness · ' + runtime.name : 'modèle cloud · ' + provider.name}</span></div>
    ${runtime?.description && html`<p class="note">${runtime.description}</p>`}
    ${model && html`<${KV} label="Identifiant"><code class="mono">${model.model}</code></${KV}>
      <${KV} label="Dans le sélecteur">${model.enabled ? 'oui' : 'non'}</${KV}>`}
    ${provider && html`<${KV} label="Connexion">${provider.ready ? 'clé en mémoire' : 'clé à reconnecter'}</${KV}>
      <label class="field"><span>URL de base</span><code class="mono" style="overflow-wrap:anywhere">${provider.endpoint}</code></label>
      <p class="note">API Chat Completions. Les limites de contexte et capacités propres à ce modèle ne sont pas déclarées par ce catalogue.</p>`}
    ${runtime && html`<${KV} label="Adaptateur">${runtime.implemented ? 'disponible' : 'en préparation'}</${KV}>
      ${runtime.cli && html`<${KV} label="CLI"><code class="mono">${runtime.cli}</code></${KV}>`}
      <${KV} label="Catalogue">${native.length ? native.length + ' modèles déclarés' : 'non connecté'}</${KV}>
      ${model && html`<${KV} label="Exécution">${model.ready ? 'CLI disponible' : 'CLI indisponible'}</${KV}>
        <${KV} label="Réflexion">${model.reasoning_efforts?.length ? model.reasoning_efforts.join(' · ') : 'non déclarée'}</${KV}>
        ${model.default_effort && html`<${KV} label="Niveau par défaut">${model.default_effort}</${KV}>`}`}
      <div class="sec-h"><h2>Capacités de l’adaptateur</h2></div>
      ${(runtime.capabilities || []).map(c => html`<${KV} label=${CAP_LABELS[c] || c}><span class="tag green">oui</span></${KV}>`)}
      ${!runtime.capabilities?.length && html`<p class="note">Aucune capacité active pour cet adaptateur.</p>`}
      <p class="note">Le harness conserve ses permissions, sa mémoire et ses réglages natifs.</p>`}
  </div>`;
}
