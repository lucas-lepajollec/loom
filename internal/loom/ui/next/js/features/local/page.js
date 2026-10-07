import { t } from '../../core/i18n.js';
// Local : tes modèles sur cette machine, servis par llama.cpp.
// Onglets : Bibliothèque · Hub. Le moteur (état, API, installation) est la page Modèles › Moteur.
import { useVllm, VllmLibrary, VllmHub, VllmEngine } from '../settings/vllm.js';
import { SectionTabs } from '../../app/sections.js';
import { Logo } from '../../ui/logo.js';
import { vendorOf } from '../chat/picker.js';
import { html, useState, useEffect, useRef, useStore, useMemo, cls, fmtBytes, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tabs, Menu, Empty, Tip, Switch, Seg } from '../../ui/controls.js';
import { confirm, toast, prompt, Modal } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, engineState, refreshLibrary, refreshStatus, refreshEngineNode } from '../../core/state.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { ParamsEditor, draftSource, liveSource } from '../inspector/params.js';
import { Config } from '../inspector/config.js';
import { Hub } from './hub.js';

const TABS = () => ([{ value: 'library', label: t("local.page.bibliotheque") }, { value: 'hub', label: t("local.page.hub") }]);
const gib = mb => ((+mb || 0) / 1024).toFixed(1);

const QUANT = /(?:^|[-_.])((?:I?Q\d(?:_[A-Z0-9]+)*)|F16|BF16|F32)(?=[-_.]|$)/i;
export const quantOf = name => ((name || '').replace(/\.gguf$/i, '').match(QUANT) || [])[1]?.toUpperCase() || '';

// Mémoire estimée au contexte natif : besoin, débordement éventuel en RAM, jauge fine.
function Estimate({ model }) {
  const [e, setE] = useState(null);
  useEffect(() => { post('/api/estimate', { model }).then(setE).catch(() => {}); }, [model]);
  if (!e || !e.ok) return html`<span class="muted">—</span>`;
  const off = (e.ram_offload_mb | 0) > 64, total = e.vram_total_mb || 0;
  const pct = total ? Math.min(100, Math.round(e.gpu_mb * 100 / total)) : 0;
  return html`<span class="fitc">
    <span class="est">${gib(e.gpu_mb)} ${t("local.page.go")}<small>${off ? '+' + gib(e.ram_offload_mb) + t("inspector.params.go_en_ram") : total ? t("local.page.tient_en_vram") : ''}</small></span>
    ${total > 0 && html`<span class=${cls('gauge', off && 'over')}><i style=${`width:${pct}%`}></i></span>`}
  </span>`;
}

// Bandeau machine : VRAM, RAM, moteur, modèle chargé.
function Strip() {
  const { status, gpus } = useStore(app, s => ({ status: s.status, gpus: s.gpus }));
  const [ram, setRam] = useState(null);
  useEffect(() => { const f = () => get('/api/ram').then(setRam).catch(() => {}); f(); const localT = setInterval(f, 5000); return () => clearInterval(localT); }, []);
  const g = (gpus || [])[0], st = engineState(status);
  const pct = (u, localT) => localT ? Math.min(100, Math.round(u * 100 / localT)) : 0;
  return html`<div class="card local-strip">
    <div><div class="lbl">${g ? 'VRAM · ' + g.name.replace(/^NVIDIA (GeForce )?/, '') : 'VRAM'}</div>
      ${g ? html`<div class="v"><b>${gib(g.used)} ${t("local.page.go")}</b><small>/ ${gib(g.total)}</small></div><div class="gauge"><i style=${`width:${pct(g.used, g.total)}%`}></i></div>`
        : html`<div class="v"><span class="t">${t("local.page.aucun_gpu_detecte")}</span></div>`}</div>
    <div><div class="lbl">${t("local.page.ram_systeme")}</div>
      ${ram && ram.total ? html`<div class="v"><b>${gib(ram.used)} ${t("local.page.go")}</b><small>/ ${gib(ram.total)}</small></div><div class="gauge"><i style=${`width:${pct(ram.used, ram.total)}%`}></i></div>`
        : html`<div class="v"><span class="t muted">${t("local.page.inconnue")}</span></div>`}</div>
    <div><div class="lbl">${t("local.page.moteur")}</div><div class="v"><i class=${'dot ' + (status && status.active ? 'green' : '')}></i><span class="t">${t("local.page.llama_cpp")}</span></div>
      <div class="sub">${status ? (status.active ? t("local.page.actif_port") + status.port : t("local.page.arrete")) : '…'}</div></div>
    <div><div class="lbl">${t("local.page.charge")}</div><div class="v"><span class="t">${status && status.active && status.model ? (status.preset_name || status.model_name || '').replace(/\.gguf$/i, '') : t("local.page.aucun_modele")}</span></div>
      <div class="sub">${status && status.active && status.model ? (st.tone === 'green' ? t("local.page.pret") : st.label) : t("local.page.charge_un_modele_ci_dessous")}</div></div>
  </div>`;
}

// Nouveau preset : un nom et un modèle ; les réglages se font ensuite dans le
// panneau, comme pour un modèle.
function NewPreset({ models, onClose, onCreated }) {
  const [name, setName] = useState('');
  const [model, setModel] = useState(models[0] ? (models[0].value || models[0].path) : '');
  const create = async () => {
    if (!name.trim() || !model) return;
    const r = await post('/api/preset/save', { id: '', name: name.trim(), content: 'MODEL=' + model + '\n' });
    if (!r.ok) return toast(r.error || t("local.page.creation_impossible"), 'err');
    toast(t("local.page.preset_cree_regle_le_dans_le_panneau"));
    onCreated({ id: r.id, name: name.trim(), model });
  };
  return html`<${Modal} title="${t("local.page.nouveau_preset")}" sub="${t("local.page.choisis_le_modele_tu_regleras_ensuite_ses_parametres_dans_le_pann")}" onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>${t("local.page.annuler")}</button><button class="btn primary" disabled=${!name.trim() || !model} onClick=${create}>${t("local.page.creer")}</button>`}>
    <label class="field"><span>${t("local.page.nom")}</span><input class="input" autofocus placeholder="${t("local.page.ex_qwen_27b_long_contexte")}" value=${name} onInput=${e => setName(e.target.value)} onKeyDown=${e => e.key === 'Enter' && create()} /></label>
    <label class="field"><span>${t("local.page.modele")}</span><select class="select" value=${model} onChange=${e => setModel(e.target.value)}>
      ${models.map(m => html`<option value=${m.value || m.path}>${m.name.replace(/\.gguf$/i, '')}</option>`)}</select></label>
    ${!models.length && html`<p class="note">${t("local.page.aucun_modele_dans_la_bibliotheque_telecharge_en_un_depuis_le_hub")}</p>`}
  </${Modal}>`;
}

function Library() {
  const { models, presets, status } = useStore(app, s => ({ models: s.models, presets: s.presets, status: s.status }));
  const [q, setQ] = useState('');
  const [draft, setDraft] = useState(null);
  const request = useRef(0);
  useEffect(() => () => { request.current++; }, []);
  const [menu, setMenu] = useState(null);
  const [creating, setCreating] = useState(() => { const v = !!(app.get && app.get().newPreset); if (v) app.set({ newPreset: false }); return v; });
  const [ordering, setOrdering] = useState(false);
  const orderBusy = useRef(false);
  useEffect(() => { refreshLibrary(); }, []);
  const live = status && status.active && status.model ? baseName(status.model) : '';
  const weights = (models || []).filter(m => !m.mmproj && !/mmproj/i.test(m.name) && (!q || m.name.toLowerCase().includes(q.toLowerCase())));
  const plist = (presets || []).filter(p => !q || (p.name || '').toLowerCase().includes(q.toLowerCase()));
  const openDraft = async m => {
    const token = ++request.current;
    setDraft({ name: m.name, src: null });
    try {
      const src = status?.model === (m.value || m.path) && !status.preset_id ? await liveSource() : await draftSource(m.value || m.path);
      if (token === request.current) setDraft({ name: m.name, src });
    } catch (e) { if (token === request.current) { setDraft(null); toast(e.message, 'err'); } }
  };
  const openPreset = async p => {
    const token = ++request.current;
    setDraft({ name: p.name, src: null });
    try {
      const data = await get('/api/preset?id=' + encodeURIComponent(p.id));
      if (data.error || typeof data.content !== 'string') throw new Error(data.error || t("local.page.preset_indisponible"));
      const src = { live: status?.preset_id === p.id, mode: 'preset', model: new Config(data.content).get('MODEL'), base: data.content, presetId: p.id, presetName: p.name, presetIndex: presets.findIndex(x => x.id === p.id) + 1 };
      if (token === request.current) setDraft({ name: p.name, src });
    } catch (e) { if (token === request.current) { setDraft(null); toast(e.message, 'err'); } }
  };
  const closeDraft = () => { request.current++; setDraft(null); };
  const load = async m => { const r = await post('/api/load-model', { model: m.value || m.path }); if (!r.ok) return toast(r.error, 'err'); toast(t("local.page.chargement_de") + m.name + '…'); setTimeout(refreshStatus, 800); };
  const unload = async () => { if (!await confirm(t("local.page.decharger_le_modele"), t("local.page.la_vram_est_liberee_le_moteur_reste_pret"), { ok: t("local.page.decharger") })) return; await post('/api/unload', {}); refreshStatus(); };
  const del = async m => { if (!await confirm(t("local.page.supprimer_le_fichier"), '« ' + m.name + ' » (' + fmtBytes(m.size) + t("local.page.sera_supprime_du_disque"), { ok: t("local.page.supprimer"), danger: true })) return; const r = await post('/api/models/delete', { name: m.path }); if (!r.ok) return toast(r.error, 'err'); refreshLibrary(); };
  const delPreset = async p => { if (!await confirm(t("local.page.supprimer_le_preset"), '« ' + p.name + t("local.page.sera_supprime_le_modele_reste_sur_le_disque"), { ok: t("local.page.supprimer"), danger: true })) return; await post('/api/preset/delete', { id: p.id }); refreshLibrary(); };
  const movePreset = async (p, step) => {
    if (orderBusy.current) return;
    // L’ordre porte toujours sur tout le catalogue, même si la liste est filtrée.
    const ids = presets.map(x => x.id), i = ids.indexOf(p.id), j = i + step;
    if (i < 0 || j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    orderBusy.current = true; setOrdering(true);
    try {
      const r = await post('/api/presets/order', { ids });
      if (!r.ok) throw new Error(r.error || t("local.page.ordre_non_enregistre"));
      await refreshLibrary();
    } catch (_) { toast(t("local.page.ordre_des_presets_non_enregistre"), 'err'); }
    finally { orderBusy.current = false; setOrdering(false); }
  };

  return html`<div class="lib">
    <div class="toolbar"><label class="search"><${Icon} n="search" /><input placeholder="${t("local.page.filtrer_les_modeles_et_presets")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <span class="grow"></span></div>
    <section class="sec"><div class="sec-h"><h2>${t("local.page.presets")} <span class="count">${plist.length}</span><${Tip} text="${t("local.page.un_preset_un_modele_et_ses_reglages_enregistres_sous_un_nom_tu_en")}" /></h2>
      <button class="btn sm" onClick=${() => setCreating(true)}><${Icon} n="plus" />${t("local.page.nouveau_preset")}</button></div>
      ${plist.length ? html`<div class="card rows stagger">${plist.map(p => {
        const on = status && status.preset_id === p.id;
        const index = presets.findIndex(x => x.id === p.id);
        return html`<div class="row" key=${p.id}><${Logo} name=${vendorOf(p.model || p.name) === t("local.page.autres") ? p.name : vendorOf(p.model || p.name)} />
          <div class="grow" ...${inspectTrigger(() => openPreset(p), t("local.page.regler_2") + p.name)}><div class="t">${p.name}${on && html` <span class=${'tag ' + (status.health ? 'green' : 'amber')}>${status.health ? t("local.page.charge") : t("local.page.chargement")}</span>`}</div><div class="s">${baseName(p.model || '').replace(/\.gguf$/i, '') || 'preset'}</div></div>
          <span class="order-btns"><button class="icon-btn" aria-label=${t("local.page.monter_prefix") + p.name} title="${t("local.page.monter")}" disabled=${ordering || index === 0} onClick=${() => movePreset(p, -1)}><${Icon} n="chevron" class="up" /><span class="sr">${t("local.page.monter")}</span></button>
          <button class="icon-btn" aria-label=${t("local.page.descendre_prefix") + p.name} title="${t("local.page.descendre")}" disabled=${ordering || index === presets.length - 1} onClick=${() => movePreset(p, 1)}><${Icon} n="chevron" /><span class="sr">${t("local.page.descendre")}</span></button></span>
          <button class="icon-btn" aria-label="${t("local.page.regler")}" title="${t("local.page.regler")}" onClick=${() => openPreset(p)}><${Icon} n="sliders" /></button>
          ${on ? html`<button class="btn sm" onClick=${unload}>${t("local.page.decharger")}</button>` : html`<button class="btn sm" onClick=${async () => { await post('/api/switch', { n: presets.findIndex(x => x.id === p.id) + 1 }); toast(t("local.page.chargement_de") + p.name + '…'); setTimeout(refreshStatus, 800); }}>${t("local.page.charger")}</button>`}
          <button class="icon-btn" aria-label="${t("local.page.actions")}" onClick=${e => setMenu({ a: e.currentTarget, items: [{ label: t("local.page.supprimer"), icon: 'trash', danger: true, run: () => delPreset(p) }] })}><${Icon} n="more" /></button></div>`;
      })}</div>`
      : html`<div class="card pad"><p class="note">${t("local.page.aucun_preset_cree_en_un_pour_garder_un_modele_avec_ses_reglages_c")}</p></div>`}
    </section>
    ${creating && html`<${NewPreset} models=${weights} onClose=${() => setCreating(false)} onCreated=${p => { setCreating(false); refreshLibrary().then(() => openPreset(p)); }} />`}
    <section class="sec"><div class="sec-h"><h2>${t("local.page.modeles")} <span class="count">${weights.length}</span></h2></div>
      ${weights.length ? html`<div class="card table stagger">
        <div class="tr th"><span>${t("local.page.modele")}</span><span>${t("local.page.quant")}</span><span>${t("local.page.fichier")}</span><span>${t("local.page.memoire_estimee")}<${Tip} text="${t("local.page.estimation_au_contexte_natif_du_modele_regle_le_avant_de_charger")}" /></span><span></span></div>
        ${weights.map(m => {
          const on = live === m.name && !(status && status.preset_id);
          return html`<div class="tr" key=${m.path}>
            <span class="cell-id"><${Logo} name=${vendorOf(m.name) === t("local.page.autres") ? m.name : vendorOf(m.name)} />
              <span class="cell-main" ...${inspectTrigger(() => openDraft(m), t("cloud.page.inspecter") + m.name)}><b><span class="nm">${m.name.replace(/\.gguf$/i, '')}</span>${on && html`<span class=${'tag ' + (status.health ? 'green' : 'amber')}>${status.health ? t("local.page.charge") : t("local.page.chargement")}</span>`}</b><small>${m.dir.replace(/^\/home\/[^/]+/, '~')}</small></span></span>
            <span class="mono dim">${quantOf(m.name) || '—'}</span>
            <span class="num dim">${fmtBytes(m.size)}${m.shards > 1 ? ' · ' + m.shards + ' parts' : ''}</span>
            <span><${Estimate} model=${m.value || m.path} /></span>
            <span class="acts">
              <button class="icon-btn" aria-label="${t("local.page.regler")}" title="${t("local.page.regler")}" onClick=${() => openDraft(m)}><${Icon} n="sliders" /></button>
              ${on ? html`<button class="btn sm" onClick=${unload}>${t("local.page.decharger")}</button>` : html`<button class="btn sm" onClick=${() => load(m)}>${t("local.page.charger")}</button>`}
              <button class="icon-btn" aria-label="${t("local.page.actions")}" onClick=${e => setMenu({ a: e.currentTarget, items: [{ label: t("local.page.supprimer_le_fichier"), icon: 'trash', danger: true, run: () => del(m) }] })}><${Icon} n="more" /></button>
            </span></div>`;
        })}</div>` : html`<div class="card"><${Empty} icon="chip" title=${q ? t("local.page.aucun_resultat") : t("local.page.aucun_modele")} text=${q ? t("local.page.essaie_un_autre_filtre") : t("local.page.telecharge_un_gguf_depuis_le_hub_ou_ajoute_ton_dossier_de_modeles")}>
          ${!q && html`<button class="btn primary" onClick=${() => go('local', 'hub')}>${t("local.page.ouvrir_le_hub")}</button>`}</${Empty}></div>`}
    </section>
    ${menu && html`<${Menu} anchor=${menu.a} onClose=${() => setMenu(null)} items=${menu.items} />`}
    ${draft && html`<${Drawer} title=${t("local.page.regler_3") + draft.name.replace(/\.gguf$/i, '')} onClose=${closeDraft}>
      ${draft.src ? html`<${ParamsEditor} key=${draft.src.model} src=${draft.src} onLoaded=${closeDraft} />` : html`<div class="insp-body"><p class="note">${t("local.page.lecture_des_parametres")}</p></div>`}</${Drawer}>`}
  </div>`;
}

export function EngineRuntime() {
  const status = useStore(app, s => s.status);
  const [srv, setSrv] = useState(null);
  const [lc, setLc] = useState(null);
  const [np, setNp] = useState('');
  const [log, setLog] = useState(null);
  const load = async () => { try { const s = await get('/api/server'); setSrv(s); if (!np) setNp(String(s.np || '')); } catch (_) {} };
  useEffect(() => { load(); get('/api/llamacpp').then(setLc).catch(() => {}); const localT = setInterval(load, 1500); return () => clearInterval(localT); }, []);
  const st = engineState(status);
  const act = async a => { const r = await post('/api/' + a, {}); if (r.ok === false) toast(r.out || r.error || t("local.page.action_impossible"), 'err'); setTimeout(refreshStatus, 600); };
  const copy = localT => navigator.clipboard && navigator.clipboard.writeText(localT).then(() => toast(t("local.page.copie")));
  const slots = (srv && srv.slots && srv.slots.items) || [];
  const busy = slots.filter(s => s.busy).length;
  const curl = srv ? `curl ${srv.url}/chat/completions ${srv.key_required ? '-H \'Authorization: Bearer YOUR_API_KEY\' ' : ''}\\\n  -H 'Content-Type: application/json' \\\n  -d '{"model":"loom","messages":[{"role":"user","content":"Bonjour"}]}'` : '';
  return html`<div class="engine stagger">
    <div class="card engine-hero">
      <div class="eh-main"><div class="eh-state"><i class=${'dot ' + st.tone}></i><b>${status && status.active ? (status.health ? t("local.page.moteur_actif") : status.model ? t("local.page.chargement_du_modele") : t("local.page.moteur_pret")) : t("local.page.moteur_arrete")}</b></div>
        <span class="mono muted">${lc ? `llama.cpp ${lc.commit || ''} · ${lc.plan && lc.plan.backend ? lc.plan.backend.toUpperCase() : lc.kind} · ${srv ? srv.url : ''}` : '…'}</span></div>
      <div class="stat"><b>${srv && srv.model_name ? baseName(srv.model_name).replace(/\.gguf$/i, '') : '—'}</b><span>${t("local.page.modele_2")}</span></div>
      <div class="stat"><b>${busy} / ${slots.length || (srv && srv.np) || 0}</b><span>${t("local.page.slots_actifs")}</span></div>
      <div class="stat"><b>${srv && srv.stats ? (srv.stats.live_toks || srv.stats.avg_toks || 0).toFixed(1) : '0'}</b><span>${t("local.page.tok_s")}</span></div>
      <div class="eh-acts">
        ${status && status.active ? html`<button class="btn" onClick=${() => act('restart')}><${Icon} n="refresh" />${t("local.page.redemarrer")}</button><button class="btn" onClick=${() => act('stop')}>${t("local.page.arreter")}</button>`
          : html`<button class="btn primary" onClick=${() => act('start')}><${Icon} n="play" />${t("local.page.demarrer")}</button>`}
        <button class="icon-btn" aria-label="${t("local.page.journal")}" title="${t("local.page.journal_du_moteur")}" onClick=${async () => { const r = await get('/api/service/log?n=200'); setLog(r.log || ''); }}><${Icon} n="file" /></button>
      </div>
    </div>
    ${status && status.load_error && html`<div class="alert red"><${Icon} n="alert" /><span>${status.load_error}</span></div>`}
    <div class="grid2">
      <div class="card pad">
        <div class="sec-h"><h2>${t("local.page.slots")}</h2><span class="muted">${slots[0] ? Math.round(slots[0].ctx / 1024) + t("local.page.k_de_contexte_par_slot") : ''}</span></div>
        <div class="slots">${slots.map(s => html`<div class=${cls('slot', s.busy && 'busy')}><span class="mono">#${s.id}</span>
          <div class="meter"><i style=${`width:${s.busy && s.ctx ? Math.min(100, (s.prompt + s.tokens) * 100 / s.ctx) : 0}%;background:var(--blue)`}></i></div>
          <span class="mono">${s.busy ? (s.toks || 0).toFixed(1) + ' tok/s' : t('local.page.free')}</span></div>`)}</div>
        <div class="np-row">${srv?.np_supported === false ? html`<a class="btn sm" href="#/engine">${t("node.vllm_parameters")}</a>` : html`<span>${t("local.page.slots_paralleles")}<${Tip} text="${t("local.page.requetes_traitees_en_meme_temps_le_contexte_est_partage_entre_les")}" /></span>
          <input class="input sm num" type="number" min="1" max="32" value=${np} onInput=${e => setNp(e.target.value)} />
          <button class="btn sm" onClick=${async () => { const r = await post('/api/server', { np: +np }); if (!r.ok) toast(r.error, 'err'); else toast(t("local.page.rechargement_avec") + np + ' slots…'); }}>${t("local.page.appliquer")}</button>`}</div>
      </div>
      <div class="card pad">
        <div class="sec-h"><h2>${t("local.page.api_compatible_openai")}</h2>${srv && html`<span class=${'tag ' + (srv.lan ? 'amber' : 'green')}>${srv.lan ? t("local.page.reseau_local_2") : t("local.page.cette_machine")}</span>`}</div>
        <div class="url-row"><code class="mono">${srv ? srv.url : '…'}</code><button class="icon-btn" aria-label="${t("local.page.copier_l_url")}" onClick=${() => copy(srv.url)}><${Icon} n="copy" /></button></div>
        <div class="kv"><span>${t("local.page.cle_api")}</span><span>${srv && srv.key_required ? html`<span class="tag green">${t("local.page.exigee")}</span>` : html`<span class="tag">${t("local.page.aucune")}</span>`}</span></div>
        <div class="kv"><span>${t("local.page.expose_sur_le_reseau")}<${Tip} text="${t("local.page.allume_un_autre_appareil_du_reseau_local_peut_utiliser_ce_serveur")}" /></span>
          ${srv?.node_managed ? html`<span class="muted">${t("node.network_managed")}</span>` : html`<${Switch} checked=${srv && srv.lan} label="${t("local.page.reseau_local")}" onChange=${async on => { const r = await post('/api/network', { exposed: on }); if (r.ok === false) toast(r.error, 'err'); load(); }} />`}</div>
        <details class="curl"><summary>${t("local.page.exemple_curl")}</summary><pre class="mono">${curl}</pre><button class="btn sm" onClick=${() => copy(curl)}>${t("local.page.copier")}</button></details>
      </div>
    </div>
    <div class="card pad">
      <div class="sec-h"><h2>${t("local.page.requetes_recentes")}</h2></div>
      ${srv && srv.recent && srv.recent.length ? html`<div class="req-list">${srv.recent.slice(0, 12).map(r => html`<div class="req"><span class="mono">${t("local.page.slot")} ${r.slot}</span><span class="mono muted">${r.prompt || 0} → ${r.tokens || 0} ${t("local.page.tok")}</span><span class="mono muted">${r.ms ? (r.ms / 1000).toFixed(1) + ' s' : ''}</span><span class="mono">${(r.toks || 0).toFixed(1)} ${t("local.page.tok_s")}</span></div>`)}</div>`
        : html`<p class="note">${t("local.page.aucune_requete_depuis_le_demarrage_les_applications_connectees_a")}</p>`}
    </div>
    ${log !== null && html`<${Drawer} title="${t("local.page.journal_du_moteur")}" onClose=${() => setLog(null)}><pre class="log mono">${log || t("local.page.journal_vide")}</pre></${Drawer}>`}
  </div>`;
}

// Moteur lié directement (llama.cpp / vLLM sur une autre machine, sans Loom) :
// Loom choisit seulement le modèle servi ; le reste se gère sur sa machine.
const KIND = { 'llama.cpp': 'llama.cpp', vllm: 'vLLM', get openai() { return t("local.page.serveur_compatible_openai"); } };
function DirectEngine({ node }) {
  const [models, setModels] = useState(null);
  const [cur, setCur] = useState(node.model);
  useEffect(() => { get('/api/models').then(r => setModels(Array.isArray(r) ? r : [])).catch(() => setModels([])); }, [node.url]);
  const pick = async m => { const r = await post('/api/load-model', { model: m }); if (r.ok === false) return toast(r.error, 'err'); setCur(m); refreshEngineNode(); refreshStatus(); toast(t("local.page.modele_utilise") + m); };
  return html`<div class="view page"><div class="page-in wide">
    <${SectionTabs} /><div class="page-head"><div><h1>${t("local.page.local")}</h1><p>${t("local.page.moteur")} ${KIND[node.kind] || node.kind} ${t("local.page.lie_directement_sur")} <b>${node.hostname}</b>${t("local.page.ses_modeles_et_reglages_se_gerent_sur_sa_machine")}</p></div>
      <div class="acts"><a class="btn" href="#/engine">${t("local.page.emplacement_du_moteur")}</a></div></div>
    <div class="card"><div class="sec-h pad-h"><h2>${t("local.page.modeles_servis")} <span class="count">${models ? models.length : ''}</span></h2><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.reachable ? t('common.reachable') : t('common.unreachable')} · <span class="mono">${node.url}</span></span></div>
      ${!models ? html`<div class="skeleton" style="height:80px;margin:0 16px 16px"></div>` : html`<div class="rows">${models.map(m => html`<div class="row" key=${m.value}>
        <div class="grow"><div class="t mono">${m.name}</div></div>
        ${m.value === cur ? html`<span class="state"><i class="dot green"></i>${t("local.page.utilise_par_loom")}</span>` : html`<button class="btn sm" onClick=${() => pick(m.value)}>${t("local.page.utiliser")}</button>`}</div>`)}</div>`}
    </div>
  </div></div>`;
}

// vLLM lancé par Loom sur cette machine : il apparaît comme un moteur lié en
// local, mais se gère ici comme un moteur de la machine.
const ownVllm = node => node && node.direct && node.kind === 'vllm' && /^http:\/\/127\.0\.0\.1:/.test(node.url || '');

export function LocalPage({ route }) {
  if (route.sub === 'engine') { go('engine'); return null; }
  const tab = TABS().some(localT => localT.value === route.sub) ? route.sub : 'library';
  const node = useStore(app, a => a.engineNode);
  const v = useVllm();
  const vllmOn = !!(v.x && v.x.installed);
  const [engine, setEngineState] = useState(() => { try { return localStorage.getItem('loom.local.engine') || ''; } catch (_) { return ''; } });
  const setEngine = e => { setEngineState(e); try { localStorage.setItem('loom.local.engine', e); } catch (_) {} };
  const which = vllmOn ? (engine || (ownVllm(node) ? 'vllm' : 'llama')) : 'llama';
  if (node && node.direct && !ownVllm(node)) return html`<${DirectEngine} node=${node} />`;
  const isV = which === 'vllm';
  return html`<div class="view page"><div class="page-in wide">
    <${SectionTabs} /><div class="page-head"><div><h1>${t("local.page.local")}</h1><p>${isV ? t('vllm.local.lead') : node && !node.direct ? html`${t("local.page.les_modeles_de")} <b>${node.hostname}</b>${t("local.page.servis_par_llama_cpp_sur_cette_autre_machine")}` : t("local.page.les_modeles_de_cette_machine_servis_par_llama_cpp")}</p></div>
      <div class="acts">${vllmOn && html`<${Seg} label=${t('vllm.local.engine')} value=${which} onChange=${setEngine} options=${[{ value: 'llama', label: 'llama.cpp' }, { value: 'vllm', label: 'vLLM' }]} />`}
        <button class="btn primary" onClick=${() => go('local', 'hub')}><${Icon} n="download" />${t("local.page.telecharger_un_modele")}</button></div></div>
    ${!isV && html`<${Strip} />`}
    <${Tabs} value=${tab} options=${TABS()} onChange=${localT => go('local', localT)} label="${t("local.page.local")}" />
    <div class="tab-body" key=${which + tab}>${isV
      ? (tab === 'library' ? html`<${VllmLibrary} onHub=${() => go('local', 'hub')} />` : html`<${VllmHub} />`)
      : (tab === 'library' ? html`<${Library} />` : html`<${Hub} />`)}</div>
  </div></div>`;
}
