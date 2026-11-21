// Observation du moteur et réglages de son service d'inférence.
import { getLang, t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { app, refreshStatus } from '../../core/state.js';
import { visibleRefresh } from '../../core/poll.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Switch, Menu } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { copyText } from '../../ui/clipboard.js';
import { get, post } from '../../core/api.js';

const fmtN = n => n >= 1e6 ? (n / 1e6).toFixed(1) + ' M' : n >= 1e3 ? (n / 1e3).toFixed(1) + ' k' : String(n);
const ago = ms => {
  if (!ms) return t('engine.svc.never');
  const m = Math.round((Date.now() - ms) / 60000);
  return m < 1 ? t('resources.brain.a_l_instant') : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) });
};
// Go's zero time (0001-01-01) means never.
const msOf = v => {
  const ms = typeof v === 'number' ? (v < 1e12 ? v * 1000 : v) : Date.parse(v || '') || 0;
  return ms > 0 ? ms : 0;
};
const clockTime = ms => new Date(ms).toLocaleTimeString(getLang(), { hour: '2-digit', minute: '2-digit' });

export function EngineSlots({ server }) {
  const [draft, setDraft] = useState(null);
  const [busy, setBusy] = useState(false);
  const slots = server?.slots?.items || [];
  const current = String(server?.np || '');
  const value = draft ?? current;
  const save = async () => {
    setBusy(true);
    const r = await post('/api/server', { np: +value });
    setBusy(false);
    if (!r.ok) return toast(r.error, 'err');
    toast(t('models.engine.slots_applying', { n: value }));
    setTimeout(refreshStatus, 600);
  };
  useEffect(() => { if (draft === current) setDraft(null); }, [current, draft]);
  const label = t('local.page.slots_paralleles');
  return html`
    <div class="models-row">
      <div class="models-row-main"><span>${label}</span><small>${t('models.engine.slots_hint')}</small>
        ${slots[0]?.ctx > 0 && html`<small>${Math.round(slots[0].ctx / 1024)}${t('local.page.k_de_contexte_par_slot')}</small>`}
      </div>
      ${server?.np_supported === false ? html`<a class="btn sm" href="#/local">${t('node.vllm_parameters')}</a>`
        : server && html`<div class="models-actions">
          <${ListPick} class="models-slots-picker" value=${value} label=${label} disabled=${busy} onChange=${setDraft}
            options=${Array.from({ length: 32 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }))} />
          ${draft !== null && html`<button class="btn sm" disabled=${busy} onClick=${save}>${t('local.page.appliquer')}</button>`}
        </div>`}
    </div>
    ${slots.some(s => s.busy) && html`<div class="models-row"><div class="slots models-row-main">${slots.map(s => {
      const percent = s.busy && s.ctx > 0 ? Math.min(100, ((s.prompt || 0) + (s.tokens || 0)) * 100 / s.ctx) : 0;
      return html`<div class=${cls('slot', s.busy && 'busy')} key=${s.id}>
        <span class="mono">#${s.id}</span><div class="gauge"><i style=${{ width: percent + '%' }}></i></div>
        <span class="mono">${s.busy ? Number.isFinite(s.toks) ? s.toks.toFixed(1) + ' ' + t('local.page.tok_s') : '—' : t('local.page.free')}</span>
      </div>`;
    })}</div></div>`}`;
}

export function EngineRecent({ server }) {
  const recent = Array.isArray(server?.recent) ? server.recent.slice(-12).reverse() : [];
  return html`<details class="models-extra models-card">
    <summary>${t('local.page.requetes_recentes')}</summary>
    <div class="models-card">${recent.length ? recent.map(r => {
      const tokens = Number.isFinite(r.prompt) && Number.isFinite(r.tokens) ? r.prompt + r.tokens : null;
      const when = msOf(r.ended);
      return html`<div class="models-row models-recent-row" key=${[r.slot, r.task, r.ended].join('/')}>
        <span class="models-row-main">${t('models.engine.slot_number', { n: r.slot })}</span>
        <span class="models-row-meta" title=${tokens === null ? undefined : r.prompt + ' → ' + r.tokens}>
          ${[tokens === null ? '' : t('models.engine.tokens', { n: tokens.toLocaleString(getLang()) }), when && clockTime(when)].filter(Boolean).join(' · ')}
        </span>
      </div>`;
    }) : html`<div class="models-row models-recent-row models-empty">${server ? t('models.engine.no_requests') : t('core.state.lecture_de_l_etat')}</div>`}</div>
  </details>`;
}

export function EngineUnavailable({ what, error }) {
  const node = useStore(app, a => a.engineNode);
  const outdated = error && (error.error_code === 'engine_outdated' || /older|404|not found/i.test(error.error || ''));
  return html`<div class="card pad eng-na"><b>${what}</b><p class="note">${outdated
    ? (node ? t('engine.na.outdated_remote', { machine: node.hostname || '' }) : t('engine.na.outdated_local'))
    : t('engine.na.unreachable')}</p>${outdated && node && html`<a class="btn sm" href="#/machines">${t('engine.na.open_machines')}</a>`}</div>`;
}

const pickOptions = (values, current, label) => {
  const choices = Number.isInteger(current) && !values.includes(current) ? [...values, current] : values;
  return choices.map(value => ({ value: String(value), label: label(value) }));
};

export function EngineMemory({ children, local }) {
  const [settings, setSettings] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  // A poll that began before a write cannot overwrite its result.
  const revision = useRef(0);
  const grace = useRef(15);
  useEffect(() => visibleRefresh(async alive => {
    const at = revision.current;
    if (at % 2) return;
    try {
      const result = await get('/api/engine/service', { timeout: 6000 });
      if (alive() && at === revision.current) { setSettings(result); setError(null); }
    } catch (e) {
      if (alive() && at === revision.current) setError({ error: e.message });
    }
  }, 5000), []);
  const save = async patch => {
    if (revision.current % 2) return;
    revision.current++;
    setSaving(true);
    try {
      const r = await post('/api/engine/service', patch);
      if (r.ok === false || r.error) return toast(r.error, 'err');
      setSettings(r);
    } finally { revision.current++; setSaving(false); }
  };
  const minutes = n => t('engine.svc.minutes', { n });
  const protect = settings?.interactive_grace_minutes > 0;
  const setProtection = on => {
    if (!on) grace.current = settings.interactive_grace_minutes;
    save({ interactive_grace_minutes: on ? grace.current : 0 });
  };
  const resident = Array.isArray(settings?.resident) ? settings.resident : [];
  const queue = Array.isArray(settings?.queue) ? settings.queue : [];
  const unloadAt = msOf(settings?.idle_unload_at);
  return html`<section class="models-group">
    <div class="models-section-head"><h2>${t('models.engine.performance')}</h2>${local && html`<small>${t('models.engine.local_settings')}</small>`}</div>
    <div class="models-card">
      ${error ? html`<${EngineUnavailable} what=${t('engine.svc.memory')} error=${error} />` : settings && html`
        <div class="models-row">
          <div class="models-row-main"><span>${t('models.engine.idle')} <${Tip} text=${t('engine.svc.idle_tip')} /></span>
            ${unloadAt && resident.length > 0 ? html`<small>${t('models.engine.unload_at', { when: clockTime(unloadAt) })}</small>` : ''}
          </div>
          <div class="models-choice"><${ListPick} label=${t('models.engine.idle')} value=${String(settings.idle_unload_minutes)} disabled=${saving}
            options=${pickOptions([5, 15, 30, 60, 120, 0], settings.idle_unload_minutes, n => n === 0 ? t('engine.svc.never_unload') : minutes(n))}
            onChange=${v => save({ idle_unload_minutes: +v })} /></div>
        </div>`}
      ${settings && !error && html`<div class="models-row">
        <span class="models-row-main">${t('engine.svc.resident')}</span>
        <div class="models-actions models-resident">
          ${resident.length ? resident.map(r => html`<span class="tag" key=${r.model}>${r.model}${r.in_flight ? ' · ' + t('engine.svc.in_flight', { n: r.in_flight }) : ''}</span>`)
            : html`<span class="muted">${t('engine.svc.none_loaded')}</span>`}
          ${queue.map(q => html`<span class="tag amber" key=${q.model}>${t('engine.svc.waiting', { model: q.model, n: q.waiting })}</span>`)}
        </div>
      </div>`}
      <details class="models-extra models-tuning">
        <summary>${t('models.engine.tuning')}</summary>
        ${settings && !error && html`
          <div class="models-row">
            <div class="models-row-main"><span>${t('engine.svc.models_max')} <${Tip} text=${t('engine.svc.models_max_tip')} /></span></div>
            <div class="models-choice"><${ListPick} label=${t('engine.svc.models_max')} value=${String(settings.models_max)} disabled=${saving}
              options=${pickOptions([1, 2, 3, 4], settings.models_max, n => n === 0 ? t('models.engine.unlimited') : n === 1 ? t('models.engine.one_model') : t('engine.svc.n_models', { n }))}
              onChange=${v => save({ models_max: +v })} /></div>
          </div>
          <div class="models-row">
            <div class="models-row-main"><span>${t('models.engine.protect')} <${Tip} text=${t('models.engine.protect_delay')} /></span><small>${t('models.engine.protect_hint')}</small></div>
            <div class="models-actions">
              ${protect && html`<div class="models-choice"><${ListPick} label=${t('engine.svc.grace')} value=${String(settings.interactive_grace_minutes)} disabled=${saving}
                options=${pickOptions([5, 15, 30, 60], settings.interactive_grace_minutes, minutes)}
                onChange=${v => save({ interactive_grace_minutes: +v })} /></div>`}
              <${Switch} checked=${protect} label=${t('models.engine.protect')} disabled=${saving} onChange=${setProtection} />
            </div>
          </div>`}
        ${children}
      </details>
    </div>
  </section>`;
}

function KeySecret({ secret, onClose }) {
  return html`<${Modal} title=${t('engine.keys.created')} sub=${t('engine.keys.once')} onClose=${onClose}
    foot=${html`<button class="btn primary" onClick=${onClose}>${t('engine.keys.done')}</button>`}>
    <div class="es-secret"><code class="mono">${secret}</code><button class="btn sm" onClick=${async () => toast(await copyText(secret) ? t('local.page.copie') : t('settings.page.copie_refusee'))}>${t('local.page.copier')}</button></div>
  </${Modal}>`;
}

function KeyForm({ k, models, onClose }) {
  const [v, setV] = useState({
    name: k?.name || '', allowed_models: Array.isArray(k?.allowed_models) ? k.allowed_models : [],
    max_concurrency: k?.max_concurrency || 0, requests_per_minute: k?.requests_per_minute || 0, priority: k?.priority || 'interactive',
  });
  const [secret, setSecret] = useState('');
  const [busy, setBusy] = useState(false);
  const set = p => setV({ ...v, ...p });
  const save = async () => {
    setBusy(true);
    const r = await post(k ? '/api/engine/keys/update' : '/api/engine/keys', k ? { id: k.id, ...v } : v);
    setBusy(false);
    if (r.ok === false || r.error) return toast(r.error, 'err');
    if (r.secret) setSecret(r.secret); else onClose();
  };
  if (secret) return html`<${KeySecret} secret=${secret} onClose=${onClose} />`;
  const choices = [...new Set([...models, ...v.allowed_models])];
  const toggleModel = m => set({ allowed_models: v.allowed_models.includes(m) ? v.allowed_models.filter(x => x !== m) : [...v.allowed_models, m] });
  return html`<${Modal} title=${k ? t('engine.keys.edit') : t('engine.keys.new')} onClose=${busy ? undefined : onClose}
      foot=${html`<button class="btn" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !v.name.trim()} onClick=${save}>${k ? t('ui.dialog.enregistrer') : t('engine.keys.create')}</button>`}>
    <div class="ws-form">
      <label class="field"><span>${t('engine.keys.name')}</span><input class="input" value=${v.name} placeholder=${t('engine.keys.name_ph')} onInput=${e => set({ name: e.target.value })} /></label>
      <label class="field"><span>${t('engine.keys.priority')}<${Tip} text=${t('engine.keys.priority_tip')} /></span><${ListPick} label=${t('engine.keys.priority')} value=${v.priority} onChange=${priority => set({ priority })} options=${[{ value: 'interactive', label: t('engine.keys.interactive') }, { value: 'background', label: t('engine.keys.background') }]} /></label>
      <div class="es-two">
        <label class="field"><span>${t('engine.keys.concurrency')}</span><input class="input num" type="number" min="0" max="64" value=${v.max_concurrency} onInput=${e => set({ max_concurrency: +e.target.value || 0 })} /><small>${t('engine.keys.zero')}</small></label>
        <label class="field"><span>${t('engine.keys.rpm')}</span><input class="input num" type="number" min="0" max="10000" value=${v.requests_per_minute} onInput=${e => set({ requests_per_minute: +e.target.value || 0 })} /><small>${t('engine.keys.zero')}</small></label>
      </div>
      ${choices.length > 0 && html`<div class="field"><span>${t('engine.keys.models')}</span><div class="chips">${choices.map(m => html`<button type="button" key=${m} class=${cls('btn sm', v.allowed_models.includes(m) && 'on')} aria-pressed=${String(v.allowed_models.includes(m))} onClick=${() => toggleModel(m)}>${m}</button>`)}</div><small>${v.allowed_models.length ? t('engine.keys.models_some', { n: v.allowed_models.length }) : t('engine.keys.models_all')}</small></div>`}
    </div></${Modal}>`;
}

function KeyRow({ k, compact, onEdit, onChanged }) {
  const [anchor, setAnchor] = useState(null);
  const u = k.usage;
  const rotate = async () => {
    if (!await confirm(t('engine.keys.rotate'), t('engine.keys.rotate_text', { name: k.name }), { ok: t('engine.keys.rotate') })) return;
    const r = await post('/api/engine/keys/rotate', { id: k.id });
    if (r.ok === false || r.error) return toast(r.error, 'err');
    onEdit({ ...k, secret: r.secret });
  };
  const remove = async () => {
    if (!await confirm(t('engine.keys.delete'), t('engine.keys.delete_text', { name: k.name }), { ok: t('engine.keys.delete'), danger: true })) return;
    const r = await post('/api/engine/keys/delete', { id: k.id });
    if (r.ok === false || r.error) return toast(r.error, 'err');
    onChanged();
  };
  const name = k.id === 'default' ? t('engine.keys.default_name') : k.name;
  const legacy = k.id === 'default' && html` <span class="tag">${t('engine.keys.legacy')}</span>`;
  const hasUsage = Number.isFinite(u?.requests) && Number.isFinite(u.prompt_tokens) && Number.isFinite(u.completion_tokens);
  const summary = [
    k.priority === 'background' ? t('engine.keys.background') : t('engine.keys.interactive'),
    k.max_concurrency && t('engine.keys.conc_n', { n: k.max_concurrency }),
    k.requests_per_minute && t('engine.keys.rpm_n', { n: k.requests_per_minute }),
    k.allowed_models?.length ? t('engine.keys.models_some', { n: k.allowed_models.length }) : t('models.keys.all_models'),
    t('engine.keys.last', { when: ago(msOf(k.last_used_at)) }),
    hasUsage && t('engine.keys.usage', { req: fmtN(u.requests), tok: fmtN(u.prompt_tokens + u.completion_tokens) }),
  ].filter(Boolean);
  const more = html`<button class="icon-btn" aria-label=${t('models.row.actions', { name })} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button>`;
  return html`<div class=${compact ? 'models-row' : 'bs-row'}>
    ${compact ? html`<div class="models-row-main"><span>${name}${legacy}</span><small>${summary.join(' · ')}</small>
      ${u?.last_error && html`<small class="models-warning" title=${u.last_error}>${t('resources.brain.erreur')}</small>`}</div>
      <div class="models-actions"><button class="btn sm" onClick=${() => onEdit(k)}>${t('memory.edit_short')}</button>${more}</div>`
      : html`<span class="logo-mark sm"><${Icon} n="key" /></span>
        <div class="grow"><div class="bs-name">${name}${legacy}</div><div class="bs-sub">${summary.map(text => html`<span>${text}</span>`)}
          ${u?.last_error && html`<span class="bs-state err" title=${u.last_error}>${t('resources.brain.erreur')}</span>`}</div></div>${more}`}
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[
      ...(!compact ? [{ icon: 'edit', label: t('memory.edit_short'), run: () => onEdit(k) }] : []),
      { icon: 'refresh', label: t('engine.keys.rotate'), run: rotate }, '-', { icon: 'trash', label: t('engine.keys.delete'), danger: true, run: remove },
    ]} />`}
  </div>`;
}

export function EngineKeys({ compact = false, catalog = [], local = false }) {
  const [data, setData] = useState(null);
  const [models, setModels] = useState([]);
  const [form, setForm] = useState(null);
  const [error, setError] = useState(null);
  const load = () => get('/api/engine/keys').then(r => { setData(r); setError(null); }).catch(e => {
    setData(null); setError({ error: e.message });
  });
  useEffect(() => {
    load();
    get('/api/models').then(r => setModels(r.filter(m => !m.remote && !m.mmproj).map(m => m.name.replace(/\.gguf$/i, '')))).catch(() => {});
  }, []);
  if (!data) return error ? html`<${EngineUnavailable} what=${t('engine.keys.title')} error=${error} />` : null;
  const keys = Array.isArray(data.keys) ? data.keys : [];
  const create = html`<button class="btn sm" onClick=${() => setForm({})}>${t('engine.keys.new')}</button>`;
  const close = () => { setForm(null); load(); };
  return html`<section class=${compact ? 'models-group' : 'set-group'}>
    ${compact ? html`<div class="models-section-head spread"><h2>${t('engine.keys.title')} <${Tip} text=${t('engine.keys.tip')} /></h2>${create}</div>`
      : html`<h3>${t('engine.keys.title')}<${Tip} text=${t('engine.keys.tip')} /></h3>`}
    <div class=${compact ? 'models-card' : 'card bs-list'}>
      ${local && html`<div class="models-row"><div class="models-row-main"><span>${t('models.api.this_machine')}</span><small>${data.endpoint}</small></div></div>`}
      ${keys.length ? keys.map(k => html`<${KeyRow} key=${k.id} k=${k} compact=${compact} onEdit=${setForm} onChanged=${load} />`)
        : html`<div class=${compact ? 'models-row models-empty' : 'bs-row muted'}>${t('engine.keys.empty')}</div>`}
      ${!compact && html`<div class="bs-row"><span class="grow muted es-note">${data.endpoint ? html`${t('engine.keys.endpoint')} <code class="mono">${data.endpoint}</code>` : ''}</span>${create}</div>`}
    </div>
    ${form && (form.secret ? html`<${KeySecret} secret=${form.secret} onClose=${close} />`
      : html`<${KeyForm} k=${form.id ? form : null} models=${[...new Set([...catalog, ...models])]} onClose=${close} />`)}
  </section>`;
}
