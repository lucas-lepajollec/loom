// Quotas des abonnements de chaque harness installé : Claude Code, Codex,
// Antigravity (lus par Loom auprès des CLI) et Copilot (relevé par Codeman).
// Loom garde l'historique des mesures et montre les réinitialisations
// offertes, qu'il n'utilise que sur demande explicite.
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Seg, Tip, Popover } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { get, post } from '../../core/api.js';
import { visibleRefresh } from '../../core/poll.js';
import { SectionTabs } from '../../app/sections.js';
import { agents, sessionList, stateOf } from './store.js';
import { openSession } from './nav.js';
import { setViewOf } from './layout.js';
import { toast, confirm } from '../../ui/dialog.js';
import { CliMark } from './new-session.js';
import { api, sendPrompt } from './client.js';
import { currentWindow, dailyUse } from './quota-math.js';

const H = 3600e3, DAY = 24 * H;
const SPAN = { '5h': 5 * H, '7d': 7 * DAY };
const ACCOUNTS = { claude: ['Claude', 'claude'], codex: ['Codex', 'openai'], antigravity: ['Antigravity', 'google'], copilot: ['Copilot', ''] };
const accName = id => (ACCOUNTS[id] || [id])[0];

function fmtIn(ms) {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m >= 1440) return t('quotas.in_dh', { d: Math.floor(m / 1440), h: Math.floor((m % 1440) / 60) });
  if (m >= 60) return t('quotas.in_hm', { h: Math.floor(m / 60), m: m % 60 });
  return t('quotas.in_m', { m });
}
const clock = ms => new Date(ms).toLocaleString(locale(), Date.now() + DAY > ms ? { timeStyle: 'short' } : { weekday: 'short', hour: '2-digit', minute: '2-digit' });
const day = ms => new Date(ms).toLocaleDateString(locale(), { day: 'numeric', month: 'short' });
const tone = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : 'ok');

// Nom d'une limite : fenêtre connue (5 h, semaine, mois), précisée par le
// modèle quand le CLI la découpe (Claude : Opus, Sonnet), sinon son libellé.
function limitName(l) {
  const base = { '5h': t('quotas.win_5h'), '7d': t('quotas.win_7d'), month: t('quotas.win_month') }[l.window];
  if (!base) return l.label || l.id;
  return l.label ? base + ' · ' + l.label : base;
}

// Une limite : part consommée, remise à zéro, et rythme (la part attendue si
// la consommation était régulière sur la fenêtre).
function Window({ l }) {
  const now = Date.now();
  const span = l.label ? 0 : SPAN[l.window];
  const used = Math.round(l.used);
  const left = l.reset_at ? l.reset_at - now : 0;
  const expected = span && l.reset_at && left > 0 && left <= span ? Math.round((1 - left / span) * 100) : null;
  const pace = expected == null ? '' : used - expected > 8 ? 'ahead' : expected - used > 8 ? 'under' : 'even';
  return html`<div class=${cls('q-win', 'q-' + tone(used))}>
    <div class="q-win-h"><span>${limitName(l)}</span><b>${used}<small>%</small></b></div>
    <div class="q-bar"><i style=${`width:${Math.min(100, used)}%`}></i>${expected != null && html`<u style=${`left:${expected}%`} title=${t('quotas.expected', { v: expected })}></u>`}</div>
    <div class="q-win-f muted">
      ${l.reset_at ? html`<span>${left > 0 ? t(used >= 100 ? 'quotas.back_in' : 'quotas.resets_in', { in: fmtIn(left), at: clock(l.reset_at) }) : t('quotas.reset_done')}</span>` : html`<span></span>`}
      ${pace && html`<span class=${'q-pace ' + pace}>${pace === 'ahead' ? t('quotas.pace_ahead') : pace === 'under' ? t('quotas.pace_under') : t('quotas.pace_even')}</span>`}
    </div>
  </div>`;
}

function Account({ a, sub }) {
  const [name, logo] = ACCOUNTS[a.id] || [a.id, ''];
  // Antigravity regroupe ses limites par famille de modèles.
  const groups = [], order = ['5h', '7d', 'month', 'other'];
  const limits = [...(a.limits || [])].sort((x, y) => order.indexOf(x.window) - order.indexOf(y.window));
  for (const l of limits) {
    let g = groups.find(x => x.name === (l.group || ''));
    if (!g) groups.push(g = { name: l.group || '', list: [] });
    g.list.push(l);
  }
  return html`<div class=${cls('card pad q-acc', groups.length > 1 && 'wide')}>
    <div class="q-acc-h"><${Logo} id=${logo} name=${name} size="sm" /><b>${name}</b><span class="muted">${sub}</span></div>
    ${!groups.length ? html`<p class="muted q-none">${t('quotas.no_data')}</p>`
      : html`<div class="q-groups">${groups.map(g => html`<div class="q-group" key=${g.name}>${g.name && html`<h3>${g.name}</h3>`}${g.list.map(l => html`<${Window} key=${l.id} l=${l} />`)}</div>`)}</div>`}
  </div>`;
}

// Claude Code n'offre ses réinitialisations que dans son menu /limit-reset :
// Loom l'ouvre dans une session Claude, en vue terminal, et la personne
// choisit et confirme dans le CLI.
const RANK = { limited: 0, idle: 1 };
async function openClaudeReset() {
  const s = sessionList().filter(x => x.mode === 'claude' && x.pid != null)
    .sort((a, b) => (RANK[stateOf(a)] ?? 2) - (RANK[stateOf(b)] ?? 2) || (b.lastActivityAt || 0) - (a.lastActivityAt || 0))[0];
  if (!s) return toast(t('quotas.reset_no_session'), 'err');
  try { setViewOf(s.id, 'term'); await sendPrompt(s.id, '/limit-reset'); openSession(s.id); toast(t('quotas.reset_opened', { name: s.name || s.id.slice(0, 8) })); }
  catch (e) { toast(e.message, 'err'); }
}

// Codex : Loom utilise le crédit choisi, après confirmation.
async function useCodexReset(c, done) {
  const ok = await confirm(t('quotas.reset_confirm_title'), t('quotas.reset_confirm_codex', { title: c.title || t('quotas.reset_full'), date: c.expires_at ? day(c.expires_at) : '—' }), { ok: t('quotas.reset_confirm_ok') });
  if (!ok) return;
  try {
    const r = await post('/api/quotas/reset', { account: 'codex', credit_id: c.id });
    if (r.ok === false) throw new Error(r.error || 'reset');
    const out = { reset: ['quotas.reset_out_reset', 'ok'], nothingToReset: ['quotas.reset_out_nothing', 'err'], noCredit: ['quotas.reset_out_none', 'err'], alreadyRedeemed: ['quotas.reset_out_reset', 'ok'] }[r.outcome] || ['quotas.reset_out_unknown', 'err'];
    toast(t(out[0]), out[1]);
  } catch (e) { toast(e.message, 'err'); }
  done();
}

// Une ligne par harness : combien de réinitialisations, de quelle nature, et
// comment s'en servir.
function Resets({ accounts, codeman, refresh }) {
  const rows = [];
  for (const a of accounts) {
    const r = a.resets, [name, logo] = ACCOUNTS[a.id] || [a.id, ''];
    const mark = html`<${Logo} id=${logo} name=${name} size="sm" />`;
    if (a.id === 'codex' && r) {
      if (!r.available) rows.push(html`<div class="q-rs-row" key="codex-none">${mark}<div class="q-rs-t"><b>${name}</b><span class="muted">${t('quotas.resets_none')}</span></div></div>`);
      (r.credits || []).forEach(c => rows.push(html`<div class="q-rs-row" key=${'codex-' + c.id}>${mark}
        <div class="q-rs-t"><b>${name} · ${c.title || t('quotas.reset_full')}</b><span class="muted">${t('quotas.reset_codex_what')}${c.expires_at ? ' · ' + t('quotas.reset_until', { date: day(c.expires_at) }) : ''}</span></div>
        <button class="btn sm primary" onClick=${() => useCodexReset(c, refresh)}>${t('quotas.reset_use_one')}</button></div>`));
      const extra = r.available - (r.credits || []).length;
      if (extra > 0) rows.push(html`<p class="muted q-rs-more" key="codex-more">${t('quotas.reset_more', { n: extra })}</p>`);
    } else if (a.id === 'claude' && r) {
      rows.push(r.available ? html`<div class="q-rs-row" key="claude">${mark}
        <div class="q-rs-t"><b>${name} · ${t('quotas.reset_claude_one')}</b><span class="muted">${t(r.stale ? 'quotas.reset_claude_stale' : 'quotas.reset_claude_seen', { date: day(r.checked_at) })} ${t('quotas.reset_claude_kind')}</span></div>
        ${codeman ? html`<button class="btn sm primary" onClick=${openClaudeReset}>${t('quotas.reset_open_claude')}</button>` : html`<code class="q-rs-cmd">/limit-reset</code>`}</div>`
        : html`<div class="q-rs-row" key="claude">${mark}<div class="q-rs-t"><b>${name}</b><span class="muted">${t('quotas.reset_claude_none')}</span></div></div>`);
    } else if (a.id === 'antigravity') {
      rows.push(html`<div class="q-rs-row" key="agy">${mark}<div class="q-rs-t"><b>${name}</b><span class="muted">${t('quotas.reset_agy', { n: a.credits ?? '—' })}</span></div></div>`);
    } else if (a.id === 'copilot') {
      rows.push(html`<div class="q-rs-row" key="copilot">${mark}<div class="q-rs-t"><b>${name}</b><span class="muted">${t('quotas.reset_copilot')}</span></div></div>`);
    }
  }
  if (!rows.length) return null;
  const total = accounts.reduce((n, a) => n + (a.resets?.available || 0), 0);
  return html`<section class="sec"><div class="sec-h"><h2>${t('quotas.resets')}${total ? html`<span class="q-rs-n">${total}</span>` : ''}<${Tip} text=${t('quotas.resets_tip')} /></h2></div>
    <div class="card q-rs">${rows}</div></section>`;
}

// Largeur réelle d'un conteneur : les dessins suivent la carte au pixel près,
// le texte reste en HTML et garde sa taille.
function useWidth() {
  const ref = useRef();
  const [w, setW] = useState(0);
  useEffect(() => {
    const el = ref.current; if (!el) return;
    const measure = () => setW(Math.round(el.clientWidth));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el); return () => ro.disconnect();
  }, []);
  return [ref, w];
}

// Fenêtre en cours d'une limite : consommation depuis son ouverture (trait
// plein), rythme régulier jusqu'à la remise à zéro (diagonale), et projection
// au rythme récent (pointillés), avec la conclusion en clair.
// Clé d'une limite dans les relevés : la fenêtre (5h, 7d, month) pour Claude,
// Codex et Copilot, l'identifiant de la limite pour Antigravity.
const sampleKey = (acc, l) => (acc === 'antigravity' ? l.id : l.label ? l.id : l.window);
// Pas de projection tant que la fenêtre a moins d'une demi-heure (ou d'un
// dixième de sa durée) : trop peu de mesures pour un rythme fiable.
const tooEarly = w => Date.now() - w.start < Math.max(30 * 60e3, (w.end - w.start) / 10);

function WindowChart({ acc, l, samples }) {
  const [ref, W] = useWidth();
  const now = Date.now();
  const w = currentWindow(samples, acc, sampleKey(acc, l), l, now);
  const name = (l.group ? l.group + ' · ' : '') + limitName(l);
  let verdict, warn = false;
  if (!w) verdict = l.used > 0 ? t('quotas.win_unknown') : t('quotas.win_closed');
  else if (w.used < 100 && tooEarly(w)) verdict = t('quotas.win_early', { at: clock(w.start) });
  else if (w.eta != null) { verdict = w.used >= 100 ? t('quotas.win_out', { at: clock(w.end) }) : t('quotas.win_eta', { at: clock(w.eta) }); warn = true; }
  else if (w.rate <= 0) verdict = t('quotas.win_flat', { at: clock(w.end) });
  else verdict = t('quotas.win_at_reset', { v: Math.round(w.atReset), at: clock(w.end) });
  const Hc = 96;
  let svg = null;
  if (w && W) {
    const x = at => ((at - w.start) / (w.end - w.start)) * W, y = v => Hc - (Math.min(100, Math.max(0, v)) / 100) * Hc;
    const line = w.pts.map((p, i) => (i ? 'L' : 'M') + x(p[0]).toFixed(1) + ' ' + y(p[1]).toFixed(1)).join(' ');
    const first = w.pts[0], last = w.pts[w.pts.length - 1];
    const area = line + ` L${x(last[0]).toFixed(1)} ${Hc} L${x(first[0]).toFixed(1)} ${Hc} Z`;
    const proj = w.eta != null ? [x(w.eta), y(100)] : [W, y(w.atReset)];
    svg = html`<svg width=${W} height=${Hc + 2} viewBox=${`0 -1 ${W} ${Hc + 2}`} aria-hidden="true">
      <line class="qw-grid" x1="0" x2=${W} y1=${y(100)} y2=${y(100)} /><line class="qw-grid" x1="0" x2=${W} y1=${y(50)} y2=${y(50)} /><line class="qw-base" x1="0" x2=${W} y1=${Hc} y2=${Hc} />
      <line class="qw-pace" x1="0" y1=${Hc} x2=${W} y2="0" />
      <path class="qw-area" d=${area} /><path class="qw-line" d=${line} />
      ${w.used < 100 && !tooEarly(w) && html`<line class=${cls('qw-proj', warn && 'warn')} x1=${x(now)} y1=${y(w.used)} x2=${proj[0]} y2=${proj[1]} />`}
      <line class="qw-now" x1=${x(now)} x2=${x(now)} y1="0" y2=${Hc} /><circle class="qw-dot" cx=${x(now)} cy=${y(w.used)} r="3.5" />
    </svg>`;
  }
  return html`<div class=${cls('qw', 'q-' + tone(l.used))}>
    <div class="qw-h"><span>${name}</span><b>${Math.round(l.used)}<small>%</small></b></div>
    <div class="qw-plot" ref=${ref}>${svg}</div>
    ${w && html`<div class="qw-axis muted"><span>${clock(w.start)}</span><span>${t('quotas.win_reset_at', { at: clock(w.end) })}</span></div>`}
    <p class=${cls('qw-verdict', warn && 'warn')}>${verdict}</p>
  </div>`;
}

// Part de la semaine (ou du mois) consommée chaque jour, sur 14 jours.
function DailyChart({ acc, l, samples }) {
  const days = dailyUse(samples, acc, sampleKey(acc, l), 14);
  const max = Math.max(10, ...days.map(d => d.used));
  const total = days.reduce((n, d) => n + d.used, 0);
  const name = (l.group ? l.group + ' · ' : '') + limitName(l);
  return html`<div class="qd">
    <div class="qd-h"><span>${t('quotas.daily_of', { name })}</span>${total > 0 && html`<span class="muted">${t('quotas.daily_avg', { v: Math.round((total / days.filter(d => d.used > 0).length) * 10) / 10 })}</span>`}</div>
    <div class="qd-bars" role="img" aria-label=${t('quotas.daily_of', { name })}>${days.map(d => {
      const label = new Date(d.day).toLocaleDateString(locale(), { weekday: 'short', day: 'numeric' });
      return html`<div class="qd-col" key=${d.day} title=${label + ' · ' + d.used + ' %'}>
        <small class="qd-v">${d.used >= 1 ? Math.round(d.used) : ''}</small>
        <i style=${`height:${(d.used / max) * 100}%`}></i>
        <small class="qd-d">${new Date(d.day).toLocaleDateString(locale(), { weekday: 'narrow' })}</small></div>`;
    })}</div>
  </div>`;
}

const CHARTED = ['5h', '7d', 'month'];
function Evolution({ acc, accounts, samples }) {
  const a = accounts.find(x => x.id === acc);
  const order = ['5h', '7d', 'month'];
  const limits = ((a && a.limits) || []).filter(l => CHARTED.includes(l.window)).sort((x, y) => (x.group || '').localeCompare(y.group || '') || order.indexOf(x.window) - order.indexOf(y.window));
  const long = limits.filter(l => l.window !== '5h');
  if (!limits.length) return html`<p class="muted q-none">${t('quotas.history_empty')}</p>`;
  return html`<div class="q-evo">
    <div class="q-evo-grid">${limits.map(l => html`<div class="card pad" key=${l.id}><${WindowChart} acc=${acc} l=${l} samples=${samples} /></div>`)}</div>
    ${long.map(l => html`<div class="card pad" key=${'d-' + l.id}><${DailyChart} acc=${acc} l=${l} samples=${samples} /></div>`)}
  </div>`;
}

// Sessions arrêtées sur une limite : Codeman les relance seul à la remise à
// zéro (reprise automatique, réglable par session dans Infos).
function Paused() {
  useStore(agents, s => [s.order, s.sessions]);
  const list = sessionList().filter(s => stateOf(s) === 'limited').sort((a, b) => a.autoResumeAt - b.autoResumeAt);
  if (!list.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t('quotas.paused')}<${Tip} text=${t('quotas.paused_tip')} /></h2></div>
    <div class="card q-paused">${list.map(s => html`<button key=${s.id} class="q-paused-row" onClick=${() => openSession(s.id)}>
      <${CliMark} mode=${s.mode} size="sm" /><b class="trunc grow">${s.name || s.id.slice(0, 8)}</b>
      <span class="muted">${t('quotas.resumes_at', { at: clock(s.autoResumeAt), in: fmtIn(s.autoResumeAt - Date.now()) })}</span></button>`)}</div></section>`;
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

// Aperçu des quotas dans la barre Agents : une jauge (la limite la plus
// entamée), le détail de chaque abonnement en un clic, la page complète
// dans Activité. Mêmes données que la page.
const WIN_ORDER = ['5h', '7d', 'month', 'other'];
const byWindow = list => [...list].sort((x, y) => (x.group || '').localeCompare(y.group || '') || WIN_ORDER.indexOf(x.window) - WIN_ORDER.indexOf(y.window));
let lastQuotas = null;
async function loadQuotas() {
  const r = await get('/api/quotas', { retryAuth: false, timeout: 45000 }).catch(() => null);
  if (r && r.ok !== false) lastQuotas = r;
  return lastQuotas;
}
export function QuotaPeek() {
  const live = useStore(agents, s => s.planUsage);
  const [data, setData] = useState(lastQuotas);
  const [anchor, setAnchor] = useState(null);
  useEffect(() => visibleRefresh(async alive => { const r = await loadQuotas(); if (alive() && r) setData(r); }, 120000), []);
  const accounts = withLive(data?.accounts || [], live).filter(a => (a.limits || []).length);
  if (!accounts.length) return null;
  const top = Math.round(Math.max(...accounts.flatMap(a => a.limits.map(l => l.used))));
  const warn = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : '');
  return html`<button class=${cls('icon-btn cm-quota-btn', anchor && 'on', warn(top))} aria-label=${t('quotas.chip')} title=${t('quotas.chip') + ' · ' + top + '%'}
      aria-expanded=${String(!!anchor)} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="gauge" /><i class="cm-quota-dot"></i></button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${() => setAnchor(null)} width=${300} class="cm-quota-pop">
      ${accounts.map(a => html`<div class="cm-qp-acc" key=${a.id}><b>${accName(a.id)}</b>
        ${byWindow(a.limits).map(l => html`<div class=${cls('cm-qp-win', warn(l.used))} key=${l.id}>
          <div class="cm-qp-h"><span>${(l.group ? l.group + ' · ' : '') + limitName(l)}</span><em>${Math.round(l.used)}%</em></div>
          <div class="cm-qp-bar"><i style=${`width:${Math.min(100, l.used)}%`}></i></div>
          ${l.reset_at && l.reset_at > Date.now() ? html`<small class="muted">${t('quotas.resets_in', { in: fmtIn(l.reset_at - Date.now()), at: clock(l.reset_at) })}</small>` : ''}
        </div>`)}</div>`)}
      <a class="cm-qp-more" href="#/quotas" onClick=${() => setAnchor(null)}>${t('quotas.open')}<${Icon} n="right" /></a>
    </${Popover}>`}`;
}

// La mesure en direct de Codeman (statusline d'une session Claude, Codex)
// passe avant le dernier relevé.
function withLive(accounts, live) {
  const out = accounts.map(a => ({ ...a, limits: [...(a.limits || [])] }));
  const fromLive = w => (w && Number.isFinite(w.usedPercentage) ? { used: w.usedPercentage, reset_at: w.resetAt } : null);
  const over = (id, src) => {
    if (!src) return;
    const w = { '5h': fromLive(src.fiveHour), '7d': fromLive(src.sevenDay) };
    if (!w['5h'] && !w['7d']) return;
    let a = out.find(x => x.id === id);
    if (!a) out.push(a = { id, source: 'codeman', limits: [] });
    a.limits = a.limits.map(l => (!l.label && w[l.window] ? { ...l, ...w[l.window] } : l));
    for (const k of ['5h', '7d']) if (w[k] && !a.limits.some(l => l.window === k && !l.label)) a.limits.push({ id: id + '-' + k, window: k, ...w[k] });
  };
  over('claude', live);
  over('codex', live?.codex);
  return out.sort((a, b) => Object.keys(ACCOUNTS).indexOf(a.id) - Object.keys(ACCOUNTS).indexOf(b.id));
}

const stored = (k, d) => { try { return localStorage.getItem(k) || d; } catch (_) { return d; } };
const store = (k, v) => { try { localStorage.setItem(k, String(v)); } catch (_) {} };

export function QuotasPage() {
  const conn = useStore(agents, s => s.conn);
  const live = useStore(agents, s => s.planUsage);
  const [data, setData] = useState(null);
  const [stats, setStats] = useState(null);
  const [tick, setTick] = useState(0);
  const [acc, setAcc] = useState(() => stored('loom.quotas.acc', 'claude'));
  const ready = conn.state === 'ready';
  useEffect(() => visibleRefresh(async alive => {
    const r = await loadQuotas();
    if (alive() && r) setData(r);
    if (ready) { const s = await api.tokenStats().catch(() => null); if (alive() && s) setStats(s); }
  }, 60000), [ready, tick]);
  const accounts = withLive(data?.accounts || [], live);
  const samples = data?.samples || [];
  const charted = Object.keys(ACCOUNTS).filter(id => accounts.some(a => a.id === id && (a.limits || []).some(l => CHARTED.includes(l.window))));
  const shown = charted.includes(acc) ? acc : charted[0];
  const sub = a => (a.id === 'claude' && live?.modelDisplayName) || (a.plan ? a.plan[0].toUpperCase() + a.plan.slice(1) : '');
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} />
    <div class="page-head"><div><h1>${t('quotas.title')}</h1><p>${t('quotas.lead')}</p></div></div>
    ${data === null && !live ? html`<div class="skeleton" style="height:220px"></div>`
      : !accounts.length ? html`<div class="card"><${Empty} icon="gauge" title=${t('quotas.empty_title')} text=${t('quotas.empty_text')}>${!conn.configured && html`<a class="btn" href="#/settings/agents"><${Icon} n="plug" />${t('quotas.connect')}</a>`}</${Empty}></div>`
      : html`
        ${conn.configured && !ready && html`<p class="note"><i class="dot amber"></i> ${t('quotas.offline')}</p>`}
        <${Resets} accounts=${accounts} codeman=${ready} refresh=${() => setTick(n => n + 1)} />
        <section class="sec"><div class="sec-h"><h2>${t('quotas.limits')}<${Tip} text=${t('quotas.limits_tip')} /></h2></div>
          <div class="grid2 q-accs">${accounts.map(a => html`<${Account} key=${a.id} a=${a} sub=${sub(a)} />`)}</div></section>
        ${conn.configured && html`<${Paused} />`}
        ${shown && html`<section class="sec"><div class="sec-h"><h2>${t('quotas.history')}<${Tip} text=${t('quotas.history_tip')} /></h2>
          ${charted.length > 1 && html`<div class="q-hist-ctl"><${Seg} size="sm" value=${shown} onChange=${v => { setAcc(v); store('loom.quotas.acc', v); }} label=${t('quotas.history_account')} options=${charted.map(id => ({ value: id, label: accName(id) }))} /></div>`}</div>
          <${Evolution} acc=${shown} accounts=${accounts} samples=${samples} /></section>`}
        ${ready && html`<section class="sec"><div class="sec-h"><h2>${t('quotas.tokens')}<${Tip} text=${t('quotas.tokens_tip')} /></h2></div>
          <${Tokens} stats=${stats} /></section>`}`}
  </div></div>`;
}
