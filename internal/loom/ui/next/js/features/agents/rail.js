// Rail des sessions : sessions vivantes groupées par projet (dossier de
// travail), puis l'historique que Codeman sait reprendre.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { confirm, toast } from '../../ui/dialog.js';
import { agents, stateOf, TONE, projectOf, sessionList } from './store.js';
import { resumeSession, resumeSupport } from './actions.js';
import { api } from './client.js';
import { CliMark } from './new-session.js';

export function ago(ms) {
  if (!ms) return '';
  const s = Math.max(0, (Date.now() - ms) / 1000);
  // Durée compacte (« 4 min », « 3 h », « 8 j ») : le contexte dit déjà « il y a ».
  const unit = (n, u) => new Intl.NumberFormat(locale(), { style: 'unit', unit: u, unitDisplay: 'short' }).format(n);
  if (s < 50) return t('agents.now');
  if (s < 3600) return unit(Math.round(s / 60), 'minute');
  if (s < 86400) return unit(Math.round(s / 3600), 'hour');
  return unit(Math.round(s / 86400), 'day');
}

export const StateDot = ({ state }) => html`<i class=${cls('dot', TONE[state])} title=${lt('state', state)}></i>`;

function SessionRow({ s, active, onOpen }) {
  const state = stateOf(s);
  return html`<button class=${cls('cm-row', active && 'on', 'st-' + state)} aria-current=${active ? 'page' : undefined} onClick=${() => onOpen(s.id)}>
    <${CliMark} mode=${s.mode} size="sm" />
    <span class="cm-row-t"><b class="trunc">${s.name || s.id.slice(0, 8)}</b>
      <small class="trunc">${state === 'idle' || state === 'stopped' ? ago(s.lastActivityAt) : lt('state', state)}${s.displayModel?.model ? ' · ' + s.displayModel.model : ''}</small></span>
    <${StateDot} state=${state} /></button>`;
}

function HistoryRow({ r, onOpen }) {
  const [busy, setBusy] = useState(false);
  const support = resumeSupport(r);
  const resume = async () => {
    if (busy) return;
    const text = support === 'none' ? t('agents.history.no_resume', { cli: r.mode }) : support === 'latest' ? t('agents.history.resume_latest') : t('agents.history.resume_exact');
    if (!await confirm(t('agents.history.resume_title'), text, { ok: support === 'none' ? t('agents.history.new_here') : t('agents.history.resume') })) return;
    setBusy(true);
    try { onOpen(await resumeSession(r)); } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  return html`<button class="cm-row hist" disabled=${busy} onClick=${resume} title=${r.workingDir}>
    <${CliMark} mode=${r.mode || 'claude'} size="sm" />
    <span class="cm-row-t"><b class="trunc">${r.name || (r.firstPrompt ? r.firstPrompt.slice(0, 60) : r.sessionId.slice(0, 8))}</b>
      <small class="trunc">${projectOf(r.workingDir)} · ${ago(r.lastActivityAt || Date.parse(r.lastModified || '') || 0)}</small></span>
    ${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="history" />`}</button>`;
}

function RestoreOffer() {
  const restore = useStore(agents, s => s.restore);
  const [busy, setBusy] = useState(false);
  if (!restore.length) return null;
  const run = async () => {
    setBusy(true);
    try { const r = await api.restore(); toast(t('agents.restore.done', { n: (r?.restored || []).length })); agents.set({ restore: [] }); }
    catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  return html`<div class="cm-restore"><${Icon} n="refresh" /><span>${t('agents.restore.offer', { n: restore.length })}</span>
    <button class="btn sm" disabled=${busy} onClick=${run}>${t('agents.restore.do')}</button>
    <button class="icon-btn" aria-label=${t('agents.restore.dismiss')} onClick=${() => api.dismissRestore().then(() => agents.set({ restore: [] })).catch(e => toast(e.message, 'err'))}><${Icon} n="close" /></button></div>`;
}

export function Rail({ activeId, onOpen, onNew }) {
  useStore(agents, s => [s.order, s.sessions, s.approvals]);
  const unified = useStore(agents, s => s.unified);
  const [q, setQ] = useState('');
  const [showHist, setShowHist] = useState(() => { try { return localStorage.getItem('loom.codeman.hist') !== '0'; } catch (_) { return true; } });
  const [histLimit, setHistLimit] = useState(15);
  const needle = q.trim().toLowerCase();
  const match = (...xs) => !needle || xs.some(x => String(x || '').toLowerCase().includes(needle));
  const live = sessionList().filter(s => match(s.name, s.workingDir, s.mode, s.displayModel?.model));
  const groups = new Map();
  for (const s of live) { const k = s.workingDir || ''; if (!groups.has(k)) groups.set(k, []); groups.get(k).push(s); }
  const liveIds = new Set(live.map(s => s.id));
  const history = unified.filter(r => !(r.sources || []).includes('live') && !liveIds.has(r.sessionId) && match(r.name, r.workingDir, r.firstPrompt, r.mode))
    .sort((a, b) => (b.lastActivityAt || 0) - (a.lastActivityAt || 0));
  const toggleHist = () => { const v = !showHist; setShowHist(v); try { localStorage.setItem('loom.codeman.hist', v ? '1' : '0'); } catch (_) {} };
  return html`<div class="cm-rail">
    <div class="cm-rail-h">
      <label class="search"><${Icon} n="search" /><input placeholder=${t('agents.rail.search')} value=${q} onInput=${e => setQ(e.target.value)} aria-label=${t('agents.rail.search')} /></label>
      <button class="btn primary sm cm-new" onClick=${onNew}><${Icon} n="plus" />${t('agents.rail.new')}</button>
    </div>
    <${RestoreOffer} />
    <div class="cm-rail-list">
      ${!live.length && html`<p class="cm-rail-empty muted">${needle ? t('agents.rail.no_match') : t('agents.rail.empty')}</p>`}
      ${[...groups.entries()].map(([dir, list]) => html`<section class="cm-group" key=${dir}>
        <div class="cm-group-h" title=${dir}><${Icon} n="folder" /><span class="trunc">${projectOf(dir)}</span><em>${list.length}</em></div>
        ${list.map(s => html`<${SessionRow} key=${s.id} s=${s} active=${s.id === activeId} onOpen=${onOpen} />`)}</section>`)}
      ${history.length > 0 && html`<section class="cm-group">
        <button class="cm-group-h toggle" aria-expanded=${String(showHist)} onClick=${toggleHist}><${Icon} n=${showHist ? 'chevron' : 'right'} /><span>${t('agents.rail.history')}</span><em>${history.length}</em></button>
        ${showHist && history.slice(0, histLimit).map(r => html`<${HistoryRow} key=${r.sessionId} r=${r} onOpen=${onOpen} />`)}
        ${showHist && history.length > histLimit && html`<button class="cm-more" onClick=${() => setHistLimit(histLimit + 30)}>${t('agents.rail.more', { n: history.length - histLimit })}</button>`}
      </section>`}
    </div>
  </div>`;
}
