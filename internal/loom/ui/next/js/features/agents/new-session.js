// Nouvelle session : un CLI du registre Codeman, un dossier vu par le serveur
// Codeman (pas forcément la machine de Loom), un nom et, selon le CLI, un
// modèle. Les modes de permission restent ceux réglés dans Codeman.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useMemo, useStore, useRef, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Modal, toast } from '../../ui/dialog.js';
import { ListPick } from '../../ui/listpick.js';
import { app } from '../../core/state.js';
import { api, flattenPrompt, sendPrompt } from './client.js';
import { agents, cliLogo, ensureHistory } from './store.js';
import { agentDraftKey, createSession, supportsModel } from './actions.js';
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
