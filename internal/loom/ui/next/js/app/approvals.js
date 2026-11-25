// Accords demandés par Loom hors d'une discussion : un agent branché sur
// l'accès des agents attend ton accord (règle « Confirmer » des Autorisations)
// pour une commande ou une session. Affichés dans la cloche et la boîte de
// réception du téléphone ; le détail (machine, commande) n'existe qu'en mémoire.
import { html, createStore, useState } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { get, post } from '../core/api.js';
import { toast } from '../ui/dialog.js';

export const loomApprovals = createStore({ items: [] });

export async function refreshLoomApprovals() {
  const r = await get('/api/policy/pending', { timeout: 6000, retryAuth: false }).catch(() => null);
  loomApprovals.set({ items: Array.isArray(r?.requests) ? r.requests.filter(i => i && i.session_id && i.request_id) : [] });
}

const TITLES = () => ({
  'mcp.tool:loom/loom_machine_run': t('approvals.run'),
  'mcp.tool:agents/start_session': t('approvals.session'),
});

export const loomApprovalTitle = item => TITLES()[item.subject] || t('approvals.other');

export function LoomApproval({ item, class: c = '' }) {
  const [busy, setBusy] = useState(false);
  const title = TITLES()[item.subject];
  const answer = async decision => {
    setBusy(true);
    const r = await post('/api/workspace/sessions/' + encodeURIComponent(item.session_id) + '/requests/' + encodeURIComponent(item.request_id), { decision })
      .catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false) toast(r.error || t('approvals.failed'), 'err');
    await refreshLoomApprovals();
  };
  return html`<article class=${'loom-approval ' + c}>
    <b>${title || t('approvals.other')}</b>
    ${!title && html`<small class="mono">${item.subject}</small>`}
    ${item.detail && html`<code class="mono">${item.detail}</code>`}
    <div class="loom-approval-acts">
      <button type="button" class="btn sm" disabled=${busy} onClick=${() => answer('deny')}>${t('approvals.deny')}</button>
      <button type="button" class="btn sm primary" disabled=${busy} onClick=${() => answer('allow_once')}>${t('approvals.allow')}</button>
    </div>
  </article>`;
}
