import { t, tSource } from '../../core/i18n.js';
// vLLM : second moteur, installé par Loom dans son propre environnement Python
// et lancé ici ; Loom l'utilise alors comme moteur (lien direct local).
// Bibliothèque : le cache Hugging Face de cette machine, la recherche sur le
// Hub avec une estimation de VRAM, et des réglages enregistrés par modèle.
import { html, useState, useEffect, useStore, cls, fmtBytes } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip, Empty, Menu } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshEngineNode, refreshLibrary, refreshStatus } from '../../core/state.js';
import { Line, Group } from './kit.js';

const VERDICT = () => ({ fits: ['green', t('vllm.verdict.fits')], tensor_parallel: ['amber', t('vllm.verdict.tp')], no: ['red', t('vllm.verdict.no')], unknown: ['', t('vllm.verdict.unknown')] });
export const ownsVllm = (node, status) => node?.direct && node.kind === 'vllm'
  && /^http:\/\/127\.0\.0\.1:/.test(node.url || '') && status?.running && node.model === status.model;

// Réglages enregistrés pour un modèle : le catalogue vient du serveur, les
// valeurs vides laissent vLLM choisir.
function Params({ model, onClose, onStart }) {
  const [d, setD] = useState(null);
  const [v, setV] = useState({});
  const [more, setMore] = useState(false);
  useEffect(() => { get('/api/engines/vllm/params?model=' + encodeURIComponent(model)).then(r => { setD(r); setV(r.values || {}); }).catch(e => toast(e.message, 'err')); }, [model]);
  const save = async start => {
    const r = await post('/api/engines/vllm/params', { model, values: v }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t('settings.page.action_impossible'), 'err');
    toast(t('vllm.params.saved'));
    onClose();
    if (start) onStart(model);
  };
  const set = (k, x) => setV({ ...v, [k]: x });
  const control = p => {
    const val = v[p.key] == null ? '' : String(v[p.key]);
    if (p.kind === 'bool') return html`<${Switch} label=${tSource(p.label)} checked=${/^(on|true)$/i.test(val || p.default || '')} onChange=${on => set(p.key, on ? 'on' : 'off')} />`;
    if (p.kind === 'enum') return html`<select class="select sm" value=${val} onChange=${e => set(p.key, e.target.value)}><option value="">${t('inspector.params.defaut_2')}</option>${(p.choices || []).map(([c, l]) => html`<option value=${c}>${l}</option>`)}</select>`;
    if (p.kind === 'number') return html`<input class="input sm num" type="number" step=${p.step || 'any'} min=${p.min} max=${p.max} value=${val} placeholder=${p.default || t('settings.page.auto')} onChange=${e => set(p.key, e.target.value)} />`;
    return html`<input class="input sm mono" value=${val} placeholder=${p.default || 'auto'} onChange=${e => set(p.key, e.target.value)} />`;
  };
  const row = p => html`<div class=${cls('prow', p.dangerous && 'danger')} key=${p.id}><div class="prow-l"><span>${tSource(p.label)}</span>${p.tip && html`<${Tip} text=${tSource(p.tip)} />`}${p.dangerous && html`<span class="tag red">${t('vllm.params.danger')}</span>`}</div><div class="prow-c">${control(p)}</div></div>`;
  const specs = d ? (d.params || []).filter(p => p.available !== false) : [];
  return html`<${Modal} wide title=${t('vllm.params.title')} sub=${model} onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${() => setV({})}>${t('vllm.params.reset')}</button><span class="grow"></span><button class="btn" onClick=${() => save(false)}>${t('vllm.params.save')}</button><button class="btn primary" onClick=${() => save(true)}>${t('vllm.params.save_start')}</button>`}>
    ${!d ? html`<div class="skeleton" style="height:160px"></div>` : html`
      <div class="vl-params">${specs.filter(p => p.tier === 'essential').map(row)}</div>
      <button class="btn sm ghost" onClick=${() => setMore(!more)}><${Icon} n=${more ? 'chevron' : 'right'} />${t('vllm.params.advanced')}</button>
      ${more && html`<div class="vl-params">${specs.filter(p => p.tier !== 'essential').map(row)}</div>`}
      <p class="note">${t('vllm.params.note')}</p>`}
  </${Modal}>`;
}

function Search({ onDownload, busy, enginePicker, initialQuery = '' }) {
  const [q, setQ] = useState(initialQuery);
  const [quant, setQuant] = useState('');
  const [res, setRes] = useState(null);
  const [loading, setLoading] = useState(false);
  const search = async (query, quantization, signal) => {
    if (!query.trim()) return;
    setLoading(true);
    const r = await get('/api/engines/vllm/hub/search?q=' + encodeURIComponent(query.trim()) + (quantization ? '&quantization=' + quantization : ''), { signal })
      .catch(e => ({ error: e.message }));
    if (signal?.aborted) return;
    setLoading(false);
    if (r.error) return toast(r.error, 'err');
    setRes(r.models || []);
  };
  useEffect(() => {
    if (!initialQuery) return;
    const controller = new AbortController();
    search(initialQuery, '', controller.signal);
    return () => controller.abort();
  }, [initialQuery]);
  const verdicts = VERDICT();
  const label = t('vllm.search.placeholder');
  return html`
    <form class="models-toolbar" onSubmit=${e => { e.preventDefault(); search(q, quant); }}><label class="search"><${Icon} n="search" /><input aria-label=${label} placeholder=${label} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      ${enginePicker}
      <select class="select" aria-label=${t('local.hub.quantisations')} value=${quant} onChange=${e => setQuant(e.target.value)}><option value="">${t('vllm.search.any')}</option><option value="awq">AWQ</option><option value="gptq">GPTQ</option><option value="fp8">FP8</option></select>
      <button class="btn" type="submit" disabled=${loading}>${loading ? html`<span class="spinner"></span>` : t('resources.brain.chercher')}</button></form>
    ${res && (res.length ? html`<div class="models-card">${res.slice(0, 20).map(m => {
      const [tone, verdict] = verdicts[m.verdict] || verdicts.unknown;
      const details = [m.estimated_vram_mb && t('vllm.search.vram', { size: fmtBytes(m.estimated_vram_mb * 1048576) }), m.quantization?.toUpperCase(), verdict];
      return html`<div class="models-row" key=${m.id}>
        <div class="models-row-main"><b>${m.id}</b><small class=${cls((tone === 'amber' || tone === 'red') && 'models-warning')}>${details.filter(Boolean).join(' · ')}</small></div>
        <button class="btn sm" disabled=${busy} onClick=${() => onDownload(m.id)}>${t('vllm.search.download')}</button>
      </div>`;
    })}</div>` : html`<div class="models-card"><${Empty} icon="search" title=${t('vllm.search.none')} /></div>`)}
    <p class="models-row-meta">${t('vllm.search.estimate')}</p>`;
}

// État vLLM partagé par Local (bibliothèque, Hub, moteur) et les Réglages
// (installation, mises à jour). Un seul module, plusieurs vues.
const vstore = { source: '', revision: 0, x: null, lib: null, cache: '', dl: null, subs: new Set(), timer: null };
const vemit = () => vstore.subs.forEach(f => f());
async function vread(path, update) {
  const revision = vstore.revision;
  try {
    const data = await get('/api/engines/vllm' + path);
    if (revision === vstore.revision) { update(data); vemit(); }
  } catch (_) {}
}
const vload = () => vread('', data => { vstore.x = data; });
const vloadLib = () => vread('/models', data => { vstore.lib = data.models || []; vstore.cache = data.cache || ''; });
const vloadDl = () => vread('/download', data => { vstore.dl = data.download; });
const dlActive = dl => dl && !dl.finished && !dl.canceled && !dl.error;
function vtick() {
  if (vstore.timer) return;
  const revision = vstore.revision;
  vstore.timer = setInterval(async () => {
    const wasDl = dlActive(vstore.dl), wasJob = vstore.x && vstore.x.job;
    if (wasJob) await vload();
    if (revision !== vstore.revision) return;
    if (wasDl) await vloadDl();
    if (revision !== vstore.revision) return;
    if (wasDl && vstore.dl?.finished) vloadLib();
    if (wasJob && vstore.x?.installed && vstore.lib === null) vloadLib();
    if (vstore.x && !vstore.x.job && vstore.x.running && wasJob) { refreshEngineNode(); refreshStatus(); }
    if (!(vstore.x && vstore.x.job) && !dlActive(vstore.dl)) { clearInterval(vstore.timer); vstore.timer = null; }
  }, 1800);
}
export function useVllm() {
  const source = useStore(app, s => s.engineNode?.direct ? 'local' : s.engineNode?.url || 'local');
  const [, bump] = useState(0);
  useEffect(() => {
    const f = () => bump(n => n + 1);
    vstore.subs.add(f);
    if (vstore.source !== source) {
      clearInterval(vstore.timer);
      Object.assign(vstore, { source, revision: vstore.revision + 1, x: null, lib: null, cache: '', dl: null, timer: null });
    }
    const revision = vstore.revision;
    if (!vstore.x) vload().then(() => { if (revision === vstore.revision && vstore.x?.installed) vloadLib(); });
    vloadDl().then(() => { if (revision === vstore.revision) vtick(); });
    return () => vstore.subs.delete(f);
  }, [source]);
  const act = async body => {
    const r = await post('/api/engines/vllm', body).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) { toast(r.error || t('settings.page.action_impossible'), 'err'); return false; }
    await vload();
    if (body.action === 'stop') {
      await refreshEngineNode();
      refreshLibrary();
      refreshStatus();
    }
    vtick();
    return true;
  };
  const download = async model => {
    const r = await post('/api/engines/vllm/download', { model }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t('local.hub.telechargement_impossible'), 'err');
    toast(t('vllm.lib.download_started', { model })); await vloadDl(); vtick();
  };
  const remove = async m => {
    if (!await confirm(t('vllm.lib.delete_title', { model: m.id }), t('vllm.lib.delete_text'), { ok: t('vllm.lib.delete'), danger: true })) return;
    const r = await post('/api/engines/vllm/models/delete', { model: m.id }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error, 'err');
    vloadLib();
  };
  const data = vstore.source === source ? vstore : { x: null, lib: null, cache: '', dl: null };
  return { ...data, act, start: model => act({ action: 'start', model }), download, remove, reload: vload, reloadDl: vloadDl };
}

function DownloadRow({ v }) {
  const dl = v.dl;
  if (!dl || !(dlActive(dl) || dl.error)) return null;
  return html`<div class="models-card models-row"><div class="models-row-main"><b>${dl.model}</b>
      ${dl.error ? html`<small class="models-warning">${dl.error}</small>` : html`<small>${t('vllm.lib.downloading', { done: fmtBytes(dl.done), total: dl.total ? fmtBytes(dl.total) : '—' })}</small><div class="gauge"><i style=${{ width: (dl.total ? Math.min(100, Math.round(dl.done / dl.total * 100)) : 0) + '%' }}></i></div>`}</div>
    ${dlActive(dl) && html`<button class="btn sm ghost" onClick=${async () => { await post('/api/engines/vllm/download/cancel', { model: dl.model }); v.reloadDl(); }}>${t('settings.page.annuler')}</button>`}</div>`;
}

// Local › Bibliothèque (vLLM) : modèles Hugging Face présents sur la machine.
export function VllmLibrary({ v, onHub, enginePicker }) {
  const [params, setParams] = useState(null);
  const [q, setQ] = useState('');
  const [menu, setMenu] = useState(null);
  const x = v.x;
  const idle = x.installed && !x.running && !x.job;
  const matches = (v.lib || []).filter(m => m.id?.toLowerCase().includes(q.trim().toLowerCase()));
  const servable = matches.filter(m => m.servable), others = matches.filter(m => !m.servable);
  const loading = x.job === 'start';
  const remove = m => v.remove(m);
  const openMenu = (anchor, m) => setMenu({ anchor, items: [{ label: t('vllm.lib.delete'), icon: 'trash', danger: true, run: () => remove(m) }] });
  const filter = t('models.library.filter_models');
  return html`
    <section class="models-card models-status" aria-label=${t('local.page.moteur')}>
      <i class=${cls('dot', loading ? 'amber' : x.running ? 'green' : '')} aria-hidden="true"></i>
      <div class="models-row-main"><b>${loading ? t('models.library.loading', { name: x.model }) : x.running && x.model ? t('models.library.loaded', { name: x.model }) : t('vllm.eng.stopped')}</b>
        <small>${['vLLM', x.version].filter(Boolean).join(' · ')}</small></div>
      ${(loading || x.running) && html`<button class="btn" onClick=${() => v.act({ action: 'stop' })}>${t('local.page.arreter')}</button>`}
    </section>
    <div class="models-toolbar"><label class="search"><${Icon} n="search" /><input aria-label=${filter} placeholder=${filter} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      ${enginePicker}<a class="btn primary" href="#/local/hub">${t('local.page.telecharger_un_modele')}</a>
    </div>
    <${DownloadRow} v=${v} />
    <section class="models-group">
      <div class="models-section-head"><h2>${t('local.page.modeles')}</h2></div>
      ${v.lib === null ? html`<div class="skeleton models-loading"></div>` : html`<div class="models-card">
        ${servable.length ? servable.map(m => {
          const on = x.model === m.id && (x.running || loading);
          return html`<div class="models-row" key=${m.id}>
            <div class="models-row-main"><b>${m.id}</b><small>${fmtBytes(m.size)}</small></div>
            <div class="models-actions">
              ${on ? html`<span class=${cls('models-row-state', loading ? 'models-warning' : 'models-good')}>${loading ? t('local.page.chargement') : t('local.page.charge')}</span>`
                : html`<button class="btn sm" disabled=${!idle} onClick=${() => v.start(m.id)}>${t('local.page.charger')}</button>`}
              <button class="btn sm ghost" onClick=${() => setParams(m.id)}>${t('local.page.regler')}</button>
              <button class="icon-btn" aria-label=${t('models.row.actions', { name: m.id })} disabled=${on} onClick=${e => openMenu(e.currentTarget, m)}><${Icon} n="more" /></button>
            </div>
          </div>`;
        }) : html`<${Empty} icon="download" title=${q ? t('local.page.aucun_resultat') : t('vllm.lib.empty_title')} text=${q ? t('local.page.essaie_un_autre_filtre') : t('vllm.lib.empty')}>
          ${!q && html`<button class="btn" onClick=${onHub}>${t('vllm.lib.add')}</button>`}
        </${Empty}>`}
      </div>`}
    </section>
    ${others.length > 0 && html`<details class="models-extra models-card"><summary>${t('vllm.lib.others', { n: others.length })}</summary>
      ${others.map(m => html`<div class="models-row" key=${m.id}>
        <div class="models-row-main"><b>${m.id}</b><small>${fmtBytes(m.size)} · ${t('vllm.lib.not_servable')}</small></div>
        <button class="icon-btn" aria-label=${t('vllm.lib.delete')} onClick=${() => remove(m)}><${Icon} n="trash" /></button>
      </div>`)}
    </details>`}
    ${v.cache && html`<p class="models-row-meta">${t('vllm.lib.cache')} <code class="mono">${v.cache.replace(/^\/home\/[^/]+/, '~')}</code></p>`}
    ${menu && html`<${Menu} anchor=${menu.anchor} items=${menu.items} onClose=${() => setMenu(null)} />`}
    ${params && html`<${Params} model=${params} onClose=${() => setParams(null)} onStart=${v.start} />`}`;
}

// Local › Hub (vLLM) : recherche sur Hugging Face avec estimation de VRAM.
export function VllmHub({ v, enginePicker, initialQuery }) {
  return html`<${Search} busy=${dlActive(v.dl)} onDownload=${v.download} enginePicker=${enginePicker} initialQuery=${initialQuery} /><${DownloadRow} v=${v} />`;
}

// Local › Moteur (vLLM) : ce qui est servi, arrêt, journal.
export function VllmEngine({ v }) {
  const x = v.x;
  if (!x) return html`<div class="skeleton" style="height:120px"></div>`;
  return html`<section class="models-group">
    <div class="models-section-head"><h2>vLLM</h2></div>
    <div class="models-card">
      <div class="models-row">
        <i class=${cls('dot', x.job ? 'amber' : x.running && 'green')} aria-hidden="true"></i>
        <div class="models-row-main"><b>${x.job === 'start' ? t('vllm.eng.loading', { model: x.model }) : x.running ? t('vllm.eng.serving') : t('vllm.eng.stopped')}</b>
          <small>${[x.model, x.version].filter(Boolean).join(' · ')}</small></div>
        ${(x.running || x.job === 'start') && html`<button class="btn sm" onClick=${() => v.act({ action: 'stop' })}>${t('settings.page.arreter')}</button>`}
      </div>
      ${x.log && html`<details class="models-extra"><summary>${t('settings.page.journal_vllm')}</summary><pre class="log mono vl-log">${x.log.split('\n').slice(-120).join('\n')}</pre></details>`}
    </div>
    ${(x.error || x.auto_update?.last_error) && html`<div class="alert red"><${Icon} n="alert" /><span>${x.error || x.auto_update?.last_error}</span></div>`}
    ${!x.running && !x.job && html`<p class="note">${t('vllm.eng.hint')}</p>`}
  </section>`;
}

// Réglages › Moteurs : installation, version et mises à jour de vLLM. Les
// modèles se gèrent dans Local.
export function VLLMEngine() {
  const v = useVllm();
  const x = v.x;
  if (!x) return null;
  const idle = x.installed && !x.running && !x.job;
  const toggleAuto = async on => { const r = await post('/api/engines/vllm/auto-update', { auto: on }); if (!r.ok) return toast(r.error || t('settings.page.reglage_impossible'), 'err'); v.reload(); };
  const install = async () => { if (await confirm(t('vllm.install_title'), t('settings.page.loom_cree_un_environnement_python_dans') + x.dir + t('settings.page.et_y_installe_vllm_plusieurs_go_quelques_minutes_rien_n_est_insta'), { ok: t('settings.page.installer') })) v.act({ action: 'install' }); };
  return html`<${Group} title="${t('settings.page.vllm')}">
    <${Line} label="${t('settings.page.etat')}" tip="${t('settings.page.vllm_sert_des_modeles_hugging_face_pas_les_gguf_avec_beaucoup_de')}">
      ${x.job === 'install' || x.job === 'update' ? html`<span class="state"><span class="spinner"></span>${x.job === 'update' ? t('vllm.updating') : t('settings.page.installation_plusieurs_minutes')}</span>`
        : x.running || x.job === 'start' ? html`<span class="state"><i class="dot green"></i>${t('settings.page.sert')} <b class="mono">${x.model}</b></span>`
        : x.installed ? html`<span class="state">${t('settings.page.installe_arrete')}${x.version ? ' · ' + x.version : ''}</span>`
        : x.missing ? html`<span class="state err">${x.missing}</span>`
        : html`<span class="state">${t('settings.page.non_installe')}</span><button class="btn sm" onClick=${install}>${t('settings.page.installer_vllm')}</button>`}
      ${x.installed && html`<a class="btn sm ghost" href="#/local" onClick=${() => { try { localStorage.setItem('loom.local.engine', 'vllm'); } catch (_) {} }}>${t('vllm.settings.models')}</a>`}</${Line}>
    ${x.installed && html`<${Line} label=${t('settings.page.mise_a_jour_automatique')} tip=${t('vllm.auto_tip')}>
      ${idle && html`<button class="btn sm ghost" onClick=${() => v.act({ action: 'update' })}><${Icon} n="refresh" />${t('settings.page.mettre_a_jour')}</button>`}
      <${Switch} label=${t('settings.page.mise_a_jour_automatique')} checked=${!!(x.auto_update && x.auto_update.auto)} onChange=${toggleAuto} /></${Line}>`}
    ${x.error && html`<${Line} label="${t('settings.page.derniere_erreur')}"><span class="state err">${x.error}</span></${Line}>`}
    ${x.job === 'install' || x.job === 'update' ? html`<details class="lc-log" open><summary>${t('settings.page.journal_vllm')}</summary><pre>${(x.log || '').split('\n').slice(-60).join('\n')}</pre></details>` : ''}
  </${Group}>`;
}
