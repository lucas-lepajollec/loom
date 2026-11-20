import { t, locale } from '../../core/i18n.js';
// Soldes cloud et consommation des discussions Loom.
import { Logo } from '../../ui/logo.js';
import { SectionTabs } from '../../app/sections.js';
import { html, useState, useStore, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { Modal, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';
import { useVisibleRefresh } from './refresh.js';
import { recall, remember } from '../../core/observations.js';

export function ActivityLayout({ title, children }) {
  return html`<div class="view page activity-view">
    <header class="activity-mobile-head only-mobile">
      <button type="button" class="icon-btn" aria-label=${t('app.main.menu')} onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button>
      <b>${t('app.groups.activity')}</b>
    </header>
    <div class="page-in activity-in"><${SectionTabs} />${title && html`<h1 class="sr">${title}</h1>`}${children}</div>
  </div>`;
}

const modelName = m => m.name && m.name !== 'default' ? m.name : t('usage.page.modele_par_defaut');
const tokens = n => n == null ? '—' : fmtTok(n);
const quiet = e => /unavailable|not available|not installed/i.test(e || '');
const age = s => {
  const m = Math.round((Date.now() / 1000 - s) / 60);
  if (m < 1) return t('usage.page.a_l_instant');
  return m < 60 ? t('usage.page.ago_min', { n: m }) : t('usage.page.ago_h', { n: Math.round(m / 60) });
};

function Price({ m, onSaved }) {
  const [draft, setDraft] = useState(null);
  const [saving, setSaving] = useState(false);
  const input = m.price?.input_per_million, output = m.price?.output_per_million, currency = m.price?.currency || 'USD';
  const title = t('usage.activity_price_title', { model: modelName(m) });
  const close = () => { if (!saving) setDraft(null); };
  const save = async e => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    try {
      const r = await post('/api/usage/price', {
        choice_id: m.choice_id, input_per_million: Number(draft.input), output_per_million: Number(draft.output), currency: draft.currency,
      });
      if (!r.ok) return toast(r.error || t('common.error'), 'err');
      setDraft(null);
      onSaved();
    } catch (err) { toast(err.message, 'err'); }
    finally { setSaving(false); }
  };
  return html`<button type="button" class="btn sm usage-price" title=${title}
      onClick=${() => setDraft({ input: input ?? '', output: output ?? '', currency })}>
    ${input != null && output != null ? `${input} / ${output} ${currency}` : t('usage.page.saisir_le_prix')}
  </button>
  ${draft && html`<${Modal} title=${title} sub=${t('usage.page.prix_m_tokens')} onClose=${close}>
    <form class="usage-price-form" onSubmit=${save}>
      <div class="usage-price-fields">
        <label class="field"><span>${t('usage.page.entree_2')}</span><input class="input num" type="number" min="0" max="10000" step="any" required value=${draft.input}
          onInput=${e => setDraft({ ...draft, input: e.target.value })} /></label>
        <label class="field"><span>${t('usage.page.sortie_2')}</span><input class="input num" type="number" min="0" max="10000" step="any" required value=${draft.output}
          onInput=${e => setDraft({ ...draft, output: e.target.value })} /></label>
      </div>
      <label class="field"><span>${t('usage.activity_currency')}</span><select class="select" value=${draft.currency}
        onChange=${e => setDraft({ ...draft, currency: e.target.value })}><option>USD</option><option>EUR</option></select></label>
      <div class="acts"><button type="button" class="btn ghost" disabled=${saving} onClick=${close}>${t('ui.dialog.annuler')}</button>
        <button type="submit" class="btn primary" disabled=${saving}>${t('ui.dialog.enregistrer')}</button></div>
    </form>
  </${Modal}>`}`;
}

function BalanceCard({ b, refresh }) {
  const cur = b.currency;
  const money = v => cur && /^[A-Z]{3}$/.test(cur)
    ? Number(v).toLocaleString(locale(), { style: 'currency', currency: cur, maximumFractionDigits: 2 })
    : Number(v).toLocaleString(locale(), { maximumFractionDigits: 2 }) + (cur ? ' ' + cur : '');
  const metrics = [
    [t('usage.bal.granted'), b.granted], [t('usage.bal.used'), b.used], [t('usage.bal.day'), b.period_usage?.day],
    [t('usage.bal.week'), b.period_usage?.week], [t('usage.bal.month'), b.period_usage?.month],
  ].filter(([, value]) => value != null);
  const hasLimit = b.limit > 0 && b.limit_remaining != null;
  const used = hasLimit ? Math.max(0, Math.min(100, 100 - b.limit_remaining / b.limit * 100)) : 0;
  return html`<article class="activity-card usage-balance">
    <header class="usage-balance-head"><h3>${b.name}</h3>${b.fetched_at > 0 && html`<small class="muted">${age(b.fetched_at)}</small>`}
      <button type="button" class="icon-btn" aria-label=${t('usage.native.reread')} title=${t('usage.native.reread')} onClick=${() => refresh(b.provider_id)}><${Icon} n="refresh" /></button>
    </header>
    ${b.error && html`<p class=${quiet(b.error) ? 'note' : 'note err'}>${b.error}</p>`}
    ${b.balance != null && html`<div class="usage-balance-total"><span>${t('usage.bal.balance')}</span><b>${money(b.balance)}</b></div>`}
    ${hasLimit && html`<div class="usage-balance-limit">
      <div><span>${t('usage.bal.key_limit')}</span><b>${t('usage.bal.left', { v: money(b.limit_remaining) })}</b></div>
      <div class="meter" aria-hidden="true"><i style=${`width:${used}%;background:${used > 85 ? 'var(--amber)' : 'var(--text-2)'}`}></i></div>
      <small class="muted">${t('usage.bal.of', { v: money(b.limit) })}</small>
    </div>`}
    ${metrics.length > 0 && html`<dl class="usage-balance-metrics">${metrics.map(([label, value]) => html`<div key=${label}><dt>${label}</dt><dd>${money(value)}</dd></div>`)}</dl>`}
    ${b.free_tier && html`<span class="tag">${t('usage.bal.free')}</span>`}
  </article>`;
}


// Soldes des fournisseurs cloud qui les exposent (OpenRouter, DeepSeek…),
// lus avec leur propre clé, sans génération. Un montant absent reste « — ».
function Balances() {
  const [rows, setRows] = useState(recall('balances'));
  const load = () => get('/api/usage/providers').then(r => { if (r.ok !== false) setRows(remember('balances', r.providers || [])); }).catch(() => {});
  useVisibleRefresh(async alive => { const r = await get('/api/usage/providers', { retryAuth: false }); if (alive() && r.ok !== false) setRows(remember('balances', r.providers || [])); }, 30000);
  const refresh = async id => {
    const r = await post('/api/usage/providers/refresh', { provider_id: id }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error, 'err');
    load();
  };
  if (!rows || !rows.length) return null;
  const shown = rows.filter(b => b.supported), unsupported = rows.filter(b => !b.supported);
  return html`<section class="activity-section"><div class="activity-heading"><h2>${t('usage.bal.title')}<${Tip} text=${t('usage.bal.tip')} /></h2></div>
    ${shown.length > 0 && html`<div class="activity-grid usage-balances">${shown.map(b => html`<${BalanceCard} key=${b.provider_id} b=${b} refresh=${refresh} />`)}</div>`}
    ${unsupported.length > 0 && html`<p class="note">${t('usage.bal.unsupported', { list: unsupported.map(b => b.name).join(', ') })}</p>`}
  </section>`;
}

export function UsagePage() {
  const ws = useStore(app, s => s.workspace);
  const runtimes = (ws && ws.runtimes) || [];
  const [d, setD] = useState(recall('usage'));
  const [loading, setLoading] = useState(false);
  const accept = r => { if (r.ok !== false) setD(remember('usage', r)); };
  const load = async () => { setLoading(true); try { accept(await get('/api/usage')); } catch (_) {} finally { setLoading(false); } };
  useVisibleRefresh(async alive => {
    setLoading(true);
    try { const r = await get('/api/usage', { retryAuth: false }); if (alive()) accept(r); }
    finally { if (alive()) setLoading(false); }
  }, 4000);
  const models = d ? d.models || [] : [];
  const fmtMoney = (v, cur) => Number(v).toLocaleString(locale(), { style: 'currency', currency: cur || 'USD', maximumFractionDigits: 3 });
  const money = m => m.estimated_cost != null ? fmtMoney(m.estimated_cost, m.price && m.price.currency) : '—';
  const isCloud = m => m.kind === 'cloud' || (!m.kind && (!m.runtime_id || m.runtime_id === 'openai-compatible' || runtimes.some(rt => rt.id === m.runtime_id && rt.kind === 'cloud')));
  const cost = m => m.reported_cost != null ? html`<span title="${t("usage.page.cout_declare_par_le_harness")}">${fmtMoney(m.reported_cost, m.currency)}</span>` : isCloud(m) ? money(m) : '—';
  return html`<${ActivityLayout} title=${t('app.routes.usage')}>
    ${!d ? html`<div class="skeleton" style="height:200px"></div>` : html`
      <${Balances} />
      <section class="activity-section"><div class="activity-heading">
        <h2>${t('usage.page.in_loom')}<${Tip} text=${t('usage.page.tokens_rapportes_par_les_runtimes_dans_tes_discussions_loom_le_co')} /></h2>
        ${loading && html`<small class="usage-refresh muted" role="status"><span class="spinner"></span>${t('usage.refreshing')}</small>`}
      </div>
        ${models.length ? html`<div class="card table usage-t">
          <div class="tr th"><span>${t('usage.page.modele')}</span><span>${t('usage.page.entree_2')}</span><span>${t('usage.page.sortie_2')}</span><span>${t('usage.page.prix_m_tokens')}</span><span>${t('usage.page.estimation')}</span></div>
          ${models.map(m => html`<div class="tr" key=${m.choice_id || `${m.runtime_id}:${m.provider}:${m.name}`}>
            <span class="cell-id"><${Logo} name=${m.runtime_id || m.provider} size="sm" /><span class="cell-main"><b>${modelName(m)}</b>
              <small>${[m.provider, m.turns != null && t('usage.activity_turns', { n: m.turns }), m.reported_turns && t('usage.activity_counted', { n: m.reported_turns })].filter(Boolean).join(' · ')}</small>
            </span></span>
            <span class="num" data-label=${t('usage.page.entree_2')}>${m.reported_turns ? tokens(m.usage?.prompt_tokens) : '—'}</span>
            <span class="num" data-label=${t('usage.page.sortie_2')}>${m.reported_turns ? tokens(m.usage?.completion_tokens) : '—'}</span>
            <span data-label=${t('usage.page.prix_m_tokens')}>${isCloud(m) ? html`<${Price} m=${m} onSaved=${load} />` : html`<span class="muted">—</span>`}</span>
            <span class="num strong" data-label=${t('usage.page.estimation')}>${cost(m)}</span>
          </div>`)}</div>`
          : html`<div class="activity-card"><${Empty} icon="chart" title=${t('usage.page.rien_pour_l_instant')} text=${t('usage.page.les_tokens_des_discussions_cloud_et_harness_apparaitront_ici_les')} /></div>`}
      </section>`}
  </${ActivityLayout}>`;
}
