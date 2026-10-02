import { t, tSource } from '../../core/i18n.js';
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
  <div class="prow-l"><span>${tSource(spec.label) || spec.id}</span>${spec.tip && html`<${Tip} text=${tSource(spec.tip)} />`}</div>
  <div class="prow-c">${children}</div></div>`;

const Select = ({ value, options, onChange }) => html`<select class="select sm" value=${value} onChange=${e => onChange(e.target.value)}>
  ${options.map(([v, l]) => html`<option value=${v} selected=${String(v) === String(value)}>${tSource(l)}</option>`)}</select>`;

const Num = ({ value, placeholder, onChange, step }) => html`<input class="input sm num" type="number" step=${step || 'any'} value=${value} placeholder=${placeholder || '—'} onChange=${e => onChange(e.target.value)} />`;

function VramCard({ est, note }) {
  if (!est || !est.ok) return html`<div class="vram skeleton" style="height:72px"></div>`;
  const vram = est.vram_total_mb || 0, gpu = est.gpu_mb || 0, kv = est.kv_mb || 0, off = est.ram_offload_mb || 0;
  const w = vram ? Math.min(100, (gpu - kv) * 100 / vram) : 0, all = vram ? Math.min(100, gpu * 100 / vram) : 0;
  return html`<div class="vram">
    <div class="vram-h"><span>${t("inspector.params.memoire_estimee")}${note && html`<${Tip} text=${note} />`}</span><b>${gib(gpu)} ${t("inspector.params.go")} <small>/ ${gib(vram)}</small></b></div>
    <div class=${cls('gauge', off > 64 && 'over')}><i class="ghost" style=${`width:${all}%`}></i><i style=${`width:${w}%`}></i></div>
    <div class="vram-leg"><span>${t("inspector.params.poids")} ${gib(gpu - kv)} ${t("inspector.params.go")}</span><span>${t("inspector.params.cache_kv")} ${gib(kv)} ${t("inspector.params.go")}</span><span class="end">${off > 64 ? '+' + gib(off) + t("inspector.params.go_en_ram") : est.ctx ? fmtK(est.ctx) + ' tokens' : ''}</span></div>
    ${off > 64 && est.max_ctx_fit > 0 && html`<div class="vram-warn"><${Icon} n="alert" />~${fmtK(est.max_ctx_fit)} ${t("inspector.params.tokens_tiendraient_en_vram")}</div>`}
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
  if (status && status.engine_direct) return html`<div class="insp-empty anim-rise"><${Icon} n="server" /><h3>${baseName(status.model_name || '')}</h3><p>${t("inspector.params.servi_par")} ${status.engine_kind === 'vllm' ? 'vLLM' : status.engine_kind} ${t("inspector.params.sur")} ${status.hostname}${t("inspector.params.lie_directement_ses_parametres_contexte_couches_gpu_se_reglent_su")}</p><a class="btn sm" href="#/settings/engine">${t("inspector.params.emplacement_du_moteur")}</a></div>`;
  if (!src) return html`<div class="insp-body"><div class="skeleton" style="height:78px"></div><div class="skeleton" style="height:30px;margin-top:16px"></div></div>`;
  if (src.mode === 'empty') return html`<div class="insp-empty anim-rise"><${Icon} n="chip" /><h3>${t("inspector.params.aucun_modele_charge")}</h3><p>${t("inspector.params.choisis_un_modele_ou_un_preset_en_haut_de_la_discussion_pour_regl")}</p></div>`;
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
    get('/api/engine/params').then(r => setSpecs((r && r.params) || [])).catch(() => toast(t("inspector.params.catalogue_des_parametres_indisponible"), 'err'));
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
  const efforts = [['', caps.effort_default ? t("inspector.params.defaut") + caps.effort_default + ')' : t("inspector.params.defaut_2")], ...(caps.effort || []).map(e => [e, e])];
  const vision = !!(caps.mmproj && caps.mmproj.length) || !!cfg.get('MMPROJ');
  const visionOn = !cfg.has('--no-mmproj') && (!!cfg.get('MMPROJ') || !!(caps.mmproj && caps.mmproj.length));

  // Data chooses rows/order/copy/choices. Only model-dependent behavior stays
  // here; generic native flags use the same Config path as Expert.
  const nativeFlag = p => ({ ...p, kind: p.native_kind || p.kind });
  const visible = p => p.available && (!p.when || (p.when === 'thinks' ? thinks
    : p.when === 'effort' ? thinks && reasonOn && efforts.length > 1 : p.when === 'vision' ? vision : false));
  const control = raw => {
    const p = { ...raw, label: tSource(raw.label), placeholder: tSource(raw.placeholder) };
    const f = nativeFlag(p);
    const value = p.key && !p.requires_flag && p.tier !== 'expert' ? cfg.get(p.key) : cfg.flagValue(f);
    const change = v => edit(c => p.key && !p.requires_flag && p.tier !== 'expert' ? c.set(p.key, v) : c.setFlagValue(f, v));
    switch (p.control || p.kind) {
      case 'context': return html`<div class="slide-row"><${Slider} value=${Math.min(+ctx || native, native)} min=${p.min} max=${native} step=${p.step} label=${p.label}
        onInput=${v => edit(c => c.set(p.key, v))} /><input class="input sm num" value=${ctx} placeholder=${autoFit ? t("inspector.params.auto") : fmtK(native)} onChange=${e => edit(c => c.set(p.key, e.target.value))} aria-label="${t("inspector.params.contexte_tokens")}" /></div>
        <small class="note">${t("inspector.params.natif")} ${fmtK(native)} ${t("inspector.params.effectif_par_slot")} ${observed > 0 ? fmtK(observed) : '—'}</small>`;
      case 'layers': return html`<div class="slide-row"><${Slider} value=${ngl} min=${p.min} max=${layers} label=${p.label}
        onInput=${v => edit(c => c.set(p.key, v >= layers ? '999' : v))} /><span class="val mono">${!nglRaw && autoFit ? 'auto' : ngl >= layers ? t("inspector.params.tout") : ngl}</span></div>`;
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
        ? html`<${Select} value=${value} options=${p.tier === 'expert' ? [['', t("inspector.params.defaut_2")], ...p.choices] : p.choices} onChange=${change} />`
        : html`<input class="input sm" value=${value} placeholder=${p.default || ''} onChange=${e => change(e.target.value)} />`;
      default: return html`<input class="input sm" value=${value} placeholder=${p.default || ''} onChange=${e => change(e.target.value)} />`;
    }
  };
  const rows = ps => ps.map(p => html`<${Row} key=${p.id} spec=${p}>${control(p)}</${Row}>`);
  const essentials = rows(specs.filter(p => p.tier === 'essential' && visible(p)));
  const advanced = rows(specs.filter(p => p.tier === 'advanced' && visible(p)));
  const q = filter.trim().toLowerCase();
  const expertFlags = specs.filter(p => p.tier === 'expert' && p.available && (!q || (p.id + ' ' + tSource(p.tip)).toLowerCase().includes(q)));
  const expert = html`<label class="search sm"><${Icon} n="search" /><input placeholder="${t("inspector.params.filtrer_les_drapeaux_llama_cpp")}" value=${filter} onInput=${e => setFilter(e.target.value)} /></label>
    ${rows(expertFlags.slice(0, 120))}
    ${expertFlags.length > 120 && html`<p class="note">${t("inspector.params.affine_le_filtre_pour_voir_les")} ${expertFlags.length - 120} ${t("inspector.params.autres_drapeaux")}</p>`}`;

  const saved = text => { setBase(text); onSaved && onSaved(text); };
  const apply = async () => {
    if (!await confirm(t("inspector.params.appliquer_les_parametres"), t("inspector.params.le_modele_est_recharge_avec_ces_reglages_le_moteur_reste_demarre"), { ok: t("inspector.params.appliquer") })) return;
    const r = await post('/api/apply', { content: cfg.text, preset_id: src.presetId });
    if (!r.ok) { toast(r.error || t("inspector.params.echec"), 'err'); return; }
    toast(t("inspector.params.rechargement_du_modele")); saved(cfg.text); setTimeout(refreshStatus, 800);
  };
  const remember = async quiet => {
    const r = await post('/api/naked/remember', { content: cfg.text, model: src.model });
    if (!r.ok) { toast(r.error, 'err'); return false; }
    if (!quiet) toast(t("inspector.params.reglages_memorises_pour_le_prochain_chargement"));
    saved(cfg.text); return true;
  };
  const loadNow = async () => {
    if ((!autoFit || cfg.get('CTX') || cfg.get('NGL') || /(?:^|\s)(?:-c|--ctx-size|-ngl|--gpu-layers)(?:\s|=)/.test(cfg.get('EXTRA_ARGS'))) && est && (est.ram_offload_mb | 0) > 64 && !await confirm(t("inspector.params.le_modele_depasse_la_vram"), t("inspector.params.environ") + gib(est.ram_offload_mb) + t("inspector.params.go_iraient_en_ram_et_la_generation_sera_plus_lente_charger_quand"), { ok: t("inspector.params.charger_quand_meme"), danger: true })) return;
    let r;
    if (src.mode === 'preset') {
      r = dirty ? await post('/api/apply', { content: cfg.text, preset_id: src.presetId }) : await post('/api/switch', { n: src.presetIndex });
    } else {
      if (dirty && !await remember(true)) return;
      r = await post('/api/load-model', { model: src.model });
    }
    if (!r.ok) { toast(r.error || t("inspector.params.chargement_impossible"), 'err'); return; }
    toast(t("inspector.params.chargement_de") + baseName(src.model) + '…'); setTimeout(refreshStatus, 800); onLoaded && onLoaded();
  };
  const newPreset = async () => {
    const name = await prompt(t("inspector.params.nouveau_preset"), { placeholder: t("inspector.params.ex_qwen_27b_long_contexte"), ok: t("inspector.params.creer") }); if (!name) return;
    const r = await post('/api/preset/save', { id: '', name: name.trim(), content: cfg.text }); if (!r.ok) return toast(r.error, 'err');
    refreshLibrary();
    if (src.live) { await post('/api/apply', { content: cfg.text, preset_id: r.id || '' }); setTimeout(refreshStatus, 800); }
    toast(t("inspector.params.preset_prefix") + name.trim() + t("inspector.params.cree"));
  };
  const saveMenu = [
    ...(src.mode === 'preset'
      ? [{ label: t("inspector.params.mettre_a_jour") + src.presetName + ' »', icon: 'check', run: async () => { const r = await post('/api/preset/save', { id: src.presetId, name: src.presetName, content: cfg.text }); if (!r.ok) return toast(r.error, 'err'); if (src.live) await post('/api/apply', { content: cfg.text, preset_id: src.presetId }); toast(t("inspector.params.preset_mis_a_jour")); saved(cfg.text); } }]
      : [{ label: src.live ? t("inspector.params.se_souvenir_pour_ce_modele") : t("inspector.params.memoriser_sans_charger"), icon: 'star', run: () => remember(false) }]),
    { label: t("inspector.params.creer_un_preset"), icon: 'plus', run: newPreset },
    '-',
    { label: t("inspector.params.revenir_aux_defauts_du_modele"), icon: 'refresh', run: async () => { const r = await get('/api/naked/defaults?model=' + encodeURIComponent(src.model)); if (r && r.ok) setCfg(new Config(r.content || '')); } },
    { label: t("inspector.params.annuler_les_modifications"), icon: 'close', run: () => setCfg(new Config(base)) },
  ];

  return html`<div class="insp-body">
      <div class="insp-model-row"><div class="insp-model"><b>${src.mode === 'preset' ? src.presetName : baseName(src.model).replace(/\.gguf$/i, '')}</b>
        <span>${src.mode === 'preset' ? t("inspector.params.preset_model") + baseName(src.model) : !src.live ? t("inspector.params.pas_charge") : src.remembered ? t("inspector.params.modele_reglages_memorises") : t("inspector.params.modele_reglages_automatiques")}</span></div>
        <button class="btn sm ghost" title="${t("inspector.params.enregistrer_ces_reglages_sous_un_nom")}" onClick=${newPreset}><${Icon} n="plus" />${t("inspector.params.preset")}</button></div>
      <${VramCard} est=${est} note=${!cfg.get('CTX') ? (autoFit ? t("inspector.params.estimation_au_contexte_natif_llama_cpp_ajustera_les_valeurs_non_f") : t("inspector.params.ajustement_automatique_indisponible_ou_desactive_le_contexte_nati")) : ''} />
      <${Seg} value=${tier} onChange=${localT => { setTier(localT); localStorage.setItem('loom.next.tier', localT); }} label="${t("inspector.params.niveau")}" options=${[{ value: 'essential', label: t("inspector.params.essentiel") }, { value: 'advanced', label: t("inspector.params.avance") }, { value: 'expert', label: t("inspector.params.expert") }]} />
      <div class="prows" key=${tier}>${tier === 'essential' ? essentials : tier === 'advanced' ? advanced : expert}</div>
    </div>
    <div class=${cls('insp-foot', dirty && 'dirty')}>
      <span class="insp-state">${!src.live ? t("inspector.params.le_moteur_ne_change_pas_tant_que_tu_ne_charges_pas") : dirty ? t("inspector.params.modifications_non_appliquees") : t("inspector.params.applique_au_modele_charge")}</span>
      <div class="insp-acts">
        ${src.live ? html`<button class="btn primary" disabled=${!dirty} onClick=${apply}>${t("inspector.params.appliquer")}</button>` : html`<button class="btn primary" onClick=${loadNow}><${Icon} n="play" />${t("inspector.params.charger")}</button>`}
        <button class="btn" onClick=${e => setMenu(e.currentTarget)}>${t("inspector.params.enregistrer")}<${Icon} n="chevron" /></button>
      </div>
      ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${saveMenu} />`}
    </div>`;
}
