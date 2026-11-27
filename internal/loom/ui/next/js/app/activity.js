import { t, tSource } from '../core/i18n.js';
// Centre de notifications et d'activité : demandes d'accord (Loom + Codeman),
// réponses terminées hors vue (Discussion et Agents), tâches en cours, alertes
// de quotas et mises à jour disponibles (Loom, Codeman, agents, moteur local).
import { html, useState, useEffect, useRef, useMemo, useStore, cls } from '../core/lib.js';
import { loomApprovals, LoomApproval } from './approvals.js';
import { Icon } from '../ui/icons.js';
import { Popover } from '../ui/controls.js';
import { get, post, request } from '../core/api.js';
import { app, go } from '../core/state.js';
import { visibleRefresh } from '../core/poll.js';
import { shortVersion } from '../core/version.js';
import { notifications, notifyChatReply, clearChatReply, clearAllChatReplies, dismissNotice } from '../core/notifications.js';
import { agents, attentionOf, stateOf, sessionList, markSeen, cliOf } from '../features/agents/store.js';
import { api as agentsApi } from '../features/agents/client.js';
import { openSession } from '../features/agents/nav.js';
import { ago } from '../features/agents/rail.js';
import { chat, open as openChat } from '../features/chat/engine.js';

const pct = (a, b) => (b > 0 ? Math.min(100, Math.round(a * 100 / b)) : null);
const ACCOUNTS = { claude: 'Claude', codex: 'Codex', antigravity: 'Antigravity', copilot: 'Copilot' };

async function readTasks() {
  const [dl, job, bench] = await Promise.all([
    get('/api/models/download/status', { timeout: 6000, retryAuth: false }).catch(() => null),
    get('/api/llamacpp/job', { timeout: 6000, retryAuth: false }).catch(() => null),
    get('/api/bench/queue?compact=1', { timeout: 6000, retryAuth: false }).catch(() => null),
  ]);
  const items = [];
  for (const d of Array.isArray(dl) ? dl : []) {
    if (d.finished || d.canceled) continue;
    const go1 = n => (n / 1073741824).toFixed(1);
    items.push({ id: 'dl:' + d.filename, icon: 'download', title: d.filename, err: d.error,
      sub: d.error || (d.total ? `${go1(d.done)} / ${go1(d.total)} Go` + (d.speed ? ` · ${(d.speed / 1048576).toFixed(0)} Mo/s` : '') : t("app.activity.demarrage")),
      p: pct(d.done, d.total), cancel: d.error ? null : () => post('/api/models/download/cancel', { filename: d.filename }), to: () => go('local', 'hub') });
  }
  if (job && job.exists) {
    const label = { install: t("app.activity.installation_de_llama_cpp"), update: t("app.activity.mise_a_jour_de_llama_cpp"), prebuilt: t("app.activity.telechargement_de_llama_cpp") }[job.action] || 'llama.cpp';
    items.push({ id: 'job', icon: 'server', title: label, err: !job.running && job.error ? job.error : '',
      sub: job.running ? (tSource(job.phase) || t("app.activity.en_cours")) : job.error ? job.error : t("app.activity.termine"),
      p: null, cancel: job.running ? null : () => post('/api/llamacpp/job/dismiss', {}), cancelLabel: t("app.activity.masquer"), to: () => go('engine') });
  }
  const b = bench && bench.job;
  if (b && b.status === 'running') {
    const done = (b.rows || []).filter(r => ['ok', 'err', 'skip'].includes(r.status)).length;
    const title = [t('app.activity.bench_queue'), b.test_name].filter(Boolean).join(' · ');
    const cancel = b.id ? () => request('/api/bench/queue/cancel', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Loom-Bench-Job': b.id }, body: '{}',
    }).catch(() => null) : null;
    items.push({ id: 'bench', icon: 'gauge', title, sub: t("app.activity.v0_v1_modeles", { v0: done, v1: (b.rows || []).length }),
      p: pct(done, (b.rows || []).length), cancel, cancelLabel: t("app.activity.arreter") });
  }
  return items;
}

async function readQuotaAlerts() {
  const r = await get('/api/quotas', { timeout: 15000, retryAuth: false }).catch(() => null);
  const alerts = [];
  for (const acc of r?.accounts || []) {
    const name = ACCOUNTS[acc.id] || acc.id;
    let worst = null;
    for (const l of acc.limits || []) {
      const used = Math.round(Number(l.used) || 0);
      if (used >= 85 && (!worst || used > worst.used)) {
        worst = { id: l.id || l.window || 'limit', used, label: l.label || l.window || '' };
      }
    }
    if (worst) {
      alerts.push({
        id: `quota:${acc.id}:${worst.id}:${worst.used >= 100 ? 'out' : 'hi'}`,
        account: name,
        used: worst.used,
        label: worst.label,
        critical: worst.used >= 100,
      });
    }
  }
  return alerts;
}

async function readUpdates(codemanReady) {
  const updates = [];
  const [loomUpd, lcStatus, cmUpd] = await Promise.all([
    get('/api/update', { timeout: 10000, retryAuth: false }).catch(() => null),
    get('/api/llamacpp', { timeout: 8000, retryAuth: false }).catch(() => null),
    codemanReady ? agentsApi.updateCheck().catch(() => null) : Promise.resolve(null),
  ]);
  if (loomUpd?.available && loomUpd.latest) {
    updates.push({
      id: 'upd:loom:' + loomUpd.latest,
      icon: 'download',
      title: t('notif.update_loom', { v: shortVersion(loomUpd.latest) }),
      sub: t('notif.update_available'),
      to: () => go('settings', 'about'),
    });
  }
  if (cmUpd?.updateAvailable && cmUpd.latestVersion) {
    updates.push({
      id: 'upd:codeman:' + cmUpd.latestVersion,
      icon: 'agent',
      title: t('notif.update_codeman', { v: shortVersion(cmUpd.latestVersion) }),
      sub: t('notif.update_available'),
      to: () => go('agents'),
    });
  }
  if (lcStatus?.installed && lcStatus?.can_update) {
    const checkURL = lcStatus.update_kind === 'prebuilt' ? '/api/llamacpp/prebuilt/check' : '/api/llamacpp/check';
    const lcCheck = await get(checkURL, { timeout: 10000, retryAuth: false }).catch(() => null);
    const hasEngineUpd = !!(lcCheck?.available || lcCheck?.update_available || (lcCheck?.behind > 0));
    const latest = lcCheck?.latest || lcCheck?.tag || (lcCheck?.behind > 0 ? `+${lcCheck.behind}` : '');
    if (hasEngineUpd) {
      updates.push({
        id: 'upd:engine:' + (latest || 'new'),
        icon: 'chip',
        title: t('notif.update_engine', { v: latest ? shortVersion(String(latest)) : '' }).trim(),
        sub: t('notif.update_available'),
        to: () => go('engine'),
      });
    }
  }
  return updates;
}

export function Activity() {
  const [tasks, setTasks] = useState([]);
  const [quotas, setQuotas] = useState([]);
  const [updates, setUpdates] = useState([]);
  const [anchor, setAnchor] = useState(null);
  const btn = useRef();

  const route = useStore(app, s => s.route);
  const approvals = useStore(loomApprovals, s => s.items);
  const chatReplies = useStore(notifications, s => s.chatReplies);
  const dismissed = useStore(notifications, s => s.dismissed);
  const agentState = useStore(agents, s => ({
    sessions: s.sessions,
    order: s.order,
    approvals: s.approvals,
    completed: s.completed,
    seen: s.seen,
    clis: s.clis,
    ready: s.conn.state === 'ready',
  }));

  // Suit les fins de génération en mode Discussion hors vue et nettoie au retour.
  const chatBusy = useStore(chat, s => ({ busy: s.busy, replaying: s.replaying, sessionId: s.sessionId, title: s.session?.title || '' }));
  const wasBusy = useRef(false);
  useEffect(() => {
    if (wasBusy.current && !chatBusy.busy && !chatBusy.replaying) {
      const r = app.get().route;
      if (r.section !== 'chat' || document.hidden) {
        const title = chatBusy.title
          || app.get().nav?.conversations?.find(c => c.id === chatBusy.sessionId)?.title
          || '';
        notifyChatReply({ id: chatBusy.sessionId || 'native', title });
      }
    }
    wasBusy.current = !!chatBusy.busy && !chatBusy.replaying;
  }, [chatBusy.busy, chatBusy.replaying, chatBusy.sessionId]);

  useEffect(() => {
    if (route.section === 'chat' && !document.hidden) {
      clearChatReply(chatBusy.sessionId || 'native');
    }
  }, [route.section, route.sub, chatBusy.sessionId]);

  useEffect(() => visibleRefresh(async alive => {
    const r = await readTasks(); if (alive()) setTasks(r);
  }, 3000), []);

  useEffect(() => visibleRefresh(async alive => {
    const q = await readQuotaAlerts(); if (alive()) setQuotas(q);
  }, 60000), []);

  useEffect(() => visibleRefresh(async alive => {
    const u = await readUpdates(agentState.ready); if (alive()) setUpdates(u);
  }, 300000), [agentState.ready]);

  const dismissedSet = useMemo(() => new Set(dismissed), [dismissed]);

  const agentApprovals = useMemo(
    () => (agentState.approvals || []).filter(a => a && a.kind !== 'idle'),
    [agentState.approvals],
  );

  const unseenAgents = useMemo(() => {
    return sessionList()
      .filter(s => attentionOf(s) === 'unseen')
      .map(s => ({
        id: 'agent-done:' + s.id,
        sessionId: s.id,
        title: s.name || cliOf(s.mode).label,
        sub: t('notif.agent_done', { cli: cliOf(s.mode).label }),
        at: agentState.completed[s.id] || s.lastActivityAt || 0,
      }))
      .sort((a, b) => b.at - a.at);
  }, [agentState.sessions, agentState.order, agentState.completed, agentState.seen]);

  const limitedAgents = useMemo(() => {
    return sessionList()
      .filter(s => stateOf(s) === 'limited')
      .map(s => ({
        id: 'agent-limited:' + s.id,
        sessionId: s.id,
        title: s.name || cliOf(s.mode).label,
        sub: t('notif.quota_paused'),
      }));
  }, [agentState.sessions, agentState.order]);

  const cliUpdates = useMemo(() => {
    const out = [];
    for (const c of agentState.clis || []) {
      const next = c.latestVersion && c.version && c.latestVersion !== c.version ? c.latestVersion : '';
      if (c.installed !== false && (c.updateAvailable || next)) {
        const id = 'upd:cli:' + c.id + ':' + (next || 'new');
        if (!dismissedSet.has(id)) {
          out.push({
            id,
            icon: 'agent',
            title: t('notif.update_cli', { name: c.label || c.id, v: next ? shortVersion(next) : '' }).trim(),
            sub: t('notif.update_available'),
            to: () => go('agents', 'settings'),
          });
        }
      }
    }
    return out;
  }, [agentState.clis, dismissedSet]);

  const activeUpdates = useMemo(
    () => [...updates.filter(u => !dismissedSet.has(u.id)), ...cliUpdates],
    [updates, cliUpdates, dismissedSet],
  );

  const activeQuotas = useMemo(
    () => quotas.filter(q => !dismissedSet.has(q.id)),
    [quotas, dismissedSet],
  );

  const running = tasks.filter(i => !i.err).length;
  const urgentCount = approvals.length + agentApprovals.length + limitedAgents.length + activeQuotas.filter(q => q.critical).length;
  const repliesCount = chatReplies.length + unseenAgents.length;
  const totalCount = urgentCount + repliesCount + tasks.length + activeQuotas.filter(q => !q.critical).length + activeUpdates.length;
  const hasClearable = repliesCount > 0 || activeUpdates.length > 0 || activeQuotas.length > 0;

  const markAllRead = () => {
    clearAllChatReplies();
    for (const s of unseenAgents) markSeen(s.sessionId);
    for (const u of activeUpdates) dismissNotice(u.id);
    for (const q of activeQuotas) dismissNotice(q.id);
  };

  return html`<button class=${cls('icon-btn act-btn', running && 'busy', urgentCount > 0 && 'waiting', totalCount > 0 && 'has-items')} ref=${btn}
      aria-label=${t("notif.title") + (totalCount ? ' · ' + totalCount : '')} title=${t("notif.title")}
      onClick=${() => setAnchor(anchor ? null : btn.current)}>
      <${Icon} n="bell" />
      ${totalCount > 0 && html`<i class="act-dot"></i>`}
    </button>
    ${anchor && html`<${Popover} sheet anchor=${anchor} onClose=${() => setAnchor(null)} label=${t("notif.title")} width=${356} class="act-pop">
      <div class="act-head">
        <span>${t("notif.title")}${totalCount > 0 && html` <em class="act-badge">${totalCount}</em>`}</span>
        ${hasClearable && html`<button type="button" class="act-clear" onClick=${markAllRead}>${t('notif.mark_read')}</button>`}
      </div>
      <div class="act-scroll">
        ${approvals.map(a => html`<${LoomApproval} key=${a.request_id} item=${a} class="act-approval" />`)}

        ${agentApprovals.length > 0 && html`<div class="act-group">
          <div class="act-group-h">${t('notif.sec_attention')}</div>
          ${agentApprovals.map(a => {
            const s = agentState.sessions[a.sessionId];
            return html`<div class="act-row act-tone-warn" key=${'ap:' + a.id}>
              <${Icon} n=${a.kind === 'question' ? 'info' : 'lock'} />
              <button type="button" class="act-main" onClick=${() => { setAnchor(null); openSession(a.sessionId); }}>
                <b>${s?.name || cliOf(s?.mode).label}</b>
                <small>${a.message || a.toolSummary || t('agents.tray.waiting')}</small>
              </button>
            </div>`;
          })}
        </div>`}

        ${repliesCount > 0 && html`<div class="act-group">
          <div class="act-group-h">${t('notif.sec_replies')}</div>
          ${chatReplies.map(c => html`<div class="act-row" key=${'chat:' + c.id}>
            <${Icon} n="chat" />
            <button type="button" class="act-main" onClick=${() => {
              setAnchor(null);
              clearChatReply(c.id);
              go('chat');
              if (c.id && c.id !== 'native') openChat(c.id);
            }}>
              <b>${c.title || t('notif.chat_reply_untitled')}</b>
              <small>${[t('notif.chat_done'), c.at ? ago(c.at) : ''].filter(Boolean).join(' · ')}</small>
            </button>
            <button type="button" class="icon-btn act-dismiss" aria-label=${t('app.activity.masquer')} onClick=${() => clearChatReply(c.id)}><${Icon} n="close" /></button>
          </div>`)}
          ${unseenAgents.map(a => html`<div class="act-row" key=${a.id}>
            <${Icon} n="agent" />
            <button type="button" class="act-main" onClick=${() => { setAnchor(null); markSeen(a.sessionId); openSession(a.sessionId); }}>
              <b>${a.title}</b>
              <small>${[a.sub, a.at ? ago(a.at) : ''].filter(Boolean).join(' · ')}</small>
            </button>
            <button type="button" class="icon-btn act-dismiss" aria-label=${t('app.activity.masquer')} onClick=${() => markSeen(a.sessionId)}><${Icon} n="close" /></button>
          </div>`)}
        </div>`}

        ${tasks.length > 0 && html`<div class="act-group">
          <div class="act-group-h">${t('notif.sec_tasks')}</div>
          ${tasks.map(i => {
            const row = i.to ? 'button' : 'div';
            const open = i.to && (() => { setAnchor(null); i.to(); });
            return html`<div class="act-row" key=${i.id}>
              <${Icon} n=${i.icon} />
              <${row} type=${i.to ? 'button' : undefined} class="act-main" onClick=${open}><b>${i.title}</b><small class=${i.err ? 'err' : ''}>${i.sub}</small>
                ${i.p != null && !i.err && html`<span class="gauge"><i style=${`width:${i.p}%`}></i></span>`}</${row}>
              ${i.cancel && html`<button class="btn sm" onClick=${async () => { await i.cancel(); setTasks(await readTasks()); }}>${i.cancelLabel || t("app.activity.annuler")}</button>`}
            </div>`;
          })}
        </div>`}

        ${(activeQuotas.length > 0 || limitedAgents.length > 0) && html`<div class="act-group">
          <div class="act-group-h">${t('notif.sec_quotas')}</div>
          ${limitedAgents.map(a => html`<div class="act-row act-tone-warn" key=${a.id}>
            <${Icon} n="gauge" />
            <button type="button" class="act-main" onClick=${() => { setAnchor(null); openSession(a.sessionId); }}>
              <b>${a.title}</b>
              <small>${a.sub}</small>
            </button>
          </div>`)}
          ${activeQuotas.map(q => html`<div class=${cls('act-row', q.critical ? 'act-tone-err' : 'act-tone-warn')} key=${q.id}>
            <${Icon} n="gauge" />
            <button type="button" class="act-main" onClick=${() => { setAnchor(null); go('activity'); }}>
              <b>${t('notif.quota_title', { name: q.account, pct: q.used })}</b>
              <small>${q.critical ? t('notif.quota_out') : t('notif.quota_high', { win: q.label || '' }).trim()}</small>
            </button>
            <button type="button" class="icon-btn act-dismiss" aria-label=${t('app.activity.masquer')} onClick=${() => dismissNotice(q.id)}><${Icon} n="close" /></button>
          </div>`)}
        </div>`}

        ${activeUpdates.length > 0 && html`<div class="act-group">
          <div class="act-group-h">${t('notif.sec_updates')}</div>
          ${activeUpdates.map(u => html`<div class="act-row" key=${u.id}>
            <${Icon} n=${u.icon} />
            <button type="button" class="act-main" onClick=${() => { setAnchor(null); u.to(); }}>
              <b>${u.title}</b>
              <small>${u.sub}</small>
            </button>
            <button type="button" class="icon-btn act-dismiss" aria-label=${t('app.activity.masquer')} onClick=${() => dismissNotice(u.id)}><${Icon} n="close" /></button>
          </div>`)}
        </div>`}

        ${totalCount === 0 && html`<div class="act-empty">${t("notif.empty")}</div>`}
      </div>
    </${Popover}>`}`;
}
