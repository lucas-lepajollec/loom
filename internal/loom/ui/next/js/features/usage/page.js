// Usage : quotas des abonnements (lecture explicite, sans génération) et
// tokens/coûts estimés des discussions. Une donnée absente reste « inconnue ».
import { html, useState, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';

const when = s => s ? new Date(s * 1000).toLocaleString('fr-FR', { dateStyle: 'medium', timeStyle: 'short' }) : 'non communiqué';
const ago = s => { if (!s) return ''; const m = Math.round((Date.now() / 1000 - s) / 60); return m < 1 ? 'à l’instant' : m < 60 ? 'il y a ' + m + ' min' : 'il y a ' + Math.round(m / 60) + ' h'; };

function Quota({ q, rt, onRefresh }) {
  const [busy, setBusy] = useState(false);
  const readable = rt && rt.implemented && (rt.capabilities || []).includes('quota');
  const refresh = async () => {
    setBusy(true);
    try { const r = await post('/api/runtimes/' + encodeURIComponent(q.runtime_id) + '/quota', {}); if (!r.ok) toast(r.error, 'err'); onRefresh(); }
    catch (e) { toast(e.message, 'err'); }
    finally { setBusy(false); }
  };
  return html`<div class="card pad quota">
    <div class="q-h"><b>${(rt && rt.name) || q.name || q.runtime_id}</b><span class="muted">${q.fetched_at ? ago(q.fetched_at) : ''}</span>
      ${readable && html`<button class="icon-btn" aria-label="Lire le compte" title="Lire le compte (sans génération)" disabled=${busy} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}</button>`}</div>
    ${q.error && html`<p class="note err">${q.error}</p>`}
    ${(q.windows || []).length ? q.windows.map(w => { const known = typeof w.remaining_percent === 'number'; const used = known ? 100 - Math.max(0, Math.min(100, w.remaining_percent)) : 0;
      return html`<div class="qw"><div class="qw-h"><span>${w.group}${w.name ? ' · ' + w.name : ''}</span><b class="mono">${known ? Math.round(w.remaining_percent) + ' % restant' : '—'}</b></div>
        ${known && html`<div class="meter"><i style=${`width:${used}%;background:${used > 85 ? 'var(--amber)' : 'var(--text)'}`}></i></div>`}
        <small class="muted">reset ${when(w.reset_at)}</small></div>`; })
      : html`<p class="note">${readable ? (q.fetched_at ? 'Le compte ne communique pas de quota.' : 'Lis ton compte pour voir quotas et resets. Aucune génération, aucun reset consommé.') : 'Lecture des quotas pas encore intégrée pour ce harness.'}</p>`}
    ${q.credits != null && html`<div class="kv"><span>Crédits IA</span><span class="mono">${q.credits}</span></div>`}
    ${q.reset_credits && q.reset_credits.available_count != null && html`<div class="kv"><span>Resets disponibles</span><span class="mono">${q.reset_credits.available_count}</span></div>`}
  </div>`;
}

function Price({ m, onSaved }) {
  const [open, setOpen] = useState(false);
  const [inp, setInp] = useState(m.price ? m.price.input_per_million : '');
  const [out, setOut] = useState(m.price ? m.price.output_per_million : '');
  const [cur, setCur] = useState(m.price ? m.price.currency : 'USD');
  const save = async () => { const r = await post('/api/usage/price', { choice_id: m.choice_id, input_per_million: +inp, output_per_million: +out, currency: cur }); if (!r.ok) return toast(r.error, 'err'); setOpen(false); onSaved(); };
  if (!open) return html`<button class="btn sm ghost" onClick=${() => setOpen(true)}>${m.price ? m.price.input_per_million + ' / ' + m.price.output_per_million + ' ' + m.price.currency : 'Saisir le prix'}</button>`;
  return html`<span class="price-edit"><input class="input sm num" placeholder="entrée" value=${inp} onInput=${e => setInp(e.target.value)} /><input class="input sm num" placeholder="sortie" value=${out} onInput=${e => setOut(e.target.value)} />
    <select class="select sm" style="min-width:74px" value=${cur} onChange=${e => setCur(e.target.value)}><option>USD</option><option>EUR</option></select><button class="btn sm primary" onClick=${save}>OK</button></span>`;
}

export function UsagePage() {
  const ws = useStore(app, s => s.workspace);
  const runtimes = (ws && ws.runtimes) || [];
  const [d, setD] = useState(null);
  const load = async () => { try { const r = await get('/api/usage'); setD(r); } catch (_) {} };
  useEffect(() => { load(); }, []);
  const quotas = d ? d.quotas || [] : [];
  const models = d ? d.models || [] : [];
  const money = m => m.estimated_cost != null ? Number(m.estimated_cost).toLocaleString('fr-FR', { style: 'currency', currency: (m.price && m.price.currency) || 'USD', maximumFractionDigits: 3 }) : '—';
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>Usage</h1><p>Quotas de tes abonnements et consommation dans Loom. Une donnée absente reste inconnue, jamais zéro.</p></div></div>
    ${!d ? html`<div class="skeleton" style="height:200px"></div>` : html`
      <section class="sec"><div class="sec-h"><h2>Abonnements</h2></div>
        <div class="grid3 stagger">${quotas.map(q => html`<${Quota} key=${q.runtime_id} q=${q} rt=${runtimes.find(r => r.id === q.runtime_id)} onRefresh=${load} />`)}</div></section>
      <section class="sec"><div class="sec-h"><h2>Tokens et coût estimé<${Tip} text="Tokens rapportés par les runtimes dans tes discussions Loom. Le coût API est une estimation à partir du prix que tu saisis, pas une facture." /></h2></div>
        ${models.length ? html`<div class="card table usage-t">
          <div class="tr th"><span>Modèle</span><span>Entrée</span><span>Sortie</span><span>Prix / M tokens</span><span>Estimation</span></div>
          ${models.map(m => html`<div class="tr"><span class="cell-main"><b>${m.name}</b><small>${m.provider} · ${m.reported_turns}/${m.turns} tours avec décompte</small></span>
            <span class="mono">${m.reported_turns ? fmtTok(m.usage.prompt_tokens) : '—'}</span><span class="mono">${m.reported_turns ? fmtTok(m.usage.completion_tokens) : '—'}</span>
            <span>${m.runtime_id ? html`<span class="muted">abonnement</span>` : html`<${Price} m=${m} onSaved=${load} />`}</span><span class="mono strong">${m.runtime_id ? '—' : money(m)}</span></div>`)}</div>`
          : html`<div class="card"><${Empty} icon="chart" title="Rien pour l’instant" text="Les tokens des discussions cloud et harness apparaîtront ici. Les modèles locaux n’ont pas de coût API." /></div>`}
      </section>`}
  </div></div>`;
}
