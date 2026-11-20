// Centre de contrôle : ce qui attend une personne, ce qui travaille, ce qui
// est programmé. Vue d'ensemble de toutes les sessions Codeman.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { api, cm } from './client.js';
import { agents, stateOf, sessionList, projectOf, cliOf, capsOf, hasConversation, onDomain } from './store.js';
import { answerApproval, dismissApproval, interrupt } from './actions.js';
import { StateDot, ago } from './rail.js';
import { HistoryRow } from './side.js';
import { CliMark } from './new-session.js';
import { officialLink, openSession } from './nav.js';
import { Menu } from '../../ui/controls.js';
import { get } from '../../core/api.js';
import { visibleRefresh } from '../../core/poll.js';
import { ServerSetup, disconnectServer } from './connection.js';
import { canInstall, InstallCli } from './settings.js';
import { schedule } from './cron.js';
import { setViewOf } from './layout.js';

function ApprovalRow({ a }) {
  const [busy, setBusy] = useState(false);
  const s = agents.get().sessions[a.sessionId];
  const terminal = a.answerInTerminal || capsOf(s?.mode).approvals !== 'answer';
  const run = async body => { setBusy(true); try { await answerApproval(a, body); } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); } };
  return html`<li class="cm-cc-appr">
    <i class="dot blue"></i>${s && html`<${CliMark} mode=${s.mode} size="sm" />`}
    <div class="cm-cc-appr-t"><b>${a.sessionName || (s && s.name) || a.sessionId.slice(0, 8)}</b>
      <span class="muted">${a.kind === 'question' ? t('agents.approval.question') : a.kind === 'idle' ? t('agents.approval.idle') : t('agents.approval.permission')}${a.toolName ? ' · ' + a.toolName : ''} · ${ago(a.createdAt)}</span>
      ${(a.toolSummary || a.message) && html`<p>${a.toolSummary || a.message}</p>`}</div>
    <div class="cm-cc-appr-a">
      ${a.kind !== 'idle' && !terminal && html`${(a.options && a.options.length ? a.options : [{ n: 1, label: t('agents.approval.approve') }]).map(o => html`<button class=${cls('btn sm', o.n === 1 && 'primary')} disabled=${busy}
          onClick=${() => run(a.options && a.options.length ? { action: 'option', option: o.n } : { action: 'approve' })}>${o.label}</button>`)}
        <button class="btn sm ghost danger" disabled=${busy} onClick=${() => run({ action: 'deny' })}>${t('agents.approval.deny')}</button>`}
      <button class="btn sm" onClick=${() => { if (terminal) setViewOf(a.sessionId, 'term'); openSession(a.sessionId); }}>${terminal ? t('agents.approval.open_terminal') : t('agents.cc.open')}</button>
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

function ServerCard({ counts, update }) {
  const conn = useStore(agents, s => s.conn);
  const live = useStore(agents, s => s.live);
  const [anchor, setAnchor] = useState(null);
  const [change, setChange] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  useEffect(() => { setSubmitted(false); }, [conn.url, conn.version]);
  const canUpdate = update?.updateAvailable && update.latestVersion && update.selfUpdateEnabled === true &&
    ['git', 'docker-compose'].includes(update.installKind) && !update.environment?.blockers?.length;
  const apply = async () => {
    if (!await confirm(t('agents.admin.update'), t('agents.admin.update_text', { v: update.latestVersion }), { ok: t('agents.admin.update') })) return;
    setUpdating(true);
    try {
      await cm.post('/system/update');
      setSubmitted(true);
      toast(t('agents.admin.update_started'));
    } catch (e) { toast(e.message, 'err'); } finally { setUpdating(false); }
  };
  const link = officialLink('');
  const version = conn.version || update?.currentVersion || '';
  const host = conn.url.replace(/^https?:\/\//, '').replace(/\/$/, '');
  const status = live ? t('agents.admin.connected', { host }) : [host, t('agents.conn.connecting')].filter(Boolean).join(' · ');
  return html`<section class="cm-server">
    <i class=${cls('dot', live ? 'green' : 'amber')}></i>
    <div class="cm-server-info"><b>${t('agents.admin.server', { v: version })}</b>
      <small>${[status, ...counts].join(' · ')}</small></div>
    <div class="cm-server-actions">
      ${update?.updateAvailable && html`<span class="muted">${t('agents.admin.available', { v: update.latestVersion })}</span>`}
      ${update?.updateAvailable === false && !update.error && update.latestVersion && html`<span class="muted">${t('agents.admin.current')}</span>`}
      ${canUpdate && html`<button class="btn sm primary" disabled=${updating || submitted} onClick=${apply}>
        ${submitted ? t('agents.admin.update_started') : t('agents.admin.update')}</button>`}
      ${link && html`<a class="btn sm cm-server-link" href=${link} target="codeman" rel="noopener" aria-label=${t('agents.official')} title=${t('agents.official')}><span>${t('agents.official')}</span><${Icon} n="link" /></a>`}
      <button class="icon-btn" aria-label=${t('agents.admin.server_menu')} aria-expanded=${!!anchor} onClick=${e => setAnchor(e.currentTarget)}><${Icon} n="more" /></button>
    </div>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[
      { label: t('agents.setup.change'), icon: 'plug', run: () => setChange(true) },
      { label: t('agents.setup.disconnect'), danger: true, run: disconnectServer },
    ]} />`}
    ${change && html`<${Modal} title=${t('agents.setup.change')} onClose=${() => setChange(false)}>
      <${ServerSetup} initial=${conn} onDone=${() => setChange(false)} />
    </${Modal}>`}
  </section>`;
}

// Only compare account-wide windows or limits for the same model family.
function usageRows(account) {
  const groups = new Map();
  const percent = new Intl.NumberFormat(locale(), { style: 'percent' });
  for (const limit of account?.limits || []) {
    if (!['5h', '7d'].includes(limit.window) || !Number.isFinite(limit.used)) continue;
    const name = [limit.group, limit.label].filter(Boolean).join(' · ');
    if (!groups.has(name)) groups.set(name, {});
    groups.get(name)[limit.window] = percent.format(limit.used / 100);
  }
  return [...groups].map(([name, values]) => [name, [values['5h'] || '—', values['7d'] || '—'].join(' · ')].filter(Boolean).join(' · '));
}

function HarnessTable() {
  const clis = useStore(agents, s => s.clis);
  const manage = useStore(agents, s => s.settings.cliManagementEnabled === true);
  const sessions = useStore(agents, s => s.sessions);
  const loaded = useStore(agents, s => s.loaded);
  const [accounts, setAccounts] = useState([]);
  useEffect(() => visibleRefresh(async alive => {
    const data = await get('/api/quotas', { retryAuth: false, timeout: 45000 });
    if (alive() && Array.isArray(data?.accounts)) setAccounts(data.accounts);
  }, 120000), []);
  const harnesses = clis.filter(c => c.kind !== 'shell');
  return html`<section class="cm-cc-sec">
    <h2>${t('agents.admin.harness')}</h2>
    ${!harnesses.length ? html`<p class="cm-admin-empty">${loaded ? t('agents.admin.no_registry') : t('agents.conn.connecting')}</p>`
      : html`<table class="cm-harness"><thead><tr>
        <th>${t('agents.admin.harness')}</th><th>${t('settings.page.version')}</th><th>${t('agents.admin.account')}</th>
        <th>${t('agents.admin.windows')}</th><th>${t('agents.admin.in_loom')}</th><th aria-label=${t('app.shell.actions')}></th>
      </tr></thead><tbody>${harnesses.map(c => {
        const account = accounts.find(a => a.id === (c.id === 'gemini' ? 'antigravity' : c.id));
        const installedVersion = c.installed !== false && (c.installedVersion || c.version);
        const observed = !installedVersion && c.installed === true && Object.values(sessions)
          .filter(s => s.mode === c.id && s.cliVersion).sort((a, b) => (b.createdAt || 0) - (a.createdAt || 0))[0]?.cliVersion;
        const version = installedVersion || (observed ? t('agents.admin.session_version', { v: observed }) : '');
        const caps = capsOf(c.id);
        const capability = caps.approvals === 'answer' ? t('agents.admin.cap_answer')
          : hasConversation(c.id) ? t('agents.admin.cap_terminal') : t('agents.caps.shell');
        const plan = [account?.plan];
        if (Number.isFinite(account?.credits)) plan.push(t('agents.admin.credits', { n: account.credits }));
        if (account?.resets?.exact && account.resets.available > 0) plan.push(t('agents.admin.resets', { n: account.resets.available }));
        const accountText = plan.filter(Boolean).join(' · ');
        const usage = usageRows(account);
        const mobileMeta = [version, accountText, ...usage].filter(Boolean).join(' · ');
        return html`<tr key=${c.id}>
          <th scope="row"><span class="cm-harness-name"><${CliMark} mode=${c.id} size="sm" /><span>${c.label}<small class="cm-harness-meta">${mobileMeta || (c.installed === false ? t('agents.new.not_installed') : '')}</small></span></span></th>
          <td class="cm-harness-version mono">${version || '—'}</td>
          <td class="cm-harness-account">${accountText || '—'}</td>
          <td class="cm-harness-usage">${usage.length ? usage.map(row => html`<span class="cm-usage-line" key=${row}>${row}</span>`) : '—'}</td>
          <td class="cm-harness-caps">${capability}</td>
          <td class="cm-harness-action">${canInstall(c) ? html`<${InstallCli} cli=${c} manage=${manage} />`
            : c.installed === false ? t('agents.new.not_installed') : c.enabled === false ? t('agents.cfg.cli.off') : c.installed === true ? t('agents.caps.installed') : ''}</td>
        </tr>`;
      })}</tbody></table>`}
  </section>`;
}

const ORDER = ['waiting', 'error', 'limited', 'working', 'starting', 'idle', 'stopped'];
// Conversations passées que Codeman sait reprendre (dans le même dossier).
function ResumeList() {
  const unified = useStore(agents, s => s.unified);
  useStore(agents, s => s.order);
  const ids = new Set(sessionList().map(s => s.id));
  const rows = unified.filter(r => !(r.sources || []).includes('live') && !ids.has(r.sessionId))
    .sort((a, b) => (b.lastActivityAt || 0) - (a.lastActivityAt || 0)).slice(0, 12);
  if (!rows.length) return null;
  return html`<section class="cm-cc-sec">
    <h2>${t('agents.cc.resume')}</h2>
    <div class="cm-resume">${rows.map(r => html`<${HistoryRow} key=${r.sessionId} r=${r} />`)}</div>
  </section>`;
}

export function ControlCenter() {
  useStore(agents, s => [s.order, s.sessions]);
  const approvals = useStore(agents, s => s.approvals);
  const loaded = useStore(agents, s => s.loaded);
  const conn = useStore(agents, s => s.conn);
  const [jobs, setJobs] = useState(null);
  const [error, setError] = useState('');
  const [update, setUpdate] = useState(null);
  useEffect(() => {
    let active = true, revision = 0;
    setUpdate(null);
    setJobs(null);
    const load = async () => {
      const request = ++revision;
      try {
        const list = await api.cronJobs();
        if (active && revision === request) { setJobs(list || []); setError(''); }
      } catch (e) { if (active && revision === request) setError(e.message); }
    };
    load();
    api.updateCheck().then(value => { if (active) setUpdate(value); }).catch(() => {});
    const off = onDomain('cron', load);
    return () => { active = false; off(); };
  }, [conn.url, conn.version]);
  const list = sessionList().sort((a, b) => ORDER.indexOf(stateOf(a)) - ORDER.indexOf(stateOf(b)) || (b.lastActivityAt || 0) - (a.lastActivityAt || 0));
  const working = list.filter(s => ['working', 'starting'].includes(stateOf(s))).length;
  const pending = approvals.filter(a => a.kind !== 'idle');
  const counts = loaded ? [t('agents.admin.sessions', { n: list.length }), t('agents.admin.working', { n: working }), t('agents.admin.waiting', { n: pending.length })] : [];
  if (jobs) counts.push(t('agents.admin.scheduled', { n: jobs.filter(j => j.enabled).length }));
  const next = (jobs || []).filter(j => j.enabled && j.nextRunAt).sort((a, b) => a.nextRunAt - b.nextRunAt).slice(0, 4);
  const fmt = ms => new Date(ms).toLocaleString(locale(), { weekday: 'short', hour: '2-digit', minute: '2-digit' });
  return html`<div class="cm-scroll"><div class="cm-cc">
    <${ServerCard} key=${conn.url} counts=${counts} update=${update} />
    <section class="cm-cc-sec">
      <h2>${t('agents.cc.attention')}</h2>
      ${approvals.length ? html`<ul class="cm-cc-list">${[...pending, ...approvals.filter(a => a.kind === 'idle')].map(a => html`<${ApprovalRow} key=${a.id} a=${a} />`)}</ul>`
        : html`<p class="cm-admin-empty">${loaded ? t('agents.cc.nothing') : t('agents.conn.connecting')}</p>`}
    </section>
    <${HarnessTable} />
    <section class="cm-cc-sec">
      <div class="cm-section-head"><h2>${t('agents.cc.next')}</h2><a href="#/agents/cron">${t('agents.cc.all_cron')}</a></div>
      ${error && html`<p class="msg-err">${error}</p>`}
      ${next.length ? html`<ul class="cm-next-runs">${next.map(j => html`<li key=${j.id}><b>${j.name}</b><span>${cliOf(j.agentType).label} · ${schedule(j)}</span><time>${fmt(j.nextRunAt)}</time></li>`)}</ul>`
        : !error && html`<p class="cm-admin-empty">${jobs ? t('agents.cc.no_next') : t('agents.conn.connecting')}</p>`}
    </section>
    <${ResumeList} />
    <details class="cm-session-board"><summary>${t('agents.cc.board')} · ${list.length}</summary>
      ${list.length ? html`<ul class="cm-cc-grid">${list.map(s => html`<${SessionCard} key=${s.id} s=${s} />`)}</ul>` : html`<p class="muted">${t('agents.rail.empty')}</p>`}
    </details>
  </div></div>`;
}
