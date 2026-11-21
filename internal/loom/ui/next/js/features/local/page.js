import { getLang, t } from '../../core/i18n.js';
import { ListPick } from '../../ui/listpick.js';
import { html, useState, useEffect, useRef, useStore, cls, fmtBytes, baseName } from '../../core/lib.js';
import { get, post } from '../../core/api.js';
import { app, go, engineState, refreshLibrary, refreshStatus, refreshEngineNode } from '../../core/state.js';
import { visibleRefresh } from '../../core/poll.js';
import { groupOf } from '../../app/sections.js';
import { Icon } from '../../ui/icons.js';
import { Menu, Empty, Tip, Seg } from '../../ui/controls.js';
import { TabNav } from '../../ui/tabnav.js';
import { confirm, toast, Modal } from '../../ui/dialog.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { ParamsEditor, draftSource, liveSource } from '../inspector/params.js';
import { Config } from '../inspector/config.js';
import { useVllm, VllmLibrary, VllmLibraryStatus, VllmHub, ownsVllm } from '../settings/vllm.js';
import { Hub } from './hub.js';

const modelName = name => (name || '').replace(/\.gguf$/i, '');
const QUANT = /(?:^|[-_.])((?:I?Q\d(?:_[A-Z0-9]+)*)|F16|BF16|F32)(?=[-_.]|$)/i;
export const quantOf = name => (modelName(name).match(QUANT) || [])[1]?.toUpperCase() || '';
const gib = mb => new Intl.NumberFormat(getLang(), { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(mb / 1024);

function ModelEstimate({ model, 'data-label': label }) {
  const [estimate, setEstimate] = useState(null);
  useEffect(() => {
    const controller = new AbortController();
    post('/api/estimate', { model: model.value || model.path }, { signal: controller.signal, retryAuth: false }).then(r => {
      if (!controller.signal.aborted && r.ok) setEstimate(r);
    });
    return () => controller.abort();
  }, [model.value, model.path]);
  const spill = estimate?.ram_offload_mb > 64;
  const fitsVRAM = estimate?.gpu_mb > 0 && estimate.gpu_mb <= estimate.vram_total_mb;
  const fit = spill ? t('models.library.ram_spill') : fitsVRAM ? t('local.page.tient_en_vram') : '';
  const context = estimate?.native_ctx;
  const contextLabel = context >= 1000 ? Math.round(context / 1000) + ' k' : context;
  const details = [context > 0 && t('models.library.native_context', { context: contextLabel }), fit].filter(Boolean);
  const memory = [
    estimate?.gpu_mb > 0 && `${gib(estimate.gpu_mb)} ${t('local.page.go')} ${t('models.library.in_vram')}`,
    spill && '+' + gib(estimate.ram_offload_mb) + t('inspector.params.go_en_ram'),
  ].filter(Boolean).join(' · ');
  if (!details.length && !memory) return null;
  return html`<span data-label=${label} class=${cls(spill && 'models-warning')} title=${[label, memory].filter(Boolean).join(' · ')}>
    <span class="sr">${label} </span>${(details.length ? details : [memory]).join(' · ')}
  </span>`;
}

export async function controlEngine(action) {
  const result = await post('/api/' + action, {});
  if (!result.ok) toast(result.out || result.error || t('local.page.action_impossible'), 'err');
  await refreshStatus();
}

function LibraryStatus({ onUnload }) {
  const { status, gpus, node, unavailable } = useStore(app, s => ({
    status: s.status, gpus: s.gpus, node: s.engineNode, unavailable: s.unavailable.status,
  }));
  const server = useEngineServer();
  const [ram, setRam] = useState(null);
  const [pending, setPending] = useState('');
  const toggle = async () => {
    const action = status.active ? 'stop' : 'start';
    setPending(action);
    try { await controlEngine(action); }
    finally { setPending(''); }
  };
  useEffect(() => visibleRefresh(async alive => {
    const result = await get('/api/ram', { timeout: 6000 });
    if (alive()) setRam(result);
  }, 5000), []);
  const gpu = gpus[0];
  const hasGPU = gpu?.total > 0 && Number.isFinite(gpu.used);
  const percent = hasGPU ? Math.min(100, Math.max(0, Math.round(gpu.used * 100 / gpu.total))) : 0;
  const ramLabel = ram?.total > 0 ? `${gib(ram.used)} / ${gib(ram.total)} ${t('local.page.go')}` : t('local.page.inconnue');
  const st = engineState(status);
  const name = modelName(status?.preset_name || status?.model_name || baseName(status?.model));
  const activeModel = status?.active && name && !status.load_error;
  const starting = pending === 'start' || status?.active && !status.health && !status.load_error;
  const kind = engineKind(status?.engine_kind || server?.engine_kind);
  const state = starting ? t('models.engine.starting') : status?.active ? t('models.engine.running') : t('local.page.arrete');
  const title = unavailable ? t('models.engine.unavailable') : !status ? st.label : t('models.engine.state', { engine: kind, state });
  const host = node?.hostname || status?.hostname;
  const model = activeModel && (status.health ? t('models.library.loaded', { name }) : t('models.library.loading', { name }));
  const detail = [!unavailable && model, host, ...(!unavailable && status?.active ? serverActivity(server) : [])].filter(Boolean).join(' · ');
  return html`<section class="models-card models-status" aria-label=${t('local.page.moteur')}>
    <i class=${cls('dot', unavailable || starting ? 'amber' : st.tone)} aria-hidden="true"></i>
    <div class="models-row-main">
      <b>${title}</b>
      ${detail && html`<small>${detail}</small>`}
      ${status?.load_error && !unavailable && html`<small class="models-warning">${status.load_error}</small>`}
    </div>
    <div class="models-memory-meter">
      <div><span>${t('models.library.gpu_memory')}</span>
        ${hasGPU && html`<span class="num">${gib(gpu.used)} / ${gib(gpu.total)} ${t('local.page.go')}</span>`}
      </div>
      ${hasGPU ? html`<div class="gauge" role="meter" aria-label=${t('models.library.gpu_memory')}
        aria-valuemin="0" aria-valuemax="100" aria-valuenow=${percent}><i style=${{ width: percent + '%' }}></i></div>`
        : html`<small>${t('local.page.aucun_gpu_detecte')}</small>`}
      <${Tip} text=${[gpu?.name, t('local.page.ram_systeme') + ' · ' + ramLabel].filter(Boolean).join(' · ')} />
    </div>
    <div class="models-actions">
      <button class=${cls('btn', !status?.active && 'primary')} disabled=${!!pending || unavailable || !status} onClick=${toggle}>
        ${status?.active ? t('local.page.arreter') : t('local.page.demarrer')}
      </button>
      ${status?.active && !unavailable && (status.model || status.via_api) && html`<${RowMenu} name=${name || kind}
        items=${[{ label: t('local.page.decharger'), run: onUnload }]} />`}
    </div>
  </section>`;
}

function NewPreset({ models, onClose, onCreated }) {
  const [name, setName] = useState('');
  const [model, setModel] = useState(models[0]?.value || models[0]?.path || '');
  const [busy, setBusy] = useState(false);
  const create = async () => {
    if (!name.trim() || !model || busy) return;
    setBusy(true);
    const r = await post('/api/preset/save', { id: '', name: name.trim(), content: 'MODEL=' + model + '\n' });
    setBusy(false);
    if (!r.ok) return toast(r.error || t('local.page.creation_impossible'), 'err');
    toast(t('local.page.preset_cree_regle_le_dans_le_panneau'));
    onCreated({ id: r.id, name: name.trim(), model });
  };
  return html`<${Modal} title=${t('local.page.nouveau_preset')} sub=${t('local.page.choisis_le_modele_tu_regleras_ensuite_ses_parametres_dans_le_pann')} onClose=${onClose}
      foot=${html`<button class="btn" onClick=${onClose}>${t('local.page.annuler')}</button><button class="btn primary" disabled=${busy || !name.trim() || !model} onClick=${create}>${t('local.page.creer')}</button>`}>
    <label class="field"><span>${t('local.page.nom')}</span><input class="input" autofocus placeholder=${t('local.page.ex_qwen_27b_long_contexte')} value=${name} onInput=${e => setName(e.target.value)} onKeyDown=${e => e.key === 'Enter' && create()} /></label>
    <label class="field"><span>${t('local.page.modele')}</span>
      <${ListPick} value=${model} onChange=${setModel} disabled=${!models.length}
        options=${models.map(m => ({ value: m.value || m.path, label: modelName(m.name) }))} /></label>
    ${!models.length && html`<p class="note">${t('local.page.aucun_modele_dans_la_bibliotheque_telecharge_en_un_depuis_le_hub')}</p>`}
  </${Modal}>`;
}

function RowMenu({ name, items }) {
  const [anchor, setAnchor] = useState(null);
  return html`<button type="button" class="icon-btn" aria-label=${t('models.row.actions', { name })}
    aria-haspopup="menu" aria-expanded=${String(!!anchor)} onClick=${e => setAnchor(e.currentTarget)}><${Icon} n="more" /></button>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${items} />`}`;
}

function Library({ enginePicker }) {
  const { models, presets, status, unavailable = {} } = useStore(app, s => ({
    models: s.models, presets: s.presets, status: s.status, unavailable: s.unavailable,
  }));
  const [q, setQ] = useState('');
  const [draft, setDraft] = useState(null);
  const request = useRef(0);
  const [menu, setMenu] = useState(null);
  const [creating, setCreating] = useState(() => {
    const open = app.get().newPreset;
    if (open) app.set({ newPreset: false });
    return open;
  });
  const [ordering, setOrdering] = useState(false);
  const orderBusy = useRef(false);
  useEffect(() => {
    refreshLibrary();
    return () => { request.current++; };
  }, []);
  const query = q.trim().toLowerCase();
  const weights = models.filter(m => !m.remote && !m.mmproj && !/mmproj/i.test(m.name));
  const filteredModels = weights.filter(m => m.name.toLowerCase().includes(query));
  const filteredPresets = presets.filter(p => [p.name, baseName(p.model)].some(name => name?.toLowerCase().includes(query)));
  const duplicateNames = new Set(), seen = new Set();
  for (const m of weights) {
    if (seen.has(m.name)) duplicateNames.add(m.name);
    seen.add(m.name);
  }
  const current = status?.active && !status.via_api && !unavailable.status ? status.model : '';
  const openEditor = async (name, loadSource) => {
    const token = ++request.current;
    setDraft({ name, src: null });
    try {
      const src = await loadSource();
      if (token === request.current) setDraft({ name, src });
    } catch (e) {
      if (token === request.current) {
        setDraft(null);
        toast(e.message, 'err');
      }
    }
  };
  const openModel = m => openEditor(m.name, () => {
    const model = m.value || m.path;
    return !status?.preset_id && (model === current || m.path === current) ? liveSource() : draftSource(model);
  });
  const openPreset = p => openEditor(p.name, async () => {
    const data = await get('/api/preset?id=' + encodeURIComponent(p.id));
    if (typeof data.content !== 'string') throw new Error(t('local.page.preset_indisponible'));
    return {
      live: !!current && status.preset_id === p.id, mode: 'preset', model: new Config(data.content).get('MODEL'),
      base: data.content, presetId: p.id, presetName: p.name, presetIndex: app.get().presets.findIndex(x => x.id === p.id) + 1,
    };
  });
  const closeDraft = () => { request.current++; setDraft(null); };
  const load = async (endpoint, body, name) => {
    const r = await post(endpoint, body);
    if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
    toast(t('models.library.loading', { name }));
    setTimeout(refreshStatus, 800);
  };
  const loadPreset = p => {
    const index = app.get().presets.findIndex(x => x.id === p.id);
    if (index >= 0) load('/api/switch', { n: index + 1 }, p.name);
  };
  const unload = async () => {
    if (!await confirm(t('local.page.decharger_le_modele'), t('local.page.la_vram_est_liberee_le_moteur_reste_pret'), { ok: t('local.page.decharger') })) return;
    const r = await post('/api/unload', {});
    if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
    refreshStatus();
  };
  const remove = async m => {
    if (!await confirm(t('local.page.supprimer_le_fichier'), '« ' + m.name + ' » (' + fmtBytes(m.size) + t('local.page.sera_supprime_du_disque'), { ok: t('local.page.supprimer'), danger: true })) return;
    const r = await post('/api/models/delete', { name: m.path });
    if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
    refreshLibrary();
  };
  const removePreset = async p => {
    if (!await confirm(t('local.page.supprimer_le_preset'), '« ' + p.name + t('local.page.sera_supprime_le_modele_reste_sur_le_disque'), { ok: t('local.page.supprimer'), danger: true })) return;
    const r = await post('/api/preset/delete', { id: p.id });
    if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
    refreshLibrary();
  };
  const movePreset = async (p, step) => {
    if (orderBusy.current) return;
    // Reorder the entire library even when a filter hides the adjacent preset.
    const ids = presets.map(x => x.id), i = ids.indexOf(p.id), j = i + step;
    if (i < 0 || j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    orderBusy.current = true;
    setOrdering(true);
    try {
      const r = await post('/api/presets/order', { ids });
      if (!r.ok) return toast(r.error || t('local.page.ordre_des_presets_non_enregistre'), 'err');
      await refreshLibrary();
    } finally { orderBusy.current = false; setOrdering(false); }
  };
  const modelState = status?.health ? t('local.page.charge') : t('local.page.chargement');
  const stateClass = cls('models-row-state', status?.health ? 'models-good' : 'models-warning');

  return html`
    <${LibraryStatus} onUnload=${unload} />
    <div class="models-toolbar">
      <label class="search"><${Icon} n="search" />
        <input aria-label=${t('models.library.filter')} placeholder=${t('models.library.filter')} value=${q} onInput=${e => setQ(e.target.value)} />
      </label>
      ${enginePicker}
      <button class="btn" onClick=${() => setCreating(true)}>${t('local.page.nouveau_preset')}</button>
      <a class="btn primary" href="#/local/hub">${t('local.page.telecharger_un_modele')}</a>
    </div>
    <section class="models-group" aria-labelledby="library-presets">
      <div class="models-section-head"><h2 id="library-presets">${t('local.page.presets')}</h2><small>${t('models.library.preset_hint')}</small></div>
      <div class="models-card">
        ${filteredPresets.length ? filteredPresets.map(p => {
          const on = current && status.preset_id === p.id;
          const index = presets.findIndex(x => x.id === p.id);
          const metadata = [
            modelName(baseName(p.model)), p.ctx, p.reasoning && t('models.library.reasoning_on'),
            p.vision && t('inspector.catalog.vision.label'),
          ].filter(Boolean).join(' · ');
          return html`<div class="models-row" key=${p.id}>
            <div class="models-row-main" ...${inspectTrigger(() => openPreset(p), t('local.page.regler_2') + p.name)}>
              <b>${p.name}</b>${metadata && html`<small>${metadata}</small>`}
            </div>
            <div class="models-actions">
              ${on ? html`<span class=${stateClass}>${modelState}</span><button class="btn sm" onClick=${unload}>${t('local.page.decharger')}</button>`
                : html`<button class="btn sm" onClick=${() => loadPreset(p)}>${t('local.page.charger')}</button>`}
              <button class="btn sm" onClick=${() => openPreset(p)}>${t('local.page.regler')}</button>
              <${RowMenu} name=${p.name} items=${[
                { label: t('local.page.monter'), disabled: ordering || index === 0, run: () => movePreset(p, -1) },
                { label: t('local.page.descendre'), disabled: ordering || index === presets.length - 1, run: () => movePreset(p, 1) },
                '-', { label: t('local.page.supprimer'), icon: 'trash', danger: true, run: () => removePreset(p) },
              ]} />
            </div>
          </div>`;
        }) : html`<div class="models-row models-empty">${unavailable.presets ? t('engine.na.unreachable') : query
          ? t('local.page.aucun_resultat') : t('local.page.aucun_preset_cree_en_un_pour_garder_un_modele_avec_ses_reglages_c')}</div>`}
      </div>
    </section>
    <section class="models-group" aria-labelledby="library-models">
      <div class="models-section-head"><h2 id="library-models">${t('local.page.modeles')}</h2>
        ${filteredModels.length > 0 && html`<${Tip} text=${t('models.library.estimate_hint')} />`}
      </div>
      <div class="models-card">
        ${filteredModels.length ? filteredModels.map(m => {
          const name = modelName(m.name);
          const on = current && !status.preset_id && (current === (m.value || m.path) || current === m.path);
          const quantLabel = t('local.page.quant'), fileLabel = t('local.page.fichier');
          return html`<div class="models-row" key=${m.path}>
            <div class="models-row-main" ...${inspectTrigger(() => openModel(m), t('local.page.regler_2') + name)}>
              <b>${name}</b>
              <small class="models-metadata">
                <span data-label=${quantLabel}><span class="sr">${quantLabel} </span>${quantOf(m.name) || '—'}</span>
                <span data-label=${fileLabel} title=${m.path}><span class="sr">${fileLabel} </span>${fmtBytes(m.size)}${m.shards > 1 ? ' · ' + m.shards + ' ' + t('local.hub.parts') : ''}</span>
                <${ModelEstimate} model=${m} data-label=${t('local.page.memoire_estimee')} />
                ${duplicateNames.has(m.name) && m.dir && html`<span>${m.dir.replace(/^\/home\/[^/]+/, '~')}</span>`}
              </small>
            </div>
            <div class="models-actions">
              ${on ? html`<span class=${stateClass}>${modelState}</span>`
                : html`<button class="btn sm" onClick=${() => load('/api/load-model', { model: m.value || m.path }, name)}>${t('local.page.charger')}</button>`}
              <button class="btn sm" onClick=${() => openModel(m)}>${t('local.page.regler')}</button>
              <button class="icon-btn" aria-label=${t('models.row.actions', { name })} onClick=${e => setMenu({ anchor: e.currentTarget, items: [
                ...(on ? [{ label: t('local.page.decharger'), run: unload }, '-'] : []),
                { label: t('local.page.supprimer_le_fichier'), icon: 'trash', danger: true, run: () => remove(m) },
              ] })}><${Icon} n="more" /></button>
            </div>
          </div>`;
        }) : html`<${Empty} icon="chip" title=${unavailable.models ? t('engine.na.unreachable') : query ? t('local.page.aucun_resultat') : t('local.page.aucun_modele')}
            text=${query ? t('local.page.essaie_un_autre_filtre') : t('models.library.empty')}>
          ${!query && html`<a class="btn" href="#/local/hub">${t('local.page.ouvrir_le_hub')}</a>`}
        </${Empty}>`}
      </div>
    </section>
    ${creating && html`<${NewPreset} models=${weights} onClose=${() => setCreating(false)} onCreated=${async p => {
      setCreating(false);
      await refreshLibrary();
      openPreset(p);
    }} />`}
    ${menu && html`<${Menu} anchor=${menu.anchor} onClose=${() => setMenu(null)} items=${menu.items} />`}
    ${draft && html`<${Drawer} title=${t('local.page.regler_3') + modelName(draft.name)} onClose=${closeDraft}>
      ${draft.src ? html`<${ParamsEditor} key=${draft.src.model} src=${draft.src} onLoaded=${closeDraft} />` : html`<div class="insp-body"><p class="note">${t('local.page.lecture_des_parametres')}</p></div>`}
    </${Drawer}>`}`;
}

export const engineKind = kind => kind === 'vllm' ? 'vLLM' : kind === 'openai' ? t('local.page.serveur_compatible_openai') : kind || 'llama.cpp';

export function useEngineServer() {
  const [server, setServer] = useState(null);
  useEffect(() => {
    let mounted = true, pending = false;
    const load = async () => {
      if (pending || globalThis.document?.hidden) return;
      pending = true;
      try {
        const result = await get('/api/server', { timeout: 6000 });
        if (mounted) setServer(result);
      } catch (_) {} finally { pending = false; }
    };
    load();
    const timer = setInterval(load, 1500);
    return () => { mounted = false; clearInterval(timer); };
  }, []);
  return server;
}

export function serverActivity(server) {
  const slots = server?.slots;
  const active = slots?.ok === false
    ? null : slots?.busy ?? slots?.items?.filter(s => s.busy).length;
  const total = slots?.items?.length || server?.np;
  const rate = server?.stats?.live_toks || server?.stats?.avg_toks;
  return [
    Number.isInteger(active) && t('models.engine.active_slots', { n: active, total: total || active }),
    rate > 0 && new Intl.NumberFormat(getLang(), { maximumFractionDigits: 1 }).format(rate) + ' ' + t('local.page.tok_s'),
  ].filter(Boolean);
}

export function EngineRuntime({ node, location, vllm, management, access, maintenance }) {
  const server = useEngineServer();
  const { status, unavailable } = useStore(app, s => ({ status: s.status, unavailable: s.unavailable?.status }));
  const [log, setLog] = useState(null);
  const [acting, setActing] = useState(false);
  const direct = node?.direct && !vllm;
  const x = vllm?.x;
  const kind = engineKind(node?.kind || status?.engine_kind || server?.engine_kind);
  const running = x?.running ?? status?.active;
  const starting = x ? x.job === 'start' : running && !status?.health;
  const error = x ? x.error || x.auto_update?.last_error : status?.load_error;
  const st = engineState(status);
  const label = direct ? (node.reachable ? t('common.reachable') : t('common.unreachable'))
    : unavailable ? t('models.engine.unavailable') : error ? t('core.state.erreur_de_chargement')
      : starting ? t('models.engine.starting')
        : running ? t('local.page.pret')
          : status ? t('local.page.arrete') : st.label;
  const tone = direct ? (node.reachable ? 'green' : 'red')
    : unavailable || starting ? 'amber' : error ? 'red' : running ? 'green' : st.tone;
  const machine = node?.hostname || status?.hostname;
  const port = !node?.direct && (status?.port || server?.port);
  const description = [machine, port && t('models.engine.port', { port }), running && t('local.page.utilise_par_loom')].filter(Boolean);
  const model = x?.model || status?.preset_name || status?.model_name || baseName(server?.model_name);
  const activity = direct || !running || unavailable ? [] : [model?.replace(/\.gguf$/i, ''), ...(!x ? serverActivity(server) : [])].filter(Boolean);
  const act = async action => {
    setActing(true);
    try {
      if (vllm) {
        if (action === 'stop') await vllm.act({ action });
        else if (model) await vllm.start(model);
      } else {
        await controlEngine(action);
      }
    } finally { setActing(false); }
  };
  const showLog = async () => {
    try { setLog(x ? x.log || '' : (await get('/api/service/log?n=200')).log || ''); }
    catch (e) { toast(e.message, 'err'); }
  };
  const engine = html`<section class="models-group">
    <div class="models-section-head"><h2>${t('local.page.moteur')}</h2></div>
    <div class="models-card">
      <div class="models-row">
        <i class=${cls('dot', tone)} aria-hidden="true"></i>
        <div class="models-row-main">
          <span>${t('models.engine.state', { engine: kind, state: label })}</span>
          ${description.length > 0 && html`<small>${description.join(' · ')}</small>`}
          ${activity.length > 0 && html`<small>${activity.join(' · ')}</small>`}
          ${server?.node_managed && html`<small>${server.url}</small>`}
        </div>
        ${!direct && html`<div class="models-actions">
          ${running || x?.job === 'start' ? html`
            ${x?.job !== 'start' && html`<button class="btn sm" disabled=${acting || unavailable} onClick=${() => act('restart')}>${t('local.page.redemarrer')}</button>`}
            <button class="btn sm" disabled=${acting || unavailable} onClick=${() => act('stop')}>${t('local.page.arreter')}</button>`
            : vllm ? html`<a class="btn sm" href="#/local">${t('local.page.bibliotheque')}</a>`
              : html`<button class="btn sm primary" disabled=${acting || unavailable || !status} onClick=${() => act('start')}>${t('local.page.demarrer')}</button>`}
        </div>`}
      </div>
      ${location}
      ${!direct && html`<div class="models-row">
        <span class="models-row-main">${t('local.page.journal_du_moteur')}</span>
        <button class="btn sm" onClick=${showLog}>${t('models.engine.show_log')}</button>
      </div>`}
    </div>
    ${error && html`<div class="alert red"><${Icon} n="alert" /><span>${error}</span></div>`}
    ${log !== null && html`<${Drawer} title=${t('local.page.journal_du_moteur')} onClose=${() => setLog(null)}><pre class="log mono">${log || t('local.page.journal_vide')}</pre></${Drawer}>`}
  </section>`;
  return html`<div class="models-engine-grid">
    <div class="models-engine-column">${engine}${management?.(server)}</div>
    ${access && html`<div class="models-engine-column">${access(server)}</div>`}
    ${maintenance}
  </div>`;
}

function DirectLibrary({ node, hub }) {
  const { models, unavailable } = useStore(app, s => ({ models: s.models, unavailable: s.unavailable.models }));
  useEffect(() => { refreshLibrary(); }, []);
  const list = models.filter(m => m.remote);
  const kind = engineKind(node.kind);
  const pick = async m => {
    const r = await post('/api/load-model', { model: m });
    if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
    refreshEngineNode();
    refreshStatus();
    toast(t('local.page.modele_utilise') + m);
  };
  return html`
    <section class="models-card models-status" aria-label=${t('local.page.moteur')}>
      <i class=${cls('dot', node.reachable ? 'green' : 'red')} aria-hidden="true"></i>
      <div class="models-row-main"><b>${t('models.engine.on_host', { engine: kind, host: node.hostname })}</b>
        <small>${[node.reachable ? t('common.reachable') : t('common.unreachable'), node.model, node.url].filter(Boolean).join(' · ')}</small></div>
      <a class="btn" href="#/engine">${t('local.page.emplacement_du_moteur')}</a>
    </section>
    ${hub ? html`<div class="models-card"><${Empty} icon="server" title=${t('models.hub.direct_title')} text=${t('models.hub.direct_hint')}>
      <a class="btn" href="#/local">${t('local.page.bibliotheque')}</a>
    </${Empty}></div>` : html`
      <section class="models-group">
        <div class="models-section-head"><h2>${t('local.page.modeles_servis')}</h2></div>
        <div class="models-card">${list.length ? list.map(m => html`<div class="models-row" key=${m.value}>
          <div class="models-row-main"><b>${m.name}</b>${m.name !== m.value && html`<small>${m.value}</small>`}</div>
          ${node.model === m.value ? html`<span class="models-good">${t('local.page.utilise_par_loom')}</span>`
            : html`<button class="btn sm" onClick=${() => pick(m.value)}>${t('local.page.utiliser')}</button>`}
        </div>`) : html`<div class="models-row models-empty">${unavailable ? t('engine.na.unreachable') : t('local.page.aucun_modele')}</div>`}</div>
      </section>`}`;
}

export function LocalPage({ route }) {
  const node = useStore(app, a => a.engineNode);
  const v = useVllm();
  const [engine, setEngineState] = useState(() => {
    try { return localStorage.getItem('loom.local.engine') || ''; } catch (_) { return ''; }
  });
  const setEngine = value => {
    setEngineState(value);
    try { localStorage.setItem('loom.local.engine', value); } catch (_) {}
  };
  if (route.sub === 'engine') { go('engine'); return null; }
  const hub = route.sub === 'hub';
  const hubQuery = hub ? route.id : '';
  const ownedVllm = ownsVllm(node, v.x);
  const direct = node?.direct && !ownedVllm;
  const which = v.x?.installed && (engine === 'vllm' || (!engine && (ownedVllm || v.x.running))) ? 'vllm' : 'llama';
  const enginePicker = v.x?.installed && html`<${Seg} size="sm" label=${t('vllm.local.engine')} value=${which} onChange=${setEngine}
    options=${[{ value: 'llama', label: 'llama.cpp' }, { value: 'vllm', label: 'vLLM' }]} />`;
  const owner = node?.url || 'local';
  const hubKey = owner + '/' + hubQuery;
  const group = groupOf('local');
  const activeTab = hub ? 'local/hub' : 'local';
  const tabs = group.tabs.filter(([, label]) => label)
    .map(([id, label]) => ({ href: '#/' + id, label: label(), current: id === activeTab }));
  return html`<div class="view page models-page" key=${route.sub}><div class=${cls('page-in models-layout', !hub && 'models-library')}>
    <${TabNav} id=${group.id} label=${group.label} tabs=${tabs} />
    <h1 class="sr">${hub ? t('local.page.hub') : t('local.page.bibliotheque')}</h1>
    ${direct ? html`<${DirectLibrary} key=${owner} node=${node} hub=${hub} />`
      : which === 'vllm' ? (hub ? html`<${VllmHub} key=${hubKey} v=${v} enginePicker=${enginePicker} initialQuery=${hubQuery} />`
        : html`<${VllmLibrary} key=${owner} v=${v} onHub=${() => go('local', 'hub')} enginePicker=${enginePicker} />`)
      : hub ? html`<${Hub} key=${hubKey} enginePicker=${enginePicker} initialQuery=${hubQuery} />`
      : ownedVllm ? html`<${VllmLibraryStatus} v=${v} />
          <div class="models-toolbar">${enginePicker}</div>
          <p class="models-row-meta">${t('models.library.stop_vllm_hint')}</p>`
      : html`<${Library} key=${owner} enginePicker=${enginePicker} />`}
  </div></div>`;
}
