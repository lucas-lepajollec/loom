import { html, useState, useEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { FolderPicker } from '../../ui/folder.js';
import { Modal, toast, confirm } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { Line, Group } from '../settings/kit.js';

const endpoint = target => '/api/workspaces?target=' + encodeURIComponent(target || 'local');
async function change(target, body) {
  const r = await post(endpoint(target), body);
  if (!r.ok) throw new Error(r.error || t('workspaces.failed'));
  return r;
}

function FolderForm({ target, current, editing, onClose, onDone, choose }) {
  const [name, setName] = useState(editing?.name || '');
  const [path, setPath] = useState(editing?.path || current || '');
  const [save, setSave] = useState(!choose || !!editing);
  const [def, setDef] = useState(!!editing?.default);
  const [create, setCreate] = useState(false);
  const [pick, setPick] = useState(false);
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  const submit = async e => {
    e?.preventDefault();
    if (running.current || !path.trim() || ((save || def) && !name.trim())) return;
    running.current = true; setBusy(true);
    try {
      let folder = { id: editing?.id || '', target, name: name.trim(), path: path.trim(), default: def };
      if (save || def) {
        const result = await change(target, { action: 'save', workspace: folder, create });
        folder = result.workspace;

      } else if (create) {
        // Creating a directory is an explicit saved-workspace action.
        throw new Error(t('workspaces.create_requires_save'));
      }
      if (await onDone(folder) === false) throw new Error(t('workspaces.failed')); onClose();
    } catch (e) { toast(e.message, 'err'); }
    finally { running.current = false; setBusy(false); }
  };
  return html`<${Modal} title=${editing ? t('workspaces.edit') : t('workspaces.new')} onClose=${busy ? undefined : onClose}
    foot=${html`<button class="btn ghost" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !path.trim() || ((save || def) && !name.trim())} onClick=${submit}>${t('ui.dialog.enregistrer')}</button>`}>
    <form onSubmit=${submit}>
      <label class="lbl">${t('workspaces.path')}</label>
      <div class="row"><input class="input" aria-label=${t('workspaces.path')} value=${path} onInput=${e => setPath(e.target.value)} placeholder=${t('workspaces.absolute_path')} />${target === 'local' && html`<button type="button" class="btn" onClick=${() => setPick(true)}><${Icon} n="folder" /></button>`}</div>
      ${choose && html`<label class="row"><input type="checkbox" disabled=${!target} checked=${save || def} onChange=${e => { setSave(e.target.checked); if (!e.target.checked) setDef(false); }} />${t('workspaces.save')}</label>`}
      ${(save || def) && html`<label class="lbl">${t('workspaces.name')}</label><input class="input" aria-label=${t('workspaces.name')} value=${name} onInput=${e => setName(e.target.value)} />`}
      <label class="row"><input type="checkbox" disabled=${!target} checked=${def} onChange=${e => { setDef(e.target.checked); if (e.target.checked) setSave(true); }} />${t('workspaces.make_default')}</label>
      ${(save || def) && html`<label class="row"><input type="checkbox" checked=${create} onChange=${e => setCreate(e.target.checked)} />${t('workspaces.create')}</label>`}
    </form>
    ${pick && html`<${FolderPicker} start=${path} onClose=${() => setPick(false)} onPick=${p => { setPath(p); setPick(false); }} />`}
  </${Modal}>`;
}

export function WorkspacePicker({ target = 'local', current = '', onPick, inheritDefault = false }) {
  const [items, setItems] = useState([]);
  const [form, setForm] = useState(false);
  const [busy, setBusy] = useState(false);
  const refresh = () => get(endpoint(target)).then(r => { if (!r.ok) throw new Error(r.error); setItems(r.workspaces || []); }).catch(e => toast(e.message, 'err'));
  useEffect(() => { let alive = true; setItems([]); get(endpoint(target)).then(r => { if (alive && r.ok) setItems(r.workspaces || []); }).catch(() => {}); return () => { alive = false; }; }, [target]);
  const select = async id => {
    const w = items.find(w => w.id === id);
    if (!w) return;
    setBusy(true); try { await onPick(w); } finally { setBusy(false); }
  };
  const inherited = items.find(w => w.default);
  return html`<div class="workspace-picker">
    <select class="select" aria-label=${t('workspaces.title')} disabled=${busy || !target} value=${items.find(w => w.path === current)?.id || ''} onChange=${e => e.target.value ? select(e.target.value) : onPick({ path: '' })}>
      <option value="" disabled=${!inheritDefault}>${current || (inheritDefault && inherited ? inherited.name + ' · ' + t('workspaces.default') : t('workspaces.choose'))}</option>
      ${items.map(w => html`<option key=${w.id} value=${w.id}>${w.name}${w.default ? ' · ' + t('workspaces.default') : ''}</option>`)}
    </select>
    <button class="btn ghost sm" onClick=${() => setForm(true)}>${t('workspaces.new')}</button>
    ${form && html`<${FolderForm} target=${target} current=${current} choose onClose=${() => setForm(false)} onDone=${async w => { const picked = await onPick(w); if (picked === false) return false; await refresh(); }} />`}
  </div>`;
}

export function WorkspaceManager({ fixedTarget }) {
  const [target, setTarget] = useState(fixedTarget || 'local');
  const [machines, setMachines] = useState([]);
  const [items, setItems] = useState([]);
  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);
  const refresh = () => get(endpoint(target)).then(r => { if (!r.ok) throw new Error(r.error); setItems(r.workspaces || []); });
  useEffect(() => { if (!fixedTarget) get('/api/machines').then(r => setMachines(r.machines || [])).catch(() => {}); }, []);
  useEffect(() => { let alive = true; setItems([]); get(endpoint(target)).then(r => { if (alive && r.ok) setItems(r.workspaces || []); }).catch(e => toast(e.message, 'err')); return () => { alive = false; }; }, [target]);
  const action = async (action, w) => {
    if (action === 'remove' && !await confirm(t('workspaces.remove'), t('workspaces.remove_note'))) return;
    setBusy(true);
    try {
      // The implicit Loom directory becomes a saved entry only on explicit edit/default.
      if (action === 'default' && w.managed) setItems((await change(target, { action: 'save', workspace: w, create: true })).workspaces || []);
      else setItems((await change(target, { action, id: w.id })).workspaces || []);
    } catch (e) { toast(e.message, 'err'); }
    finally { setBusy(false); }
  };
  return html`<div>
    ${!fixedTarget && html`<${Group}><${Line} label=${t('workspaces.machine')}><select class="select" value=${target} onChange=${e => setTarget(e.target.value)}><option value="local">${t('workspaces.local')}</option>${machines.map(m => html`<option key=${m.id} value=${m.id}>${m.name || m.host}</option>`)}</select></${Line}></${Group}>`}
    <${Group} title=${t('workspaces.title')}>
      ${items.map(w => html`<${Line} key=${w.id} label=${w.name + (w.default ? ' · ' + t('workspaces.default') : '')} tip=${w.path}>
        <button class="btn sm ghost" disabled=${busy} onClick=${() => setForm(w)}>${t('workspaces.edit')}</button>
        ${!w.default && html`<button class="btn sm" disabled=${busy} onClick=${() => action('default', w)}>${t('workspaces.make_default')}</button>`}
        ${!w.managed && html`<button class="icon-btn" aria-label=${t('workspaces.remove')} disabled=${busy} onClick=${() => action('remove', w)}><${Icon} n="trash" /></button>`}
      </${Line}>`)}
      <${Line} label=${t('workspaces.new')}><button class="btn sm" onClick=${() => setForm({})}><${Icon} n="plus" />${t('workspaces.add')}</button></${Line}>
    </${Group}>
    <p class="note">${t('workspaces.note')}</p>
    ${form && html`<${FolderForm} target=${target} editing=${form.id ? form : null} onClose=${() => setForm(null)} onDone=${refresh} />`}
  </div>`;
}
