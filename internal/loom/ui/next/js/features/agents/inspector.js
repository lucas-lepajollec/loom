// Panneau latéral d'une session d'agent : redimensionnable sur bureau, organisé
// en onglets dédiés (Session, Modifications, Fichiers, Sous-agents et Aperçu
// dynamique). Chaque onglet lit les routes officielles de Codeman à la demande ;
// l'onglet Aperçu apparaît automatiquement dès que l'agent cite un document ou
// quand l'utilisateur ouvre un fichier.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useLayoutEffect, useRef, useMemo, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { DropdownButton, Switch, Empty, Seg, Menu } from '../../ui/controls.js';
import { toast, prompt, confirm } from '../../ui/dialog.js';
import { copyText } from '../../ui/clipboard.js';
import { md } from '../chat/md.js';
import { request } from '../../core/api.js';
import { api, sendPrompt } from './client.js';
import { ago } from './rail.js';
import { agents, cliOf, onDomain, upsertSession, stateOf, attentionOf, projectOf, hasConversation, sessionList, queueCommand } from './store.js';
import { closeSession, renameSession, createSession } from './actions.js';
import { CliMark } from './new-session.js';
import { openSession, officialLink } from './nav.js';
import { HarnessQuota } from './quotas.js';
import { changedFiles } from '../conversation/turns.js';
import { go } from '../../core/state.js';
import {
  panelState, DEFAULT_PANEL_WIDTH, clampPanelWidth, setLivePanelWidth, savePanelWidth,
  previewFor, setPanelTab, openPreview, closePreview, isMarkdownPath, normalizeSessionPath,
} from './panel-state.js';
import {
  orchestration, descendantsOf, rootIdOf, loadBoard, postBoard, spawnSubagent, stopTree, cleanupWorktree,
} from './orchestration.js';

const today = ms => new Date(ms).toDateString() === new Date().toDateString();
const time = ms => !ms ? '' : today(ms) ? new Date(ms).toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' }) : new Date(ms).toLocaleDateString(locale(), { day: 'numeric', month: 'short' });
const when = ms => ms ? new Date(ms).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '—';

function splitPath(path) {
  const s = String(path || '');
  const i = s.lastIndexOf('/');
  return i >= 0 ? { dir: s.slice(0, i + 1), name: s.slice(i + 1) } : { dir: '', name: s };
}

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

const Failed = ({ error, retry }) => html`<div class="cm-insp-err"><${Icon} n="alert" /><span>${error}</span><button type="button" class="btn sm" onClick=${() => retry()}>${t('agents.retry')}</button></div>`;
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

const cleanDiffLines = text => String(text || '').split('\n').filter(l => !/^(diff --git|index |--- |\+\+\+ )/.test(l));

function diffStats(text) {
  let add = 0, del = 0;
  for (const l of cleanDiffLines(text)) {
    if (l[0] === '+') add++;
    else if (l[0] === '-') del++;
  }
  return { add, del };
}

function DiffLines({ text, truncated }) {
  const lines = useMemo(() => cleanDiffLines(text), [text]);
  return html`<div class="cm-chg-diff" role="region">${lines.map((l, i) => {
    const hunk = l.startsWith('@@');
    const sign = !hunk && (l[0] === '+' || l[0] === '-') ? l[0] : '';
    const body = sign ? l.slice(1) : l;
    return html`<div key=${i} class=${cls('cm-dl', hunk ? 'h' : sign === '+' ? 'a' : sign === '-' ? 'd' : '')}>
      ${!hunk && html`<span class="cm-dl-g" aria-hidden="true">${sign || ' '}</span>`}
      <span class="cm-dl-t">${body || ' '}</span>
    </div>`;
  })}${truncated && html`<div class="cm-dl h"><span class="cm-dl-t">…</span></div>`}</div>`;
}

// Ligne de modification style labos (Codex / Claude Desktop / Cursor) :
// nom de fichier net, dossier discret, compteur +N -M, actions au survol et diff unifié lisible.
function ChangeRow({ id, repoRoot, file, onPreview }) {
  const [open, setOpen] = useState(false);
  const [diff, setDiff] = useState(null);
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next && diff === null) {
      api.gitDiff(id, { repo: repoRoot, path: file.path, kind: file.kind })
        .then(data => setDiff({ data }))
        .catch(e => setDiff({ error: e.message }));
    }
  };
  const { dir, name } = splitPath(file.path);
  const code = file.kind === 'untracked' ? 'U' : file.kind === 'conflicted' ? '!' : (file.kind === 'staged' ? file.index : file.worktree) || 'M';
  const deleted = code === 'D';
  const stats = useMemo(() => (diff?.data?.diff ? diffStats(diff.data.diff) : null), [diff]);
  return html`<div class=${cls('cm-chg-file', open && 'open')}>
    <div class="cm-chg-row">
      <button type="button" class="cm-chg-btn" aria-expanded=${String(open)} onClick=${toggle}>
        <${Icon} n="right" class="cm-chg-caret" />
        <span class="cm-chg-path" title=${file.path}><span class="name">${name}</span>${dir && html`<span class="dir">${dir}</span>`}</span>
        ${stats && (stats.add > 0 || stats.del > 0) && html`<span class="cm-chg-stat">${stats.add > 0 && html`<em class="a">+${stats.add}</em>`}${stats.del > 0 && html`<em class="d">-${stats.del}</em>`}</span>`}
        <i class=${'gk ' + file.kind} title=${lt('gitKind', file.kind)}>${code}</i>
      </button>
      <div class="cm-chg-acts">
        <button type="button" class="icon-btn cm-chg-eye" aria-label=${t('agents.files.copy_path')} title=${t('agents.files.copy_path')} onClick=${() => copyText(file.path).then(() => toast(t('agents.copied')))}><${Icon} n="copy" /></button>
        ${!deleted && html`<button type="button" class="icon-btn cm-chg-eye" aria-label=${t('agents.preview.open')} title=${t('agents.preview.open')} onClick=${() => onPreview(file.path)}><${Icon} n="eye" /></button>`}
      </div>
    </div>
    <div class="cm-chg-wrap" aria-hidden=${String(!open)}>
      <div class="cm-chg-inner">
        ${(open || diff !== null) && html`<div class="cm-chg-body">
          ${diff === null ? html`<div class="cv-diff-wait"><span class="spinner"></span></div>`
            : diff.error ? html`<p class="cv-diff-err">${diff.error}</p>`
            : diff.data?.binary ? html`<p class="muted cm-chg-msg">${t('agents.git.binary')}</p>`
            : !diff.data?.diff ? html`<p class="muted cm-chg-msg">${t('agents.git.clean')}</p>`
            : html`<${DiffLines} text=${diff.data.diff} truncated=${diff.data.truncated} />`}
          ${diff?.data?.truncated && html`<p class="muted cm-chg-msg">${t('agents.git.truncated')}</p>`}
        </div>`}
      </div>
    </div>
  </div>`;
}

function EditChangeRow({ id, workingDir, file, onPreview }) {
  const [open, setOpen] = useState(false);
  const [diff, setDiff] = useState(null);
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next && diff === null) {
      Promise.all((file.ids || []).map(eid => api.editDiff(id, eid)))
        .then(parts => setDiff({
          data: {
            diff: parts.map(p => p?.diff || '').filter(Boolean).join('\n'),
            truncated: parts.some(p => p?.truncated),
          },
        }))
        .catch(e => setDiff({ error: e.message }));
    }
  };
  const displayPath = normalizeSessionPath(file.path, workingDir) || file.path;
  const { dir, name } = splitPath(displayPath);
  const code = file.created ? 'A' : 'M';
  const kind = file.created ? 'staged' : 'unstaged';
  return html`<div class=${cls('cm-chg-file', open && 'open')}>
    <div class="cm-chg-row">
      <button type="button" class="cm-chg-btn" aria-expanded=${String(open)} onClick=${toggle}>
        <${Icon} n="right" class="cm-chg-caret" />
        <span class="cm-chg-path" title=${file.path}><span class="name">${name}</span>${dir && html`<span class="dir">${dir}</span>`}</span>
        ${(file.added > 0 || file.removed > 0) && html`<span class="cm-chg-stat">${file.added > 0 && html`<em class="a">+${file.added}</em>`}${file.removed > 0 && html`<em class="d">-${file.removed}</em>`}</span>`}
        <i class=${'gk ' + kind} title=${file.created ? t('agents.changes.created') : t('agents.changes.modified')}>${code}</i>
      </button>
      <div class="cm-chg-acts">
        <button type="button" class="icon-btn cm-chg-eye" aria-label=${t('agents.files.copy_path')} title=${t('agents.files.copy_path')} onClick=${() => copyText(displayPath).then(() => toast(t('agents.copied')))}><${Icon} n="copy" /></button>
        <button type="button" class="icon-btn cm-chg-eye" aria-label=${t('agents.preview.open')} title=${t('agents.preview.open')} onClick=${() => onPreview(displayPath)}><${Icon} n="eye" /></button>
      </div>
    </div>
    <div class="cm-chg-wrap" aria-hidden=${String(!open)}>
      <div class="cm-chg-inner">
        ${(open || diff !== null) && html`<div class="cm-chg-body">
          ${diff === null ? html`<div class="cv-diff-wait"><span class="spinner"></span></div>`
            : diff.error ? html`<p class="cv-diff-err">${diff.error}</p>`
            : !diff.data?.diff ? html`<p class="muted cm-chg-msg">${t('agents.git.clean')}</p>`
            : html`<${DiffLines} text=${diff.data.diff} truncated=${diff.data.truncated} />`}
          ${diff?.data?.truncated && html`<p class="muted cm-chg-msg">${t('agents.git.truncated')}</p>`}
        </div>`}
      </div>
    </div>
  </div>`;
}

function ChangesTab({ id, workingDir, editsState, refreshEdits, gitState, refreshGit }) {
  const agentChanges = useMemo(() => changedFiles(editsState.data?.entries || []), [editsState.data]);
  const hasAgentEdits = agentChanges.files.length > 0;
  const refreshAll = () => { refreshEdits(true); refreshGit(true); };

  if (!hasAgentEdits && gitState.error) return html`<${Failed} error=${gitState.error} retry=${refreshAll} />`;
  if (!hasAgentEdits && (gitState.loading && !gitState.data) && (editsState.loading && !editsState.data)) return html`<${Loading} />`;

  const d = gitState.data || {};
  const repos = Array.isArray(d.repos) ? d.repos : [];
  const sortedRepos = useMemo(() => [...repos].sort((a, b) => {
    const ca = a?.status?.counts?.uncommitted || a?.status?.files?.length || 0;
    const cb = b?.status?.counts?.uncommitted || b?.status?.files?.length || 0;
    return (cb > 0 ? 1 : 0) - (ca > 0 ? 1 : 0);
  }), [repos]);
  const showGit = !gitState.error && d.state !== 'not-a-repo' && d.state !== 'unsupported' && d.state !== 'error' && sortedRepos.length > 0;

  if (!hasAgentEdits && !showGit) {
    if (d.state === 'not-a-repo') return html`<${Empty} icon="folder" title=${t('agents.panel.changes')} text=${t('agents.git.none')} />`;
    if (d.state === 'unsupported') return html`<p class="muted">${t('agents.git.unsupported')}</p>`;
    if (d.state === 'error') return html`<p class="muted">${d.error}</p>`;
    return html`<${Empty} icon="folder" title=${t('agents.panel.changes')} text=${t('agents.git.clean')} />`;
  }

  const checkedTitle = d.checkedAt ? t('agents.git.checked', { time: time(d.checkedAt) }) : t('common.refresh');
  return html`<div class="cm-git">
    ${hasAgentEdits && html`<section class="cm-repo cm-agent-edits">
      <header class="cm-repo-h">
        <div class="cm-repo-id">
          <b class="trunc">${t('agents.changes.by_agent')}</b>
          <span class="cm-branch mono">${agentChanges.files.length}</span>
          ${(agentChanges.added > 0 || agentChanges.removed > 0) && html`<span class="cm-chg-stat">
            ${agentChanges.added > 0 && html`<em class="a">+${agentChanges.added}</em>`}
            ${agentChanges.removed > 0 && html`<em class="d">-${agentChanges.removed}</em>`}
          </span>`}
        </div>
        <button type="button" class="icon-btn cm-repo-ref" aria-label=${t('common.refresh')} title=${checkedTitle} onClick=${refreshAll}><${Icon} n="refresh" /></button>
      </header>
      <div class="cm-chg-list">${agentChanges.files.map(f => html`<${EditChangeRow} key=${f.path} id=${id} workingDir=${workingDir} file=${f} onPreview=${p => openPreview(id, p)} />`)}</div>
    </section>`}
    ${showGit && html`${hasAgentEdits && html`<div class="cm-chg-subh">${t('agents.changes.workspace')}</div>`}
      ${sortedRepos.map((repo, idx) => {
        const s = repo.status || {};
        const files = s.files || [];
        const count = s.counts?.uncommitted || files.length;
        return html`<section class="cm-repo" key=${repo.path}>
          <header class="cm-repo-h">
            <div class="cm-repo-id">
              <b class="trunc">${repo.name}</b>
              <span class="cm-branch mono trunc">${s.branch || t('agents.git.detached')}</span>
              ${s.ahead ? html`<span class="cm-sync up">↑${s.ahead}</span>` : ''}
              ${s.behind ? html`<span class="cm-sync down">↓${s.behind}</span>` : ''}
            </div>
            ${!hasAgentEdits && idx === 0 && html`<button type="button" class="icon-btn cm-repo-ref" aria-label=${t('common.refresh')} title=${checkedTitle} onClick=${refreshAll}><${Icon} n="refresh" /></button>`}
          </header>
          ${s.state === 'error' ? html`<p class="muted cm-repo-sum">${s.error}</p>` : html`
            <div class="cm-repo-meta">
              <span>${count ? t('agents.git.changes', { n: count }) : t('agents.git.clean')}</span>
              ${s.unpushedCount ? html`<span>· ${t('agents.git.unpushed', { n: s.unpushedCount })}</span>` : ''}
            </div>
            ${files.length > 0 && html`<div class="cm-chg-list">${files.map(f => html`<${ChangeRow} key=${f.kind + ':' + f.path} id=${id} repoRoot=${s.repoRoot} file=${f} onPreview=${p => openPreview(id, p)} />`)}</div>`}
            ${(s.unpushed || []).length > 0 && html`<details class="cm-commits"><summary>${t('agents.git.commits')} (${s.unpushed.length})</summary><div class="cm-commit-list">${s.unpushed.map(c => html`<div class="cm-commit-row" key=${c.hash}><code>${c.hash.slice(0, 7)}</code><span class="trunc">${c.subject}</span></div>`)}</div></details>`}`}
        </section>`;
      })}`}
  </div>`;
}

function filterTree(nodes, q) {
  if (!q) return nodes || [];
  const out = [];
  for (const n of nodes || []) {
    if (n.type === 'directory') {
      const kids = filterTree(n.children, q);
      if (kids.length || String(n.name || '').toLowerCase().includes(q)) out.push({ ...n, children: kids });
    } else if (String(n.path || n.name || '').toLowerCase().includes(q)) {
      out.push(n);
    }
  }
  return out;
}

function Tree({ nodes, depth, onOpen, forceOpen }) {
  const [open, setOpen] = useState({});
  return html`<ul class="cm-tree">${(nodes || []).map(n => {
    const expanded = forceOpen || !!open[n.path];
    return html`<li key=${n.path}>
      <button type="button" style=${`padding-left:${6 + depth * 12}px`} onClick=${() => n.type === 'directory' ? setOpen({ ...open, [n.path]: !expanded }) : onOpen(n.path)}>
        <${Icon} n=${n.type === 'directory' ? (expanded ? 'chevron' : 'right') : 'file'} /><span class="trunc">${n.name}</span></button>
      ${n.type === 'directory' && expanded && html`<${Tree} nodes=${n.children} depth=${depth + 1} onOpen=${onOpen} forceOpen=${forceOpen} />`}</li>`;
  })}</ul>`;
}

function FilesTab({ id }) {
  const [st, run] = useRead(() => api.files(id), [id]);
  const [query, setQuery] = useState('');
  const q = query.trim().toLowerCase();
  const filtered = useMemo(() => filterTree(st.data?.tree, q), [st.data?.tree, q]);
  if (st.error) return html`<${Failed} error=${st.error} retry=${run} />`;
  if (st.loading && !st.data) return html`<${Loading} />`;
  return html`<div class="cm-files-tab">
    <div class="cm-insp-bar">
      <div class="search cm-files-search">
        <${Icon} n="search" />
        <input type="search" value=${query} placeholder=${t('agents.files.filter')} aria-label=${t('agents.files.filter')} onInput=${e => setQuery(e.target.value)} />
        ${query && html`<button type="button" class="icon-btn" aria-label=${t('agents.insp.close')} onClick=${() => setQuery('')}><${Icon} n="close" /></button>`}
      </div>
      <button type="button" class="icon-btn" aria-label=${t('common.refresh')} onClick=${() => run()}><${Icon} n="refresh" /></button>
    </div>
    ${st.data?.root && html`<div class="cm-files-root mono trunc" title=${st.data.root}>${st.data.root}</div>`}
    ${filtered.length ? html`<${Tree} nodes=${filtered} depth=${0} onOpen=${p => openPreview(id, p)} forceOpen=${!!q} />`
      : html`<p class="muted">${q ? t('agents.files.no_match') : t('agents.insp.no_activity')}</p>`}
  </div>`;
}

function SubagentView({ agent, onBack }) {
  const [st, run] = useRead(() => api.subagentTranscript(agent.agentId), [agent.agentId]);
  const f = st.data?.formatted;
  const text = Array.isArray(f) ? f.join('\n') : typeof f === 'string' ? f : '';
  return html`<div class="cm-sub-view">
    <div class="cm-sub-head">
      <button type="button" class="btn sm" onClick=${onBack}><${Icon} n="left" /><span>${t('agents.sub.back')}</span></button>
      <span class="grow"></span>
      ${text && html`<button type="button" class="icon-btn" aria-label=${t('chat.messages.copy')} title=${t('chat.messages.copy')} onClick=${() => copyText(text).then(() => toast(t('chat.messages.copied')))}><${Icon} n="copy" /></button>`}
      <button type="button" class="icon-btn" aria-label=${t('common.refresh')} onClick=${() => run()}><${Icon} n="refresh" /></button>
    </div>
    <div class="cm-sub-meta">
      ${agent.status === 'active' ? html`<span class="loom-pulse" aria-hidden="true"><i></i><i></i><i></i><i></i></span>` : html`<${Icon} n="agent" />`}
      <div class="grow">
        <b class=${cls(agent.status === 'active' && 'shimmer-text')}>${agent.description || agent.agentId}</b>
        <small class="muted">${[agent.model, agent.status, agent.toolCallCount ? t('agents.sub.tools', { n: agent.toolCallCount }) : ''].filter(Boolean).join(' · ')}</small>
      </div>
    </div>
    ${st.loading && !st.data ? html`<${Loading} />`
      : st.error ? html`<${Failed} error=${st.error} retry=${run} />`
      : !text ? html`<p class="muted">${t('agents.sub.empty_transcript')}</p>`
      : html`<pre class="cm-preview-code cm-sub-log">${text}</pre>`}
  </div>`;
}

function SpawnSubagentForm({ session, onCancel, onSpawned }) {
  const allClis = useStore(agents, s => s.clis);
  const clis = useMemo(() => allClis.filter(c => c.enabled !== false && c.installed !== false), [allClis]);
  const [role, setRole] = useState('');
  const [cli, setCli] = useState(session.mode && session.mode !== 'shell' ? session.mode : (clis[0]?.id || 'claude'));
  const [workspace, setWorkspace] = useState('worktree');
  const [task, setTask] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async e => {
    e.preventDefault();
    if (!role.trim() || !task.trim() || busy) return;
    setBusy(true);
    try {
      const created = await spawnSubagent({
        parent_id: session.id,
        role: role.trim(),
        cli,
        workspace,
        working_dir: session.workingDir || '',
        task: task.trim(),
      });
      toast(t('agents.orch.spawned', { role: created.role || role.trim() }));
      onSpawned?.(created);
    } catch (err) {
      toast(err.message, 'err');
    } finally {
      setBusy(false);
    }
  };

  return html`<form class="cm-orch-spawn" onSubmit=${submit}>
    <div class="cm-orch-spawn-h">
      <b>${t('agents.orch.new_subagent')}</b>
      <button type="button" class="icon-btn" aria-label=${t('agents.insp.close')} onClick=${onCancel}><${Icon} n="close" /></button>
    </div>
    <input class="input" value=${role} placeholder=${t('agents.orch.role_ph')} aria-label=${t('agents.orch.role')} onInput=${e => setRole(e.target.value)} required />
    <div class="cm-orch-spawn-row">
      <select class="input" value=${cli} aria-label=${t('agents.new.cli')} onChange=${e => setCli(e.target.value)}>
        ${(clis.length ? clis : [{ id: 'claude', label: 'Claude Code' }, { id: 'codex', label: 'Codex' }]).map(c => html`<option key=${c.id} value=${c.id}>${c.label}</option>`)}
      </select>
      <${Seg} size="sm" label=${t('agents.orch.workspace')} value=${workspace} onChange=${setWorkspace} options=${[
        { value: 'worktree', label: t('agents.orch.ws_worktree') },
        { value: 'inherit', label: t('agents.orch.ws_inherit') },
      ]} />
    </div>
    <textarea class="textarea" rows="3" value=${task} placeholder=${t('agents.orch.task_ph')} aria-label=${t('agents.orch.task')} onInput=${e => setTask(e.target.value)} required></textarea>
    <div class="cm-orch-spawn-foot">
      <button type="button" class="btn sm" disabled=${busy} onClick=${onCancel}>${t('agents.cancel')}</button>
      <button type="submit" class="btn sm primary" disabled=${busy || !role.trim() || !task.trim()}>${busy ? t('agents.new.starting') : t('agents.orch.launch')}</button>
    </div>
  </form>`;
}

function BoardView({ rootId, nodes }) {
  const messages = useStore(orchestration, o => o.boards[rootId] || []);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (rootId) loadBoard(rootId).catch(() => {}); }, [rootId]);

  const send = async e => {
    e.preventDefault();
    if (!text.trim() || busy) return;
    setBusy(true);
    try {
      await postBoard(rootId, text.trim(), 'human');
      setText('');
    } catch (err) {
      toast(err.message, 'err');
    } finally {
      setBusy(false);
    }
  };

  const labelFor = sid => {
    if (!sid) return '';
    const n = nodes[sid];
    return n?.role || n?.name || sid.slice(0, 8);
  };

  const kindLabel = k => ({
    update: t('agents.orch.kind.update'),
    question: t('agents.orch.kind.question'),
    result: t('agents.orch.kind.result'),
    handoff: t('agents.orch.kind.handoff'),
    human: t('agents.orch.kind.human'),
  })[k] || t('agents.orch.kind.update');

  return html`<div class="cm-board">
    <div class="cm-board-list">
      ${messages.length === 0 ? html`<${Empty} icon="chat" title=${t('agents.orch.board')} text=${t('agents.orch.board_empty')} />`
        : messages.map(m => html`<article key=${m.id} class=${cls('cm-board-msg', 'kind-' + (m.kind || 'update'))}>
          <header class="cm-board-meta">
            <b>${m.from_role || labelFor(m.from_session) || t('agents.orch.agent')}</b>
            ${m.to_session && html`<span class="muted">→ ${labelFor(m.to_session)}</span>`}
            <span class="cm-board-kind">${kindLabel(m.kind)}</span>
            <span class="grow"></span>
            ${m.created_at && html`<time class="muted">${time(m.created_at)}</time>`}
          </header>
          <p class="cm-board-body">${m.text}</p>
        </article>`)}
    </div>
    <form class="cm-board-form" onSubmit=${send}>
      <input class="input" value=${text} placeholder=${t('agents.orch.board_ph')} aria-label=${t('agents.orch.board_ph')} onInput=${e => setText(e.target.value)} disabled=${busy} />
      <button type="submit" class="btn sm primary" disabled=${busy || !text.trim()}>${t('agents.orch.board_send')}</button>
    </form>
  </div>`;
}

function SubagentsTab({ session, subState, refreshSub }) {
  const [selected, setSelected] = useState(null);
  const [view, setView] = useState('sessions');
  const [spawning, setSpawning] = useState(false);
  const [stopping, setStopping] = useState(false);
  const orchNodes = useStore(orchestration, o => o.nodes);
  const boardCounts = useStore(orchestration, o => o.boardCounts);
  const liveSessions = useStore(agents, a => a.sessions);
  const nativeList = Array.isArray(subState.data) ? subState.data : [];

  const curNode = orchNodes[session.id] || null;
  const rootId = rootIdOf(session.id, orchNodes);
  const parentLive = curNode?.parent_id ? liveSessions[curNode.parent_id] : null;
  const loomChildren = useMemo(() => descendantsOf(session.id, orchNodes), [session.id, orchNodes]);
  const activeChildren = loomChildren.filter(n => n.status !== 'stopped' && liveSessions[n.session_id]);
  const boardCount = boardCounts[rootId] || 0;
  const showBoardSwitch = loomChildren.length > 0 || !!curNode?.parent_id || boardCount > 0;

  useEffect(() => {
    if (selected && !nativeList.some(a => a.agentId === selected.agentId)) setSelected(null);
  }, [nativeList.length]);
  useEffect(() => { setSpawning(false); }, [session.id]);

  if (selected) return html`<${SubagentView} agent=${selected} onBack=${() => setSelected(null)} />`;

  const doStopTree = async () => {
    if (!await confirm(t('agents.orch.stop_tree_title'), t('agents.orch.stop_tree_text'), { ok: t('agents.orch.stop_tree'), danger: true })) return;
    setStopping(true);
    try {
      await stopTree(session.id, { mode: 'close' });
      toast(t('agents.orch.tree_stopped'));
    } catch (e) { toast(e.message, 'err'); }
    finally { setStopping(false); }
  };

  const quickSend = async child => {
    const msg = await prompt(t('agents.orch.send_to', { name: child.role || child.name }), { placeholder: t('agents.orch.send_ph') });
    if (!msg) return;
    try { await sendPrompt(child.session_id, msg); toast(t('agents.orch.sent')); } catch (e) { toast(e.message, 'err'); }
  };

  const quickClose = async child => {
    if (!await confirm(t('agents.session.close_title'), t('agents.session.close_text', { name: child.name || child.role || child.session_id.slice(0, 8) }), { ok: t('agents.session.close'), danger: true })) return;
    try { await closeSession(child.session_id, true); } catch (e) { toast(e.message, 'err'); }
  };

  const quickCleanWT = async child => {
    try { await cleanupWorktree(child.session_id, false); toast(t('agents.orch.wt_cleaned')); }
    catch (e) { toast(e.message, 'err'); }
  };

  return html`<div class="cm-subs-wrap">
    <div class="cm-insp-bar cm-orch-bar">
      ${showBoardSwitch ? html`<${Seg} size="sm" label=${t('agents.insp.subagents')} value=${view} onChange=${setView} options=${[
        { value: 'sessions', label: t('agents.orch.tab_sessions') + (loomChildren.length ? ` (${loomChildren.length})` : '') },
        { value: 'board', label: t('agents.orch.board') + (boardCount ? ` (${boardCount})` : '') },
      ]} />` : html`<span class="muted">${t('agents.orch.loom_sessions')}</span>`}
      <span class="grow"></span>
      ${view === 'sessions' && !spawning && html`<button type="button" class="btn sm" onClick=${() => setSpawning(true)}><${Icon} n="plus" /><span>${t('agents.orch.spawn_btn')}</span></button>`}
      ${view === 'sessions' && activeChildren.length > 0 && html`<button type="button" class="btn sm danger" disabled=${stopping} onClick=${doStopTree} title=${t('agents.orch.stop_tree')}><${Icon} n="stop" /></button>`}
      <button type="button" class="icon-btn" aria-label=${t('common.refresh')} onClick=${() => { refreshSub(); if (rootId) loadBoard(rootId).catch(() => {}); }}><${Icon} n="refresh" /></button>
    </div>

    ${parentLive && html`<button type="button" class="cm-orch-parent" onClick=${() => openSession(parentLive.id)}>
      <${Icon} n="left" />
      <span class="trunc"><small class="muted">${t('agents.orch.parent_session')}</small><b>${parentLive.name || parentLive.id.slice(0, 8)}</b></span>
      ${curNode?.branch && html`<code class="cm-branch mono trunc">${curNode.branch}</code>`}
    </button>`}

    ${spawning && html`<${SpawnSubagentForm} session=${session} onCancel=${() => setSpawning(false)} onSpawned=${() => setSpawning(false)} />`}

    ${view === 'board' ? html`<${BoardView} rootId=${rootId} nodes=${orchNodes} />` : html`
      ${loomChildren.length > 0 && html`<section class="cm-orch-sec">
        <div class="cm-orch-sec-h"><span>${t('agents.orch.loom_sessions')}</span><small>${activeChildren.length}/${loomChildren.length}</small></div>
        <ul class="cm-orch-list">${loomChildren.map(child => {
          const live = liveSessions[child.session_id] || null;
          const st = live ? attentionOf(live) : 'stopped';
          const busy = st === 'working' || st === 'starting';
          return html`<li key=${child.session_id} class=${cls('cm-orch-card', !live && 'is-stopped')}>
            <button type="button" class="cm-orch-main" disabled=${!live} onClick=${() => live && openSession(child.session_id)}>
              ${busy ? html`<span class="loom-pulse sm" aria-hidden="true"><i></i><i></i><i></i><i></i></span>` : html`<${CliMark} mode=${child.cli || live?.mode || 'claude'} size="xs" />`}
              <span class="cm-orch-copy">
                <b class=${cls('trunc', busy && 'shimmer-text')}>${child.role || child.name || child.session_id.slice(0, 8)}</b>
                <small class="muted trunc">${[cliOf(child.cli || live?.mode).label, child.branch, lt('state', st)].filter(Boolean).join(' · ')}</small>
              </span>
            </button>
            <div class="cm-orch-acts">
              ${live && html`<button type="button" class="icon-btn" aria-label=${t('agents.orch.send_msg')} title=${t('agents.orch.send_msg')} onClick=${() => quickSend(child)}><${Icon} n="chat" /></button>`}
              ${live && html`<button type="button" class="icon-btn" aria-label=${t('agents.session.close')} title=${t('agents.session.close')} onClick=${() => quickClose(child)}><${Icon} n="close" /></button>`}
              ${!live && child.worktree_path && html`<button type="button" class="btn sm" onClick=${() => quickCleanWT(child)}>${t('agents.orch.clean_wt')}</button>`}
            </div>
          </li>`;
        })}</ul>
      </section>`}

      ${nativeList.length > 0 && html`<section class="cm-orch-sec">
        <div class="cm-orch-sec-h"><span>${t('agents.orch.native_subagents')}</span><small>${nativeList.length}</small></div>
        <ul class="cm-subs">${nativeList.map(a => html`<li key=${a.agentId}><button type="button" onClick=${() => setSelected(a)}>
          ${a.status === 'active' ? html`<span class="loom-pulse" aria-hidden="true"><i></i><i></i><i></i><i></i></span>` : html`<${Icon} n="agent" class="cm-sub-ico" />`}
          <span class="grow">
            <b class=${cls('trunc', a.status === 'active' && 'shimmer-text')}>${a.description || a.agentId}</b>
            <small class="muted">${[a.model, a.status, a.toolCallCount ? t('agents.sub.tools', { n: a.toolCallCount }) : ''].filter(Boolean).join(' · ')}</small>
          </span>
          <${Icon} n="right" class="cm-sub-chev" />
        </button></li>`)}</ul>
      </section>`}

      ${!loomChildren.length && !nativeList.length && !spawning && (subState.loading && !subState.data ? html`<${Loading} />`
        : subState.error ? html`<${Failed} error=${subState.error} retry=${refreshSub} />`
        : html`<${Empty} icon="agent" title=${t('agents.insp.subagents')} text=${t('agents.sub.none')} />`)}
    `}
  </div>`;
}

async function readOutsideFile(absPath) {
  const rel = String(absPath || '').replace(/^\/+/, '');
  if (!rel) return { outside: true };
  const r = await request('/api/files/read?scope=machine&path=' + encodeURIComponent(rel), { timeout: 15000 });
  let d = null;
  try { d = await r.json(); } catch (_) {}
  if (!r.ok || !d || d.ok === false) return { outside: true };
  if (d.binary || d.too_large) return { binary: true, truncated: !!d.too_large };
  return { content: typeof d.content === 'string' ? d.content : null };
}

function PreviewTab({ session, preview }) {
  const id = session.id;
  const activePath = preview.active || preview.detected[0] || preview.recent[0] || '';
  const allDocs = useMemo(() => {
    const out = [];
    const seen = new Set();
    for (const p of [activePath, ...(preview.recent || []), ...(preview.detected || [])]) {
      if (!p || seen.has(p)) continue;
      seen.add(p);
      out.push(p);
    }
    return out.slice(0, 10);
  }, [activePath, preview.recent, preview.detected]);

  const markdown = isMarkdownPath(activePath);
  const [mode, setMode] = useState('rendered');
  useEffect(() => { setMode('rendered'); }, [activePath]);

  const isAbs = activePath.startsWith('/');
  const [st, run] = useRead(
    () => !activePath ? Promise.resolve(null) : isAbs ? readOutsideFile(activePath) : api.fileContent(id, activePath),
    [id, activePath, isAbs],
  );
  const outsideRoot = !!st.data?.outside;
  const content = typeof st.data?.content === 'string' ? st.data.content : null;
  const renderedHtml = useMemo(() => (markdown && mode === 'rendered' && content !== null ? md(content) : ''), [markdown, mode, content]);

  if (!activePath) {
    return html`<${Empty} icon="file" title=${t('agents.insp.preview')} text=${t('agents.preview.empty')} />`;
  }

  const { dir, name } = splitPath(activePath);
  const onMdClick = e => {
    const a = e.target.closest('a');
    if (!a) return;
    const raw = a.getAttribute('data-file-href') || a.getAttribute('href') || '';
    const target = normalizeSessionPath(raw, session.workingDir);
    if (target) {
      e.preventDefault();
      openPreview(id, target, { openPanel: false });
    }
  };

  return html`<div class="cm-preview">
    ${allDocs.length > 1 && html`<div class="cm-preview-pills">${allDocs.map(p => {
      const sp = splitPath(p);
      return html`<button type="button" key=${p} class=${cls('cm-preview-pill', p === activePath && 'on')} title=${p} onClick=${() => openPreview(id, p, { openPanel: false })}>
        <${Icon} n="file" /><span class="trunc">${sp.name}</span></button>`;
    })}</div>`}
    <div class="cm-preview-head">
      <div class="cm-preview-title" title=${activePath}>
        <b class="trunc">${name}</b>
        ${dir && html`<small class="mono trunc">${dir}</small>`}
      </div>
      <div class="cm-preview-acts">
        ${markdown && html`<div class="cm-preview-mode" role="group" aria-label=${t('agents.insp.preview')}>
          <button type="button" class=${cls(mode === 'rendered' && 'on')} onClick=${() => setMode('rendered')}>${t('agents.preview.rendered')}</button>
          <button type="button" class=${cls(mode === 'raw' && 'on')} onClick=${() => setMode('raw')}>${t('agents.preview.raw')}</button>
        </div>`}
        <button type="button" class="icon-btn" aria-label=${t('agents.files.copy_path')} title=${t('agents.files.copy_path')} onClick=${() => copyText(activePath).then(() => toast(t('agents.copied')))}><${Icon} n="copy" /></button>
        <button type="button" class="icon-btn" aria-label=${t('common.refresh')} title=${t('common.refresh')} onClick=${() => run()}><${Icon} n="refresh" /></button>
        <button type="button" class="icon-btn" aria-label=${t('agents.preview.close')} title=${t('agents.preview.close')} onClick=${() => closePreview(id)}><${Icon} n="close" /></button>
      </div>
    </div>
    <div class="cm-preview-body">
      ${st.loading && !st.data ? html`<${Loading} />`
        : outsideRoot ? html`<div class="cm-preview-msg"><${Icon} n="info" /><span>${t('agents.preview.outside')}</span></div>`
        : st.error ? html`<${Failed} error=${st.error} retry=${run} />`
        : content === null ? html`<p class="muted">${t('agents.files.binary')}</p>`
        : markdown && mode === 'rendered' ? html`<div class="md cm-preview-md" onClick=${onMdClick} dangerouslySetInnerHTML=${{ __html: renderedHtml }}></div>`
        : html`<pre class="cm-preview-code">${content}</pre>`}
      ${st.data?.truncated && html`<p class="muted cm-chg-msg">${t('agents.files.truncated')}</p>`}
    </div>
  </div>`;
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
    <${Row} k=${t('agents.info.id')}><button type="button" class="btn sm mono" onClick=${() => copyText(session.id).then(() => toast(t('agents.copied')))}>${session.id.slice(0, 8)}…</button></${Row}>
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
  return html`<button type="button" class=${cls(phone ? 'btn sm' : 'icon-btn', menu && 'on')} aria-label=${t('agents.session.actions')} onClick=${e => setMenu(menu ? null : e.currentTarget)}><${Icon} n="more" />${phone && t('mobile.session_options_short')}</button>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}`;
}

// Sections repliables secondaires dans l'onglet Session (accordéon fluide 0fr → 1fr).
const openKey = 'loom.codeman.panel.open';
const readOpen = () => { try { return JSON.parse(localStorage.getItem(openKey)) || []; } catch (_) { return []; } };
function Section({ id, label, hint, open, toggle, children }) {
  const [seen, setSeen] = useState(open);
  useEffect(() => { if (open && !seen) setSeen(true); }, [open, seen]);
  return html`<section class=${cls('cm-ps', open && 'open')}>
    <button type="button" class="cm-ps-h" aria-expanded=${String(open)} onClick=${() => toggle(id)}><span>${label}</span>${hint && html`<small>${hint}</small>`}<${Icon} n="right" class="cm-ps-caret" /></button>
    <div class="cm-ps-wrap" aria-hidden=${String(!open)}><div class="cm-ps-inner">${seen && html`<div class="cm-ps-b">${children}</div>`}</div></div>
  </section>`;
}

const CLAUDE_MODELS = ['opus', 'sonnet', 'haiku'];
const CLAUDE_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
const effortKey = id => 'loom.codeman.effort.' + id;
const effortLabel = e => ({ low: t('agents.admin.low'), medium: t('agents.admin.medium'), high: t('agents.admin.high'), xhigh: t('agents.admin.xhigh'), max: t('agents.admin.max') })[e] || e;
function ModelRow({ s }) {
  const [menu, setMenu] = useState(null);
  const [effortMenu, setEffortMenu] = useState(null);
  const [effort, setEffort] = useState(() => { try { return localStorage.getItem(effortKey(s.id)) || ''; } catch (_) { return ''; } });
  const model = s.displayModel?.model || '';
  const run = cmd => queueCommand(s.id, cmd)
    .then(r => toast(r === 'queued' ? t('agents.panel.queued', { cmd }) : t('agents.panel.sent', { cmd })))
    .catch(e => toast(e.message, 'err'));
  if (s.mode !== 'claude') return model ? html`<div class="cm-kv"><span>${t('agents.info.model')}</span><b>${model}</b></div>` : null;
  const pickEffort = e => { setEffort(e); try { localStorage.setItem(effortKey(s.id), e); } catch (_) {} run('/effort ' + e); };
  return html`<div class="cm-kv"><span>${t('agents.info.model')}</span>
    <${DropdownButton} class="sm fit" open=${menu} onClick=${e => setMenu(e.currentTarget)}><span class="trunc">${model || t('agents.new.model_ph')}</span></${DropdownButton}>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${CLAUDE_MODELS.map(m => ({ label: m[0].toUpperCase() + m.slice(1), icon: model.toLowerCase().includes(m) ? 'check' : '', run: () => run('/model ' + m) }))} />`}</div>
  <div class="cm-kv"><span>${t('agents.cfg.effort')}</span>
    <${DropdownButton} class="sm fit" open=${effortMenu} onClick=${e => setEffortMenu(e.currentTarget)}><span class="trunc">${effort ? effortLabel(effort) : t('agents.cfg.cli_default')}</span></${DropdownButton}>
    ${effortMenu && html`<${Menu} anchor=${effortMenu} onClose=${() => setEffortMenu(null)} items=${CLAUDE_EFFORTS.map(e => ({ label: effortLabel(e), icon: e === effort ? 'check' : '', run: () => pickEffort(e) }))} />`}</div>`;
}

function OthersTab({ s }) {
  const others = sessionList().filter(x => x.mode === s.mode && x.id !== s.id);
  if (!others.length) return html`<p class="muted">${t('agents.panel.no_others')}</p>`;
  return html`<ul class="cm-others">${others.map(o => html`<li key=${o.id}><button type="button" onClick=${() => openSession(o.id)}><span class="trunc">${o.name || o.id.slice(0, 8)}</span><small class="muted">${projectOf(o.workingDir)} · ${ago(o.lastActivityAt)}</small></button></li>`)}</ul>`;
}

export function Inspector({ session, open: isOpen = true, onClose, view, setView, narrowView }) {
  const [open, setOpen] = useState(readOpen);
  const [busy, setBusy] = useState('');
  const [mounted, setMounted] = useState(!!isOpen);
  useEffect(() => { if (isOpen && !mounted) setMounted(true); }, [isOpen, mounted]);
  const tab = useStore(panelState, p => p.tab);
  const width = useStore(panelState, p => p.width);
  const preview = useStore(panelState, p => previewFor(p, session?.id));
  const turn = useStore(agents, a => (session && a.turns[session.id]) || 0);
  const sid = session?.id || '';
  const liveFetch = !!(isOpen && sid);

  // Chargements légers partagés par les badges d'onglet et leurs vues dédiées (uniquement quand ouvert).
  const [editsState, refreshEdits] = useRead(sync => liveFetch ? api.edits(sid, !!sync).catch(() => ({ entries: [] })) : Promise.resolve({ entries: [] }), [sid, liveFetch]);
  const [gitState, refreshGit] = useRead(fresh => liveFetch ? api.gitStatus(sid, fresh) : Promise.resolve(null), [sid, liveFetch]);
  const [subState, refreshSub] = useRead(() => liveFetch ? api.subagents(sid) : Promise.resolve([]), [sid, liveFetch]);
  useEffect(() => { if (liveFetch && turn) { refreshEdits(true); refreshGit(false); } }, [sid, turn, liveFetch]);
  useEffect(() => (liveFetch ? onDomain('subagent', () => refreshSub()) : undefined), [sid, liveFetch]);

  const dragRef = useRef(null);
  const tabNavRef = useRef(null);
  const tabIndRef = useRef(null);
  const [tabScroll, setTabScroll] = useState({ left: false, right: false });
  useEffect(() => () => { document.body.classList.remove('cm-dragging'); }, []);

  const agentEditCount = useMemo(() => changedFiles(editsState.data?.entries || []).files.length, [editsState.data]);
  const gitChangesCount = (gitState.data?.repos || []).reduce((acc, r) => acc + (r?.status?.counts?.uncommitted || r?.status?.files?.length || 0), 0);
  const changesCount = agentEditCount || gitChangesCount;
  const orchNodes = useStore(orchestration, o => o.nodes);
  const liveSessions = useStore(agents, a => a.sessions);
  const loomSubs = useMemo(() => (sid ? descendantsOf(sid, orchNodes).filter(n => n.status !== 'stopped' && liveSessions[n.session_id]) : []), [sid, orchNodes, liveSessions]);
  const loomSubActive = loomSubs.some(n => {
    const st = attentionOf(liveSessions[n.session_id]);
    return st === 'working' || st === 'starting';
  });
  const subList = Array.isArray(subState.data) ? subState.data : [];
  const subTotal = subList.length + loomSubs.length;
  const subActive = loomSubActive || subList.some(a => a.status === 'active');
  const hasPreview = !!(preview.active || preview.detected.length || preview.recent.length);
  const activeTab = tab === 'preview' && !hasPreview ? 'session' : tab;

  // Indicateur animé glissant sous l'onglet actif + détection du débordement horizontal.
  useLayoutEffect(() => {
    const nav = tabNavRef.current, ind = tabIndRef.current;
    if (!nav || !ind || !isOpen) return;
    const syncScroll = () => {
      const left = nav.scrollLeft > 4;
      const right = nav.scrollLeft + nav.clientWidth < nav.scrollWidth - 4;
      setTabScroll(prev => (prev.left === left && prev.right === right ? prev : { left, right }));
    };
    const place = () => {
      const cur = nav.querySelector('.cm-ptab[aria-selected="true"]');
      if (!cur) { ind.style.opacity = '0'; }
      else {
        ind.style.opacity = '1';
        ind.style.width = cur.offsetWidth + 'px';
        ind.style.transform = `translateX(${cur.offsetLeft}px)`;
      }
      syncScroll();
    };
    place();
    const cur = nav.querySelector('.cm-ptab[aria-selected="true"]');
    if (cur && typeof cur.scrollIntoView === 'function') cur.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    nav.addEventListener('scroll', syncScroll, { passive: true });
    const ro = typeof ResizeObserver === 'function' ? new ResizeObserver(place) : null;
    ro?.observe(nav);
    return () => { nav.removeEventListener('scroll', syncScroll); ro?.disconnect(); };
  }, [activeTab, isOpen, hasPreview, changesCount, subList.length, width]);

  const onTabsWheel = e => {
    const nav = tabNavRef.current;
    if (!nav || nav.scrollWidth <= nav.clientWidth) return;
    if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
      e.preventDefault();
      nav.scrollLeft += e.deltaY;
    }
  };
  const nudgeTabs = dir => {
    const nav = tabNavRef.current;
    if (nav) nav.scrollBy({ left: dir * 140, behavior: 'smooth' });
  };

  if (!session) return html`<aside class="cm-insp"><${Empty} icon="info" title=${t('agents.insp.none')} /></aside>`;

  const toggle = secId => {
    const n = open.includes(secId) ? open.filter(x => x !== secId) : [...open, secId];
    setOpen(n);
    try { localStorage.setItem(openKey, JSON.stringify(n)); } catch (_) {}
  };
  const s = session, c = cliOf(s.mode);
  const shown = attentionOf(s) === 'unseen' ? stateOf(s) : attentionOf(s);
  const working = shown === 'working' || shown === 'starting';
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
  const link = officialLink(s.id);

  // Poignée gauche de redimensionnement fluide (souris, tactile, clavier).
  const onResizeDown = e => {
    if (e.button !== 0 && e.pointerType === 'mouse') return;
    e.preventDefault();
    const startX = e.clientX;
    const startW = width;
    dragRef.current = { startX, startW, lastW: startW };
    e.currentTarget.setPointerCapture(e.pointerId);
    document.body.classList.add('cm-dragging');
  };
  const onResizeMove = e => {
    if (!dragRef.current) return;
    const next = setLivePanelWidth(dragRef.current.startW + (dragRef.current.startX - e.clientX));
    dragRef.current.lastW = next;
  };
  const onResizeUp = e => {
    if (!dragRef.current) return;
    const finalW = dragRef.current.lastW;
    dragRef.current = null;
    try { e.currentTarget.releasePointerCapture(e.pointerId); } catch (_) {}
    document.body.classList.remove('cm-dragging');
    savePanelWidth(finalW);
  };
  const onResizeKey = e => {
    if (e.key === 'ArrowLeft') { e.preventDefault(); savePanelWidth(width + 24); }
    else if (e.key === 'ArrowRight') { e.preventDefault(); savePanelWidth(width - 24); }
    else if (e.key === 'Enter' || e.key === '0') { e.preventDefault(); savePanelWidth(DEFAULT_PANEL_WIDTH); }
  };

  const tabs = [
    { id: 'session', label: t('agents.insp.info') },
    { id: 'changes', label: t('agents.panel.changes'), count: changesCount },
    { id: 'files', label: t('agents.insp.files') },
    { id: 'subagents', label: t('agents.insp.subagents'), count: subTotal, active: subActive },
    ...(hasPreview ? [{ id: 'preview', label: t('agents.insp.preview'), dot: !preview.active && preview.detected.length > 0, count: !preview.active && preview.detected.length > 1 ? preview.detected.length : 0 }] : []),
  ];

  const wPx = clampPanelWidth(width);
  const inner = html`<aside class=${cls('cm-insp cm-panel', !narrowView && isOpen && 'open')} style=${!narrowView ? `width:${wPx}px` : undefined} aria-label=${t('agents.insp.title')}>
    ${!narrowView && html`<header class="cm-panel-h">
      <b>${t('agents.insp.info')}</b>
      <span class="grow"></span>
      ${preview.detected.length > 0 && activeTab !== 'preview' && html`<button type="button" class="btn sm cm-panel-peek" title=${preview.detected[0]} onClick=${() => openPreview(s.id, preview.active || preview.detected[0], { openPanel: false })}>
        <${Icon} n="eye" /><span>${t('agents.insp.preview')}</span></button>`}
      <${SessionMenu} s=${s} />
      ${onClose && html`<button type="button" class="icon-btn" aria-label=${t('agents.insp.close')} onClick=${onClose}><${Icon} n="close" /></button>`}
    </header>`}
    <div class=${cls('cm-panel-tabs-wrap', tabScroll.left && 'has-left', tabScroll.right && 'has-right')}>
      ${tabScroll.left && html`<button type="button" class="cm-ptab-scroll left" tabIndex="-1" aria-hidden="true" onClick=${() => nudgeTabs(-1)}><${Icon} n="left" /></button>`}
      <nav class="cm-panel-tabs" role="tablist" aria-label=${t('agents.insp.tabs')} ref=${tabNavRef} onWheel=${onTabsWheel}>
        <span class="cm-ptab-ind" ref=${tabIndRef} aria-hidden="true"></span>
        ${tabs.map(tb => html`<button type="button" key=${tb.id} role="tab" aria-selected=${String(activeTab === tb.id)}
          class=${cls('cm-ptab', activeTab === tb.id && 'on')}
          onClick=${() => {
            if (tb.id === 'preview' && !preview.active && (preview.detected[0] || preview.recent[0])) {
              openPreview(s.id, preview.detected[0] || preview.recent[0], { openPanel: false });
            } else {
              setPanelTab(tb.id);
            }
          }}>
          <span>${tb.label}</span>
          ${tb.active && html`<span class="loom-pulse sm" aria-hidden="true"><i></i><i></i><i></i><i></i></span>`}
          ${tb.dot && html`<i class="cm-ptab-dot" aria-hidden="true"></i>`}
          ${tb.count > 0 && html`<em class="cm-ptab-n">${tb.count}</em>`}
        </button>`)}
      </nav>
      ${tabScroll.right && html`<button type="button" class="cm-ptab-scroll right" tabIndex="-1" aria-hidden="true" onClick=${() => nudgeTabs(1)}><${Icon} n="right" /></button>`}
    </div>
    <div class=${cls('cm-panel-body', activeTab === 'preview' && 'is-preview')}>
      ${mounted && html`<div class="cm-tab-pane" key=${activeTab}>
        ${activeTab === 'session' && html`
          ${!narrowView && setView && hasConversation(s.mode) && html`<${Seg} value=${view} options=${views} onChange=${setView} label=${t('agents.view.label')} />`}
          <div class="cm-hero-card">
            <div class="cm-hero-top">
              <${CliMark} mode=${s.mode} size="sm" />
              <div class="cm-hero-id">
                <b class="trunc">${c.label}${s.cliVersion ? ' ' + s.cliVersion : ''}</b>
                <small class=${cls('cm-hero-state', 'st-' + shown)}>
                  ${working && html`<span class="loom-pulse sm" aria-hidden="true"><i></i><i></i><i></i><i></i></span>`}
                  <span class=${cls(working && 'shimmer-text')}>${lt('state', shown)}</span>
                </small>
              </div>
              ${(s.tokens?.total > 0 || s.totalCost > 0) && html`<div class="cm-hero-stat">
                ${s.tokens?.total > 0 && html`<span>${fmtTok(s.tokens.total)} ${t('agents.panel.tokens')}</span>`}
                ${s.totalCost > 0 && html`<b>$${s.totalCost.toFixed(2)}</b>`}
              </div>`}
            </div>
            ${s.workingDir && html`<button type="button" class="cm-hero-dir" title=${s.workingDir} onClick=${() => copyText(s.workingDir).then(() => toast(t('agents.copied')))}>
              <${Icon} n="folder" /><span class="mono trunc">${s.workingDir}</span><${Icon} n="copy" class="cm-hero-copy" />
            </button>`}
          </div>
          <div class="cm-panel-acts">
            ${narrowView && html`<${SessionMenu} s=${s} phone />`}
            ${s.mode !== 'shell' && html`<button type="button" class="btn sm" disabled=${!!busy} onClick=${shell}>${busy === 'shell' ? html`<span class="spinner"></span>` : html`<${Icon} n="terminal" />`}<span>${t('agents.panel.shell')}</span></button>`}
            ${s.mode === 'claude' ? html`
              <button type="button" class="btn sm" disabled=${!!busy} onClick=${() => slash('/compact')}><${Icon} n="sliders" /><span>${t('agents.panel.compact')}</span></button>
              <button type="button" class="btn sm" disabled=${!!busy} onClick=${() => slash('/clear', true)}><${Icon} n="refresh" /><span>${t('agents.panel.clear')}</span></button>`
            : link ? html`<a class="btn sm" href=${link} target="codeman" rel="noopener"><${Icon} n="link" /><span>${t('agents.panel.codeman')}</span></a>` : ''}
          </div>
          ${(s.mode === 'claude' || s.displayModel?.model) && html`<div class="cm-panel-rows">
            <${ModelRow} s=${s} />
          </div>`}
          ${preview.detected.length > 0 && html`<section class="cm-cited-box">
            <div class="cm-cited-h"><span>${t('agents.preview.cited')}</span><small>${preview.detected.length}</small></div>
            <div class="cm-cited-list">${preview.detected.slice(0, 4).map(p => {
              const sp = splitPath(p);
              return html`<button type="button" key=${p} class="cm-cited-row" title=${p} onClick=${() => openPreview(s.id, p, { openPanel: false })}>
                <${Icon} n="file" />
                <span class="trunc"><b>${sp.name}</b>${sp.dir && html`<small class="mono">${sp.dir}</small>`}</span>
                <${Icon} n="eye" class="cm-cited-eye" />
              </button>`;
            })}</div>
          </section>`}
          <${HarnessQuota} mode=${s.mode} />
          <div class="cm-panel-secs">
            <${Section} id="activity" label=${lt('insp', 'activity')} hint=${ago(s.lastActivityAt)} open=${open.includes('activity')} toggle=${toggle}><${ActivityTab} id=${s.id} /></${Section}>
            <${Section} id="others" label=${t('agents.panel.others', { cli: c.label })} hint=${String(sessionList().filter(x => x.mode === s.mode && x.id !== s.id).length || '')} open=${open.includes('others')} toggle=${toggle}><${OthersTab} s=${s} /></${Section}>
            <${Section} id="info" label=${t('agents.panel.details')} hint=${s.tokens?.total > 0 ? fmtTok(s.tokens.total) + ' ' + t('agents.panel.tokens') : ''} open=${open.includes('info')} toggle=${toggle}><${InfoTab} session=${s} /></${Section}>
          </div>`}
        ${activeTab === 'changes' && html`<${ChangesTab} id=${s.id} workingDir=${s.workingDir} editsState=${editsState} refreshEdits=${refreshEdits} gitState=${gitState} refreshGit=${refreshGit} />`}
        ${activeTab === 'files' && html`<${FilesTab} id=${s.id} />`}
        ${activeTab === 'subagents' && html`<${SubagentsTab} session=${s} subState=${subState} refreshSub=${refreshSub} />`}
        ${activeTab === 'preview' && html`<${PreviewTab} session=${s} preview=${preview} />`}
      </div>`}
    </div>
  </aside>`;

  if (narrowView) return inner;
  return html`<div class=${cls('cm-insp-shell', isOpen && 'open')} style=${isOpen ? `width:${wPx}px` : undefined} aria-hidden=${String(!isOpen)}>
    ${isOpen && html`<div class="cm-insp-resize" role="separator" aria-orientation="vertical" aria-label=${t('agents.insp.resize')} title=${t('agents.insp.resize')} tabIndex="0"
      onPointerDown=${onResizeDown} onPointerMove=${onResizeMove} onPointerUp=${onResizeUp} onPointerCancel=${onResizeUp}
      onDblClick=${() => savePanelWidth(DEFAULT_PANEL_WIDTH)} onKeyDown=${onResizeKey}><i></i></div>`}
    ${inner}
  </div>`;
}
