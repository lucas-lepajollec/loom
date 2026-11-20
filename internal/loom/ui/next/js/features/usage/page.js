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
function modelKind(model, runtimes) {
  const kind = model.kind || runtimes.find(rt => rt.id === model.runtime_id)?.kind;
  if (kind) return kind;
  if (!model.runtime_id || model.runtime_id === 'openai-compatible') return 'cloud';
  return model.runtime_id === 'llama.cpp' ? 'local' : 'harness';
}

const tokens = n => Number.isFinite(n) && n >= 0 ? fmtTok(n) : '—';
const quiet = e => /unavailable|not available|not installed/i.test(e || '');
const number = (value, digits) => value.toLocaleString(locale(), { maximumFractionDigits: digits });
function money(value, currency, digits = 2) {
  if (currency && /^[A-Z]{3}$/i.test(currency)) {
    return value.toLocaleString(locale(), { style: 'currency', currency, maximumFractionDigits: digits });
  }
  return [number(value, digits), currency].filter(Boolean).join(' ');
}

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
  const label = input != null && output != null
    ? `${number(input, 6)} / ${number(output, 6)} ${currency}` : t('usage.page.saisir_le_prix');
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
  return html`<button type="button" class="btn sm usage-price" title=${title} aria-label=${label + ' · ' + title}
      onClick=${() => setDraft({ input: input ?? '', output: output ?? '', currency })}>
    <${Icon} n="edit" />
    <span>${label}</span>
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
  const [reading, setReading] = useState(false);
  const amount = v => money(v, b.currency);
  const label = t('usage.activity_refresh_provider', { provider: b.name });
  const reread = async () => {
    setReading(true);
    try { await refresh(b.provider_id); }
    finally { setReading(false); }
  };
  const metrics = [
    [t('usage.bal.granted'), b.granted], [t('usage.bal.used'), b.used], [t('usage.bal.day'), b.period_usage?.day],
    [t('usage.bal.week'), b.period_usage?.week], [t('usage.bal.month'), b.period_usage?.month],
  ].filter(([, value]) => value != null);
  const hasLimit = b.limit > 0 && b.limit_remaining != null;
  const used = hasLimit ? Math.max(0, Math.min(100, (1 - b.limit_remaining / b.limit) * 100)) : 0;
  return html`<article class="activity-card usage-balance">
    <header class="usage-balance-head">
      <${Logo} name=${b.name} size="sm" />
      <h3>${b.name}</h3>
      ${b.fetched_at > 0 && html`<small class="muted">${age(b.fetched_at)}</small>`}
      <button type="button" class="icon-btn" aria-label=${label} title=${label} disabled=${reading} onClick=${reread}>
        ${reading ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}
      </button>
    </header>
    ${b.error && html`<p class=${quiet(b.error) ? 'note' : 'note err'}>${b.error}</p>`}
    ${b.balance != null && html`<div class="usage-balance-total"><span>${t('usage.activity_balance')}</span><b>${amount(b.balance)}</b></div>`}
    ${hasLimit && html`<div class="usage-balance-limit">
      <div><span>${t('usage.bal.key_limit')}</span><b>${t('usage.bal.left', { v: amount(b.limit_remaining) })}</b></div>
      <div class=${used > 85 ? 'q-bar q-amber' : 'q-bar'} aria-hidden="true"><i style=${{ width: used + '%' }}></i></div>
      <small class="muted">${t('usage.bal.of', { v: amount(b.limit) })}</small>
    </div>`}
    ${metrics.length > 0 && html`<dl class="usage-balance-metrics">${metrics.map(([label, value]) => html`<div key=${label}><dt>${label}</dt><dd>${amount(value)}</dd></div>`)}</dl>`}
    ${!b.error && b.balance == null && !hasLimit && !metrics.length && html`<p class="note">${t('usage.activity_no_balance')}</p>`}
    ${b.free_tier && html`<span class="tag">${t('usage.bal.free')}</span>`}
  </article>`;
}

function Balances({ revision }) {
  const [rows, setRows] = useState(recall('balances'));
  const [error, setError] = useState('');
  const [reread, setReread] = useState(0);
  useVisibleRefresh(async alive => {
    try {
      const r = await get('/api/usage/providers', { retryAuth: false });
      if (!alive()) return;
      setRows(remember('balances', r.providers || []));
      setError('');
    } catch (err) {
      if (alive()) setError(err.message || t('common.error'));
    }
  }, 30000, `${revision}:${reread}`);
  const refresh = async id => {
    const r = await post('/api/usage/providers/refresh', { provider_id: id }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t('common.error'), 'err');
    // Invalidate a read already in flight so it cannot overwrite the fresh balance.
    setReread(n => n + 1);
  };
  const shown = rows?.filter(b => b.supported) || [];
  const unsupported = rows?.filter(b => !b.supported) || [];
  return html`<section class="activity-section"><div class="activity-heading"><h2>${t('usage.bal.title')}<${Tip} text=${t('usage.bal.tip')} /></h2></div>
    ${error && html`<p class="note err" role="alert">${t('usage.activity_balances_unavailable', { error })}</p>`}
    ${!rows && !error && html`<div class="skeleton usage-balance-loading"></div>`}
    ${rows?.length === 0 && html`<div class="activity-card usage-empty"><${Empty} icon="cloud" title=${t('usage.activity_no_providers')} /></div>`}
    ${shown.length > 0 && html`<div class="activity-grid usage-balances">${shown.map(b => html`<${BalanceCard} key=${b.provider_id} b=${b} refresh=${refresh} />`)}</div>`}
    ${unsupported.length > 0 && html`<p class="activity-card usage-card-note note">${t('usage.bal.unsupported', { list: unsupported.map(b => b.name).join(', ') })}</p>`}
  </section>`;
}

export function UsagePage() {
  const ws = useStore(app, s => s.workspace);
  const runtimes = ws?.runtimes || [];
  const [data, setData] = useState(recall('usage'));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const refresh = () => setRevision(n => n + 1);
  useVisibleRefresh(async alive => {
    setLoading(true);
    try {
      const r = await get('/api/usage', { retryAuth: false });
      if (!alive()) return;
      setData(remember('usage', r));
      setError('');
    } catch (err) {
      if (alive()) setError(err.message || t('common.error'));
      throw err;
    } finally {
      if (alive()) setLoading(false);
    }
  }, 4000, revision);
  const models = data?.models || [];
  const headings = [t('usage.page.modele'), t('usage.page.entree_2'), t('usage.page.sortie_2'), t('usage.page.prix_m_tokens'), t('usage.page.estimation')];
  return html`<${ActivityLayout} title=${t('app.routes.usage')}>
    <${Balances} revision=${revision} />
    <section class="activity-section" aria-labelledby="usage-models">
      <div class="activity-heading">
        <h2 id="usage-models">${t('usage.page.tokens_et_cout_estime')}<${Tip} text=${t('usage.page.tokens_rapportes_par_les_runtimes_dans_tes_discussions_loom_le_co')} /></h2>
        <button type="button" class="btn sm usage-refresh" onClick=${refresh} disabled=${loading}>
          ${loading && html`<span class="spinner"></span><span class="sr" role="status">${t('usage.refreshing')}</span>`}
          ${t('common.refresh')}
        </button>
      </div>
      ${error && html`<p class="note err" role="alert">${t('usage.activity_unavailable', { error })}</p>`}
      ${data && !models.some(m => modelKind(m, runtimes) === 'local') && html`<p class="note usage-scope">${t('usage.activity_local_scope')}</p>`}
      ${!data ? !error && html`<div class="skeleton usage-model-loading"></div>` : !models.length
        ? html`<div class="activity-card usage-empty"><${Empty} icon="chart" title=${t('usage.page.rien_pour_l_instant')} text=${t('usage.page.les_tokens_des_discussions_cloud_et_harness_apparaitront_ici_les')} /></div>`
        : html`<div class="card table usage-t" role="table" aria-labelledby="usage-models">
          <div class="tr th" role="row">${headings.map(label => html`<span key=${label} role="columnheader">${label}</span>`)}</div>
          ${models.map(m => {
            const type = modelKind(m, runtimes);
            const typeLabel = type === 'cloud' ? t('usage.activity_cloud') : type === 'local' ? t('usage.activity_local') : t('usage.activity_archive');
            const name = modelName(m);
            const recorded = m.reported_cost != null;
            const amount = recorded ? m.reported_cost : type === 'cloud' ? m.estimated_cost : null;
            const currency = recorded ? m.currency : m.price?.currency || 'USD';
            return html`<div class="tr" role="row" key=${m.choice_id || `${m.runtime_id}:${m.provider}:${m.name}`}>
              <span class="cell-id" role="rowheader">
                <${Logo} name=${m.provider || m.runtime_id} size="sm" />
                <span class="cell-main">
                  <span class="usage-model-name"><b title=${name}>${name}</b><span class=${`tag usage-kind usage-kind-${type}`}>${typeLabel}</span></span>
                  <small>${[m.provider, m.turns != null && t('usage.activity_turns', { n: m.turns }), m.reported_turns > 0 && t('usage.activity_counted', { n: m.reported_turns })].filter(Boolean).join(' · ')}</small>
                </span>
              </span>
              <span class="num" role="cell" data-label=${headings[1]}>${m.reported_turns > 0 ? tokens(m.usage?.prompt_tokens) : '—'}</span>
              <span class="num" role="cell" data-label=${headings[2]}>${m.reported_turns > 0 ? tokens(m.usage?.completion_tokens) : '—'}</span>
              <span role="cell" data-label=${headings[3]}>${type === 'cloud' ? html`<${Price} m=${m} onSaved=${refresh} />` : html`<span class="muted">—</span>`}</span>
              <span class="num strong usage-cost" role="cell" data-label=${headings[4]}>
                <span>${amount == null ? '—' : money(amount, currency, amount > 0 && amount < 0.01 ? 6 : 3)}</span>
                ${recorded && html`<small class="muted" title=${t('usage.page.cout_declare_par_le_harness')}>${t('usage.activity_reported_cost')}</small>`}
              </span>
            </div>`;
          })}
        </div>`}
    </section>
  </${ActivityLayout}>`;
}
