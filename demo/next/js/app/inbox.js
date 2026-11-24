import { t } from '../core/i18n.js';
import { html, useEffect, useRef, useState, useStore } from '../core/lib.js';
import { Empty } from '../ui/controls.js';
import { Icon } from '../ui/icons.js';
import { toast } from '../ui/dialog.js';
import { agents, recentCompletionMs, refreshAll, refreshConnection, restorable, stateOf } from '../features/agents/store.js';
import { flattenPrompt, sendPrompt } from '../features/agents/client.js';
import { dismissApproval } from '../features/agents/actions.js';
import { Approval } from '../features/agents/conversation.js';
import { RestoreBanner } from '../features/agents/control.js';
import { CliMark } from '../features/agents/new-session.js';
import { openSession } from '../features/agents/nav.js';
import { ago } from '../features/agents/rail.js';
import { usePullRefresh } from './touch.js';
import { loomApprovals, LoomApproval, refreshLoomApprovals } from './approvals.js';

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

function InboxTitle({ item, session }) {
  return html`<div class="inbox-item-head">
    <button type="button" class="phone-row-open" onClick=${() => openSession(item.sessionId)}>
      ${session ? html`<${CliMark} mode=${session.mode} size="xs" />` : html`<${Icon} n="agent" />`}
      <b class="trunc">${session?.name || item.sessionName || item.sessionId.slice(0, 8)}</b>
    </button>
    ${item.createdAt && html`<small>${ago(item.createdAt)}</small>`}
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

export function InboxPage({ route }) {
  const st = useStore(agents, s => ({ approvals: s.approvals, sessions: s.sessions, completed: s.completed, restore: s.restore, conn: s.conn, loaded: s.loaded }));
  const { approvals, replies } = inboxItems(st);
  const needsReply = new Set([...approvals, ...replies].map(a => a.sessionId));
  const completed = Object.entries(st.completed).filter(([id, at]) =>
    st.sessions[id] && !needsReply.has(id) && Date.now() - at < recentCompletionMs &&
    ['idle', 'stopped'].includes(stateOf(st.sessions[id]))).sort(([, a], [, b]) => b - a);
  const restore = restorable(st);
  const waiting = useStore(loomApprovals, s => s.items);
  const ready = st.conn.state === 'ready';
  const matched = approvals.some(a => a.id === route.sub) || replies.some(a => a.id === route.sub);
  const list = useRef();
  const pull = usePullRefresh(() => Promise.all([refreshAgentInbox(), refreshLoomApprovals()]));
  useEffect(() => {
    if (!matched) return;
    const target = list.current?.querySelector('[data-current]');
    target?.scrollIntoView({ block: 'nearest' });
    target?.focus({ preventScroll: true });
  }, [route.sub, matched]);
  return html`<div class="view phone-page">
    <header class="phone-title"><h1>${t('mobile.inbox')}</h1></header>
    <div class="phone-scroll inbox-list" ref=${list} ...${pull}>
      ${st.conn.configured && !ready && html`<a class="inbox-card phone-link" href="#/agents"><${Icon} n="alert" /><span>${t('mobile.inbox_unavailable')}</span><${Icon} n="right" /></a>`}
      ${waiting.map(item => html`<div key=${item.request_id} class="inbox-item"><${LoomApproval} item=${item} class="inbox-card" /></div>`)}
      ${approvals.map(item => html`<div key=${item.id} class="inbox-item" tabindex="-1" data-current=${item.id === route.sub ? '' : undefined}>
        <${Approval} item=${item} session=${st.sessions[item.sessionId]} disabled=${!ready} onTerminal=${() => openSession(item.sessionId, 'term')}>
          <${InboxTitle} item=${item} session=${st.sessions[item.sessionId]} />
        </${Approval}>
      </div>`)}
      ${replies.map(item => html`<div key=${item.id} class="inbox-item" tabindex="-1" data-current=${item.id === route.sub ? '' : undefined}>
        <${QuickReply} item=${item} session=${st.sessions[item.sessionId]} ready=${ready && stateOf(st.sessions[item.sessionId]) === 'idle'} />
      </div>`)}
      ${completed.length > 0 && html`<section class="inbox-completed"><h2>${t('mobile.completed')}</h2>
        ${completed.map(([id, at]) => html`<a key=${id} class="inbox-card phone-link" href=${'#/agents/s/' + encodeURIComponent(id)}>
          <${Icon} n="check" class="inbox-done" /><span><b>${st.sessions[id].name || id.slice(0, 8)}</b><small>${ago(at)}</small></span><${Icon} n="right" />
        </a>`)}
      </section>`}
      ${ready && restore.length > 0 && html`<${RestoreBanner} />`}
      ${!waiting.length && !approvals.length && !replies.length && !completed.length && !restore.length &&
        (st.conn.state === 'idle' || ready && !st.loaded ? html`<div class="skeleton inbox-loading"></div>`
          : html`<${Empty} icon="inbox" title=${t('mobile.inbox_empty')} />`)}
    </div>
  </div>`;
}
