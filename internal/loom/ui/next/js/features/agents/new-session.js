// Nouvelle session : un CLI du registre Codeman, un dossier vu par le serveur
// Codeman (pas forcément la machine de Loom), un nom et, selon le CLI, un
// modèle. Les modes de permission restent ceux réglés dans Codeman.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useMemo, useStore, useRef, cls } from '../../core/lib.js';
import { Icon, LoomMark } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { AmbientField } from '../../ui/ambient.js';
import { Popover, Menu } from '../../ui/controls.js';
import { Modal, toast } from '../../ui/dialog.js';
import { ListPick } from '../../ui/listpick.js';
import { app } from '../../core/state.js';
import { api, flattenPrompt, sendPrompt } from './client.js';
import { agents, cliLogo, ensureHistory, projectOf, queueCommand } from './store.js';
import { agentDraftKey, createSession, supportsModel } from './actions.js';
import { filesOf, hasFiles, quotePath, uploadFiles } from './attach.js';
import { openSession } from './nav.js';
import { fileAgent } from '../../app/sessions.js';
import { startPhoneDiscussion } from '../../app/phone-discussion.js';

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

const EMPTY_LIST = [];
const EMPTY_OBJ = {};
export function NewSession({ onClose, onCreated, preset, phone, initialType = 'agent', folder: initialFolder = '' }) {
  const allClis = useStore(agents, s => s.clis);
  const clis = useMemo(() => allClis.filter(c => c.enabled !== false), [allClis]);
  const unified = useStore(agents, s => s.unified);
  const cases = useStore(agents, s => s.cases);
  const defaults = useStore(agents, s => s.settings || EMPTY_OBJ);
  const ready = useStore(agents, s => s.conn.state === 'ready');
  const projects = useStore(app, s => s.nav.projects || EMPTY_LIST);
  useEffect(() => { if (ready) ensureHistory(); }, [ready]);
  const [type, setType] = useState(initialType);
  const [folder, setFolder] = useState(initialFolder);
  const [firstMessage, setFirstMessage] = useState('');
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
  useEffect(() => { if (phone && !preset?.mode && !clis.some(c => c.id === mode)) setMode(preferred); }, [phone, clis, preferred]);
  const cli = clis.find(c => c.id === mode);
  const discussion = phone && type === 'chat';
  const canStart = discussion || (!!dir.trim() && cli?.installed !== false && (!phone || ready && !!cli));
  const close = () => { if (!busy) onClose(); };
  const submit = async () => {
    if (submitting.current || !canStart) return;
    submitting.current = true;
    if (discussion) { onClose(); startPhoneDiscussion(folder, firstMessage.trim()); return; }
    setBusy(true);
    try {
      const id = await createSession({ mode, workingDir: dir.trim(), name, model });
      if (phone) {
        if (folder) await fileAgent(id, folder).catch(err => toast(err.message, 'err'));
        if (flattenPrompt(firstMessage.trim())) await sendFirstMessage(id, mode, firstMessage.trim());
      }
      onCreated?.(id); onClose();
    }
    catch (e) {
      toast(e.message, 'err');
      if (phone && e.sessionId) {
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

// Page plein cadre « Nouvelle session d'agent » (mode Agents) : composeur centré
// sur fond pixel-matrix, choix du dossier de travail, de l'agent, du modèle et
// du mode de réflexion juste sous la bulle de saisie, lancement avec ou sans prompt.
const MODEL_PRESETS = {
  claude: [
    { value: 'opus', label: 'Opus' },
    { value: 'sonnet', label: 'Sonnet' },
    { value: 'haiku', label: 'Haiku' },
  ],
  codex: [
    { value: 'gpt-5.4', label: 'gpt-5.4' },
    { value: 'gpt-5.3-codex', label: 'gpt-5.3-codex' },
    { value: 'o3', label: 'o3' },
  ],
};
const EFFORT_MODES = new Set(['claude', 'codex']);
const EFFORT_LEVELS = {
  claude: ['low', 'medium', 'high', 'xhigh', 'max'],
  codex: ['low', 'medium', 'high', 'xhigh'],
};
const effortLabel = e => ({ low: t('agents.admin.low'), medium: t('agents.admin.medium'), high: t('agents.admin.high'), xhigh: t('agents.admin.xhigh'), max: t('agents.admin.max') })[e] || e;
const CLAUDE_PERM_OPTIONS = () => [
  { value: 'dangerously-skip-permissions', label: t('agents.cfg.mode.skip') },
  { value: 'auto', label: t('agents.cfg.mode.auto') },
  { value: 'normal', label: t('agents.cfg.mode.normal') },
  { value: 'allowedTools', label: t('agents.cfg.mode.allowed') },
];
const CODEX_PERM_OPTIONS = () => [
  { value: 'normal', label: t('agents.studio.perm_normal') },
  { value: 'bypass', label: t('agents.cfg.codex_bypass') },
];

export function AgentNewStudio({ onCreated }) {
  const allClis = useStore(agents, s => s.clis);
  const clis = useMemo(() => allClis.filter(c => c.enabled !== false), [allClis]);
  const unified = useStore(agents, s => s.unified);
  const cases = useStore(agents, s => s.cases);
  const defaults = useStore(agents, s => s.settings || EMPTY_OBJ);
  const conn = useStore(agents, s => s.conn);
  const ready = conn.state === 'ready';
  const readOnly = !!(conn.multi_user && conn.role === 'user');
  useEffect(() => { if (ready) ensureHistory(); }, [ready]);

  const usable = id => clis.some(c => c.id === id && c.installed !== false);
  const preferred = usable(defaults.runMode) ? defaults.runMode : usable('claude') ? 'claude' : (clis.find(c => c.installed && c.kind === 'agent') || {}).id || 'shell';
  const [mode, setMode] = useState(preferred);
  const [dir, setDir] = useState(defaults.defaultWorkingDir || '');
  const [model, setModel] = useState('');
  const [effort, setEffort] = useState('');
  const [claudePerm, setClaudePerm] = useState(defaults.claudeMode || 'dangerously-skip-permissions');
  const [codexPerm, setCodexPerm] = useState(defaults.codexDangerouslyBypassApprovals ? 'bypass' : 'normal');
  const [promptText, setPromptText] = useState('');
  const [files, setFiles] = useState([]);
  const [over, setOver] = useState(false);
  const [busy, setBusy] = useState(false);
  const [cliPop, setCliPop] = useState(null);
  const [dirPop, setDirPop] = useState(null);
  const [modelPop, setModelPop] = useState(null);
  const [effortPop, setEffortPop] = useState(null);
  const [permPop, setPermPop] = useState(null);
  const [showBrowse, setShowBrowse] = useState(false);
  const [customModel, setCustomModel] = useState('');
  const taRef = useRef(null);
  const fileRef = useRef(null);
  const submitting = useRef(false);

  const recent = useMemo(() => [...new Set(unified.map(r => r.workingDir).filter(Boolean))].slice(0, 6), [unified]);
  const folders = useMemo(() => [...new Set([...recent, ...cases.filter(c => c.location !== 'remote').map(c => c.path)])].slice(0, 10), [recent, cases]);
  useEffect(() => { if (!dir && folders.length) setDir(folders[0]); }, [folders.length]);
  useEffect(() => { if (clis.length && !clis.some(c => c.id === mode)) setMode(preferred); }, [clis.length, preferred]);
  useEffect(() => { if (defaults.claudeMode) setClaudePerm(defaults.claudeMode); }, [defaults.claudeMode]);
  useEffect(() => { if (typeof defaults.codexDangerouslyBypassApprovals === 'boolean') setCodexPerm(defaults.codexDangerouslyBypassApprovals ? 'bypass' : 'normal'); }, [defaults.codexDangerouslyBypassApprovals]);
  useEffect(() => () => files.forEach(f => f.preview && URL.revokeObjectURL(f.preview)), []);
  useEffect(() => {
    const el = taRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 220) + 'px';
  }, [promptText]);

  const addFiles = list => {
    if (!list.length) return;
    const next = list.map(f => ({
      key: Math.random().toString(36).slice(2),
      file: f,
      name: f.name,
      preview: /^image\//.test(f.type) ? URL.createObjectURL(f) : '',
    }));
    setFiles(cur => [...cur, ...next]);
    taRef.current?.focus();
  };
  const dropFile = key => setFiles(cur => {
    const found = cur.find(x => x.key === key);
    if (found?.preview) URL.revokeObjectURL(found.preview);
    return cur.filter(x => x.key !== key);
  });

  const applyPerm = async patch => {
    if (!ready || readOnly) return;
    const prev = agents.get().settings || {};
    agents.set({ settings: { ...prev, ...patch } });
    try {
      await api.saveSettings(patch);
    } catch (e) {
      agents.set({ settings: prev });
      toast(e.message, 'err');
    }
  };
  const pickClaudePerm = value => {
    setClaudePerm(value);
    applyPerm({ claudeMode: value });
  };
  const pickCodexPerm = value => {
    setCodexPerm(value);
    applyPerm({ codexDangerouslyBypassApprovals: value === 'bypass' });
  };

  const cli = clis.find(c => c.id === mode);
  const hasModel = supportsModel(mode);
  const hasEffort = EFFORT_MODES.has(mode);
  const hasPerm = !readOnly && (mode === 'claude' || mode === 'codex');
  const canStart = ready && !!dir.trim() && cli?.installed !== false;
  const hasContent = !!promptText.trim() || files.length > 0;

  const launch = async () => {
    if (submitting.current || !canStart) return;
    submitting.current = true;
    setBusy(true);
    const initial = promptText.trim();
    const queued = [...files];
    try {
      if (!readOnly && mode === 'claude' && claudePerm && defaults.claudeMode !== claudePerm) {
        await api.saveSettings({ claudeMode: claudePerm }).catch(() => {});
      } else if (!readOnly && mode === 'codex' && (codexPerm === 'bypass') !== !!defaults.codexDangerouslyBypassApprovals) {
        await api.saveSettings({ codexDangerouslyBypassApprovals: codexPerm === 'bypass' }).catch(() => {});
      }
      const id = await createSession({ mode, workingDir: dir.trim(), model, effort });
      if (mode === 'claude' && effort) {
        try { localStorage.setItem('loom.codeman.effort.' + id, effort); } catch (_) {}
        queueCommand(id, '/effort ' + effort).catch(() => {});
      }
      let uploaded = [];
      if (queued.length) {
        try {
          uploaded = await uploadFiles(id, queued.map(f => f.file));
        } catch (err) {
          toast(err.message, 'err');
        }
      }
      const combined = [uploaded.map(quotePath).join(' '), initial].filter(x => flattenPrompt(x)).join(' ');
      if (combined) {
        await sendFirstMessage(id, mode, combined);
      }
      setPromptText('');
      queued.forEach(f => f.preview && URL.revokeObjectURL(f.preview));
      setFiles([]);
      if (onCreated) onCreated(id);
      else openSession(id);
    } catch (e) {
      toast(e.message, 'err');
      if (e.sessionId) {
        if (initial) saveFirstMessage(e.sessionId, initial);
        if (onCreated) onCreated(e.sessionId);
        else openSession(e.sessionId);
      }
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };

  const onKey = e => {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      launch();
    }
  };
  const onPaste = e => {
    const list = filesOf(e.clipboardData);
    if (list.length) {
      e.preventDefault();
      addFiles(list);
    }
  };
  const dnd = {
    onDragOver: e => { if (hasFiles(e)) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setOver(true); } },
    onDragLeave: e => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(false); },
    onDrop: e => { setOver(false); if (!hasFiles(e)) return; e.preventDefault(); addFiles(filesOf(e.dataTransfer)); },
  };

  const presets = MODEL_PRESETS[mode] || [];
  const effortItems = [
    { label: t('agents.cfg.cli_default'), icon: !effort ? 'check' : '', run: () => setEffort('') },
    ...(EFFORT_LEVELS[mode] || []).map(lv => ({ label: effortLabel(lv), icon: effort === lv ? 'check' : '', run: () => setEffort(lv) })),
  ];
  const permOptions = mode === 'claude' ? CLAUDE_PERM_OPTIONS() : CODEX_PERM_OPTIONS();
  const permValue = mode === 'claude' ? claudePerm : codexPerm;
  const permItems = permOptions.map(o => ({
    label: o.label,
    icon: permValue === o.value ? 'check' : '',
    run: () => (mode === 'claude' ? pickClaudePerm(o.value) : pickCodexPerm(o.value)),
  }));
  const permLabel = permOptions.find(o => o.value === permValue)?.label || t('agents.cfg.claude_mode');
  const dirLabel = dir ? projectOf(dir) : t('agents.studio.folder_pick');
  const modelLabel = model || t('agents.studio.default_model');

  return html`<div class="cm-main cm-studio">
    <${AmbientField} variant="calm" />
    <div class="cm-studio-in anim-rise">
      <div class="cm-studio-head">
        <div class="welcome-mark"><${LoomMark} /></div>
        <h1>${t('agents.studio.title')}</h1>
        <p>${t('agents.studio.sub')}</p>
      </div>
      <div class=${cls('cm-studio-composer composer', over && 'drop-on')} ...${dnd}>
        ${files.length > 0 && html`<div class="cm-attach">${files.map(f => html`<span key=${f.key} class="cm-att" title=${f.name}>
          ${f.preview ? html`<img src=${f.preview} alt="" />` : html`<${Icon} n="file" />`}
          <span class="trunc">${f.name}</span>
          <button type="button" class="cm-att-x" aria-label=${t('agents.attach.remove')} onClick=${() => dropFile(f.key)}><${Icon} n="close" /></button>
        </span>`)}</div>`}
        <input type="file" multiple hidden ref=${fileRef} onChange=${e => { const list = [...(e.target.files || [])]; e.target.value = ''; addFiles(list); }} />
        <textarea ref=${taRef} rows="1" value=${promptText} disabled=${busy}
          onInput=${e => setPromptText(e.target.value)} onKeyDown=${onKey} onPaste=${onPaste}
          placeholder=${t('agents.studio.placeholder', { cli: cli?.label || t('agents.new.cli') })}
          aria-label=${t('agents.studio.title')}></textarea>
        <div class="composer-bar">
          <button type="button" class="icon-btn" aria-label=${t('agents.composer.attach')} title=${t('agents.composer.attach_hint')} disabled=${busy} onClick=${() => fileRef.current?.click()}><${Icon} n="paperclip" /></button>
          ${over
            ? html`<span class="cm-studio-hint">${t('agents.attach.drop')}</span>`
            : cli && cli.installed === false && html`<span class="cm-studio-warn">${t('agents.new.install_hint')} <code>${cli.installCommand || ''}</code></span>`}
          <span class="grow"></span>
          ${!ready
            ? html`<a class="btn primary sm cm-studio-go" href="#/agents/settings"><${Icon} n="plug" /><span>${t('agents.conn.gate_title')}</span></a>`
            : html`<button type="button" class="send" disabled=${busy || !canStart} aria-label=${hasContent ? t('agents.composer.send') : t('agents.studio.launch')} title=${hasContent ? t('agents.composer.send') : t('agents.studio.launch')} onClick=${launch}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="arrowUp" />`}</button>`}
        </div>
      </div>
      <div class="cm-studio-dock">
        <button type="button" class=${cls('cm-dock-pill', dirPop && 'on', !dir && 'empty')} aria-haspopup="dialog" aria-expanded=${String(!!dirPop)} title=${dir || t('agents.new.folder')} onClick=${e => { setShowBrowse(false); setDirPop(dirPop ? null : e.currentTarget); }}>
          <${Icon} n="folder" /><span class="mono trunc">${dirLabel}</span><${Icon} n="chevron" class="cm-dock-caret" />
        </button>
        <button type="button" class=${cls('cm-dock-pill', cliPop && 'on')} aria-haspopup="dialog" aria-expanded=${String(!!cliPop)} onClick=${e => setCliPop(cliPop ? null : e.currentTarget)}>
          <${CliMark} mode=${mode} size="xs" /><span class="trunc">${cli?.label || mode}</span><${Icon} n="chevron" class="cm-dock-caret" />
        </button>
        ${hasModel && html`<button type="button" class=${cls('cm-dock-pill', modelPop && 'on')} aria-haspopup="dialog" aria-expanded=${String(!!modelPop)} onClick=${e => { setCustomModel(model); setModelPop(modelPop ? null : e.currentTarget); }}>
          <${Icon} n="chip" /><span class="trunc">${modelLabel}</span><${Icon} n="chevron" class="cm-dock-caret" />
        </button>`}
        ${hasEffort && html`<button type="button" class=${cls('cm-dock-pill', effortPop && 'on')} aria-haspopup="menu" aria-expanded=${String(!!effortPop)} onClick=${e => setEffortPop(effortPop ? null : e.currentTarget)}>
          <${Icon} n="sliders" /><span class="trunc">${effort ? effortLabel(effort) : t('agents.studio.effort')}</span><${Icon} n="chevron" class="cm-dock-caret" />
        </button>`}
        ${hasPerm && html`<button type="button" class=${cls('cm-dock-pill', permPop && 'on')} aria-haspopup="menu" aria-expanded=${String(!!permPop)} title=${t('agents.cfg.claude_mode')} onClick=${e => setPermPop(permPop ? null : e.currentTarget)}>
          <${Icon} n="lock" /><span class="trunc">${permLabel}</span><${Icon} n="chevron" class="cm-dock-caret" />
        </button>`}
      </div>
    </div>
    ${dirPop && html`<${Popover} sheet anchor=${dirPop} place="above" onClose=${() => setDirPop(null)} label=${t('agents.new.folder')} width=${420} class="cm-dock-pop">
      <div class="cm-dock-pop-in">
        <div class="cm-dir">
          <${Icon} n="folder" />
          <input class="input sm mono" value=${dir} placeholder=${t('agents.studio.path_ph')} onInput=${e => setDir(e.target.value)} onKeyDown=${e => { if (e.key === 'Enter') setDirPop(null); }} />
          <button type="button" class="btn sm" aria-expanded=${String(showBrowse)} onClick=${() => setShowBrowse(!showBrowse)}>${showBrowse ? t('agents.new.hide_browser') : t('agents.new.browse')}</button>
        </div>
        ${showBrowse ? html`<${Browser} start=${dir} onPick=${p => { setDir(p); setShowBrowse(false); setDirPop(null); }} />`
          : folders.length > 0 && html`<div class="cm-dock-list">${folders.map(f => html`<button type="button" key=${f} class=${cls('item', dir === f && 'active')} onClick=${() => { setDir(f); setDirPop(null); }}>
              <${Icon} n="folder" /><span class="item-label"><span class="item-title">${projectOf(f)}</span><small class="mono trunc">${f}</small></span>
              ${dir === f && html`<${Icon} n="check" class="item-check" />`}
            </button>`)}</div>`}
      </div>
    </${Popover}>`}
    ${cliPop && html`<${Popover} sheet anchor=${cliPop} place="above" onClose=${() => setCliPop(null)} label=${t('agents.new.cli')} width=${300} class="cm-dock-pop">
      <div class="cm-dock-list">${clis.map(c => html`<button type="button" key=${c.id} class=${cls('item', mode === c.id && 'active')} onClick=${() => { setMode(c.id); setModel(''); setEffort(''); setCliPop(null); }}>
        <${CliMark} mode=${c.id} size="xs" />
        <span class="item-label"><span class="item-title">${c.label}</span>${c.installed === false && html`<small>${t('agents.new.not_installed')}</small>`}</span>
        ${mode === c.id && html`<${Icon} n="check" class="item-check" />`}
      </button>`)}</div>
    </${Popover}>`}
    ${modelPop && html`<${Popover} sheet anchor=${modelPop} place="above" onClose=${() => setModelPop(null)} label=${t('agents.info.model')} width=${300} class="cm-dock-pop">
      <div class="cm-dock-pop-in">
        <div class="cm-dock-list">
          <button type="button" class=${cls('item', !model && 'active')} onClick=${() => { setModel(''); setModelPop(null); }}>
            <span class="item-label"><span class="item-title">${t('agents.cfg.cli_default')}</span></span>
            ${!model && html`<${Icon} n="check" class="item-check" />`}
          </button>
          ${presets.map(p => html`<button type="button" key=${p.value} class=${cls('item', model === p.value && 'active')} onClick=${() => { setModel(p.value); setModelPop(null); }}>
            <span class="item-label"><span class="item-title">${p.label}</span></span>
            ${model === p.value && html`<${Icon} n="check" class="item-check" />`}
          </button>`)}
        </div>
        <div class="cm-dock-custom">
          <input class="input sm mono" value=${customModel} placeholder=${t('agents.studio.custom_model')} onInput=${e => setCustomModel(e.target.value)}
            onKeyDown=${e => { if (e.key === 'Enter') { setModel(customModel.trim()); setModelPop(null); } }} />
          <button type="button" class="btn sm" onClick=${() => { setModel(customModel.trim()); setModelPop(null); }}>${t('agents.save')}</button>
        </div>
      </div>
    </${Popover}>`}
    ${effortPop && html`<${Menu} anchor=${effortPop} place="above" onClose=${() => setEffortPop(null)} items=${effortItems} label=${t('agents.cfg.effort')} />`}
    ${permPop && html`<${Menu} anchor=${permPop} place="above" onClose=${() => setPermPop(null)} items=${permItems} label=${t('agents.cfg.claude_mode')} />`}
  </div>`;
}
