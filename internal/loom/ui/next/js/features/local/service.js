// Le moteur comme service : qui l'utilise (clés d'API nommées, limites,
// consommation) et comment il gère la mémoire vidéo (déchargement après
// inactivité, file d'attente quand un autre modèle est demandé).
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Menu } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';

const fmtN = n => n >= 1e6 ? (n / 1e6).toFixed(1) + ' M' : n >= 1e3 ? (n / 1e3).toFixed(1) + ' k' : String(n || 0);
const ago = ms => {
  if (!ms) return t('engine.svc.never');
  const m = Math.round((Date.now() - ms) / 60000);
  return m < 1 ? t('resources.brain.a_l_instant') : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) });
};
// Go's zero time (0001-01-01) means never.
const msOf = v => { const ms = typeof v === 'number' ? (v < 1e12 ? v * 1000 : v) : Date.parse(v || '') || 0; return ms > 0 ? ms : 0; };

export function EngineMemory() {
  const [s, setS] = useState(null);
  const load = () => get('/api/engine/service').then(r => setS(r.ok === false ? null : r)).catch(() => setS(null));
  useEffect(() => { load(); const id = setInterval(load, 5000); return () => clearInterval(id); }, []);
  if (!s) return null;
  const save = async patch => {
    const r = await post('/api/engine/service', patch).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false || r.error) return toast(r.error, 'err');
    setS({ ...s, ...r });
  };
  const opt = (values, unit) => values.map(v => ({ value: String(v), label: v === 0 ? t('engine.svc.never_unload') : t(unit, { n: v }) }));
  const resident = s.resident || [], queue = s.queue || [];
  return html`<section class="set-group"><h3>${t('engine.svc.memory')}<${Tip} text=${t('engine.svc.memory_tip')} /></h3>
    <div class="card">
      <div class="set-line"><div class="set-l"><span>${t('engine.svc.resident')}</span></div>
        <div class="set-c es-res">${resident.length ? resident.map(r => html`<span class="tag"><i class=${'dot ' + (r.in_flight ? 'blue' : 'green')}></i>${r.model}${r.in_flight ? ' · ' + t('engine.svc.in_flight', { n: r.in_flight }) : ''}</span>`) : html`<span class="muted">${t('engine.svc.none_loaded')}</span>`}
          ${queue.map(q => html`<span class="tag amber">${t('engine.svc.waiting', { model: q.model, n: q.waiting })}</span>`)}</div></div>
      <div class="set-line"><div class="set-l"><span>${t('engine.svc.idle')}</span><${Tip} text=${t('engine.svc.idle_tip')} />${s.idle_unload_at && resident.length ? html`<span class="muted es-note">${t('engine.svc.idle_at', { when: new Date(msOf(s.idle_unload_at)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) })}</span>` : ''}</div>
        <div class="set-c es-pick"><${ListPick} label=${t('engine.svc.idle')} value=${String(s.idle_unload_minutes)} onChange=${v => save({ idle_unload_minutes: +v })} options=${opt([5, 15, 30, 60, 120, 0], 'engine.svc.minutes')} /></div></div>
      <div class="set-line"><div class="set-l"><span>${t('engine.svc.models_max')}</span><${Tip} text=${t('engine.svc.models_max_tip')} /></div>
        <div class="set-c es-pick"><${ListPick} label=${t('engine.svc.models_max')} value=${String(s.models_max)} onChange=${v => save({ models_max: +v })} options=${[1, 2, 3, 4].map(n => ({ value: String(n), label: n === 1 ? t('engine.svc.one_model') : t('engine.svc.n_models', { n }) }))} /></div></div>
      <div class="set-line"><div class="set-l"><span>${t('engine.svc.grace')}</span><${Tip} text=${t('engine.svc.grace_tip')} /></div>
        <div class="set-c es-pick"><${ListPick} label=${t('engine.svc.grace')} value=${String(s.interactive_grace_minutes)} onChange=${v => save({ interactive_grace_minutes: +v })} options=${[5, 15, 30, 60].map(n => ({ value: String(n), label: t('engine.svc.minutes', { n }) }))} /></div></div>
    </div>
  </section>`;
}

function KeyForm({ k, models, onClose }) {
  const [v, setV] = useState(k ? { name: k.name, allowed_models: k.allowed_models || [], max_concurrency: k.max_concurrency || 0, requests_per_minute: k.requests_per_minute || 0, priority: k.priority || 'interactive' } : { name: '', allowed_models: [], max_concurrency: 0, requests_per_minute: 0, priority: 'interactive' });
  const [secret, setSecret] = useState('');
  const [busy, setBusy] = useState(false);
  const set = p => setV({ ...v, ...p });
  const save = async () => {
    setBusy(true);
    const r = await post(k ? '/api/engine/keys/update' : '/api/engine/keys', k ? { id: k.id, ...v } : v).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false || r.error) return toast(r.error, 'err');
    if (r.secret) setSecret(r.secret); else onClose(true);
  };
  if (secret) return html`<${Modal} title=${t('engine.keys.created')} sub=${t('engine.keys.once')} onClose=${() => onClose(true)} foot=${html`<button class="btn primary" onClick=${() => onClose(true)}>${t('engine.keys.done')}</button>`}>
    <div class="es-secret"><code class="mono">${secret}</code><button class="btn sm" onClick=${() => navigator.clipboard && navigator.clipboard.writeText(secret).then(() => toast(t('local.page.copie')))}><${Icon} n="copy" />${t('local.page.copier')}</button></div></${Modal}>`;
  const toggleModel = m => set({ allowed_models: v.allowed_models.includes(m) ? v.allowed_models.filter(x => x !== m) : [...v.allowed_models, m] });
  return html`<${Modal} title=${k ? t('engine.keys.edit') : t('engine.keys.new')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`<button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !v.name.trim()} onClick=${save}>${k ? t('ui.dialog.enregistrer') : t('engine.keys.create')}</button>`}>
    <div class="ws-form">
      <label class="field"><span>${t('engine.keys.name')}</span><input class="input" value=${v.name} placeholder=${t('engine.keys.name_ph')} onInput=${e => set({ name: e.target.value })} /></label>
      <label class="field"><span>${t('engine.keys.priority')}<${Tip} text=${t('engine.keys.priority_tip')} /></span><${ListPick} label=${t('engine.keys.priority')} value=${v.priority} onChange=${priority => set({ priority })} options=${[{ value: 'interactive', label: t('engine.keys.interactive') }, { value: 'background', label: t('engine.keys.background') }]} /></label>
      <div class="es-two">
        <label class="field"><span>${t('engine.keys.concurrency')}</span><input class="input num" type="number" min="0" max="64" value=${v.max_concurrency} onInput=${e => set({ max_concurrency: +e.target.value || 0 })} /><small>${t('engine.keys.zero')}</small></label>
        <label class="field"><span>${t('engine.keys.rpm')}</span><input class="input num" type="number" min="0" max="10000" value=${v.requests_per_minute} onInput=${e => set({ requests_per_minute: +e.target.value || 0 })} /><small>${t('engine.keys.zero')}</small></label>
      </div>
      ${models.length > 0 && html`<div class="field"><span>${t('engine.keys.models')}</span><div class="chips">${models.map(m => html`<button type="button" class=${cls('chip-btn', v.allowed_models.includes(m) && 'on')} onClick=${() => toggleModel(m)}>${m}</button>`)}</div><small>${v.allowed_models.length ? t('engine.keys.models_some', { n: v.allowed_models.length }) : t('engine.keys.models_all')}</small></div>`}
    </div></${Modal}>`;
}

function KeyRow({ k, onEdit, onChanged }) {
  const [anchor, setAnchor] = useState(null);
  const u = k.usage || {};
  const rotate = async () => {
    if (!await confirm(t('engine.keys.rotate'), t('engine.keys.rotate_text', { name: k.name }), { ok: t('engine.keys.rotate') })) return;
    const r = await post('/api/engine/keys/rotate', { id: k.id });
    if (r.ok === false || r.error) return toast(r.error, 'err');
    onEdit({ ...k, secret: r.secret });
  };
  const del = async () => {
    if (!await confirm(t('engine.keys.delete'), t('engine.keys.delete_text', { name: k.name }), { ok: t('engine.keys.delete'), danger: true })) return;
    const r = await post('/api/engine/keys/delete', { id: k.id });
    if (r.ok === false || r.error) return toast(r.error, 'err');
    onChanged();
  };
  const limits = [k.priority === 'background' && t('engine.keys.background'), k.max_concurrency && t('engine.keys.conc_n', { n: k.max_concurrency }), k.requests_per_minute && t('engine.keys.rpm_n', { n: k.requests_per_minute }), (k.allowed_models || []).length && t('engine.keys.models_some', { n: k.allowed_models.length })].filter(Boolean);
  return html`<div class="bs-row">
    <span class="mono-tile"><${Icon} n="key" /></span>
    <div class="grow"><div class="bs-name">${k.id === 'default' ? t('engine.keys.default_name') : k.name}${k.id === 'default' && html`<span class="tag">${t('engine.keys.legacy')}</span>`}</div>
      <div class="bs-sub"><span>${t('engine.keys.usage', { req: fmtN(u.requests), tok: fmtN((u.prompt_tokens || 0) + (u.completion_tokens || 0)) })}</span><span>${t('engine.keys.last', { when: ago(msOf(k.last_used_at)) })}</span>${limits.map(l => html`<span>${l}</span>`)}${u.last_error && html`<span class="bs-state err" title=${u.last_error}>${t('resources.brain.erreur')}</span>`}</div></div>
    <button class="icon-btn" aria-label=${t('brain.src.actions')} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[{ icon: 'edit', label: t('memory.edit_short'), run: () => onEdit(k) }, { icon: 'refresh', label: t('engine.keys.rotate'), run: rotate }, '-', { icon: 'trash', label: t('engine.keys.delete'), danger: true, run: del }]} />`}
  </div>`;
}

export function EngineKeys() {
  const [d, setD] = useState(null);
  const [models, setModels] = useState([]);
  const [form, setForm] = useState(null);
  const load = () => get('/api/engine/keys').then(r => setD(r.ok === false ? null : r)).catch(() => setD(null));
  useEffect(() => { load(); get('/api/models').then(r => setModels((Array.isArray(r) ? r : []).map(m => String(m.name || m.file || m.id || '').replace(/\.gguf$/i, '')).filter(Boolean).slice(0, 40))).catch(() => {}); }, []);
  if (!d) return null;
  const keys = d.keys || [];
  return html`<section class="set-group"><h3>${t('engine.keys.title')}<${Tip} text=${t('engine.keys.tip')} /></h3>
    <div class="card bs-list">
      ${keys.length ? keys.map(k => html`<${KeyRow} key=${k.id} k=${k} onEdit=${setForm} onChanged=${load} />`) : html`<div class="bs-row"><span class="muted">${t('engine.keys.empty')}</span></div>`}
      <div class="bs-row"><span class="grow muted es-note">${d.endpoint ? html`${t('engine.keys.endpoint')} <code class="mono">${d.endpoint}</code>` : ''}</span><button class="btn sm" onClick=${() => setForm({})}><${Icon} n="plus" />${t('engine.keys.new')}</button></div>
    </div>
    ${form && (form.secret ? html`<${Modal} title=${t('engine.keys.created')} sub=${t('engine.keys.once')} onClose=${() => { setForm(null); load(); }} foot=${html`<button class="btn primary" onClick=${() => { setForm(null); load(); }}>${t('engine.keys.done')}</button>`}><div class="es-secret"><code class="mono">${form.secret}</code></div></${Modal}>`
      : html`<${KeyForm} k=${form.id ? form : null} models=${models} onClose=${() => { setForm(null); load(); }} />`)}
  </section>`;
}
