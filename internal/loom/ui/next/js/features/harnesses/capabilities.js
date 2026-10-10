import { capabilityEvidenceLabel } from './options.js';
import { t } from '../../core/i18n.js';
import { html } from '../../core/lib.js';
import { Tip } from '../../ui/controls.js';

// Ce que Loom sait vraiment piloter aujourd'hui, par capacité déclarée.
export const CAPS = () => ([
  ['chat', t("harnesses.page.discussion_dans_le_fil_commun")], ['native-events', t("harnesses.page.outils_natifs_visibles")], ['usage', t("harnesses.page.tokens_et_quotas")],
  ['reasoning-summary', t("harnesses.page.resume_de_reflexion")], ['approvals', t("harnesses.page.autorisations_interactives")], ['skills', t("harnesses.page.skills_partages")], ['mcp', t("harnesses.page.serveurs_mcp")],
  ['tools', t("harnesses.page.outils_et_modifications_visibles")], ['plan', t("harnesses.page.plan_de_l_agent")], ['workdir', t("harnesses.page.dossier_de_travail")], ['remote', t("harnesses.page.sur_une_autre_machine")],
]);
export const isACP = rt => (rt.capabilities || []).includes('workdir');
export function CapabilityEvidence({ rt, compat = rt.compatibility || {} }) {
  if (rt.kind !== 'harness') return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t('agents.evidence.title')}<${Tip} text=${t('agents.evidence.tip')} /></h2></div>
      <div class="card pad details-kv">${[...new Set([...(rt.capabilities || []), ...(rt.degraded || []).map(d => d.capability), ...Object.keys(compat.evidence || {})])].map(id => {
        const evidence = compat.evidence?.[id];
        const health = (rt.degraded || []).some(d => d.capability === id) ? { ...evidence, health: 'degraded' } : evidence;
        return html`<div class="kv"><span>${(CAPS().find(c => c[0] === id) || [id, id])[1]}</span><span class=${health?.health === 'degraded' ? 'state err' : 'muted'}>${capabilityEvidenceLabel(health, t)}</span></div>`;
      })}</div></section>`;
}
