// Usage : quotas des abonnements (lecture explicite, sans génération) et
// tokens/coûts estimés des discussions. Une donnée absente reste « inconnue ».
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip, Seg } from '../../ui/controls.js';
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
  const resets = typeof q.reset_credits === 'number' ? q.reset_credits : q.reset_credits && q.reset_credits.available_count != null ? q.reset_credits.available_count : null;
  return html`<div class="card pad quota">
    <div class="q-h"><b>${(rt && rt.name) || q.name || q.runtime_id}</b>${rt && rt.machine && html`<small class="q-m">${rt.machine}</small>`}<span class="muted">${q.fetched_at ? ago(q.fetched_at) : ''}</span>
      ${readable && html`<button class="icon-btn" aria-label="Lire le compte" title="Lire le compte (sans génération)" disabled=${busy} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}</button>`}</div>
    ${q.error && html`<p class="note err">${q.error}</p>`}
    ${(q.windows || []).length ? q.windows.map(w => { const known = typeof w.remaining_percent === 'number'; const used = known ? 100 - Math.max(0, Math.min(100, w.remaining_percent)) : 0;
      return html`<div class="qw"><div class="qw-h"><span>${w.group}${w.name ? ' · ' + w.name : ''}</span><b class="mono">${known ? Math.round(w.remaining_percent) + ' % restant' : '—'}</b></div>
        ${known && html`<div class="meter"><i style=${`width:${used}%;background:${used > 85 ? 'var(--amber)' : 'var(--text)'}`}></i></div>`}
        <small class="muted">reset ${when(w.reset_at)}</small></div>`; })
      : html`<p class="note">${readable ? (q.fetched_at ? 'Le compte ne communique pas de quota.' : 'Lis ton compte pour voir quotas et resets. Aucune génération, aucun reset consommé.') : 'Lecture des quotas pas encore intégrée pour ce harness.'}</p>`}
    ${q.credits != null && html`<div class="kv"><span>Crédits IA</span><span class="mono">${q.credits}</span></div>`}
    ${resets != null ? html`<div class="kv"><span>Resets disponibles</span><span class="mono">${resets}</span></div>` : ''}
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

// Usage natif : toutes les sessions du harness, dans Loom et en dehors, lues
// dans ses journaux ou ses statistiques. Aucun prix n'est reconstruit.
const quiet = e => /non disponible|non installé/.test(e || '');
function NativeRow({ h, rt, onRefresh }) {
  const [busy, setBusy] = useState(false);
  const refresh = async () => { setBusy(true); try { await onRefresh(h.runtime_id); } finally { setBusy(false); } };
  const models = (h.by_model || []).slice().sort((a, b) => b.tokens - a.tokens);
  const top = models.slice(0, 3), total = models.reduce((n, m) => n + m.tokens, 0) || 1;
  const machine = rt && rt.machine ? rt.machine : '';
  return html`<div class="tr">
    <span class="cell-id"><${Logo} name=${(rt && rt.logo) || h.runtime_id} size="sm" /><span class="cell-main"><b>${(rt && rt.name) || h.runtime_id}${machine && html` <small class="q-m">${machine}</small>`}</b><small>${(h.source || '').replace(' · machine SSH', '')}</small></span></span>
    <span class="mono">${h.sessions || '—'}</span>
    <span class="mono">${fmtTok(h.input_tokens)}</span><span class="mono">${fmtTok(h.output_tokens)}</span><span class="mono">${h.cache_read_tokens ? fmtTok(h.cache_read_tokens) : '—'}</span>
    <span class="mono strong">${fmtTok(h.total_tokens)}</span>
    <span class="nm">${top.length ? top.map(m => html`<span class="nm-r" title=${m.model + ' · ' + fmtTok(m.tokens) + ' tokens'}><i style=${`width:${Math.max(4, Math.round(m.tokens / total * 100))}%`}></i><em>${m.model}</em></span>`) : html`<span class="muted">—</span>`}${models.length > 3 ? html`<small class="muted">+${models.length - 3}</small>` : ''}</span>
    <span class="num">${h.cost_usd != null ? html`<span title="Coût déclaré par le harness">${Number(h.cost_usd).toLocaleString('fr-FR', { style: 'currency', currency: 'USD', maximumFractionDigits: 2 })}</span>` : html`<span class="muted" title="Le harness ne communique pas de coût">—</span>`}
      <button class="icon-btn" aria-label="Relire" title="Relire" disabled=${busy} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}</button></span>
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
  const shown = known.filter(h => !quiet(h.error) && (h.sessions || h.total_tokens || h.error));
  const idle = known.filter(h => !quiet(h.error) && !(h.sessions || h.total_tokens || h.error));
  const absent = known.filter(h => quiet(h.error));
  const name = id => (runtimes.find(r => r.id === id) || {}).name || id;
  return html`<section class="sec"><div class="sec-h"><h2>Usage des harnesses<${Tip} text="Toutes les sessions de chaque harness, dans Loom et en dehors, lues dans ses propres journaux ou statistiques. Seuls les nombres et les noms de modèles sont lus, jamais le contenu. Le coût n’apparaît que si le harness le déclare." /></h2>
      <${Seg} size="sm" label="Période" value=${days} onChange=${setDays} options=${[{ value: 7, label: '7 jours' }, { value: 30, label: '30 jours' }]} /></div>
    ${rows === null ? html`<div class="skeleton" style="height:160px"></div>` : shown.length ? html`<div class="card table native-t">
      <div class="tr th"><span>Harness</span><span>Sessions</span><span>Entrée</span><span>Sortie</span><span>Cache</span><span>Total</span><span>Modèles</span><span>Coût</span></div>
      ${shown.map(h => html`<${NativeRow} key=${h.runtime_id} h=${h} rt=${runtimes.find(r => r.id === h.runtime_id)} onRefresh=${refresh} />`)}</div>`
      : html`<div class="card"><${Empty} icon="chart" title="Aucun usage lisible" text="Aucun harness installé ne fournit de statistiques natives sur cette période." /></div>`}
    ${idle.length ? html`<p class="note">Aucune session sur la période pour ${idle.map(h => name(h.runtime_id)).join(', ')}.</p>` : ''}
    ${absent.length ? html`<p class="note">Pas de source native pour ${absent.map(h => name(h.runtime_id) + (/installé/.test(h.error) ? ' (non installé)' : '')).join(', ')}.</p>` : ''}
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
  const fmtMoney = (v, cur) => Number(v).toLocaleString('fr-FR', { style: 'currency', currency: cur || 'USD', maximumFractionDigits: 3 });
  const money = m => m.estimated_cost != null ? fmtMoney(m.estimated_cost, m.price && m.price.currency) : '—';
  const cost = m => m.reported_cost != null ? html`<span title="Coût déclaré par le harness">${fmtMoney(m.reported_cost, m.currency)}</span>` : m.runtime_id ? '—' : money(m);
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>Usage</h1><p>Quotas de tes abonnements, usage de chaque harness et consommation dans Loom. Une donnée absente reste inconnue, jamais zéro.</p></div></div>
    ${!d ? html`<div class="skeleton" style="height:200px"></div>` : html`
      <section class="sec"><div class="sec-h"><h2>Abonnements</h2>${quotas.length > 1 && html`<button class="btn sm" disabled=${reading} onClick=${readAll}>${reading ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`} Tout lire</button>`}</div>
        <div class="grid3 stagger">${quotas.map(q => html`<${Quota} key=${q.runtime_id} q=${q} rt=${runtimes.find(r => r.id === q.runtime_id)} onRefresh=${load} />`)}</div></section>
      <${NativeUsage} runtimes=${runtimes} />
      <section class="sec"><div class="sec-h"><h2>Dans Loom<${Tip} text="Tokens rapportés par les runtimes dans tes discussions Loom. Le coût API est une estimation à partir du prix que tu saisis, pas une facture. Pour un harness, c’est le coût qu’il déclare lui-même." /></h2></div>
        ${models.length ? html`<div class="card table usage-t">
          <div class="tr th"><span>Modèle</span><span>Entrée</span><span>Sortie</span><span>Prix / M tokens</span><span>Estimation</span></div>
          ${models.map(m => html`<div class="tr"><span class="cell-id"><${Logo} name=${m.runtime_id || m.provider} size="sm" /><span class="cell-main"><b>${m.name && m.name !== 'default' ? m.name : 'Modèle par défaut'}</b><small>${m.provider} · ${m.turns} tour${m.turns > 1 ? 's' : ''}${m.reported_turns ? ' · ' + m.reported_turns + ' avec décompte' : ''}</small></span></span>
            <span class="mono">${m.reported_turns ? fmtTok(m.usage.prompt_tokens) : '—'}</span><span class="mono">${m.reported_turns ? fmtTok(m.usage.completion_tokens) : '—'}</span>
            <span>${m.runtime_id ? html`<span class="muted">abonnement</span>` : html`<${Price} m=${m} onSaved=${load} />`}</span><span class="num strong">${cost(m)}</span></div>`)}</div>`
          : html`<div class="card"><${Empty} icon="chart" title="Rien pour l’instant" text="Les tokens des discussions cloud et harness apparaîtront ici. Les modèles locaux n’ont pas de coût API." /></div>`}
      </section>`}
  </div></div>`;
}
