// Mémoire du Brain : les éléments que Loom et les agents retiennent (réflexes,
// état de travail, procédures, faits, épisodes), rangés par classe, avec leur
// portée et leur provenance. Modifier peut garder l'ancienne version ;
// oublier ne supprime jamais le fichier.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Switch } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go } from '../../core/state.js';

export const MEMORY_CLASSES = ['reflex', 'working', 'procedural', 'semantic', 'episodic', 'session'];
const STATUSES = ['active', 'uncertain', 'superseded', 'expired', 'all'];

const CLASS = () => ({ reflex: t('memory.class.reflex'), working: t('memory.class.working'), procedural: t('memory.class.procedural'), semantic: t('memory.class.semantic'), episodic: t('memory.class.episodic'), session: t('memory.class.session') });
const CLASS_NOTE = () => ({ reflex: t('memory.class.reflex_note'), working: t('memory.class.working_note'), procedural: t('memory.class.procedural_note'), semantic: t('memory.class.semantic_note'), episodic: t('memory.class.episodic_note'), session: t('memory.class.session_note') });
const STATUS = () => ({ active: t('memory.status.active'), uncertain: t('memory.status.uncertain'), superseded: t('memory.status.superseded'), expired: t('memory.status.expired'), candidate: t('memory.status.candidate'), all: t('memory.status.all') });
const CLASS_ICON = { reflex: 'pulse', working: 'tool', procedural: 'sliders', semantic: 'info', episodic: 'history', session: 'chat' };
const PROV = () => ({ user: t('memory.prov.user'), agent: t('memory.prov.agent'), discussion: t('memory.prov.discussion'), import: t('memory.prov.import'), distilled: t('memory.prov.distilled') });
const SCOPE_KIND = () => ({ machine: t('memory.scope.machine'), agent: t('memory.scope.agent'), task: t('memory.scope.task'), project: t('memory.scope.project') });

const ago = ms => {
  if (!ms) return '';
  const m = Math.round((Date.now() - ms) / 60000);
  return m < 1 ? t('resources.brain.a_l_instant') : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) });
};

function scopeLabel(scope, projects) {
  if (!scope || scope === 'global') return t('memory.scope.global');
  const [kind, id] = [scope.slice(0, scope.indexOf(':')), scope.slice(scope.indexOf(':') + 1)];
  if (kind === 'project') return (projects.find(p => p.id === id) || {}).name || t('memory.scope.project');
  return (SCOPE_KIND()[kind] || kind) + ' · ' + id;
}

function ItemForm({ item, accept, projects, onClose, onSaved }) {
  const [v, setV] = useState(item ? { text: item.text, scope: item.scope, importance: item.importance ?? 0.5, tags: (item.tags || []).join(', '), supersede: false }
    : { class: 'semantic', scope: 'global', text: '', importance: 0.5, tags: '' });
  const [busy, setBusy] = useState(false);
  const set = patch => setV({ ...v, ...patch });
  const tags = v.tags.split(',').map(x => x.trim()).filter(Boolean);
  const save = async () => {
    setBusy(true);
    const r = item
      ? await post('/api/brain/items/update', { id: item.id, supersede: !!v.supersede, patch: { text: v.text, scope: v.scope, importance: +v.importance, tags, ...(accept ? { status: 'active' } : {}) } })
      : await post('/api/brain/items', { class: v.class, scope: v.scope, text: v.text, importance: +v.importance, tags, provenance: { kind: 'user' } });
    setBusy(false);
    if (!r.ok) return toast(r.error || t('memory.save_failed'), 'err');
    onSaved();
  };
  const scopes = [{ value: 'global', label: t('memory.scope.global') }, ...projects.map(p => ({ value: 'project:' + p.id, label: p.name, group: t('memory.scope.projects') }))];
  return html`<${Modal} title=${item ? t('memory.edit') : t('memory.add')} onClose=${busy ? undefined : onClose}
    foot=${html`<button class="btn ghost" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !v.text.trim()} onClick=${save}>${t('ui.dialog.enregistrer')}</button>`}>
    <div class="ws-form">
      ${!item && html`<label class="field"><span>${t('memory.class')}</span><${ListPick} label=${t('memory.class')} value=${v.class} onChange=${c => set({ class: c })} options=${MEMORY_CLASSES.filter(c => c !== 'session').map(c => ({ value: c, label: CLASS()[c] }))} />
        <small>${CLASS_NOTE()[v.class]}</small></label>`}
      <label class="field"><span>${t('memory.text')}</span><textarea class="textarea" rows="5" maxlength="8000" value=${v.text} onInput=${e => set({ text: e.target.value })}></textarea></label>
      <label class="field"><span>${t('memory.scope')}</span><${ListPick} label=${t('memory.scope')} value=${v.scope} onChange=${s => set({ scope: s })} options=${scopes} /></label>
      <label class="field"><span>${t('memory.importance')} · ${Math.round(v.importance * 100)} %</span><input type="range" min="0" max="1" step="0.05" value=${v.importance} onInput=${e => set({ importance: e.target.value })} /></label>
      <label class="field"><span>${t('memory.tags')}</span><input class="input" value=${v.tags} placeholder=${t('memory.tags_hint')} onInput=${e => set({ tags: e.target.value })} /></label>
      ${item && html`<label class="check"><input type="checkbox" checked=${v.supersede} onChange=${e => set({ supersede: e.target.checked })} /><span>${t('memory.supersede')}</span><${Tip} text=${t('memory.supersede_tip')} /></label>`}
    </div></${Modal}>`;
}

// Suggestions : ce que Loom repère dans tes messages (« retiens… »,
// « toujours… ») ou tire d'une discussion analysée. Rien n'entre dans le
// contexte avant d'être gardé.
function Consolidate({ onClose }) {
  const convs = useStore(app, x => (x.nav && x.nav.conversations) || []);
  const [id, setId] = useState(convs[0] ? convs[0].id : '');
  const [busy, setBusy] = useState(false);
  const run = async consent => {
    setBusy(true);
    const r = await post('/api/brain/consolidate', consent ? { discussion_id: id, consent: true } : { discussion_id: id }, { timeout: 610000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.error && /consent required/i.test(r.error) && !consent) {
      if (await confirm(t('resources.dist.remote_title'), t('resources.dist.remote_text'), { ok: t('resources.dist.send') })) return run(true);
      return;
    }
    if (r.ok === false || r.error) return toast(r.error || t('resources.dist.failed'), 'err');
    toast(t('memory.cand.found', { n: (r.items || []).length }));
    onClose(true);
  };
  return html`<${Modal} title=${t('memory.cand.analyze')} sub=${t('memory.cand.analyze_sub')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`<button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !id} onClick=${() => run(false)}>${busy ? html`<span class="spinner"></span>${t('resources.dist.running')}` : t('memory.cand.run')}</button>`}>
    <label class="field"><span>${t('resources.dist.discussion')}</span><${ListPick} label=${t('resources.dist.discussion')} value=${id} onChange=${setId} options=${convs.map(c => ({ value: c.id, label: c.title || t('app.palette.nouvelle_discussion') }))} /></label>
    <p class="note">${t('resources.dist.engine_note')}</p>
  </${Modal}>`;
}

function Suggestions({ projects, onChanged }) {
  const [items, setItems] = useState(null);
  const [auto, setAuto] = useState(null);
  const [dlg, setDlg] = useState(false);
  const [edit, setEdit] = useState(null);
  const load = () => get('/api/brain/items?status=candidate&limit=100').then(r => setItems(r.items || [])).catch(() => setItems([]));
  useEffect(() => { load(); get('/api/brain/consolidation').then(r => setAuto(r.ok === false ? null : !!r.auto_candidates)).catch(() => {}); }, []);
  const toggle = async on => { setAuto(on); const r = await post('/api/brain/consolidation', { auto_candidates: on }).catch(e => ({ ok: false, error: e.message })); if (r.ok === false) { setAuto(!on); toast(r.error, 'err'); } };
  const keep = async it => { const r = await post('/api/brain/items/update', { id: it.id, patch: { status: 'active' } }); if (!r.ok) return toast(r.error || t('memory.save_failed'), 'err'); load(); onChanged(); };
  const drop = async it => { const r = await post('/api/brain/items/forget', { id: it.id }); if (!r.ok) return toast(r.error || t('memory.save_failed'), 'err'); load(); };
  if (items === null) return null;
  return html`<section class="sec">
    <div class="sec-h"><h2>${t('memory.cand.title')}${items.length > 0 && html` <span class="count">${items.length}</span>`}<${Tip} text=${t('memory.cand.tip')} /></h2><span class="grow"></span>
      ${auto !== null && html`<label class="mi-auto"><span>${t('memory.cand.auto')}</span><${Switch} label=${t('memory.cand.auto')} checked=${auto} onChange=${toggle} /></label>`}
      <button class="btn sm ghost" onClick=${() => setDlg(true)}><${Icon} n="sparkle" />${t('memory.cand.analyze')}</button></div>
    ${items.length ? html`<div class="mcards">${items.map(it => html`<div class="mcard mi-card" key=${it.id}>
        <div class="mcard-h"><span class="mx-ico"><${Icon} n="sparkle" /></span><span class="grow"><b>${CLASS()[it.class] || it.class}</b><small>${scopeLabel(it.scope, projects)} · ${PROV()[(it.provenance || {}).kind] || ''}</small></span></div>
        <p class="mcard-d mi-card-d">${it.text}</p>
        <div class="ws-card-f"><button class="btn sm" onClick=${() => keep(it)}><${Icon} n="check" />${t('memory.cand.keep')}</button><span class="grow"></span>
          <button class="icon-btn" aria-label=${t('memory.edit_short')} title=${t('memory.edit_short')} onClick=${() => setEdit(it)}><${Icon} n="edit" /></button>
          <button class="icon-btn" aria-label=${t('memory.cand.drop')} title=${t('memory.cand.drop')} onClick=${() => drop(it)}><${Icon} n="close" /></button></div>
      </div>`)}</div>` : html`<p class="note">${t(auto === false ? 'memory.cand.empty_off' : 'memory.cand.empty')}</p>`}
    ${dlg && html`<${Consolidate} onClose=${ok => { setDlg(false); if (ok) load(); }} />`}
    ${edit && html`<${ItemForm} item=${edit} accept projects=${projects} onClose=${() => setEdit(null)} onSaved=${() => { setEdit(null); load(); onChanged(); }} />`}
  </section>`;
}

// Mémoire cœur (comme USER.md / MEMORY.md de Hermes) : « Toi » et une note
// par projet, courtes, toujours dans le contexte et tenues à jour par les agents
// eux-mêmes. En option, une copie lisible dans le cerveau, synchronisée.
const PROFILE = 'user-profile', NOTES = 'project-notes';
const LIMITS = { [PROFILE]: 1400, [NOTES]: 2200 };
const isProfile = i => (i.tags || []).includes(PROFILE) && i.scope === 'global' && i.class === 'semantic' && i.status === 'active';
const isNotes = i => (i.tags || []).includes(NOTES) && i.scope.startsWith('project:') && i.class === 'semantic' && i.status === 'active';

function CoreEditor({ target, onClose }) {
  const [text, setText] = useState(target.item ? target.item.text : '');
  const [busy, setBusy] = useState(false);
  const limit = LIMITS[target.tag];
  const save = async () => {
    setBusy(true);
    const r = target.item ? await post('/api/brain/items/update', { id: target.item.id, patch: { text } })
      : await post('/api/brain/items', { class: 'semantic', scope: target.scope, text, importance: 1, tags: [target.tag], provenance: { kind: 'user' } });
    setBusy(false);
    if (!r.ok) return toast(r.error || t('memory.save_failed'), 'err');
    onClose(true);
  };
  return html`<${Modal} title=${target.title} sub=${target.tag === PROFILE ? t('memory.me.sub') : t('memory.notes.sub')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`<span class=${cls('mi-count-chars grow', [...text].length > limit && 'err')}>${[...text].length} / ${limit}</span><button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !text.trim() || [...text].length > limit} onClick=${save}>${t('ui.dialog.enregistrer')}</button>`}>
    <textarea class="textarea" rows="12" value=${text} placeholder=${target.tag === PROFILE ? t('memory.me.placeholder') : t('memory.notes.placeholder')} onInput=${e => setText(e.target.value)}></textarea>
  </${Modal}>`;
}

function CoreMemory({ onSaved }) {
  const projects = useStore(app, a => (a.workspace && a.workspace.projects) || []);
  const [items, setItems] = useState(null);
  const [files, setFiles] = useState(null);
  const [edit, setEdit] = useState(null);
  const load = () => get('/api/brain/items?class=semantic&limit=1000').then(r => setItems((r.items || []).filter(i => isProfile(i) || isNotes(i)))).catch(() => setItems([]));
  useEffect(() => { load(); get('/api/brain/core-files').then(r => setFiles(r.ok === false ? null : r)).catch(() => {}); }, []);
  if (items === null) return null;
  const toggleFiles = async enabled => {
    const r = await post('/api/brain/core-files', { ...files, enabled }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false || r.error) return toast(r.error, 'err');
    setFiles(r);
  };
  const profile = items.find(isProfile);
  const rows = [{ key: 'me', icon: 'heart', title: t('memory.me.title'), tag: PROFILE, scope: 'global', item: profile, empty: t('memory.me.empty') },
    ...projects.map(p => ({ key: p.id, icon: 'folder', title: p.name, tag: NOTES, scope: 'project:' + p.id, item: items.find(i => isNotes(i) && i.scope === 'project:' + p.id), empty: t('memory.notes.empty') }))];
  return html`<section class="sec">
    <div class="sec-h"><h2>${t('memory.core.title')}<${Tip} text=${t('memory.core.tip')} /></h2><span class="grow"></span>
      ${files && html`<label class="mi-auto" title=${t('memory.core.files_tip', { folder: files.folder })}><span>${t('memory.core.files', { folder: files.folder })}</span><${Switch} label=${t('memory.core.files', { folder: files.folder })} checked=${!!files.enabled} onChange=${toggleFiles} /></label>`}</div>
    <div class="mcards">${rows.map(r => html`<button type="button" class=${cls('mcard mi-card', !r.item && 'empty')} key=${r.key} onClick=${() => setEdit(r)}>
      <div class="mcard-h"><span class="mx-ico"><${Icon} n=${r.icon} /></span><span class="grow"><b>${r.title}</b><small>${r.item ? [...r.item.text].length + ' / ' + LIMITS[r.tag] : r.tag === PROFILE ? t('memory.core.profile') : t('memory.core.notes')}</small></span><${Icon} n="edit" /></div>
      <p class="mcard-d mi-card-d">${r.item ? r.item.text : r.empty}</p></button>`)}</div>
    ${edit && html`<${CoreEditor} target=${edit} onClose=${ok => { setEdit(null); if (ok) { load(); onSaved(); } }} />`}
  </section>`;
}

// Passations : écrites par Loom à chaque échange (et états de projet). Lues
// par les agents, pas à éditer : on les montre à part, en clair.
const AUTO_TAGS = ['handoff', 'discussion-state', 'project-state', 'session-summary'];
const isAuto = i => (i.tags || []).some(tag => AUTO_TAGS.includes(tag));
const readable = text => String(text || '').split('\n').filter(l => !/^Quoted discussion data/.test(l)).map(l => l.replace(/^> ?/, '')).join('\n').trim();
function Handoffs({ items, projects }) {
  if (!items.length) return null;
  const title = it => {
    const p = it.scope.startsWith('project:') && projects.find(x => 'project:' + x.id === it.scope);
    if (p) return t('memory.handoff.project', { name: p.name });
    const goal = readable(it.text).split('\n').find(l => l && !l.startsWith('#'));
    return goal ? goal.slice(0, 90) : t('memory.handoff.discussion');
  };
  return html`<details class="mi-hand"><summary><b>${t('memory.handoff.title')}</b> <span class="count">${items.length}</span><${Tip} text=${t('memory.handoff.tip')} /></summary>
    <div class="card bs-list">${items.map(it => html`<details class="mi-hand-it" key=${it.id}><summary><span class="grow trunc">${title(it)}</span><span class="mi-meta"><span>${ago(it.updated_at || it.created_at)}</span></span></summary>
      <pre class="mi-hand-text">${readable(it.text)}</pre></details>`)}</div>
  </details>`;
}

export function MemoryItems({ q = '' }) {
  const projects = useStore(app, a => (a.workspace && a.workspace.projects) || []);
  const [cls_, setClass] = useState('all');
  const [status, setStatus] = useState('active');
  const [data, setData] = useState(null);
  const [form, setForm] = useState(null);
  const load = () => {
    const params = new URLSearchParams({ status, limit: '500' });
    if (cls_ !== 'all') params.set('class', cls_);
    if (q.trim()) params.set('query', q.trim());
    get('/api/brain/items?' + params).then(r => setData(r.ok === false ? { error: r.error, items: [] } : r)).catch(e => setData({ error: e.message, items: [] }));
  };
  useEffect(() => { const timer = setTimeout(load, q ? 250 : 0); return () => clearTimeout(timer); }, [cls_, status, q]);
  const forget = async it => {
    if (!await confirm(t('memory.forget'), t('memory.forget_note'), { ok: t('memory.forget') })) return;
    const r = await post('/api/brain/items/forget', { id: it.id });
    if (!r.ok) return toast(r.error || t('memory.save_failed'), 'err');
    load();
  };
  const all = ((data && data.items) || []).filter(i => !isProfile(i) && !isNotes(i));
  const auto = all.filter(isAuto), items = all.filter(i => !isAuto(i));
  const groups = MEMORY_CLASSES.map(c => [c, items.filter(i => i.class === c)]).filter(([, list]) => list.length);
  const prov = it => {
    const p = it.provenance || {};
    return p.discussion_id ? html`<a href="#/chat" onClick=${e => { e.preventDefault(); import('../chat/engine.js').then(m => { m.open(p.discussion_id); go('chat'); }); }}>${PROV()[p.kind || 'discussion'] || p.kind}</a>` : PROV()[p.kind || 'user'] || p.kind;
  };
  const itemCard = it => html`<div class=${cls('mcard mi-card', it.status !== 'active' && 'dim')} key=${it.id}>
      <div class="mcard-h"><span class="mx-ico"><${Icon} n=${CLASS_ICON[it.class] || 'brain'} /></span><span class="grow"><b>${CLASS()[it.class] || it.class}</b><small>${scopeLabel(it.scope, projects)} · ${prov(it)} · ${ago(it.updated_at || it.created_at)}</small></span>
        ${it.status !== 'expired' && it.status !== 'superseded' && html`<button class="icon-btn" aria-label=${t('memory.edit_short')} title=${t('memory.edit_short')} onClick=${() => setForm(it)}><${Icon} n="edit" /></button>
          <button class="icon-btn" aria-label=${t('memory.forget')} title=${t('memory.forget')} onClick=${() => forget(it)}><${Icon} n="trash" /></button>`}</div>
      <p class="mcard-d mi-card-d">${it.text}</p>
      ${(it.status !== 'active' || (it.tags || []).length > 0) && html`<div class="ws-card-f">${it.status !== 'active' && html`<span class="tag">${STATUS()[it.status]}</span>`}${(it.tags || []).map(tag => html`<span class="tag" key=${tag}>${tag}</span>`)}</div>`}
    </div>`;
  return html`<div class="bs">
    <${CoreMemory} onSaved=${load} />
    <${Suggestions} projects=${projects} onChanged=${load} />
    <section class="sec"><div class="sec-h"><h2>${t('memory.items')}${data && !data.error && html` <span class="count">${items.length}</span>`}<${Tip} text=${t('memory.intro')} /></h2><span class="grow"></span>
        <div class="mi-pick"><${ListPick} label=${t('memory.class')} value=${cls_} onChange=${setClass} options=${[{ value: 'all', label: t('memory.all_classes') }, ...MEMORY_CLASSES.map(c => ({ value: c, label: CLASS()[c] }))]} /></div>
        <div class="mi-pick"><${ListPick} label=${t('memory.status')} value=${status} onChange=${setStatus} options=${STATUSES.map(s => ({ value: s, label: STATUS()[s] }))} /></div></div>
      ${data === null ? html`<div class="skeleton" style="height:180px"></div>`
        : data.error ? html`<div class="card pad"><p class="note err">${data.error}</p></div>`
        : html`<div class="mcards">${groups.flatMap(([, list]) => list).map(itemCard)}<button type="button" class="mcard add" onClick=${() => setForm({})}><${Icon} n="plus" /><span>${t('memory.add')}</span><small>${t('memory.add_note')}</small></button></div>`}
    </section>
    <${Handoffs} items=${auto} projects=${projects} />
    ${data && data.malformed > 0 && html`<p class="note">${t('memory.malformed', { n: data.malformed })}</p>`}
    ${form && html`<${ItemForm} item=${form.id ? form : null} projects=${projects} onClose=${() => setForm(null)} onSaved=${() => { setForm(null); load(); }} />`}
  </div>`;
}
