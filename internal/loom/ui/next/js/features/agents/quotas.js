// Quotas des abonnements, relevés par Codeman : Claude (fenêtres de 5 heures
// et de 7 jours, lues dans la statusline de ses sessions) et Codex. Loom garde
// l'historique des mesures pour en montrer l'évolution, et les tokens comptés
// par Codeman jour par jour.
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Seg, Tip } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { get } from '../../core/api.js';
import { visibleRefresh } from '../../core/poll.js';
import { SectionTabs } from '../../app/sections.js';
import { agents } from './store.js';
import { api } from './client.js';

const H = 3600e3, DAY = 24 * H;
const SPAN = { '5h': 5 * H, '7d': 7 * DAY };

function fmtIn(ms) {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m >= 1440) return t('quotas.in_dh', { d: Math.floor(m / 1440), h: Math.floor((m % 1440) / 60) });
  if (m >= 60) return t('quotas.in_hm', { h: Math.floor(m / 60), m: m % 60 });
  return t('quotas.in_m', { m });
}
const clock = ms => new Date(ms).toLocaleString(locale(), Date.now() + DAY > ms ? { timeStyle: 'short' } : { weekday: 'short', hour: '2-digit', minute: '2-digit' });
const tone = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : 'ok');

// Une fenêtre : part consommée, remise à zéro, et rythme (la part attendue si
// la consommation était régulière sur la fenêtre).
function Window({ name, w }) {
  const now = Date.now();
  const span = SPAN[name];
  const used = Math.round(w.used);
  const left = w.reset_at ? w.reset_at - now : 0;
  const expected = span && w.reset_at && left > 0 && left <= span ? Math.round((1 - left / span) * 100) : null;
  const pace = expected == null ? '' : used - expected > 8 ? 'ahead' : expected - used > 8 ? 'under' : 'even';
  return html`<div class=${cls('q-win', 'q-' + tone(used))}>
    <div class="q-win-h"><span>${name === '5h' ? t('quotas.win_5h') : t('quotas.win_7d')}</span><b>${used}<small>%</small></b></div>
    <div class="q-bar"><i style=${`width:${Math.min(100, used)}%`}></i>${expected != null && html`<u style=${`left:${expected}%`} title=${t('quotas.expected', { v: expected })}></u>`}</div>
    <div class="q-win-f muted">
      ${w.reset_at ? html`<span>${left > 0 ? t('quotas.resets_in', { in: fmtIn(left), at: clock(w.reset_at) }) : t('quotas.reset_done')}</span>` : html`<span></span>`}
      ${pace && html`<span class=${'q-pace ' + pace}>${pace === 'ahead' ? t('quotas.pace_ahead') : pace === 'under' ? t('quotas.pace_under') : t('quotas.pace_even')}</span>`}
    </div>
  </div>`;
}

function Account({ logo, title, windows, sub }) {
  const names = ['5h', '7d'].filter(n => windows && windows[n]);
  return html`<div class="card pad q-acc">
    <div class="q-acc-h"><${Logo} id=${logo} size="sm" /><b>${title}</b><span class="muted">${sub}</span></div>
    ${names.length ? names.map(n => html`<${Window} key=${n} name=${n} w=${windows[n]} />`) : html`<p class="muted q-none">${t('quotas.no_data')}</p>`}
  </div>`;
}

// Courbes de l'historique : une ligne par fenêtre, de 0 à 100 %.
const SERIES = [['claude', '5h', 'q-s1'], ['claude', '7d', 'q-s2'], ['codex', '5h', 'q-s3'], ['codex', '7d', 'q-s4']];
function History({ samples, range }) {
  // Le dessin suit la largeur réelle : le texte des axes garde sa taille.
  const box = useRef();
  const [W, setW] = useState(720);
  useEffect(() => {
    const el = box.current; if (!el) return;
    const ro = new ResizeObserver(() => setW(Math.max(240, Math.round(el.clientWidth) - 48)));
    ro.observe(el); return () => ro.disconnect();
  }, []);
  const now = Date.now(), from = now - range;
  const pts = samples.filter(s => s.at >= from);
  const Hh = 180;
  const x = at => ((at - from) / range) * W, y = v => Hh - (v / 100) * Hh;
  const lines = SERIES.map(([acc, win, klass]) => {
    const p = pts.filter(s => s[acc] && s[acc][win]).map(s => [x(s.at), y(s[acc][win].used)]);
    return p.length ? { klass, label: (acc === 'claude' ? 'Claude' : 'Codex') + ' · ' + (win === '5h' ? t('quotas.w5h') : t('quotas.w7d')), d: p.map((q, i) => (i ? 'L' : 'M') + q[0].toFixed(1) + ' ' + q[1].toFixed(1)).join(' ') + (p.length === 1 ? ' l1 0' : '') } : null;
  }).filter(Boolean);
  if (!lines.length) return html`<p class="muted q-none" ref=${box}>${t('quotas.history_empty')}</p>`;
  const every = Math.max(1, Math.ceil((range <= DAY ? 5 : range / DAY + 1) / Math.max(2, Math.floor(W / 90))));
  const ticks = (range <= DAY ? [0, 6, 12, 18, 24].map(h => from + h * H) : Array.from({ length: Math.round(range / DAY) + 1 }, (_, i) => from + i * DAY)).filter((_, i) => i % every === 0);
  return html`<div class="q-chart" ref=${box}>
    <svg viewBox=${`-40 -8 ${W + 48} ${Hh + 30}`} style=${`height:${Hh + 30}px`} role="img" aria-label=${t('quotas.history')}>
      ${[0, 25, 50, 75, 100].map(v => html`<g key=${v}><line class="q-grid" x1="0" x2=${W} y1=${y(v)} y2=${y(v)} /><text class="q-ax" x="-8" y=${y(v) + 3.5} text-anchor="end">${v}%</text></g>`)}
      ${ticks.map(tk => html`<text key=${tk} class="q-ax" x=${x(tk)} y=${Hh + 18} text-anchor="middle">${new Date(tk).toLocaleString(locale(), range <= DAY ? { hour: '2-digit', minute: '2-digit' } : { day: 'numeric', month: 'short' })}</text>`)}
      ${lines.map(l => html`<path key=${l.klass} class=${'q-line ' + l.klass} d=${l.d}/>`)}
    </svg>
    <div class="q-legend">${lines.map(l => html`<span key=${l.klass}><i class=${l.klass}></i>${l.label}</span>`)}</div>
  </div>`;
}

function Tokens({ stats }) {
  const days = (stats?.daily || []).slice().sort((a, b) => (a.date < b.date ? 1 : -1)).slice(0, 14);
  if (!days.length) return html`<p class="muted q-none">${t('quotas.tokens_empty')}</p>`;
  const max = Math.max(...days.map(d => (d.inputTokens || 0) + (d.outputTokens || 0)), 1);
  const money = v => Number(v || 0).toLocaleString(locale(), { style: 'currency', currency: 'USD', maximumFractionDigits: 2 });
  return html`<div class="card table q-tokens">
    <div class="tr th"><span>${t('quotas.day')}</span><span>${t('usage.page.entree_2')}</span><span>${t('usage.page.sortie_2')}</span><span>${t('quotas.sessions')}</span><span>${t('quotas.cost')}<${Tip} text=${t('quotas.cost_tip')} /></span></div>
    ${days.map(d => { const total = (d.inputTokens || 0) + (d.outputTokens || 0); return html`<div class="tr" key=${d.date}>
      <span class="q-day"><b>${new Date(d.date + 'T12:00:00').toLocaleDateString(locale(), { weekday: 'short', day: 'numeric', month: 'short' })}</b><i class="q-mini"><b style=${`width:${(total / max) * 100}%`}></b></i></span>
      <span class="mono" data-label=${t('usage.page.entree_2')}>${fmtTok(d.inputTokens || 0)}</span><span class="mono" data-label=${t('usage.page.sortie_2')}>${fmtTok(d.outputTokens || 0)}</span>
      <span class="mono" data-label=${t('quotas.sessions')}>${d.sessions ?? '—'}</span><span class="num strong" data-label=${t('quotas.cost')}>${money(d.estimatedCost)}</span></div>`; })}
  </div>`;
}

export function QuotasPage() {
  const conn = useStore(agents, s => s.conn);
  const live = useStore(agents, s => s.planUsage);
  const [data, setData] = useState(null);
  const [stats, setStats] = useState(null);
  const [range, setRange] = useState(() => { try { return +localStorage.getItem('loom.quotas.range') || DAY; } catch (_) { return DAY; } });
  const ready = conn.state === 'ready';
  useEffect(() => visibleRefresh(async alive => {
    const r = await get('/api/codeman/quotas', { retryAuth: false, timeout: 15000 }).catch(() => null);
    if (alive() && r && r.ok !== false) setData(r);
    if (ready) { const s = await api.tokenStats().catch(() => null); if (alive() && s) setStats(s); }
  }, 60000), [ready]);
  // La mesure en direct (flux Codeman) passe avant la dernière relevée par Loom.
  const fromLive = w => (w && Number.isFinite(w.usedPercentage) ? { used: w.usedPercentage, reset_at: w.resetAt } : null);
  const pick = (acc, win) => {
    const src = acc === 'claude' ? live : live?.codex;
    const v = fromLive(src && src[win === '5h' ? 'fiveHour' : 'sevenDay']);
    return v || data?.latest?.[acc]?.[win] || null;
  };
  const claude = { '5h': pick('claude', '5h'), '7d': pick('claude', '7d') };
  const codex = { '5h': pick('codex', '5h'), '7d': pick('codex', '7d') };
  const has = o => o['5h'] || o['7d'];
  const setR = v => { setRange(v); try { localStorage.setItem('loom.quotas.range', String(v)); } catch (_) {} };
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} />
    <div class="page-head"><div><h1>${t('quotas.title')}</h1><p>${t('quotas.lead')}</p></div></div>
    ${!conn.configured ? html`<div class="card"><${Empty} icon="gauge" title=${t('quotas.connect_title')} text=${t('quotas.connect_text')}><a class="btn primary" href="#/settings/agents"><${Icon} n="plug" />${t('quotas.connect')}</a></${Empty}></div>`
      : data === null && !live ? html`<div class="skeleton" style="height:220px"></div>`
      : html`
        ${!ready && html`<p class="note"><i class="dot amber"></i> ${t('quotas.offline')}</p>`}
        <div class="grid2 q-accs">
          <${Account} logo="claude" title="Claude" windows=${claude} sub=${live?.modelDisplayName || ''} />
          ${has(codex) ? html`<${Account} logo="openai" title="Codex" windows=${codex} sub="" />` : ''}
        </div>
        ${!has(claude) && !has(codex) && html`<p class="note">${t('quotas.no_data_note')}</p>`}
        <section class="sec"><div class="sec-h"><h2>${t('quotas.history')}<${Tip} text=${t('quotas.history_tip')} /></h2>
          <${Seg} size="sm" value=${range} onChange=${setR} label=${t('quotas.history')} options=${[{ value: DAY, label: t('quotas.r24h') }, { value: 7 * DAY, label: t('quotas.r7d') }, { value: 14 * DAY, label: t('quotas.r14d') }]} /></div>
          <div class="card pad"><${History} samples=${data?.samples || []} range=${range} /></div></section>
        <section class="sec"><div class="sec-h"><h2>${t('quotas.tokens')}<${Tip} text=${t('quotas.tokens_tip')} /></h2></div>
          <${Tokens} stats=${stats} /></section>`}
  </div></div>`;
}
