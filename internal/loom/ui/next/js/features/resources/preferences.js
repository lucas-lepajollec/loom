import { html, useState, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { toast } from '../../ui/dialog.js';

export function SharedPreferences() {
  const [selected, setSelected] = useState(null), [pages, setPages] = useState([]), [error, setError] = useState(''), [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([get('/api/context/preferences', { signal: controller.signal }), get('/api/agent', { signal: controller.signal })])
      .then(([r, a]) => { if (controller.signal.aborted) return; if (!r.ok) throw new Error(r.error); setSelected(r.selection); setPages(a.pages || a.skills || []); setError(''); })
      .catch(e => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [retry]);
  const save = async () => {
    const r = await post('/api/context/preferences', selected).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return toast(r.error, 'err');
    toast(t('resources.memory.page_enregistree'));
  };
  return html`<section class="sec"><div class="sec-h"><h2>${t('brain.preferences.title')}</h2></div><div class="card pad">
    <p class="note">${t('brain.preferences.note')}</p>
    ${error ? html`<p class="note err" role="alert">${error}</p><button class="btn" onClick=${() => setRetry(n => n + 1)}>${t('access.retry')}</button>` : !selected ? html`<div class="skeleton" style="height:80px"></div>` : html`
      <label class="field"><span>${t('brain.preferences.page')}</span><select class="select" value=${selected.page} onChange=${e => setSelected({ ...selected, page: e.target.value })}><option value="">—</option>${pages.map(p => html`<option value=${p.name}>${p.name}</option>`)}</select></label>
      <label class="check"><input type="checkbox" checked=${selected.external} disabled=${!selected.page} onChange=${e => setSelected({ ...selected, external: e.target.checked })} /><span>${t('brain.preferences.external')}</span></label>
      <div class="btn-row"><button class="btn primary" onClick=${save}>${t('resources.memory.enregistrer')}</button><button class="btn ghost" onClick=${() => setRetry(n => n + 1)}>${t('harnesses.page.actualiser')}</button></div>`}
  </div></section>`;
}
