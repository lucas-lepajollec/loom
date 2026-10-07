// Mémoire du Brain : les éléments que Loom et les agents retiennent (réflexes,
// état de travail, procédures, faits, épisodes), rangés par classe, avec leur
// portée et leur provenance. Modifier peut garder l'ancienne version ;
// oublier ne supprime jamais le fichier.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Empty, Tip } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go } from '../../core/state.js';

export const MEMORY_CLASSES = ['reflex', 'working', 'procedural', 'semantic', 'episodic', 'session'];
const STATUSES = ['active', 'uncertain', 'superseded', 'expired', 'all'];

const CLASS = () => ({ reflex: t('memory.class.reflex'), working: t('memory.class.working'), procedural: t('memory.class.procedural'), semantic: t('memory.class.semantic'), episodic: t('memory.class.episodic'), session: t('memory.class.session') });
const CLASS_NOTE = () => ({ reflex: t('memory.class.reflex_note'), working: t('memory.class.working_note'), procedural: t('memory.class.procedural_note'), semantic: t('memory.class.semantic_note'), episodic: t('memory.class.episodic_note'), session: t('memory.class.session_note') });
const STATUS = () => ({ active: t('memory.status.active'), uncertain: t('memory.status.uncertain'), superseded: t('memory.status.superseded'), expired: t('memory.status.expired'), all: t('memory.status.all') });
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

function ItemForm({ item, projects, onClose, onSaved }) {
  const [v, setV] = useState(item ? { text: item.text, scope: item.scope, importance: item.importance ?? 0.5, tags: (item.tags || []).join(', '), supersede: false }
    : { class: 'semantic', scope: 'global', text: '', importance: 0.5, tags: '' });
  const [busy, setBusy] = useState(false);
  const set = patch => setV({ ...v, ...patch });
  const tags = v.tags.split(',').map(x => x.trim()).filter(Boolean);
  const save = async () => {
    setBusy(true);
    const r = item
      ? await post('/api/brain/items/update', { id: item.id, supersede: !!v.supersede, patch: { text: v.text, scope: v.scope, importance: +v.importance, tags } })
      : await post('/api/brain/items', { class: v.class, scope: v.scope, text: v.text, importance: +v.importance, tags, provenance: { kind: 'user' } });
    setBusy(false);
    if (!r.ok) return toast(r.error || t('memory.save_failed'), 'err');
    onSaved();
  };
  const scopes = [{ value: 'global', label: t('memory.scope.global') }, ...projects.map(p => ({ value: 'project:' + p.id, label: p.name, group: t('memory.scope.projects') }))];
  return html`<${Modal} title=${item ? t('memory.edit') : t('memory.add')} onClose=${busy ? undefined : onClose}
    foot=${html`<button class="btn ghost" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !v.text.trim()} onClick=${save}>${t('ui.dialog.enregistrer')}</button>`}>
    <div class="ws-form">
      ${!item && html`<label class="field"><span>${t('memory.class')}</span><${Seg} label=${t('memory.class')} value=${v.class} onChange=${c => set({ class: c })} options=${MEMORY_CLASSES.filter(c => c !== 'session').map(c => ({ value: c, label: CLASS()[c] }))} />
        <small>${CLASS_NOTE()[v.class]}</small></label>`}
      <label class="field"><span>${t('memory.text')}</span><textarea class="textarea" rows="5" maxlength="8000" value=${v.text} onInput=${e => set({ text: e.target.value })}></textarea></label>
      <label class="field"><span>${t('memory.scope')}</span><${ListPick} label=${t('memory.scope')} value=${v.scope} onChange=${s => set({ scope: s })} options=${scopes} /></label>
      <label class="field"><span>${t('memory.importance')} · ${Math.round(v.importance * 100)} %</span><input type="range" min="0" max="1" step="0.05" value=${v.importance} onInput=${e => set({ importance: e.target.value })} /></label>
      <label class="field"><span>${t('memory.tags')}</span><input class="input" value=${v.tags} placeholder=${t('memory.tags_hint')} onInput=${e => set({ tags: e.target.value })} /></label>
      ${item && html`<label class="check"><input type="checkbox" checked=${v.supersede} onChange=${e => set({ supersede: e.target.checked })} /><span>${t('memory.supersede')}</span><${Tip} text=${t('memory.supersede_tip')} /></label>`}
    </div></${Modal}>`;
}

export function MemoryItems() {
  const projects = useStore(app, a => (a.workspace && a.workspace.projects) || []);
  const [cls_, setClass] = useState('all');
  const [status, setStatus] = useState('active');
  const [q, setQ] = useState('');
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
  const items = (data && data.items) || [];
  const groups = MEMORY_CLASSES.map(c => [c, items.filter(i => i.class === c)]).filter(([, list]) => list.length);
  const prov = it => {
    const p = it.provenance || {};
    return p.discussion_id ? html`<a href="#/chat" onClick=${e => { e.preventDefault(); import('../chat/engine.js').then(m => { m.open(p.discussion_id); go('chat'); }); }}>${PROV()[p.kind || 'discussion'] || p.kind}</a>` : PROV()[p.kind || 'user'] || p.kind;
  };
  return html`<div class="mi">
    <div class="mi-bar">
      <${Seg} size="sm" label=${t('memory.class')} value=${cls_} onChange=${setClass} options=${[{ value: 'all', label: t('memory.all') }, ...MEMORY_CLASSES.map(c => ({ value: c, label: CLASS()[c] }))]} />
      <span class="grow"></span>
      <div class="mi-status"><${ListPick} label=${t('memory.status')} value=${status} onChange=${setStatus} options=${STATUSES.map(s => ({ value: s, label: STATUS()[s] }))} /></div>
      <label class="search"><${Icon} n="search" /><input placeholder=${t('memory.search')} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <button class="btn primary" onClick=${() => setForm({})}><${Icon} n="plus" />${t('memory.add')}</button>
    </div>
    <p class="note mi-note">${t('memory.intro')}</p>
    ${data === null ? html`<div class="skeleton" style="height:180px"></div>`
      : data.error ? html`<div class="card pad"><p class="note err">${data.error}</p></div>`
      : !groups.length ? html`<div class="card"><${Empty} icon="brain" title=${t('memory.empty')} text=${t('memory.empty_note')}><button class="btn" onClick=${() => setForm({})}>${t('memory.add')}</button></${Empty}></div>`
      : groups.map(([c, list]) => html`<section class="set-group" key=${c}><h3>${CLASS()[c]} <span class="count">${list.length}</span><${Tip} text=${CLASS_NOTE()[c]} /></h3>
          <div class="card">${list.map(it => html`<div class=${cls('mi-item', it.status !== 'active' && 'dim')} key=${it.id}>
            <div class="grow"><div class="mi-text">${it.text}</div>
              <div class="mi-meta"><span>${scopeLabel(it.scope, projects)}</span><span>${prov(it)}</span><span>${ago(it.updated_at || it.created_at)}</span>
                ${it.status !== 'active' && html`<span class="tag">${STATUS()[it.status]}</span>`}${(it.tags || []).map(tag => html`<span class="tag" key=${tag}>${tag}</span>`)}</div></div>
            ${it.status !== 'expired' && it.status !== 'superseded' && html`<button class="btn sm ghost" onClick=${() => setForm(it)}>${t('memory.edit_short')}</button>
              <button class="icon-btn" aria-label=${t('memory.forget')} title=${t('memory.forget')} onClick=${() => forget(it)}><${Icon} n="trash" /></button>`}
          </div>`)}</div></section>`)}
    ${data && data.malformed > 0 && html`<p class="note">${t('memory.malformed', { n: data.malformed })}</p>`}
    ${form && html`<${ItemForm} item=${form.id ? form : null} projects=${projects} onClose=${() => setForm(null)} onSaved=${() => { setForm(null); load(); }} />`}
  </div>`;
}
