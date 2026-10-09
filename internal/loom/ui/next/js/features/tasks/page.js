// Tâches : ce qui travaille en ce moment, ce qui attend une réponse, ce qui
// vient de finir. Une tâche est un tour de discussion ; elle continue côté
// serveur même navigateur fermé. Flux en direct : GET /api/tasks/stream.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Empty } from '../../ui/controls.js';
import { get } from '../../core/api.js';
import { go } from '../../core/state.js';
import { open } from '../chat/engine.js';
import { SectionTabs } from '../../app/sections.js';

const dur = s => {
  if (s == null) return '';
  s = Math.max(0, Math.round(s));
  return s < 60 ? s + ' s' : s < 3600 ? Math.floor(s / 60) + ' min' : Math.floor(s / 3600) + ' h ' + String(Math.floor(s % 3600 / 60)).padStart(2, '0');
};
const ago = ms => { const m = Math.round((Date.now() - ms) / 60000); return m < 1 ? t('tasks.now') : m < 60 ? t('common.relative.minutes', { n: m }) : t('common.relative.hours', { n: Math.round(m / 60) }); };
const STATUS = () => ({ running: ['green', t('tasks.status.running')], waiting_input: ['amber', t('tasks.status.waiting_input')], waiting_approval: ['amber', t('tasks.status.waiting_approval')], done: ['', t('tasks.status.done')], failed: ['red', t('tasks.status.failed')], cancelled: ['', t('tasks.status.cancelled')] });
const REQ = () => ({ approval: t('tasks.req.approval'), user_input: t('tasks.req.user_input'), elicitation: t('tasks.req.elicitation') });

function TaskRow({ task }) {
  const [tone, label] = STATUS()[task.status] || ['', task.status];
  const waiting = task.status.startsWith('waiting');
  const openIt = async () => { await open(task.discussion_id); go('chat'); };
  const steps = task.steps_started != null ? t('tasks.steps', { done: task.steps_completed || 0, total: task.steps_started }) : '';
  return html`<button type="button" class=${cls('task-row', waiting && 'waiting')} onClick=${openIt}>
    <${Logo} name=${task.executor} size="sm" />
    <span class="grow task-main"><b class="trunc">${task.title || t('tasks.untitled')}</b>
      <small>${[task.model, task.status === 'running' || waiting ? dur(task.elapsed_seconds) : task.finished_at ? ago(task.finished_at) : '', steps].filter(Boolean).join(' · ')}</small>
      ${(task.requests || []).map(r => html`<span class="task-req" key=${r.id}><${Icon} n="chat" />${REQ()[r.kind] || r.kind}${r.summary ? ' — ' + r.summary : ''}</span>`)}</span>
    <span class=${'pill ' + tone}><i class=${'dot ' + tone}></i>${label}</span>
    ${waiting && html`<span class="btn sm primary">${t('tasks.answer')}</span>`}
  </button>`;
}

export function TasksPage() {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let es = null, stop = false, retry = null;
    const connect = () => {
      if (stop) return;
      try {
        es = new EventSource('/api/tasks/stream');
        es.onmessage = e => { try { const d = JSON.parse(e.data); if (d.ok !== false) { setData(d); setError(''); } } catch (_) {} };
        es.onerror = () => { es && es.close(); if (!stop) retry = setTimeout(connect, 4000); };
      } catch (_) { retry = setTimeout(connect, 4000); }
    };
    get('/api/tasks').then(d => { if (d.ok === false) setError(d.error || ''); else setData(d); }).catch(e => setError(e.message));
    connect();
    return () => { stop = true; clearTimeout(retry); es && es.close(); };
  }, []);
  const tasks = (data && data.tasks) || [];
  const waiting = tasks.filter(x => x.status.startsWith('waiting'));
  const running = tasks.filter(x => x.status === 'running');
  const done = tasks.filter(x => !x.status.startsWith('waiting') && x.status !== 'running');
  const section = (title, list, note) => list.length > 0 && html`<section class="sec"><div class="sec-h"><h2>${title} <span class="count">${list.length}</span></h2>${note && html`<span class="muted">${note}</span>`}</div>
    <div class="card task-list">${list.map(x => html`<${TaskRow} key=${x.id} task=${x} />`)}</div></section>`;
  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} />
    <div class="page-head"><div><h1>${t('tasks.title')}</h1><p>${t('tasks.lead')}</p></div>
      <div class="acts"><a class="btn" href="#/settings/notifications"><${Icon} n="pulse" />${t('tasks.notifications')}</a></div></div>
    ${error && html`<div class="alert amber"><${Icon} n="alert" />${error}</div>`}
    ${data === null ? html`<div class="skeleton" style="height:180px"></div>` : !tasks.length ? html`<${Empty} icon="pulse" title=${t('tasks.empty')} text=${t('tasks.empty_note')} />` : html`
      ${section(t('tasks.waiting'), waiting, t('tasks.waiting_note'))}
      ${section(t('tasks.running'), running)}
      ${section(t('tasks.recent'), done, t('tasks.recent_note'))}`}
  </div></div>`;
}
