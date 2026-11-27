import { t } from '../core/i18n.js';
import { html, useEffect, useRef, useStore, cls } from '../core/lib.js';
import { Empty } from '../ui/controls.js';
import { Icon } from '../ui/icons.js';
import { agents, recentCompletionMs, refreshAll, refreshConnection, restorable, stateOf } from '../features/agents/store.js';
import { lt } from '../features/agents/labels.js';
import { RestoreBanner } from '../features/agents/control.js';
import { CliMark } from '../features/agents/new-session.js';
import { openSession } from '../features/agents/nav.js';
import { ago } from '../features/agents/rail.js';
import { usePullRefresh } from './touch.js';
import { BurgerButton } from './phone-nav.js';
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

// Un élément d'« À traiter » ouvre la conversation, où l'on répond : accord,
// question ou message, avec tout le contexte du fil.
function InboxRow({ item, session, kind, current }) {
  const label = kind === 'reply' ? t('agents.state.reply') : lt('state', kind);
  return html`<button type="button" class="inbox-card inbox-row" tabindex="0" data-current=${current ? '' : undefined}
      onClick=${() => openSession(item.sessionId)}>
    <span class="inbox-row-h">
      ${session ? html`<${CliMark} mode=${session.mode} size="xs" />` : html`<${Icon} n="agent" />`}
      <b class="trunc">${session?.name || item.sessionName || item.sessionId.slice(0, 8)}</b>
      <small class=${'ses-flag st-' + kind}>${label}</small>
      <span class="grow"></span>
      ${item.createdAt && html`<small class="inbox-when">${ago(item.createdAt)}</small>`}
      <${Icon} n="right" />
    </span>
    ${(item.toolSummary || item.message) && html`<span class="inbox-row-text">${item.toolSummary || item.message}</span>`}
  </button>`;
}

export function InboxPage({ route }) {
  const st = useStore(agents, s => ({ approvals: s.approvals, sessions: s.sessions, completed: s.completed, seen: s.seen, restore: s.restore, conn: s.conn, loaded: s.loaded }));
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
    <header class="phone-title"><${BurgerButton} /><h1>${t('mobile.inbox')}</h1></header>
    <div class="phone-scroll inbox-list" ref=${list} ...${pull}>
      ${st.conn.configured && !ready && html`<a class="inbox-card phone-link" href="#/agents"><${Icon} n="alert" /><span>${t('mobile.inbox_unavailable')}</span><${Icon} n="right" /></a>`}
      ${waiting.map(item => html`<div key=${item.request_id} class="inbox-item"><${LoomApproval} item=${item} class="inbox-card" /></div>`)}
      ${approvals.map(item => html`<${InboxRow} key=${item.id} item=${item} session=${st.sessions[item.sessionId]} kind=${item.kind === 'question' ? 'question' : 'approval'} current=${item.id === route.sub} />`)}
      ${replies.map(item => html`<${InboxRow} key=${item.id} item=${item} session=${st.sessions[item.sessionId]} kind="reply" current=${item.id === route.sub} />`)}
      ${completed.length > 0 && html`<section class="inbox-completed"><h2>${t('mobile.completed')}</h2>
        ${completed.map(([id, at]) => html`<a key=${id} class=${cls('inbox-card phone-link', at > (st.seen[id] || 0) && 'unseen')} href=${'#/agents/s/' + encodeURIComponent(id)}>
          <${Icon} n="check" class="inbox-done" /><span><b>${st.sessions[id].name || id.slice(0, 8)}</b><small>${at > (st.seen[id] || 0) ? lt('state', 'unseen') + ' · ' : ''}${ago(at)}</small></span><${Icon} n="right" />
        </a>`)}
      </section>`}
      ${ready && restore.length > 0 && html`<${RestoreBanner} />`}
      ${!waiting.length && !approvals.length && !replies.length && !completed.length && !restore.length &&
        (st.conn.state === 'idle' || ready && !st.loaded ? html`<div class="skeleton inbox-loading"></div>`
          : html`<${Empty} icon="inbox" title=${t('mobile.inbox_empty')} />`)}
    </div>
  </div>`;
}
