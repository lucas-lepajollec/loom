// Mémoire du Brain : des fichiers Markdown dans le second cerveau, au format
// de la mémoire automatique de Claude Code (un index MEMORY.md, un fichier par
// souvenir). Les agents les lisent et les écrivent eux-mêmes ; Loom les met à
// jour après chaque discussion et les affiche ici.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Switch } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';

const TYPES = ['user', 'feedback', 'project', 'reference'];
const TYPE = () => ({ user: t('memory.type.user'), feedback: t('memory.type.feedback'), project: t('memory.type.project'), reference: t('memory.type.reference') });
const TYPE_NOTE = () => ({ user: t('memory.type.user_note'), feedback: t('memory.type.feedback_note'), project: t('memory.type.project_note'), reference: t('memory.type.reference_note') });
const TYPE_ICON = { user: 'heart', feedback: 'check', project: 'folder', reference: 'link' };
const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const ago = ms => {
  if (!ms) return '';
  const m = Math.round((Date.now() - ms) / 60000);
  return m < 1 ? t('resources.brain.a_l_instant') : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) });
};

function MemoryEditor({ scope, item, onClose }) {
  const [v, setV] = useState(item ? { name: item.name, description: item.description, type: item.type || 'project', text: item.text } : { name: '', description: '', type: 'user', text: '' });
  const [busy, setBusy] = useState(false);
  const set = p => setV({ ...v, ...p });
  const save = async () => {
    setBusy(true);
    const r = await post('/api/brain/memory', { scope, ...(item ? { file: item.file } : {}), ...v }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false || r.error) return toast(r.error || t('memory.save_failed'), 'err');
    onClose(true);
  };
  const remove = async () => {
    if (!await confirm(t('memory.forget'), t('memory.forget_note_file'), { ok: t('memory.forget'), danger: true })) return;
    const r = await post('/api/brain/memory/delete', { scope, file: item.file }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false || r.error) return toast(r.error || t('memory.save_failed'), 'err');
    onClose(true);
  };
  return html`<${Modal} wide title=${item ? t('memory.edit') : t('memory.add')} sub=${item ? html`<span class="mono">${item.file}</span>` : t('memory.add_sub')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`${item && html`<button class="btn ghost danger" disabled=${busy} onClick=${remove}>${t('memory.forget')}</button>`}<span class="grow"></span><button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !v.name.trim() || !v.text.trim()} onClick=${save}>${t('ui.dialog.enregistrer')}</button>`}>
    <div class="ws-form">
      <label class="field"><span>${t('memory.kind')}</span><${ListPick} label=${t('memory.kind')} value=${v.type} onChange=${type => set({ type })} options=${TYPES.map(k => ({ value: k, label: TYPE()[k], note: TYPE_NOTE()[k] }))} /></label>
      <label class="field"><span>${t('memory.name')}</span><input class="input" value=${v.name} maxlength="120" onInput=${e => set({ name: e.target.value })} /></label>
      <label class="field"><span>${t('memory.description')}</span><input class="input" value=${v.description} maxlength="240" placeholder=${t('memory.description_hint')} onInput=${e => set({ description: e.target.value })} /></label>
      <label class="field"><span>${t('memory.text')}</span><textarea class="textarea" rows="10" value=${v.text} onInput=${e => set({ text: e.target.value })}></textarea>
        ${(v.type === 'feedback' || v.type === 'project') && html`<small>${t('memory.why_how')}</small>`}</label>
    </div></${Modal}>`;
}

// Mise à jour automatique : après chaque discussion, un modèle relit ce qui a
// été dit et ajoute, corrige ou retire des souvenirs. Aucune phrase magique.
function Consolidation() {
  const ws = useStore(app, a => a.workspace);
  const providers = ((ws && ws.providers) || []).filter(p => p.ready);
  const [st, setSt] = useState(null);
  const [cfg, setCfg] = useState(null);
  const load = () => {
    get('/api/brain/memory/status').then(r => setSt(r.ok === false ? null : r)).catch(() => {});
    get('/api/brain/memory/settings').then(r => setCfg(r.ok === false ? null : r)).catch(() => {});
  };
  useEffect(() => { load(); }, []);
  if (!cfg) return null;
  const value = m => typeof m === 'string' ? m : m && m.provider_id ? 'p:' + m.provider_id + ':' + m.model : 'discussion';
  const decode = v => v.startsWith('p:') ? (([, id, ...model]) => ({ provider_id: id, model: model.join(':') }))(v.split(':')) : v;
  const options = fallback => [...(fallback ? [] : [{ value: 'discussion', label: t('memory.cons.discussion'), note: t('memory.cons.discussion_note') }]),
    { value: 'local-loaded', label: t('memory.cons.local'), note: t('memory.cons.local_note') },
    ...providers.flatMap(p => (p.models && p.models.length ? p.models.slice(0, 6) : [p.model]).filter(Boolean).map(m => ({ value: 'p:' + p.id + ':' + m, label: m, group: p.name }))),
    { value: 'off', label: t('memory.cons.off') }];
  const save = async (key, v) => {
    if (v.startsWith('p:') && !await confirm(t('memory.cons.consent_title'), t('memory.cons.consent_text'), { ok: t('memory.cons.consent_ok') })) return;
    const r = await post('/api/brain/memory/settings', { [key]: decode(v) }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false || r.error) return toast(r.error, 'err');
    setCfg(r);
  };
  const ops = (st && st.last_operations) || [];
  return html`<section class="sec"><div class="sec-h"><h2>${t('memory.cons.title')}<${Tip} text=${t('memory.cons.tip')} /></h2></div>
    <div class="card bs-list">
      <div class="bs-row"><span class="mono-tile"><${Icon} n="history" /></span>
        <div class="grow"><div class="bs-name">${t('memory.cons.model')}</div><div class="bs-sub"><span>${st && st.last_run ? t('memory.cons.last', { when: ago(st.last_run), n: ops.length }) : t('memory.cons.never')}</span>${st && st.last_error && html`<span class="bs-state err" title=${st.last_error}>${t('resources.brain.erreur')}</span>`}</div></div>
        <div class="es-pick"><${ListPick} label=${t('memory.cons.model')} value=${value(cfg.model)} onChange=${v => save('model', v)} options=${options(false)} /></div></div>
      ${value(cfg.model) === 'discussion' && html`<div class="bs-row"><span class="mono-tile"><${Icon} n="terminal" /></span>
        <div class="grow"><div class="bs-name">${t('memory.cons.agents')}</div><div class="bs-sub"><span>${t('memory.cons.agents_note')}</span></div></div>
        <div class="es-pick"><${ListPick} label=${t('memory.cons.agents')} value=${value(cfg.fallback) === 'discussion' ? 'off' : value(cfg.fallback)} onChange=${v => save('fallback', v)} options=${options(true)} /></div></div>`}
    </div></section>`;
}

// Agents reliés au cerveau : chacun lit et écrit la mémoire par ses propres
// mécanismes officiels (réglage de Claude Code, fichier AGENTS.md…), même lancé
// sans Loom.
function LinkedAgents() {
  const [d, setD] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => get('/api/brain/agents').then(r => setD(r.ok === false ? null : r)).catch(() => setD(null));
  useEffect(() => { load(); }, []);
  if (!d) return null;
  const toggle = async (a, enabled) => {
    setBusy(a.id);
    const r = await post('/api/brain/agents', { id: a.id, enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.ok === false || r.error) return toast(r.error, 'err');
    setD(r);
  };
  return html`<section class="sec"><div class="sec-h"><h2>${t('memory.agents.title')}<${Tip} text=${t('memory.agents.tip')} /></h2></div>
    <div class="card bs-list">${(d.agents || []).filter(a => a.supported || !a.id.startsWith('custom-node-')).map(a => html`<div class="bs-row" key=${a.id}>
      <span class="mono-tile"><${Icon} n="terminal" /></span>
      <div class="grow"><div class="bs-name">${a.name}</div><div class="bs-sub">${a.supported ? html`<span class="mono trunc" title=${a.file}>${home(a.file)}</span>${a.linked && html`<span class="bs-state"><i class="dot green"></i>${t('memory.agents.linked')}</span>`}` : html`<span>${a.note || t('mcp.gw.unsupported')}</span>`}</div></div>
      <${Switch} checked=${!!a.linked} disabled=${!a.supported || busy === a.id} label=${a.name} onChange=${on => toggle(a, on)} /></div>`)}</div>
  </section>`;
}

export function MemoryItems({ q = '' }) {
  const projects = useStore(app, a => (a.workspace && a.workspace.projects) || []);
  const [scope, setScope] = useState('global');
  const [data, setData] = useState(null);
  const [edit, setEdit] = useState(null);
  const load = () => get('/api/brain/memory?scope=' + encodeURIComponent(scope)).then(r => setData(r.ok === false ? { error: r.error, items: [] } : r)).catch(e => setData({ error: e.message, items: [] }));
  useEffect(() => { setData(null); load(); }, [scope]);
  const needle = q.trim().toLowerCase();
  const items = ((data && data.items) || []).filter(m => !needle || (m.name + ' ' + m.description + ' ' + m.text).toLowerCase().includes(needle));
  const order = { user: 0, feedback: 1, project: 2, reference: 3 };
  items.sort((a, b) => (order[a.type] ?? 9) - (order[b.type] ?? 9) || (b.updated_at || 0) - (a.updated_at || 0));
  const scopes = [{ value: 'global', label: t('memory.scope.everywhere') }, ...projects.map(p => ({ value: 'project:' + p.id, label: p.name, group: t('memory.scope.projects') }))];
  return html`<div class="bs">
    <section class="sec"><div class="sec-h mi-items-h"><h2>${t('memory.files.title')}${data && !data.error && html` <span class="count">${(data.items || []).length}</span>`}<${Tip} text=${t('memory.files.tip')} /></h2><span class="grow"></span>
        <div class="mi-pick"><${ListPick} label=${t('memory.scope')} value=${scope} onChange=${setScope} options=${scopes} /></div></div>
      ${data && data.path && html`<p class="note mi-path"><${Icon} n="folder" /><span class="mono">${home(data.path)}</span>${data.warning && html` · <b>${data.warning}</b>`}</p>`}
      ${data === null ? html`<div class="skeleton" style="height:140px"></div>`
        : data.error ? html`<div class="card pad"><p class="note err">${data.error}</p></div>`
        : html`<div class="mcards">${items.map(m => html`<button type="button" class=${cls('mcard mi-card', m.malformed && 'dim')} key=${m.file} onClick=${() => setEdit(m)}>
            <div class="mcard-h"><span class="mx-ico"><${Icon} n=${TYPE_ICON[m.type] || 'file'} /></span><span class="grow"><b>${m.name || m.file}</b><small>${TYPE()[m.type] || t('memory.type.unknown')}${m.updated_at ? ' · ' + ago(m.updated_at) : ''}</small></span><${Icon} n="edit" /></div>
            ${(d => d && html`<p class="mcard-d mi-card-d">${d}</p>`)([m.description, m.text].find(x => x && x.trim() !== (m.name || '').trim()))}</button>`)}
          <button type="button" class="mcard add" onClick=${() => setEdit({})}><${Icon} n="plus" /><span>${t('memory.add')}</span><small>${t('memory.add_note')}</small></button></div>`}
    </section>
    <${Consolidation} />
    <${LinkedAgents} />
    ${edit && html`<${MemoryEditor} scope=${scope} item=${edit.file ? edit : null} onClose=${ok => { setEdit(null); if (ok) load(); }} />`}
  </div>`;
}
