import { t, tSource, locale } from '../../core/i18n.js';
// Usage : quotas des abonnements (lecture automatique, sans génération) et
// tokens/coûts estimés des discussions. Une donnée absente reste « inconnue ».
import { Logo } from '../../ui/logo.js';
import { SectionTabs } from '../../app/sections.js';
import { html, useState, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip, Seg } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';
import { useVisibleRefresh } from './refresh.js';
import { recall, remember } from '../../core/observations.js';

const when = s => s ? new Date(s * 1000).toLocaleString(locale(), { dateStyle: 'medium', timeStyle: 'short' }) : t("usage.page.non_communique");
const ago = s => { if (!s) return ''; const m = Math.round((Date.now() / 1000 - s) / 60); return m < 1 ? t("usage.page.a_l_instant") : m < 60 ? t("usage.page.ago_min", { n: m }) : t("usage.page.ago_h", { n: Math.round(m / 60) }); };



function Price({ m, onSaved }) {
  const [open, setOpen] = useState(false);
  const [inp, setInp] = useState(m.price ? m.price.input_per_million : '');
  const [out, setOut] = useState(m.price ? m.price.output_per_million : '');
  const [cur, setCur] = useState(m.price ? m.price.currency : 'USD');
  const save = async () => { const r = await post('/api/usage/price', { choice_id: m.choice_id, input_per_million: +inp, output_per_million: +out, currency: cur }); if (!r.ok) return toast(r.error, 'err'); setOpen(false); onSaved(); };
  if (!open) return html`<button class="btn sm ghost" onClick=${() => setOpen(true)}>${m.price ? m.price.input_per_million + ' / ' + m.price.output_per_million + ' ' + m.price.currency : t("usage.page.saisir_le_prix")}</button>`;
  return html`<span class="price-edit"><input class="input sm num" placeholder="${t("usage.page.entree")}" value=${inp} onInput=${e => setInp(e.target.value)} /><input class="input sm num" placeholder="${t("usage.page.sortie")}" value=${out} onInput=${e => setOut(e.target.value)} />
    <select class="select sm" style="min-width:74px" value=${cur} onChange=${e => setCur(e.target.value)}><option>USD</option><option>EUR</option></select><button class="btn sm primary" onClick=${save}>OK</button></span>`;
}

// Usage natif : toutes les sessions du harness, dans Loom et en dehors, lues
// dans ses journaux ou ses statistiques. Aucun prix n'est reconstruit.
const quiet = e => /unavailable|not available|not installed/i.test(e || '');




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
  const money = (v, cur) => v == null ? '—' : cur && /^[A-Z]{3}$/.test(cur) ? Number(v).toLocaleString(locale(), { style: 'currency', currency: cur, maximumFractionDigits: 2 }) : Number(v).toLocaleString(locale(), { maximumFractionDigits: 2 }) + (cur ? ' ' + cur : '');
  const kv = (label, v) => v == null ? '' : html`<div class="kv"><span>${label}</span><span class="mono">${v}</span></div>`;
  return html`<section class="sec"><div class="sec-h"><h2>${t('usage.bal.title')}<${Tip} text=${t('usage.bal.tip')} /></h2></div>
    ${shown.length ? html`<div class="grid3 stagger bal-grid">${shown.map(b => { const cur = b.currency || 'USD'; const lim = b.limit != null && b.limit_remaining != null && b.limit > 0;
      const used = lim ? Math.max(0, Math.min(100, 100 - b.limit_remaining / b.limit * 100)) : 0;
      return html`<div class="card pad quota" key=${b.provider_id}>
        <div class="q-h"><b>${b.name}</b><span class="muted">${b.fetched_at ? ago(b.fetched_at) : ''}</span>
          <button class="icon-btn" aria-label=${t('usage.native.reread')} title=${t('usage.native.reread')} onClick=${() => refresh(b.provider_id)}><${Icon} n="refresh" /></button></div>
        ${b.error && html`<p class="note err">${b.error}</p>`}
        ${b.balance != null && html`<div class="bal-v"><b>${money(b.balance, cur)}</b><small class="muted">${t('usage.bal.balance')}</small></div>`}
        ${lim && html`<div class="qw"><div class="qw-h"><span>${t('usage.bal.key_limit')}</span><b class="mono">${t('usage.bal.left', { v: money(b.limit_remaining, cur) })}</b></div>
          <div class="meter"><i style=${`width:${used}%;background:${used > 85 ? 'var(--amber)' : 'var(--text)'}`}></i></div><small class="muted">${t('usage.bal.of', { v: money(b.limit, cur) })}</small></div>`}
        ${kv(t('usage.bal.granted'), b.granted != null ? money(b.granted, cur) : null)}
        ${kv(t('usage.bal.used'), b.used != null ? money(b.used, cur) : null)}
        ${kv(t('usage.bal.day'), b.period_usage && b.period_usage.day != null ? money(b.period_usage.day, cur) : null)}
        ${kv(t('usage.bal.week'), b.period_usage && b.period_usage.week != null ? money(b.period_usage.week, cur) : null)}
        ${kv(t('usage.bal.month'), b.period_usage && b.period_usage.month != null ? money(b.period_usage.month, cur) : null)}
        ${b.free_tier && html`<span class="tag">${t('usage.bal.free')}</span>`}
      </div>`; })}</div>` : ''}
    ${unsupported.length ? html`<p class="note">${t('usage.bal.unsupported', { list: unsupported.map(b => b.name).join(', ') })}</p>` : ''}
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
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} /><div class="page-head"><div><h1>${t("usage.page.usage")}</h1><p>${t("usage.page.quotas_de_tes_abonnements_et_consommation_dans_loom_une_donnee_ab")}</p></div></div>
    ${!d ? html`<div class="skeleton" style="height:200px"></div>` : html`
      ${loading && html`<p class="note" role="status"><span class="spinner"></span> ${t('usage.refreshing')}</p>`}
      <${Balances} />
      <section class="sec"><div class="sec-h"><h2>${t("usage.page.in_loom")}<${Tip} text="${t("usage.page.tokens_rapportes_par_les_runtimes_dans_tes_discussions_loom_le_co")}" /></h2></div>
        ${models.length ? html`<div class="card table usage-t">
          <div class="tr th"><span>${t("usage.page.modele")}</span><span>${t("usage.page.entree_2")}</span><span>${t("usage.page.sortie_2")}</span><span>${t("usage.page.prix_m_tokens")}</span><span>${t("usage.page.estimation")}</span></div>
          ${models.map(m => html`<div class="tr"><span class="cell-id"><${Logo} name=${m.runtime_id || m.provider} size="sm" /><span class="cell-main"><b>${m.name && m.name !== 'default' ? m.name : t("usage.page.modele_par_defaut")}</b><small>${m.provider} · ${m.turns} ${t("usage.page.tour")}${m.turns > 1 ? 's' : ''}${m.reported_turns ? ' · ' + m.reported_turns + t("usage.page.avec_decompte") : ''}</small></span></span>
            <span class="mono" data-label=${t("usage.page.entree_2")}>${m.reported_turns ? fmtTok(m.usage.prompt_tokens) : '—'}</span><span class="mono" data-label=${t("usage.page.sortie_2")}>${m.reported_turns ? fmtTok(m.usage.completion_tokens) : '—'}</span>
            <span data-label=${t("usage.page.prix_m_tokens")}>${isCloud(m) ? html`<${Price} m=${m} onSaved=${load} />` : html`<span class="muted">—</span>`}</span><span class="num strong" data-label=${t("usage.page.estimation")}>${cost(m)}</span></div>`)}</div>`
          : html`<div class="card"><${Empty} icon="chart" title="${t("usage.page.rien_pour_l_instant")}" text="${t("usage.page.les_tokens_des_discussions_cloud_et_harness_apparaitront_ici_les")}" /></div>`}
      </section>`}
  </div></div>`;
}
