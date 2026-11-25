import { ContextItems, ContextNav } from './sections.js';
import { useMedia } from '../core/viewport.js';
import { Activity } from './activity.js';
import { t } from '../core/i18n.js';
import { html, useEffect, useRef, useState, useStore } from '../core/lib.js';
import { Empty } from '../ui/controls.js';
import { Icon } from '../ui/icons.js';
import { toast } from '../ui/dialog.js';
import { agents, cliOf, recentCompletionMs, refreshAll, refreshConnection, restorable, stateOf } from '../features/agents/store.js';
import { flattenPrompt, sendPrompt } from '../features/agents/client.js';
import { dismissApproval } from '../features/agents/actions.js';
import { Approval } from '../features/agents/conversation.js';
import { RestoreBanner } from '../features/agents/control.js';
import { CliMark } from '../features/agents/new-session.js';
import { openSession } from '../features/agents/nav.js';
import { ago } from '../features/agents/rail.js';
import { usePullRefresh } from './touch.js';
import { loomApprovalTitle, loomApprovals, LoomApproval, refreshLoomApprovals } from './approvals.js';

export function inboxItems(st) {
  const approvals = st.approvals.filter(a => a.id && a.sessionId && a.kind !== 'idle');
  const blocked = new Set(approvals.map(a => a.sessionId));
  const replies = new Map();
  for (const a of st.approvals) {
    if (a.id && a.kind === 'idle' && a.sessionId && !blocked.has(a.sessionId) &&
        (!replies.has(a.sessionId) || a.createdAt > replies.get(a.sessionId).createdAt)) replies.set(a.sessionId, a);
  }
  return { approvals, replies: [...replies.values()] };
}

export async function refreshAgentInbox() {
  if (await refreshConnection() === 'ready') await refreshAll();
}

function InboxTitle({ item, session, detail }) {
  const open = () => openSession(item.sessionId);
  const name = session?.name || item.sessionName || item.sessionId.slice(0, 8);
  const icon = session ? html`<${CliMark} mode=${session.mode} size="xs" />` : html`<${Icon} n="agent" />`;
  return html`<div class="inbox-item-head">
    ${detail ? html`<div class="inbox-session-name">${icon}<b class="trunc">${name}</b></div>`
      : html`<button type="button" class="phone-row-open" onClick=${open}>${icon}<b class="trunc">${name}</b></button>`}
    ${item.createdAt && html`<small>${ago(item.createdAt)}</small>`}
    ${detail && html`<button class="btn sm" onClick=${open}>${t('noir.open_session')}</button>`}
  </div>`;
}

function QuickReply({ item, session, ready }) {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const inflight = useRef(false);
  const send = async e => {
    e.preventDefault();
    const current = agents.get();
    if (inflight.current || current.conn.state !== 'ready' || stateOf(current.sessions[item.sessionId]) !== 'idle' ||
        !flattenPrompt(text.trim()) || !inboxItems(current).replies.some(a => a.id === item.id)) return;
    inflight.current = true; setBusy(true);
    try {
      await sendPrompt(item.sessionId, text.trim());
      setText('');
      await dismissApproval(item);
    } catch (err) {
      toast(err.code === 'NOT_DELIVERED' ? t('agents.composer.not_delivered') : err.message, 'err');
    } finally {
      inflight.current = false; setBusy(false);
    }
  };
  return html`<article class="inbox-card">
    <${InboxTitle} item=${item} session=${session} />
    <p>${item.message || item.toolSummary || t('mobile.reply_waiting')}</p>
    ${ready && html`<form class="composer inbox-reply" onSubmit=${send}>
      <input aria-label=${t('mobile.reply')} placeholder=${t('mobile.reply')} value=${text} onInput=${e => setText(e.target.value)} disabled=${busy} enterkeyhint="send" />
      <button type="submit" class="send" aria-label=${t('agents.composer.send')} disabled=${busy || !text.trim()}><${Icon} n="arrowUp" /></button>
    </form>`}
    <button type="button" class="btn sm inbox-dismiss" title=${t('agents.approval.dismiss_hint')} disabled=${busy} onClick=${() => dismissApproval(item).catch(e => toast(e.message, 'err'))}>${t('agents.approval.dismiss')}</button>
  </article>`;
}

export function useCompletionNotices() {
  const completed = useStore(agents, s => s.completed);
  const notified = useRef(new Map());
  useEffect(() => {
    const seen = notified.current;
    for (const id of seen.keys()) if (!completed[id]) seen.delete(id);
    for (const [id, at] of Object.entries(completed)) {
      const session = agents.get().sessions[id];
      if (!session || at - (seen.get(id) || 0) < 8000) continue;
      seen.set(id, at);
      if (document.hidden || Date.now() - at > 10000) continue;
      toast(session.name || id.slice(0, 8), '', {
        icon: 'check', title: t('noir.finished_notice'), href: '#/agents/s/' + encodeURIComponent(id), label: t('app.palette.ouvrir'),
      });
    }
  }, [completed]);
}

export function InboxPage({ route }) {
  const desktop = useMedia('(min-width: 860px)');
  const st = useStore(agents, s => ({ approvals: s.approvals, sessions: s.sessions, completed: s.completed, restore: s.restore, conn: s.conn, loaded: s.loaded }));
  const { approvals, replies } = inboxItems(st);
  const needsReply = new Set([...approvals, ...replies].map(a => a.sessionId));
  const completed = Object.entries(st.completed).filter(([id, at]) =>
    st.sessions[id] && !needsReply.has(id) && Date.now() - at < recentCompletionMs &&
    ['idle', 'stopped'].includes(stateOf(st.sessions[id]))).sort(([, a], [, b]) => b - a);
  const waiting = useStore(loomApprovals, s => s.items);
  const restore = restorable(st);
  const ready = st.conn.state === 'ready';
  const list = useRef();
  const pull = usePullRefresh(() => Promise.all([refreshAgentInbox(), refreshLoomApprovals()]));
  const forAgent = (item, kind) => {
    const session = st.sessions[item.sessionId];
    return {
      id: item.id, kind, item, session, label: item.toolSummary || item.message || (kind === 'reply' ? t('mobile.reply_waiting') : t('agents.approval.permission')),
      detail: [session && cliOf(session.mode).label, session?.name || item.sessionName].filter(Boolean).join(' · '), at: item.createdAt,
    };
  };
  const pending = [
    ...waiting.map(item => ({ id: 'loom:' + item.request_id, kind: 'loom', item, label: loomApprovalTitle(item), detail: item.detail })),
    ...approvals.map(item => forAgent(item, 'approval')),
    ...replies.map(item => forAgent(item, 'reply')),
  ];
  const news = completed.map(([sessionId, at]) => ({
    id: 'done:' + sessionId, kind: 'done', sessionId, at, label: t('noir.finished_notice'),
    detail: st.sessions[sessionId].name || sessionId.slice(0, 8),
  }));
  const selected = [...pending, ...news].find(record => record.id === route.sub) || pending[0] || news[0];
  const active = selected && selected.id === route.sub ? selected.id : null;
  useEffect(() => {
    if (!active) return;
    const target = list.current?.querySelector('[data-current]');
    target?.scrollIntoView({ block: 'nearest' });
    target?.focus({ preventScroll: true });
  }, [active, desktop]);
  const linkItems = records => records.map(record => ({
    href: '#/inbox/' + encodeURIComponent(record.id), label: record.label, detail: record.detail,
    meta: record.at ? ago(record.at) : '', icon: record.kind === 'done' ? 'check' : 'inbox',
    current: selected?.id === record.id,
  }));
  const clearNews = () => {
    const next = { ...agents.get().completed };
    for (const item of news) delete next[item.sessionId];
    agents.set({ completed: next });
  };
  const doneAction = news.length > 0 && html`<button class="btn sm" onClick=${clearNews}>${t('noir.mark_read')}</button>`;
  const render = record => html`<article key=${record.id} class="inbox-item" tabindex="-1" data-current=${record.id === active ? '' : undefined}>
    ${record.kind === 'loom' ? html`<${LoomApproval} item=${record.item} class="inbox-card" />`
      : record.kind === 'approval' ? html`<${Approval} item=${record.item} session=${record.session} disabled=${!ready}
        onTerminal=${() => openSession(record.item.sessionId, 'term')}><${InboxTitle} item=${record.item} session=${record.session} detail=${desktop} /></${Approval}>`
      : record.kind === 'reply' ? html`<${QuickReply} item=${record.item} session=${record.session}
        ready=${ready && stateOf(record.session) === 'idle'} />`
      : html`<a class="inbox-card phone-link" href=${'#/agents/s/' + encodeURIComponent(record.sessionId)}>
        <${Icon} n="check" class="inbox-done" /><span><b>${record.detail}</b><small>${ago(record.at)}</small></span><${Icon} n="right" /></a>`}
  </article>`;
  return html`<div class="view with-context inbox-view">
    <${ContextNav} title=${t('mobile.inbox')} action=${doneAction}>
      ${pending.length > 0 && html`<${ContextItems} title=${t('noir.inbox_pending')} items=${linkItems(pending)} />`}
      ${news.length > 0 && html`<${ContextItems} title=${t('noir.inbox_news')} items=${linkItems(news)} />`}
    </${ContextNav}>
    <div class="context-content phone-page">
      <header class="phone-title"><h1>${desktop && selected ? selected.label : t('mobile.inbox')}</h1>
        ${!desktop && doneAction}<${Activity} /></header>
      <div class="phone-scroll inbox-list" ref=${list} ...${pull}>
        ${st.conn.configured && !ready && html`<a class="inbox-card phone-link" href="#/agents"><${Icon} n="alert" /><span>${t('mobile.inbox_unavailable')}</span><${Icon} n="right" /></a>`}
        ${desktop ? selected && render(selected) : pending.map(render)}
        ${!desktop && news.length > 0 && html`<section class="inbox-completed"><h2>${t('noir.inbox_news')}</h2>${news.map(render)}</section>`}
        ${ready && restore.length > 0 && html`<${RestoreBanner} />`}
        ${!pending.length && !news.length && !restore.length &&
          (st.conn.state === 'idle' || ready && !st.loaded ? html`<div class="skeleton inbox-loading"></div>`
            : html`<${Empty} icon="inbox" title=${t('mobile.inbox_empty')} />`)}
      </div>
    </div>
  </div>`;
}
