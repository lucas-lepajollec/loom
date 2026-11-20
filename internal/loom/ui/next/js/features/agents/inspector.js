// Panneau d'une session d'agent : affichage, harness, actions (shell,
// interruption, Codeman), quotas de l'abonnement, puis les sections Git,
// activité, fichiers, sous-agents et détails. Chaque section lit une route
// officielle de Codeman quand on l'ouvre ; rien n'est déduit hors de ces
// réponses. Même structure que le panneau d'une discussion.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Seg, Menu } from '../../ui/controls.js';
import { Modal, toast, prompt, confirm } from '../../ui/dialog.js';
import { copyText } from '../../ui/clipboard.js';
import { api } from './client.js';
import { agents, cliOf, onDomain, upsertSession, stateOf, projectOf, hasConversation } from './store.js';
import { closeSession, renameSession, createSession, interrupt } from './actions.js';
import { CliMark } from './new-session.js';
import { openSession, officialLink } from './nav.js';
import { HarnessQuota } from './quotas.js';
import { go } from '../../core/state.js';

const today = ms => new Date(ms).toDateString() === new Date().toDateString();
const time = ms => !ms ? '' : today(ms) ? new Date(ms).toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' }) : new Date(ms).toLocaleDateString(locale(), { day: 'numeric', month: 'short' });
const when = ms => ms ? new Date(ms).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '—';

function useRead(read, deps) {
  const [st, setSt] = useState({ data: null, error: '', loading: true });
  const run = async (...a) => {
    setSt(s => ({ ...s, loading: true }));
    try { setSt({ data: await read(...a), error: '', loading: false }); }
    catch (e) { setSt({ data: null, error: e.message, loading: false }); }
  };
  useEffect(() => { run(); }, deps);
  return [st, run];
}

const Failed = ({ error, retry }) => html`<div class="cm-insp-err"><${Icon} n="alert" /><span>${error}</span><button class="btn sm ghost" onClick=${() => retry()}>${t('agents.retry')}</button></div>`;
const Loading = () => html`<div class="skeleton" style="height:18px"></div><div class="skeleton" style="height:18px;width:70%"></div>`;

function ActivityTab({ id }) {
  const [st, run] = useRead(() => api.runSummary(id), [id]);
  const turn = useStore(agents, s => s.turns[id] || 0);
  useEffect(() => { if (turn) run(); }, [turn]);
  if (st.error) return html`<${Failed} error=${st.error} retry=${run} />`;
  const events = [...(st.data?.summary?.events || [])].sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0)).slice(0, 120);
  if (st.loading && !st.data) return html`<${Loading} />`;
  if (!events.length) return html`<p class="muted">${t('agents.insp.no_activity')}</p>`;
  return html`<ol class="cm-timeline">${events.map(e => html`<li key=${e.id} class=${'sev-' + (e.severity || 'info')}>
    <time>${time(e.timestamp)}</time><div><b>${e.title}</b>${e.details && html`<small>${e.details}</small>`}</div></li>`)}</ol>`;
}

function DiffModal({ id, file, onClose }) {
  const [st] = useRead(() => api.gitDiff(id, file), [file.path]);
  const lines = String(st.data?.diff || '').split('\n');
  return html`<${Modal} wide title=${file.path} sub=${lt('gitKind', file.kind)} onClose=${onClose}>
    ${st.loading ? html`<${Loading} />` : st.error ? html`<p class="muted">${st.error}</p>`
      : st.data?.binary ? html`<p class="muted">${t('agents.git.binary')}</p>`
      : html`<pre class="diff cm-diff">${lines.map(l => html`<span class=${'dl ' + (l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : l.startsWith('@@') ? 'hunk' : '')}>${l || ' '}</span>`)}</pre>`}
    ${st.data?.truncated && html`<p class="muted">${t('agents.git.truncated')}</p>`}
  </${Modal}>`;
}

function GitTab({ id }) {
  const [st, run] = useRead(fresh => api.gitStatus(id, fresh), [id]);
  const [diff, setDiff] = useState(null);
  if (st.error) return html`<${Failed} error=${st.error} retry=${run} />`;
  if (st.loading && !st.data) return html`<${Loading} />`;
  const d = st.data || {};
  if (d.state === 'not-a-repo') return html`<p class="muted">${t('agents.git.none')}</p>`;
  if (d.state === 'unsupported') return html`<p class="muted">${t('agents.git.unsupported')}</p>`;
  if (d.state === 'error') return html`<p class="muted">${d.error}</p>`;
  return html`<div class="cm-git">
    <div class="cm-insp-bar"><span class="muted">${t('agents.git.checked', { time: time(d.checkedAt) })}</span><button class="icon-btn" aria-label=${t('common.refresh')} onClick=${() => run(true)}><${Icon} n="refresh" /></button></div>
    ${(d.repos || []).map(repo => { const s = repo.status || {}; return html`<section class="cm-repo" key=${repo.path}>
      <header><b>${repo.name}</b><span class="tag">${s.branch || t('agents.git.detached')}</span>
        ${s.ahead ? html`<span class="tag green">↑${s.ahead}</span>` : ''}${s.behind ? html`<span class="tag amber">↓${s.behind}</span>` : ''}</header>
      ${s.state === 'error' ? html`<p class="muted">${s.error}</p>` : html`
        <p class="muted cm-repo-sum">${s.counts && s.counts.uncommitted ? t('agents.git.changes', { n: s.counts.uncommitted }) : t('agents.git.clean')}${s.unpushedCount ? ' · ' + t('agents.git.unpushed', { n: s.unpushedCount }) : ''}</p>
        <ul class="cm-files">${(s.files || []).map(f => html`<li key=${f.kind + f.path}><button onClick=${() => setDiff({ repo: s.repoRoot, path: f.path, kind: f.kind })}>
          <i class=${'gk ' + f.kind}>${f.kind === 'untracked' ? '?' : f.kind === 'conflicted' ? '!' : (f.kind === 'staged' ? f.index : f.worktree) || 'M'}</i><span class="trunc mono">${f.path}</span></button></li>`)}</ul>
        ${(s.unpushed || []).length > 0 && html`<details class="cm-commits"><summary>${t('agents.git.commits')}</summary>${s.unpushed.map(c => html`<div key=${c.hash}><code>${c.hash.slice(0, 7)}</code> ${c.subject}</div>`)}</details>`}`}
    </section>`; })}
    ${diff && html`<${DiffModal} id=${id} file=${diff} onClose=${() => setDiff(null)} />`}
  </div>`;
}

function Tree({ nodes, depth, onOpen }) {
  const [open, setOpen] = useState({});
  return html`<ul class="cm-tree">${(nodes || []).map(n => html`<li key=${n.path}>
    <button style=${`padding-left:${6 + depth * 12}px`} onClick=${() => n.type === 'directory' ? setOpen({ ...open, [n.path]: !open[n.path] }) : onOpen(n.path)}>
      <${Icon} n=${n.type === 'directory' ? (open[n.path] ? 'chevron' : 'right') : 'file'} /><span class="trunc">${n.name}</span></button>
    ${n.type === 'directory' && open[n.path] && html`<${Tree} nodes=${n.children} depth=${depth + 1} onOpen=${onOpen} />`}</li>`)}</ul>`;
}

function FileModal({ id, path, onClose }) {
  const [st] = useRead(() => api.fileContent(id, path), [path]);
  const content = st.data?.content || '';
  return html`<${Modal} wide title=${path} onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${() => copyText(path)}>${t('agents.files.copy_path')}</button>`}>
    ${st.loading ? html`<${Loading} />` : st.error ? html`<p class="muted">${st.error}</p>`
      : typeof st.data?.content !== 'string' ? html`<p class="muted">${t('agents.files.binary')}</p>`
      : html`<pre class="cm-file">${content}</pre>`}
    ${st.data?.truncated && html`<p class="muted">${t('agents.files.truncated')}</p>`}
  </${Modal}>`;
}

function FilesTab({ id }) {
  const [st, run] = useRead(() => api.files(id), [id]);
  const [open, setOpen] = useState(null);
  if (st.error) return html`<${Failed} error=${st.error} retry=${run} />`;
  if (st.loading && !st.data) return html`<${Loading} />`;
  return html`<div class="cm-files-tab">
    <div class="cm-insp-bar"><span class="muted mono trunc">${st.data?.root}</span><button class="icon-btn" aria-label=${t('common.refresh')} onClick=${() => run()}><${Icon} n="refresh" /></button></div>
    <${Tree} nodes=${st.data?.tree} depth=${0} onOpen=${setOpen} />
    ${open && html`<${FileModal} id=${id} path=${open} onClose=${() => setOpen(null)} />`}
  </div>`;
}

function SubagentModal({ agent, onClose }) {
  const [st] = useRead(() => api.subagentTranscript(agent.agentId), [agent.agentId]);
  const f = st.data?.formatted;
  const text = Array.isArray(f) ? f.join('\n') : typeof f === 'string' ? f : '';
  return html`<${Modal} wide title=${agent.description || agent.agentId} sub=${agent.model || ''} onClose=${onClose}>
    ${st.loading ? html`<${Loading} />` : st.error ? html`<p class="muted">${st.error}</p>`
      : !text ? html`<p class="muted">${t('agents.sub.empty_transcript')}</p>`
      : html`<pre class="cm-file">${text}</pre>`}
  </${Modal}>`;
}

function SubagentsTab({ id }) {
  const [st, run] = useRead(() => api.subagents(id), [id]);
  const [open, setOpen] = useState(null);
  useEffect(() => onDomain('subagent', () => run()), [id]);
  if (st.error) return html`<${Failed} error=${st.error} retry=${run} />`;
  if (st.loading && !st.data) return html`<${Loading} />`;
  const list = Array.isArray(st.data) ? st.data : [];
  if (!list.length) return html`<p class="muted">${t('agents.sub.none')}</p>`;
  return html`<ul class="cm-subs">${list.map(a => html`<li key=${a.agentId}><button onClick=${() => setOpen(a)}>
    <i class=${cls('dot', a.status === 'active' ? 'amber' : a.status === 'idle' ? 'green' : '')}></i>
    <span class="grow"><b class="trunc">${a.description || a.agentId}</b><small class="muted">${a.status || ''}${a.toolCallCount ? ' · ' + t('agents.sub.tools', { n: a.toolCallCount }) : ''}</small></span></button></li>`)}
    ${open && html`<${SubagentModal} agent=${open} onClose=${() => setOpen(null)} />`}</ul>`;
}

function Row({ k, children }) { return html`<div class="cm-kv"><span>${k}</span><b>${children}</b></div>`; }

function InfoTab({ session }) {
  const [busy, setBusy] = useState(false);
  const toggle = async (fn, enabled) => { setBusy(true); try { await fn(session.id, enabled); } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); } };
  const c = cliOf(session.mode), pc = session.promptCache;
  return html`<div class="cm-info">
    <${Row} k=${t('agents.info.cli')}>${c.label}${session.cliVersion ? ' ' + session.cliVersion : ''}</${Row}>
    ${session.displayModel?.model && html`<${Row} k=${t('agents.info.model')}>${session.displayModel.model}</${Row}>`}
    <${Row} k=${t('agents.info.dir')}><span class="mono trunc" title=${session.workingDir}>${session.workingDir}</span></${Row}>
    <${Row} k=${t('agents.info.created')}>${when(session.createdAt)}</${Row}>
    <${Row} k=${t('agents.info.activity')}>${when(session.lastActivityAt)}</${Row}>
    ${session.tokens?.total > 0 && html`<${Row} k=${t('agents.info.tokens')}>${fmtTok(session.tokens.input)} ↓ · ${fmtTok(session.tokens.output)} ↑</${Row}>`}
    ${session.totalCost > 0 && html`<${Row} k=${t('agents.info.cost')}>$${session.totalCost.toFixed(2)}</${Row}>`}
    ${pc && html`<${Row} k=${t('agents.info.cache')}>${pc.warm ? t('agents.info.cache_warm', { ttl: pc.ttl || '' }) : t('agents.info.cache_cold')}${typeof pc.hitRatio === 'number' ? ' · ' + Math.round(pc.hitRatio * 100) + '%' : ''}</${Row}>`}
    <${Row} k=${t('agents.info.id')}><button class="cm-copy mono" onClick=${() => copyText(session.id).then(() => toast(t('agents.copied')))}>${session.id.slice(0, 8)}…</button></${Row}>
    ${session.mode === 'claude' && html`<div class="cm-switches">
      <label class="cm-switch"><span><b>${t('agents.info.auto_resume')}</b><small>${session.autoResumeAt > Date.now() ? t('agents.info.auto_resume_at', { at: when(session.autoResumeAt) }) : t('agents.info.auto_resume_hint')}</small></span>
        <${Switch} checked=${!!session.autoResumeEnabled} disabled=${busy} onChange=${v => toggle((id, on) => api.autoResume(id, on).then(r => upsertSession({ id, autoResumeEnabled: on, autoResumeAt: r?.autoResume?.resumeAt ?? (on ? session.autoResumeAt : null) })), v)} label=${t('agents.info.auto_resume')} /></label>
      <label class="cm-switch"><span><b>${t('agents.info.auto_compact')}</b><small>${t('agents.info.auto_compact_hint', { n: fmtTok(session.autoCompactThreshold || 0) })}</small></span>
        <${Switch} checked=${!!session.autoCompactEnabled} disabled=${busy} onChange=${v => toggle(api.autoCompact, v)} label=${t('agents.info.auto_compact')} /></label>
      <label class="cm-switch"><span><b>${t('agents.info.auto_clear')}</b><small>${t('agents.info.auto_clear_hint', { n: fmtTok(session.autoClearThreshold || 0) })}</small></span>
        <${Switch} checked=${!!session.autoClearEnabled} disabled=${busy} onChange=${v => toggle(api.autoClear, v)} label=${t('agents.info.auto_clear')} /></label>
    </div>`}
  </div>`;
}

// Menu de la session : renommer, relancer, interface Codeman, retirer ou fermer.
function SessionMenu({ s }) {
  const [menu, setMenu] = useState(null);
  const state = stateOf(s);
  const rename = async () => {
    const name = await prompt(t('agents.session.rename'), { value: s.name || '' });
    if (name && name !== s.name) renameSession(s.id, name).catch(e => toast(e.message, 'err'));
  };
  const close = async killMux => {
    const ok = await confirm(killMux ? t('agents.session.close_title') : t('agents.session.detach_title'),
      killMux ? t('agents.session.close_text', { name: s.name || s.id.slice(0, 8) }) : t('agents.session.detach_text'), { ok: killMux ? t('agents.session.close') : t('agents.session.detach'), danger: killMux });
    if (!ok) return;
    try { await closeSession(s.id, killMux); go('agents'); } catch (e) { toast(e.message, 'err'); }
  };
  const restart = () => api.start(s.id, s.mode === 'shell').then(() => api.session(s.id).then(upsertSession)).catch(e => toast(e.message, 'err'));
  const link = officialLink(s.id);
  const items = [
    { label: t('agents.session.rename'), icon: 'edit', run: rename },
    ...(state === 'stopped' ? [{ label: t('agents.session.restart'), icon: 'play', run: restart }] : []),
    ...(link ? [{ label: t('agents.session.official'), icon: 'link', run: () => window.open(link, 'codeman') }] : []),
    '-',
    { label: t('agents.session.detach'), icon: 'power', run: () => close(false) },
    { label: t('agents.session.close'), icon: 'trash', danger: true, run: () => close(true) },
  ];
  return html`<button class=${cls('icon-btn', menu && 'on')} aria-label=${t('agents.session.actions')} onClick=${e => setMenu(menu ? null : e.currentTarget)}><${Icon} n="more" /></button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}`;
}

// Une section repliable ; son contenu (et donc sa lecture) n'existe qu'ouvert.
const openKey = 'loom.codeman.panel.open';
const readOpen = () => { try { return JSON.parse(localStorage.getItem(openKey)) || ['git']; } catch (_) { return ['git']; } };
function Section({ id, label, open, toggle, children }) {
  return html`<section class=${cls('cm-ps', open && 'open')}>
    <button class="cm-ps-h" aria-expanded=${String(open)} onClick=${() => toggle(id)}><span>${label}</span><${Icon} n=${open ? 'chevron' : 'right'} /></button>
    ${open && html`<div class="cm-ps-b">${children}</div>`}
  </section>`;
}

export function Inspector({ session, onClose, view, setView, narrowView }) {
  const [open, setOpen] = useState(readOpen);
  const [busy, setBusy] = useState('');
  if (!session) return html`<aside class="cm-insp"><${Empty} icon="info" title=${t('agents.insp.none')} /></aside>`;
  const toggle = id => { const n = open.includes(id) ? open.filter(x => x !== id) : [...open, id]; setOpen(n); try { localStorage.setItem(openKey, JSON.stringify(n)); } catch (_) {} };
  const s = session, c = cliOf(s.mode), state = stateOf(s);
  const shell = async () => {
    setBusy('shell');
    try { openSession(await createSession({ mode: 'shell', workingDir: s.workingDir })); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  const views = [{ value: 'chat', label: t('agents.view.chat') }, { value: 'term', label: t('agents.view.terminal') }, ...(narrowView ? [] : [{ value: 'split', label: t('agents.view.split') }])];
  const link = officialLink(s.id);
  return html`<aside class="cm-insp cm-panel" aria-label=${t('agents.insp.title')}>
    <header class="cm-panel-h"><b>${t('agents.insp.info')}</b><span class="grow"></span>
      <${SessionMenu} s=${s} />
      ${onClose && html`<button class="icon-btn" aria-label=${t('agents.insp.close')} onClick=${onClose}><${Icon} n="close" /></button>`}</header>
    <div class="cm-panel-body">
      ${setView && hasConversation(s.mode) && html`<${Seg} value=${view} options=${views} onChange=${setView} label=${t('agents.view.label')} />`}
      <div class="cm-panel-rows">
        <div class="cm-kv"><span>${t('agents.info.cli')}</span><b class="cm-panel-cli"><${CliMark} mode=${s.mode} size="xs" />${c.label}${s.cliVersion ? ' ' + s.cliVersion : ''}</b></div>
        ${s.displayModel?.model && html`<div class="cm-kv"><span>${t('agents.info.model')}</span><b>${s.displayModel.model}</b></div>`}
        <div class="cm-kv"><span>${t('agents.info.dir')}</span><b class="mono trunc" title=${s.workingDir}>${projectOf(s.workingDir)}</b></div>
      </div>
      <div class="cm-panel-acts">
        ${s.mode !== 'shell' && html`<button disabled=${!!busy} onClick=${shell}>${busy === 'shell' ? html`<span class="spinner"></span>` : html`<${Icon} n="terminal" />`}<span>${t('agents.panel.shell')}</span></button>`}
        <button disabled=${state !== 'working'} onClick=${() => interrupt(s.id).catch(e => toast(e.message, 'err'))}><${Icon} n="stop" /><span>${t('agents.panel.interrupt')}</span></button>
        ${link ? html`<button onClick=${() => window.open(link, 'codeman')}><${Icon} n="link" /><span>${t('agents.panel.codeman')}</span></button>` : ''}
      </div>
      <${HarnessQuota} mode=${s.mode} />
      <div class="cm-panel-secs">
        <${Section} id="git" label=${t('agents.panel.changes')} open=${open.includes('git')} toggle=${toggle}><${GitTab} id=${s.id} /></${Section}>
        <${Section} id="activity" label=${lt('insp', 'activity')} open=${open.includes('activity')} toggle=${toggle}><${ActivityTab} id=${s.id} /></${Section}>
        <${Section} id="files" label=${lt('insp', 'files')} open=${open.includes('files')} toggle=${toggle}><${FilesTab} id=${s.id} /></${Section}>
        <${Section} id="subagents" label=${lt('insp', 'subagents')} open=${open.includes('subagents')} toggle=${toggle}><${SubagentsTab} id=${s.id} /></${Section}>
        <${Section} id="info" label=${t('agents.panel.details')} open=${open.includes('info')} toggle=${toggle}><${InfoTab} session=${s} /></${Section}>
      </div>
    </div>
  </aside>`;
}
