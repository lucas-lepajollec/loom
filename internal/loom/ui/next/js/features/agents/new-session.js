// Nouvelle session : un CLI du registre Codeman, un dossier vu par le serveur
// Codeman (pas forcément la machine de Loom), un nom et, selon le CLI, un
// modèle. Les modes de permission restent ceux réglés dans Codeman.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Modal, toast } from '../../ui/dialog.js';
import { api } from './client.js';
import { agents, cliLogo } from './store.js';
import { createSession, supportsModel } from './actions.js';

export const CliMark = ({ mode, size }) => {
  if (mode === 'shell') return html`<span class=${cls('mono-tile', size)} aria-hidden="true"><${Icon} n="terminal" /></span>`;
  const c = agents.get().clis.find(x => x.id === mode);
  const logo = cliLogo(mode);
  return logo ? html`<${Logo} id=${logo} size=${size} />` : html`<span class=${cls('mono-tile cm-badge', size)} aria-hidden="true">${(c && c.shortBadge) || String(mode || '?').slice(0, 2).toUpperCase()}</span>`;
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
      : st.entries.length ? st.entries.map(e => html`<button key=${e.path} onClick=${() => go(e.path)}><${Icon} n="folder" /><span class="trunc">${e.name}</span></button>`)
      : html`<p class="muted">${t('agents.new.no_subfolders')}</p>`}</div>`}
  </div>`;
}

export function NewSession({ onClose, onCreated, preset }) {
  const clis = useStore(agents, s => s.clis.filter(c => c.enabled !== false));
  const unified = useStore(agents, s => s.unified);
  const cases = useStore(agents, s => s.cases);
  const [mode, setMode] = useState(preset?.mode || (clis.find(c => c.id === 'claude' && c.installed) ? 'claude' : (clis.find(c => c.installed && c.kind === 'agent') || {}).id || 'shell'));
  const [dir, setDir] = useState(preset?.workingDir || '');
  const [name, setName] = useState('');
  const [model, setModel] = useState('');
  const [browse, setBrowse] = useState(false);
  const [busy, setBusy] = useState(false);
  const recent = [...new Set(unified.map(r => r.workingDir).filter(Boolean))].slice(0, 6);
  const folders = [...new Set([...recent, ...cases.filter(c => c.location !== 'remote').map(c => c.path)])].slice(0, 10);
  useEffect(() => { if (!dir && folders.length) setDir(folders[0]); }, [folders.length]);
  const cli = clis.find(c => c.id === mode);
  const submit = async () => {
    if (busy || !dir.trim()) return;
    setBusy(true);
    try { const id = await createSession({ mode, workingDir: dir.trim(), name, model }); onCreated && onCreated(id); onClose(); }
    catch (e) { toast(e.message, 'err'); }
    finally { setBusy(false); }
  };
  return html`<${Modal} wide title=${t('agents.new.title')} sub=${t('agents.new.sub')} onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>${t('agents.cancel')}</button><button class="btn primary" disabled=${busy || !dir.trim() || (cli && cli.installed === false)} onClick=${submit}>${busy ? t('agents.new.starting') : t('agents.new.start')}</button>`}>
    <div class="field"><span>${t('agents.new.cli')}</span>
      <div class="cm-cli-grid">${clis.map(c => html`<button type="button" key=${c.id} class=${cls('cm-cli', mode === c.id && 'on', c.installed === false && 'missing')} onClick=${() => { setMode(c.id); setModel(''); }} aria-pressed=${String(mode === c.id)}>
        <${CliMark} mode=${c.id} size="sm" /><span><b>${c.label}</b>${c.installed === false && html`<small>${t('agents.new.not_installed')}</small>`}</span></button>`)}</div>
      ${cli && cli.installed === false && html`<small>${t('agents.new.install_hint')} <code>${cli.installCommand || ''}</code></small>`}</div>
    <div class="field"><span>${t('agents.new.folder')}</span>
      <div class="cm-dir"><${Icon} n="folder" /><input class="input mono" value=${dir} placeholder="/home/…/projet" onInput=${e => setDir(e.target.value)} />
        <button class="btn sm" onClick=${() => setBrowse(!browse)}>${browse ? t('agents.new.hide_browser') : t('agents.new.browse')}</button></div>
      ${folders.length > 0 && html`<div class="chips">${folders.map(f => html`<button type="button" key=${f} class=${cls('chip-btn', dir === f && 'on')} onClick=${() => setDir(f)} title=${f}>${f.split('/').pop() || f}</button>`)}</div>`}
      ${browse && html`<${Browser} start=${dir} onPick=${p => { setDir(p); setBrowse(false); }} />`}</div>
    <div class="cm-row2">
      <label class="field"><span>${t('agents.new.name')}</span><input class="input" value=${name} placeholder=${t('agents.new.name_ph')} onInput=${e => setName(e.target.value)} /></label>
      ${supportsModel(mode) && html`<label class="field"><span>${t('agents.new.model')}</span><input class="input mono" value=${model} placeholder=${t('agents.new.model_ph')} onInput=${e => setModel(e.target.value)} /></label>`}
    </div>
    <p class="note muted">${t('agents.new.permissions')}</p>
  </${Modal}>`;
}
