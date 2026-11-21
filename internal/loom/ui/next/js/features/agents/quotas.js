// Quotas des abonnements de chaque harness installé : Claude Code, Codex,
// Antigravity (lus par Loom auprès des CLI) et Copilot (relevé par Codeman).
// Loom garde l'historique des mesures et montre les réinitialisations
// offertes, qu'il n'utilise que sur demande explicite.
import { t, locale } from '../../core/i18n.js';
import { ListPick } from '../../ui/listpick.js';
import { html, useState, useEffect, useRef, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { DropdownButton, Empty, Menu, Seg, Tip, Popover } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { get, post } from '../../core/api.js';
import { visibleRefresh } from '../../core/poll.js';
import { ActivityLayout } from '../usage/page.js';
import { agents, sessionList, stateOf } from './store.js';
import { openSession } from './nav.js';
import { setViewOf } from './layout.js';
import { toast, confirm } from '../../ui/dialog.js';
import { CliMark } from './new-session.js';
import { api, sendPrompt } from './client.js';
import { H, currentWindow, dailyUse } from './quota-math.js';

const DAY = 24 * H;
const SPAN = { '5h': 5 * H, '7d': 7 * DAY };
const CHARTED = ['5h', '7d', 'month'];
const WIN_ORDER = [...CHARTED, 'other'];
const ACCOUNTS = { claude: 'Claude', codex: 'Codex', antigravity: 'Antigravity', copilot: 'Copilot' };
const TILES = { codex: 'Cx', antigravity: 'Ag', copilot: 'Cp' };
const accName = id => ACCOUNTS[id] || id;

function fmtIn(ms) {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m >= 1440) return t('quotas.in_dh', { d: Math.floor(m / 1440), h: Math.floor((m % 1440) / 60) });
  if (m >= 60) return t('quotas.in_hm', { h: Math.floor(m / 60), m: m % 60 });
  return t('quotas.in_m', { m });
}
const clock = ms => new Date(ms).toLocaleString(locale(), Date.now() + DAY > ms ? { timeStyle: 'short' } : { weekday: 'short', hour: '2-digit', minute: '2-digit' });
const day = ms => new Date(ms).toLocaleDateString(locale(), { day: 'numeric', month: 'short' });
const tone = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : 'ok');
const percent = (v, decimals = 0) => (v / 100).toLocaleString(locale(), { style: 'percent', maximumFractionDigits: decimals });
const joinList = values => new Intl.ListFormat(locale()).format(values);

// Nom d'une limite : fenêtre connue (5 h, semaine, mois), précisée par le
// modèle quand le CLI la découpe (Claude : Opus, Sonnet), sinon son libellé.
function limitName(l, compact = false) {
  const base = {
    '5h': compact ? t('quotas.short_5h') : t('quotas.win_5h'),
    '7d': compact ? t('quotas.short_7d') : t('quotas.win_7d'),
    month: t('quotas.win_month'),
  }[l.window];
  if (!base) return l.label || l.id;
  return l.label ? base + ' · ' + l.label : base;
}

function AccountMark({ id }) {
  return id === 'claude'
    ? html`<${Logo} id="claude" size="xs" />`
    : html`<span class="logo-mark xs mono" aria-hidden="true">${TILES[id] || id.slice(0, 1).toUpperCase()}</span>`;
}

function Window({ acc, l, samples }) {
  const now = Date.now();
  const span = l.label ? 0 : SPAN[l.window];
  const used = Math.round(l.used);
  const left = l.reset_at ? l.reset_at - now : 0;
  const expected = span && l.reset_at && left > 0 && left <= span ? Math.round((1 - left / span) * 100) : null;
  const pace = expected == null ? '' : used - expected > 8 ? 'ahead' : expected - used > 8 ? 'under' : 'even';
  const verdict = { ahead: t('quotas.pace_ahead'), under: t('quotas.pace_under'), even: t('quotas.pace_even') }[pace];
  const name = [l.group, limitName(l, true)].filter(Boolean).join(' · ');
  const resetTime = left > 0 ? clock(l.reset_at) : '';
  const closed = !l.reset_at && l.used === 0 && CHARTED.includes(l.window);
  let caption = closed ? t('quotas.closed_short') : '';
  if (l.reset_at) caption = left <= 0 ? t('quotas.reset_done') : used >= 100
    ? t('quotas.back_in', { in: fmtIn(left), at: resetTime }) : t('quotas.reset_at_short', { at: resetTime });
  const w = currentWindow(samples, acc, sampleKey(acc, l), l, now);
  const forecast = windowProjection(w, now);
  const warning = forecast?.warning || pace === 'ahead';
  const color = tone(used) === 'red' ? 'red' : warning ? 'amber' : tone(used);
  const compact = forecast?.warning ? forecast.short : used < 100 ? resetTime : '';
  const resetTitle = resetTime && t('quotas.resets_in', { in: fmtIn(left), at: resetTime });
  if (forecast?.warning) caption = forecast.detail;
  return html`<div class=${cls('q-win', 'q-' + color, closed && 'closed')}>
    <div class="q-win-h"><span>${name}</span><span class="q-win-value" title=${resetTitle || undefined}>${percent(l.used)}
      ${compact && html`<span class="q-win-compact"> · ${compact}</span>`}</span></div>
    <div class="q-bar" role="meter" aria-label=${name} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${l.used}>
      <i style=${`width:${Math.min(100, Math.max(0, l.used))}%`}></i>
      ${expected != null && html`<u style=${`left:${expected}%`} title=${t('quotas.expected', { v: expected })}></u>`}
    </div>
    ${(caption || verdict) && html`<small class="q-win-f muted">
      ${caption && html`<span class=${cls(compact && 'q-win-desktop')} title=${resetTitle || undefined}>${caption}</span>`}
      ${verdict && html`<span class=${cls('q-pace', pace)}>${verdict}</span>`}
    </small>`}
  </div>`;
}

function Account({ a, sub, samples, first }) {
  const name = accName(a.id);
  const groups = new Map();
  for (const l of a.limits || []) {
    const group = l.group || '';
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push(l);
  }
  const [primary, ...others] = groups.values();
  const otherNames = [...groups.keys()].slice(1).map(group => group || t('quotas.other_limits'));
  const otherUsed = others.flat().map(l => Math.round(l.used));
  const common = otherUsed.length && otherUsed.every(v => v === otherUsed[0]) ? percent(otherUsed[0]) : '';
  const tip = [
    first && t('quotas.limits_tip'),
    a.id === 'antigravity' && t('quotas.reset_agy', { n: a.credits?.toLocaleString(locale()) ?? '—' }),
    a.id === 'copilot' && t('quotas.reset_copilot'),
  ].filter(Boolean).join(' ');
  const draw = list => byWindow(list).map(l => html`<${Window} key=${l.id} acc=${a.id} l=${l} samples=${samples} />`);
  return html`<article class="activity-card q-acc" aria-label=${name}>
    <header class="q-acc-h"><${AccountMark} id=${a.id} /><h3>${name}</h3>${sub && html`<span class="q-acc-sub muted">${sub}</span>`}
      ${tip && html`<${Tip} text=${tip} />`}
    </header>
    ${primary ? html`<div class="q-group">${draw(primary)}</div>` : html`<p class="muted q-none">${t('quotas.no_data')}</p>`}
    ${others.length > 0 && html`<details class="q-group-details"><summary>${joinList(otherNames)}${common && ' · ' + common}</summary>
      <div class="q-group">${draw(others.flat())}</div></details>`}
  </article>`;
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

function ResetRow({ id, title, detail, class: variant, children }) {
  return html`<div class=${cls('q-reset-row', variant)}>
    <${AccountMark} id=${id} /><div class="q-reset-copy"><span>${accName(id)}</span>
      ${title && html`<span class="q-reset-title">${title}</span>`}
      ${detail && html`<small>${detail}</small>`}
    </div>${children}
  </div>`;
}

const creditTitle = c => c.title || t('quotas.reset_full');
const creditExpiry = c => c.expires_at ? t('quotas.reset_until', { date: day(c.expires_at) }) : '';

function CodexResets({ reset, refresh }) {
  const [anchor, setAnchor] = useState(null);
  const credits = reset.credits || [];
  const extra = reset.available - credits.length;
  const grouped = credits.length > 1;
  const title = grouped && credits[0].title && credits.every(c => c.title === credits[0].title)
    ? t('quotas.reset_group_kind', { n: credits.length, kind: credits[0].title })
    : t('quotas.reset_group_count', { n: credits.length });
  const expiry = grouped && credits.every(c => c.expires_at)
    ? t('quotas.reset_until', { date: joinList(credits.map(c => day(c.expires_at))) }) : '';
  return html`
    ${!reset.available && html`<${ResetRow} id="codex" detail=${t('quotas.resets_none')} />`}
    ${credits.map(c => html`<${ResetRow} id="codex" key=${c.id} title=${creditTitle(c)} detail=${creditExpiry(c)} class=${grouped ? 'q-reset-desktop' : ''}>
      <button type="button" class="btn sm q-reset-action" onClick=${() => useCodexReset(c, refresh)}>${t('quotas.reset_use_one')}</button>
    </${ResetRow}>`)}
    ${grouped && html`<${ResetRow} id="codex" title=${title} detail=${expiry} class="q-reset-phone">
      <${DropdownButton} class="sm fit q-reset-action" open=${anchor} onClick=${e => setAnchor(e.currentTarget)}>${t('quotas.reset_use_one')}</${DropdownButton}>
    </${ResetRow}>`}
    ${anchor && grouped && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} width=${260}
      items=${credits.map(c => ({ key: c.id, label: creditTitle(c), note: c.expires_at && creditExpiry(c), run: () => useCodexReset(c, refresh) }))} />`}
    ${extra > 0 && html`<p class="muted q-reset-more">${t('quotas.reset_more', { n: extra })}</p>`}`;
}

function Resets({ accounts, codeman, refresh }) {
  const claude = accounts.find(a => a.id === 'claude')?.resets;
  const codex = accounts.find(a => a.id === 'codex')?.resets;
  if (!claude && !codex) return null;
  const total = (claude?.available || 0) + (codex?.available || 0);
  const checked = claude?.checked_at ? day(claude.checked_at) : '';
  const reported = claude?.stale ? (checked ? t('quotas.reset_claude_stale', { date: checked }) : t('quotas.reset_claude_unconfirmed'))
    : checked ? t('quotas.reset_claude_reported', { date: checked }) : '';
  const tip = [t('quotas.resets_tip'), claude?.available && t('quotas.reset_claude_kind')].filter(Boolean).join(' ');
  return html`<section class="activity-section">
    <div class="activity-heading"><h2>${t('quotas.resets')}${total > 0 && html`<span class="q-reset-count">${total}</span>`}<${Tip} text=${tip} /></h2></div>
    <div class="activity-card q-resets">
      ${claude && (claude.available ? html`<${ResetRow} id="claude" title=${t('quotas.reset_claude_one')} detail=${reported} class="q-reset-claude">
        ${codeman ? html`<button type="button" class="btn sm q-reset-action" aria-label=${t('quotas.reset_open_command')} onClick=${openClaudeReset}>
          <span class="q-reset-text">${t('quotas.reset_open_command')}</span><span class="q-reset-short">${t('quotas.reset_open_short')}</span>
        </button>` : html`<code class="q-reset-command">/limit-reset</code>`}
      </${ResetRow}>` : html`<${ResetRow} id="claude" detail=${t('quotas.reset_claude_none')} />`)}
      ${codex && html`<${CodexResets} reset=${codex} refresh=${refresh} />`}
    </div>
  </section>`;
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

// Clé d'une limite dans les relevés : la fenêtre (5h, 7d, month) pour Claude,
// Codex et Copilot, l'identifiant de la limite pour Antigravity.
const sampleKey = (acc, l) => (acc === 'antigravity' ? l.id : l.label ? l.id : l.window);
// Pas de projection tant que la fenêtre a moins d'une demi-heure (ou d'un
// dixième de sa durée) : trop peu de mesures pour un rythme fiable.
const tooEarly = (w, now) => now - w.start < Math.max(30 * 60e3, (w.end - w.start) / 10);

function windowProjection(w, now) {
  if (!w || w.used >= 100 || tooEarly(w, now) || w.rate <= 0) return null;
  if (w.eta != null) {
    const at = clock(w.eta);
    return { warning: true, short: t('quotas.eta_short', { at }), detail: t('quotas.win_eta', { at }) };
  }
  const v = percent(w.atReset);
  return { warning: tone(w.atReset) !== 'ok', short: t('quotas.projection_short', { v }),
    detail: t('quotas.projection_detail', { v, at: clock(w.end) }) };
}

const chartName = l => l.window === '7d' && !l.group && !l.label
  ? t('quotas.current_week') : [l.group, limitName(l, true)].filter(Boolean).join(' · ');

function WindowChart({ acc, l, samples, label }) {
  const [ref, width] = useWidth();
  const now = Date.now();
  const w = currentWindow(samples, acc, sampleKey(acc, l), l, now);
  const forecast = windowProjection(w, now);
  const used = percent(l.used);
  const color = tone(l.used) === 'red' ? 'red' : forecast?.warning ? 'amber' : tone(l.used);
  let verdict = '';
  if (!w) verdict = l.used > 0 ? t('quotas.win_unknown') : t('quotas.win_closed');
  else if (w.used >= 100) verdict = t('quotas.win_out', { at: clock(w.end) });
  else if (tooEarly(w, now)) verdict = t('quotas.win_early', { at: clock(w.start) });
  else if (!forecast) verdict = t('quotas.win_flat', { at: clock(w.end) });
  const height = 120;
  let svg = null;
  if (w && width > 8) {
    const x = at => 4 + Math.max(0, (at - w.start) / (w.end - w.start)) * (width - 8);
    const y = v => height - 4 - Math.min(100, Math.max(0, v)) / 100 * (height - 8);
    const line = w.pts.map(([at, value], i) => `${i ? 'L' : 'M'}${x(at).toFixed(1)} ${y(value).toFixed(1)}`).join(' ');
    const area = `${line} L${x(now)} ${y(0)} L${x(w.pts[0][0])} ${y(0)} Z`;
    svg = html`<svg width=${width} height=${height} viewBox=${`0 0 ${width} ${height}`} role="img"
      aria-label=${t('quotas.chart_used', { name: chartName(l), v: used })}>
      <line class="q-chart-pace" x1=${x(w.start)} y1=${y(0)} x2=${x(w.end)} y2=${y(100)} />
      <path class="q-chart-area" d=${area} /><path class="q-chart-line" d=${line} />
      ${w.used < 100 && !tooEarly(w, now) && html`<line class="q-chart-proj" x1=${x(now)} y1=${y(w.used)}
        x2=${x(w.eta ?? w.end)} y2=${y(w.eta == null ? w.atReset : 100)} />`}
      <circle class="q-chart-dot" cx=${x(now)} cy=${y(w.used)} r="3.5" />
    </svg>`;
  }
  return html`<figure class=${cls('q-chart', 'q-' + color)}>
    <figcaption class="q-chart-head">${label}<span class="q-chart-value">${forecast?.short || used}</span></figcaption>
    <div class=${cls('q-chart-plot', w && 'has-window')} ref=${ref}>${svg}</div>
    ${w && html`<div class="q-chart-foot muted"><span>${clock(w.start)}</span><span>${t('quotas.win_reset_at', { at: clock(w.end) })}</span></div>`}
    ${verdict && html`<p class="q-none muted">${verdict}</p>`}
  </figure>`;
}

// Part de la semaine (ou du mois) consommée chaque jour, sur 14 jours.
function DailyChart({ acc, l, samples }) {
  const key = sampleKey(acc, l);
  const hasSamples = samples.some(s => s[acc]?.[key]);
  const days = dailyUse(samples, acc, key, 14);
  const max = Math.max(100, ...days.map(d => d.used));
  const total = days.reduce((n, d) => n + d.used, 0);
  return html`<figure class="q-chart">
    <figcaption class="q-chart-head"><span>${t('quotas.daily_period', { n: days.length })}</span>
      ${total > 0 && html`<span class="muted">${t('quotas.daily_average', { v: percent(total / days.filter(d => d.used > 0).length, 1) })}</span>`}
    </figcaption>
    ${hasSamples ? html`<div class="q-day-bars" role="img" aria-label=${t('quotas.daily_of', { name: chartName(l) })}>${days.map(d => {
      const date = new Date(d.day);
      const label = date.toLocaleDateString(locale(), { weekday: 'short', day: 'numeric' });
      return html`<div class="q-day-column" key=${d.day} title=${label + ' · ' + percent(d.used, 1)}>
        <span><i style=${`height:${(d.used / max) * 100}%`}></i></span>
        <small>${date.toLocaleDateString(locale(), { weekday: 'narrow' })}</small></div>`;
    })}</div>` : html`<p class="q-none muted">${t('quotas.history_empty')}</p>`}
  </figure>`;
}

function Evolution({ account, samples }) {
  const [selected, setSelected] = useState(null);
  const limits = account.limits.filter(l => CHARTED.includes(l.window));
  if (!limits.length) return html`<p class="muted q-none">${t('quotas.history_empty')}</p>`;
  const l = limits.find(l => l.id === selected) || limits.find(l => l.window === '7d' && !l.label)
    || limits.find(l => l.window !== '5h') || limits[0];
  const label = limits.length > 1 ? html`<${ListPick} class="sm q-chart-select" label=${t('quotas.history_window')}
    value=${l.id} onChange=${setSelected} options=${byWindow(limits).map(x => ({ value: x.id, label: chartName(x) }))} />`
    : html`<span>${chartName(l)}</span>`;
  return html`<div class=${cls('activity-card q-charts', l.window !== '5h' && 'with-daily')}>
    <${WindowChart} acc=${account.id} l=${l} samples=${samples} label=${label} />
    ${l.window !== '5h' && html`<${DailyChart} acc=${account.id} l=${l} samples=${samples} />`}
  </div>`;
}

// Sessions arrêtées sur une limite : Codeman les relance seul à la remise à
// zéro (reprise automatique, réglable par session dans Infos).
function Paused() {
  useStore(agents, s => [s.order, s.sessions]);
  const list = sessionList().filter(s => stateOf(s) === 'limited').sort((a, b) => a.autoResumeAt - b.autoResumeAt);
  if (!list.length) return null;
  return html`<section class="activity-section"><div class="activity-heading"><h2>${t('quotas.paused')}<${Tip} text=${t('quotas.paused_tip')} /></h2></div>
    <div class="activity-card q-paused">${list.map(s => html`<button type="button" key=${s.id} class="q-paused-row" onClick=${() => openSession(s.id)}>
      <${CliMark} mode=${s.mode} size="sm" /><span><b>${s.name || s.id.slice(0, 8)}</b>
      <small class="muted">${t('quotas.resumes_at', { at: clock(s.autoResumeAt), in: fmtIn(s.autoResumeAt - Date.now()) })}</small></span>
      <${Icon} n="right" /></button>`)}</div></section>`;
}

function Tokens({ stats }) {
  const days = (stats?.daily || []).slice().sort((a, b) => (a.date < b.date ? 1 : -1)).slice(0, 14);
  if (!days.length) return html`<p class="muted q-none">${t('quotas.tokens_empty')}</p>`;
  const max = Math.max(...days.map(d => (d.inputTokens || 0) + (d.outputTokens || 0)), 1);
  const money = v => v == null ? '—' : Number(v).toLocaleString(locale(), { style: 'currency', currency: 'USD', maximumFractionDigits: 2 });
  const tokens = n => n == null ? '—' : fmtTok(n);
  return html`<div class="card table q-tokens">
    <div class="tr th"><span>${t('quotas.day')}</span><span>${t('usage.page.entree_2')}</span><span>${t('usage.page.sortie_2')}</span><span>${t('quotas.sessions')}</span><span>${t('quotas.cost')}</span></div>
    ${days.map(d => { const total = (d.inputTokens || 0) + (d.outputTokens || 0); return html`<div class="tr" key=${d.date}>
      <span class="q-day"><b>${new Date(d.date + 'T12:00:00').toLocaleDateString(locale(), { weekday: 'short', day: 'numeric', month: 'short' })}</b><i class="q-mini"><b style=${`width:${(total / max) * 100}%`}></b></i></span>
      <span class="num" data-label=${t('usage.page.entree_2')}>${tokens(d.inputTokens)}</span><span class="num" data-label=${t('usage.page.sortie_2')}>${tokens(d.outputTokens)}</span>
      <span class="num" data-label=${t('quotas.sessions')}>${d.sessions ?? '—'}</span><span class="num strong" data-label=${t('quotas.cost')}>${money(d.estimatedCost)}</span></div>`; })}
  </div>`;
}

// Aperçu des quotas dans la barre Agents : une jauge (la limite la plus
// entamée), le détail de chaque abonnement en un clic, la page complète
// dans Activité. Mêmes données que la page.
const byWindow = list => [...list].sort((x, y) => (x.group || '').localeCompare(y.group || '') || WIN_ORDER.indexOf(x.window) - WIN_ORDER.indexOf(y.window));
let lastQuotas = null;
async function loadQuotas() {
  const r = await get('/api/quotas', { retryAuth: false, timeout: 45000 }).catch(() => null);
  if (r && r.ok !== false) lastQuotas = r;
  return lastQuotas;
}

// Quotas de l'abonnement d'un harness, dans le panneau de sa session.
const ACCOUNT_OF = { claude: 'claude', codex: 'codex', antigravity: 'antigravity', gemini: 'antigravity', copilot: 'copilot' };
export function HarnessQuota({ mode }) {
  const live = useStore(agents, s => s.planUsage);
  const [data, setData] = useState(lastQuotas);
  useEffect(() => visibleRefresh(async alive => { const r = await loadQuotas(); if (alive() && r) setData(r); }, 120000), []);
  const id = ACCOUNT_OF[mode];
  const a = id && withLive(data?.accounts || [], live).find(x => x.id === id);
  if (!a || !(a.limits || []).length) return null;
  const warn = v => (v >= 90 ? 'red' : v >= 70 ? 'amber' : '');
  return html`<section class="cm-hq" aria-label=${t('quotas.title') + ' · ' + accName(id)}>
    <div class="cm-hq-h"><span>${t('quotas.title')} · ${accName(id)}</span><a href="#/quotas">${t('quotas.open')}</a></div>
    ${byWindow(a.limits).slice(0, 4).map(l => html`<div class=${cls('cm-qp-win', warn(l.used))} key=${l.id}>
      <div class="cm-qp-h"><span>${(l.group ? l.group + ' · ' : '') + limitName(l)}</span><em>${Math.round(l.used)}%</em></div>
      <div class="cm-qp-bar"><i style=${`width:${Math.min(100, l.used)}%`}></i></div>
      ${l.reset_at && l.reset_at > Date.now() ? html`<small class="muted">${t('quotas.resets_in', { in: fmtIn(l.reset_at - Date.now()), at: clock(l.reset_at) })}</small>` : ''}
    </div>`)}
  </section>`;
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
  const charted = accounts.filter(a => a.limits.some(l => CHARTED.includes(l.window)));
  const shown = charted.find(a => a.id === acc) || charted[0];
  const sub = a => {
    if (a.id === 'antigravity' && a.credits != null) return t('quotas.ai_credits', { n: a.credits, v: a.credits.toLocaleString(locale()) });
    return (a.id === 'claude' && live?.modelDisplayName) || (a.plan ? a.plan[0].toUpperCase() + a.plan.slice(1) : '');
  };
  return html`<${ActivityLayout} title=${t('quotas.title')} refresh=${() => setTick(n => n + 1)}>
    ${data === null && !live ? html`<div class="skeleton" style="height:220px"></div>`
      : !accounts.length ? html`<div class="activity-card"><${Empty} icon="gauge" title=${t('quotas.empty_title')} text=${t('quotas.empty_text')}>${!conn.configured && html`<a class="btn" href="#/agents/settings"><${Icon} n="plug" />${t('quotas.connect')}</a>`}</${Empty}></div>`
      : html`
        ${conn.configured && !ready && html`<p class="note"><i class="dot amber"></i> ${t('quotas.offline')}</p>`}
        <${Resets} accounts=${accounts} codeman=${ready} refresh=${() => setTick(n => n + 1)} />
        <section class="activity-section" aria-labelledby="quota-limits">
          <h2 class="q-limits-title" id="quota-limits">${t('quotas.limits')}</h2>
          <div class="activity-grid q-accs">${accounts.map((a, i) => html`<${Account} key=${a.id} a=${a} sub=${sub(a)} samples=${samples} first=${i === 0} />`)}</div>
        </section>
        ${shown && html`<section class="activity-section"><div class="activity-heading"><h2>${t('quotas.history')}<${Tip} text=${t('quotas.history_tip')} /></h2>
          ${charted.length > 1 && html`<${Seg} size="sm" value=${shown.id} onChange=${v => { setAcc(v); store('loom.quotas.acc', v); }}
            label=${t('quotas.history_account')} options=${charted.map(a => ({ value: a.id, label: accName(a.id) }))} />`}
          </div><${Evolution} key=${shown.id} account=${shown} samples=${samples} /></section>`}
        ${conn.configured && html`<${Paused} />`}
        ${ready && html`<section class="activity-section"><div class="activity-heading"><h2>${t('quotas.tokens')}<${Tip} text=${t('quotas.tokens_tip') + ' ' + t('quotas.cost_tip')} /></h2></div>
          <${Tokens} stats=${stats} /></section>`}`}
  </${ActivityLayout}>`;
}
