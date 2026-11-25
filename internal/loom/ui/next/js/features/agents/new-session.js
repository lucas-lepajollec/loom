// Nouvelle session : un CLI du registre Codeman, un dossier vu par le serveur
// Codeman (pas forcément la machine de Loom), un nom et, selon le CLI, un
// modèle. Les modes de permission restent ceux réglés dans Codeman.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, useRef, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { DropdownButton, Menu } from '../../ui/controls.js';
import { useDictation } from '../voice/dictation.js';
import { upload } from '../chat/composer.js';
import { uploadFiles, quotePath } from './attach.js';
import { Logo } from '../../ui/logo.js';
import { Modal, toast } from '../../ui/dialog.js';
import { ListPick } from '../../ui/listpick.js';
import { app } from '../../core/state.js';
import { api, flattenPrompt, sendPrompt } from './client.js';
import { agents, cliLogo, projectOf } from './store.js';
import { agentDraftKey, createSession, supportsModel } from './actions.js';
import { fileAgent } from '../../app/sessions.js';
import { defaultModel, startPhoneDiscussion } from '../../app/phone-discussion.js';

export const CliMark = ({ mode, size }) => {
  if (mode === 'shell') return html`<span class=${cls('logo-mark', size)} aria-hidden="true"><${Icon} n="terminal" /></span>`;
  const c = agents.get().clis.find(x => x.id === mode);
  const logo = cliLogo(mode);
  return logo ? html`<${Logo} id=${logo} size=${size} />` : html`<span class=${cls('logo-mark mono dim', size)} aria-hidden="true">${(c && c.shortBadge) || String(mode || '?').slice(0, 2).toUpperCase()}</span>`;
};

function Browser({ start, onPick }) {
  const [st, setSt] = useState({ path: start || '', entries: [], parent: null, loading: true, error: '' });
  const go = async path => {
    setSt(s => ({ ...s, loading: true }));
    try { const r = await api.browse(path); setSt({ path: r.path, parent: r.parent, entries: (r.entries || []).filter(e => e.type === 'directory'), loading: false, error: '' }); }
    catch (e) { setSt(s => ({ ...s, loading: false, error: e.message })); }
  };
  useEffect(() => { go(start); }, []);
  return html`<div class="cm-browse">
    <div class="cm-browse-h">
      <button class="icon-btn" disabled=${!st.parent} aria-label=${t('agents.new.up')} onClick=${() => go(st.parent)}><${Icon} n="left" /></button>
      <span class="mono trunc">${st.path}</span>
      <button class="btn sm" onClick=${() => onPick(st.path)}>${t('agents.new.use_folder')}</button></div>
    ${st.error ? html`<p class="muted">${st.error}</p>` : html`<div class="cm-browse-list">${st.loading ? html`<div class="skeleton" style="height:120px"></div>`
      : st.entries.length ? st.entries.map(e => html`<button type="button" class="item" key=${e.path} onClick=${() => go(e.path)}><${Icon} n="folder" /><span class="trunc">${e.name}</span></button>`)
      : html`<p class="muted">${t('agents.new.no_subfolders')}</p>`}</div>`}
  </div>`;
}

export function NewSession({ onClose, onCreated, preset, phone, inline, initialType = 'agent', folder: initialFolder = '', message = '' }) {
  const clis = useStore(agents, s => s.clis.filter(c => c.enabled !== false));
  const unified = useStore(agents, s => s.unified);
  const cases = useStore(agents, s => s.cases);
  const defaults = useStore(agents, s => s.settings || {});
  const ready = useStore(agents, s => s.conn.state === 'ready');
  const projects = useStore(app, s => s.nav.projects || []);
  const [type, setType] = useState(initialType);
  const [folder, setFolder] = useState(initialFolder);
  const [firstMessage, setFirstMessage] = useState(message || '');
  const [choiceId, setChoiceId] = useState('');
  const [attached, setAttached] = useState([]);
  const [configure, setConfigure] = useState(false);
  const [branch, setBranch] = useState('');
  const [branchMenu, setBranchMenu] = useState(null);
  const [worktrees, setWorktrees] = useState([]);
  const [readingWorktrees, setReadingWorktrees] = useState(false);
  const fileInput = useRef();
  const dictation = useDictation(text => setFirstMessage(value => [value.trimEnd(), text].filter(Boolean).join(' ')));
  const models = useStore(app, s => s.workspace?.models || []).filter(m => m.enabled && (m.kind === 'local' || m.ready));
  const modelName = useStore(app, defaultModel);
  const localMachine = useStore(app, s => s.engineNode?.hostname || s.serverInfo?.hostname);
  const codemanHost = useStore(agents, s => s.conn.ui_url || s.conn.url);
  let agentMachine = '';
  try { agentMachine = new URL(codemanHost).hostname; } catch (_) {}
  const usable = id => clis.some(c => c.id === id && c.installed !== false);
  const preferred = preset?.mode || (usable(defaults.runMode) ? defaults.runMode : usable('claude') ? 'claude' : (clis.find(c => c.installed && c.kind === 'agent') || {}).id || 'shell');
  const [mode, setMode] = useState(preferred);
  const [dir, setDir] = useState(preset?.workingDir || defaults.defaultWorkingDir || '');
  const [name, setName] = useState('');
  const [model, setModel] = useState('');
  const [browse, setBrowse] = useState(false);
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const recent = [...new Set(unified.map(r => r.workingDir).filter(Boolean))].slice(0, 6);
  const folders = [...new Set([...recent, ...cases.filter(c => c.location !== 'remote').map(c => c.path)])].slice(0, 10);
  useEffect(() => { if (!dir && folders.length) setDir(folders[0]); }, [folders.length]);
  useEffect(() => { if ((phone || inline) && !preset?.mode && !clis.some(c => c.id === mode)) setMode(preferred); }, [phone, inline, clis, preferred]);
  const cli = clis.find(c => c.id === mode);
  const unifiedForm = phone || inline;
  const discussion = unifiedForm && type === 'chat';
  const canStart = discussion || (!!dir.trim() && cli?.installed !== false && (!unifiedForm || ready && !!cli));
  const knownSession = useStore(agents, s => s.order.find(id => s.sessions[id]?.workingDir === dir));
  useEffect(() => {
    if (!branchMenu) return;
    let active = true;
    const candidates = new Map();
    for (const s of Object.values(agents.get().sessions)) {
      if (s.workingDir && s.workingDir !== dir && !candidates.has(s.workingDir)) candidates.set(s.workingDir, s.id);
    }
    setReadingWorktrees(true);
    setWorktrees([]);
    Promise.allSettled([...candidates].slice(0, 5).map(async ([path, id]) => {
      const result = await api.gitStatus(id);
      return { path, branch: result?.repos?.length === 1 ? result.repos[0].status?.branch || '' : '' };
    })).then(results => {
      if (!active) return;
      setWorktrees(results.flatMap(result => result.status === 'fulfilled' && result.value.branch ? [result.value] : []));
      setReadingWorktrees(false);
    });
    return () => { active = false; };
  }, [branchMenu, dir]);
  useEffect(() => {
    let current = true;
    setBranch('');
    if (inline && !discussion && knownSession) api.gitStatus(knownSession).then(result => {
      if (current) setBranch(result?.repos?.length === 1 ? result.repos[0].status?.branch || '' : '');
    }).catch(() => {});
    return () => { current = false; };
  }, [inline, discussion, knownSession]);
  useEffect(() => {
    if (!inline) return;
    const key = e => { if (e.key === 'Escape' && !busy && !document.querySelector('.scrim,.pop')) onClose(); };
    addEventListener('keydown', key);
    return () => removeEventListener('keydown', key);
  }, [inline, busy]);
  const close = () => { if (!busy) onClose(); };
  const submit = async () => {
    if (submitting.current || !canStart) return;
    submitting.current = true;
    setBusy(true);
    try {
      if (discussion) {
        const files = [];
        for (const file of attached) files.push(await upload(file));
        const choice = models.find(m => m.id === choiceId);
        onClose(); startPhoneDiscussion(folder, firstMessage.trim(), !!(firstMessage.trim() || files.length), { files, choice });
        return;
      }
      const id = await createSession({ mode, workingDir: dir.trim(), name, model });
      if (unifiedForm) {
        if (folder) await fileAgent(id, folder).catch(err => toast(err.message, 'err'));
        let message = firstMessage.trim();
        if (attached.length) {
          try { message = [...(await uploadFiles(id, attached)).map(quotePath), message].filter(Boolean).join(' '); }
          catch (err) { toast(err.message, 'err'); saveFirstMessage(id, message); onCreated?.(id); onClose(); return; }
        }
        if (flattenPrompt(message)) await sendFirstMessage(id, mode, message);
      }
      onCreated?.(id); onClose();
    }
    catch (e) {
      toast(e.message, 'err');
      if (unifiedForm && e.sessionId) {
        if (firstMessage.trim()) saveFirstMessage(e.sessionId, firstMessage.trim());
        onCreated?.(e.sessionId); onClose();
      }
    }
    finally { submitting.current = false; setBusy(false); }
  };
  const details = html`<div class="cm-row2">
    <label class="field"><span>${t('agents.new.name')}</span><input class="input" value=${name} placeholder=${t('agents.new.name_ph')} onInput=${e => setName(e.target.value)} /></label>
    ${supportsModel(mode) && html`<label class="field"><span>${t('agents.new.model')}</span><input class="input mono" value=${model} placeholder=${t('agents.new.model_ph')} onInput=${e => setModel(e.target.value)} /></label>`}
  </div>`;
  if (inline) {
    const dirOptions = [...new Set([dir, ...folders].filter(Boolean))].map(path => ({ value: path, label: projectOf(path) }));
    const pick = value => {
      if (value.startsWith('model:')) { setType('chat'); setChoiceId(value.slice(6)); }
      else if (value.startsWith('cli:')) { setType('agent'); setMode(value.slice(4)); setModel(''); }
      else { setType('chat'); setChoiceId(''); }
    };
    const executor = discussion ? (choiceId ? 'model:' + choiceId : 'chat') : 'cli:' + mode;
    const cliModel = id => agents.get().order.map(key => agents.get().sessions[key]).find(s => s?.mode === id && s.displayModel?.model)?.displayModel.model || '';
    const options = [
      ...clis.filter(c => c.kind !== 'shell' && c.installed !== false).map(c => ({ value: 'cli:' + c.id, label: c.label, detail: cliModel(c.id), icon: 'agent' })),
      { value: 'chat', label: modelName ? t('side.type_chat') + ' · ' + modelName : t('side.type_chat'), icon: 'chat' },
      ...models.filter(m => m.name !== modelName).map(m => ({ value: 'model:' + m.id, label: m.name, icon: m.kind === 'local' ? 'chip' : 'globe' })),
      ...clis.filter(c => c.kind === 'shell' && c.installed !== false).map(c => ({ value: 'cli:' + c.id, label: c.label, icon: 'terminal' })),
    ];
    const modelOptions = projects.map(p => ({ value: p.id, label: p.name }));
    const desktopOptions = options.map(o => ({
      ...o,
      group: o.value.startsWith('cli:') ? t('noir.codeman_executors') : t('side.type_chat'),
      note: o.detail,
      display: o.detail ? o.label + ' · ' + o.detail : o.label,
    }));
    const execution = html`<${ListPick} class="fit" label=${t('noir.new_with')} value=${executor} options=${desktopOptions} onChange=${pick} />`;
    return html`<div class=${cls('view', 'new-session-page', phone && 'phone-new-session')} aria-labelledby="new-session-title">
      <header class="dialog-head"><button class="icon-btn" type="button" onClick=${close} aria-label=${t('ui.dialog.fermer')}><${Icon} n="close" /></button></header>
      <div class="new-session-in">
        <h1 id="new-session-title">${t('noir.new_title')}</h1>
        ${phone && html`<div class="new-executor-group"><span id="new-executor-label">${t('noir.new_with')}</span><div class="new-executors" role="group" aria-labelledby="new-executor-label">${options.map(o => html`<button class="btn" key=${o.value} disabled=${busy} aria-pressed=${String(o.value === executor)} onClick=${() => pick(o.value)}><${Icon} n=${o.icon} /><span class="trunc">${o.label}</span>${o.detail && html`<small class="trunc">${o.detail}</small>`}${o.value === executor && html`<${Icon} n="check" />`}</button>`)}</div></div>`}
        <form class="composer new-composer" onSubmit=${e => { e.preventDefault(); submit(); }}>
          <textarea rows=${phone ? 2 : 3} value=${firstMessage} aria-label=${t('mobile.first_message')} placeholder=${t('noir.new_prompt')} disabled=${busy} onInput=${e => setFirstMessage(e.target.value)}></textarea>
          <input type="file" multiple hidden ref=${fileInput} onChange=${e => { setAttached(list => [...list, ...e.target.files]); e.target.value = ''; }} />
          ${attached.length > 0 && html`<div class="attach-row">${attached.map((f, i) => html`<span class="file-pill"><${Icon} n="file" />${f.name}<button type="button" aria-label=${t('agents.attach.remove')} onClick=${() => setAttached(list => list.filter((_, n) => n !== i))}><${Icon} n="close" /></button></span>`)}</div>`}
          <div class="composer-bar">
            <button class="icon-btn" type="button" aria-label=${t('agents.composer.attach')} disabled=${busy} onClick=${() => fileInput.current.click()}><${Icon} n="paperclip" /></button>
            ${!phone && !discussion && html`<a class="chip-btn" href="#/settings/policy"><${Icon} n="lock" />${t('inspector.inspector.autorisations')}</a>`}
            <span class="grow"></span>
            ${!phone && execution}
            <button class="icon-btn" type="button" aria-label=${dictation.phase ? t('mobile.dictation_stop') : t('mobile.dictation')} onClick=${dictation.toggle} disabled=${busy || dictation.phase === 'finishing'}><${Icon} n=${dictation.phase ? 'stop' : 'mic'} /></button>
            <button class="send" type="submit" aria-label=${t('agents.new.start')} disabled=${busy || !canStart || !!dictation.phase}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="arrowUp" />`}</button>
          </div>
        </form>
        <div class="new-session-meta">
          ${discussion ? html`
            ${(!choiceId || models.find(m => m.id === choiceId)?.kind === 'local') && localMachine && html`<a class="chip-btn" href="#/engine" title=${t('workspaces.machine')}><${Icon} n="server" /><span class="trunc">${localMachine}</span></a>`}
            ${modelOptions.length > 0 && html`<${ListPick} class="fit" label=${t('mobile.session_folder')} value=${folder} onChange=${setFolder} options=${[{ value: '', label: t('mobile.no_folder') }, ...modelOptions]} />`}
          ` : html`
            ${agentMachine && html`<a class="chip-btn" href="#/agents/settings" title=${t('workspaces.machine')}><${Icon} n="server" /><span class="trunc">${agentMachine}</span></a>`}
            ${ready ? html`
              ${dirOptions.length > 0 && html`<${ListPick} class="fit" label=${t('agents.new.folder')} value=${dir} options=${dirOptions} onChange=${setDir} />`}
              ${branch ? html`<${DropdownButton} class="fit" aria-label=${t('noir.branch')} open=${branchMenu} onClick=${e => setBranchMenu(e.currentTarget)}><${Icon} n="git" /><span class="trunc">${branch}</span></${DropdownButton}>
                ${branchMenu && html`<${Menu} label=${t('noir.branch')} anchor=${branchMenu} onClose=${() => setBranchMenu(null)} items=${[
                  { label: branch, icon: 'check', run: () => {} },
                  ...worktrees.map(tree => ({ label: tree.branch + ' · ' + projectOf(tree.path), icon: 'git', run: () => setDir(tree.path) })),
                  ...(readingWorktrees ? [{ label: t('local.page.chargement'), disabled: true }] : []),
                  '-',
                  { label: t('mobile.session_options'), icon: 'sliders', run: () => setConfigure(!configure) },
                ]} />`}`
                : html`<button type="button" class="chip-btn" aria-expanded=${String(configure)} onClick=${() => setConfigure(!configure)}><${Icon} n="sliders" />${t('mobile.session_options')}</button>`}
            ` : html`<a class="chip-btn" href="#/agents/settings">${t('mobile.connect_agents')}</a>`}
          `}
        </div>
        ${configure && !discussion && ready && html`<div class="new-session-options">
          <label class="field"><span>${t('agents.new.folder')}</span><input class="input mono" value=${dir} onInput=${e => setDir(e.target.value)} /></label>
          <button type="button" class="btn" aria-expanded=${String(browse)} onClick=${() => setBrowse(!browse)}>${t('agents.new.browse')}</button>
          ${browse && html`<${Browser} start=${dir} onPick=${p => { setDir(p); setBrowse(false); }} />`}
          ${modelOptions.length > 0 && html`<${ListPick} class="fit" label=${t('mobile.session_folder')} value=${folder} onChange=${setFolder} options=${[{ value: '', label: t('mobile.no_folder') }, ...modelOptions]} />`}
          ${details}
        </div>`}
      </div>
    </div>`;
  }
  return html`<${Modal} wide title=${phone ? t('side.new_session') : t('agents.new.title')} sub=${!phone && t('agents.new.sub')} onClose=${phone ? close : onClose}
      class=${phone ? 'phone-new-session' : undefined} focusField=${!phone}
      foot=${html`${!phone && html`<button type="button" class="btn" onClick=${onClose}>${t('agents.cancel')}</button>`}
        <button type="button" class="btn primary" disabled=${busy || !canStart} onClick=${submit}>${busy ? t('agents.new.starting') : t('agents.new.start')}</button>`}>
    ${phone && html`<div class="phone-session-types" role="group" aria-label=${t('side.session_type')}>
      <button type="button" class="btn" aria-pressed=${String(discussion)} disabled=${busy} onClick=${() => setType('chat')}><${Icon} n="chat" /><b>${t('side.type_chat')}</b><small>${t('mobile.new_chat_desc')}</small></button>
      <button type="button" class="btn" aria-pressed=${String(!discussion)} disabled=${busy} onClick=${() => setType('agent')}><${Icon} n="agent" /><b>${t('side.type_agent')}</b><small>${t('mobile.new_agent_desc')}</small></button>
    </div>`}
    ${phone && !discussion && !ready && html`<a class="btn" href="#/agents/settings" onClick=${onClose}>${t('mobile.connect_agents')}</a>`}
    ${!discussion && (!phone || ready) && html`
    <div class="field"><span>${t('agents.new.cli')}</span>
      <div class="cm-cli-grid" role="group" aria-label=${t('agents.new.cli')}>${clis.map(c => html`<button type="button" key=${c.id} class=${cls('btn cm-cli', c.installed === false && 'missing')} onClick=${() => { setMode(c.id); setModel(''); }} aria-pressed=${String(mode === c.id)}>
        <${CliMark} mode=${c.id} size="sm" /><span class="cm-cli-copy">${c.label}${c.installed === false && html`<small>${t('agents.new.not_installed')}</small>`}</span>
        ${mode === c.id && html`<${Icon} n="check" class="cm-cli-check" />`}</button>`)}</div>
      ${cli && cli.installed === false && html`<small>${t('agents.new.install_hint')} <code>${cli.installCommand || ''}</code></small>`}</div>
    <div class="field"><span><label for="cm-new-session-dir">${t('agents.new.folder')}</label></span>
      <div class="cm-dir"><${Icon} n="folder" /><input id="cm-new-session-dir" class="input mono" value=${dir} onInput=${e => setDir(e.target.value)} />
        <button type="button" class="btn" aria-expanded=${String(browse)} onClick=${() => setBrowse(!browse)}>${browse ? t('agents.new.hide_browser') : t('agents.new.browse')}</button></div>
      ${folders.length > 0 && html`<div class="chips">${folders.map(f => html`<button type="button" key=${f} class="btn sm" aria-pressed=${String(dir === f)} onClick=${() => setDir(f)} title=${f}>${f.split('/').pop() || f}</button>`)}</div>`}
      ${browse && html`<${Browser} start=${dir} onPick=${p => { setDir(p); setBrowse(false); }} />`}</div>
    ${phone ? html`<details><summary>${t('mobile.session_options')}</summary>${details}</details>` : html`${details}<p class="note">${t('agents.new.permissions')}</p>`}`}
    ${phone && (discussion || ready) && html`
      ${projects.length > 0 && html`<div class="field"><span>${t('mobile.session_folder')}</span>
        <${ListPick} label=${t('mobile.session_folder')} value=${folder} onChange=${setFolder}
          options=${[{ value: '', label: t('mobile.no_folder') }, ...projects.map(p => ({ value: p.id, label: p.name }))]} />
      </div>`}
      <label class="field"><span>${t('mobile.first_message')}</span><textarea class="textarea" rows="2" disabled=${busy}
        value=${firstMessage} onInput=${e => setFirstMessage(e.target.value)}></textarea></label>`}
  </${Modal}>`;
}

async function sendFirstMessage(id, mode, text) {
  // Other CLIs can stop on startup dialogs that Codeman only exposes on screen.
  let attempted = false;
  try {
    if (mode === 'claude' || mode === 'shell') {
      const [session, pending] = await Promise.all([api.session(id), api.approvals()]);
      if (session?.pid != null && !session.isWorking && Array.isArray(pending?.approvals) &&
          !pending.approvals.some(a => a.sessionId === id && a.kind !== 'idle')) {
        attempted = true;
        await sendPrompt(id, text);
        return;
      }
    }
  } catch (err) {
    if (err.code === 'NOT_DELIVERED') attempted = false;
  }
  saveFirstMessage(id, text, attempted);
}

function saveFirstMessage(id, text, attempted = false) {
  try { sessionStorage.setItem(agentDraftKey(id), text); } catch (_) {}
  toast(attempted ? t('mobile.first_message_uncertain') : t('mobile.first_message_draft'));
}
