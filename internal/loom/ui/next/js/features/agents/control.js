// Centre de contrôle : ce qui attend une personne, ce qui travaille, ce qui
// est programmé. Vue d'ensemble de toutes les sessions Codeman.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { api, cm } from './client.js';
import { agents, stateOf, projectOf, cliOf, capsOf, onDomain, loadHistory, refreshAll, restorable } from './store.js';
import { answerApproval, dismissApproval, interrupt } from './actions.js';
import { StateDot, ago } from './rail.js';
import { HistoryRow } from './side.js';
import { CliMark } from './new-session.js';
import { officialLink, openSession } from './nav.js';
import { Menu } from '../../ui/controls.js';
import { ServerSetup, disconnectServer } from './connection.js';
import { canInstall, InstallCli } from './settings.js';
import { schedule } from './cron.js';
import { setViewOf } from './layout.js';
import { go } from '../../core/state.js';

const PAGE_SIZE = 8;

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
        <button class="btn sm danger" disabled=${busy} onClick=${() => run({ action: 'deny' })}>${t('agents.approval.deny')}</button>`}
      <button class="btn sm" onClick=${() => { if (terminal) setViewOf(a.sessionId, 'term'); openSession(a.sessionId); }}>${terminal ? t('agents.approval.open_terminal') : t('agents.cc.open')}</button>
      <button class="icon-btn" aria-label=${t('agents.approval.dismiss')} onClick=${() => dismissApproval(a).catch(e => toast(e.message, 'err'))}><${Icon} n="close" /></button>
    </div></li>`;
}

function SessionRow({ s, state }) {
  const activity = s.lastActivityAt || s.createdAt;
  return html`<li class=${cls('cm-session-row', 'st-' + state)}>
    <button class="cm-session-open" type="button" onClick=${() => openSession(s.id)}>
      <${CliMark} mode=${s.mode} size="sm" />
      <span class="cm-session-title">
        <b class="trunc" title=${s.name}>${s.name || s.id.slice(0, 8)}</b>
        ${s.workingDir && html`<small class="trunc" title=${s.workingDir}>${projectOf(s.workingDir)}</small>`}
      </span>
      <span class="cm-session-meta">
        <span class="cm-cc-state"><${StateDot} state=${state} /><span>${lt('state', state)}</span></span>
        ${activity && html`<time datetime=${new Date(activity).toISOString()} title=${new Date(activity).toLocaleString(locale())}>${ago(activity)}</time>`}
      </span>
    </button>
    ${state === 'working' && html`<button class="icon-btn cm-session-stop" type="button"
      aria-label=${t('agents.composer.interrupt')} title=${t('agents.composer.interrupt')}
      onClick=${() => interrupt(s.id).catch(e => toast(e.message, 'err'))}><${Icon} n="stop" /></button>`}
  </li>`;
}

function Sessions({ list, loaded }) {
  const [expanded, setExpanded] = useState(false);
  return html`<section class="cm-cc-sec">
    <h2>${t('agents.cc.board')}</h2>
    ${list.length ? html`<ul class="cm-session-list" id="cm-live-sessions">
      ${(expanded ? list : list.slice(0, PAGE_SIZE)).map(({ s, state }) => html`<${SessionRow} key=${s.id} s=${s} state=${state} />`)}
    </ul>` : html`<p class="cm-admin-empty">${loaded ? t('agents.rail.empty') : t('agents.conn.connecting')}</p>`}
    ${list.length > PAGE_SIZE && html`<button class="btn sm cm-more" aria-controls="cm-live-sessions"
      aria-expanded=${expanded} onClick=${() => setExpanded(!expanded)}>${expanded ? t('agents.cc.less') : t('agents.cc.more')}</button>`}
  </section>`;
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
      ...(innerWidth > 760 ? [{ label: t('agents.nav.grid'), icon: 'grid', run: () => go('agents', 'grid') }] : []),
      { label: t('agents.setup.change'), icon: 'plug', run: () => setChange(true) },
      { label: t('agents.setup.disconnect'), danger: true, run: disconnectServer },
    ]} />`}
    ${change && html`<${Modal} title=${t('agents.setup.change')} onClose=${() => setChange(false)}>
      <${ServerSetup} initial=${conn} onDone=${() => setChange(false)} />
    </${Modal}>`}
  </section>`;
}

function HarnessTable({ list, loaded }) {
  const clis = useStore(agents, s => s.clis);
  const manage = useStore(agents, s => s.settings.cliManagementEnabled === true && (!s.conn.multi_user || s.conn.role !== 'user'));
  const harnesses = clis.filter(c => c.kind !== 'shell');
  const counts = new Map();
  for (const { s, state } of list) {
    if (s.pid != null && !['error', 'stopped'].includes(state)) counts.set(s.mode, (counts.get(s.mode) || 0) + 1);
  }
  return html`<section class="cm-cc-sec">
    <h2>${t('agents.admin.harness')}</h2>
    ${!harnesses.length ? html`<p class="cm-admin-empty">${loaded ? t('agents.admin.no_registry') : t('agents.conn.connecting')}</p>`
      : html`<table class="cm-harness"><thead><tr>
        <th scope="col">${t('agents.admin.harness')}</th>
        <th scope="col" class="cm-harness-count">${t('agents.admin.active_sessions')}</th>
        <th scope="col" class="cm-harness-action">${t('agents.admin.installation')}</th>
      </tr></thead><tbody>${harnesses.map(c => html`<tr key=${c.id}>
        <th scope="row"><span class="cm-harness-name"><${CliMark} mode=${c.id} size="sm" /><span>${c.label}</span></span></th>
        <td class="cm-harness-count">${counts.get(c.id) || 0}</td>
        <td class="cm-harness-action">
          ${c.installed === true ? t('agents.caps.installed')
            : canInstall(c) ? html`<${InstallCli} cli=${c} manage=${manage} />`
            : c.installed === false ? t('agents.new.not_installed') : '—'}
        </td>
      </tr>`)}</tbody></table>`}
  </section>`;
}

const ORDER = ['waiting', 'error', 'limited', 'working', 'starting', 'idle', 'stopped'];
const timeOf = r => r.lastActivityAt || r.createdAt || 0;

function ResumeList({ sessions }) {
  const unified = useStore(agents, s => s.unified);
  const total = useStore(agents, s => s.historyTotal);
  const offset = useStore(agents, s => s.historyOffset);
  const error = useStore(agents, s => s.historyError);
  const [limit, setLimit] = useState(PAGE_SIZE);
  const [busy, setBusy] = useState(false);
  const rows = unified.filter(r => r.workingDir && !r.remote && !sessions[r.sessionId] && !r.sources?.includes('live'))
    .sort((a, b) => timeOf(b) - timeOf(a));
  const more = offset < total;
  const showMore = async () => {
    if (busy) return;
    if (limit >= rows.length && more) {
      setBusy(true);
      const loaded = await loadHistory(true);
      setBusy(false);
      if (!loaded) return;
    }
    setLimit(limit + PAGE_SIZE);
  };
  return html`<section class="cm-cc-sec">
    <h2>${t('agents.cc.resume')}</h2>
    ${error && html`<p class="msg-err" role="alert">${error}</p>`}
    ${rows.length ? html`<ul class="cm-resume" id="cm-resume-sessions">
      ${rows.slice(0, limit).map(r => html`<li key=${r.sessionId}><${HistoryRow} r=${r} /></li>`)}
    </ul>` : !error && html`<p class="cm-admin-empty">${total < 0 ? t('agents.conn.connecting') : t('agents.cc.no_history')}</p>`}
    ${rows.length === 0 && error && !more && html`<button class="btn sm cm-more" onClick=${() => loadHistory()}>${t('agents.cfg.retry')}</button>`}
    ${(rows.length > limit || more) && html`<button class="btn sm cm-more" disabled=${busy}
      aria-controls=${rows.length ? 'cm-resume-sessions' : undefined} onClick=${showMore}>${t('agents.cc.more')}</button>`}
  </section>`;
}

// Sessions coupées par un redémarrage de la machine (ou une coupure de courant) :
// Codeman garde le plan, Loom propose de les relancer d'un clic. Chaque CLI
// reprend sa conversation dans son dossier ; l'historique du terminal est perdu.
export function RestoreBanner() {
  const entries = useStore(agents, restorable);
  const [busy, setBusy] = useState('');
  if (!entries.length) return null;
  const run = async ids => {
    setBusy(ids ? ids[0] : 'all');
    try {
      const r = await api.restore(ids);
      const restored = (r && r.restored) || [], skipped = (r && r.skipped) || [];
      if (restored.length) toast(t('agents.restore.done', { n: restored.length }));
      for (const k of skipped) toast(t('agents.restore.skipped', { name: (entries.find(e => e.id === k.sessionId) || {}).name || k.sessionId, reason: lt('restoreReason', k.reason) }), 'err');
      await refreshAll();
      if (!ids) agents.set({ restore: [] });
    } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  const dismiss = async () => { setBusy('dismiss'); try { await api.dismissRestore(); agents.set({ restore: [] }); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); } };
  return html`<section class="cm-cc-sec cm-restore" aria-labelledby="cm-restore-title">
    <div class="cm-section-head"><h2 id="cm-restore-title">${t('agents.restore.title', { n: entries.length })}</h2>
      <span class="cm-restore-acts"><button class="btn sm" disabled=${!!busy} onClick=${dismiss}>${t('agents.restore.dismiss')}</button>
      <button class="btn sm primary" disabled=${!!busy} onClick=${() => run()}>${busy === 'all' ? t('agents.restore.working') : t('agents.restore.all')}</button></span></div>
    <p class="cm-restore-note">${t('agents.restore.text')}</p>
    <ul class="cm-cc-list">${entries.map(e => html`<li class="cm-restore-row" key=${e.id}>
      <${CliMark} mode=${e.mode} size="xs" />
      <span class="cm-restore-name"><b>${e.name || cliOf(e.mode).label}</b><small>${e.workingDir}</small></span>
      <button class="btn sm" disabled=${!!busy} onClick=${() => run([e.id])}>${busy === e.id ? t('agents.restore.working') : t('agents.restore.do')}</button>
    </li>`)}</ul>
  </section>`;
}

export function ControlCenter() {
  const order = useStore(agents, s => s.order);
  const sessions = useStore(agents, s => s.sessions);
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
  const list = order.map(id => sessions[id]).filter(Boolean).map(s => ({ s, state: stateOf(s) }))
    .sort((a, b) => ORDER.indexOf(a.state) - ORDER.indexOf(b.state) || timeOf(b.s) - timeOf(a.s));
  const working = list.filter(({ state }) => state === 'working' || state === 'starting').length;
  const pending = approvals.filter(a => a.kind !== 'idle');
  const counts = loaded ? [t('agents.admin.sessions', { n: list.length }), t('agents.admin.working', { n: working }), t('agents.admin.waiting', { n: pending.length })] : [];
  if (jobs) counts.push(t('agents.admin.scheduled', { n: jobs.filter(j => j.enabled).length }));
  const next = (jobs || []).filter(j => j.enabled && j.nextRunAt).sort((a, b) => a.nextRunAt - b.nextRunAt).slice(0, 4);
  const fmt = ms => new Date(ms).toLocaleString(locale(), { weekday: 'short', hour: '2-digit', minute: '2-digit' });
  return html`<div class="cm-scroll"><div class="cm-cc">
    <${RestoreBanner} />
    <${Sessions} list=${list} loaded=${loaded} />
    <${ServerCard} key=${conn.url} counts=${counts} update=${update} />
    <section class="cm-cc-sec">
      <h2>${t('agents.cc.attention')}</h2>
      ${approvals.length ? html`<ul class="cm-cc-list">${[...pending, ...approvals.filter(a => a.kind === 'idle')].map(a => html`<${ApprovalRow} key=${a.id} a=${a} />`)}</ul>`
        : html`<p class="cm-admin-empty">${loaded ? t('agents.cc.nothing') : t('agents.conn.connecting')}</p>`}
    </section>
    <${HarnessTable} list=${list} loaded=${loaded} />
    <section class="cm-cc-sec">
      <div class="cm-section-head"><h2>${t('agents.cc.next')}</h2><a href="#/agents/cron">${t('agents.cc.all_cron')}</a></div>
      ${error && html`<p class="msg-err">${error}</p>`}
      ${next.length ? html`<ul class="cm-next-runs">${next.map(j => html`<li key=${j.id}><b>${j.name}</b><span>${cliOf(j.agentType).label} · ${schedule(j)}</span><time>${fmt(j.nextRunAt)}</time></li>`)}</ul>`
        : !error && html`<p class="cm-admin-empty">${jobs ? t('agents.cc.no_next') : t('agents.conn.connecting')}</p>`}
    </section>
    <${ResumeList} sessions=${sessions} />
  </div></div>`;
}
