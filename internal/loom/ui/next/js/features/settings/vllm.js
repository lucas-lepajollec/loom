import { t, tSource } from '../../core/i18n.js';
// vLLM : second moteur, installé par Loom dans son propre environnement Python
// et lancé ici ; Loom l'utilise alors comme moteur (lien direct local).
// Bibliothèque : le cache Hugging Face de cette machine, la recherche sur le
// Hub avec une estimation de VRAM, et des réglages enregistrés par modèle.
import { html, useState, useEffect, cls, fmtBytes } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { refreshEngineNode, refreshStatus } from '../../core/state.js';
import { Line, Group } from './kit.js';

const VERDICT = () => ({ fits: ['green', t('vllm.verdict.fits')], tensor_parallel: ['amber', t('vllm.verdict.tp')], no: ['red', t('vllm.verdict.no')], unknown: ['', t('vllm.verdict.unknown')] });

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

function Search({ onDownload, busy }) {
  const [q, setQ] = useState('');
  const [quant, setQuant] = useState('');
  const [res, setRes] = useState(null);
  const [loading, setLoading] = useState(false);
  const run = async e => {
    e && e.preventDefault();
    if (!q.trim()) return;
    setLoading(true);
    const r = await get('/api/engines/vllm/hub/search?q=' + encodeURIComponent(q.trim()) + (quant ? '&quantization=' + quant : '')).catch(e => ({ error: e.message }));
    setLoading(false);
    if (r.error) return toast(r.error, 'err');
    setRes(r.models || []);
  };
  const verdicts = VERDICT();
  return html`<div class="vl-search">
    <form class="brain-search" onSubmit=${run}><label class="search"><${Icon} n="search" /><input placeholder=${t('vllm.search.placeholder')} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <select class="select" value=${quant} onChange=${e => setQuant(e.target.value)}><option value="">${t('vllm.search.any')}</option><option value="awq">AWQ</option><option value="gptq">GPTQ</option><option value="fp8">FP8</option></select>
      <button class="btn" type="submit" disabled=${loading}>${loading ? html`<span class="spinner"></span>` : t('resources.brain.chercher')}</button></form>
    ${res && (res.length ? html`<div class="card rows">${res.slice(0, 20).map(m => { const vd = verdicts[m.verdict] || verdicts.unknown; return html`<div class="row" key=${m.id}>
        <div class="grow"><div class="t mono">${m.id}</div><div class="s">${m.estimated_vram_mb ? t('vllm.search.vram', { size: fmtBytes(m.estimated_vram_mb * 1048576) }) : t('vllm.verdict.unknown')}${m.quantization ? ' · ' + m.quantization.toUpperCase() : ''}</div></div>
        <span class=${cls('tag', vd[0])}>${vd[1]}</span>
        <button class="btn sm" disabled=${busy} onClick=${() => onDownload(m.id)}><${Icon} n="download" />${t('vllm.search.download')}</button></div>`; })}</div>`
      : html`<p class="note">${t('vllm.search.none')}</p>`)}
    <p class="note">${t('vllm.search.estimate')}</p>
  </div>`;
}

export function VLLMEngine() {
  const [x, setX] = useState(null);
  const [lib, setLib] = useState(null);
  const [cache, setCache] = useState('');
  const [dl, setDl] = useState(null);
  const [params, setParams] = useState(null);
  const [adding, setAdding] = useState(false);
  const [logOpen, setLogOpen] = useState(false);
  const load = () => get('/api/engines/vllm').then(setX).catch(() => setX(null));
  const loadLib = () => get('/api/engines/vllm/models').then(r => { setLib(r.models || []); setCache(r.cache || ''); }).catch(() => setLib([]));
  const loadDl = () => get('/api/engines/vllm/download').then(r => setDl(r.download)).catch(() => {});
  useEffect(() => { load(); loadDl(); }, []);
  useEffect(() => { if (x && x.installed) loadLib(); }, [x && x.installed]);
  useEffect(() => { if (!x || !x.job) return; const id = setInterval(load, 2500); return () => clearInterval(id); }, [x && x.job]);
  const dlActive = dl && !dl.finished && !dl.canceled && !dl.error;
  useEffect(() => { if (!dlActive) return; const id = setInterval(async () => { await loadDl(); }, 1500); return () => clearInterval(id); }, [dlActive]);
  useEffect(() => { if (dl && dl.finished) loadLib(); }, [dl && dl.finished]);
  useEffect(() => { if (x && !x.job && x.running) { refreshEngineNode(); refreshStatus(); } }, [x && x.running, x && x.job]);
  const act = async body => {
    const r = await post('/api/engines/vllm', body).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return toast(r.error || t('settings.page.action_impossible'), 'err');
    setLogOpen(true); load();
  };
  const start = model => act({ action: 'start', model });
  const download = async model => {
    const r = await post('/api/engines/vllm/download', { model }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t('local.hub.telechargement_impossible'), 'err');
    setAdding(false); loadDl();
  };
  const remove = async m => {
    if (!await confirm(t('vllm.lib.delete_title', { model: m.id }), t('vllm.lib.delete_text'), { ok: t('vllm.lib.delete'), danger: true })) return;
    const r = await post('/api/engines/vllm/models/delete', { model: m.id }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error, 'err');
    loadLib();
  };
  const toggleAuto = async on => { const r = await post('/api/engines/vllm/auto-update', { auto: on }); if (!r.ok) return toast(r.error || t('settings.page.reglage_impossible'), 'err'); load(); };
  if (!x) return null;
  const idle = x.installed && !x.running && !x.job;
  const servable = (lib || []).filter(m => m.servable), others = (lib || []).filter(m => !m.servable);
  return html`<${Group} title="${t('settings.page.vllm')}">
    <${Line} label="${t('settings.page.etat')}" tip="${t('settings.page.vllm_sert_des_modeles_hugging_face_pas_les_gguf_avec_beaucoup_de')}">
      ${x.job === 'install' || x.job === 'update' ? html`<span class="state"><span class="spinner"></span>${x.job === 'update' ? t('vllm.updating') : t('settings.page.installation_plusieurs_minutes')}</span>`
        : x.job === 'start' ? html`<span class="state"><span class="spinner"></span>${t('settings.page.chargement_de')} ${x.model}…</span>`
        : x.running ? html`<span class="state"><i class="dot green"></i>${t('settings.page.sert')} <b class="mono">${x.model}</b></span><button class="btn sm ghost" onClick=${() => act({ action: 'stop' })}>${t('settings.page.arreter')}</button>`
        : x.installed ? html`<span class="state">${t('settings.page.installe_arrete')}${x.version ? ' · ' + x.version : ''}</span>`
        : x.missing ? html`<span class="state err">${x.missing}</span>`
        : html`<span class="state">${t('settings.page.non_installe')}</span><button class="btn sm" onClick=${async () => { if (await confirm(t('vllm.install_title'), t('settings.page.loom_cree_un_environnement_python_dans') + x.dir + t('settings.page.et_y_installe_vllm_plusieurs_go_quelques_minutes_rien_n_est_insta'), { ok: t('settings.page.installer') })) act({ action: 'install' }); }}>${t('settings.page.installer_vllm')}</button>`}</${Line}>
    ${x.installed && html`<${Line} label=${t('settings.page.mise_a_jour_automatique')} tip=${t('vllm.auto_tip')}>
      ${idle && html`<button class="btn sm ghost" onClick=${() => act({ action: 'update' })}><${Icon} n="refresh" />${t('settings.page.mettre_a_jour')}</button>`}
      <${Switch} label=${t('settings.page.mise_a_jour_automatique')} checked=${!!(x.auto_update && x.auto_update.auto)} onChange=${toggleAuto} /></${Line}>`}
    ${x.error && html`<${Line} label="${t('settings.page.derniere_erreur')}"><span class="state err">${x.error}</span></${Line}>`}
    ${x.installed && html`<div class="vl-lib">
      <div class="vl-lib-h"><b>${t('vllm.lib.title')}</b><span class="muted">${lib ? t('vllm.lib.count', { n: lib.length }) : ''}</span><span class="grow"></span>
        <button class="btn sm" onClick=${() => setAdding(!adding)}><${Icon} n=${adding ? 'close' : 'plus'} />${adding ? t('settings.page.annuler') : t('vllm.lib.add')}</button></div>
      ${dl && (dlActive || dl.error) && html`<div class="vl-dl"><div class="grow"><div class="t mono">${dl.model}</div>
          ${dl.error ? html`<div class="s err">${dl.error}</div>` : html`<div class="s">${t('vllm.lib.downloading', { done: fmtBytes(dl.done), total: dl.total ? fmtBytes(dl.total) : '—' })}</div><div class="meter"><i style=${`width:${dl.total ? Math.round(dl.done / dl.total * 100) : 3}%;background:var(--text)`}></i></div>`}</div>
        ${dlActive && html`<button class="btn sm ghost" onClick=${async () => { await post('/api/engines/vllm/download/cancel', { model: dl.model }); loadDl(); }}>${t('settings.page.annuler')}</button>`}</div>`}
      ${adding && html`<${Search} busy=${dlActive} onDownload=${download} />`}
      ${lib === null ? html`<div class="skeleton" style="height:60px"></div>` : servable.length ? html`<div class="card rows">${servable.map(m => html`<div class="row" key=${m.id}>
          <div class="grow"><div class="t mono">${m.id}</div><div class="s">${fmtBytes(m.size)}</div></div>
          ${x.running && !x.job && x.model === m.id ? html`<span class="tag green">${t('vllm.lib.serving')}</span>` : ''}
          <button class="icon-btn" aria-label=${t('vllm.params.title')} title=${t('vllm.params.title')} onClick=${() => setParams(m.id)}><${Icon} n="sliders" /></button>
          <button class="btn sm" disabled=${!idle} onClick=${() => start(m.id)}><${Icon} n="play" />${t('vllm.lib.start')}</button>
          <button class="icon-btn" aria-label=${t('vllm.lib.delete')} title=${t('vllm.lib.delete')} disabled=${x.running && x.model === m.id} onClick=${() => remove(m)}><${Icon} n="trash" /></button></div>`)}</div>`
        : html`<p class="note">${t('vllm.lib.empty')}</p>`}
      ${others.length ? html`<details class="vl-others"><summary>${t('vllm.lib.others', { n: others.length })}</summary><div class="card rows">${others.map(m => html`<div class="row" key=${m.id}>
          <div class="grow"><div class="t mono">${m.id}</div><div class="s">${fmtBytes(m.size)} · ${t('vllm.lib.not_servable')}</div></div>
          <button class="icon-btn" aria-label=${t('vllm.lib.delete')} title=${t('vllm.lib.delete')} onClick=${() => remove(m)}><${Icon} n="trash" /></button></div>`)}</div></details>` : ''}
      ${cache && html`<p class="note">${t('vllm.lib.cache')} <code class="mono">${cache.replace(/^\/home\/[^/]+/, '~')}</code></p>`}
    </div>`}
    ${x.log && html`<details class="lc-log" open=${logOpen || !!x.job}><summary>${t('settings.page.journal_vllm')}</summary><pre>${x.log.split('\n').slice(-80).join('\n')}</pre></details>`}
    ${params && html`<${Params} model=${params} onClose=${() => setParams(null)} onStart=${start} />`}
  </${Group}>`;
}
