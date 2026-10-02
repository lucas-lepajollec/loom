import { t, tSource, locale } from '../../core/i18n.js';
// Usage : quotas des abonnements (lecture explicite, sans génération) et
// tokens/coûts estimés des discussions. Une donnée absente reste « inconnue ».
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip, Seg } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';

const when = s => s ? new Date(s * 1000).toLocaleString(locale(), { dateStyle: 'medium', timeStyle: 'short' }) : t("usage.page.non_communique");
const ago = s => { if (!s) return ''; const m = Math.round((Date.now() / 1000 - s) / 60); return m < 1 ? t("usage.page.a_l_instant") : m < 60 ? t("usage.page.ago_min", { n: m }) : t("usage.page.ago_h", { n: Math.round(m / 60) }); };

function Quota({ q, rt, onRefresh }) {
  const [busy, setBusy] = useState(false);
  const readable = rt && rt.implemented && (rt.capabilities || []).includes('quota');
  const refresh = async () => {
    setBusy(true);
    try { const r = await post('/api/runtimes/' + encodeURIComponent(q.runtime_id) + '/quota', {}); if (!r.ok) toast(r.error, 'err'); onRefresh(); }
    catch (e) { toast(e.message, 'err'); }
    finally { setBusy(false); }
  };
  const resets = typeof q.reset_credits === 'number' ? q.reset_credits : q.reset_credits && q.reset_credits.available_count != null ? q.reset_credits.available_count : null;
  return html`<div class="card pad quota">
    <div class="q-h"><b>${(rt && rt.name) || q.name || q.runtime_id}</b>${rt && rt.machine && html`<small class="q-m">${rt.machine}</small>`}<span class="muted">${q.fetched_at ? ago(q.fetched_at) : ''}</span>
      ${readable && html`<button class="icon-btn" aria-label="${t("usage.page.lire_le_compte")}" title="${t("usage.page.lire_le_compte_sans_generation")}" disabled=${busy} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}</button>`}</div>
    ${q.error && html`<p class="note err">${q.error}</p>`}
    ${(q.windows || []).length ? q.windows.map(w => { const known = typeof w.remaining_percent === 'number'; const used = known ? 100 - Math.max(0, Math.min(100, w.remaining_percent)) : 0;
      return html`<div class="qw"><div class="qw-h"><span>${w.group}${w.name ? ' · ' + w.name : ''}</span><b class="mono">${known ? Math.round(w.remaining_percent) + t("usage.page.restant") : '—'}</b></div>
        ${known && html`<div class="meter"><i style=${`width:${used}%;background:${used > 85 ? 'var(--amber)' : 'var(--text)'}`}></i></div>`}
        <small class="muted">${t("usage.page.reset")} ${when(w.reset_at)}</small></div>`; })
      : html`<p class="note">${readable ? (q.fetched_at ? t("usage.page.le_compte_ne_communique_pas_de_quota") : t("usage.page.lis_ton_compte_pour_voir_quotas_et_resets_aucune_generation_aucun")) : t("usage.page.lecture_des_quotas_pas_encore_integree_pour_ce_harness")}</p>`}
    ${q.credits != null && html`<div class="kv"><span>${t("usage.page.credits_ia")}</span><span class="mono">${q.credits}</span></div>`}
    ${resets != null ? html`<div class="kv"><span>${t("usage.page.resets_disponibles")}</span><span class="mono">${resets}</span></div>` : ''}
  </div>`;
}

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
function NativeRow({ h, rt, onRefresh }) {
  const [busy, setBusy] = useState(false);
  const refresh = async () => { setBusy(true); try { await onRefresh(h.runtime_id); } finally { setBusy(false); } };
  const models = (h.by_model || []).slice().sort((a, b) => b.tokens - a.tokens);
  const top = models.slice(0, 3), total = models.reduce((n, m) => n + m.tokens, 0) || 1;
  const machine = rt && rt.machine ? rt.machine : '';
  return html`<div class="tr">
    <span class="cell-id"><${Logo} name=${(rt && rt.logo) || h.runtime_id} size="sm" /><span class="cell-main"><b>${(rt && rt.name) || h.runtime_id}${machine && html` <small class="q-m">${machine}</small>`}</b><small>${tSource((h.source || '').replace(' · SSH machine', ''))}</small></span></span>
    <span class="mono">${h.sessions || '—'}</span>
    <span class="mono">${fmtTok(h.input_tokens)}</span><span class="mono">${fmtTok(h.output_tokens)}</span><span class="mono">${h.cache_read_tokens ? fmtTok(h.cache_read_tokens) : '—'}</span>
    <span class="mono strong">${fmtTok(h.total_tokens)}</span>
    <span class="nm">${top.length ? top.map(m => html`<span class="nm-r" title=${t("usage.native.model_tokens", { model: m.model, tokens: fmtTok(m.tokens) })}><i style=${`width:${Math.max(4, Math.round(m.tokens / total * 100))}%`}></i><em>${m.model}</em></span>`) : html`<span class="muted">—</span>`}${models.length > 3 ? html`<small class="muted">+${models.length - 3}</small>` : ''}</span>
    <span class="num">${h.cost_usd != null ? html`<span title=${t("usage.page.cout_declare_par_le_harness")}>${Number(h.cost_usd).toLocaleString(locale(), { style: 'currency', currency: 'USD', maximumFractionDigits: 2 })}</span>` : html`<span class="muted" title=${t("usage.native.no_cost")}>—</span>`}
      <button class="icon-btn" aria-label=${t("usage.native.reread")} title=${t("usage.native.reread")} disabled=${busy} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}</button></span>
    ${h.error && html`<p class="note err nat-err">${h.error}</p>`}
  </div>`;
}

function NativeUsage({ runtimes }) {
  const [days, setDays] = useState(7);
  const [rows, setRows] = useState(null);
  const load = async n => { setRows(null); try { const r = await get('/api/usage/native?days=' + n); setRows(r.harnesses || []); } catch (e) { setRows([]); toast(e.message, 'err'); } };
  useEffect(() => { load(days); }, [days]);
  const refresh = async id => {
    const r = await post('/api/usage/native/refresh', { runtime_id: id, days }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return toast(r.error, 'err');
    setRows(list => list.map(h => h.runtime_id === id ? r.harness : h));
  };
  const known = (rows || []).filter(h => runtimes.some(r => r.id === h.runtime_id));
  const active = h => h.sessions || h.total_tokens || h.error;
  const shown = known.filter(h => !quiet(h.error) && active(h));
  const idle = known.filter(h => !quiet(h.error) && !active(h));
  const absent = known.filter(h => quiet(h.error));
  const name = id => (runtimes.find(r => r.id === id) || {}).name || id;
  return html`<section class="sec"><div class="sec-h"><h2>${t("usage.native.title")}<${Tip} text=${t("usage.native.tip")} /></h2>
      <${Seg} size="sm" label=${t("usage.native.period")} value=${days} onChange=${setDays} options=${[{ value: 7, label: t("usage.native.days", { n: 7 }) }, { value: 30, label: t("usage.native.days", { n: 30 }) }]} /></div>
    ${rows === null ? html`<div class="skeleton" style="height:160px"></div>` : shown.length ? html`<div class="card table native-t">
      <div class="tr th"><span>${t("usage.native.harness")}</span><span>${t("usage.native.sessions")}</span><span>${t("usage.page.entree_2")}</span><span>${t("usage.page.sortie_2")}</span><span>${t("usage.native.cache")}</span><span>${t("usage.native.total")}</span><span>${t("usage.native.models")}</span><span>${t("usage.native.cost")}</span></div>
      ${shown.map(h => html`<${NativeRow} key=${h.runtime_id} h=${h} rt=${runtimes.find(r => r.id === h.runtime_id)} onRefresh=${refresh} />`)}</div>`
      : html`<div class="card"><${Empty} icon="chart" title=${t("usage.native.empty_title")} text=${t("usage.native.empty_text")} /></div>`}
    ${idle.length ? html`<p class="note">${t("usage.native.idle", { list: idle.map(h => name(h.runtime_id)).join(', ') })}</p>` : ''}
    ${absent.length ? html`<p class="note">${t("usage.native.absent", { list: absent.map(h => name(h.runtime_id) + (/not installed/i.test(h.error) ? t("usage.native.not_installed") : '')).join(', ') })}</p>` : ''}
  </section>`;
}

// Soldes des fournisseurs cloud qui les exposent (OpenRouter, DeepSeek…),
// lus avec leur propre clé, sans génération. Un montant absent reste « — ».
function Balances() {
  const [rows, setRows] = useState(null);
  const load = () => get('/api/usage/providers').then(r => setRows(r.providers || [])).catch(() => setRows([]));
  useEffect(() => { load(); }, []);
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
  const [d, setD] = useState(null);
  const load = async () => { try { const r = await get('/api/usage'); setD(r); } catch (_) {} };
  useEffect(() => { load(); }, []);
  const quotas = (d ? d.quotas || [] : []).filter(q => { const rt = runtimes.find(r => r.id === q.runtime_id) || {}; return rt.available !== false && (rt.capabilities || []).includes('quota'); });
  const [reading, setReading] = useState(false);
  const readAll = async () => {
    setReading(true);
    await Promise.all(quotas.map(q => post('/api/runtimes/' + encodeURIComponent(q.runtime_id) + '/quota', {}).catch(() => null)));
    setReading(false); load();
  };
  const models = d ? d.models || [] : [];
  const fmtMoney = (v, cur) => Number(v).toLocaleString(locale(), { style: 'currency', currency: cur || 'USD', maximumFractionDigits: 3 });
  const money = m => m.estimated_cost != null ? fmtMoney(m.estimated_cost, m.price && m.price.currency) : '—';
  const cost = m => m.reported_cost != null ? html`<span title="${t("usage.page.cout_declare_par_le_harness")}">${fmtMoney(m.reported_cost, m.currency)}</span>` : m.runtime_id ? '—' : money(m);
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${t("usage.page.usage")}</h1><p>${t("usage.page.quotas_de_tes_abonnements_et_consommation_dans_loom_une_donnee_ab")}</p></div></div>
    ${!d ? html`<div class="skeleton" style="height:200px"></div>` : html`
      <section class="sec"><div class="sec-h"><h2>${t("usage.page.abonnements")}</h2>${quotas.length > 1 && html`<button class="btn sm" disabled=${reading} onClick=${readAll}>${reading ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`} ${t("usage.page.read_all")}</button>`}</div>
        <div class="grid3 stagger">${quotas.map(q => html`<${Quota} key=${q.runtime_id} q=${q} rt=${runtimes.find(r => r.id === q.runtime_id)} onRefresh=${load} />`)}</div></section>
      <${Balances} />
      <${NativeUsage} runtimes=${runtimes} />
      <section class="sec"><div class="sec-h"><h2>${t("usage.page.in_loom")}<${Tip} text="${t("usage.page.tokens_rapportes_par_les_runtimes_dans_tes_discussions_loom_le_co")}" /></h2></div>
        ${models.length ? html`<div class="card table usage-t">
          <div class="tr th"><span>${t("usage.page.modele")}</span><span>${t("usage.page.entree_2")}</span><span>${t("usage.page.sortie_2")}</span><span>${t("usage.page.prix_m_tokens")}</span><span>${t("usage.page.estimation")}</span></div>
          ${models.map(m => html`<div class="tr"><span class="cell-id"><${Logo} name=${m.runtime_id || m.provider} size="sm" /><span class="cell-main"><b>${m.name && m.name !== 'default' ? m.name : t("usage.page.modele_par_defaut")}</b><small>${m.provider} · ${m.turns} ${t("usage.page.tour")}${m.turns > 1 ? 's' : ''}${m.reported_turns ? ' · ' + m.reported_turns + t("usage.page.avec_decompte") : ''}</small></span></span>
            <span class="mono">${m.reported_turns ? fmtTok(m.usage.prompt_tokens) : '—'}</span><span class="mono">${m.reported_turns ? fmtTok(m.usage.completion_tokens) : '—'}</span>
            <span>${m.runtime_id ? html`<span class="muted">${t("usage.page.abonnement")}</span>` : html`<${Price} m=${m} onSaved=${load} />`}</span><span class="num strong">${cost(m)}</span></div>`)}</div>`
          : html`<div class="card"><${Empty} icon="chart" title="${t("usage.page.rien_pour_l_instant")}" text="${t("usage.page.les_tokens_des_discussions_cloud_et_harness_apparaitront_ici_les")}" /></div>`}
      </section>`}
  </div></div>`;
}
