// Paramètres du modèle local. Lit la configuration courante (modèle nu ou
// preset), l'édite en mémoire, estime la VRAM à chaque changement et applique
// d'un bloc. Trois niveaux : Essentiel, Avancé, Expert (drapeaux llama.cpp).
import { html, useState, useEffect, useRef, useMemo, useStore, cls, debounce, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Switch, Slider, Tip, Menu } from '../../ui/controls.js';
import { confirm, prompt, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshStatus, refreshLibrary } from '../../core/state.js';
import { Config, fromMap } from './config.js';

const fmtK = n => { n = +n || 0; return n >= 1e6 ? (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M' : n >= 1000 ? Math.round(n / 1000) + 'K' : String(n); };
const gib = mb => ((+mb || 0) / 1024).toFixed(1);

const Row = ({ spec, children }) => html`<div class=${cls('prow', spec.stack && 'stack')} data-row=${spec.id}>
  <div class="prow-l"><span>${spec.label || spec.id}</span>${spec.tip && html`<${Tip} text=${spec.tip} />`}</div>
  <div class="prow-c">${children}</div></div>`;

const Select = ({ value, options, onChange }) => html`<select class="select sm" value=${value} onChange=${e => onChange(e.target.value)}>
  ${options.map(([v, l]) => html`<option value=${v} selected=${String(v) === String(value)}>${l}</option>`)}</select>`;

const Num = ({ value, placeholder, onChange, step }) => html`<input class="input sm num" type="number" step=${step || 'any'} value=${value} placeholder=${placeholder || '—'} onChange=${e => onChange(e.target.value)} />`;

function VramCard({ est, note }) {
  if (!est || !est.ok) return html`<div class="vram skeleton" style="height:72px"></div>`;
  const vram = est.vram_total_mb || 0, gpu = est.gpu_mb || 0, kv = est.kv_mb || 0, off = est.ram_offload_mb || 0;
  const w = vram ? Math.min(100, (gpu - kv) * 100 / vram) : 0, all = vram ? Math.min(100, gpu * 100 / vram) : 0;
  return html`<div class="vram">
    <div class="vram-h"><span>Mémoire estimée${note && html`<${Tip} text=${note} />`}</span><b>${gib(gpu)} Go <small>/ ${gib(vram)}</small></b></div>
    <div class=${cls('gauge', off > 64 && 'over')}><i class="ghost" style=${`width:${all}%`}></i><i style=${`width:${w}%`}></i></div>
    <div class="vram-leg"><span>Poids ${gib(gpu - kv)} Go</span><span>Cache KV ${gib(kv)} Go</span><span class="end">${off > 64 ? '+' + gib(off) + ' Go en RAM' : est.ctx ? fmtK(est.ctx) + ' tokens' : ''}</span></div>
    ${off > 64 && est.max_ctx_fit > 0 && html`<div class="vram-warn"><${Icon} n="alert" />~${fmtK(est.max_ctx_fit)} tokens tiendraient en VRAM</div>`}
  </div>`;
}

// Charge la configuration du modèle actuellement dans le moteur.
export async function liveSource() {
  const [c, presets] = await Promise.all([get('/api/config'), get('/api/presets')]);
  const model = String((c && c.MODEL) || '').trim();
  const act = (presets || []).find(p => p.active);
  let text = fromMap(c).text, remembered = false;
  if (act) { const d = await get('/api/preset?id=' + encodeURIComponent(act.id)); text = d.content || text; }
  else if (model) { const r = await get('/api/naked/remember?model=' + encodeURIComponent(model)).catch(() => ({})); remembered = !!(r && r.remembered); }
  return { live: true, mode: act ? 'preset' : model ? 'model' : 'empty', model, presetId: act ? act.id : '', presetName: act ? act.name : '', base: text, remembered };
}

// A draft preserves unset memory settings so llama.cpp can fit them at load.
export async function draftSource(model) {
  const [d, rem] = await Promise.all([
    get('/api/naked/defaults?model=' + encodeURIComponent(model)),
    get('/api/naked/remember?model=' + encodeURIComponent(model)).catch(() => ({})),
  ]);
  // The remember endpoint returns the merged loading configuration.
  return { live: false, mode: 'model', model, presetId: '', presetName: '', base: rem?.remembered && rem.content ? rem.content : (d && d.content) || '', remembered: !!rem?.remembered };
}

export function LocalParams() {
  const status = useStore(app, s => s.status);
  const [src, setSrc] = useState(null);
  const key = status && (status.model + '|' + status.preset_id + '|' + status.health);
  useEffect(() => { liveSource().then(setSrc); }, [key]);
  if (status && status.engine_direct) return html`<div class="insp-empty anim-rise"><${Icon} n="server" /><h3>${baseName(status.model_name || '')}</h3><p>Servi par ${status.engine_kind === 'vllm' ? 'vLLM' : status.engine_kind} sur ${status.hostname}, lié directement. Ses paramètres (contexte, couches GPU…) se règlent sur sa machine.</p><a class="btn sm" href="#/settings/engine">Emplacement du moteur</a></div>`;
  if (!src) return html`<div class="insp-body"><div class="skeleton" style="height:78px"></div><div class="skeleton" style="height:30px;margin-top:16px"></div></div>`;
  if (src.mode === 'empty') return html`<div class="insp-empty anim-rise"><${Icon} n="chip" /><h3>Aucun modèle chargé</h3><p>Choisis un modèle ou un preset en haut de la discussion pour régler ses paramètres.</p></div>`;
  return html`<${ParamsEditor} src=${src} onSaved=${base => setSrc({ ...src, base })} />`;
}

export function ParamsEditor({ src, onSaved, onLoaded }) {
  const [cfg, setCfg] = useState(() => new Config(src.base));
  const [caps, setCaps] = useState({});
  const [specs, setSpecs] = useState([]);
  const [tier, setTier] = useState(localStorage.getItem('loom.next.tier') || 'essential');
  const [est, setEst] = useState(null);
  const [filter, setFilter] = useState('');
  const [menu, setMenu] = useState(null);
  const [base, setBase] = useState(src.base);
  useEffect(() => { setCfg(new Config(src.base)); setBase(src.base); }, [src.base, src.model, src.presetId]);
  useEffect(() => {
    get('/api/engine/params').then(r => setSpecs((r && r.params) || [])).catch(() => toast('Catalogue des paramètres indisponible', 'err'));
    if (src.model) get('/api/model-caps?model=' + encodeURIComponent(src.model)).then(r => setCaps(r && r.ok ? r : {})).catch(() => {});
  }, [src.model]);
  const estimate = useMemo(() => debounce(async (model, content) => {
    try { setEst(await post('/api/estimate', { model, content })); } catch (_) { setEst(null); }
  }, 180), []);
  useEffect(() => { if (src.model) estimate(src.model, cfg.text); }, [cfg.text, src.model]);
  const edit = fn => { const c = cfg.clone(); fn(c); setCfg(c); };
  const dirty = cfg.text.trim() !== base.trim();
  const native = caps.native_ctx || 8192, layers = Math.max(1, caps.n_layers || 80);
  const ctx = cfg.get('CTX');
  const status = useStore(app, s => s.status);
  const observed = src.live && status?.model === src.model ? status.ctx_effective : null;
  const fitSupported = specs.some(p => p.id === 'fit' && p.supported);
  const autoFit = fitSupported && /^on$/i.test(cfg.arg('--fit') || cfg.get('FIT'));
  const nglRaw = cfg.get('NGL'), ngl = !nglRaw || nglRaw === '999' ? layers : +nglRaw;
  const thinks = !!caps.thinks || !!cfg.get('REASONING');
  const reasonOn = /^(off|none|false|0)$/i.test(cfg.get('REASONING')) ? false : /^(on|1|true|auto|deepseek)$/i.test(cfg.get('REASONING')) ? true : !!caps.thinks;
  const efforts = [['', caps.effort_default ? 'défaut (' + caps.effort_default + ')' : 'défaut'], ...(caps.effort || []).map(e => [e, e])];
  const vision = !!(caps.mmproj && caps.mmproj.length) || !!cfg.get('MMPROJ');
  const visionOn = !cfg.has('--no-mmproj') && (!!cfg.get('MMPROJ') || !!(caps.mmproj && caps.mmproj.length));

  // Data chooses rows/order/copy/choices. Only model-dependent behavior stays
  // here; generic native flags use the same Config path as Expert.
  const nativeFlag = p => ({ ...p, kind: p.native_kind || p.kind });
  const visible = p => p.available && (!p.when || (p.when === 'thinks' ? thinks
    : p.when === 'effort' ? thinks && reasonOn && efforts.length > 1 : p.when === 'vision' ? vision : false));
  const control = p => {
    const f = nativeFlag(p);
    const value = p.key && !p.requires_flag && p.tier !== 'expert' ? cfg.get(p.key) : cfg.flagValue(f);
    const change = v => edit(c => p.key && !p.requires_flag && p.tier !== 'expert' ? c.set(p.key, v) : c.setFlagValue(f, v));
    switch (p.control || p.kind) {
      case 'context': return html`<div class="slide-row"><${Slider} value=${Math.min(+ctx || native, native)} min=${p.min} max=${native} step=${p.step} label=${p.label}
        onInput=${v => edit(c => c.set(p.key, v))} /><input class="input sm num" value=${ctx} placeholder=${autoFit ? 'Auto' : fmtK(native)} onChange=${e => edit(c => c.set(p.key, e.target.value))} aria-label="Contexte (tokens)" /></div>
        <small class="note">natif ${fmtK(native)} · effectif par slot ${observed > 0 ? fmtK(observed) : '—'}</small>`;
      case 'layers': return html`<div class="slide-row"><${Slider} value=${ngl} min=${p.min} max=${layers} label=${p.label}
        onInput=${v => edit(c => c.set(p.key, v >= layers ? '999' : v))} /><span class="val mono">${!nglRaw && autoFit ? 'auto' : ngl >= layers ? 'tout' : ngl}</span></div>`;
      case 'reasoning': return html`<${Switch} checked=${reasonOn} label=${p.label} onChange=${on => edit(c => c.set(p.key, on ? (caps.thinks ? '' : 'on') : 'off'))} />`;
      case 'effort': return html`<${Select} value=${cfg.get(p.key)} options=${efforts} onChange=${v => edit(c => { c.set(p.key, v); if (v) c.flag('--jinja', true); })} />`;
      case 'vision': return html`<${Switch} checked=${visionOn} label=${p.label} onChange=${on => edit(c => { if (!on) { c.set(p.key, ''); c.flag('--no-mmproj', true); } else { c.flag('--no-mmproj', false); if (caps.mmproj && caps.mmproj.length === 1) c.set(p.key, caps.mmproj[0]); } })} />`;
      case 'kv': return html`<${Select} value=${cfg.kv()} options=${p.choices} onChange=${v => edit(c => c.setKv(v))} />`;
      case 'parallel': return html`<${Num} value=${cfg.get(p.key) || cfg.arg('-np')} placeholder=${p.placeholder} step=${p.step} onChange=${v => edit(c => c.set(p.key, v))} />`;
      case 'fit': return html`<${Switch} checked=${/^on$/i.test(cfg.arg('--fit') || cfg.get('FIT'))} label=${p.label} onChange=${on => edit(c => c.setFlagValue(f, on ? 'on' : 'off'))} />`;
      case 'textarea': return html`<textarea class="textarea sm" rows="3" placeholder=${p.placeholder} value=${value} onChange=${e => change(e.target.value)}></textarea>`;
      case 'number': return html`<${Num} value=${value} placeholder=${p.placeholder} step=${p.step} onChange=${change} />`;
      case 'bool': return html`<${Switch} checked=${cfg.flagValue(f) === 'on'} label=${p.label} onChange=${on => edit(c => c.setFlagValue(f, on ? 'on' : ''))} />`;
      case 'enum': return p.choices && p.choices.length
        ? html`<${Select} value=${value} options=${p.tier === 'expert' ? [['', 'défaut'], ...p.choices] : p.choices} onChange=${change} />`
        : html`<input class="input sm" value=${value} placeholder=${p.default || ''} onChange=${e => change(e.target.value)} />`;
      default: return html`<input class="input sm" value=${value} placeholder=${p.default || ''} onChange=${e => change(e.target.value)} />`;
    }
  };
  const rows = ps => ps.map(p => html`<${Row} key=${p.id} spec=${p}>${control(p)}</${Row}>`);
  const essentials = rows(specs.filter(p => p.tier === 'essential' && visible(p)));
  const advanced = rows(specs.filter(p => p.tier === 'advanced' && visible(p)));
  const q = filter.trim().toLowerCase();
  const expertFlags = specs.filter(p => p.tier === 'expert' && p.available && (!q || (p.id + ' ' + p.tip).toLowerCase().includes(q)));
  const expert = html`<label class="search sm"><${Icon} n="search" /><input placeholder="Filtrer les drapeaux llama.cpp…" value=${filter} onInput=${e => setFilter(e.target.value)} /></label>
    ${rows(expertFlags.slice(0, 120))}
    ${expertFlags.length > 120 && html`<p class="note">Affine le filtre pour voir les ${expertFlags.length - 120} autres drapeaux.</p>`}`;

  const saved = text => { setBase(text); onSaved && onSaved(text); };
  const apply = async () => {
    if (!await confirm('Appliquer les paramètres', 'Le modèle est rechargé avec ces réglages. Le moteur reste démarré.', { ok: 'Appliquer' })) return;
    const r = await post('/api/apply', { content: cfg.text, preset_id: src.presetId });
    if (!r.ok) { toast(r.error || 'Échec', 'err'); return; }
    toast('Rechargement du modèle…'); saved(cfg.text); setTimeout(refreshStatus, 800);
  };
  const remember = async quiet => {
    const r = await post('/api/naked/remember', { content: cfg.text, model: src.model });
    if (!r.ok) { toast(r.error, 'err'); return false; }
    if (!quiet) toast('Réglages mémorisés pour le prochain chargement');
    saved(cfg.text); return true;
  };
  const loadNow = async () => {
    if ((!autoFit || cfg.get('CTX') || cfg.get('NGL') || /(?:^|\s)(?:-c|--ctx-size|-ngl|--gpu-layers)(?:\s|=)/.test(cfg.get('EXTRA_ARGS'))) && est && (est.ram_offload_mb | 0) > 64 && !await confirm('Le modèle dépasse la VRAM', 'Environ ' + gib(est.ram_offload_mb) + ' Go iraient en RAM et la génération sera plus lente. Charger quand même ?', { ok: 'Charger quand même', danger: true })) return;
    let r;
    if (src.mode === 'preset') {
      r = dirty ? await post('/api/apply', { content: cfg.text, preset_id: src.presetId }) : await post('/api/switch', { n: src.presetIndex });
    } else {
      if (dirty && !await remember(true)) return;
      r = await post('/api/load-model', { model: src.model });
    }
    if (!r.ok) { toast(r.error || 'Chargement impossible', 'err'); return; }
    toast('Chargement de ' + baseName(src.model) + '…'); setTimeout(refreshStatus, 800); onLoaded && onLoaded();
  };
  const newPreset = async () => {
    const name = await prompt('Nouveau preset', { placeholder: 'ex. Qwen 27B · long contexte', ok: 'Créer' }); if (!name) return;
    const r = await post('/api/preset/save', { id: '', name: name.trim(), content: cfg.text }); if (!r.ok) return toast(r.error, 'err');
    refreshLibrary();
    if (src.live) { await post('/api/apply', { content: cfg.text, preset_id: r.id || '' }); setTimeout(refreshStatus, 800); }
    toast('Preset « ' + name.trim() + ' » créé');
  };
  const saveMenu = [
    ...(src.mode === 'preset'
      ? [{ label: 'Mettre à jour « ' + src.presetName + ' »', icon: 'check', run: async () => { const r = await post('/api/preset/save', { id: src.presetId, name: src.presetName, content: cfg.text }); if (!r.ok) return toast(r.error, 'err'); if (src.live) await post('/api/apply', { content: cfg.text, preset_id: src.presetId }); toast('Preset mis à jour'); saved(cfg.text); } }]
      : [{ label: src.live ? 'Se souvenir pour ce modèle' : 'Mémoriser sans charger', icon: 'star', run: () => remember(false) }]),
    { label: 'Créer un preset…', icon: 'plus', run: newPreset },
    '-',
    { label: 'Revenir aux défauts du modèle', icon: 'refresh', run: async () => { const r = await get('/api/naked/defaults?model=' + encodeURIComponent(src.model)); if (r && r.ok) setCfg(new Config(r.content || '')); } },
    { label: 'Annuler les modifications', icon: 'close', run: () => setCfg(new Config(base)) },
  ];

  return html`<div class="insp-body">
      <div class="insp-model-row"><div class="insp-model"><b>${src.mode === 'preset' ? src.presetName : baseName(src.model).replace(/\.gguf$/i, '')}</b>
        <span>${src.mode === 'preset' ? 'preset · ' + baseName(src.model) : !src.live ? 'pas chargé' : src.remembered ? 'modèle · réglages mémorisés' : 'modèle · réglages automatiques'}</span></div>
        <button class="btn sm ghost" title="Enregistrer ces réglages sous un nom" onClick=${newPreset}><${Icon} n="plus" />Preset</button></div>
      <${VramCard} est=${est} note=${!cfg.get('CTX') ? (autoFit ? 'Estimation au contexte natif. llama.cpp ajustera les valeurs non fixées au chargement.' : 'Ajustement automatique indisponible ou désactivé ; le contexte natif sert de défaut.') : ''} />
      <${Seg} value=${tier} onChange=${t => { setTier(t); localStorage.setItem('loom.next.tier', t); }} label="Niveau" options=${[{ value: 'essential', label: 'Essentiel' }, { value: 'advanced', label: 'Avancé' }, { value: 'expert', label: 'Expert' }]} />
      <div class="prows" key=${tier}>${tier === 'essential' ? essentials : tier === 'advanced' ? advanced : expert}</div>
    </div>
    <div class=${cls('insp-foot', dirty && 'dirty')}>
      <span class="insp-state">${!src.live ? 'Le moteur ne change pas tant que tu ne charges pas' : dirty ? 'Modifications non appliquées' : 'Appliqué au modèle chargé'}</span>
      <div class="insp-acts">
        ${src.live ? html`<button class="btn primary" disabled=${!dirty} onClick=${apply}>Appliquer</button>` : html`<button class="btn primary" onClick=${loadNow}><${Icon} n="play" />Charger</button>`}
        <button class="btn" onClick=${e => setMenu(e.currentTarget)}>Enregistrer<${Icon} n="chevron" /></button>
      </div>
      ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${saveMenu} />`}
    </div>`;
}
