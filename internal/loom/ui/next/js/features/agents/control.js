// Centre de contrôle : ce qui attend une personne, ce qui travaille, ce qui
// est programmé. Vue d'ensemble de toutes les sessions Codeman.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { toast } from '../../ui/dialog.js';
import { api } from './client.js';
import { agents, stateOf, sessionList, projectOf, cliOf, capsOf, onDomain } from './store.js';
import { answerApproval, dismissApproval, interrupt } from './actions.js';
import { StateDot, ago } from './rail.js';
import { CliMark } from './new-session.js';
import { openSession } from './nav.js';

function ApprovalRow({ a }) {
  const [busy, setBusy] = useState(false);
  const s = agents.get().sessions[a.sessionId];
  const terminal = a.answerInTerminal || (s && capsOf(s.mode).approvals !== 'answer');
  const run = async body => { setBusy(true); try { await answerApproval(a, body); } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); } };
  return html`<li class="cm-cc-appr">
    <div class="cm-cc-appr-t"><b>${a.sessionName || (s && s.name) || a.sessionId.slice(0, 8)}</b>
      <span class="muted">${a.kind === 'question' ? t('agents.approval.question') : a.kind === 'idle' ? t('agents.approval.idle') : t('agents.approval.permission')}${a.toolName ? ' · ' + a.toolName : ''} · ${ago(a.createdAt)}</span>
      ${(a.toolSummary || a.message) && html`<p>${a.toolSummary || a.message}</p>`}</div>
    <div class="cm-cc-appr-a">
      ${a.kind !== 'idle' && !terminal && html`${(a.options && a.options.length ? a.options : [{ n: 1, label: t('agents.approval.approve') }]).map(o => html`<button class=${cls('btn sm', o.n === 1 && 'primary')} disabled=${busy}
          onClick=${() => run(a.options && a.options.length ? { action: 'option', option: o.n } : { action: 'approve' })}>${o.label}</button>`)}
        <button class="btn sm ghost danger" disabled=${busy} onClick=${() => run({ action: 'deny' })}>${t('agents.approval.deny')}</button>`}
      <button class="btn sm" onClick=${() => openSession(a.sessionId)}>${terminal ? t('agents.approval.open_terminal') : t('agents.cc.open')}</button>
      <button class="icon-btn" aria-label=${t('agents.approval.dismiss')} onClick=${() => dismissApproval(a).catch(e => toast(e.message, 'err'))}><${Icon} n="close" /></button>
    </div></li>`;
}

function SessionCard({ s }) {
  const state = stateOf(s);
  return html`<li class=${cls('cm-cc-card', 'st-' + state)}>
    <button class="cm-cc-card-main" onClick=${() => openSession(s.id)}>
      <${CliMark} mode=${s.mode} size="sm" />
      <span class="grow"><b class="trunc">${s.name || s.id.slice(0, 8)}</b><small class="trunc muted">${projectOf(s.workingDir)} · ${cliOf(s.mode).label}${s.displayModel?.model ? ' · ' + s.displayModel.model : ''}</small></span>
      <span class="cm-cc-state"><${StateDot} state=${state} />${lt('state', state)}</span></button>
    <div class="cm-cc-card-f"><span class="muted">${ago(s.lastActivityAt)}</span>
      ${s.tokens?.total > 0 && html`<span class="muted mono">${Math.round(s.tokens.total / 1000)}k tok</span>`}
      <span class="grow"></span>
      ${state === 'working' && html`<button class="btn sm ghost" onClick=${() => interrupt(s.id).catch(e => toast(e.message, 'err'))}><${Icon} n="stop" />${t('agents.composer.interrupt')}</button>`}</div>
  </li>`;
}

const ORDER = ['waiting', 'error', 'working', 'starting', 'idle', 'stopped'];
export function ControlCenter() {
  useStore(agents, s => [s.order, s.sessions]);
  const approvals = useStore(agents, s => s.approvals);
  const conn = useStore(agents, s => s.conn);
  const [cron, setCron] = useState({ jobs: [], runs: [] });
  const [update, setUpdate] = useState(null);
  const loadCron = () => Promise.all([api.cronJobs(), api.cronRuns()]).then(([jobs, runs]) => setCron({ jobs: jobs || [], runs: runs || [] })).catch(() => {});
  useEffect(() => { loadCron(); api.updateCheck().then(setUpdate).catch(() => {}); return onDomain('cron', loadCron); }, []);
  const list = sessionList().sort((a, b) => ORDER.indexOf(stateOf(a)) - ORDER.indexOf(stateOf(b)) || (b.lastActivityAt || 0) - (a.lastActivityAt || 0));
  const counts = ORDER.reduce((m, k) => (m[k] = list.filter(s => stateOf(s) === k).length, m), {});
  const pending = approvals.filter(a => a.kind !== 'idle'), idleWaits = approvals.filter(a => a.kind === 'idle');
  const next = cron.jobs.filter(j => j.enabled && j.nextRunAt).sort((a, b) => a.nextRunAt - b.nextRunAt).slice(0, 4);
  const runs = [...cron.runs].sort((a, b) => b.startedAt - a.startedAt).slice(0, 5);
  const fmt = ms => new Date(ms).toLocaleString(locale(), { weekday: 'short', hour: '2-digit', minute: '2-digit' });
  return html`<div class="cm-scroll"><div class="cm-cc">
    <div class="cm-cc-sum">
      <span><b>${list.length}</b> ${t('agents.cc.sessions')}</span>
      <span class=${counts.working ? 'amber' : ''}><b>${counts.working + counts.starting}</b> ${t('agents.cc.working')}</span>
      <span class=${pending.length ? 'red' : ''}><b>${pending.length}</b> ${t('agents.cc.waiting')}</span>
      <span><b>${cron.jobs.filter(j => j.enabled).length}</b> ${t('agents.cc.scheduled')}</span>
      <span class="grow"></span>
      ${update && html`<span class="muted">${update.updateAvailable ? t('agents.cc.update', { v: update.latestVersion }) : t('agents.cc.uptodate', { v: update.currentVersion || conn.version })}</span>`}
    </div>
    <section class="cm-cc-sec">
      <h2>${t('agents.cc.attention')}</h2>
      ${pending.length || idleWaits.length ? html`<ul class="cm-cc-list">${[...pending, ...idleWaits].map(a => html`<${ApprovalRow} key=${a.id} a=${a} />`)}</ul>`
        : html`<p class="muted cm-cc-none">${t('agents.cc.nothing')}</p>`}
    </section>
    <section class="cm-cc-sec">
      <h2>${t('agents.cc.board')}</h2>
      ${list.length ? html`<ul class="cm-cc-grid">${list.map(s => html`<${SessionCard} key=${s.id} s=${s} />`)}</ul>` : html`<p class="muted cm-cc-none">${t('agents.rail.empty')}</p>`}
    </section>
    <section class="cm-cc-sec cm-cc-two">
      <div><h2>${t('agents.cc.next')}</h2>
        ${next.length ? html`<ul class="cm-cc-mini">${next.map(j => html`<li key=${j.id}><${CliMark} mode=${j.agentType} size="sm" /><span class="grow trunc">${j.name}</span><time class="muted">${fmt(j.nextRunAt)}</time></li>`)}</ul>`
          : html`<p class="muted">${t('agents.cc.no_next')}</p>`}
        <a class="btn sm ghost" href="#/agents/cron">${t('agents.cc.all_cron')}</a></div>
      <div><h2>${t('agents.cc.runs')}</h2>
        ${runs.length ? html`<ul class="cm-cc-mini">${runs.map(r => html`<li key=${r.id}><i class=${cls('dot', r.status === 'failed' ? 'red' : r.status === 'skipped' ? '' : 'green')}></i>
            <span class="grow trunc">${(cron.jobs.find(j => j.id === r.cronJobId) || {}).name || r.cronJobId}</span><span class="muted">${lt('runStatus', r.status)}</span>
            ${r.sessionId && agents.get().sessions[r.sessionId] && html`<button class="btn sm ghost" onClick=${() => openSession(r.sessionId)}>${t('agents.cc.open')}</button>`}</li>`)}</ul>`
          : html`<p class="muted">${t('agents.cc.no_runs')}</p>`}</div>
    </section>
  </div></div>`;
}
