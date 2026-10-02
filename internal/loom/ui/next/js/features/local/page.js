// Local : tes modèles sur cette machine, servis par llama.cpp.
// Onglets : Bibliothèque · Hub · Moteur & API.
import { Logo } from '../../ui/logo.js';
import { vendorOf } from '../chat/picker.js';
import { html, useState, useEffect, useRef, useStore, useMemo, cls, fmtBytes, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tabs, Menu, Empty, Tip, Switch } from '../../ui/controls.js';
import { confirm, toast, prompt, Modal } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, engineState, refreshLibrary, refreshStatus, refreshEngineNode } from '../../core/state.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { ParamsEditor, draftSource, liveSource } from '../inspector/params.js';
import { Config } from '../inspector/config.js';
import { Hub } from './hub.js';

const TABS = [{ value: 'library', label: 'Bibliothèque' }, { value: 'hub', label: 'Hub' }, { value: 'engine', label: 'Moteur & API' }];
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
    <span class="est">${gib(e.gpu_mb)} Go<small>${off ? '+' + gib(e.ram_offload_mb) + ' Go en RAM' : total ? 'tient en VRAM' : ''}</small></span>
    ${total > 0 && html`<span class=${cls('gauge', off && 'over')}><i style=${`width:${pct}%`}></i></span>`}
  </span>`;
}

// Bandeau machine : VRAM, RAM, moteur, modèle chargé.
function Strip() {
  const { status, gpus } = useStore(app, s => ({ status: s.status, gpus: s.gpus }));
  const [ram, setRam] = useState(null);
  useEffect(() => { const f = () => get('/api/ram').then(setRam).catch(() => {}); f(); const t = setInterval(f, 5000); return () => clearInterval(t); }, []);
  const g = (gpus || [])[0], st = engineState(status);
  const pct = (u, t) => t ? Math.min(100, Math.round(u * 100 / t)) : 0;
  return html`<div class="card local-strip">
    <div><div class="lbl">${g ? 'VRAM · ' + g.name.replace(/^NVIDIA (GeForce )?/, '') : 'VRAM'}</div>
      ${g ? html`<div class="v"><b>${gib(g.used)} Go</b><small>/ ${gib(g.total)}</small></div><div class="gauge"><i style=${`width:${pct(g.used, g.total)}%`}></i></div>`
        : html`<div class="v"><span class="t">Aucun GPU détecté</span></div>`}</div>
    <div><div class="lbl">RAM système</div>
      ${ram && ram.total ? html`<div class="v"><b>${gib(ram.used)} Go</b><small>/ ${gib(ram.total)}</small></div><div class="gauge"><i style=${`width:${pct(ram.used, ram.total)}%`}></i></div>`
        : html`<div class="v"><span class="t muted">inconnue</span></div>`}</div>
    <div><div class="lbl">Moteur</div><div class="v"><i class=${'dot ' + (status && status.active ? 'green' : '')}></i><span class="t">llama.cpp</span></div>
      <div class="sub">${status ? (status.active ? 'actif · port ' + status.port : 'arrêté') : '…'}</div></div>
    <div><div class="lbl">Chargé</div><div class="v"><span class="t">${status && status.active && status.model ? (status.preset_name || status.model_name || '').replace(/\.gguf$/i, '') : 'Aucun modèle'}</span></div>
      <div class="sub">${status && status.active && status.model ? (st.tone === 'green' ? 'prêt' : st.label) : 'Charge un modèle ci-dessous'}</div></div>
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
    if (!r.ok) return toast(r.error || 'Création impossible', 'err');
    toast('Preset créé · règle-le dans le panneau');
    onCreated({ id: r.id, name: name.trim(), model });
  };
  return html`<${Modal} title="Nouveau preset" sub="Choisis le modèle ; tu régleras ensuite ses paramètres dans le panneau." onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" disabled=${!name.trim() || !model} onClick=${create}>Créer</button>`}>
    <label class="field"><span>Nom</span><input class="input" autofocus placeholder="ex. Qwen 27B · long contexte" value=${name} onInput=${e => setName(e.target.value)} onKeyDown=${e => e.key === 'Enter' && create()} /></label>
    <label class="field"><span>Modèle</span><select class="select" value=${model} onChange=${e => setModel(e.target.value)}>
      ${models.map(m => html`<option value=${m.value || m.path}>${m.name.replace(/\.gguf$/i, '')}</option>`)}</select></label>
    ${!models.length && html`<p class="note">Aucun modèle dans la bibliothèque : télécharge-en un depuis le Hub.</p>`}
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
      if (data.error || typeof data.content !== 'string') throw new Error(data.error || 'Preset indisponible');
      const src = { live: status?.preset_id === p.id, mode: 'preset', model: new Config(data.content).get('MODEL'), base: data.content, presetId: p.id, presetName: p.name, presetIndex: presets.findIndex(x => x.id === p.id) + 1 };
      if (token === request.current) setDraft({ name: p.name, src });
    } catch (e) { if (token === request.current) { setDraft(null); toast(e.message, 'err'); } }
  };
  const closeDraft = () => { request.current++; setDraft(null); };
  const load = async m => { const r = await post('/api/load-model', { model: m.value || m.path }); if (!r.ok) return toast(r.error, 'err'); toast('Chargement de ' + m.name + '…'); setTimeout(refreshStatus, 800); };
  const unload = async () => { if (!await confirm('Décharger le modèle', 'La VRAM est libérée. Le moteur reste prêt.', { ok: 'Décharger' })) return; await post('/api/unload', {}); refreshStatus(); };
  const del = async m => { if (!await confirm('Supprimer le fichier', '« ' + m.name + ' » (' + fmtBytes(m.size) + ') sera supprimé du disque.', { ok: 'Supprimer', danger: true })) return; const r = await post('/api/models/delete', { name: m.path }); if (!r.ok) return toast(r.error, 'err'); refreshLibrary(); };
  const delPreset = async p => { if (!await confirm('Supprimer le preset', '« ' + p.name + ' » sera supprimé. Le modèle reste sur le disque.', { ok: 'Supprimer', danger: true })) return; await post('/api/preset/delete', { id: p.id }); refreshLibrary(); };
  const movePreset = async (p, step) => {
    if (orderBusy.current) return;
    // L’ordre porte toujours sur tout le catalogue, même si la liste est filtrée.
    const ids = presets.map(x => x.id), i = ids.indexOf(p.id), j = i + step;
    if (i < 0 || j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    orderBusy.current = true; setOrdering(true);
    try {
      const r = await post('/api/presets/order', { ids });
      if (!r.ok) throw new Error(r.error || 'Ordre non enregistré');
      await refreshLibrary();
    } catch (_) { toast('Ordre des presets non enregistré', 'err'); }
    finally { orderBusy.current = false; setOrdering(false); }
  };

  return html`<div class="lib">
    <div class="toolbar"><label class="search"><${Icon} n="search" /><input placeholder="Filtrer les modèles et presets…" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <span class="grow"></span></div>
    <section class="sec"><div class="sec-h"><h2>Presets <span class="count">${plist.length}</span><${Tip} text="Un preset = un modèle et ses réglages, enregistrés sous un nom. Tu en charges un en un clic, ici ou dans le sélecteur de la discussion." /></h2>
      <button class="btn sm" onClick=${() => setCreating(true)}><${Icon} n="plus" />Nouveau preset</button></div>
      ${plist.length ? html`<div class="card rows stagger">${plist.map(p => {
        const on = status && status.preset_id === p.id;
        const index = presets.findIndex(x => x.id === p.id);
        return html`<div class="row" key=${p.id}><${Logo} name=${vendorOf(p.model || p.name) === 'Autres' ? p.name : vendorOf(p.model || p.name)} />
          <div class="grow" ...${inspectTrigger(() => openPreset(p), 'Régler ' + p.name)}><div class="t">${p.name}${on && html` <span class=${'tag ' + (status.health ? 'green' : 'amber')}>${status.health ? 'Chargé' : 'Chargement'}</span>`}</div><div class="s">${baseName(p.model || '').replace(/\.gguf$/i, '') || 'preset'}</div></div>
          <span class="order-btns"><button class="icon-btn" aria-label=${'Monter ' + p.name} title="Monter" disabled=${ordering || index === 0} onClick=${() => movePreset(p, -1)}><${Icon} n="chevron" class="up" /><span class="sr">Monter</span></button>
          <button class="icon-btn" aria-label=${'Descendre ' + p.name} title="Descendre" disabled=${ordering || index === presets.length - 1} onClick=${() => movePreset(p, 1)}><${Icon} n="chevron" /><span class="sr">Descendre</span></button></span>
          <button class="icon-btn" aria-label="Régler" title="Régler" onClick=${() => openPreset(p)}><${Icon} n="sliders" /></button>
          ${on ? html`<button class="btn sm" onClick=${unload}>Décharger</button>` : html`<button class="btn sm" onClick=${async () => { await post('/api/switch', { n: presets.findIndex(x => x.id === p.id) + 1 }); toast('Chargement de ' + p.name + '…'); setTimeout(refreshStatus, 800); }}>Charger</button>`}
          <button class="icon-btn" aria-label="Actions" onClick=${e => setMenu({ a: e.currentTarget, items: [{ label: 'Supprimer', icon: 'trash', danger: true, run: () => delPreset(p) }] })}><${Icon} n="more" /></button></div>`;
      })}</div>`
      : html`<div class="card pad"><p class="note">Aucun preset. Crée-en un pour garder un modèle avec ses réglages (contexte, couches GPU, température…) et le recharger en un clic.</p></div>`}
    </section>
    ${creating && html`<${NewPreset} models=${weights} onClose=${() => setCreating(false)} onCreated=${p => { setCreating(false); refreshLibrary().then(() => openPreset(p)); }} />`}
    <section class="sec"><div class="sec-h"><h2>Modèles <span class="count">${weights.length}</span></h2></div>
      ${weights.length ? html`<div class="card table stagger">
        <div class="tr th"><span>Modèle</span><span>Quant</span><span>Fichier</span><span>Mémoire estimée<${Tip} text="Estimation au contexte natif du modèle. Règle-le avant de charger pour qu’il tienne." /></span><span></span></div>
        ${weights.map(m => {
          const on = live === m.name && !(status && status.preset_id);
          return html`<div class="tr" key=${m.path}>
            <span class="cell-id"><${Logo} name=${vendorOf(m.name) === 'Autres' ? m.name : vendorOf(m.name)} />
              <span class="cell-main" ...${inspectTrigger(() => openDraft(m), 'Inspecter ' + m.name)}><b><span class="nm">${m.name.replace(/\.gguf$/i, '')}</span>${on && html`<span class=${'tag ' + (status.health ? 'green' : 'amber')}>${status.health ? 'Chargé' : 'Chargement'}</span>`}</b><small>${m.dir.replace(/^\/home\/[^/]+/, '~')}</small></span></span>
            <span class="mono dim">${quantOf(m.name) || '—'}</span>
            <span class="num dim">${fmtBytes(m.size)}${m.shards > 1 ? ' · ' + m.shards + ' parts' : ''}</span>
            <span><${Estimate} model=${m.value || m.path} /></span>
            <span class="acts">
              <button class="icon-btn" aria-label="Régler" title="Régler" onClick=${() => openDraft(m)}><${Icon} n="sliders" /></button>
              ${on ? html`<button class="btn sm" onClick=${unload}>Décharger</button>` : html`<button class="btn sm" onClick=${() => load(m)}>Charger</button>`}
              <button class="icon-btn" aria-label="Actions" onClick=${e => setMenu({ a: e.currentTarget, items: [{ label: 'Supprimer le fichier', icon: 'trash', danger: true, run: () => del(m) }] })}><${Icon} n="more" /></button>
            </span></div>`;
        })}</div>` : html`<div class="card"><${Empty} icon="chip" title=${q ? 'Aucun résultat' : 'Aucun modèle'} text=${q ? 'Essaie un autre filtre.' : 'Télécharge un GGUF depuis le Hub ou ajoute ton dossier de modèles dans les Réglages.'}>
          ${!q && html`<button class="btn primary" onClick=${() => go('local', 'hub')}>Ouvrir le Hub</button>`}</${Empty}></div>`}
    </section>
    ${menu && html`<${Menu} anchor=${menu.a} onClose=${() => setMenu(null)} items=${menu.items} />`}
    ${draft && html`<${Drawer} title=${'Régler · ' + draft.name.replace(/\.gguf$/i, '')} onClose=${closeDraft}>
      ${draft.src ? html`<${ParamsEditor} key=${draft.src.model} src=${draft.src} onLoaded=${closeDraft} />` : html`<div class="insp-body"><p class="note">Lecture des paramètres…</p></div>`}</${Drawer}>`}
  </div>`;
}

function Engine() {
  const status = useStore(app, s => s.status);
  const [srv, setSrv] = useState(null);
  const [lc, setLc] = useState(null);
  const [np, setNp] = useState('');
  const [log, setLog] = useState(null);
  const load = async () => { try { const s = await get('/api/server'); setSrv(s); if (!np) setNp(String(s.np || '')); } catch (_) {} };
  useEffect(() => { load(); get('/api/llamacpp').then(setLc).catch(() => {}); const t = setInterval(load, 1500); return () => clearInterval(t); }, []);
  const st = engineState(status);
  const act = async a => { const r = await post('/api/' + a, {}); if (r.ok === false) toast(r.out || r.error || 'Action impossible', 'err'); setTimeout(refreshStatus, 600); };
  const copy = t => navigator.clipboard && navigator.clipboard.writeText(t).then(() => toast('Copié'));
  const slots = (srv && srv.slots && srv.slots.items) || [];
  const busy = slots.filter(s => s.busy).length;
  const curl = srv ? `curl ${srv.url}/chat/completions \\\n  -H 'Content-Type: application/json' \\\n  -d '{"model":"${srv.model_name || 'loom'}","messages":[{"role":"user","content":"Bonjour"}]}'` : '';
  return html`<div class="engine stagger">
    <div class="card engine-hero">
      <div class="eh-main"><div class="eh-state"><i class=${'dot ' + st.tone}></i><b>${status && status.active ? (status.health ? 'Moteur actif' : status.model ? 'Chargement du modèle' : 'Moteur prêt') : 'Moteur arrêté'}</b></div>
        <span class="mono muted">${lc ? `llama.cpp ${lc.commit || ''} · ${lc.plan && lc.plan.backend ? lc.plan.backend.toUpperCase() : lc.kind} · ${srv ? srv.url : ''}` : '…'}</span></div>
      <div class="stat"><b>${srv && srv.model_name ? baseName(srv.model_name).replace(/\.gguf$/i, '') : '—'}</b><span>modèle</span></div>
      <div class="stat"><b>${busy} / ${slots.length || (srv && srv.np) || 0}</b><span>slots actifs</span></div>
      <div class="stat"><b>${srv && srv.stats ? (srv.stats.live_toks || srv.stats.avg_toks || 0).toFixed(1) : '0'}</b><span>tok/s</span></div>
      <div class="eh-acts">
        ${status && status.active ? html`<button class="btn" onClick=${() => act('restart')}><${Icon} n="refresh" />Redémarrer</button><button class="btn" onClick=${() => act('stop')}>Arrêter</button>`
          : html`<button class="btn primary" onClick=${() => act('start')}><${Icon} n="play" />Démarrer</button>`}
        <button class="icon-btn" aria-label="Journal" title="Journal du moteur" onClick=${async () => { const r = await get('/api/service/log?n=200'); setLog(r.log || ''); }}><${Icon} n="file" /></button>
      </div>
    </div>
    ${status && status.load_error && html`<div class="alert red"><${Icon} n="alert" /><span>${status.load_error}</span></div>`}
    <div class="grid2">
      <div class="card pad">
        <div class="sec-h"><h2>Slots</h2><span class="muted">${slots[0] ? Math.round(slots[0].ctx / 1024) + 'K de contexte par slot' : ''}</span></div>
        <div class="slots">${slots.map(s => html`<div class=${cls('slot', s.busy && 'busy')}><span class="mono">#${s.id}</span>
          <div class="meter"><i style=${`width:${s.busy && s.ctx ? Math.min(100, (s.prompt + s.tokens) * 100 / s.ctx) : 0}%;background:var(--blue)`}></i></div>
          <span class="mono">${s.busy ? (s.toks || 0).toFixed(1) + ' tok/s' : 'libre'}</span></div>`)}</div>
        <div class="np-row"><span>Slots parallèles<${Tip} text="Requêtes traitées en même temps. Le contexte est partagé entre les slots." /></span>
          <input class="input sm num" type="number" min="1" max="32" value=${np} onInput=${e => setNp(e.target.value)} />
          <button class="btn sm" onClick=${async () => { const r = await post('/api/server', { np: +np }); if (!r.ok) toast(r.error, 'err'); else toast('Rechargement avec ' + np + ' slots…'); }}>Appliquer</button></div>
      </div>
      <div class="card pad">
        <div class="sec-h"><h2>API compatible OpenAI</h2>${srv && html`<span class=${'tag ' + (srv.lan ? 'amber' : 'green')}>${srv.lan ? 'réseau local' : 'cette machine'}</span>`}</div>
        <div class="url-row"><code class="mono">${srv ? srv.url : '…'}</code><button class="icon-btn" aria-label="Copier l’URL" onClick=${() => copy(srv.url)}><${Icon} n="copy" /></button></div>
        <div class="kv"><span>Clé API</span><span>${srv && srv.key_required ? html`<span class="tag green">exigée</span>` : html`<span class="tag">aucune</span>`}</span></div>
        <div class="kv"><span>Exposé sur le réseau<${Tip} text="Allumé, un autre appareil du réseau local peut utiliser ce serveur. Une clé API est alors exigée." /></span>
          <${Switch} checked=${srv && srv.lan} label="Réseau local" onChange=${async on => { const r = await post('/api/network', { exposed: on }); if (r.ok === false) toast(r.error, 'err'); load(); }} /></div>
        <details class="curl"><summary>Exemple curl</summary><pre class="mono">${curl}</pre><button class="btn sm" onClick=${() => copy(curl)}>Copier</button></details>
      </div>
    </div>
    <div class="card pad">
      <div class="sec-h"><h2>Requêtes récentes</h2></div>
      ${srv && srv.recent && srv.recent.length ? html`<div class="req-list">${srv.recent.slice(0, 12).map(r => html`<div class="req"><span class="mono">slot ${r.slot}</span><span class="mono muted">${r.prompt || 0} → ${r.tokens || 0} tok</span><span class="mono muted">${r.ms ? (r.ms / 1000).toFixed(1) + ' s' : ''}</span><span class="mono">${(r.toks || 0).toFixed(1)} tok/s</span></div>`)}</div>`
        : html`<p class="note">Aucune requête depuis le démarrage. Les applications connectées à l’API apparaîtront ici.</p>`}
    </div>
    ${log !== null && html`<${Drawer} title="Journal du moteur" onClose=${() => setLog(null)}><pre class="log mono">${log || 'Journal vide.'}</pre></${Drawer}>`}
  </div>`;
}

// Moteur lié directement (llama.cpp / vLLM sur une autre machine, sans Loom) :
// Loom choisit seulement le modèle servi ; le reste se gère sur sa machine.
const KIND = { 'llama.cpp': 'llama.cpp', vllm: 'vLLM', openai: 'serveur compatible OpenAI' };
function DirectEngine({ node }) {
  const [models, setModels] = useState(null);
  const [cur, setCur] = useState(node.model);
  useEffect(() => { get('/api/models').then(r => setModels(Array.isArray(r) ? r : [])).catch(() => setModels([])); }, [node.url]);
  const pick = async m => { const r = await post('/api/load-model', { model: m }); if (r.ok === false) return toast(r.error, 'err'); setCur(m); refreshEngineNode(); refreshStatus(); toast('Modèle utilisé : ' + m); };
  return html`<div class="view page"><div class="page-in wide">
    <div class="page-head"><div><h1>Local</h1><p>Moteur ${KIND[node.kind] || node.kind} lié directement sur <b>${node.hostname}</b>. Ses modèles et réglages se gèrent sur sa machine.</p></div>
      <div class="acts"><a class="btn" href="#/settings/engine">Emplacement du moteur</a></div></div>
    <div class="card"><div class="sec-h pad-h"><h2>Modèles servis <span class="count">${models ? models.length : ''}</span></h2><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.reachable ? 'joignable' : 'injoignable'} · <span class="mono">${node.url}</span></span></div>
      ${!models ? html`<div class="skeleton" style="height:80px;margin:0 16px 16px"></div>` : html`<div class="rows">${models.map(m => html`<div class="row" key=${m.value}>
        <div class="grow"><div class="t mono">${m.name}</div></div>
        ${m.value === cur ? html`<span class="state"><i class="dot green"></i>utilisé par Loom</span>` : html`<button class="btn sm" onClick=${() => pick(m.value)}>Utiliser</button>`}</div>`)}</div>`}
    </div>
  </div></div>`;
}

export function LocalPage({ route }) {
  const tab = TABS.some(t => t.value === route.sub) ? route.sub : 'library';
  const node = useStore(app, a => a.engineNode);
  if (node && node.direct) return html`<${DirectEngine} node=${node} />`;
  return html`<div class="view page"><div class="page-in wide">
    <div class="page-head"><div><h1>Local</h1><p>${node ? html`Les modèles de <b>${node.hostname}</b>, servis par llama.cpp sur cette autre machine.` : 'Les modèles de cette machine, servis par llama.cpp.'}</p></div>
      <div class="acts"><button class="btn primary" onClick=${() => go('local', 'hub')}><${Icon} n="download" />Télécharger un modèle</button></div></div>
    <${Strip} />
    <${Tabs} value=${tab} options=${TABS} onChange=${t => go('local', t)} label="Local" />
    <div class="tab-body" key=${tab}>${tab === 'library' ? html`<${Library} />` : tab === 'hub' ? html`<${Hub} />` : html`<${Engine} />`}</div>
  </div></div>`;
}
