// Panneau d'une session d'agent : affichage, harness, actions (shell,
// interruption, Codeman), quotas de l'abonnement, puis les sections Git,
// activité, fichiers, sous-agents et détails. Chaque section lit une route
// officielle de Codeman quand on l'ouvre ; rien n'est déduit hors de ces
// réponses. Même structure que le panneau d'une discussion.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls, fmtTok } from '../../core/lib.js';
import { SessionPanel, PanelTabs } from '../../ui/session-panel.js';
import { PreviewPanel } from '../previews/panel.js';
import { get } from '../../core/api.js';
import { JobForm } from './cron.js';
import { Icon } from '../../ui/icons.js';
import { DropdownButton, Switch, Empty, Seg, Menu } from '../../ui/controls.js';
import { Modal, toast, prompt, confirm } from '../../ui/dialog.js';
import { copyText } from '../../ui/clipboard.js';
import { api, sendPrompt } from './client.js';
import { ago } from './rail.js';
import { agents, cliOf, onDomain, upsertSession, stateOf, projectOf, hasConversation, sessionList } from './store.js';
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
  const revision = useRef(0);
  const run = async (...a) => {
    const request = ++revision.current;
    setSt(s => ({ ...s, loading: true }));
    try {
      const data = await read(...a);
      if (request === revision.current) setSt({ data, error: '', loading: false });
    } catch (e) { if (request === revision.current) setSt({ data: null, error: e.message, loading: false }); }
  };
  useEffect(() => { run(); return () => { revision.current++; }; }, deps);
  return [st, run];
}

const Failed = ({ error, retry }) => html`<div class="cm-insp-err"><${Icon} n="alert" /><span>${error}</span><button class="btn sm" onClick=${() => retry()}>${t('agents.retry')}</button></div>`;
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

function DiffView({ id, file, onClose }) {
  const [st] = useRead(() => api.gitDiff(id, file), [id, file.repo, file.path, file.kind]);
  const lines = String(st.data?.diff || '').split('\n');
  return html`<div class="cm-inline-diff">
    <div class="cm-insp-bar"><span class="mono trunc" title=${file.path}>${file.path}</span><button class="icon-btn" aria-label=${t('ui.drawer.fermer')} onClick=${onClose}><${Icon} n="close" /></button></div>
    ${st.loading ? html`<${Loading} />` : st.error ? html`<p class="note err">${st.error}</p>`
      : st.data?.binary ? html`<p class="muted">${t('agents.git.binary')}</p>`
      : html`<pre class="diff cm-diff">${lines.map(l => html`<span class=${'dl ' + (l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : l.startsWith('@@') ? 'hunk' : '')}>${l || ' '}</span>`)}</pre>`}
    ${st.data?.truncated && html`<p class="muted">${t('agents.git.truncated')}</p>`}
  </div>`;
}

export function useSessionGit(id) {
  const turn = useStore(agents, s => s.turns[id] || 0);
  return useRead(fresh => api.gitStatus(id, fresh), [id, turn]);
}

export const gitFiles = data => (data?.repos || []).flatMap(repo => (repo.status?.files || [])
  .map(file => ({ ...file, repo: repo.status.repoRoot || repo.path })));

function GitTab({ id, git }) {
  const [st, run] = git;
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
        <ul class="cm-files">${(s.files || []).map(f => html`<li key=${f.kind + f.path}><button onClick=${() => setDiff({ repo: s.repoRoot || repo.path, path: f.path, kind: f.kind })}>
          <i class=${'gk ' + f.kind}>${f.kind === 'untracked' ? '?' : f.kind === 'conflicted' ? '!' : (f.kind === 'staged' ? f.index : f.worktree) || 'M'}</i><span class="trunc mono">${f.path}</span></button></li>`)}</ul>
        ${(s.unpushed || []).length > 0 && html`<details class="cm-commits"><summary>${t('agents.git.commits')}</summary>${s.unpushed.map(c => html`<div key=${c.hash}><code>${c.hash.slice(0, 7)}</code> ${c.subject}</div>`)}</details>`}`}
    </section>`; })}
    ${diff && html`<${DiffView} id=${id} file=${diff} onClose=${() => setDiff(null)} />`}
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
      foot=${html`<button class="btn" onClick=${() => copyText(path)}>${t('agents.files.copy_path')}</button>`}>
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
    <${Row} k=${t('agents.info.id')}><button class="btn sm mono" onClick=${() => copyText(session.id).then(() => toast(t('agents.copied')))}>${session.id.slice(0, 8)}…</button></${Row}>
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
function SessionMenu({ s, phone }) {
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
  return html`<button class=${cls(phone ? 'btn sm' : 'icon-btn', menu && 'on')} aria-label=${t('agents.session.actions')} onClick=${e => setMenu(menu ? null : e.currentTarget)}><${Icon} n="more" />${phone && t('mobile.session_options_short')}</button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}`;
}

// Une section repliable ; son contenu (et donc sa lecture) n'existe qu'ouvert.
const openKey = 'loom.codeman.panel.open';
const readOpen = () => { try { return JSON.parse(localStorage.getItem(openKey)) || []; } catch (_) { return []; } };
function Section({ id, label, hint, open, toggle, children }) {
  return html`<section class=${cls('cm-ps', open && 'open')}>
    <button class="cm-ps-h" aria-expanded=${String(open)} onClick=${() => toggle(id)}><span>${label}</span>${hint && html`<small>${hint}</small>`}<${Icon} n=${open ? 'chevron' : 'right'} /></button>
    ${open && html`<div class="cm-ps-b">${children}</div>`}
  </section>`;
}

// Modèles proposés à Claude Code par sa commande /model.
const CLAUDE_MODELS = ['opus', 'sonnet', 'haiku'];
function ModelRow({ s }) {
  const [menu, setMenu] = useState(null);
  const model = s.displayModel?.model || '';
  const send = cmd => sendPrompt(s.id, cmd).then(() => toast(t('agents.panel.sent', { cmd }))).catch(e => toast(e.message, 'err'));
  if (s.mode !== 'claude') return model ? html`<div class="cm-kv"><span>${t('agents.info.model')}</span><b>${model}</b></div>` : null;
  return html`<div class="cm-kv"><span>${t('agents.info.model')}</span>
    <${DropdownButton} class="sm fit" open=${menu} onClick=${e => setMenu(e.currentTarget)}><span class="trunc">${model || t('agents.new.model_ph')}</span></${DropdownButton}>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${CLAUDE_MODELS.map(m => ({ label: m[0].toUpperCase() + m.slice(1), icon: model.toLowerCase().includes(m) ? 'check' : '', run: () => send('/model ' + m) }))} />`}</div>`;
}

function OthersTab({ s }) {
  const others = sessionList().filter(x => x.mode === s.mode && x.id !== s.id);
  if (!others.length) return html`<p class="muted">${t('agents.panel.no_others')}</p>`;
  return html`<ul class="cm-others">${others.map(o => html`<li key=${o.id}><button onClick=${() => openSession(o.id)}><span class="trunc">${o.name || o.id.slice(0, 8)}</span><small class="muted">${projectOf(o.workingDir)} · ${ago(o.lastActivityAt)}</small></button></li>`)}</ul>`;
}

function SessionPreview({ session }) {
  const [{ data, loading }] = useRead(() => get('/api/machines/roles'), []);
  const target = data?.roles?.codeman;
  const link = officialLink(session.id);
  if (loading) return html`<${Loading} />`;
  if (target) return html`<${PreviewPanel} target=${target} />`;
  return link ? html`<a class="btn" href=${link} target="codeman" rel="noopener"><${Icon} n="link" />${t('agents.session.official')}</a>`
    : html`<p class="note">${t('noir.preview_unavailable')}</p>`;
}

export function Inspector({ session, onClose, view, setView, narrowView, git, tab, setTab }) {
  const [open, setOpen] = useState(readOpen);
  const [busy, setBusy] = useState('');
  const [schedule, setSchedule] = useState(false);
  const roles = useStore(agents, s => s.conn);
  const toggle = id => { const n = open.includes(id) ? open.filter(x => x !== id) : [...open, id]; setOpen(n); try { localStorage.setItem(openKey, JSON.stringify(n)); } catch (_) {} };
  const s = session, c = cliOf(s.mode), state = stateOf(s);
  const status = git[0].data?.repos?.length === 1 ? git[0].data.repos[0].status : null;
  let machine = '';
  try { machine = new URL(roles.ui_url || roles.url).hostname; } catch (_) {}
  const shell = async () => {
    setBusy('shell');
    try { openSession(await createSession({ mode: 'shell', workingDir: s.workingDir })); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  const slash = async (cmd, ask) => {
    if (ask && !await confirm(t('agents.panel.clear'), t('agents.panel.clear_text'), { ok: t('agents.panel.clear'), danger: true })) return;
    setBusy(cmd);
    try { await sendPrompt(s.id, cmd); toast(t('agents.panel.sent', { cmd })); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  const views = [{ value: 'chat', label: t('agents.view.chat') }, { value: 'term', label: t('agents.view.terminal') }, ...(narrowView ? [] : [{ value: 'split', label: t('agents.view.split') }])];
  const tabs = [
    ['session', t('agents.insp.info')],
    ['git', t('agents.panel.changes')],
    ['terminal', t('agents.view.terminal')],
    ['files', t('files.title')],
    ['preview', t('noir.preview')],
  ];
  return html`<${SessionPanel} title=${t('agents.insp.title')} onClose=${onClose}>
    <div class="cm-panel session-agent-panel">
      <${PanelTabs} label=${t('agents.insp.title')} value=${tab} tabs=${tabs} onChange=${setTab} />
      <div class="cm-panel-body" key=${tab}>
        ${tab === 'session' && html`
          <div class="cm-session-stats">
            <div><strong>${s.tokens?.total > 0 ? fmtTok(s.tokens.total) : '—'}</strong><span>${t('agents.info.tokens')}</span></div>
            <div><strong>${s.totalCost > 0 ? '$' + s.totalCost.toFixed(2) : '—'}</strong><span>${t('agents.info.cost')}</span></div>
          </div>
          <${HarnessQuota} mode=${s.mode} />
          <div class="cm-panel-rows">
            <${Row} k=${t('agents.info.cli')}><span class="cm-panel-cli"><${CliMark} mode=${s.mode} size="xs" />${c.label}${s.cliVersion ? ' ' + s.cliVersion : ''}</span></${Row}>
            <${ModelRow} s=${s} />
            ${s.displayModel?.effort && html`<${Row} k=${t('noir.effort')}>${s.displayModel.effort}</${Row}>`}
            ${s.permissionMode && html`<${Row} k=${t('inspector.inspector.autorisations')}>${s.permissionMode}</${Row}>`}
          </div>
          <div class="cm-panel-rows">
            ${machine && html`<${Row} k=${t('workspaces.machine')}>${machine}</${Row}>`}
            <${Row} k=${t('agents.info.dir')}><span class="mono trunc" title=${s.workingDir}>${projectOf(s.workingDir)}</span></${Row}>
            ${status?.branch && html`<${Row} k=${t('noir.branch')}><span class="mono trunc">${status.branch}</span></${Row}>`}
          </div>
          <div class="cm-panel-acts">
            ${s.mode === 'claude' && html`
              <button class="btn sm" disabled=${!!busy} onClick=${() => slash('/compact')}><${Icon} n="sliders" /><span>${t('agents.panel.compact')}</span></button>
              <button class="btn sm" disabled=${!!busy} onClick=${() => slash('/clear', true)}><${Icon} n="refresh" /><span>${t('agents.panel.clear')}</span></button>`}
            ${s.mode !== 'shell' && html`<button class="btn sm" onClick=${() => setSchedule(true)}><${Icon} n="clock" />${t('noir.schedule')}</button>`}
            ${['working', 'waiting'].includes(state) && html`<button class="btn sm danger" onClick=${() => interrupt(s.id).catch(e => toast(e.message, 'err'))}><${Icon} n="stop" />${t('agents.composer.interrupt')}</button>`}
            <${SessionMenu} s=${s} phone />
          </div>
          <div class="cm-panel-secs">
            <${Section} id="activity" label=${lt('insp', 'activity')} hint=${ago(s.lastActivityAt)} open=${open.includes('activity')} toggle=${toggle}><${ActivityTab} id=${s.id} /></${Section}>
            <${Section} id="subagents" label=${lt('insp', 'subagents')} open=${open.includes('subagents')} toggle=${toggle}><${SubagentsTab} id=${s.id} /></${Section}>
            <${Section} id="others" label=${t('agents.panel.others', { cli: c.label })} open=${open.includes('others')} toggle=${toggle}><${OthersTab} s=${s} /></${Section}>
            <${Section} id="info" label=${t('agents.panel.details')} open=${open.includes('info')} toggle=${toggle}><${InfoTab} session=${s} /></${Section}>
          </div>
          ${schedule && html`<${JobForm} initial=${{ agentType: s.mode, workingDir: s.workingDir, name: s.name || '' }} phone=${narrowView}
            onClose=${() => setSchedule(false)} onSaved=${() => setSchedule(false)} />`}
        `}
        ${tab === 'git' && html`<${GitTab} id=${s.id} git=${git} />`}
        ${tab === 'files' && html`<${FilesTab} id=${s.id} />`}
        ${tab === 'terminal' && html`
          ${setView && hasConversation(s.mode) && html`<${Seg} value=${view} options=${views} onChange=${v => { setView(v); if (narrowView) onClose?.(); }} label=${t('agents.view.label')} />`}
          ${s.mode !== 'shell' && html`<button class="btn" disabled=${!!busy} onClick=${shell}><${Icon} n="terminal" />${t('agents.panel.shell')}</button>`}
        `}
        ${tab === 'preview' && html`<${SessionPreview} session=${s} />`}
      </div>
    </div>
  </${SessionPanel}>`;
}
