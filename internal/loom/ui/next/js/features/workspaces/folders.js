import { html, useState, useEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { FolderPicker } from '../../ui/folder.js';
import { Modal, toast, confirm } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { ListPick } from '../../ui/listpick.js';
import { Menu } from '../../ui/controls.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');

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
    <form class="ws-form" onSubmit=${submit}>
      <label class="field"><span>${t('workspaces.path')}</span>
        <div class="field-row"><input class="input" aria-label=${t('workspaces.path')} value=${path} onInput=${e => setPath(e.target.value)} placeholder=${t('workspaces.absolute_path')} />${target === 'local' && html`<button type="button" class="btn" title=${t('workspaces.browse')} aria-label=${t('workspaces.browse')} onClick=${() => setPick(true)}><${Icon} n="folder" /></button>`}</div></label>
      ${(save || def || !choose) && html`<label class="field"><span>${t('workspaces.name')}</span><input class="input" aria-label=${t('workspaces.name')} value=${name} onInput=${e => setName(e.target.value)} /></label>`}
      <div class="ws-checks">
        ${choose && html`<label class="check"><input type="checkbox" disabled=${!target} checked=${save || def} onChange=${e => { setSave(e.target.checked); if (!e.target.checked) setDef(false); }} /><span>${t('workspaces.save')}</span></label>`}
        <label class="check"><input type="checkbox" disabled=${!target} checked=${def} onChange=${e => { setDef(e.target.checked); if (e.target.checked) setSave(true); }} /><span>${t('workspaces.make_default')}</span></label>
        ${(save || def) && html`<label class="check"><input type="checkbox" checked=${create} onChange=${e => setCreate(e.target.checked)} /><span>${t('workspaces.create')}</span></label>`}
      </div>
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
    <${ListPick} label=${t('workspaces.title')} disabled=${busy || !target} value=${items.find(w => w.path === current)?.id || ''} onChange=${v => v ? select(v) : onPick({ path: '' })}
      options=${[{ value: '', label: current || (inheritDefault && inherited ? inherited.name + ' · ' + t('workspaces.default') : t('workspaces.choose')), disabled: !inheritDefault }, ...items.map(w => ({ value: w.id, label: w.name + (w.default ? ' · ' + t('workspaces.default') : '') }))]} />
    <button class="btn ghost sm" onClick=${() => setForm(true)}>${t('workspaces.new')}</button>
    ${form && html`<${FolderForm} target=${target} current=${current} choose onClose=${() => setForm(false)} onDone=${async w => { const picked = await onPick(w); if (picked === false) return false; await refresh(); }} />`}
  </div>`;
}

// Une carte par espace de travail, rangées par machine, comme la page Machines.
function WorkspaceCard({ w, busy, onEdit, onAction }) {
  const [anchor, setAnchor] = useState(null);
  const items = [{ icon: 'edit', label: t('workspaces.edit'), run: onEdit }, ...(!w.default ? [{ icon: 'star', label: t('workspaces.make_default'), run: () => onAction('default', w) }] : []),
    ...(!w.managed ? ['-', { icon: 'trash', label: t('workspaces.remove'), danger: true, run: () => onAction('remove', w) }] : [])];
  return html`<div class="mcard ws-card">
    <div class="mcard-h"><span class="mx-ico"><${Icon} n="folder" /></span><span class="grow"><b>${w.name}</b><small class="mono" title=${w.path}>${home(w.path)}</small></span>
      <button class="icon-btn" disabled=${busy} aria-label=${t('brain.src.actions')} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button></div>
    <div class="ws-card-f">${w.default ? html`<span class="tag">${t('workspaces.default')}</span>` : html`<span class="muted">${t('workspaces.saved')}</span>`}${w.managed && html`<span class="muted">${t('workspaces.managed')}</span>`}</div>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${items} />`}
  </div>`;
}

function MachineWorkspaces({ target, title, sub }) {
  const [items, setItems] = useState(null);
  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);
  const refresh = () => get(endpoint(target)).then(r => { if (!r.ok) throw new Error(r.error); setItems(r.workspaces || []); }).catch(e => { setItems([]); toast(e.message, 'err'); });
  useEffect(() => { refresh(); }, [target]);
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
  return html`<section class="sec ws-machine">
    ${title && html`<div class="sec-h"><h2>${title}</h2>${sub && html`<span class="muted mono ws-sub">${sub}</span>`}</div>`}
    ${items === null ? html`<div class="skeleton" style="height:96px"></div>` : html`<div class="mcards">
      ${items.map(w => html`<${WorkspaceCard} key=${w.id} w=${w} busy=${busy} onEdit=${() => setForm(w)} onAction=${action} />`)}
      <button type="button" class="mcard add" onClick=${() => setForm({})}><${Icon} n="plus" /><span>${t('workspaces.add')}</span></button>
    </div>`}
    ${form && html`<${FolderForm} target=${target} editing=${form.id ? form : null} onClose=${() => setForm(null)} onDone=${refresh} />`}
  </section>`;
}

export function WorkspaceManager({ fixedTarget }) {
  const [machines, setMachines] = useState(null);
  const [local, setLocal] = useState(null);
  useEffect(() => {
    if (fixedTarget) return;
    get('/api/machines').then(r => setMachines(r.machines || [])).catch(() => setMachines([]));
    get('/api/machines/local').then(setLocal).catch(() => {});
  }, []);
  if (fixedTarget) return html`<${MachineWorkspaces} target=${fixedTarget} />`;
  return html`<div class="ws-page">
    <${MachineWorkspaces} target="local" title=${(local && local.hostname) || t('workspaces.local')} sub=${t('settings.machines.cette_machine')} />
    ${(machines || []).map(m => html`<${MachineWorkspaces} key=${m.id} target=${m.id} title=${m.name || m.host} sub=${m.user + '@' + m.host} />`)}
    <p class="note">${t('workspaces.note')}</p>
  </div>`;
}
